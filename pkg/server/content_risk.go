package server

import (
	"context"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
)

// ContentRiskScore is the module-boundary-safe summary of one security
// scan over a free-text excerpt. bastio-cloud (and any other consumer of
// the OSS module) cannot import internal/security directly — the Go
// toolchain rejects cross-module internal imports — so this exported
// facade carries the aggregate result across the pkg/server seam without
// leaking internal types.
type ContentRiskScore struct {
	// Score is the aggregate threat score, 0.0 (clean) to 1.0 (critical).
	Score float64
	// ThreatTypes lists the distinct threat categories that fired
	// (e.g. "injection", "pii", "jailbreak").
	ThreatTypes []string
	// Findings is the number of individual detector findings.
	Findings int
}

// ContentRiskScorer wraps a security engine built from the stateless rule
// detectors only — no TopicPolicy (needs a PG pool + tenant context) and
// no Crescendo (needs Redis session state). That makes it safe to run
// offline over stored excerpts: batch jobs, governance trace analysis,
// anything outside a live gateway request.
type ContentRiskScorer struct {
	engine *security.Engine
}

// NewContentRiskScorer constructs the offline excerpt scorer. Pattern
// compilation happens once here; the returned scorer is safe for
// concurrent use.
func NewContentRiskScorer() *ContentRiskScorer {
	return &ContentRiskScorer{engine: security.NewEngine(
		detection.NewInjectionDetector(),
		detection.NewPIIDetector(),
		detection.NewJailbreakDetector(),
		detection.NewSecretsDetector(),
		detection.NewIndirectInjectionDetector(),
		detection.NewExfilDetector(),
	)}
}

// Score runs every rule detector over content and returns the aggregate.
// Detection-only: no sanitization, no blocking decision, no session state.
func (s *ContentRiskScorer) Score(ctx context.Context, content string) ContentRiskScore {
	res := s.engine.Scan(ctx, &security.ScanRequest{
		Content:          content,
		PIIAction:        security.ActionLogOnly,
		SkipSanitization: true,
	})
	out := ContentRiskScore{Score: res.ThreatScore, Findings: len(res.Findings)}
	for _, t := range res.ThreatTypes {
		out.ThreatTypes = append(out.ThreatTypes, string(t))
	}
	return out
}
