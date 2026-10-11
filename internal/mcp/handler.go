package mcp

import (
	"bytes"
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
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	store  Store
	engine *security.Engine
}

func NewHandler(store Store, engine *security.Engine) *Handler { return &Handler{store, engine} }
func (h *Handler) Routes(manage func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	r.Get("/servers", h.list)
	r.Get("/events", h.events)
	r.With(manage).Post("/servers", h.create)
	r.With(manage).Put("/servers/{id}/policy", h.policy)
	r.With(manage).Post("/servers/{id}/rotate-token", h.rotate)
	r.With(manage).Delete("/servers/{id}", h.revoke)
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
	if len(p.AllowedTools) > 100 || len(p.BlockedTools) > 100 {
		return false
	}
	for _, list := range [][]string{p.AllowedTools, p.BlockedTools} {
		for _, name := range list {
			if !toolNameRE.MatchString(name) {
				return false
			}
		}
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
	return p
}
func (h *Handler) storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "MCP server not found")
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
		Name   string `json:"name"`
		Client string `json:"client"`
		Policy Policy `json:"policy"`
	}
	if !decode(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
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
	token, err := newToken()
	if err != nil {
		h.storeError(w, err)
		return
	}
	v, err := h.store.Create(r.Context(), cid, b.Name, b.Client, normalizePolicy(b.Policy), tokenHash(token))
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
	if err := h.store.UpdatePolicy(r.Context(), cid, id, normalizePolicy(b)); err != nil {
		h.storeError(w, err)
		return
	}
	respond(w, 200, struct {
		Policy Policy `json:"policy"`
	}{normalizePolicy(b)})
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

// Runtime is outside dashboard session auth; an enrollment can write only its own server's metadata.
func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request) {
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		fail(w, 401, "MCP enrollment required")
		return
	}
	token := strings.TrimPrefix(authorization, "Bearer ")
	if len(token) != 68 || !strings.HasPrefix(token, "mcp_") {
		fail(w, 401, "MCP enrollment required")
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
		case "allowed", "blocked", "sanitized", "warned":
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
