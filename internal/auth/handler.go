package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/pkg/tenant"
)

// DefaultCustomerID is the seeded OSS single-tenant customer, mirrored
// from pkg/tenant. Kept for backward compatibility; the handlers in
// this file MUST resolve the tenant from request context via
// tenantIDFromCtx so cloud (multi-tenant) requests are correctly scoped.
var DefaultCustomerID = tenant.DefaultOSSID.String()

// tenantIDFromCtx reads the request-bound customer from context.
// OSSMiddleware sets DefaultOSSID in single-tenant deployments; cloud's
// auth middleware overrides with the real session-bound customer.
func tenantIDFromCtx(ctx context.Context) string {
	if id, err := tenant.FromContext(ctx); err == nil {
		return id.String()
	}
	return DefaultCustomerID
}

// APIKeyHandler manages API key CRUD endpoints.
type APIKeyHandler struct {
	db *pgxpool.Pool
}

// NewAPIKeyHandler creates a new API key handler.
func NewAPIKeyHandler(db *pgxpool.Pool) *APIKeyHandler {
	return &APIKeyHandler{db: db}
}

// Routes returns the Chi router for API key endpoints.
func (h *APIKeyHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Delete("/{id}", h.Revoke)
	return r
}

// List returns all API keys for the default customer.
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT id::text, name, key_prefix, scopes,
			rate_limit_rpm, is_active,
			last_used_at::text, created_at::text
		FROM gateway_api_keys
		WHERE customer_id = $1::uuid
		ORDER BY created_at DESC
	`, tenantIDFromCtx(r.Context()))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"query api keys: %s"}`, err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var keys []map[string]any
	for rows.Next() {
		var (
			id, name, keyPrefix string
			scopes              []string
			rateLimitRPM        *int32
			isActive            bool
			lastUsedAt          *string
			createdAt           string
		)
		if err := rows.Scan(&id, &name, &keyPrefix, &scopes, &rateLimitRPM, &isActive, &lastUsedAt, &createdAt); err != nil {
			slog.Error("scan api key row", "error", err)
			continue
		}
		keys = append(keys, map[string]any{
			"id":             id,
			"name":           name,
			"key_prefix":     keyPrefix,
			"scopes":         scopes,
			"rate_limit_rpm": rateLimitRPM,
			"is_active":      isActive,
			"last_used_at":   lastUsedAt,
			"created_at":     createdAt,
		})
	}

	if keys == nil {
		keys = []map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// Create generates a new API key.
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		RateLimitRPM *int   `json:"rate_limit_rpm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = "Unnamed Key"
	}

	// Generate the key
	plainKey := GenerateAPIKey()
	keyHash := HashAPIKey(plainKey)
	keyPrefix := plainKey[:12]

	id := uuid.New()
	var createdAt string
	err := h.db.QueryRow(r.Context(), `
		INSERT INTO gateway_api_keys (id, customer_id, name, key_hash, key_prefix, scopes, rate_limit_rpm)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7)
		RETURNING created_at::text
	`, id, tenantIDFromCtx(r.Context()), req.Name, keyHash, keyPrefix, []string{}, req.RateLimitRPM).Scan(&createdAt)
	if err != nil {
		slog.Error("create api key failed", "error", err)
		http.Error(w, `{"error":"failed to create api key"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":         id.String(),
		"name":       req.Name,
		"key":        plainKey,
		"key_prefix": keyPrefix,
		"is_active":  true,
		"created_at": createdAt,
	})
}

// Revoke soft-deletes an API key.
func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	_, err = h.db.Exec(r.Context(),
		`UPDATE gateway_api_keys SET is_active = false WHERE id = $1 AND customer_id = $2::uuid`,
		id, tenantIDFromCtx(r.Context()))
	if err != nil {
		slog.Error("revoke api key failed", "error", err)
		http.Error(w, `{"error":"failed to revoke"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
