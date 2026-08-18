package security

import "testing"

func TestScopedSessionID(t *testing.T) {
	if got := scopedSessionID("", "abc"); got != "abc" {
		t.Fatalf("empty customer: %s", got)
	}
	if got := scopedSessionID("tenant-1", "abc"); got != "tenant-1:abc" {
		t.Fatalf("scoped: %s", got)
	}
}

func TestFindingSuppressed(t *testing.T) {
	list := []PatternSuppression{{Detector: "jailbreak", Pattern: "persona.dan"}}
	if !findingSuppressed(Finding{DetectorName: "jailbreak", MatchedPattern: "persona.dan"}, list) {
		t.Fatal("matched_pattern should suppress")
	}
	if !findingSuppressed(Finding{DetectorName: "Jailbreak", SubCategory: "persona.dan"}, list) {
		t.Fatal("subcategory should suppress")
	}
	if findingSuppressed(Finding{DetectorName: "jailbreak", MatchedPattern: "fiction_framing"}, list) {
		t.Fatal("other pattern must not suppress")
	}
	if findingSuppressed(Finding{DetectorName: "secrets", MatchedPattern: "persona.dan"}, list) {
		t.Fatal("other detector must not suppress")
	}
}

func TestValidateSuppression(t *testing.T) {
	if err := validateSuppression(createSuppressionRequest{Detector: "jailbreak", Pattern: "persona.dan"}); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := validateSuppression(createSuppressionRequest{Detector: "", Pattern: "x"}); err == nil {
		t.Fatal("empty detector must fail")
	}
	if err := validateSuppression(createSuppressionRequest{Detector: "not valid", Pattern: "x"}); err == nil {
		t.Fatal("invalid detector must fail")
	}
}

func TestFilterSuppressed_DropsBeforeStrategy(t *testing.T) {
	engine := NewEngine(&fakeDetector{name: "jailbreak", findings: []Finding{{
		DetectorName:   "jailbreak",
		ThreatType:     ThreatJailbreak,
		MatchedPattern: "fiction_framing",
		Score:          0.9,
		Confidence:     1,
		Action:         ActionBlock,
	}}})
	res := engine.RunSteps(t.Context(), "payload", []Step{
		{Detector: "jailbreak", Strategy: ActionBlock, Threshold: 0.5},
	}, &RunOptions{
		Suppressions: []PatternSuppression{{Detector: "jailbreak", Pattern: "fiction_framing"}},
	})
	if res.ShouldBlock {
		t.Fatal("suppressed finding must not block")
	}
	if len(res.Steps) != 1 || len(res.Steps[0].Findings) != 0 {
		t.Fatalf("expected no findings, got %+v", res.Steps)
	}
}

func TestScan_SuppressionsSkipFinding(t *testing.T) {
	engine := NewEngine(&mockDetector{name: "secrets", findings: []Finding{{
		DetectorName:   "secrets",
		ThreatType:     ThreatInjection,
		MatchedPattern: "aws_access_key",
		Score:          0.95,
		Confidence:     1,
		Action:         ActionBlock,
	}}})
	result := engine.Scan(t.Context(), &ScanRequest{
		Content: "payload",
		Suppressions: []PatternSuppression{
			{Detector: "secrets", Pattern: "aws_access_key"},
		},
	})
	if result.ShouldBlock {
		t.Fatal("suppressed secret must not block")
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
}
