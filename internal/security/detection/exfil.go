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
		{regexp.MustCompile(`(?i)\bhere\s+(?:is|are)\s+(?:my|the)\s+(?:system|hidden|internal|developer|original)\s+(?:prompt|instructions|rules|guidelines)\b`), "prompt_reveal", security.SeverityCritical, 0.95},
		{regexp.MustCompile(`(?i)\bmy\s+(?:system|hidden|internal|developer|original)\s+(?:prompt|instructions)\s+(?:is|are|says?|states?)\b`), "prompt_reveal", security.SeverityCritical, 0.92},
		{regexp.MustCompile(`(?i)\b(?:I\s+was|I\s+am|you\s+are)\s+(?:instructed|told|programmed|trained)\b[^.?!]{0,80}\b(?:system|hidden|internal|developer)\s+(?:prompt|instructions?|directives?)\b`), "instruction_reveal", security.SeverityHigh, 0.8},

		// A generic assistant-template opener is public text and does not
		// establish disclosure. Explicit private-prompt announcements above
		// remain protected, as do actual credentials in the secrets detector.

		// Echoing API keys / secrets in the response. Light touch —
		// the secrets detector catches the actual values; this rule
		// catches the *announcement* of a secret ("the key is…").
		{regexp.MustCompile(`(?i)\bthe\s+(?:API\s+)?(?:key|token|secret|password)\s+is\s*[:=]?\s*['"]?[A-Za-z0-9_-]{16,}\b`), "key_announcement", security.SeverityCritical, 0.9},

		// Internal tool / function names the user shouldn't see. Real
		// values come from per-customer config; this default catches
		// common conventions.
		{regexp.MustCompile(`(?i)\b(?:internal|admin|debug|staging)_(?:endpoint|url|token|key|path|route)\b\s*(?:[:=]|is\b)\s*[\x27\x22]?(?:https?://[^\s\x27\x22<>]+|[A-Za-z0-9_-]{16,})`), "internal_reference", security.SeverityHigh, 0.7},

		// Model names and generic training disclaimers are public facts,
		// not evidence of private-data disclosure.
	}
}
