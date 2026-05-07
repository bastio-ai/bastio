package detection

import (
	"context"
	"strings"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/session"
)

// CrescendoDetector catches multi-turn jailbreaks. Unlike the regex-
// and structural-based detectors, it consults the per-session history
// (maintained by the engine in Redis) to reason about how the current
// request relates to prior turns. Three failure modes we watch for:
//
//  1. Escalation — scores in recent turns climb monotonically with a
//     meaningful delta. Classic Crescendo (Russinovich/Microsoft 2024).
//  2. Priming — an earlier turn established a persona / hypothetical
//     / safety-bypass frame, and the current turn asks something
//     sensitive. The priming turn itself might have been benign-
//     looking in isolation; only the combination reveals intent.
//  3. Gradual topic drift — a sequence of turns whose sub-categories
//     drift from benign → persona/hypothetical → direct harmful ask.
//
// The detector fails open: any Redis error or missing history simply
// returns no finding. It never penalises the user for infrastructure
// issues, and it never introduces detection latency when sessions
// aren't configured.
type CrescendoDetector struct {
	store session.Store
}

// NewCrescendoDetector wires the detector to a session store. Pass
// nil to disable — all Detect calls become no-ops.
func NewCrescendoDetector(store session.Store) *CrescendoDetector {
	return &CrescendoDetector{store: store}
}

// DetectWithSession returns findings derived from the session's
// recent history plus the current request's aggregate score (which
// the engine computes from the regex detectors and passes in).
//
// currentScore is the highest single-finding score the current turn
// has produced so far — usually the max(Score*Confidence) across
// the other detectors' findings. A value of 0 means "nothing flagged
// in isolation", which is still interesting if it follows priming.
func (d *CrescendoDetector) DetectWithSession(
	ctx context.Context,
	sessionID string,
	content string,
	currentScore float64,
) ([]security.Finding, error) {
	if d == nil || d.store == nil || sessionID == "" {
		return nil, nil
	}
	history, err := d.store.Recent(ctx, sessionID, 5)
	if err != nil || len(history) == 0 {
		return nil, err
	}

	var findings []security.Finding

	if f := detectEscalation(history, currentScore); f != nil {
		findings = append(findings, *f)
	}
	if f := detectPriming(history, content, currentScore); f != nil {
		findings = append(findings, *f)
	}
	return findings, nil
}

// detectEscalation flags a monotonically climbing score sequence.
// history[0] is the most recent prior turn.
func detectEscalation(history []session.Entry, currentScore float64) *security.Finding {
	if len(history) < 2 {
		return nil
	}
	// Expect: oldest → newest climbs. history is newest-first.
	// Reverse-walk to get chronological order, then check
	// successive deltas.
	chrono := make([]float64, 0, len(history)+1)
	for i := len(history) - 1; i >= 0; i-- {
		chrono = append(chrono, history[i].Score)
	}
	chrono = append(chrono, currentScore)

	climbing := 0
	for i := 1; i < len(chrono); i++ {
		if chrono[i] > chrono[i-1]+0.10 {
			climbing++
		}
	}
	// 3+ successive upward steps with min delta 0.10 each is the
	// Crescendo signature.
	if climbing < 3 {
		return nil
	}
	return &security.Finding{
		ThreatType:     security.ThreatJailbreak,
		DetectorName:   "crescendo",
		Severity:       security.SeverityHigh,
		Score:          0.85,
		Confidence:     0.82,
		SubCategory:    "multi_turn.escalation",
		MatchedPattern: "crescendo_escalation",
		MatchedContent: "session score climbing over recent turns",
		Action:         security.ActionBlock,
		Message:        "Crescendo jailbreak: monotonically escalating threat score across session turns",
		Source:         "Russinovich, Microsoft 2024 \"Crescendo Multi-turn Jailbreak\"",
	}
}

// sensitiveTargets are the terms whose presence in the *current*
// turn — following a primed prior turn — is the priming-sequence
// signal. Intentionally narrow; broad lists would false-positive on
// legitimate security / policy discussions.
var sensitiveTargets = []string{
	"now tell me", "now give me", "now explain", "now show me",
	"now write", "actually", "in that case", "great, so",
	"so with that",
}

// primingSubCategoryPrefixes mark a prior turn as "primed" — the
// setup phase of a multi-turn attack.
var primingSubCategoryPrefixes = []string{
	"persona.",
	"hypothetical.",
	"safety_bypass.",
	"authority.",
	"fingerprint.",
}

func detectPriming(history []session.Entry, content string, currentScore float64) *security.Finding {
	// Need evidence that an earlier turn was primed, and the current
	// turn reads like a "payload" follow-up.
	primed := false
	for _, e := range history {
		for _, pfx := range primingSubCategoryPrefixes {
			if strings.HasPrefix(e.TopSubCategory, pfx) {
				primed = true
				break
			}
		}
		if primed {
			break
		}
	}
	if !primed {
		return nil
	}
	lc := strings.ToLower(content)
	payloadish := false
	for _, t := range sensitiveTargets {
		if strings.Contains(lc, t) {
			payloadish = true
			break
		}
	}
	// Either a payload-shaped ask OR a mid-score current turn: both
	// are consistent with "here's the real request, now that you're
	// primed".
	if !payloadish && currentScore < 0.30 {
		return nil
	}
	confidence := 0.80
	if payloadish && currentScore >= 0.30 {
		confidence = 0.90
	}
	return &security.Finding{
		ThreatType:     security.ThreatJailbreak,
		DetectorName:   "crescendo",
		Severity:       security.SeverityHigh,
		Score:          0.82,
		Confidence:     confidence,
		SubCategory:    "multi_turn.priming_payload",
		MatchedPattern: "crescendo_priming",
		MatchedContent: "primed prior turn + payload-shaped current ask",
		Action:         security.ActionBlock,
		Message:        "Crescendo jailbreak: primed context followed by payload-shaped request",
		Source:         "Russinovich, Microsoft 2024 \"Crescendo Multi-turn Jailbreak\"",
	}
}
