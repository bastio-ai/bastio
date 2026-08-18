package server

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/bastio-ai/bastio/internal/security/session"
	"github.com/bastio-ai/bastio/pkg/cache"
	"github.com/bastio-ai/bastio/pkg/tenant"
)

// BuildSecurityEngine constructs the canonical Bastio security engine
// (every detector + profile lookup) so the same configuration drives
// the gateway, the workspace handler, the standalone worker, the
// detect SDK endpoint, and any future surface that needs scanning.
//
// Without a single shared constructor the construction drifts: the
// gateway gets the full detector list, the worker gets a half-built
// engine, and a malicious chunk that the gateway would block lands
// happily in storage. This helper guarantees parity.
//
// Inputs:
//
//   - pool: required. The Postgres pool. The TopicPolicy detector
//     pulls per-customer rules from PG; the engine boots without
//     them and the detector self-disables when the table is empty.
//   - redis: optional. When non-nil, the multi-turn Crescendo
//     jailbreak detector is wired with a Redis-backed session store
//     so attacks distributed across multiple turns get caught.
//     When nil, Crescendo no-ops — single-turn detection still works
//     fully via the other detectors.
//
// Returns the engine, the per-customer profile lookup, and the topic
// detector (for cache invalidation after dashboard pattern writes).
// All three are safe to share across goroutines.
func BuildSecurityEngine(_ context.Context, pool *pgxpool.Pool, redis *cache.Cache) (*security.Engine, security.ProfileLookup, *detection.TopicPolicyDetector) {
	topic := detection.NewTopicPolicyDetector(pool, func(ctx context.Context) uuid.UUID {
		if id, err := tenant.FromContext(ctx); err == nil {
			return id
		}
		return tenant.DefaultOSSID
	}, 0)
	engine := security.NewEngine(
		detection.NewInjectionDetector(),
		detection.NewPIIDetector(),
		detection.NewJailbreakDetector(),
		detection.NewSecretsDetector(),
		detection.NewIndirectInjectionDetector(),
		detection.NewExfilDetector(),
		topic,
	)
	if redis != nil {
		// Crescendo needs cross-request memory; without Redis the
		// session store is a no-op (returns nil for every
		// lookup) and the detector skips. That's the documented
		// degraded mode for OSS deployments without Redis.
		store := session.NewRedisStore(redis.Client())
		engine.SetSessionAware(store, detection.NewCrescendoDetector(store))
		// Rate anomaly shares the same session buffer. Registered
		// unconditionally but profile-gated: it only runs on scans
		// whose profile sets rate_anomaly_enabled (default off).
		engine.AddSessionDetector(detection.NewRateAnomalyDetector(store, detection.DefaultRateAnomalyConfig()))
	}
	return engine, security.NewProfileLookup(pool), topic
}
