package security

import (
	"context"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/security/overlay"
)

// NewOverlayPreviewRunner returns an overlay.PreviewRunner that
// re-runs the detection engine on a set of user-supplied samples
// against two effective profiles — the candidate (the version the
// caller wants to preview) and the currently-active overlay for
// that tenant — and reports per-sample verdicts + a diff summary.
//
// Used by the dashboard "Preview impact" flow so operators can see
// "would X still be blocked under this candidate?" before clicking
// activate. Unlike shadow mode this is synchronous and explicit —
// no sampling, no async write. Safe for small batches (handler
// caps at 50).
//
// The runner needs the engine, the base-profile lookup, and the
// overlay loader so it can build both effective profiles. It
// deliberately does NOT read ClickHouse — trace-replay is a
// separate follow-up.
func NewOverlayPreviewRunner(engine *Engine, profiles ProfileLookup, loader *overlay.Loader) overlay.PreviewRunner {
	return &overlayPreviewRunner{engine: engine, profiles: profiles, loader: loader}
}

type overlayPreviewRunner struct {
	engine   *Engine
	profiles ProfileLookup
	loader   *overlay.Loader
}

func (r *overlayPreviewRunner) Preview(
	ctx context.Context,
	customerID uuid.UUID,
	proxyID uuid.UUID,
	candidate *overlay.OverlaySnapshot,
	samples []overlay.PreviewSample,
) (*overlay.PreviewResult, error) {
	// Base profile — required to build either effective profile.
	// Nil profiles lookup falls back to DefaultProfile to match the
	// behaviour of the detect / gateway paths.
	var base Profile
	if r.profiles != nil {
		p, err := r.profiles.GetDefault(ctx, customerID)
		if err != nil {
			// Preview is advisory — a base-profile load error falls
			// back to defaults rather than erroring the request.
			base = DefaultProfile()
		} else if p != nil {
			base = *p
		} else {
			base = DefaultProfile()
		}
	} else {
		base = DefaultProfile()
	}

	// Active overlay for the tenant; optional. An absence means the
	// "active effective" profile is just the base — valid for tenants
	// without an overlay yet.
	var activeSnap *overlay.OverlaySnapshot
	var activeIdent overlay.Identity
	if r.loader != nil {
		snap, ident, _ := r.loader.LoadActive(ctx, customerID, proxyID)
		activeSnap, activeIdent = snap, ident
	}

	// Build the two effective profiles once up front; we re-run the
	// engine against them per sample.
	activeEffective := ApplyOverlay(&base, activeSnap, activeIdent)
	candidateEffective := ApplyOverlay(&base, candidate, overlay.Identity{})

	out := &overlay.PreviewResult{
		Results: make([]overlay.PreviewSampleResult, 0, len(samples)),
	}
	for _, s := range samples {
		activeRes := r.engine.RunSteps(ctx, s.Content, activeEffective.Input, &RunOptions{
			Canonicalize: activeEffective.CanonicalizeEnabled,
			Role:         RoleUser,
		})
		candRes := r.engine.RunSteps(ctx, s.Content, candidateEffective.Input, &RunOptions{
			Canonicalize: candidateEffective.CanonicalizeEnabled,
			Role:         RoleUser,
		})
		div, _ := overlay.Classify(
			string(activeRes.Action), string(candRes.Action),
			activeRes.ShouldBlock, candRes.ShouldBlock,
		)

		row := overlay.PreviewSampleResult{
			Content:         s.Content,
			ActiveAction:    string(activeRes.Action),
			CandidateAction: string(candRes.Action),
			ActiveBlock:     activeRes.ShouldBlock,
			CandidateBlock:  candRes.ShouldBlock,
			Divergence:      div,
		}
		out.Results = append(out.Results, row)

		out.Summary.Total++
		switch div {
		case overlay.DivergenceWouldBlock:
			out.Summary.WouldBlock++
		case overlay.DivergenceWouldAllow:
			out.Summary.WouldAllow++
		case overlay.DivergenceThresholdDiff:
			out.Summary.ThresholdDiff++
		default:
			out.Summary.Matching++
		}
	}
	return out, nil
}
