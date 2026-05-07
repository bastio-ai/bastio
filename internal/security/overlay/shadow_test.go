package overlay_test

import (
	"testing"

	"github.com/bastio-ai/bastio/internal/security/overlay"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name         string
		activeAction string
		shadowAction string
		activeBlock  bool
		shadowBlock  bool
		wantDiv      string
		wantOK       bool
	}{
		{
			name:         "identical pass — no event",
			activeAction: "pass", shadowAction: "pass",
		},
		{
			name:         "identical block — no event",
			activeAction: "block", shadowAction: "block",
			activeBlock: true, shadowBlock: true,
		},
		{
			name:         "primary blocks, shadow doesn't — would_allow (highest severity)",
			activeAction: "block", shadowAction: "warn",
			activeBlock: true, shadowBlock: false,
			wantDiv: overlay.DivergenceWouldAllow, wantOK: true,
		},
		{
			name:         "primary passes, shadow blocks — would_block",
			activeAction: "warn", shadowAction: "block",
			activeBlock: false, shadowBlock: true,
			wantDiv: overlay.DivergenceWouldBlock, wantOK: true,
		},
		{
			name:         "both non-blocking but different actions — threshold_diff",
			activeAction: "warn", shadowAction: "log_only",
			activeBlock: false, shadowBlock: false,
			wantDiv: overlay.DivergenceThresholdDiff, wantOK: true,
		},
		{
			name:         "both block but action labels differ — threshold_diff",
			activeAction: "block", shadowAction: "mask",
			// Artificial: this shouldn't happen in practice, but the
			// comparator must be total over action strings, and when
			// both flags say "should block" the divergence is in the
			// action label only.
			activeBlock: true, shadowBlock: true,
			wantDiv: overlay.DivergenceThresholdDiff, wantOK: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			div, ok := overlay.Classify(tc.activeAction, tc.shadowAction, tc.activeBlock, tc.shadowBlock)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if div != tc.wantDiv {
				t.Fatalf("divergence=%q want %q", div, tc.wantDiv)
			}
		})
	}
}
