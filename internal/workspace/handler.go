package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/email"
)

// Default customer + user used when running in OSS single-tenant mode
// without a session middleware. Bastio Cloud installs a middleware that
// replaces these via context.
const (
	defaultCustomerIDStr = "00000000-0000-0000-0000-000000000001"
	defaultUserID        = "default-user"
)

// Context keys used by the cloud auth middleware. Public so the cloud
// repo can populate them without importing private symbols.
type ctxKey int

const (
	ctxCustomerID ctxKey = iota + 1
	ctxUserID
	ctxUserEmail
	ctxRole
)

// CustomerIDKey is the context key the cloud auth middleware writes into.
// Must contain a uuid.UUID.
var CustomerIDKey any = ctxCustomerID

// UserIDKey is the context key the cloud auth middleware writes into.
// Must contain a string (Better Auth subject).
var UserIDKey any = ctxUserID

// UserEmailKey is the context key for the authenticated user's email.
// Must contain a string.
var UserEmailKey any = ctxUserEmail

// RoleKey is the public ctx key middlewares use to stash a member's
// role for the duration of a request. RBAC enforcement reads this
// via RoleFromCtx and 403s callers below the threshold for the
// route. Cloud's auth middleware sets it after a workspace_members
// lookup; OSS's tenant middleware sets it to RoleOwner unconditionally
// (single-tenant, no enforcement makes sense).
var RoleKey any = ctxRole

// KeyResolver returns the API key the workspace should use for a given
// provider. Cloud injects a resolver that walks the customer's
// provider_keys; OSS dev falls back to environment variables. Returning
// an empty key is allowed — the provider client surfaces an auth error
// downstream and the user sees a clear message.
//
// `provider` is a plain string (e.g. "openai", "anthropic") so cloud
// implementations don't need to import the OSS internal/providers
// package to satisfy this interface.
type KeyResolver interface {
	ResolveKey(ctx context.Context, customerID uuid.UUID, provider string) (string, error)
}

// Handler exposes all workspace HTTP routes.
type Handler struct {
	store         *Store
	registry      *providers.Registry
	keys          KeyResolver
	blobs         BlobStore
	river         *river.Client[pgx.Tx]
	txtResolver   TXTResolver     // nil → defaultTXTResolver
	embedder      EmbeddingClient // nil → keyword-only RAG
	emailer       email.Client    // nil → invitation token returned in response only
	publicBaseURL string          // accept-URL prefix; empty → emails skipped
	// inviteBaseURL overrides publicBaseURL specifically for invitation
	// links. The chat product (workspace.bastio.com) is the right
	// landing surface for invitees — they don't need to round-trip
	// through the cloud admin dashboard. Empty → fall back to
	// publicBaseURL so existing single-host deployments keep working
	// unchanged.
	inviteBaseURL string
	// Security pipeline — same engine/profile/recorder the gateway uses.
	// All optional: when nil the workspace falls back to its pre-pipeline
	// behavior (direct provider call, no policy enforcement, no
	// observability writes to ClickHouse). The OSS server wires all
	// three at startup via the existing setters.
	secEngine     *security.Engine
	secProfiles   security.ProfileLookup
	obsRecorder   *observability.Recorder
}

// NewHandler wires the workspace handler. registry is the same provider
// registry the OSS gateway uses, so workspace traffic and gateway traffic
// share the same client objects (same connection pools, retry behavior,
// rate-limit accounting).
//
// blobs and riverClient may be nil — when nil, the file-upload endpoint
// returns a clear 503 telling the operator to configure BASTIO_DATA_DIR
// and the worker. Inline-text knowledge sources work without either.
func NewHandler(store *Store, registry *providers.Registry, keys KeyResolver) *Handler {
	return &Handler{store: store, registry: registry, keys: keys}
}

// SetBlobStore wires a blob store for file uploads. Optional — without
// it, /knowledge/upload returns 503 and inline-text sources still work.
func (h *Handler) SetBlobStore(b BlobStore) { h.blobs = b }

// SetKeyResolver wires the customer-scoped LLM provider key lookup.
// Cloud injects a resolver against `proxy_provider_keys`; OSS leaves it
// nil and falls back to environment variables.
func (h *Handler) SetKeyResolver(k KeyResolver) { h.keys = k }

// SetRiverClient wires the River client used to enqueue ingestion jobs.
// Without one, the upload endpoint runs the worker inline (synchronous)
// so the OSS server-only deployment stays useful.
func (h *Handler) SetRiverClient(c *river.Client[pgx.Tx]) { h.river = c }

// SetTXTResolver swaps the DNS TXT lookup function — useful for tests
// (no real DNS) and for cloud deployments that want to route DNS lookups
// through a vetted recursive resolver. Nil restores the default.
func (h *Handler) SetTXTResolver(r TXTResolver) { h.txtResolver = r }

// SetEmbeddingClient wires the embedding client used for RAG. Without
// one, retrieval falls back to keyword search. Cloud injects a client
// that walks the customer's `provider_keys`; OSS dev uses an
// `OPENAI_API_KEY`-backed client built lazily from the env.
func (h *Handler) SetEmbeddingClient(e EmbeddingClient) { h.embedder = e }

// SetEmailClient wires the email client used to send invitation emails.
// Without it, createInvitation still succeeds and returns the bearer
// token in the response so the inviter can copy/paste it manually.
func (h *Handler) SetEmailClient(c email.Client) { h.emailer = c }

// SetPublicBaseURL sets the public URL prefix used in invitation
// accept links. e.g. "https://cloud.bastio.com" → emails contain
// "https://cloud.bastio.com/accept-invite?token=...". Empty string
// suppresses email sending; the bearer token still lands in the
// createInvitation response for manual delivery.
func (h *Handler) SetPublicBaseURL(u string) { h.publicBaseURL = u }

// SetInviteBaseURL overrides the URL prefix used specifically for
// invitation accept links — typically the workspace chat host
// (workspace.bastio.com). Set this in cloud deployments where the
// chat product and admin dashboard live on different subdomains
// so invitees land directly on the chat (no second sign-in across
// hosts). Empty → invitation links use publicBaseURL, which keeps
// OSS single-host deployments working without any extra config.
func (h *Handler) SetInviteBaseURL(u string) { h.inviteBaseURL = u }

// inviteAcceptBaseURL returns the URL prefix to use when generating
// invitation accept links. inviteBaseURL takes precedence; falls
// back to publicBaseURL.
func (h *Handler) inviteAcceptBaseURL() string {
	if h.inviteBaseURL != "" {
		return h.inviteBaseURL
	}
	return h.publicBaseURL
}

// SetSecurityEngine wires the security engine the workspace runs
// against every chat send (mirrors the gateway's pre-flight scan).
// When set together with SetSecurityProfiles, runProvider /
// streamProvider scan the user's prompt before calling the LLM and
// honor block/redact/warn actions from the matched profile.
func (h *Handler) SetSecurityEngine(e *security.Engine) { h.secEngine = e }

// SetSecurityProfiles wires the per-customer security profile lookup.
// Without it, workspace chat skips the security pipeline and behaves
// as before (direct provider call, no policy enforcement).
func (h *Handler) SetSecurityProfiles(p security.ProfileLookup) { h.secProfiles = p }

// SetObservabilityRecorder wires the ClickHouse recorder so workspace
// chat traffic appears in the same trace + threat catalog the
// gateway populates. Without it, workspace messages still land in
// workspace_messages (PG) but skip the observability tables.
func (h *Handler) SetObservabilityRecorder(r *observability.Recorder) { h.obsRecorder = r }

// Pool exposes the underlying pgxpool.Pool so customizers (e.g. cloud's
// WithWorkspaceCustomize hook) can construct stores that share the OSS
// server's connection pool — instead of opening a second pool to the
// same database. Returns the pool the workspace store was built with.
func (h *Handler) Pool() *pgxpool.Pool {
	if h.store == nil {
		return nil
	}
	return h.store.pool
}

// Routes returns the chi router mounted at /v1/workspace.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	// =========================================================
	// RBAC: every route below this point gates by role. The
	// hierarchy is owner > admin > member > viewer; RequireRole
	// 403s callers below the threshold. OSS callers are stamped
	// "owner" by pkg/server's default middleware (single-tenant,
	// no enforcement makes sense). Cloud's auth middleware
	// resolves the actual workspace_members.role.
	//
	// Read endpoints (GET) are viewer-min so auditors can observe
	// without participating. Writes that affect only the caller's
	// own data (their conversations, their messages) are member-
	// min. Workspace-wide writes (assistants, knowledge,
	// settings, members, domains) are admin-min. Owner-only
	// actions (transfer, billing) get a separate gate.
	// =========================================================

	// Status + whoami are viewer-min — even read-only auditors
	// need to see whether the workspace is enabled and who they
	// are. /whoami is the frontend's role-discovery endpoint.
	r.With(RequireRole(RoleViewer)).Get("/status", h.status)
	r.With(RequireRole(RoleViewer)).Get("/whoami", h.whoami)

	r.With(RequireRole(RoleViewer)).Get("/settings", h.getSettings)
	r.With(RequireRole(RoleAdmin)).Patch("/settings", h.patchSettings)
	// Per-user effective allowed-models — every member has their
	// own effective list; viewers can see what they'd be allowed
	// even though they can't send.
	r.With(RequireRole(RoleViewer)).Get("/me/effective-models", h.getEffectiveModels)

	r.Route("/assistants", func(r chi.Router) {
		r.With(RequireRole(RoleViewer)).Get("/", h.listAssistants)
		r.With(RequireRole(RoleAdmin)).Post("/", h.createAssistant)
		r.With(RequireRole(RoleViewer)).Get("/{id}", h.getAssistant)
		r.With(RequireRole(RoleAdmin)).Patch("/{id}", h.updateAssistant)
		r.With(RequireRole(RoleAdmin)).Delete("/{id}", h.archiveAssistant)
	})

	r.Route("/knowledge", func(r chi.Router) {
		r.With(RequireRole(RoleViewer)).Get("/", h.listKnowledge)
		r.With(RequireRole(RoleAdmin)).Post("/", h.createKnowledge)
		r.With(RequireRole(RoleAdmin)).Post("/upload", h.uploadKnowledge)
		r.With(RequireRole(RoleViewer)).Get("/{id}", h.getKnowledge)
		r.With(RequireRole(RoleAdmin)).Delete("/{id}", h.archiveKnowledge)
		// Admin-only release: a security-quarantined source flips back
		// to status='pending' so the worker re-ingests it. Useful when
		// the scan was a false positive (e.g. a doc legitimately
		// discusses passwords as part of a security policy and the
		// secrets detector tripped). Audited prominently.
		r.With(RequireRole(RoleAdmin)).Post("/{id}/release", h.releaseKnowledge)
	})

	// Ephemeral chat attachments — uploading for context to send is a
	// member action (viewers can't send messages, so no need for them
	// to upload either).
	r.With(RequireRole(RoleMember)).Post("/chat-attachments", h.uploadChatAttachment)

	r.Route("/conversations", func(r chi.Router) {
		r.With(RequireRole(RoleViewer)).Get("/", h.listConversations)
		r.With(RequireRole(RoleMember)).Post("/", h.createConversation)
		r.With(RequireRole(RoleViewer)).Get("/{id}", h.getConversation)
		r.With(RequireRole(RoleMember)).Patch("/{id}", h.updateConversation)
		r.With(RequireRole(RoleMember)).Delete("/{id}", h.archiveConversation)
		r.With(RequireRole(RoleViewer)).Get("/{id}/messages", h.listMessages)
		r.With(RequireRole(RoleMember)).Post("/{id}/messages", h.sendMessage)
		r.With(RequireRole(RoleMember)).Post("/{id}/messages/stream", h.streamSendMessage)
		r.With(RequireRole(RoleMember)).Delete("/{id}/messages/{messageID}", h.deleteFromMessage)
	})

	r.Route("/members", func(r chi.Router) {
		// Listing members is viewer-readable — everyone can see
		// who's in the workspace.
		r.With(RequireRole(RoleViewer)).Get("/", h.listMembers)
		r.With(RequireRole(RoleAdmin)).Patch("/{userID}/budgets", h.setMemberBudgets)
		r.With(RequireRole(RoleAdmin)).Patch("/{userID}/role", h.changeMemberRole)
		r.With(RequireRole(RoleAdmin)).Delete("/{userID}", h.removeMember)
	})

	// Owner transfer — only the existing owner can hand off
	// the workspace. Picks an existing member and promotes them
	// to owner; the previous owner becomes admin.
	r.With(RequireRole(RoleOwner)).Post("/owner/transfer", h.transferOwnership)

	r.Route("/invitations", func(r chi.Router) {
		r.With(RequireRole(RoleViewer)).Get("/", h.listInvitations)
		r.With(RequireRole(RoleAdmin)).Post("/", h.createInvitation)
		// Accept is intentionally NOT role-gated — the caller is
		// signed in but might not yet be a workspace member. The
		// handler authorizes via the bearer token in the body,
		// which encodes the inviter's intent.
		r.Post("/accept", h.acceptInvitation)
		r.With(RequireRole(RoleAdmin)).Delete("/{id}", h.revokeInvitation)
	})

	r.Route("/analytics", func(r chi.Router) {
		// All analytics are viewer-readable — observation is the
		// whole point of the role.
		r.Use(RequireRole(RoleViewer))
		r.Get("/summary", h.analyticsSummary)
		r.Get("/daily", h.analyticsDaily)
		r.Get("/by-model", h.analyticsByModel)
		r.Get("/users", h.analyticsTopUsers)
		r.Get("/users/{userID}", h.analyticsUserDetail)
		r.Get("/forecast", h.analyticsForecast)
		r.Get("/compare", h.analyticsCompare)
	})

	// Audit log read — admin+. Every privileged mutation in the
	// workspace handlers writes a row via h.audit; this is the
	// single endpoint admins use to see who did what.
	r.With(RequireRole(RoleAdmin)).Get("/audit", h.listAudit)

	// Branded chat admin: slug + custom-domain CRUD with DNS
	// verification. Admin-only (the public-facing branded routes
	// are mounted outside the auth group via PublicRoutes / HostRoutes).
	r.With(RequireRole(RoleAdmin)).Put("/settings/slug", h.setSlug)
	r.Route("/domains", func(r chi.Router) {
		r.With(RequireRole(RoleViewer)).Get("/", h.listDomains)
		r.With(RequireRole(RoleAdmin)).Post("/", h.createDomain)
		r.With(RequireRole(RoleAdmin)).Post("/{id}/verify", h.verifyDomain)
		r.With(RequireRole(RoleAdmin)).Delete("/{id}", h.deleteDomain)
	})

	return r
}

// status reports whether the workspace product is enabled for this caller.
// In OSS this always returns ok with note=oss-default. In cloud the auth
// middleware can short-circuit with 402 to gate behind a paid plan.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	settings, err := h.store.EnsureSettings(r.Context(), cid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"customer_id": cid,
		"settings":    settings,
	})
}

// =============================================================================
// helpers
// =============================================================================

func customerIDFromCtx(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxCustomerID).(uuid.UUID); ok {
		return v
	}
	return uuid.MustParse(defaultCustomerIDStr)
}

func userIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUserID).(string); ok && v != "" {
		return v
	}
	return defaultUserID
}

func userEmailFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUserEmail).(string); ok {
		return v
	}
	return ""
}

func uuidParam(r *http.Request, name string) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, name))
}

func intQuery(r *http.Request, name string, fallback int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStructuredError writes a 4xx response with a stable machine-
// readable `code` field plus arbitrary extra context fields. Used for
// errors the dashboard branches on — currently the seat-limit-reached
// 402, which exposes consumed/limit so the UI can render a precise
// "you're at N of M seats" message.
func writeStructuredError(w http.ResponseWriter, status int, code, msg string, extras map[string]any) {
	body := map[string]any{"error": msg, "code": code}
	maps.Copy(body, extras)
	writeJSON(w, status, body)
}

// notFoundOr500 maps ErrNotFound to 404 and otherwise returns 500.
func notFoundOr500(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
