package security_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/overlay"
)

func baseProfile() *security.Profile {
	p := security.DefaultProfile()
	return &p
}

func ident() overlay.Identity {
	return overlay.Identity{
		OverlayID: uuid.New(),
		VersionID: uuid.New(),
		Version:   1,
	}
}

func TestApplyOverlay_NilSnapshotReturnsCopyOfBase(t *testing.T) {
	base := baseProfile()
	got := security.ApplyOverlay(base, nil, ident())
	if got == base {
		t.Fatalf("Apply must not return the same pointer as base")
	}
	if got.InjectionThreshold != base.InjectionThreshold {
		t.Fatalf("threshold diverged: base=%v got=%v", base.InjectionThreshold, got.InjectionThreshold)
	}
	if got.InjectionStrategy != base.InjectionStrategy {
		t.Fatalf("strategy diverged: base=%v got=%v", base.InjectionStrategy, got.InjectionStrategy)
	}
}

func TestApplyOverlay_ThresholdOverrideTightening(t *testing.T) {
	base := baseProfile()
	baseT := base.InjectionThreshold
	tighter := baseT - 0.1
	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		DetectorOverrides: overlay.DetectorOverrides{
			Injection: &overlay.DetectorOverride{Threshold: &tighter},
		},
	}
	got := security.ApplyOverlay(base, snap, ident())
	if got.InjectionThreshold != tighter {
		t.Fatalf("threshold override not applied: want %v got %v", tighter, got.InjectionThreshold)
	}
	// Base must be unchanged — Apply is pure.
	if base.InjectionThreshold != baseT {
		t.Fatalf("base mutated: before=%v after=%v", baseT, base.InjectionThreshold)
	}
}

func TestApplyOverlay_ThresholdOverrideLooseningStillApplies(t *testing.T) {
	// Loosening is permitted (logged by merge, not rejected). The
	// new value must still end up on the profile.
	base := baseProfile()
	looser := base.InjectionThreshold + 0.1
	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		DetectorOverrides: overlay.DetectorOverrides{
			Injection: &overlay.DetectorOverride{Threshold: &looser},
		},
	}
	got := security.ApplyOverlay(base, snap, ident())
	if got.InjectionThreshold != looser {
		t.Fatalf("threshold override not applied on loosening: want %v got %v", looser, got.InjectionThreshold)
	}
}

func TestApplyOverlay_StrategyOverrideAndInheritance(t *testing.T) {
	base := baseProfile()
	base.JailbreakStrategy = security.ActionBlock
	baseInjStrat := base.InjectionStrategy
	s := "warn"
	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		DetectorOverrides: overlay.DetectorOverrides{
			// Only override jailbreak; injection should inherit.
			Jailbreak: &overlay.DetectorOverride{Strategy: &s},
		},
	}
	got := security.ApplyOverlay(base, snap, ident())
	if got.JailbreakStrategy != security.ActionWarn {
		t.Fatalf("jailbreak strategy override not applied: got %v", got.JailbreakStrategy)
	}
	if got.InjectionStrategy != baseInjStrat {
		t.Fatalf("injection strategy must inherit from base: want %v got %v", baseInjStrat, got.InjectionStrategy)
	}
}

func TestApplyOverlay_PIIActionOverride(t *testing.T) {
	base := baseProfile()
	base.PIIAction = security.ActionMask
	blockStr := "block"
	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		DetectorOverrides: overlay.DetectorOverrides{
			PII: &overlay.PIIOverride{Action: &blockStr},
		},
	}
	got := security.ApplyOverlay(base, snap, ident())
	if got.PIIAction != security.ActionBlock {
		t.Fatalf("pii action override not applied: got %v", got.PIIAction)
	}
}

func TestApplyOverlay_OnlyNonNilOverridesChangeBase(t *testing.T) {
	base := baseProfile()
	baseJailbreakThresh := base.JailbreakThreshold
	baseJailbreakStrat := base.JailbreakStrategy
	tighter := base.InjectionThreshold - 0.1
	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		DetectorOverrides: overlay.DetectorOverrides{
			Injection: &overlay.DetectorOverride{Threshold: &tighter},
			// Jailbreak has no override — must not change.
		},
	}
	got := security.ApplyOverlay(base, snap, ident())
	if got.JailbreakThreshold != baseJailbreakThresh {
		t.Fatalf("unrelated jailbreak threshold changed: base=%v got=%v", baseJailbreakThresh, got.JailbreakThreshold)
	}
	if got.JailbreakStrategy != baseJailbreakStrat {
		t.Fatalf("unrelated jailbreak strategy changed: base=%v got=%v", baseJailbreakStrat, got.JailbreakStrategy)
	}
}

func TestApplyOverlay_StepsRegeneratedFromMergedFields(t *testing.T) {
	// Merging updated threshold must show up in the Input step list so
	// the engine uses the new value. StepsFromLegacyProfile derives the
	// steps from the flat fields; if Apply forgets to re-derive, the
	// steps still carry the pre-merge threshold.
	base := baseProfile()
	tighter := 0.1
	snap := &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		DetectorOverrides: overlay.DetectorOverrides{
			Injection: &overlay.DetectorOverride{Threshold: &tighter},
		},
	}
	got := security.ApplyOverlay(base, snap, ident())

	if len(got.Input) == 0 {
		t.Fatalf("merged profile has no Input steps — Apply did not regenerate them")
	}
	// Find the injection step and confirm it carries the new threshold.
	found := false
	for _, st := range got.Input {
		if st.Detector == "injection" {
			found = true
			if st.Threshold != tighter {
				t.Fatalf("injection step threshold not updated: want %v got %v", tighter, st.Threshold)
			}
		}
	}
	if !found {
		t.Fatalf("injection step not present in regenerated Input list")
	}
}

func TestApplyOverlay_NilBaseReturnsNil(t *testing.T) {
	if got := security.ApplyOverlay(nil, nil, ident()); got != nil {
		t.Fatalf("Apply with nil base must return nil, got %+v", got)
	}
}
