package governance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/pkg/cache"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
)

// EventStore writes governance events to ClickHouse and dedupes incoming
// event_id values via Redis SETNX (24h TTL). Heartbeats land in a separate
// CH table so the dashboard can answer "which endpoints are still online?".
type EventStore struct {
	ch    *clickhouse.CH
	cache *cache.Cache
}

func NewEventStore(ch *clickhouse.CH, c *cache.Cache) *EventStore {
	return &EventStore{ch: ch, cache: c}
}

// AlreadySeen returns true if the event_id has been seen in the last 24h.
// Idempotent ingestion: duplicate POSTs return 200 with {duplicate: true}.
func (s *EventStore) AlreadySeen(ctx context.Context, eventID string) (bool, error) {
	if s.cache == nil || s.cache.Client() == nil {
		// Without Redis we cannot dedup. Accept all writes; CH is
		// append-only by design and downstream queries should DISTINCT
		// on event_id when needed.
		return false, nil
	}
	key := "bastio:gov:event:" + eventID
	ok, err := s.cache.Client().SetNX(ctx, key, "1", 24*time.Hour).Result()
	if err != nil {
		return false, fmt.Errorf("redis setnx: %w", err)
	}
	// SETNX returns true if the key was set; that means "first time we've
	// seen it." Inverted, false means "already there" → duplicate.
	return !ok, nil
}

// WriteEvent appends a single governance event to ClickHouse. In production
// the ingest service buffers and batches; this direct path is for low-volume
// OSS deployments and the synchronous test runner.
func (s *EventStore) WriteEvent(ctx context.Context, customerID uuid.UUID, e EventPayload) error {
	if s.ch == nil {
		return errors.New("clickhouse not configured")
	}

	occurredAt, err := time.Parse(time.RFC3339, e.OccurredAt)
	if err != nil {
		// Fall back to now() if the extension sent an unparseable
		// timestamp — never reject the event for clock-format reasons.
		occurredAt = time.Now().UTC()
	}

	const stmt = `INSERT INTO bastio.governance_events (
event_id, customer_id, user_id, occurred_at, source_domain, rule_ids,
severity, action, char_count_intercepted, browser, browser_version,
extension_version, redirect_target_label, override_justification
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	return s.ch.Exec(ctx, stmt,
		e.EventID,
		customerID,
		e.UserID,
		occurredAt,
		e.SourceDomain,
		e.RuleIDs,
		string(e.Severity),
		string(e.Action),
		e.CharCountIntercepted,
		e.Browser,
		e.BrowserVersion,
		e.ExtensionVersion,
		e.RedirectTargetLabel,
		e.OverrideJustification,
	)
}

// WriteHeartbeat upserts the latest seen-at for a given installation.
func (s *EventStore) WriteHeartbeat(ctx context.Context, customerID uuid.UUID, orgID uuid.UUID, h HeartbeatPayload) error {
	if s.ch == nil {
		return nil // heartbeats are best-effort
	}
	const stmt = `INSERT INTO bastio.governance_heartbeats (
customer_id, org_id, install_id, last_seen_at, browser, browser_version, extension_version
) VALUES (?, ?, ?, ?, ?, ?, ?)`
	return s.ch.Exec(ctx, stmt,
		customerID,
		orgID,
		h.InstallID,
		time.Now().UTC(),
		h.Browser,
		h.BrowserVersion,
		h.ExtensionVersion,
	)
}
