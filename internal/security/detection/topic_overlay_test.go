package detection_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/bastio-ai/bastio/internal/security/overlay"
)

// TestTopicDetector_OverlayPatternsFire verifies that additional
// patterns carried on the request context via overlay.WithActive are
// compiled, matched, and surfaced as findings alongside any
// DB-provided rules.
//
// The detector is constructed with a nil DB so only the overlay path
// runs. Nil DB is a supported configuration — it's what the e2e
// tests use when they don't need per-customer patterns.
func TestTopicDetector_OverlayPatternsFire(t *testing.T) {
	d := detection.NewTopicPolicyDetector(nil, nil, 0)

	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		AdditionalPatterns: []overlay.PatternRule{
			{
				Name:        "internal_codename",
				PatternType: "keyword",
				Pattern:     "projectfoo",
				Action:      "block",
				Severity:    "high",
			},
		},
	}
	ident := overlay.Identity{
		OverlayID: uuid.New(),
		VersionID: uuid.New(),
		Version:   1,
	}
	ctx := overlay.WithActive(context.Background(), snap, ident)

	findings, err := d.Detect(ctx, "we should not mention projectfoo in this release")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.DetectorName != "topic_policy" {
		t.Fatalf("detector = %q, want topic_policy", f.DetectorName)
	}
	if f.MatchedPattern != "internal_codename" {
		t.Fatalf("matched pattern = %q, want internal_codename", f.MatchedPattern)
	}
	if f.Action != security.ActionBlock {
		t.Fatalf("action = %q, want block", f.Action)
	}
	if !strings.Contains(f.MatchedContent, "projectfoo") {
		t.Fatalf("matched content = %q, want to contain 'projectfoo'", f.MatchedContent)
	}
}

// TestTopicDetector_NoOverlayNoFindings confirms that a context
// without an attached overlay produces no findings from the overlay
// path (no panic, no accidental ctx lookup).
func TestTopicDetector_NoOverlayNoFindings(t *testing.T) {
	d := detection.NewTopicPolicyDetector(nil, nil, 0)
	findings, err := d.Detect(context.Background(), "any content")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

// TestTopicDetector_OverlayPatternCompileCached calls Detect twice
// with the same overlay version and asserts both calls produce the
// same finding. The sync.Map cache is an internal detail, so this
// is primarily a smoke test that the cached path doesn't regress.
func TestTopicDetector_OverlayPatternCompileCached(t *testing.T) {
	d := detection.NewTopicPolicyDetector(nil, nil, 0)

	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		AdditionalPatterns: []overlay.PatternRule{
			{Name: "k", PatternType: "keyword", Pattern: "secret-phrase", Action: "warn", Severity: "low"},
		},
	}
	ident := overlay.Identity{VersionID: uuid.New(), Version: 1}
	ctx := overlay.WithActive(context.Background(), snap, ident)

	for i := range 3 {
		findings, err := d.Detect(ctx, "this has secret-phrase in it")
		if err != nil {
			t.Fatalf("iter %d Detect: %v", i, err)
		}
		if len(findings) != 1 {
			t.Fatalf("iter %d: expected 1 finding, got %d", i, len(findings))
		}
	}
}

// TestTopicDetector_RegexPatternFires covers regex patterns from an
// overlay in addition to the simpler keyword form.
func TestTopicDetector_RegexPatternFires(t *testing.T) {
	d := detection.NewTopicPolicyDetector(nil, nil, 0)

	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		AdditionalPatterns: []overlay.PatternRule{
			{
				Name:        "env_secret",
				PatternType: "regex",
				Pattern:     `[A-Z]+_SECRET=`,
				Action:      "warn",
				Severity:    "high",
			},
		},
	}
	ident := overlay.Identity{VersionID: uuid.New(), Version: 1}
	ctx := overlay.WithActive(context.Background(), snap, ident)

	findings, err := d.Detect(ctx, "export API_SECRET=xyz")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}
