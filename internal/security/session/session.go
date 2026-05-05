// Package session stores a rolling buffer of recent per-session
// scan summaries. The Crescendo detector reads it to flag multi-turn
// attacks (gradual escalation, priming-then-payload sequences).
//
// Storage is pluggable: the interface is small (Recent/Append) so
// tests and non-Redis deployments can swap in a memory-backed
// implementation. Production uses Redis with an LPUSH + LTRIM + EXPIRE
// pattern keyed by session_id.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Entry is one row in the per-session buffer — the compacted summary
// of a single scan. We deliberately *don't* store the user's content
// (privacy + size); the score + top sub-category are enough to
// reconstruct escalation patterns.
type Entry struct {
	Score          float64   `json:"score"`
	TopSubCategory string    `json:"sub_category,omitempty"`
	Action         string    `json:"action,omitempty"`
	At             time.Time `json:"at"`
}

// Store is the minimal interface Crescendo needs. Append is
// fire-and-forget in the gateway hot path; Recent must be cheap
// enough to run inline before detectors (~<5ms target).
type Store interface {
	Recent(ctx context.Context, sessionID string, n int) ([]Entry, error)
	Append(ctx context.Context, sessionID string, entry Entry) error
}

// NewRedisStore wraps a go-redis client. Safe to share across
// goroutines. A nil client is tolerated — every method becomes a
// no-op, which is what callers expect when Redis isn't configured.
func NewRedisStore(client *redis.Client) Store {
	return &redisStore{client: client}
}

type redisStore struct {
	client *redis.Client
}

// maxBufferLen caps the per-session history to keep memory small and
// the Recent() call cheap. 10 turns is plenty — escalation patterns
// play out in 3-5 turns; anything longer than that is a separate
// attack surface for a dedicated detector.
const maxBufferLen = 10

// bufferTTL refreshes on every append, so only sessions that stop
// arriving for an hour get expired.
const bufferTTL = time.Hour

func key(sessionID string) string { return "crescendo:session:" + sessionID }

func (s *redisStore) Recent(ctx context.Context, sessionID string, n int) ([]Entry, error) {
	if s.client == nil || sessionID == "" {
		return nil, nil
	}
	if n <= 0 || n > maxBufferLen {
		n = maxBufferLen
	}
	raws, err := s.client.LRange(ctx, key(sessionID), 0, int64(n-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("crescendo history read: %w", err)
	}
	out := make([]Entry, 0, len(raws))
	for _, raw := range raws {
		var e Entry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			continue // skip malformed rows rather than fail the whole read
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *redisStore) Append(ctx context.Context, sessionID string, entry Entry) error {
	if s.client == nil || sessionID == "" {
		return nil
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("crescendo marshal: %w", err)
	}
	pipe := s.client.TxPipeline()
	pipe.LPush(ctx, key(sessionID), raw)
	pipe.LTrim(ctx, key(sessionID), 0, int64(maxBufferLen-1))
	pipe.Expire(ctx, key(sessionID), bufferTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("crescendo append: %w", err)
	}
	return nil
}

// NewMemoryStore returns an in-memory Store — useful for tests and
// single-process deployments where Redis isn't warranted.
func NewMemoryStore() Store { return &memStore{data: map[string][]Entry{}} }

type memStore struct {
	// Simple; tests are single-goroutine so a mutex isn't needed. If
	// production ever uses this, add one.
	data map[string][]Entry
}

func (s *memStore) Recent(_ context.Context, sessionID string, n int) ([]Entry, error) {
	if sessionID == "" {
		return nil, nil
	}
	buf := s.data[sessionID]
	if n <= 0 || n > len(buf) {
		n = len(buf)
	}
	out := make([]Entry, n)
	copy(out, buf[:n])
	return out, nil
}

func (s *memStore) Append(_ context.Context, sessionID string, entry Entry) error {
	if sessionID == "" {
		return nil
	}
	buf := append([]Entry{entry}, s.data[sessionID]...)
	if len(buf) > maxBufferLen {
		buf = buf[:maxBufferLen]
	}
	s.data[sessionID] = buf
	return nil
}
