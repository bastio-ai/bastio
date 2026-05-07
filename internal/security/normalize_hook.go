package security

import (
	"github.com/bastio-ai/bastio/internal/security/normalize"
)

// normalize_hook wires the Layer 1 preprocessing pass into the engine.
// Kept in its own file so engine.go's diff stays tightly scoped to the
// call-sites that matter.

// applyNormalize runs the preprocessing pipeline on req.Content and
// returns the Result. Secure-by-default: when the caller hasn't set
// Normalize options (zero value) we use normalize.DefaultOptions.
// DisableNormalize is the only way to bypass entirely.
func applyNormalize(req *ScanRequest) normalize.Result {
	if req.DisableNormalize {
		return normalize.Result{Original: req.Content, Normalized: req.Content}
	}
	opts := req.Normalize
	if opts == (normalize.Options{}) {
		opts = normalize.DefaultOptions()
	}
	return normalize.Normalize(req.Content, opts)
}

// normalizeFindings turns the flags that fired during preprocessing
// into findings. Benign flags (NFKC, whitespace collapse) are ignored
// — they happen on every non-ASCII prompt and would swamp analytics.
// Suspicious flags (invisibles, homoglyph, base64/ROT13/leet decode)
// become medium-severity Warn findings; the combination-bonus in
// jailbreak.go lifts them to block when they co-occur with another
// attack signal.
func normalizeFindings(nr normalize.Result) []Finding {
	if len(nr.Flags) == 0 {
		return nil
	}
	out := make([]Finding, 0, len(nr.Flags))
	for _, f := range nr.Flags {
		nf, ok := flagToFinding(f)
		if !ok {
			continue
		}
		out = append(out, nf)
	}
	return out
}

// mergeNormalizeIntoResult lifts the result's Action when a suspicious
// normalization flag fired but the steps path hadn't already decided
// to block. Never downgrades — a block from a regex detector stays a
// block even if only benign flags fired.
func mergeNormalizeIntoResult(r *ScanResult, nr normalize.Result) {
	if r.ShouldBlock {
		return
	}
	for _, f := range nr.Flags {
		if isSuspiciousFlag(f) && r.Action == ActionPass {
			r.Action = ActionWarn
			return
		}
	}
}

func isSuspiciousFlag(f normalize.Flag) bool {
	switch f {
	case normalize.FlagInvisiblesStripped,
		normalize.FlagHomoglyphFolded,
		normalize.FlagBase64Decoded,
		normalize.FlagROT13Decoded,
		normalize.FlagLeetFolded:
		return true
	}
	return false
}

// flagToFinding maps a normalization flag to a synthetic Finding.
// Returns ok=false for benign flags we don't report.
func flagToFinding(f normalize.Flag) (Finding, bool) {
	switch f {
	case normalize.FlagInvisiblesStripped:
		return Finding{
			ThreatType:     ThreatJailbreak,
			DetectorName:   "normalize",
			Severity:       SeverityHigh,
			Score:          0.85,
			Confidence:     0.90,
			SubCategory:    "encoding.invisibles",
			MatchedPattern: "invisibles.stripped",
			Action:         ActionWarn,
			Message:        "Invisible/bidi characters stripped — possible evasion attempt",
			Source:         "Unicode UAX #9 / UAX #31 guidance",
		}, true
	case normalize.FlagHomoglyphFolded:
		return Finding{
			ThreatType:     ThreatJailbreak,
			DetectorName:   "normalize",
			Severity:       SeverityHigh,
			Score:          0.80,
			Confidence:     0.88,
			SubCategory:    "encoding.homoglyph",
			MatchedPattern: "homoglyph.folded",
			Action:         ActionWarn,
			Message:        "Homoglyph characters folded — possible script-mixing attack",
			Source:         "Unicode Confusables (UAX #39)",
		}, true
	case normalize.FlagBase64Decoded:
		return Finding{
			ThreatType:     ThreatJailbreak,
			DetectorName:   "normalize",
			Severity:       SeverityMedium,
			Score:          0.65,
			Confidence:     0.75,
			SubCategory:    "encoding.base64",
			MatchedPattern: "decoded.base64",
			Action:         ActionWarn,
			Message:        "Base64-encoded prose detected — decoded for detection",
			Source:         "general encoding-smuggle taxonomy",
		}, true
	case normalize.FlagROT13Decoded:
		return Finding{
			ThreatType:     ThreatJailbreak,
			DetectorName:   "normalize",
			Severity:       SeverityHigh,
			Score:          0.80,
			Confidence:     0.85,
			SubCategory:    "encoding.rot13",
			MatchedPattern: "decoded.rot13",
			Action:         ActionWarn,
			Message:        "ROT13-encoded prose detected — decoded for detection",
			Source:         "general encoding-smuggle taxonomy",
		}, true
	case normalize.FlagLeetFolded:
		return Finding{
			ThreatType:     ThreatJailbreak,
			DetectorName:   "normalize",
			Severity:       SeverityMedium,
			Score:          0.65,
			Confidence:     0.75,
			SubCategory:    "encoding.leet",
			MatchedPattern: "decoded.leet",
			Action:         ActionWarn,
			Message:        "Leet-substituted prose detected — folded for detection",
			Source:         "general encoding-smuggle taxonomy",
		}, true
	}
	return Finding{}, false
}
