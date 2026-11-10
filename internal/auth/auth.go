package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
		return "", fmt.Errorf("validate environment override: %w: %w", ErrLookupUnavailable, err)
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

// Authentication outcomes. ErrKeyNotFound and ErrKeyExpired are definite
// answers about the key (401). ErrLookupUnavailable means the key could not
// be checked at all (cancelled request, Redis and PostgreSQL both failing,
// pool closed during shutdown) and must never be reported as an invalid key.
var (
	ErrKeyNotFound       = errors.New("api key not found")
	ErrKeyExpired        = errors.New("api key expired")
	ErrLookupUnavailable = errors.New("api key lookup unavailable")
)

// RetryAfterSeconds is the Retry-After hint sent with a 503 when the key
// store could not be reached. Restarts finish within a few seconds.
const RetryAfterSeconds = "1"

// keyCache is the cache tier of the API key lookup.
type keyCache interface {
	getKey(ctx context.Context, keyHash string) (*APIKeyInfo, bool, error)
	setKey(ctx context.Context, keyHash string, info *APIKeyInfo) error
}

// keyStore is the authoritative tier of the API key lookup. lookupKey
// returns pgx.ErrNoRows (possibly wrapped) when no active key matches.
type keyStore interface {
	lookupKey(ctx context.Context, keyHash string) (*APIKeyInfo, error)
	touchKey(ctx context.Context, id uuid.UUID)
}

type redisKeyCache struct{ c *cache.Cache }

func (k redisKeyCache) getKey(ctx context.Context, keyHash string) (*APIKeyInfo, bool, error) {
	var info APIKeyInfo
	found, err := k.c.Get(ctx, "apikey:"+keyHash, &info)
	if err != nil || !found {
		return nil, false, err
	}
	return &info, true, nil
}

func (k redisKeyCache) setKey(ctx context.Context, keyHash string, info *APIKeyInfo) error {
	return k.c.Set(ctx, "apikey:"+keyHash, info, 5*time.Minute)
}

type pgKeyStore struct{ db *pgxpool.Pool }

func (p pgKeyStore) lookupKey(ctx context.Context, keyHash string) (*APIKeyInfo, error) {
	var info APIKeyInfo
	err := p.db.QueryRow(ctx, `
		SELECT k.id, k.customer_id, k.name, k.key_prefix, k.scopes, k.rate_limit_rpm,
		       COALESCE(e.name, ''), k.allow_environment_override, k.expires_at
		FROM gateway_api_keys k
		LEFT JOIN environments e ON e.id = k.environment_id
		WHERE k.key_hash = $1 AND k.is_active = true
	`, keyHash).Scan(
		&info.ID, &info.CustomerID, &info.Name, &info.KeyPrefix,
		&info.Scopes, &info.RateLimitRPM, &info.Environment,
		&info.AllowEnvironmentOverride, &info.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (p pgKeyStore) touchKey(ctx context.Context, id uuid.UUID) {
	_, _ = p.db.Exec(ctx, "UPDATE gateway_api_keys SET last_used_at = now() WHERE id = $1", id)
}

// Service handles API key authentication.
type Service struct {
	db    *pgxpool.Pool
	cache *cache.Cache
	keys  keyCache
	store keyStore
}

// NewService creates a new auth service.
func NewService(db *pgxpool.Pool, cache *cache.Cache) *Service {
	return &Service{db: db, cache: cache, keys: redisKeyCache{c: cache}, store: pgKeyStore{db: db}}
}

// WriteLookupUnavailable writes the 503 sent when an API key could not be
// checked. Clients should retry after Retry-After instead of treating the
// key as revoked.
func WriteLookupUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", RetryAfterSeconds)
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":{"type":"service_unavailable","code":"auth_unavailable","message":"service restarting, retry shortly"}}`))
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
		if errors.Is(err, ErrLookupUnavailable) {
			// Fixed fields only: the key and the error text derive from the request.
			slog.Warn("api key lookup unavailable, returning 503", "request_cancelled", r.Context().Err() != nil)
			WriteLookupUnavailable(w)
			return
		}
		if err != nil {
			slog.Warn("auth failed", "error", err, "prefix", safePrefix(apiKey))
			http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
			return
		}
		environment, err := EnvironmentFromRequest(r.Context(), s.db, info, r.Header.Get("X-Bastio-Environment"))
		if errors.Is(err, ErrLookupUnavailable) {
			slog.Warn("environment lookup unavailable, returning 503", "request_cancelled", r.Context().Err() != nil)
			WriteLookupUnavailable(w)
			return
		}
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
//
// Errors: ErrKeyNotFound / ErrKeyExpired are definite (401). Anything that
// prevented a definite answer wraps ErrLookupUnavailable (503). A Redis
// failure alone falls back to PostgreSQL as before.
func (s *Service) Authenticate(ctx context.Context, apiKey string) (*APIKeyInfo, error) {
	keyHash := HashAPIKey(apiKey)

	// Tier 1: cache lookup
	cached, found, err := s.keys.getKey(ctx, keyHash)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// The request is gone or timed out; the DB query would fail
			// the same way, so don't spend a pool connection on it.
			return nil, fmt.Errorf("%w: %w", ErrLookupUnavailable, ctxErr)
		}
		slog.Warn("cache lookup failed, falling back to db", "error", err)
	}
	if found {
		if cached.ExpiresAt != nil && cached.ExpiresAt.Before(time.Now()) {
			return nil, ErrKeyExpired
		}
		return cached, nil
	}

	// Tier 2: database lookup
	info, err := s.store.lookupKey(ctx, keyHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("%w: %w", ErrLookupUnavailable, err)
	}

	if info.ExpiresAt != nil && info.ExpiresAt.Before(time.Now()) {
		return nil, ErrKeyExpired
	}

	// Positive results only are cached (5 minutes); misses and lookup
	// failures are never written.
	if err := s.keys.setKey(ctx, keyHash, info); err != nil {
		slog.Warn("failed to cache api key", "error", err)
	}

	// Update last_used_at asynchronously (fire and forget)
	go func(id uuid.UUID) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.store.touchKey(bgCtx, id)
	}(info.ID)

	return info, nil
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
	if key := r.Header.Get("X-Goog-Api-Key"); key != "" {
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
