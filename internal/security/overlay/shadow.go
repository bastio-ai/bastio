package overlay

// Classify decides whether a primary-vs-shadow engine result pair
// represents a divergence worth recording, and if so which category.
//
// The categories map onto the `divergence` column on
// tenant_policy_overlay_shadow_events:
//
//   - would_block: primary didn't block, shadow did — shadow is
//     stricter on this traffic. Operators promoting this shadow
//     should expect an increase in blocks.
//
//   - would_allow: primary blocked, shadow didn't — shadow is more
//     permissive. Highest-risk category; promoting drops protection.
//     UI should surface these prominently.
//
//   - threshold_diff: same block-ness but different action (e.g.
//     primary=warn, shadow=log_only). Worth observing but lower
//     severity.
//
// Returns ("", false) when results match and no event should be written.
// Pure function — no DB, no I/O — so it's cheap to call on every request.
func Classify(activeAction, shadowAction string, activeBlock, shadowBlock bool) (string, bool) {
	if activeBlock != shadowBlock {
		if shadowBlock {
			return DivergenceWouldBlock, true
		}
		return DivergenceWouldAllow, true
	}
	if activeAction != shadowAction {
		return DivergenceThresholdDiff, true
	}
	return "", false
}
