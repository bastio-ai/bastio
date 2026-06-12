package detection

import (
	"context"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/session"
)

// seedSession appends entries at the given ages (relative to now,
// oldest first so the memory store ends up newest-first like Redis).
func seedSession(t *testing.T, store session.Store, sessionID string, now time.Time, ages ...time.Duration) {
	t.Helper()
	for i := len(ages) - 1; i >= 0; i-- {
		err := store.Append(context.Background(), sessionID, session.Entry{
			Score: 0.1,
			At:    now.Add(-ages[i]),
		})
		if err != nil {
			t.Fatalf("seed append: %v", err)
		}
	}
}

func newTestRateAnomaly(store session.Store, now time.Time) *RateAnomalyDetector {
	d := NewRateAnomalyDetector(store, DefaultRateAnomalyConfig())
	d.now = func() time.Time { return now }
	return d
}

func TestRateAnomalySteadyTrafficDoesNotFire(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	store := session.NewMemoryStore()
	// 10 turns at a steady 6s cadence — a brisk but constant 10 req/min.
	seedSession(t, store, "s1", now,
		6*time.Second, 12*time.Second, 18*time.Second, 24*time.Second,
		30*time.Second, 36*time.Second, 42*time.Second, 48*time.Second,
		54*time.Second, 60*time.Second,
	)
	d := newTestRateAnomaly(store, now)

	findings, err := d.DetectWithSession(context.Background(), "s1", "hello", 0)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("steady traffic fired: %+v", findings)
	}
}

func TestRateAnomalyBurstFires(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	store := session.NewMemoryStore()
	// Baseline: one message a minute for five minutes. Then a burst of
	// four messages inside the last 8 seconds (plus the request being
	// scanned = 5 in 10s = 30 req/min ≈ 30x the ~1/min baseline).
	seedSession(t, store, "s1", now,
		2*time.Second, 4*time.Second, 6*time.Second, 8*time.Second,
		70*time.Second, 2*time.Minute, 3*time.Minute, 4*time.Minute, 5*time.Minute,
	)
	d := newTestRateAnomaly(store, now)

	findings, err := d.DetectWithSession(context.Background(), "s1", "hello", 0)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.DetectorName != "rate_anomaly" {
		t.Errorf("detector name = %q", f.DetectorName)
	}
	if f.ThreatType != security.ThreatAnomaly {
		t.Errorf("threat type = %q", f.ThreatType)
	}
	if f.SubCategory != "rate.burst" {
		t.Errorf("sub category = %q", f.SubCategory)
	}
	if f.Action != security.ActionWarn {
		t.Errorf("action = %q, want warn", f.Action)
	}
}

func TestRateAnomalyNewSessionNeverFires(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	store := session.NewMemoryStore()
	// Two rapid-fire messages on a brand-new session — not enough
	// history to establish a baseline, so it must stay quiet even
	// though the instantaneous rate is huge.
	seedSession(t, store, "s1", now, 1*time.Second, 2*time.Second)
	d := newTestRateAnomaly(store, now)

	findings, err := d.DetectWithSession(context.Background(), "s1", "hello", 0)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("new session fired: %+v", findings)
	}
}

func TestRateAnomalyAllBurstSessionNeverFires(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	store := session.NewMemoryStore()
	// Six messages all inside the burst window: there is no trailing
	// baseline to compare against, so no finding.
	seedSession(t, store, "s1", now,
		1*time.Second, 2*time.Second, 3*time.Second,
		4*time.Second, 5*time.Second, 6*time.Second,
	)
	d := newTestRateAnomaly(store, now)

	findings, err := d.DetectWithSession(context.Background(), "s1", "hello", 0)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("all-burst session fired: %+v", findings)
	}
}

func TestRateAnomalyNoOps(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	var nilDet *RateAnomalyDetector
	if f, err := nilDet.DetectWithSession(context.Background(), "s1", "x", 0); err != nil || f != nil {
		t.Fatalf("nil detector: %v %v", f, err)
	}

	d := newTestRateAnomaly(nil, now)
	if f, err := d.DetectWithSession(context.Background(), "s1", "x", 0); err != nil || f != nil {
		t.Fatalf("nil store: %v %v", f, err)
	}

	d = newTestRateAnomaly(session.NewMemoryStore(), now)
	if f, err := d.DetectWithSession(context.Background(), "", "x", 0); err != nil || f != nil {
		t.Fatalf("empty session id: %v %v", f, err)
	}
}

func TestRateAnomalyEnabledFor(t *testing.T) {
	d := NewRateAnomalyDetector(session.NewMemoryStore(), RateAnomalyConfig{})
	if d.EnabledFor(nil) {
		t.Error("nil request should be disabled")
	}
	if d.EnabledFor(&security.ScanRequest{}) {
		t.Error("zero-value request should be disabled (default off)")
	}
	if !d.EnabledFor(&security.ScanRequest{RateAnomalyEnabled: true}) {
		t.Error("opted-in request should be enabled")
	}
}

// TestEngineRunsMultipleSessionDetectors exercises the engine-side
// change: SetSessionAware keeps working, AddSessionDetector appends,
// gated detectors only run when the request opts in, and warn-level
// session findings lift the result action.
func TestEngineRunsMultipleSessionDetectors(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	store := session.NewMemoryStore()
	seedSession(t, store, "burst-session", now,
		2*time.Second, 4*time.Second, 6*time.Second, 8*time.Second,
		70*time.Second, 2*time.Minute, 3*time.Minute, 4*time.Minute, 5*time.Minute,
	)

	engine := security.NewEngine() // no content detectors needed
	engine.SetSessionAware(store, NewCrescendoDetector(store))
	rate := newTestRateAnomaly(store, now)
	engine.AddSessionDetector(rate)

	// Gate respected: profile off → no rate finding despite the burst.
	res := engine.Scan(context.Background(), &security.ScanRequest{
		Content:   "what is a goroutine?",
		SessionID: "burst-session",
	})
	for _, f := range res.Findings {
		if f.DetectorName == "rate_anomaly" {
			t.Fatalf("rate_anomaly ran while profile-gated off: %+v", f)
		}
	}

	// Opted in → burst flagged and action lifted to warn. Re-seed the
	// session because the gated-off scan above appended its own entry.
	store2 := session.NewMemoryStore()
	seedSession(t, store2, "burst-session", now,
		2*time.Second, 4*time.Second, 6*time.Second, 8*time.Second,
		70*time.Second, 2*time.Minute, 3*time.Minute, 4*time.Minute, 5*time.Minute,
	)
	engine2 := security.NewEngine()
	engine2.SetSessionAware(store2, NewCrescendoDetector(store2))
	rate2 := NewRateAnomalyDetector(store2, DefaultRateAnomalyConfig())
	rate2.now = func() time.Time { return now }
	engine2.AddSessionDetector(rate2)

	res = engine2.Scan(context.Background(), &security.ScanRequest{
		Content:            "what is a goroutine?",
		SessionID:          "burst-session",
		RateAnomalyEnabled: true,
	})
	found := false
	for _, f := range res.Findings {
		if f.DetectorName == "rate_anomaly" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rate_anomaly finding missing: %+v", res.Findings)
	}
	if res.Action != security.ActionWarn {
		t.Errorf("action = %q, want warn", res.Action)
	}
	if res.ShouldBlock {
		t.Error("warn-level anomaly must not block")
	}
}
