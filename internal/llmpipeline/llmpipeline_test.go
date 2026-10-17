package llmpipeline

import (
	"context"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
)

func TestOutputUsesOutputPoliciesAndEmptyProfileDoesNotReenableDetectors(t *testing.T) {
	engine := security.NewEngine(detection.NewPIIDetector(), detection.NewInjectionDetector())
	empty := &security.Profile{}
	scan := PreflightScan(context.Background(), PreflightOptions{Engine: engine, Profile: empty, Content: "SSN 123-45-6789"})
	if scan.ShouldBlock || len(scan.Findings) != 0 {
		t.Fatal("empty profile enabled legacy detectors")
	}
	profile := security.DefaultProfile()
	profile.PIIScanResponse = false
	profile.SecretsEnabled = false
	profile.OutputExfilEnabled = false
	scan = PreflightScan(context.Background(), PreflightOptions{Engine: engine, Profile: &profile, Content: "SSN 123-45-6789", Output: true})
	if scan.ShouldBlock || len(scan.Findings) != 0 {
		t.Fatal("input detectors ran against disabled output policy")
	}
	profile.PIIScanResponse = true
	profile.PIIAction = security.ActionBlock
	scan = PreflightScan(context.Background(), PreflightOptions{Engine: engine, Profile: &profile, Content: "SSN 123-45-6789", Output: true})
	if !scan.ShouldBlock {
		t.Fatal("configured output block ignored")
	}
}
