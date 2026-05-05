package security

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/pkg/tenant"
)

// PlaygroundHandler serves the /v1/playground/runs endpoints that back
// the dashboard's playground history panel. It is deliberately a
// separate surface from /v1/detect: reading runs and writing runs have
// different authorization stories (read is dashboard-group auth; write
// happens as a side-effect of detect) and separate rate-limit
// expectations (history polls vs. inline detection).
type PlaygroundHandler struct {
	db     *pgxpool.Pool
	tenant func(ctx context.Context) uuid.UUID
}

// NewPlaygroundHandler constructs a handler over the shared pool. In
// OSS the tenant resolver defaults to the seeded single-tenant
// customer; cloud swaps it in via SetTenantResolver.
func NewPlaygroundHandler(db *pgxpool.Pool) *PlaygroundHandler {
	return &PlaygroundHandler{
		db:     db,
		// Read the tenant from request context. OSSMiddleware sets
		// DefaultOSSID for single-tenant deployments; cloud's auth
		// middleware overrides with the real session customer.
		// Falls back only if no middleware ran (test paths).
		tenant: func(ctx context.Context) uuid.UUID {
			if id, err := tenant.FromContext(ctx); err == nil {
				return id
			}
			return tenant.DefaultOSSID
		},
	}
}

// SetTenantResolver overrides the tenant lookup. Cloud injects a
// session-aware resolver so each user sees only their own customer's
// history.
func (h *PlaygroundHandler) SetTenantResolver(fn func(context.Context) uuid.UUID) {
	h.tenant = fn
}

// Routes returns the Chi router mounted at /v1/playground.
func (h *PlaygroundHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/runs", h.ListRuns)
	r.Delete("/runs/{id}", h.DeleteRun)
	return r
}

// playgroundRun is the JSON shape returned to the dashboard. Mirrors
// the DB row; `steps` is already stored as JSON so we decode it to the
// typed StepResult for the caller rather than leaking raw JSON.
type playgroundRun struct {
	ID               string       `json:"id"`
	ProfileName      string       `json:"profile_name"`
	ProxyID          *string      `json:"proxy_id,omitempty"`
	Direction        Direction    `json:"direction"`
	Prompt           string       `json:"prompt"`
	SanitizedContent string       `json:"sanitized_content"`
	Action           string       `json:"action"`
	ShouldBlock      bool         `json:"should_block"`
	FiredDetectors   []string     `json:"fired_detectors"`
	Steps            []StepResult `json:"steps"`
	DurationNs       int64        `json:"duration_ns"`
	CreatedAt        string       `json:"created_at"`
}

// ListRuns returns the customer's most-recent playground history, newest
// first. The default cap of 50 matches the dashboard history panel's
// viewport; callers can page via ?limit=N up to a hard ceiling of 200
// to prevent pathological scans.
func (h *PlaygroundHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT id::text, profile_name, proxy_id::text, direction,
			prompt, sanitized_content, action, should_block,
			fired_detectors, steps, duration_ns, created_at::text
		FROM playground_runs
		WHERE customer_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, customerID, limit)
	if err != nil {
		slog.Error("list playground runs failed", "error", err)
		http.Error(w, `{"error":"query playground runs"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	runs := make([]playgroundRun, 0)
	for rows.Next() {
		var (
			run          playgroundRun
			proxyID      *string
			direction    string
			firedRaw     []string
			stepsJSONRaw []byte
		)
		if err := rows.Scan(
			&run.ID, &run.ProfileName, &proxyID, &direction,
			&run.Prompt, &run.SanitizedContent, &run.Action, &run.ShouldBlock,
			&firedRaw, &stepsJSONRaw, &run.DurationNs, &run.CreatedAt,
		); err != nil {
			slog.Error("scan playground run", "error", err)
			continue
		}
		if proxyID != nil && *proxyID != "" {
			run.ProxyID = proxyID
		}
		run.Direction = Direction(direction)
		run.FiredDetectors = firedRaw
		if len(stepsJSONRaw) > 0 {
			if err := json.Unmarshal(stepsJSONRaw, &run.Steps); err != nil {
				// Keep the row — the metadata is still useful even if
				// the trace JSON is corrupt for some reason. Log and move on.
				slog.Warn("unmarshal playground run steps", "id", run.ID, "error", err)
			}
		}
		if run.Steps == nil {
			run.Steps = []StepResult{}
		}
		runs = append(runs, run)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(runs)
}

// DeleteRun removes a single history entry. Scoped by customer_id so
// one tenant cannot reference another's run by guessing the UUID.
func (h *PlaygroundHandler) DeleteRun(w http.ResponseWriter, r *http.Request) {
	customerID := h.tenant(r.Context())
	idStr := chi.URLParam(r, "id")

	tag, err := h.db.Exec(r.Context(), `
		DELETE FROM playground_runs
		WHERE id = $1::uuid AND customer_id = $2
	`, idStr, customerID)
	if err != nil {
		slog.Error("delete playground run", "error", err)
		http.Error(w, `{"error":"delete failed"}`, http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		// Could be either "not found" or "other tenant's row" — we
		// collapse both to 404 on purpose so we don't leak which.
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

