package overlay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/pkg/cache"
)

// Loader reads the active and shadow overlay snapshots for a
// (customer, proxy) pair. It sits on the gateway hot path and must
// stay under the latency budget — Redis-cached with a short TTL,
// with a negative cache so tenants without an overlay don't pay the
// cost of a PG round-trip per request.
type Loader struct {
	db    *pgxpool.Pool
	cache *cache.Cache

	ttl         time.Duration
	negativeTTL time.Duration
}

// NewLoader constructs a Loader over the shared pool and cache. A nil
// cache is permitted and disables caching (useful in tests); a nil db
// makes every call return (nil, nil), used by OSS configurations that
// haven't run the migration yet.
func NewLoader(db *pgxpool.Pool, c *cache.Cache) *Loader {
	return &Loader{
		db:          db,
		cache:       c,
		ttl:         60 * time.Second,
		negativeTTL: 30 * time.Second,
	}
}

// SetTTL overrides the default cache TTLs. Useful for tests that want
// fast invalidation and for deployments that want to tune propagation.
func (l *Loader) SetTTL(ttl, negativeTTL time.Duration) {
	if ttl > 0 {
		l.ttl = ttl
	}
	if negativeTTL > 0 {
		l.negativeTTL = negativeTTL
	}
}

// LoadActive returns the active overlay snapshot for (customer, proxy),
// or nil if none exists. Identity carries the overlay/version IDs so
// callers (like merge) can attribute loosening warnings to the right
// overlay. Errors from Postgres or Redis are returned — the caller
// decides whether to fail open or closed.
func (l *Loader) LoadActive(ctx context.Context, customerID, proxyID uuid.UUID) (*OverlaySnapshot, Identity, error) {
	return l.loadByState(ctx, customerID, proxyID, StateActive, "overlay:active")
}

// LoadShadow returns the current shadow overlay, or nil if none.
func (l *Loader) LoadShadow(ctx context.Context, customerID, proxyID uuid.UUID) (*OverlaySnapshot, Identity, error) {
	return l.loadByState(ctx, customerID, proxyID, StateShadow, "overlay:shadow")
}

// cachedEntry is what we actually store in Redis. Nil Snapshot with
// Found=false is the negative-cache sentinel.
type cachedEntry struct {
	Found    bool             `json:"found"`
	Snapshot *OverlaySnapshot `json:"snapshot,omitempty"`
	Identity Identity         `json:"identity"`
}

func (l *Loader) loadByState(
	ctx context.Context,
	customerID, proxyID uuid.UUID,
	state VersionState,
	cachePrefix string,
) (*OverlaySnapshot, Identity, error) {
	if l.db == nil {
		return nil, Identity{}, nil
	}
	key := cachePrefix + ":" + customerID.String() + ":" + proxyID.String()

	if l.cache != nil {
		var entry cachedEntry
		ok, err := l.cache.Get(ctx, key, &entry)
		if err != nil {
			// Cache errors shouldn't fail the request path — log and fall
			// through to DB.
			slog.Warn("overlay loader: cache get failed", "key", key, "error", err)
		} else if ok {
			if !entry.Found {
				return nil, Identity{}, nil
			}
			return entry.Snapshot, entry.Identity, nil
		}
	}

	snap, ident, err := l.queryDB(ctx, customerID, proxyID, state)
	if err != nil {
		return nil, Identity{}, err
	}

	if l.cache != nil {
		entry := cachedEntry{Found: snap != nil, Snapshot: snap, Identity: ident}
		ttl := l.ttl
		if snap == nil {
			ttl = l.negativeTTL
		}
		if err := l.cache.Set(ctx, key, entry, ttl); err != nil {
			slog.Warn("overlay loader: cache set failed", "key", key, "error", err)
		}
	}
	return snap, ident, nil
}

// queryDB runs the DB read. It picks the per-proxy overlay first,
// falling back to the customer-wide overlay (proxy_id IS NULL) if
// none exists. This keeps per-proxy overlays authoritative when both
// are present, without requiring callers to know about the fallback.
func (l *Loader) queryDB(
	ctx context.Context,
	customerID, proxyID uuid.UUID,
	state VersionState,
) (*OverlaySnapshot, Identity, error) {
	// proxyID may be Nil (no proxy context) — in that case only the
	// customer-wide overlay applies.
	query := `
		SELECT v.id, v.overlay_id, v.version, v.snapshot
		FROM tenant_policy_overlay_versions v
		JOIN tenant_policy_overlays o ON o.id = v.overlay_id
		WHERE v.customer_id = $1
		  AND v.state = $2
		  AND (o.proxy_id = $3 OR o.proxy_id IS NULL)
		ORDER BY (o.proxy_id = $3) DESC NULLS LAST, o.created_at DESC
		LIMIT 1
	`
	var versionID, overlayID uuid.UUID
	var versionNum int
	var snapJSON []byte
	err := l.db.QueryRow(ctx, query, customerID, string(state), proxyID).
		Scan(&versionID, &overlayID, &versionNum, &snapJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Identity{}, nil
	}
	if err != nil {
		return nil, Identity{}, fmt.Errorf("query overlay: %w", err)
	}

	var snap OverlaySnapshot
	if err := json.Unmarshal(snapJSON, &snap); err != nil {
		return nil, Identity{}, fmt.Errorf("decode snapshot: %w", err)
	}

	ident := Identity{OverlayID: overlayID, VersionID: versionID, Version: versionNum}
	return &snap, ident, nil
}

// Invalidate clears the cached entries for (customer, proxy). Called
// from the store after activate/rollback. The customer-wide variant
// (proxyID=Nil) is also cleared because it may have been hiding
// behind a per-proxy miss previously.
func (l *Loader) Invalidate(ctx context.Context, customerID, proxyID uuid.UUID) error {
	if l.cache == nil {
		return nil
	}
	keys := []string{
		"overlay:active:" + customerID.String() + ":" + proxyID.String(),
		"overlay:shadow:" + customerID.String() + ":" + proxyID.String(),
		"overlay:active:" + customerID.String() + ":" + uuid.Nil.String(),
		"overlay:shadow:" + customerID.String() + ":" + uuid.Nil.String(),
	}
	return l.cache.Del(ctx, keys...)
}
