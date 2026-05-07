package security

import (
	"context"
	"testing"
)

// mockDetector returns configurable findings for testing the engine.
type mockDetector struct {
	name     string
	findings []Finding
}

func (d *mockDetector) Name() string { return d.name }
func (d *mockDetector) Detect(_ context.Context, _ string) ([]Finding, error) {
	return d.findings, nil
}

func TestEngine_Scan_NoFindings(t *testing.T) {
	engine := NewEngine(&mockDetector{name: "test", findings: nil})
	result := engine.Scan(context.Background(), &ScanRequest{Content: "hello"})

	if result.ShouldBlock {
		t.Error("expected no block on clean input")
	}
	if result.Action != ActionPass {
		t.Errorf("expected pass action, got %s", result.Action)
	}
	if result.ThreatScore != 0 {
		t.Errorf("expected 0 threat score, got %f", result.ThreatScore)
	}
}

func TestEngine_Scan_SingleHighThreat(t *testing.T) {
	engine := NewEngine(&mockDetector{
		name: "test",
		findings: []Finding{{
			ThreatType: ThreatInjection,
			Severity:   SeverityCritical,
			Score:      0.95,
			Confidence: 0.9,
			Action:     ActionBlock,
		}},
	})

	result := engine.Scan(context.Background(), &ScanRequest{Content: "malicious"})

	if !result.ShouldBlock {
		t.Error("expected block on critical threat")
	}
	if result.Action != ActionBlock {
		t.Errorf("expected block action, got %s", result.Action)
	}
}

func TestEngine_Scan_MultipleThreatsBoostScore(t *testing.T) {
	engine := NewEngine(
		&mockDetector{name: "det1", findings: []Finding{{
			ThreatType: ThreatInjection, Score: 0.6, Confidence: 0.8, Action: ActionWarn,
		}}},
		&mockDetector{name: "det2", findings: []Finding{{
			ThreatType: ThreatJailbreak, Score: 0.6, Confidence: 0.8, Action: ActionWarn,
		}}},
	)

	result := engine.Scan(context.Background(), &ScanRequest{Content: "attack"})

	// Two different threat types should add 0.1 combination bonus
	if result.ThreatScore < 0.5 {
		t.Errorf("expected boosted score, got %f", result.ThreatScore)
	}
	if len(result.ThreatTypes) != 2 {
		t.Errorf("expected 2 threat types, got %d", len(result.ThreatTypes))
	}
}

func TestEngine_Scan_EmptyContent(t *testing.T) {
	engine := NewEngine(&mockDetector{name: "test", findings: []Finding{{
		ThreatType: ThreatInjection, Score: 0.9, Confidence: 0.9, Action: ActionBlock,
	}}})

	result := engine.Scan(context.Background(), &ScanRequest{Content: ""})

	if len(result.Findings) != 0 {
		t.Error("expected no findings on empty content")
	}
}
