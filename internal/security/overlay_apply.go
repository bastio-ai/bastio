package security

import (
	"log/slog"
	"strconv"

	"github.com/bastio-ai/bastio/internal/security/overlay"
)

// ApplyOverlay returns a new Profile built by layering an overlay
// snapshot on top of base. base is unchanged.
//
// Semantics:
//   - Detector overrides: override threshold and/or strategy when
//     non-nil; inherit base when nil.
//   - PII override: override Action when non-nil.
//   - Additional patterns and access rules: captured in the snapshot
//     for audit / UI. Runtime enforcement wiring lives in the topic
//     detector and is deferred to Phase 2.
//   - Plugin detectors: Phase 3; no runtime effect in this phase.
//
// Loosening an override (raising a threshold, weakening a strategy)
// is permitted but emits a WARN log with overlay identity so operators
// notice in their normal log stream without needing to open the UI.
//
// After merge, the Input/Output step lists are regenerated from the
// updated flat fields so the engine's step-based pipeline reflects the
// merged thresholds and strategies.
func ApplyOverlay(base *Profile, snap *overlay.OverlaySnapshot, ident overlay.Identity) *Profile {
	if base == nil {
		return nil
	}
	out := *base
	if snap == nil {
		return &out
	}

	if snap.DetectorOverrides.Injection != nil {
		applyDetectorOverride(
			"injection", snap.DetectorOverrides.Injection, ident,
			&out.InjectionThreshold, &out.InjectionStrategy,
			base.InjectionThreshold, base.InjectionStrategy,
		)
	}
	if snap.DetectorOverrides.Jailbreak != nil {
		applyDetectorOverride(
			"jailbreak", snap.DetectorOverrides.Jailbreak, ident,
			&out.JailbreakThreshold, &out.JailbreakStrategy,
			base.JailbreakThreshold, base.JailbreakStrategy,
		)
	}
	if snap.DetectorOverrides.Secrets != nil {
		applyStrategyOnly(
			"secrets", snap.DetectorOverrides.Secrets, ident,
			&out.SecretsStrategy, base.SecretsStrategy,
		)
	}
	if snap.DetectorOverrides.IndirectInjection != nil {
		applyStrategyOnly(
			"indirect_injection", snap.DetectorOverrides.IndirectInjection, ident,
			&out.IndirectInjectionStrategy, base.IndirectInjectionStrategy,
		)
	}
	if snap.DetectorOverrides.OutputExfil != nil {
		applyStrategyOnly(
			"output_exfil", snap.DetectorOverrides.OutputExfil, ident,
			&out.OutputExfilStrategy, base.OutputExfilStrategy,
		)
	}
	if snap.DetectorOverrides.PII != nil && snap.DetectorOverrides.PII.Action != nil {
		newAction := Action(*snap.DetectorOverrides.PII.Action)
		if actionRank(newAction) < actionRank(base.PIIAction) {
			logLoosening(ident, "pii", "action",
				string(base.PIIAction), string(newAction))
		}
		out.PIIAction = newAction
	}

	// Step lists are derived from flat fields. Regenerate so the engine
	// sees updated thresholds / strategies at runtime.
	out.Input, out.Output = StepsFromLegacyProfile(out)
	return &out
}

func applyDetectorOverride(
	detector string,
	ov *overlay.DetectorOverride,
	ident overlay.Identity,
	outThreshold *float64,
	outStrategy *Action,
	baseThreshold float64,
	baseStrategy Action,
) {
	if ov.Threshold != nil {
		if *ov.Threshold > baseThreshold {
			// Higher threshold = less sensitive = looser.
			logLoosening(ident, detector, "threshold",
				fmtFloat(baseThreshold), fmtFloat(*ov.Threshold))
		}
		*outThreshold = *ov.Threshold
	}
	if ov.Strategy != nil {
		newStrat := Action(*ov.Strategy)
		if actionRank(newStrat) < actionRank(baseStrategy) {
			logLoosening(ident, detector, "strategy",
				string(baseStrategy), string(newStrat))
		}
		*outStrategy = newStrat
	}
}

func applyStrategyOnly(
	detector string,
	ov *overlay.DetectorOverride,
	ident overlay.Identity,
	outStrategy *Action,
	baseStrategy Action,
) {
	if ov.Strategy == nil {
		return
	}
	newStrat := Action(*ov.Strategy)
	if actionRank(newStrat) < actionRank(baseStrategy) {
		logLoosening(ident, detector, "strategy",
			string(baseStrategy), string(newStrat))
	}
	*outStrategy = newStrat
}

// actionRank orders security actions by how strictly they handle a
// match, lowest-to-highest. Used to decide whether an override is a
// loosening. Unknown actions rank as ActionWarn so a typo doesn't
// silently mask a loosening warning.
func actionRank(a Action) int {
	switch a {
	case ActionBlock:
		return 4
	case ActionTokenize:
		return 3
	case ActionMask:
		return 2
	case ActionWarn:
		return 1
	case ActionLogOnly, ActionPass:
		return 0
	}
	return 1
}

func logLoosening(ident overlay.Identity, detector, field, from, to string) {
	slog.Warn("overlay override loosens security",
		"overlay_id", ident.OverlayID,
		"version_id", ident.VersionID,
		"version", ident.Version,
		"detector", detector,
		"field", field,
		"from", from,
		"to", to,
	)
}

// fmtFloat formats a threshold for log lines using the shortest
// round-trippable representation.
func fmtFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
