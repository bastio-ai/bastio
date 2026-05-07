package security

import (
	"context"
	"testing"
)

type fakeDetector struct {
	name     string
	findings []Finding
	mask     func(string) string
}

func (f *fakeDetector) Name() string { return f.name }
func (f *fakeDetector) Detect(_ context.Context, _ string) ([]Finding, error) {
	return f.findings, nil
}
func (f *fakeDetector) Mask(s string) string {
	if f.mask == nil {
		return s
	}
	return f.mask(s)
}
func (f *fakeDetector) TokenizeInto(s string, _ *TokenMap) string { return s }

func TestRunSteps_BlockShortCircuits(t *testing.T) {
	engine := NewEngine(
		&fakeDetector{name: "injection", findings: []Finding{{
			ThreatType: ThreatInjection, Score: 0.9, Confidence: 0.95, Action: ActionBlock,
		}}},
		&fakeDetector{name: "pii", findings: []Finding{{
			ThreatType: ThreatPII, Score: 0.9, Confidence: 0.9, Action: ActionMask,
		}}, mask: func(s string) string { return "[MASKED]" }},
	)

	res := engine.RunSteps(context.Background(), "payload", []Step{
		{Detector: "injection", Strategy: ActionBlock},
		{Detector: "pii", Strategy: ActionMask},
	}, nil)

	if !res.ShouldBlock {
		t.Fatal("expected block")
	}
	if res.Action != ActionBlock {
		t.Errorf("want block, got %s", res.Action)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("expected blocking step + one skipped step, got %d", len(res.Steps))
	}
	if !res.Steps[0].Fired || res.Steps[0].Action != ActionBlock {
		t.Errorf("first step should be the fired block, got %+v", res.Steps[0])
	}
	if !res.Steps[1].Skipped || res.Steps[1].Detector != "pii" {
		t.Errorf("remaining step should be skipped pii, got %+v", res.Steps[1])
	}
}

func TestRunSteps_SequentialRewrite(t *testing.T) {
	engine := NewEngine(
		&fakeDetector{
			name:     "pii",
			findings: []Finding{{ThreatType: ThreatPII, Score: 0.9, Confidence: 0.9}},
			mask:     func(s string) string { return "[MASKED]" },
		},
	)

	res := engine.RunSteps(context.Background(), "email me at a@b.com", []Step{
		{Detector: "pii", Strategy: ActionMask},
	}, nil)

	if res.SanitizedContent != "[MASKED]" {
		t.Errorf("expected rewrite, got %q", res.SanitizedContent)
	}
	if res.Action != ActionMask {
		t.Errorf("want mask, got %s", res.Action)
	}
}

func TestRunSteps_ThresholdGatesStrategy(t *testing.T) {
	engine := NewEngine(&fakeDetector{
		name:     "injection",
		findings: []Finding{{ThreatType: ThreatInjection, Score: 0.5, Confidence: 0.8}},
	})

	// weighted score = 0.4; threshold 0.72 should suppress the block.
	res := engine.RunSteps(context.Background(), "payload", []Step{
		{Detector: "injection", Strategy: ActionBlock, Threshold: 0.72},
	}, nil)

	if res.ShouldBlock {
		t.Fatal("expected no block below threshold")
	}
	if res.Steps[0].Fired {
		t.Error("expected step not to fire")
	}
}

func TestRunSteps_UnknownDetectorFailsOpen(t *testing.T) {
	engine := NewEngine()
	res := engine.RunSteps(context.Background(), "hello", []Step{
		{Detector: "nope", Strategy: ActionBlock},
	}, nil)
	if res.ShouldBlock {
		t.Error("missing detector must not block traffic")
	}
}

func TestValidateSteps(t *testing.T) {
	tests := []struct {
		name    string
		steps   []Step
		wantErr bool
	}{
		{"valid", []Step{{Detector: "pii", Strategy: ActionMask}}, false},
		{"empty detector", []Step{{Strategy: ActionBlock}}, true},
		{"bad strategy", []Step{{Detector: "pii", Strategy: Action("nope")}}, true},
		{"bad threshold", []Step{{Detector: "pii", Strategy: ActionMask, Threshold: 1.5}}, true},
		{"empty strategy ok", []Step{{Detector: "pii"}}, false},
	}
	for _, tt := range tests {
		err := ValidateSteps(tt.steps)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}

func TestDefaultProfile_HasSteps(t *testing.T) {
	p := DefaultProfile()
	if len(p.Input) == 0 {
		t.Error("default profile missing input steps")
	}
	if len(p.Output) == 0 {
		t.Error("default profile missing output steps")
	}
}

func TestStepsFromLegacyProfile_IncludesInjectionAndJailbreak(t *testing.T) {
	p := Profile{
		InjectionEnabled: true,
		JailbreakEnabled: true,
		PIIEnabled:       true,
		PIIAction:        ActionMask,
		PIIScanResponse:  true,
	}
	input, output := StepsFromLegacyProfile(p)

	detectors := map[string]Action{}
	for _, s := range input {
		detectors[s.Detector] = s.Strategy
	}
	if detectors["injection"] != ActionBlock {
		t.Errorf("expected injection→block on input, got %v", detectors["injection"])
	}
	if detectors["jailbreak"] != ActionWarn {
		t.Errorf("expected jailbreak→warn on input, got %v", detectors["jailbreak"])
	}
	if detectors["pii"] != ActionMask {
		t.Errorf("expected pii→mask on input, got %v", detectors["pii"])
	}
	if len(output) != 1 || output[0].Detector != "pii" {
		t.Errorf("expected output to contain pii step, got %v", output)
	}
}

func TestStepsFromLegacyProfile_OrderInjectionBeforePII(t *testing.T) {
	p := Profile{
		InjectionEnabled: true,
		PIIEnabled:       true,
		PIIAction:        ActionMask,
	}
	input, _ := StepsFromLegacyProfile(p)
	if len(input) < 2 || input[0].Detector != "injection" {
		t.Fatalf("injection must be first so it short-circuits before PII rewrites: %v", input)
	}
}
