package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/pkg/cache"
)

// APIKeyInfo contains the resolved identity for an authenticated request.
type APIKeyInfo struct {
	ID                       uuid.UUID  `json:"id"`
	CustomerID               uuid.UUID  `json:"customer_id"`
	Name                     string     `json:"name"`
	KeyPrefix                string     `json:"key_prefix"`
	Scopes                   []string   `json:"scopes"`
	RateLimitRPM             *int       `json:"rate_limit_rpm"`
	Environment              string     `json:"environment"`
	AllowEnvironmentOverride bool       `json:"allow_environment_override"`
	ExpiresAt                *time.Time `json:"expires_at"`
}

type contextKey string

const apiKeyContextKey contextKey = "api_key_info"
const effectiveEnvironmentContextKey contextKey = "effective_environment"

// FromContext retrieves APIKeyInfo from the request context.
func FromContext(ctx context.Context) (*APIKeyInfo, bool) {
	info, ok := ctx.Value(apiKeyContextKey).(*APIKeyInfo)
	return info, ok
}

// WithInfo attaches APIKeyInfo to ctx. Exposed primarily so tests can set
// up a context without running the full authentication middleware.
func WithInfo(ctx context.Context, info *APIKeyInfo) context.Context {
	return context.WithValue(ctx, apiKeyContextKey, info)
}

// EnvironmentFromRequest resolves the deployment boundary for an authenticated
// request. Credentials are authoritative by default. A shared-ingress key may
// opt into X-Bastio-Environment, but only for a registered environment owned by
// the same customer.
func EnvironmentFromRequest(ctx context.Context, db *pgxpool.Pool, info *APIKeyInfo, requested string) (string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" || requested == info.Environment {
		return info.Environment, nil
	}
	if !info.AllowEnvironmentOverride {
		return "", fmt.Errorf("environment override is not allowed for this API key")
	}
	var exists bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM environments WHERE customer_id = $1 AND name = $2
		)
	`, info.CustomerID, requested).Scan(&exists)
	if err != nil {
		return "", fmt.Errorf("validate environment override: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("environment %q is not registered in this workspace", requested)
	}
	return requested, nil
}

// WithEnvironment attaches the already-validated effective environment.
func WithEnvironment(ctx context.Context, environment string) context.Context {
	return context.WithValue(ctx, effectiveEnvironmentContextKey, environment)
}

// Environment returns the validated environment, falling back to the key's
// assignment for tests and internal callers that don't run gateway middleware.
func Environment(ctx context.Context, info *APIKeyInfo) string {
	if environment, ok := ctx.Value(effectiveEnvironmentContextKey).(string); ok && environment != "" {
		return environment
	}
	if info != nil {
		return info.Environment
	}
	return ""
}

// Service handles API key authentication.
type Service struct {
	db    *pgxpool.Pool
	cache *cache.Cache
}

// NewService creates a new auth service.
func NewService(db *pgxpool.Pool, cache *cache.Cache) *Service {
	return &Service{db: db, cache: cache}
}

// Middleware returns a Chi middleware that authenticates requests via API key.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := extractAPIKey(r)
		if apiKey == "" {
			http.Error(w, `{"error":"missing api key"}`, http.StatusUnauthorized)
			return
		}

		info, err := s.Authenticate(r.Context(), apiKey)
		if err != nil {
			slog.Warn("auth failed", "error", err, "prefix", safePrefix(apiKey))
			http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
			return
		}
		environment, err := EnvironmentFromRequest(r.Context(), s.db, info, r.Header.Get("X-Bastio-Environment"))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"error":{"type":"invalid_environment","message":%q}}`, err.Error())
			return
		}
		// Downstream handlers and recorders consume the canonical value from
		// the header today. Overwrite any client value only after validation.
		r.Header.Set("X-Bastio-Environment", environment)

		ctx := context.WithValue(r.Context(), apiKeyContextKey, info)
		ctx = WithEnvironment(ctx, environment)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Authenticate validates an API key and returns the associated identity.
// Uses a two-tier lookup: Redis cache first, then PostgreSQL.
func (s *Service) Authenticate(ctx context.Context, apiKey string) (*APIKeyInfo, error) {
	keyHash := HashAPIKey(apiKey)
	cacheKey := "apikey:" + keyHash

	// Tier 1: cache lookup
	var info APIKeyInfo
	found, err := s.cache.Get(ctx, cacheKey, &info)
	if err != nil {
		slog.Warn("cache lookup failed, falling back to db", "error", err)
	}
	if found {
		if info.ExpiresAt != nil && info.ExpiresAt.Before(time.Now()) {
			return nil, fmt.Errorf("api key expired")
		}
		return &info, nil
	}

	// Tier 2: database lookup
	row := s.db.QueryRow(ctx, `
		SELECT k.id, k.customer_id, k.name, k.key_prefix, k.scopes, k.rate_limit_rpm,
		       COALESCE(e.name, ''), k.allow_environment_override, k.expires_at
		FROM gateway_api_keys k
		LEFT JOIN environments e ON e.id = k.environment_id
		WHERE k.key_hash = $1 AND k.is_active = true
	`, keyHash)

	if err := row.Scan(
		&info.ID, &info.CustomerID, &info.Name, &info.KeyPrefix,
		&info.Scopes, &info.RateLimitRPM, &info.Environment,
		&info.AllowEnvironmentOverride, &info.ExpiresAt,
	); err != nil {
		return nil, fmt.Errorf("api key not found")
	}

	if info.ExpiresAt != nil && info.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("api key expired")
	}

	// Cache for 5 minutes
	if err := s.cache.Set(ctx, cacheKey, &info, 5*time.Minute); err != nil {
		slog.Warn("failed to cache api key", "error", err)
	}

	// Update last_used_at asynchronously (fire and forget)
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.db.Exec(bgCtx,
			"UPDATE gateway_api_keys SET last_used_at = now() WHERE id = $1", info.ID)
	}()

	return &info, nil
}

// HashAPIKey produces a SHA-256 hash of an API key for storage/lookup.
func HashAPIKey(apiKey string) string {
	h := sha256.Sum256([]byte(apiKey))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// GenerateAPIKey creates a new API key with the bastio prefix.
func GenerateAPIKey() string {
	id := uuid.New()
	return "sk-bastio-" + strings.ReplaceAll(id.String(), "-", "")
}

func extractAPIKey(r *http.Request) string {
	// Check Authorization header: "Bearer sk-bastio-..."
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Also support X-API-Key header
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	return ""
}

func safePrefix(key string) string {
	if len(key) > 12 {
		return key[:12] + "..."
	}
	return "***"
}
