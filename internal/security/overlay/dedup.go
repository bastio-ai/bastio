package overlay

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// ShadowEventDeduper caps shadow-event write volume by suppressing
// identical (shadow_version_id, divergence) records seen inside a
// rolling window. Primary reason: a tenant with a noisy shadow
// overlay can flood tenant_policy_overlay_shadow_events on every
// request. Operators don't need thousands of rows saying the same
// thing — the first few within a window are enough signal.
//
// Dedup is advisory: the caller still writes the record if
// ShouldRecord returns true, and events from DIFFERENT shadow
// versions or DIFFERENT divergence types always pass through. The
// window is per-version so promoting a new shadow resets the
// counter.
//
// Implementation:
//   - sync.Map keyed by "<version_id>:<divergence>".
//   - Each key stores the last-seen Unix-nano timestamp.
//   - A background reaper would be nicer than the opportunistic
//     cleanup below but adds shutdown concerns; for v1 the map
//     stays bounded by the finite set of (version, divergence)
//     tuples any tenant can produce (tiny in practice).
type ShadowEventDeduper struct {
	window time.Duration
	last   sync.Map // map[string]int64 (Unix nanos)
}

// NewShadowEventDeduper returns a deduper with the given window. A
// zero or negative window disables dedup — every call returns true.
func NewShadowEventDeduper(window time.Duration) *ShadowEventDeduper {
	return &ShadowEventDeduper{window: window}
}

// ShouldRecord reports whether an event for the given shadow version
// and divergence type should be written now. Returns true for the
// first occurrence in a window, false for subsequent ones. Advances
// the last-seen timestamp on every "true" return so the window
// rolls forward from the most recent recorded event.
func (d *ShadowEventDeduper) ShouldRecord(shadowVersionID uuid.UUID, divergence string) bool {
	if d == nil || d.window <= 0 {
		return true
	}
	key := shadowVersionID.String() + ":" + divergence
	now := time.Now().UnixNano()

	if prev, ok := d.last.Load(key); ok {
		if now-prev.(int64) < int64(d.window) {
			return false
		}
	}
	d.last.Store(key, now)
	return true
}

// Reset clears the deduper state — useful in tests and when a new
// shadow version is promoted (to avoid carrying forward "last seen"
// records from the prior version, though keying by version id
// already provides natural isolation).
func (d *ShadowEventDeduper) Reset() {
	if d == nil {
		return
	}
	d.last.Range(func(k, _ any) bool {
		d.last.Delete(k)
		return true
	})
}

// DefaultShadowDedupWindow is the baseline window — 60 seconds
// coalesces the common "same divergence for thousands of requests"
// case without hiding genuinely new divergences for long.
const DefaultShadowDedupWindow = 60 * time.Second
