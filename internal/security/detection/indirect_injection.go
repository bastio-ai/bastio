package detection

import (
	"context"
	"regexp"

	"github.com/bastio-ai/bastio/internal/security"
)

// IndirectInjectionDetector scans content that originates *outside*
// the operator's direct input — tool output, RAG retrievals, persistent
// memory — for prompt-injection patterns.
//
// Why a separate detector: the signal is identical to plain injection
// detection, but the THRESHOLDS differ. Operator-typed "please ignore
// the error above and try again" is benign. The same sentence coming
// back from a search-result document or a tool response is almost
// certainly an attacker trying to compromise the agent through the
// content layer. Same regex library, tighter confidence floor, and
// the detector is a no-op on role=user content so it can't double-fire
// with the plain injection detector.
//
// Role is read from the context (WithRole) so existing call sites
// don't need to change; untrusted roles (tool / retrieval / memory)
// activate this detector, everything else is skipped.
type IndirectInjectionDetector struct {
	patterns []indirectPattern
}

type indirectPattern struct {
	regex      *regexp.Regexp
	category   string
	severity   security.Severity
	confidence float64
}

func NewIndirectInjectionDetector() *IndirectInjectionDetector {
	d := &IndirectInjectionDetector{}
	d.initPatterns()
	return d
}

func (d *IndirectInjectionDetector) Name() string { return "indirect_injection" }

func (d *IndirectInjectionDetector) Detect(ctx context.Context, content string) ([]security.Finding, error) {
	role := security.RoleFromContext(ctx)
	if role != "" && !security.IsUntrustedRole(role) {
		// Operator-origin content: the plain injection detector handles
		// it. Short-circuit to avoid duplicate findings.
		return nil, nil
	}

	var findings []security.Finding
	for _, p := range d.patterns {
		if !p.regex.MatchString(content) {
			continue
		}
		match := p.regex.FindString(content)
		if len(match) > 100 {
			match = match[:100] + "..."
		}
		// Indirect attacks uplift severity one notch vs. the same
		// pattern in user content — the default action is block.
		findings = append(findings, security.Finding{
			ThreatType:     security.ThreatInjection,
			DetectorName:   "indirect_injection",
			Severity:       p.severity,
			Score:          severityToScore(p.severity),
			Confidence:     p.confidence,
			MatchedPattern: p.category,
			MatchedContent: match,
			Action:         security.ActionBlock,
			Message:        "Indirect prompt injection detected in " + role + " content: " + p.category,
		})
	}
	return findings, nil
}

func (d *IndirectInjectionDetector) initPatterns() {
	// Patterns skew toward phrasings typical of *documents* and *tool
	// responses* pretending to address the model directly. Real
	// retrieval content rarely opens with "ATTENTION AI" or "system
	// override"; when it does, it's almost always malicious.
	d.patterns = []indirectPattern{
		// Document-as-instruction attacks ("When you read this doc, …")
		{regexp.MustCompile(`(?i)\b(?:attention|note to|important|urgent)\s*(?:\s+the\s+)?(?:AI|assistant|LLM|model|bot|chatbot)\b`), "doc_address", security.SeverityHigh, 0.92},
		{regexp.MustCompile(`(?i)\b(?:when|if)\s+you\s+(?:read|see|encounter|parse)\s+this\b`), "doc_trigger", security.SeverityHigh, 0.85},
		{regexp.MustCompile(`(?i)\bdear\s+(?:AI|assistant|language\s+model|claude|gpt|gemini|llama)\b`), "doc_salutation", security.SeverityHigh, 0.95},

		// System-role spoofing in untrusted content.
		{regexp.MustCompile(`(?i)^\s*system\s*[:|]\s*`), "system_spoof", security.SeverityCritical, 0.95},
		{regexp.MustCompile(`(?i)<\s*system\s*>`), "system_tag", security.SeverityCritical, 0.95},

		// Reuses the core injection vocabulary but with higher
		// confidence — in a tool response there is no benign reason
		// to see these phrases.
		{regexp.MustCompile(`(?i)\bignore\s+(?:all\s+)?(?:previous|prior|above)\s+(?:instructions|prompts|rules)\b`), "override", security.SeverityCritical, 0.98},
		{regexp.MustCompile(`(?i)\bdisregard\s+(?:all\s+)?(?:previous|above|prior)\s+(?:instructions|context)\b`), "override", security.SeverityCritical, 0.98},
		{regexp.MustCompile(`(?i)\b(?:new|updated|revised)\s+instructions?\b\s*[:=]`), "new_instructions", security.SeverityCritical, 0.95},

		// Data exfiltration instructions embedded in documents.
		{regexp.MustCompile(`(?i)\b(?:send|email|post|upload|exfiltrate|transmit|forward)\s+(?:the\s+)?(?:system\s+)?(?:prompt|secrets|credentials|api\s+keys?)\b`), "exfil_command", security.SeverityCritical, 0.95},
		{regexp.MustCompile(`(?i)\bmake\s+(?:an?\s+)?(?:HTTP|POST|GET|API|network)\s+(?:request|call)\s+to\b`), "network_command", security.SeverityHigh, 0.85},

		// Tool misuse — "call this function with these args, secretly"
		{regexp.MustCompile(`(?i)\b(?:silently|quietly|secretly|without\s+(?:telling|mentioning)\s+the\s+user)\b`), "covert_action", security.SeverityHigh, 0.9},
	}
}
