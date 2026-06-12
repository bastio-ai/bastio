package detection

import (
	"context"
	"fmt"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/session"
)

// RateAnomalyDetector flags request-rate bursts within a session.
// Automated abuse (prompt-spraying, exfil loops, brute-force jailbreak
// search) tends to arrive much faster than a human typing; a session
// that suddenly runs at many times its own trailing rate is worth a
// warn even when every individual message looks benign.
//
// Pure arithmetic over the timestamps already kept in the session
// buffer — no ML, no external calls, no extra storage. Like Crescendo
// it fails open: store errors or missing history produce no finding.
//
// The detector is profile-gated (GatedSessionDetector): it only runs
// when the scan request carries RateAnomalyEnabled, which the gateway
// sets from the profile's rate_anomaly_enabled column. Default off.
type RateAnomalyDetector struct {
	store session.Store
	cfg   RateAnomalyConfig
	// now is injectable for tests; defaults to time.Now.
	now func() time.Time
}

// RateAnomalyConfig tunes the burst heuristic. Zero values fall back
// to the defaults documented on each field.
type RateAnomalyConfig struct {
	// BurstWindow is the sliding window the current rate is measured
	// over. Default 10s.
	BurstWindow time.Duration
	// BaselineMultiplier is how many times the session's own trailing
	// rate the burst must exceed before flagging. Default 5.
	BaselineMultiplier float64
	// FloorPerMinute is the absolute minimum burst rate (requests per
	// minute) below which the detector never fires, regardless of the
	// baseline ratio. Keeps slow sessions from flagging on tiny
	// denominators. Default 10.
	FloorPerMinute float64
	// MinPriorTurns is the minimum number of session entries OUTSIDE
	// the burst window required to establish a baseline. Brand-new
	// sessions (and sessions that are 100% burst, which have no
	// trailing rate to compare against) never fire. Default 4.
	MinPriorTurns int
}

// DefaultRateAnomalyConfig returns the documented defaults.
func DefaultRateAnomalyConfig() RateAnomalyConfig {
	return RateAnomalyConfig{
		BurstWindow:        10 * time.Second,
		BaselineMultiplier: 5,
		FloorPerMinute:     10,
		MinPriorTurns:      4,
	}
}

// NewRateAnomalyDetector wires the detector to a session store. Pass
// nil to disable — all Detect calls become no-ops. Zero-valued config
// fields are replaced with defaults.
func NewRateAnomalyDetector(store session.Store, cfg RateAnomalyConfig) *RateAnomalyDetector {
	def := DefaultRateAnomalyConfig()
	if cfg.BurstWindow <= 0 {
		cfg.BurstWindow = def.BurstWindow
	}
	if cfg.BaselineMultiplier <= 0 {
		cfg.BaselineMultiplier = def.BaselineMultiplier
	}
	if cfg.FloorPerMinute <= 0 {
		cfg.FloorPerMinute = def.FloorPerMinute
	}
	if cfg.MinPriorTurns <= 0 {
		cfg.MinPriorTurns = def.MinPriorTurns
	}
	return &RateAnomalyDetector{store: store, cfg: cfg, now: time.Now}
}

// EnabledFor implements security.GatedSessionDetector — the engine
// skips this detector unless the profile opted in.
func (d *RateAnomalyDetector) EnabledFor(req *security.ScanRequest) bool {
	return req != nil && req.RateAnomalyEnabled
}

// DetectWithSession implements security.SessionDetector. content and
// currentScore are unused — this detector reasons about timing only.
func (d *RateAnomalyDetector) DetectWithSession(
	ctx context.Context,
	sessionID string,
	_ string,
	_ float64,
) ([]security.Finding, error) {
	if d == nil || d.store == nil || sessionID == "" {
		return nil, nil
	}
	history, err := d.store.Recent(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}

	now := d.now().UTC()

	// Split prior turns into the burst window (recent) and the
	// baseline (everything older). The request being scanned right
	// now is always part of the burst.
	burstCount := 1
	baselineCount := 0
	var oldestBaseline time.Time
	for _, e := range history {
		age := now.Sub(e.At)
		if age < 0 {
			age = 0 // tolerate small clock skew between writers
		}
		if age <= d.cfg.BurstWindow {
			burstCount++
			continue
		}
		baselineCount++
		if oldestBaseline.IsZero() || e.At.Before(oldestBaseline) {
			oldestBaseline = e.At
		}
	}

	// No established baseline → never fire. Covers brand-new sessions
	// (the "first messages" case) and all-burst sessions where the
	// trailing average is the burst itself.
	if baselineCount < d.cfg.MinPriorTurns {
		return nil, nil
	}

	burstRate := float64(burstCount) / d.cfg.BurstWindow.Minutes()
	if burstRate < d.cfg.FloorPerMinute {
		return nil, nil
	}

	baselineSpan := now.Add(-d.cfg.BurstWindow).Sub(oldestBaseline)
	if baselineSpan <= 0 {
		return nil, nil
	}
	baselineRate := float64(baselineCount) / baselineSpan.Minutes()
	if baselineRate <= 0 {
		return nil, nil
	}

	if burstRate < d.cfg.BaselineMultiplier*baselineRate {
		return nil, nil
	}

	return []security.Finding{{
		ThreatType:     security.ThreatAnomaly,
		DetectorName:   "rate_anomaly",
		Severity:       security.SeverityMedium,
		Score:          0.65,
		Confidence:     0.85,
		SubCategory:    "rate.burst",
		MatchedPattern: "session_rate_burst",
		MatchedContent: fmt.Sprintf("burst %.0f req/min vs session baseline %.1f req/min", burstRate, baselineRate),
		Action:         security.ActionWarn,
		Message:        "request rate anomaly: session burst exceeds its own trailing baseline",
	}}, nil
}
