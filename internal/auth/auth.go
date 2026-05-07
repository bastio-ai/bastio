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
	ID           uuid.UUID  `json:"id"`
	CustomerID   uuid.UUID  `json:"customer_id"`
	Name         string     `json:"name"`
	KeyPrefix    string     `json:"key_prefix"`
	Scopes       []string   `json:"scopes"`
	RateLimitRPM *int       `json:"rate_limit_rpm"`
	ExpiresAt    *time.Time `json:"expires_at"`
}

type contextKey string

const apiKeyContextKey contextKey = "api_key_info"

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

		ctx := context.WithValue(r.Context(), apiKeyContextKey, info)
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
		SELECT id, customer_id, name, key_prefix, scopes, rate_limit_rpm, expires_at
		FROM gateway_api_keys
		WHERE key_hash = $1 AND is_active = true
	`, keyHash)

	if err := row.Scan(
		&info.ID, &info.CustomerID, &info.Name, &info.KeyPrefix,
		&info.Scopes, &info.RateLimitRPM, &info.ExpiresAt,
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
