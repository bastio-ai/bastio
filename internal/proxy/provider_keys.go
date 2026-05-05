package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/pkg/encryption"
)

// ProviderKey represents a stored LLM provider API key.
type ProviderKey struct {
	ID        string  `json:"id"`
	ProxyID   *string `json:"proxy_id"`
	Provider  string  `json:"provider"`
	KeyMasked string  `json:"key_masked"`
	IsDefault bool    `json:"is_default"`
	CreatedAt string  `json:"created_at"`
}

// ProviderKeyHandler manages provider key CRUD.
type ProviderKeyHandler struct {
	db  *pgxpool.Pool
	enc *encryption.Service // optional — when wired, Create encrypts before storing
}

// NewProviderKeyHandler creates a new provider key handler.
func NewProviderKeyHandler(db *pgxpool.Pool) *ProviderKeyHandler {
	return &ProviderKeyHandler{db: db}
}

// SetEncryptionService wires the encryption service used when storing
// new provider keys. Without it, Create falls back to a legacy
// plaintext-in-JSON envelope (back-compat for OSS standalone where
// encryption isn't configured). When wired, the cloud's KeyResolver
// can decrypt and pass the plaintext key to provider clients.
func (h *ProviderKeyHandler) SetEncryptionService(s *encryption.Service) {
	h.enc = s
}

// Routes returns the Chi router for top-level provider key endpoints.
func (h *ProviderKeyHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Delete("/{id}", h.Delete)
	return r
}

// queryKeys runs a query and scans provider key rows with masking.
// customerID is needed for envelope decryption (HKDF context); the
// caller already has it as the $1 arg of the WHERE clause, so it's
// passed in alongside the query rather than re-derived per row.
func (h *ProviderKeyHandler) queryKeys(ctx context.Context, customerID, query string, args ...any) ([]ProviderKey, error) {
	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []ProviderKey
	for rows.Next() {
		var (
			id, provider, createdAt string
			proxyID                 *string
			encryptedKeyStr         string
			isDefault               bool
		)
		if err := rows.Scan(&id, &proxyID, &provider, &encryptedKeyStr, &isDefault, &createdAt); err != nil {
			slog.Error("scan provider key row", "error", err)
			continue
		}

		// Decode handles both legacy plaintext-in-JSON and the
		// encrypted envelope. Returns "" if neither shape parses,
		// which still gives a sensible "****" masked display below.
		k := h.decodeKeyFromStorage(encryptedKeyStr, customerID, provider)
		masked := "****"
		if len(k) > 12 {
			masked = k[:8] + "..." + k[len(k)-4:]
		} else if len(k) > 4 {
			masked = k[:4] + "..."
		}

		keys = append(keys, ProviderKey{
			ID:        id,
			ProxyID:   proxyID,
			Provider:  provider,
			KeyMasked: masked,
			IsDefault: isDefault,
			CreatedAt: createdAt,
		})
	}

	if keys == nil {
		keys = []ProviderKey{}
	}
	return keys, nil
}

// ListForProxy returns provider keys available for a specific proxy.
func (h *ProviderKeyHandler) ListForProxy(w http.ResponseWriter, r *http.Request) {
	proxyID := chi.URLParam(r, "id")
	custID := tenantIDFromCtx(r.Context())
	keys, err := h.queryKeys(r.Context(), custID, `
		SELECT pk.id::text, pk.proxy_id::text, pk.provider, pk.encrypted_key::text, pk.is_default, pk.created_at::text
		FROM proxy_provider_keys pk
		WHERE pk.customer_id = $1::uuid
			AND (pk.proxy_id = $2::uuid
				OR (pk.proxy_id IS NULL AND pk.is_default = true
					AND pk.provider = (SELECT target_provider FROM proxies WHERE id = $2::uuid)))
		ORDER BY pk.proxy_id NULLS LAST, pk.created_at DESC
	`, custID, proxyID)
	if err != nil {
		slog.Error("list proxy provider keys failed", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// List returns all provider keys for the request's tenant.
func (h *ProviderKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	custID := tenantIDFromCtx(r.Context())
	keys, err := h.queryKeys(r.Context(), custID, `
		SELECT id::text, proxy_id::text, provider, encrypted_key::text, is_default, created_at::text
		FROM proxy_provider_keys
		WHERE customer_id = $1::uuid
		ORDER BY provider, created_at DESC
	`, custID)
	if err != nil {
		slog.Error("list provider keys failed", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// Create stores a new provider API key. If proxy_id is provided, links to that proxy.
// Also stores as default (proxy_id = NULL) so it works for any proxy of that provider.
func (h *ProviderKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string  `json:"provider"`
		Key      string  `json:"key"`
		ProxyID  *string `json:"proxy_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Provider == "" || req.Key == "" {
		http.Error(w, `{"error":"provider and key are required"}`, http.StatusBadRequest)
		return
	}

	custID := tenantIDFromCtx(r.Context())

	// Encrypt when wired (cloud); fall back to plaintext-in-JSON for
	// OSS-standalone where the encryption service isn't configured.
	// Same JSONB column either way — the read path detects which
	// shape it's looking at by presence of the ciphertext field.
	encryptedKey, encErr := h.encodeKeyForStorage(req.Key, custID, req.Provider)
	if encErr != nil {
		slog.Error("encode provider key failed", "error", encErr)
		http.Error(w, `{"error":"failed to encode key"}`, http.StatusInternalServerError)
		return
	}

	// Always store as default (proxy_id = NULL) so any proxy of this provider can use it
	id := uuid.New()
	var createdAt string
	err := h.db.QueryRow(r.Context(), `
		INSERT INTO proxy_provider_keys (id, customer_id, proxy_id, provider, encrypted_key, is_default)
		VALUES ($1, $2::uuid, NULL, $3, $4::jsonb, true)
		ON CONFLICT DO NOTHING
		RETURNING created_at::text
	`, id, custID, req.Provider, encryptedKey).Scan(&createdAt)
	if err != nil {
		// Might be a conflict, try without ON CONFLICT
		err = h.db.QueryRow(r.Context(), `
			INSERT INTO proxy_provider_keys (id, customer_id, proxy_id, provider, encrypted_key, is_default)
			VALUES ($1, $2::uuid, NULL, $3, $4::jsonb, true)
			RETURNING created_at::text
		`, id, custID, req.Provider, encryptedKey).Scan(&createdAt)
		if err != nil {
			slog.Error("create provider key failed", "error", err)
			http.Error(w, fmt.Sprintf(`{"error":"failed to create: %s"}`, err), http.StatusInternalServerError)
			return
		}
	}

	masked := "****"
	if len(req.Key) > 12 {
		masked = req.Key[:8] + "..." + req.Key[len(req.Key)-4:]
	} else if len(req.Key) > 4 {
		masked = req.Key[:4] + "..."
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ProviderKey{
		ID:        id.String(),
		Provider:  req.Provider,
		KeyMasked: masked,
		IsDefault: true,
		CreatedAt: createdAt,
	})
}

// encodeKeyForStorage produces the JSON envelope written into the
// proxy_provider_keys.encrypted_key column.
//
//	encryption.Service wired (cloud)  → real {ciphertext, nonce} envelope
//	encryption.Service not wired (OSS)→ legacy {"key":"<plain>"} JSON
//
// The KeyResolver (cloud) handles both shapes on read for back-compat
// with rows already in the database when this code shipped.
func (h *ProviderKeyHandler) encodeKeyForStorage(plain, customerID, provider string) (string, error) {
	if h.enc == nil {
		return fmt.Sprintf(`{"key":%q}`, plain), nil
	}
	env, err := h.enc.Encrypt(plain, customerID, provider)
	if err != nil {
		return "", fmt.Errorf("encrypt provider key: %w", err)
	}
	out, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("marshal envelope: %w", err)
	}
	return string(out), nil
}

// decodeKeyFromStorage is the inverse of encodeKeyForStorage. Used by
// queryKeys to mask the key for display. Falls back to legacy
// plaintext-in-JSON when the row predates encryption.
func (h *ProviderKeyHandler) decodeKeyFromStorage(envelopeJSON, customerID, provider string) string {
	// Legacy plaintext shape — no ciphertext field, just {"key":"..."}.
	var legacy struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(envelopeJSON), &legacy); err == nil && legacy.Key != "" {
		return legacy.Key
	}
	// Encrypted envelope shape.
	if h.enc == nil {
		return ""
	}
	var env encryption.Encrypted
	if err := json.Unmarshal([]byte(envelopeJSON), &env); err != nil {
		return ""
	}
	if env.Ciphertext == "" {
		return ""
	}
	plain, err := h.enc.Decrypt(&env, customerID, provider)
	if err != nil {
		return ""
	}
	return plain
}

// Delete removes a provider key.
func (h *ProviderKeyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := h.db.Exec(r.Context(),
		`DELETE FROM proxy_provider_keys WHERE id = $1::uuid AND customer_id = $2::uuid`,
		id, tenantIDFromCtx(r.Context()))
	if err != nil {
		slog.Error("delete provider key failed", "error", err)
		http.Error(w, `{"error":"failed to delete"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
