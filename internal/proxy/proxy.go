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

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/bastio-ai/bastio/pkg/tenant"
)

// Proxy represents a configured gateway endpoint.
type Proxy struct {
	ID             uuid.UUID          `json:"id"`
	CustomerID     uuid.UUID          `json:"customer_id"`
	Name           string             `json:"name"`
	Slug           string             `json:"slug"`
	ListenPath     string             `json:"listen_path"`
	TargetProvider providers.Provider `json:"target_provider"`
	TargetModel    string             `json:"target_model"`
	Settings       json.RawMessage    `json:"settings"`
	IsActive       bool               `json:"is_active"`
}

// proxyJSON is a JSON-friendly version for API responses (no uuid.UUID scanning issues).
type proxyJSON struct {
	ID             string `json:"id"`
	CustomerID     string `json:"customer_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	ListenPath     string `json:"listen_path"`
	TargetProvider string `json:"target_provider"`
	TargetModel    string `json:"target_model"`
	Settings       string `json:"settings"`
	IsActive       bool   `json:"is_active"`
	CreatedAt      string `json:"created_at"`
}

// Service manages proxies and provider key resolution.
type Service struct {
	db  *pgxpool.Pool
	enc *encryption.Service // optional; nil falls back to legacy plaintext only
}

// NewService creates a new proxy service. enc is optional — pass nil
// for OSS-standalone deployments where provider keys are stored in the
// legacy `{"key": "..."}` plaintext shape. Cloud passes its
// encryption.Service so envelope-encrypted rows from the dashboard's
// "Provider Keys" UI decrypt cleanly.
func NewService(db *pgxpool.Pool, enc *encryption.Service) *Service {
	return &Service{db: db, enc: enc}
}

// DefaultCustomerID is the seeded OSS single-tenant customer, mirrored
// from pkg/tenant for readability in this file. Kept for backward
// compatibility with external callers; the handlers in this file MUST
// resolve the tenant from request context via tenantIDFromCtx so cloud
// (multi-tenant) requests are correctly scoped.
var DefaultCustomerID = tenant.DefaultOSSID

// tenantIDFromCtx reads the request-bound customer from context.
// OSSMiddleware sets DefaultOSSID in single-tenant deployments; cloud's
// auth middleware overrides with the real session-bound customer.
// On a missing context (no middleware in the chain — should never
// happen in production), falls back to DefaultOSSID for safety so
// the OSS standalone test path keeps working.
func tenantIDFromCtx(ctx context.Context) string {
	if id, err := tenant.FromContext(ctx); err == nil {
		return id.String()
	}
	return DefaultCustomerID.String()
}

// GetByID retrieves a proxy by ID (internal use with uuid types).
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*Proxy, error) {
	var p Proxy
	err := s.db.QueryRow(ctx, `
		SELECT id, customer_id, name, slug, listen_path, target_provider, target_model, settings, is_active
		FROM proxies WHERE id = $1
	`, id).Scan(&p.ID, &p.CustomerID, &p.Name, &p.Slug, &p.ListenPath,
		&p.TargetProvider, &p.TargetModel, &p.Settings, &p.IsActive)
	if err != nil {
		return nil, fmt.Errorf("proxy not found: %w", err)
	}
	return &p, nil
}

// ListByCustomer retrieves all proxies for a customer (internal use).
func (s *Service) ListByCustomer(ctx context.Context, customerID uuid.UUID) ([]Proxy, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, customer_id, name, slug, listen_path, target_provider, target_model, settings, is_active
		FROM proxies WHERE customer_id = $1 ORDER BY created_at DESC
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		var p Proxy
		if err := rows.Scan(&p.ID, &p.CustomerID, &p.Name, &p.Slug, &p.ListenPath,
			&p.TargetProvider, &p.TargetModel, &p.Settings, &p.IsActive); err != nil {
			slog.Error("scan proxy row (internal)", "error", err)
			continue
		}
		proxies = append(proxies, p)
	}
	return proxies, nil
}

// Create creates a new proxy.
func (s *Service) Create(ctx context.Context, customerID uuid.UUID, name, slug, listenPath string, provider providers.Provider, model string) (*Proxy, error) {
	p := &Proxy{
		ID:             uuid.New(),
		CustomerID:     customerID,
		Name:           name,
		Slug:           slug,
		ListenPath:     listenPath,
		TargetProvider: provider,
		TargetModel:    model,
		Settings:       json.RawMessage("{}"),
		IsActive:       true,
	}

	_, err := s.db.Exec(ctx, `
		INSERT INTO proxies (id, customer_id, name, slug, listen_path, target_provider, target_model, settings, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, p.ID, p.CustomerID, p.Name, p.Slug, p.ListenPath, p.TargetProvider, p.TargetModel, p.Settings, p.IsActive)
	if err != nil {
		return nil, fmt.Errorf("create proxy: %w", err)
	}

	return p, nil
}

// ResolveProviderKey looks up the decrypted provider API key for a proxy.
//
// Two storage shapes are recognized for back-compat:
//
//	encrypted envelope: {"ciphertext": "...", "nonce": "...", "version": ...}  (current path)
//	legacy plaintext:   {"key": "<plain>"}                                     (older rows / OSS-standalone)
//
// Envelope decryption requires Service.enc to be set (Cloud passes it
// via NewService). The HKDF context is `customerID + provider`,
// matching the dashboard's encrypt path.
func (s *Service) ResolveProviderKey(ctx context.Context, customerID, proxyID uuid.UUID, provider providers.Provider) (string, error) {
	var encryptedKey json.RawMessage
	err := s.db.QueryRow(ctx, `
		SELECT encrypted_key FROM proxy_provider_keys
		WHERE customer_id = $1 AND provider = $2
			AND (proxy_id = $3 OR (proxy_id IS NULL AND is_default = true))
		ORDER BY proxy_id NULLS LAST
		LIMIT 1
	`, customerID, string(provider), proxyID).Scan(&encryptedKey)
	if err != nil {
		return "", fmt.Errorf("no provider key configured for %s: %w", provider, err)
	}

	// Try legacy plaintext shape first.
	var legacy struct {
		Key string `json:"key"`
	}
	if jerr := json.Unmarshal(encryptedKey, &legacy); jerr == nil && legacy.Key != "" {
		return legacy.Key, nil
	}

	// Otherwise expect a real encryption envelope.
	var env encryption.Encrypted
	if jerr := json.Unmarshal(encryptedKey, &env); jerr != nil {
		return "", fmt.Errorf("unmarshal provider key: %w", jerr)
	}
	if env.Ciphertext == "" || env.Nonce == "" {
		return "", fmt.Errorf("provider key for %s/%s has neither plaintext nor ciphertext", customerID, provider)
	}
	if s.enc == nil {
		return "", fmt.Errorf("provider key for %s/%s is envelope-encrypted but proxy.Service has no encryption service wired", customerID, provider)
	}
	plaintext, err := s.enc.Decrypt(&env, customerID.String(), string(provider))
	if err != nil {
		return "", fmt.Errorf("decrypt provider key: %w", err)
	}
	return plaintext, nil
}

// listProxiesJSON queries proxies with ::text casts for clean JSON serialization.
func (s *Service) listProxiesJSON(ctx context.Context) ([]proxyJSON, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, customer_id::text, name, slug, listen_path,
			target_provider, target_model, settings::text, is_active, created_at::text
		FROM proxies
		WHERE customer_id = $1::uuid
		ORDER BY created_at DESC
	`, tenantIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []proxyJSON
	for rows.Next() {
		var p proxyJSON
		if err := rows.Scan(&p.ID, &p.CustomerID, &p.Name, &p.Slug, &p.ListenPath,
			&p.TargetProvider, &p.TargetModel, &p.Settings, &p.IsActive, &p.CreatedAt); err != nil {
			slog.Error("scan proxy row", "error", err)
			continue
		}
		proxies = append(proxies, p)
	}
	return proxies, nil
}

// Routes returns the management API routes for proxies.
// providerKeys is optional — if provided, mounts GET /{id}/provider-keys.
func (s *Service) Routes(providerKeys ...*ProviderKeyHandler) chi.Router {
	r := chi.NewRouter()

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		proxies, err := s.listProxiesJSON(r.Context())
		if err != nil {
			slog.Error("list proxies failed", "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if proxies == nil {
			proxies = []proxyJSON{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxies)
	})

	r.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		var p proxyJSON
		err := s.db.QueryRow(r.Context(), `
			SELECT id::text, customer_id::text, name, slug, listen_path,
				target_provider, target_model, settings::text, is_active, created_at::text
			FROM proxies WHERE id = $1::uuid AND customer_id = $2::uuid
		`, idStr, tenantIDFromCtx(r.Context())).Scan(&p.ID, &p.CustomerID, &p.Name, &p.Slug, &p.ListenPath,
			&p.TargetProvider, &p.TargetModel, &p.Settings, &p.IsActive, &p.CreatedAt)
		if err != nil {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(p)
	})

	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name           string `json:"name"`
			Slug           string `json:"slug"`
			ListenPath     string `json:"listen_path"`
			TargetProvider string `json:"target_provider"`
			TargetModel    string `json:"target_model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.TargetProvider == "" {
			http.Error(w, `{"error":"name and target_provider are required"}`, http.StatusBadRequest)
			return
		}
		if req.Slug == "" {
			req.Slug = req.Name
		}
		if req.ListenPath == "" {
			req.ListenPath = "/" + req.Slug
		}

		id := uuid.New()
		var p proxyJSON
		err := s.db.QueryRow(r.Context(), `
			INSERT INTO proxies (id, customer_id, name, slug, listen_path, target_provider, target_model, settings, is_active)
			VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, '{}'::jsonb, true)
			RETURNING id::text, customer_id::text, name, slug, listen_path, target_provider, target_model, settings::text, is_active, created_at::text
		`, id, tenantIDFromCtx(r.Context()), req.Name, req.Slug, req.ListenPath, req.TargetProvider, req.TargetModel,
		).Scan(&p.ID, &p.CustomerID, &p.Name, &p.Slug, &p.ListenPath,
			&p.TargetProvider, &p.TargetModel, &p.Settings, &p.IsActive, &p.CreatedAt)
		if err != nil {
			slog.Error("create proxy failed", "error", err)
			http.Error(w, fmt.Sprintf(`{"error":"failed to create proxy: %s"}`, err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(p)
	})

	r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		var req struct {
			Name           string `json:"name"`
			TargetProvider string `json:"target_provider"`
			TargetModel    string `json:"target_model"`
			IsActive       *bool  `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		isActive := true
		if req.IsActive != nil {
			isActive = *req.IsActive
		}

		var p proxyJSON
		err := s.db.QueryRow(r.Context(), `
			UPDATE proxies SET name = $3, target_provider = $4, target_model = $5, is_active = $6
			WHERE id = $1::uuid AND customer_id = $2::uuid
			RETURNING id::text, customer_id::text, name, slug, listen_path, target_provider, target_model, settings::text, is_active, created_at::text
		`, idStr, tenantIDFromCtx(r.Context()), req.Name, req.TargetProvider, req.TargetModel, isActive,
		).Scan(&p.ID, &p.CustomerID, &p.Name, &p.Slug, &p.ListenPath,
			&p.TargetProvider, &p.TargetModel, &p.Settings, &p.IsActive, &p.CreatedAt)
		if err != nil {
			slog.Error("update proxy failed", "error", err)
			http.Error(w, `{"error":"failed to update proxy"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(p)
	})

	r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		_, err := s.db.Exec(r.Context(),
			`DELETE FROM proxies WHERE id = $1::uuid AND customer_id = $2::uuid`,
			idStr, tenantIDFromCtx(r.Context()))
		if err != nil {
			slog.Error("delete proxy failed", "error", err)
			http.Error(w, `{"error":"failed to delete proxy"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Mount provider keys sub-route on each proxy
	if len(providerKeys) > 0 && providerKeys[0] != nil {
		r.Get("/{id}/provider-keys", providerKeys[0].ListForProxy)
	}

	return r
}
