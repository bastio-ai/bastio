package overlay

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/pkg/tenant"
)

// Handler exposes the /api/v1/overlays surface. The tenant resolver
// follows the same pattern as DetectHandler: OSS defaults to the
// constant tenant; cloud swaps it via SetTenantResolver.
type Handler struct {
	store   *Store
	loader  *Loader
	tenant  func(context.Context) uuid.UUID
	// actorFn resolves the acting principal for audit entries. OSS
	// returns the empty string (no auth); cloud swaps in the user id.
	actorFn func(context.Context) string
	// analyzer produces loosening warnings for a snapshot. Injected at
	// construction time from the security package; nil disables the
	// "warnings" field on version responses (no UI banners). Runtime
	// behaviour is unaffected either way.
	analyzer WarningAnalyzer
	// previewer runs preview/validate scans against candidate
	// snapshots. Injected from the security package. Nil disables
	// the preview endpoint with 501.
	previewer PreviewRunner
}

// NewHandler wires the handler over a store and loader. Loader is used
// to invalidate the cache on activation / rollback.
func NewHandler(store *Store, loader *Loader) *Handler {
	return &Handler{
		store:  store,
		loader: loader,
		// Read tenant from request context (OSSMiddleware default in
		// single-tenant; cloud auth override in multi-tenant). Falls
		// back to DefaultOSSID only when no middleware ran.
		tenant: func(ctx context.Context) uuid.UUID {
			if id, err := tenant.FromContext(ctx); err == nil {
				return id
			}
			return tenant.DefaultOSSID
		},
		actorFn: func(_ context.Context) string { return "" },
	}
}

// SetTenantResolver lets cloud inject a session-aware resolver.
func (h *Handler) SetTenantResolver(fn func(context.Context) uuid.UUID) {
	h.tenant = fn
}

// SetActorResolver lets cloud inject a user/principal resolver for audit.
func (h *Handler) SetActorResolver(fn func(context.Context) string) {
	h.actorFn = fn
}

// SetWarningAnalyzer installs the analyzer that computes loosening
// warnings for snapshot overrides. The security package provides the
// implementation; nil disables the feature.
func (h *Handler) SetWarningAnalyzer(a WarningAnalyzer) {
	h.analyzer = a
}

// SetPreviewRunner installs the runner that executes preview /
// validate scans. The security package provides the implementation
// because it depends on the engine; injection keeps the overlay
// package engine-free.
func (h *Handler) SetPreviewRunner(p PreviewRunner) {
	h.previewer = p
}

// analyzeWarnings returns the warnings for a snapshot when an
// analyzer is configured, nil otherwise. Safe to call with a nil
// handler analyzer.
func (h *Handler) analyzeWarnings(snap *OverlaySnapshot) []Warning {
	if h.analyzer == nil || snap == nil {
		return nil
	}
	return h.analyzer(snap)
}

// Routes returns the Chi router for the overlay API.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListOverlays)
	r.Post("/", h.CreateOverlay)
	r.Post("/from-template", h.CreateFromTemplate)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetOverlay)
		r.Delete("/", h.DeleteOverlay)
		r.Get("/versions", h.ListVersions)
		r.Post("/versions", h.CreateVersion)
		r.Get("/versions/{n}", h.GetVersion)
		r.Post("/versions/{n}/shadow", h.PromoteShadow)
		r.Post("/versions/{n}/activate", h.Activate)
		r.Post("/versions/{n}/preview", h.PreviewVersion)
		r.Post("/rollback", h.Rollback)
		r.Get("/audit", h.ListAudit)
		r.Get("/shadow-events", h.ListShadowEvents)
	})
	return r
}

// TemplatesRoutes returns a router for /api/v1/overlay-templates.
// Mounted separately because it's a collection sibling to /overlays,
// not a subroute.
func (h *Handler) TemplatesRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListTemplates)
	r.Get("/{slug}", h.GetTemplate)
	return r
}

// ---- request / response envelopes ----

type createOverlayReq struct {
	Name          string           `json:"name"`
	Description   string           `json:"description"`
	ProxyID       string           `json:"proxy_id,omitempty"` // optional; empty = customer-wide
	Snapshot      *OverlaySnapshot `json:"snapshot"`
	CommitMessage string           `json:"commit_message"`
	Source        string           `json:"source,omitempty"`
}

type createVersionReq struct {
	Snapshot      *OverlaySnapshot `json:"snapshot"`
	CommitMessage string           `json:"commit_message"`
	Source        string           `json:"source,omitempty"`
}

type stateTransitionReq struct {
	Reason string `json:"reason"`
}

type fromTemplateReq struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ProxyID       string `json:"proxy_id,omitempty"`
	CommitMessage string `json:"commit_message"`
}

// ---- handlers ----

func (h *Handler) ListOverlays(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	list, err := h.store.ListOverlays(r.Context(), customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list overlays failed")
		slog.Error("overlay: list", "error", err)
		return
	}
	if list == nil {
		list = []Overlay{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"overlays": list})
}

func (h *Handler) CreateOverlay(w http.ResponseWriter, r *http.Request) {
	var req createOverlayReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		req.Name = "default"
	}
	if req.Snapshot == nil {
		writeError(w, http.StatusBadRequest, "snapshot is required")
		return
	}
	if req.Snapshot.SchemaVersion == 0 {
		req.Snapshot.SchemaVersion = CurrentSchemaVersion
	}

	var proxyIDPtr *uuid.UUID
	if req.ProxyID != "" {
		id, err := uuid.Parse(req.ProxyID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid proxy_id")
			return
		}
		proxyIDPtr = &id
	}

	customerID := h.tenant(r.Context())
	actor := h.actorFn(r.Context())
	source := req.Source
	if source == "" {
		source = "manual"
	}

	o, v, err := h.store.CreateOverlay(
		r.Context(), customerID, proxyIDPtr,
		req.Name, req.Description, actor, req.CommitMessage, source,
		req.Snapshot,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"overlay": o, "version": v})
}

func (h *Handler) GetOverlay(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	o, err := h.store.GetOverlay(r.Context(), customerID, id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "overlay not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get overlay failed")
		slog.Error("overlay: get", "error", err)
		return
	}

	// Include the active version if present, plus any loosening
	// warnings the UI should surface as a banner.
	resp := map[string]any{"overlay": o}
	if o.ActiveVersionID != nil {
		if v, err := h.store.GetActiveVersion(r.Context(), customerID, o.ID); err == nil {
			resp["active_version"] = v
			if warnings := h.analyzeWarnings(&v.Snapshot); len(warnings) > 0 {
				resp["active_warnings"] = warnings
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteOverlay(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.store.DeleteOverlay(r.Context(), customerID, id, h.actorFn(r.Context())); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "overlay not found")
		case errors.Is(err, ErrInvalidState):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "delete overlay failed")
			slog.Error("overlay: delete", "error", err)
		}
		return
	}
	// Best-effort cache invalidation (proxy_id unknown at this point;
	// the Invalidate helper clears the customer-wide keys too).
	_ = h.loader.Invalidate(r.Context(), customerID, uuid.Nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	list, err := h.store.ListVersions(r.Context(), customerID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list versions failed")
		slog.Error("overlay: list versions", "error", err)
		return
	}
	if list == nil {
		list = []Version{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": list})
}

func (h *Handler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req createVersionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Snapshot == nil {
		writeError(w, http.StatusBadRequest, "snapshot is required")
		return
	}
	if req.Snapshot.SchemaVersion == 0 {
		req.Snapshot.SchemaVersion = CurrentSchemaVersion
	}
	source := req.Source
	if source == "" {
		source = "manual"
	}
	v, err := h.store.CreateVersion(r.Context(), customerID, id, req.Snapshot, source, req.CommitMessage, h.actorFn(r.Context()))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "overlay not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"version": v})
}

func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	n, ok := parseVersionParam(w, r)
	if !ok {
		return
	}
	v, err := h.store.GetVersion(r.Context(), customerID, id, n)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get version failed")
		slog.Error("overlay: get version", "error", err)
		return
	}
	resp := map[string]any{"version": v}
	if warnings := h.analyzeWarnings(&v.Snapshot); len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) PromoteShadow(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	n, ok := parseVersionParam(w, r)
	if !ok {
		return
	}
	var req stateTransitionReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.store.PromoteToShadow(r.Context(), customerID, id, n, h.actorFn(r.Context()), req.Reason); err != nil {
		h.writeStateError(w, err, "promote to shadow failed")
		return
	}
	_ = h.loader.Invalidate(r.Context(), customerID, uuid.Nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	n, ok := parseVersionParam(w, r)
	if !ok {
		return
	}
	var req stateTransitionReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.store.Activate(r.Context(), customerID, id, n, h.actorFn(r.Context()), req.Reason); err != nil {
		h.writeStateError(w, err, "activate failed")
		return
	}
	_ = h.loader.Invalidate(r.Context(), customerID, uuid.Nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req stateTransitionReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.store.Rollback(r.Context(), customerID, id, h.actorFn(r.Context()), req.Reason); err != nil {
		h.writeStateError(w, err, "rollback failed")
		return
	}
	_ = h.loader.Invalidate(r.Context(), customerID, uuid.Nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListAudit(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	list, err := h.store.ListAudit(r.Context(), customerID, id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list audit failed")
		slog.Error("overlay: list audit", "error", err)
		return
	}
	if list == nil {
		list = []AuditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list})
}

func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListTemplates(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list templates failed")
		slog.Error("overlay: list templates", "error", err)
		return
	}
	if list == nil {
		list = []Template{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": list})
}

func (h *Handler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}
	t, err := h.store.GetTemplateBySlug(r.Context(), slug)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get template failed")
		slog.Error("overlay: get template", "error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"template": t})
}

// CreateFromTemplate creates a new overlay whose version 1 is seeded
// from a built-in template's snapshot. The request may override name,
// description, and proxy scope; the snapshot itself is copied verbatim
// from the template.
func (h *Handler) CreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	var req fromTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}
	tpl, err := h.store.GetTemplateBySlug(r.Context(), req.Slug)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "template lookup failed")
		slog.Error("overlay: template lookup", "error", err)
		return
	}

	name := req.Name
	if name == "" {
		name = tpl.Slug
	}
	description := req.Description
	if description == "" {
		description = tpl.Description
	}

	var proxyIDPtr *uuid.UUID
	if req.ProxyID != "" {
		id, perr := uuid.Parse(req.ProxyID)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid proxy_id")
			return
		}
		proxyIDPtr = &id
	}

	snap := tpl.Snapshot
	if snap.SchemaVersion == 0 {
		snap.SchemaVersion = CurrentSchemaVersion
	}

	customerID := h.tenant(r.Context())
	actor := h.actorFn(r.Context())

	o, v, err := h.store.CreateOverlay(
		r.Context(), customerID, proxyIDPtr,
		name, description, actor, req.CommitMessage, "template:"+tpl.Slug,
		&snap,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"overlay": o, "version": v, "template": tpl.Slug})
}

// PreviewVersion replays user-supplied samples through the candidate
// version's effective profile and the currently-active effective
// profile, and returns per-sample verdicts + a diff summary. Lets
// operators see "what would change if I activated this?" against
// real problem cases before flipping the switch.
//
// Request body: { "samples": [{"content": "..."}] }. Max 50 samples.
func (h *Handler) PreviewVersion(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	n, ok := parseVersionParam(w, r)
	if !ok {
		return
	}
	if h.previewer == nil {
		writeError(w, http.StatusNotImplemented, "preview runner not configured")
		return
	}

	var req struct {
		Samples []PreviewSample `json:"samples"`
		ProxyID string          `json:"proxy_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Samples) == 0 {
		writeError(w, http.StatusBadRequest, "samples is required")
		return
	}
	const maxSamples = 50
	if len(req.Samples) > maxSamples {
		writeError(w, http.StatusBadRequest, "too many samples (max 50)")
		return
	}

	v, err := h.store.GetVersion(r.Context(), customerID, id, n)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get version failed")
		slog.Error("overlay: preview get version", "error", err)
		return
	}

	proxyID := uuid.Nil
	if req.ProxyID != "" {
		if pid, perr := uuid.Parse(req.ProxyID); perr == nil {
			proxyID = pid
		}
	}

	result, err := h.previewer.Preview(r.Context(), customerID, proxyID, &v.Snapshot, req.Samples)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "preview failed")
		slog.Error("overlay: preview", "error", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListShadowEvents(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	list, err := h.store.ListShadowEvents(r.Context(), customerID, id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list shadow events failed")
		slog.Error("overlay: list shadow events", "error", err)
		return
	}
	if list == nil {
		list = []ShadowEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": list})
}

// ---- helpers ----

func (h *Handler) writeStateError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrInvalidState):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, fallback)
		slog.Error("overlay: state transition", "error", err)
	}
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func parseVersionParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := chi.URLParam(r, "n")
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "invalid version")
		return 0, false
	}
	return n, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
