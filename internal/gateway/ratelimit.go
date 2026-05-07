package gateway

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/pkg/cache"
)

// RateLimiter provides per-API-key rate limiting via Redis.
type RateLimiter struct {
	cache *cache.Cache
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(cache *cache.Cache) *RateLimiter {
	return &RateLimiter{cache: cache}
}

// Middleware enforces per-key RPM rate limits.
// Keys without a rate_limit_rpm are not limited.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := auth.FromContext(r.Context())
		if !ok || info.RateLimitRPM == nil {
			next.ServeHTTP(w, r)
			return
		}

		limit := *info.RateLimitRPM
		key := fmt.Sprintf("ratelimit:%s", info.ID.String())

		count, err := rl.cache.Incr(r.Context(), key)
		if err != nil {
			slog.Warn("rate limit check failed, allowing request", "error", err)
			next.ServeHTTP(w, r)
			return
		}

		// Set expiry on first request in window
		if count == 1 {
			_ = rl.cache.Expire(r.Context(), key, 60*time.Second)
		}

		// Set rate limit headers
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(max(0, limit-int(count))))

		if int(count) > limit {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
