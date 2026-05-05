package security

import (
	"fmt"
	"strconv"

	"github.com/bastio-ai/bastio/internal/security/overlay"
)

// NewOverlayWarningAnalyzer returns a WarningAnalyzer (overlay-package
// function type) that computes loosening warnings by comparing a
// snapshot's overrides to the shipped DefaultProfile. Hands the result
// off to the overlay HTTP handler via SetWarningAnalyzer so UI
// responses include a "warnings" field for overrides that weaken
// security.
//
// This lives in the security package because it needs Profile and the
// actionRank helper; overlay is deliberately kept independent of
// security to avoid an import cycle. The analyzer is injected at
// server construction time so overlay/handler stays decoupled.
func NewOverlayWarningAnalyzer() overlay.WarningAnalyzer {
	base := DefaultProfile()
	return func(snap *overlay.OverlaySnapshot) []overlay.Warning {
		if snap == nil {
			return nil
		}
		var out []overlay.Warning

		// Helpers factor out the two shapes: threshold+strategy
		// detectors and strategy-only detectors.
		addThresholdStrategy := func(detector string, ov *overlay.DetectorOverride, baseT float64, baseS Action) {
			if ov == nil {
				return
			}
			if ov.Threshold != nil && *ov.Threshold > baseT {
				out = append(out, overlay.Warning{
					Detector: detector,
					Field:    "threshold",
					From:     strconv.FormatFloat(baseT, 'f', -1, 64),
					To:       strconv.FormatFloat(*ov.Threshold, 'f', -1, 64),
					Message: fmt.Sprintf(
						"%s threshold raised from %s to %s — detector becomes less sensitive.",
						detector,
						strconv.FormatFloat(baseT, 'f', -1, 64),
						strconv.FormatFloat(*ov.Threshold, 'f', -1, 64),
					),
				})
			}
			if ov.Strategy != nil {
				newStrat := Action(*ov.Strategy)
				if actionRank(newStrat) < actionRank(baseS) {
					out = append(out, overlay.Warning{
						Detector: detector,
						Field:    "strategy",
						From:     string(baseS),
						To:       *ov.Strategy,
						Message: fmt.Sprintf(
							"%s strategy weakened from %s to %s.",
							detector, baseS, *ov.Strategy,
						),
					})
				}
			}
		}
		addStrategyOnly := func(detector string, ov *overlay.DetectorOverride, baseS Action) {
			if ov == nil || ov.Strategy == nil {
				return
			}
			newStrat := Action(*ov.Strategy)
			if actionRank(newStrat) < actionRank(baseS) {
				out = append(out, overlay.Warning{
					Detector: detector,
					Field:    "strategy",
					From:     string(baseS),
					To:       *ov.Strategy,
					Message: fmt.Sprintf(
						"%s strategy weakened from %s to %s.",
						detector, baseS, *ov.Strategy,
					),
				})
			}
		}

		o := snap.DetectorOverrides
		addThresholdStrategy("injection", o.Injection, base.InjectionThreshold, base.InjectionStrategy)
		addThresholdStrategy("jailbreak", o.Jailbreak, base.JailbreakThreshold, base.JailbreakStrategy)
		addStrategyOnly("secrets", o.Secrets, base.SecretsStrategy)
		addStrategyOnly("indirect_injection", o.IndirectInjection, base.IndirectInjectionStrategy)
		addStrategyOnly("output_exfil", o.OutputExfil, base.OutputExfilStrategy)

		if o.PII != nil && o.PII.Action != nil {
			newAction := Action(*o.PII.Action)
			if actionRank(newAction) < actionRank(base.PIIAction) {
				out = append(out, overlay.Warning{
					Detector: "pii",
					Field:    "action",
					From:     string(base.PIIAction),
					To:       *o.PII.Action,
					Message: fmt.Sprintf(
						"pii action weakened from %s to %s.",
						base.PIIAction, *o.PII.Action,
					),
				})
			}
		}

		return out
	}
}
