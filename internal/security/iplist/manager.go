package iplist

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/netip"
	"sort"
	"sync/atomic"
	"time"
)

// DefaultRefreshInterval is how often the manager re-fetches the feeds
// when no override is configured. FireHOL level1 updates a few times a
// day and the Tor exit list roughly hourly; 6h keeps us fresh without
// hammering the public mirrors.
const DefaultRefreshInterval = 6 * time.Hour

// ipRange is an inclusive [lo, hi] address range. Ranges within a
// listSet are sorted by lo and non-overlapping (compile merges), so a
// predecessor binary search is sufficient for containment.
type ipRange struct {
	lo, hi netip.Addr
}

// listSet is one compiled threat list.
type listSet struct {
	name   string
	ranges []ipRange
}

// contains reports whether addr falls inside any range. O(log n),
// allocation-free.
func (ls *listSet) contains(addr netip.Addr) bool {
	i := sort.Search(len(ls.ranges), func(i int) bool {
		return ls.ranges[i].lo.Compare(addr) > 0
	})
	if i == 0 {
		return false
	}
	return ls.ranges[i-1].hi.Compare(addr) >= 0
}

// snapshot is the immutable active set. Refresh builds a fresh one and
// swaps the pointer atomically; Lookup never blocks on a refresh.
type snapshot struct {
	lists []listSet
}

func (s *snapshot) find(name string) (listSet, bool) {
	for _, ls := range s.lists {
		if ls.name == name {
			return ls, true
		}
	}
	return listSet{}, false
}

// Manager owns the compiled threat lists and the background refresh
// loop. Construct with NewManager, then Start to begin refreshing;
// Lookup is safe for concurrent use at any point (it returns an
// unlisted verdict until the first refresh completes).
//
// TODO(iplist): optional Redis L2 via pkg/cache so replicas share one
// feed download instead of each fetching independently. Out of scope
// for this pass — in-memory only.
type Manager struct {
	providers []Provider
	interval  time.Duration
	logger    *slog.Logger

	snap   atomic.Pointer[snapshot]
	cancel context.CancelFunc
	done   chan struct{}
}

// ManagerOption configures a Manager at construction time.
type ManagerOption func(*Manager)

// WithRefreshInterval overrides the background refresh cadence.
// Non-positive values keep the default.
func WithRefreshInterval(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.interval = d
		}
	}
}

// WithLogger overrides the logger (defaults to slog.Default()).
func WithLogger(l *slog.Logger) ManagerOption {
	return func(m *Manager) {
		if l != nil {
			m.logger = l
		}
	}
}

// NewManager builds a Manager over the given providers. No network
// activity happens until Refresh or Start.
func NewManager(providers []Provider, opts ...ManagerOption) *Manager {
	m := &Manager{
		providers: providers,
		interval:  DefaultRefreshInterval,
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(m)
	}
	m.snap.Store(&snapshot{})
	return m
}

// Refresh fetches every provider, compiles the results, and atomically
// swaps the active set. A failing provider keeps its previous data
// (stale beats empty); the joined error is returned so callers can log
// it without losing the successful lists.
func (m *Manager) Refresh(ctx context.Context) error {
	prev := m.snap.Load()
	next := &snapshot{lists: make([]listSet, 0, len(m.providers))}
	var errs []error
	for _, p := range m.providers {
		prefixes, err := p.Fetch(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("refresh %s: %w", p.Name(), err))
			if old, ok := prev.find(p.Name()); ok {
				next.lists = append(next.lists, old)
			}
			continue
		}
		next.lists = append(next.lists, listSet{name: p.Name(), ranges: compile(prefixes)})
	}
	m.snap.Store(next)
	return errors.Join(errs...)
}

// Start launches the background refresh loop: one immediate refresh,
// then one per interval with ±10% jitter so a fleet of replicas does
// not stampede the public mirrors in lockstep. Cancel the context or
// call Stop to terminate. Start must be called at most once.
func (m *Manager) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})
	go func() {
		defer close(m.done)
		if err := m.Refresh(ctx); err != nil {
			m.logger.Warn("iplist: initial refresh incomplete", "error", err)
		}
		for {
			timer := time.NewTimer(jitter(m.interval))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				if err := m.Refresh(ctx); err != nil {
					m.logger.Warn("iplist: refresh incomplete", "error", err)
				}
			}
		}
	}()
}

// Stop cancels the refresh loop and waits for it to exit. Safe to call
// when Start was never called.
func (m *Manager) Stop() {
	if m.cancel == nil {
		return
	}
	m.cancel()
	<-m.done
}

// Lookup checks addr against every active list. O(L · log n) where L
// is the number of lists (two in the default wiring). The miss path is
// allocation-free; a hit allocates only the Lists slice on the verdict.
func (m *Manager) Lookup(addr netip.Addr) Verdict {
	if !addr.IsValid() {
		return Verdict{}
	}
	addr = addr.Unmap()
	snap := m.snap.Load()
	var v Verdict
	for i := range snap.lists {
		if snap.lists[i].contains(addr) {
			v.Listed = true
			v.Lists = append(v.Lists, snap.lists[i].name)
		}
	}
	return v
}

// compile turns raw prefixes into a sorted, merged range set. Merging
// overlapping and adjacent-containing prefixes (10.0.0.0/8 swallowing
// 10.1.0.0/16) is what makes the predecessor binary search in contains
// correct: after merging, the only candidate range for any address is
// the one with the greatest lo ≤ addr.
func compile(prefixes []netip.Prefix) []ipRange {
	ranges := make([]ipRange, 0, len(prefixes))
	for _, p := range prefixes {
		if !p.IsValid() {
			continue
		}
		p = p.Masked()
		ranges = append(ranges, ipRange{lo: p.Addr(), hi: lastAddr(p)})
	}
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].lo.Compare(ranges[j].lo) < 0
	})
	out := make([]ipRange, 0, len(ranges))
	for _, r := range ranges {
		if n := len(out); n > 0 && out[n-1].hi.Compare(r.lo) >= 0 {
			// Overlaps (or is contained by) the previous range —
			// extend instead of appending. Cross-family false merges
			// are impossible: netip orders all IPv4 before all IPv6,
			// so a v6 lo never compares ≤ a v4 hi.
			if r.hi.Compare(out[n-1].hi) > 0 {
				out[n-1].hi = r.hi
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// lastAddr returns the highest address inside p (host bits all-ones).
func lastAddr(p netip.Prefix) netip.Addr {
	if p.Addr().Is4() {
		a := p.Addr().As4()
		for b := p.Bits(); b < 32; b++ {
			a[b/8] |= 1 << (7 - b%8)
		}
		return netip.AddrFrom4(a)
	}
	a := p.Addr().As16()
	for b := p.Bits(); b < 128; b++ {
		a[b/8] |= 1 << (7 - b%8)
	}
	return netip.AddrFrom16(a)
}

// jitter spreads d by ±10%.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	f := 0.9 + rand.Float64()*0.2
	return time.Duration(float64(d) * f)
}
