package detection

import (
	"context"
	"regexp"

	"github.com/bastio-ai/bastio/internal/security"
)

// ExfilDetector scans model *output* for data-exfiltration patterns —
// the model echoing its own system prompt, revealing internal tool
// names, regurgitating embedded secrets from the context window, or
// answering a request for its configuration. Complements the
// input-side injection/jailbreak defenses with a last line of
// defense: even if an attack gets through, the outbound message
// should not leak sensitive provider-side data.
//
// Distinct from PII: PII protects the end user; exfil protects the
// operator/provider. Different patterns, different severity model.
type ExfilDetector struct {
	patterns []exfilPattern
}

type exfilPattern struct {
	regex      *regexp.Regexp
	category   string
	severity   security.Severity
	confidence float64
}

func NewExfilDetector() *ExfilDetector {
	d := &ExfilDetector{}
	d.initPatterns()
	return d
}

func (d *ExfilDetector) Name() string { return "exfil" }

func (d *ExfilDetector) Detect(_ context.Context, content string) ([]security.Finding, error) {
	var findings []security.Finding
	for _, p := range d.patterns {
		if !p.regex.MatchString(content) {
			continue
		}
		match := p.regex.FindString(content)
		if len(match) > 120 {
			match = match[:120] + "..."
		}
		findings = append(findings, security.Finding{
			ThreatType:     security.ThreatType("exfil"),
			DetectorName:   "exfil",
			Severity:       p.severity,
			Score:          severityToScore(p.severity),
			Confidence:     p.confidence,
			MatchedPattern: p.category,
			MatchedContent: match,
			Action:         security.ActionBlock,
			Message:        "Potential data exfiltration in response: " + p.category,
		})
	}
	return findings, nil
}

func (d *ExfilDetector) initPatterns() {
	d.patterns = []exfilPattern{
		// Model confirming it has / is revealing its system prompt.
		{regexp.MustCompile(`(?i)\bhere\s+(?:is|are)\s+(?:my|the)\s+(?:system\s+)?(?:prompt|instructions|rules|guidelines)\b`), "prompt_reveal", security.SeverityCritical, 0.95},
		{regexp.MustCompile(`(?i)\bmy\s+(?:system\s+)?(?:prompt|instructions)\s+(?:is|are|says|states)\b`), "prompt_reveal", security.SeverityCritical, 0.92},
		{regexp.MustCompile(`(?i)\b(?:I\s+was|I\s+am|you\s+are)\s+(?:instructed|told|programmed|trained)\s+to\b`), "instruction_reveal", security.SeverityHigh, 0.8},

		// Classic leaked-prompt openers.
		{regexp.MustCompile(`(?i)^\s*you\s+are\s+(?:a|an)\s+(?:helpful|harmless|honest)\s+(?:AI|assistant)\b`), "canonical_sysprompt", security.SeverityHigh, 0.85},
		{regexp.MustCompile(`(?i)^\s*I\s+am\s+(?:claude|gpt|gemini|llama)[,.]`), "model_self_id", security.SeverityMedium, 0.6},

		// Echoing API keys / secrets in the response. Light touch —
		// the secrets detector catches the actual values; this rule
		// catches the *announcement* of a secret ("the key is…").
		{regexp.MustCompile(`(?i)\bthe\s+(?:API\s+)?(?:key|token|secret|password)\s+is\s*[:=]?\s*['"]?[A-Za-z0-9_-]{16,}\b`), "key_announcement", security.SeverityCritical, 0.9},

		// Internal tool / function names the user shouldn't see. Real
		// values come from per-customer config; this default catches
		// common conventions.
		{regexp.MustCompile(`(?i)\b(?:internal|admin|debug|staging)_(?:endpoint|url|token|key|path|route)\b`), "internal_reference", security.SeverityHigh, 0.7},

		// Training-data regurgitation signatures. Not exhaustive; the
		// cloud tier adds semantic matching against a corpus.
		{regexp.MustCompile(`(?i)\bas\s+(?:a\s+)?(?:language\s+model|AI\s+assistant)\s+trained\s+by\b`), "training_boilerplate", security.SeverityLow, 0.5},
	}
}
