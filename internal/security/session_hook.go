package security

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/internal/security/session"
)

// applySessionAware runs the session-aware detectors (Crescendo,
// rate anomaly, ...) and updates the per-session buffer. Called after
// aggregation so it has access to the final per-turn Score and top
// sub_category.
//
// Two fail-open properties:
//   - No store set → no-op. Deployments without Redis never notice.
//   - Any error from the store is logged and swallowed. We never
//     want session-history failures to block legitimate traffic.
func (e *Engine) applySessionAware(ctx context.Context, req *ScanRequest, result *ScanResult) {
	if req.SessionID == "" {
		return
	}
	sessionID := scopedSessionID(req.CustomerID, req.SessionID)

	// Run the session-aware detectors first so they see the current
	// turn's max signal as "currentScore". Findings from them get
	// appended; the aggregate Score isn't re-computed (the step /
	// parallel paths already picked an Action, and session findings
	// are additive signal).
	if len(e.sessionDetectors) > 0 {
		maxScore := 0.0
		for _, f := range result.Findings {
			s := f.Score * f.Confidence
			if s > maxScore {
				maxScore = s
			}
		}
		for _, sd := range e.sessionDetectors {
			if gated, ok := sd.(GatedSessionDetector); ok && !gated.EnabledFor(req) {
				continue
			}
			extra, err := sd.DetectWithSession(ctx, sessionID, req.Content, maxScore)
			if err != nil {
				slog.Warn("session detector error", "error", err)
			}
			extra = filterSuppressed(extra, req.Suppressions)
			if len(extra) == 0 {
				continue
			}
			result.Findings = append(result.Findings, extra...)
			// Lift action upward only — block wins outright; warn
			// lifts pass/log but never downgrades a stronger action.
			for _, f := range extra {
				if f.Action == ActionBlock && !result.ShouldBlock {
					result.ShouldBlock = true
					result.Action = ActionBlock
				}
				if f.Action == ActionWarn && (result.Action == ActionPass || result.Action == ActionLog || result.Action == ActionLogOnly) {
					result.Action = ActionWarn
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
		if err := e.sessionStore.Append(ctx, sessionID, entry); err != nil {
			slog.Warn("crescendo store append error", "error", err)
		}
	}
}

// scopedSessionID namespaces the caller-supplied session id by tenant so
// playground /v1/detect cannot poison another customer's gateway
// Crescendo buffer. Empty customer id keeps the raw id (tests, offline).
func scopedSessionID(customerID, sessionID string) string {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return sessionID
	}
	return customerID + ":" + sessionID
}
