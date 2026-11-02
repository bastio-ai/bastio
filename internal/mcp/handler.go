package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/bastio-ai/bastio/pkg/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ApprovalNotifier func(ctx context.Context, customerID uuid.UUID, approval Approval)

type Handler struct {
	store        Store
	engine       *security.Engine
	cipher       *encryption.Service
	httpClient   *http.Client
	notifier     ApprovalNotifier
	actorFn      func(*http.Request) string
	waitMu       sync.Mutex
	waiters      map[uuid.UUID][]chan Approval
	approvalWait time.Duration
}

func NewHandler(store Store, engine *security.Engine) *Handler {
	return &Handler{
		store:        store,
		engine:       engine,
		waiters:      make(map[uuid.UUID][]chan Approval),
		approvalWait: 60 * time.Second,
	}
}

func (h *Handler) SetEncryption(cipher *encryption.Service)         { h.cipher = cipher }
func (h *Handler) SetHTTPClient(client *http.Client)                { h.httpClient = client }
func (h *Handler) SetApprovalNotifier(fn ApprovalNotifier)          { h.notifier = fn }
func (h *Handler) SetActorResolver(fn func(*http.Request) string)   { h.actorFn = fn }
func (h *Handler) SetApprovalWaitTimeout(timeout time.Duration)     { h.approvalWait = timeout }

func (h *Handler) Routes(manage func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	r.Get("/servers", h.list)
	r.Get("/events", h.events)
	r.Get("/approvals", h.approvals)
	r.With(manage).Post("/servers", h.create)
	r.With(manage).Put("/servers/{id}/policy", h.policy)
	r.With(manage).Put("/servers/{id}/connection", h.connection)
	r.With(manage).Get("/servers/{id}/tools", h.discoverTools)
	r.With(manage).Post("/servers/{id}/rotate-token", h.rotate)
	r.With(manage).Delete("/servers/{id}", h.revoke)
	r.With(manage).Post("/approvals/{id}/resolve", h.resolveApproval)
	r.With(manage).Post("/inspect", h.inspect)
	return r
}

func respond[T any](w http.ResponseWriter, status int, body T) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, struct {
		Error string `json:"error"`
	}{message})
}

func customer(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	cid, err := tenant.FromContext(r.Context())
	if err != nil || cid == uuid.Nil {
		fail(w, 401, "workspace identity required")
		return uuid.Nil, false
	}
	return cid, true
}

func decode[T any](w http.ResponseWriter, r *http.Request, v *T) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, 400, "invalid request body")
		return false
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		fail(w, 400, "only one JSON value is allowed")
		return false
	}
	return true
}

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "mcp_" + hex.EncodeToString(raw), nil
}

var toolNameRE = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,128}$`)

func validPolicy(p Policy) bool {
	if len(p.AllowedTools) > 100 || len(p.BlockedTools) > 100 || len(p.ApprovalRequiredTools) > 100 {
		return false
	}
	for _, list := range [][]string{p.AllowedTools, p.BlockedTools, p.ApprovalRequiredTools} {
		for _, name := range list {
			if !toolNameRE.MatchString(name) {
				return false
			}
		}
	}
	if p.WebhookURL != "" {
		if ValidateRemoteURL(p.WebhookURL) != nil {
			return false
		}
	}
	switch p.WebhookFormat {
	case "", "slack", "teams", "raw_json":
	default:
		return false
	}
	return true
}

func normalizePolicy(p Policy) Policy {
	if p.AllowedTools == nil {
		p.AllowedTools = []string{}
	}
	if p.BlockedTools == nil {
		p.BlockedTools = []string{}
	}
	if p.ApprovalRequiredTools == nil {
		p.ApprovalRequiredTools = []string{}
	}
	p.WebhookURL = strings.TrimSpace(p.WebhookURL)
	if p.WebhookURL == "" {
		p.WebhookFormat = ""
	} else if p.WebhookFormat == "" {
		p.WebhookFormat = "slack"
	}
	return p
}

func (h *Handler) sealCredential(cid, serverID uuid.UUID, raw string) ([]byte, error) {
	if h.cipher == nil {
		return nil, errors.New("credential encryption is not configured")
	}
	sealed, err := h.cipher.Encrypt(raw, cid.String(), "mcp-server:"+serverID.String())
	if err != nil {
		return nil, err
	}
	return json.Marshal(sealed)
}

func (h *Handler) openCredential(cid uuid.UUID, srv Server) (string, error) {
	if srv.AuthType != "bearer" || !srv.HasCredential || len(srv.credential) == 0 {
		return "", nil
	}
	if h.cipher == nil {
		return "", errors.New("credential encryption is not configured")
	}
	var sealed encryption.Encrypted
	if err := json.Unmarshal(srv.credential, &sealed); err != nil {
		return "", errors.New("stored MCP credential is invalid")
	}
	return h.cipher.Decrypt(&sealed, cid.String(), "mcp-server:"+srv.ID.String())
}

func (h *Handler) storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "MCP resource not found")
	} else {
		slog.Error("MCP store failed", "error", err)
		fail(w, 500, "MCP operation failed")
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	servers, err := h.store.List(r.Context(), cid)
	if err != nil {
		h.storeError(w, err)
		return
	}
	respond(w, 200, struct {
		Servers []Server `json:"servers"`
	}{servers})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	var b struct {
		Name        string  `json:"name"`
		Client      string  `json:"client"`
		Mode        string  `json:"mode"`
		UpstreamURL string  `json:"upstream_url"`
		AuthType    string  `json:"auth_type"`
		Credential  *string `json:"credential"`
		Policy      Policy  `json:"policy"`
	}
	if !decode(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	b.UpstreamURL = strings.TrimSpace(b.UpstreamURL)
	if len(b.Name) < 1 || len(b.Name) > 100 || !validPolicy(b.Policy) {
		fail(w, 400, "provide a server name and valid tool names")
		return
	}
	switch b.Client {
	case "claude", "cursor", "cline", "terminal", "other":
	default:
		fail(w, 400, "unsupported client")
		return
	}
	if b.Mode == "" {
		b.Mode = "stdio"
	}
	switch b.Mode {
	case "stdio":
		b.UpstreamURL = ""
		b.AuthType = "none"
	case "remote":
		if ValidateRemoteURL(b.UpstreamURL) != nil {
			fail(w, 400, "provide a public HTTPS upstream MCP URL")
			return
		}
		if b.AuthType == "" {
			if b.Credential != nil && *b.Credential != "" {
				b.AuthType = "bearer"
			} else {
				b.AuthType = "none"
			}
		}
		if b.AuthType != "none" && b.AuthType != "bearer" {
			fail(w, 400, "unsupported authentication method")
			return
		}
	default:
		fail(w, 400, "unsupported MCP mode")
		return
	}
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	var encryptedCred []byte
	if b.Mode == "remote" && b.AuthType == "bearer" && b.Credential != nil && *b.Credential != "" {
		if len(*b.Credential) > 8192 || strings.ContainsAny(*b.Credential, "\r\n") {
			fail(w, 400, "invalid upstream credential")
			return
		}
		encryptedCred, err = h.sealCredential(cid, id, *b.Credential)
		if err != nil {
			fail(w, 503, "credential encryption is not configured")
			return
		}
	}
	token, err := newToken()
	if err != nil {
		h.storeError(w, err)
		return
	}
	srv := Server{
		ID:          id,
		CustomerID:  cid,
		Name:        b.Name,
		Client:      b.Client,
		Mode:        b.Mode,
		UpstreamURL: b.UpstreamURL,
		AuthType:    b.AuthType,
		Policy:      normalizePolicy(b.Policy),
		credential:  encryptedCred,
	}
	v, err := h.store.Create(r.Context(), cid, srv, tokenHash(token))
	if err != nil {
		h.storeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 201, struct {
		Server Server `json:"server"`
		Token  string `json:"enrollment_token"`
	}{v, token})
}

func serverID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		fail(w, 400, "invalid server ID")
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) policy(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	id, ok := serverID(w, r)
	if !ok {
		return
	}
	var b Policy
	if !decode(w, r, &b) {
		return
	}
	if !validPolicy(b) {
		fail(w, 400, "invalid tool policy")
		return
	}
	normalized := normalizePolicy(b)
	if err := h.store.UpdatePolicy(r.Context(), cid, id, normalized); err != nil {
		h.storeError(w, err)
		return
	}
	respond(w, 200, struct {
		Policy Policy `json:"policy"`
	}{normalized})
}

func (h *Handler) connection(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	id, ok := serverID(w, r)
	if !ok {
		return
	}
	var b struct {
		Mode        string  `json:"mode"`
		UpstreamURL string  `json:"upstream_url"`
		AuthType    string  `json:"auth_type"`
		Credential  *string `json:"credential"`
	}
	if !decode(w, r, &b) {
		return
	}
	previous, err := h.store.Get(r.Context(), cid, id)
	if err != nil || previous.RevokedAt != nil {
		fail(w, 404, "MCP server not found")
		return
	}
	b.UpstreamURL = strings.TrimSpace(b.UpstreamURL)
	if b.Mode == "" {
		b.Mode = previous.Mode
	}
	var cred []byte
	replaceCred := false
	switch b.Mode {
	case "stdio":
		b.UpstreamURL = ""
		b.AuthType = "none"
		replaceCred = true
	case "remote":
		if ValidateRemoteURL(b.UpstreamURL) != nil {
			fail(w, 400, "provide a public HTTPS upstream MCP URL")
			return
		}
		if b.AuthType == "" {
			if b.Credential != nil && *b.Credential != "" {
				b.AuthType = "bearer"
			} else if previous.AuthType != "" {
				b.AuthType = previous.AuthType
			} else {
				b.AuthType = "none"
			}
		}
		if b.AuthType != "none" && b.AuthType != "bearer" {
			fail(w, 400, "unsupported authentication method")
			return
		}
		if b.AuthType == "none" {
			replaceCred = true
			cred = nil
		} else if previous.UpstreamURL != b.UpstreamURL && b.Credential == nil {
			// Changing the upstream host without re-supplying a credential clears the vaulted secret.
			replaceCred = true
			cred = nil
		} else if b.Credential != nil {
			replaceCred = true
			if *b.Credential != "" {
				if len(*b.Credential) > 8192 || strings.ContainsAny(*b.Credential, "\r\n") {
					fail(w, 400, "invalid upstream credential")
					return
				}
				cred, err = h.sealCredential(cid, id, *b.Credential)
				if err != nil {
					fail(w, 503, "credential encryption is not configured")
					return
				}
			}
		}
	default:
		fail(w, 400, "unsupported MCP mode")
		return
	}
	updated, err := h.store.UpdateConnection(r.Context(), cid, id, b.Mode, b.UpstreamURL, b.AuthType, cred, replaceCred)
	if err != nil {
		h.storeError(w, err)
		return
	}
	respond(w, 200, struct {
		Server Server `json:"server"`
	}{updated})
}

func (h *Handler) discoverTools(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	id, ok := serverID(w, r)
	if !ok {
		return
	}
	srv, err := h.store.Get(r.Context(), cid, id)
	if err != nil || srv.RevokedAt != nil {
		fail(w, 404, "MCP server not found")
		return
	}
	if srv.Mode != "remote" || srv.UpstreamURL == "" {
		fail(w, 400, "tool discovery requires a remote HTTPS MCP server")
		return
	}
	bearer, err := h.openCredential(cid, srv)
	if err != nil {
		fail(w, 503, err.Error())
		return
	}
	client, err := NewRemoteClient(srv.UpstreamURL, bearer, h.httpClient)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	defer closeRemote(client)
	tools, err := client.ListTools(r.Context())
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	var events []Event
	for i := range tools {
		if tools[i].Description == "" || h.engine == nil {
			continue
		}
		scanRes := h.engine.Scan(r.Context(), &security.ScanRequest{Content: tools[i].Description})
		if scanRes != nil && (scanRes.ShouldBlock || len(scanRes.Findings) > 0) {
			tools[i].Description = "[BLOCKED BY BASTIO: Malicious tool description detected]"
			events = append(events, Event{
				ID:         uuid.New(),
				ServerID:   srv.ID,
				ToolName:   tools[i].Name,
				Direction:  "tool_description",
				Action:     "sanitized",
				Categories: categoriesFromScan(scanRes),
				DurationMS: float64(scanRes.ScanDuration.Microseconds()) / 1000,
				OccurredAt: time.Now().UTC(),
			})
		}
	}
	if len(events) > 0 {
		_ = h.store.RecordServerEvents(r.Context(), cid, srv.ID, events)
	}
	respond(w, 200, struct {
		Tools []RemoteTool `json:"tools"`
	}{tools})
}

func (h *Handler) rotate(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	id, ok := serverID(w, r)
	if !ok {
		return
	}
	token, err := newToken()
	if err != nil {
		h.storeError(w, err)
		return
	}
	if err = h.store.Rotate(r.Context(), cid, id, tokenHash(token)); err != nil {
		h.storeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 200, struct {
		Token string `json:"enrollment_token"`
	}{token})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	id, ok := serverID(w, r)
	if !ok {
		return
	}
	if err := h.store.Revoke(r.Context(), cid, id); err != nil {
		h.storeError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	id := uuid.Nil
	var err error
	if value := r.URL.Query().Get("server_id"); value != "" {
		id, err = uuid.Parse(value)
		if err != nil {
			fail(w, 400, "invalid server ID")
			return
		}
	}
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 200 {
			fail(w, 400, "limit must be 1–200")
			return
		}
	}
	events, err := h.store.Events(r.Context(), cid, id, limit)
	if err != nil {
		h.storeError(w, err)
		return
	}
	respond(w, 200, struct {
		Events []Event `json:"events"`
	}{events})
}

func (h *Handler) approvals(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	serverFilter := uuid.Nil
	var err error
	if value := r.URL.Query().Get("server_id"); value != "" {
		serverFilter, err = uuid.Parse(value)
		if err != nil {
			fail(w, 400, "invalid server ID")
			return
		}
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "", "pending", "approved", "denied", "expired":
	default:
		fail(w, 400, "invalid approval status filter")
		return
	}
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 200 {
			fail(w, 400, "limit must be 1–200")
			return
		}
	}
	approvals, err := h.store.ListApprovals(r.Context(), cid, serverFilter, status, limit)
	if err != nil {
		h.storeError(w, err)
		return
	}
	respond(w, 200, struct {
		Approvals []Approval `json:"approvals"`
	}{approvals})
}

func (h *Handler) resolveApproval(w http.ResponseWriter, r *http.Request) {
	cid, ok := customer(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		fail(w, 400, "invalid approval ID")
		return
	}
	var b struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Action != "approved" && b.Action != "denied" {
		fail(w, 400, "action must be approved or denied")
		return
	}
	b.Reason = strings.TrimSpace(b.Reason)
	if len(b.Reason) > 500 {
		fail(w, 400, "reason must be 500 characters or fewer")
		return
	}
	actor := "admin"
	if h.actorFn != nil {
		if resolved := strings.TrimSpace(h.actorFn(r)); resolved != "" {
			actor = resolved
		}
	}
	approval, err := h.store.ResolveApproval(r.Context(), cid, id, b.Action, actor, b.Reason)
	if errors.Is(err, ErrApprovalExpired) {
		fail(w, 409, "approval request has already expired or been resolved")
		return
	}
	if err != nil {
		h.storeError(w, err)
		return
	}
	_ = h.store.RecordServerEvents(r.Context(), cid, approval.ServerID, []Event{{
		ID:         uuid.New(),
		ServerID:   approval.ServerID,
		ToolName:   approval.ToolName,
		Direction:  "request",
		Action:     approval.Status,
		Categories: approval.Categories,
		DurationMS: 0,
		OccurredAt: time.Now().UTC(),
	}})
	h.notifyWaiters(approval)
	respond(w, 200, struct {
		Approval Approval `json:"approval"`
	}{approval})
}

func (h *Handler) inspect(w http.ResponseWriter, r *http.Request) {
	if _, ok := customer(w, r); !ok {
		return
	}
	var b InspectRequest
	if !decode(w, r, &b) {
		return
	}
	b.Arguments = bytes.TrimSpace(b.Arguments)
	if !toolNameRE.MatchString(b.ToolName) || !validPolicy(b.Policy) || !json.Valid(b.Arguments) || len(b.Arguments) == 0 || b.Arguments[0] != '{' {
		fail(w, 400, "provide a tool name and JSON object arguments")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 200, Inspect(r.Context(), h.engine, b))
}

func bearerEnrollmentToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		fail(w, 401, "MCP enrollment required")
		return "", false
	}
	token := strings.TrimPrefix(authorization, "Bearer ")
	if len(token) != 68 || !strings.HasPrefix(token, "mcp_") {
		fail(w, 401, "MCP enrollment required")
		return "", false
	}
	return token, true
}

// Runtime is outside dashboard session auth; an enrollment can write only its own server's metadata.
func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerEnrollmentToken(w, r)
	if !ok {
		return
	}
	var b RuntimeBatch
	if !decode(w, r, &b) {
		return
	}
	if len(b.Events) > 100 || b.DroppedEvents < 0 || b.DroppedEvents > 1000000 {
		fail(w, 400, "invalid telemetry batch")
		return
	}
	now := time.Now()
	for i := range b.Events {
		e := &b.Events[i]
		if e.Categories == nil {
			e.Categories = []string{}
		}
		if e.ID == uuid.Nil || !toolNameRE.MatchString(e.ToolName) || math.IsNaN(e.DurationMS) || math.IsInf(e.DurationMS, 0) || e.DurationMS < 0 || e.DurationMS > 600000 || e.OccurredAt.Before(now.Add(-24*time.Hour)) || e.OccurredAt.After(now.Add(time.Minute)) || len(e.Categories) > 16 {
			fail(w, 400, "invalid MCP event")
			return
		}
		switch e.Direction {
		case "request", "response", "tool_description":
		default:
			fail(w, 400, "invalid event direction")
			return
		}
		switch e.Action {
		case "allowed", "blocked", "sanitized", "warned", "approval_required", "approved", "denied":
		default:
			fail(w, 400, "invalid event action")
			return
		}
		for _, cat := range e.Categories {
			if !toolNameRE.MatchString(cat) {
				fail(w, 400, "invalid event category")
				return
			}
		}
	}
	v, err := h.store.Ingest(r.Context(), tokenHash(token), b)
	if errors.Is(err, ErrEnrollment) {
		fail(w, 401, "MCP enrollment is invalid or revoked")
		return
	}
	if errors.Is(err, ErrRateLimit) {
		w.Header().Set("Retry-After", "30")
		fail(w, 429, "MCP reporting rate exceeded")
		return
	}
	if err != nil {
		h.storeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 200, struct {
		ServerID uuid.UUID `json:"server_id"`
		Policy   Policy    `json:"policy"`
	}{v.ID, normalizePolicy(v.Policy)})
}
