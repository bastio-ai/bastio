package iplist

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProvider is a Provider with swappable data + error for driving
// manager tests without a network.
type fakeProvider struct {
	name string

	mu       sync.Mutex
	prefixes []netip.Prefix
	err      error

	fetches atomic.Int64
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Fetch(_ context.Context) ([]netip.Prefix, error) {
	f.fetches.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]netip.Prefix, len(f.prefixes))
	copy(out, f.prefixes)
	return out, nil
}

func (f *fakeProvider) set(prefixes []netip.Prefix, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefixes = prefixes
	f.err = err
}

func mustPrefixes(t *testing.T, ss ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, ok := parsePrefixOrAddr(s)
		if !ok {
			t.Fatalf("bad test prefix %q", s)
		}
		out = append(out, p)
	}
	return out
}

func addr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("bad test addr %q: %v", s, err)
	}
	return a
}

func TestLookupBoundariesAndOverlaps(t *testing.T) {
	fh := &fakeProvider{name: "firehol_level1"}
	fh.set(mustPrefixes(t,
		"192.0.2.0/24",
		"10.0.0.0/8",
		"10.1.0.0/16", // contained in 10.0.0.0/8 — merge must not break /8 hits
		"203.0.113.128/25",
		"2001:db8::/32",
	), nil)
	m := NewManager([]Provider{fh})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	cases := []struct {
		ip     string
		listed bool
	}{
		{"192.0.2.0", true},    // network address (low boundary)
		{"192.0.2.255", true},  // broadcast (high boundary)
		{"192.0.1.255", false}, // one below
		{"192.0.3.0", false},   // one above
		{"10.0.0.0", true},
		{"10.1.2.3", true},   // inside the contained /16
		{"10.200.0.1", true}, // inside /8 but after /16 — the merge-correctness case
		{"10.255.255.255", true},
		{"11.0.0.0", false},
		{"203.0.113.127", false}, // just below /25 base
		{"203.0.113.128", true},
		{"203.0.113.255", true},
		{"2001:db8::", true},
		{"2001:db8:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:db9::", false},
		{"::ffff:192.0.2.5", true}, // 4-mapped form unmaps to a listed v4
	}
	for _, tc := range cases {
		v := m.Lookup(addr(t, tc.ip))
		if v.Listed != tc.listed {
			t.Errorf("Lookup(%s).Listed = %v, want %v", tc.ip, v.Listed, tc.listed)
		}
		if tc.listed && (len(v.Lists) != 1 || v.Lists[0] != "firehol_level1") {
			t.Errorf("Lookup(%s).Lists = %v, want [firehol_level1]", tc.ip, v.Lists)
		}
	}
}

func TestLookupMultipleLists(t *testing.T) {
	fh := &fakeProvider{name: "firehol_level1"}
	fh.set(mustPrefixes(t, "185.220.101.0/24"), nil)
	tor := &fakeProvider{name: "tor_exits"}
	tor.set(mustPrefixes(t, "185.220.101.1"), nil)

	m := NewManager([]Provider{fh, tor})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	v := m.Lookup(addr(t, "185.220.101.1"))
	if !v.Listed || len(v.Lists) != 2 {
		t.Fatalf("verdict = %+v, want both lists", v)
	}
	if v.Lists[0] != "firehol_level1" || v.Lists[1] != "tor_exits" {
		t.Fatalf("lists = %v", v.Lists)
	}

	v = m.Lookup(addr(t, "185.220.101.2"))
	if !v.Listed || len(v.Lists) != 1 || v.Lists[0] != "firehol_level1" {
		t.Fatalf("verdict = %+v, want firehol only", v)
	}
}

func TestLookupBeforeFirstRefreshIsUnlisted(t *testing.T) {
	m := NewManager([]Provider{&fakeProvider{name: "firehol_level1"}})
	if v := m.Lookup(addr(t, "192.0.2.1")); v.Listed {
		t.Fatalf("verdict = %+v, want unlisted before first refresh", v)
	}
}

func TestLookupMissIsAllocationFree(t *testing.T) {
	fh := &fakeProvider{name: "firehol_level1"}
	fh.set(mustPrefixes(t, "192.0.2.0/24", "10.0.0.0/8", "2001:db8::/32"), nil)
	m := NewManager([]Provider{fh})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	miss := addr(t, "8.8.8.8")
	allocs := testing.AllocsPerRun(200, func() {
		if m.Lookup(miss).Listed {
			t.Fatal("unexpected hit")
		}
	})
	if allocs != 0 {
		t.Errorf("Lookup miss allocates %.1f times per run, want 0", allocs)
	}
}

func TestRefreshKeepsStaleListOnProviderError(t *testing.T) {
	fh := &fakeProvider{name: "firehol_level1"}
	fh.set(mustPrefixes(t, "192.0.2.0/24"), nil)
	m := NewManager([]Provider{fh})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	fh.set(nil, errors.New("feed down"))
	err := m.Refresh(context.Background())
	if err == nil {
		t.Fatal("refresh: want error when provider fails")
	}
	// Stale data must survive a failed refresh.
	if v := m.Lookup(addr(t, "192.0.2.1")); !v.Listed {
		t.Fatalf("verdict = %+v, want stale data retained", v)
	}

	// Recovery replaces the stale set.
	fh.set(mustPrefixes(t, "198.51.100.0/24"), nil)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh after recovery: %v", err)
	}
	if v := m.Lookup(addr(t, "192.0.2.1")); v.Listed {
		t.Fatalf("verdict = %+v, want old range gone after recovery", v)
	}
	if v := m.Lookup(addr(t, "198.51.100.1")); !v.Listed {
		t.Fatalf("verdict = %+v, want new range active", v)
	}
}

func TestStartRefreshLoopAndStop(t *testing.T) {
	fh := &fakeProvider{name: "firehol_level1"}
	fh.set(mustPrefixes(t, "192.0.2.0/24"), nil)
	m := NewManager([]Provider{fh}, WithRefreshInterval(5*time.Millisecond))

	m.Start(context.Background())
	deadline := time.After(2 * time.Second)
	for fh.fetches.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("refresh loop ran %d times, want >= 3", fh.fetches.Load())
		case <-time.After(2 * time.Millisecond):
		}
	}
	m.Stop()

	after := fh.fetches.Load()
	time.Sleep(20 * time.Millisecond)
	if got := fh.fetches.Load(); got != after {
		t.Fatalf("fetches advanced from %d to %d after Stop", after, got)
	}
}

// TestConcurrentLookupDuringRefresh exercises the atomic snapshot swap
// under -race: many readers loop Lookup while refreshes flip between
// two datasets. Every verdict must be internally consistent.
func TestConcurrentLookupDuringRefresh(t *testing.T) {
	fh := &fakeProvider{name: "firehol_level1"}
	setA := mustPrefixes(t, "192.0.2.0/24")
	setB := mustPrefixes(t, "198.51.100.0/24")
	fh.set(setA, nil)

	m := NewManager([]Provider{fh})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	a, b := addr(t, "192.0.2.7"), addr(t, "198.51.100.7")
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Each individual Lookup must be internally
				// consistent regardless of swaps happening between
				// calls: Listed iff Lists is populated, and only with
				// the known list name. (Two sequential Lookups may
				// legitimately observe different snapshots.)
				for _, v := range []Verdict{m.Lookup(a), m.Lookup(b)} {
					if v.Listed != (len(v.Lists) > 0) {
						t.Errorf("inconsistent verdict %+v", v)
						return
					}
					if v.Listed && v.Lists[0] != "firehol_level1" {
						t.Errorf("unexpected list in verdict %+v", v)
						return
					}
				}
			}
		}()
	}

	for i := range 200 {
		if i%2 == 0 {
			fh.set(setB, nil)
		} else {
			fh.set(setA, nil)
		}
		if err := m.Refresh(context.Background()); err != nil {
			t.Errorf("refresh %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

func TestVerdictFinding(t *testing.T) {
	v := Verdict{Listed: true, Lists: []string{"firehol_level1", "tor_exits"}}
	f := v.Finding()
	if string(f.ThreatType) != "ip_reputation" {
		t.Errorf("threat type = %q", f.ThreatType)
	}
	if f.DetectorName != "iplist" {
		t.Errorf("detector = %q", f.DetectorName)
	}
	if f.MatchedPattern != "firehol_level1,tor_exits" {
		t.Errorf("matched pattern = %q", f.MatchedPattern)
	}
	if f.Confidence != 1.0 || f.Score <= 0 {
		t.Errorf("score/confidence = %v/%v", f.Score, f.Confidence)
	}
}
