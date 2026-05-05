package security

import (
	"context"
	"log/slog"
	"time"

	"github.com/bastio-ai/bastio/internal/security/session"
)

// applySessionAware runs the Crescendo pass and updates the per-
// session buffer. Called after aggregation so it has access to the
// final per-turn Score and top sub_category.
//
// Two fail-open properties:
//   - No store set → no-op. Deployments without Redis never notice.
//   - Any error from the store is logged and swallowed. We never
//     want session-history failures to block legitimate traffic.
func (e *Engine) applySessionAware(ctx context.Context, req *ScanRequest, result *ScanResult) {
	if req.SessionID == "" {
		return
	}

	// Run the session-aware detector first so it sees the current
	// turn's max signal as "currentScore". Findings from it get
	// appended; the aggregate Score isn't re-computed (the step /
	// parallel paths already picked an Action, and crescendo is
	// additive signal).
	if e.sessionDetector != nil {
		maxScore := 0.0
		for _, f := range result.Findings {
			s := f.Score * f.Confidence
			if s > maxScore {
				maxScore = s
			}
		}
		extra, err := e.sessionDetector.DetectWithSession(ctx, req.SessionID, req.Content, maxScore)
		if err != nil {
			slog.Warn("crescendo detect error", "error", err, "session", req.SessionID)
		}
		if len(extra) > 0 {
			result.Findings = append(result.Findings, extra...)
			// Lift action if crescendo said block and we weren't
			// already blocking.
			if !result.ShouldBlock {
				for _, f := range extra {
					if f.Action == ActionBlock {
						result.ShouldBlock = true
						result.Action = ActionBlock
						break
					}
				}
			}
		}
	}

	// Append this turn's compact summary to the session buffer so
	// the NEXT turn's Crescendo pass can see it. Picks the top
	// sub-category from the first finding that has one — that's
	// usually the highest-severity one.
	if e.sessionStore != nil {
		top := ""
		for _, f := range result.Findings {
			if f.SubCategory != "" {
				top = f.SubCategory
				break
			}
		}
		entry := session.Entry{
			Score:          result.ThreatScore,
			TopSubCategory: top,
			Action:         string(result.Action),
			At:             time.Now().UTC(),
		}
		if err := e.sessionStore.Append(ctx, req.SessionID, entry); err != nil {
			slog.Warn("crescendo store append error", "error", err, "session", req.SessionID)
		}
	}
}
