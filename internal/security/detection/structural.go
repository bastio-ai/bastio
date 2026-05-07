package detection

import (
	"regexp"
	"strings"

	"github.com/bastio-ai/bastio/internal/security"
)

// Layer 3 — structural / statistical heuristics. These are the checks
// that would take an unreasonable regex to cover: many-shot fake-turn
// density, assistant-prefix injection, dense encoded payloads, and
// meta-instruction clustering. They run alongside the regex patterns
// in the jailbreak detector and emit findings under structural.* and
// fingerprint.* sub-categories.
//
// None of these are silver bullets on their own; each is tuned with
// high thresholds so benign prompts don't false-positive. The
// combination-bonus path in jailbreak.go lifts them into block
// territory when another jailbreak signal co-fires.

var (
	// fakeTurnMarker matches the start of a line that looks like a
	// conversation-turn header — the many-shot smuggling hallmark.
	// Matches both the OpenAI "assistant:" style and the ChatML
	// "<|im_start|>assistant" style.
	fakeTurnMarker = regexp.MustCompile(`(?im)^(?:<\|im_start\|>\s*)?(?:assistant|user|system|human|ai)\s*[:>]`)

	// chatTemplateCloser catches message bodies that end in a
	// closing chat-template token — the prefix-injection tail where
	// the attacker tries to close the user's turn so the model
	// treats the next content as a new assistant turn.
	chatTemplateCloser = regexp.MustCompile(`(?:<\|(?:im_end|eot_id|end_of_turn)\|>|<\/s>|\[/INST\])\s*(?:<\|(?:im_start|start_header_id)\|>\s*)?(?:assistant|user|system)?\s*$`)

	// metaImperatives are verbs in the meta-instruction space.
	metaImperatives = []string{"ignore", "forget", "disregard", "override", "bypass", "skip", "discard", "wipe"}
	// metaTargets are the nouns those verbs act on in a jailbreak.
	metaTargets = []string{"instructions", "rules", "prompt", "guidelines", "restrictions", "policies", "constraints", "directives", "safeguards"}
)

// structuralFindings runs the non-regex structural checks on content
// and returns any findings that crossed threshold. Safe to call with
// empty content — returns nil.
func structuralFindings(content string) []security.Finding {
	if content == "" {
		return nil
	}
	var out []security.Finding

	if f := detectManyShot(content); f != nil {
		out = append(out, *f)
	}
	if f := detectAssistantPrefixInjection(content); f != nil {
		out = append(out, *f)
	}
	if f := detectEncodingDensity(content); f != nil {
		out = append(out, *f)
	}
	if f := detectMetaInstructionCluster(content); f != nil {
		out = append(out, *f)
	}
	return out
}

// detectManyShot flags user messages that embed many fake
// conversation turns. Anthropic disclosed in 2024 that stuffing a long
// tail of fake `assistant:` replies into the context substantially
// erodes model safety — the "many-shot jailbreak". Three or more fake
// turn markers in one message is our heuristic threshold.
func detectManyShot(content string) *security.Finding {
	matches := fakeTurnMarker.FindAllStringIndex(content, -1)
	n := len(matches)
	if n < 3 {
		return nil
	}
	// Scale severity with turn count.
	severity := security.SeverityHigh
	confidence := 0.82
	if n >= 10 {
		severity = security.SeverityCritical
		confidence = 0.92
	}
	snippet := content
	if len(snippet) > 120 {
		snippet = snippet[:120] + "..."
	}
	return &security.Finding{
		ThreatType:     security.ThreatJailbreak,
		DetectorName:   "jailbreak",
		Severity:       severity,
		Score:          severityToScore(severity),
		Confidence:     confidence,
		SubCategory:    "structural.many_shot",
		MatchedPattern: "many_shot",
		MatchedContent: snippet,
		Action:         security.ActionBlock,
		Message:        "Many-shot jailbreak pattern: embedded fake conversation turns",
		Source:         "Anil et al. 2024 \"Many-shot jailbreaking\" (Anthropic)",
	}
}

// detectAssistantPrefixInjection catches messages that close the
// user's turn with a chat-template token and try to open the
// model's — the classic "trick the model into thinking it already
// agreed" attack.
func detectAssistantPrefixInjection(content string) *security.Finding {
	trimmed := strings.TrimRightFunc(content, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if !chatTemplateCloser.MatchString(trimmed) {
		return nil
	}
	snippet := trimmed
	if len(snippet) > 120 {
		snippet = "..." + snippet[len(snippet)-120:]
	}
	return &security.Finding{
		ThreatType:     security.ThreatJailbreak,
		DetectorName:   "jailbreak",
		Severity:       security.SeverityHigh,
		Score:          severityToScore(security.SeverityHigh),
		Confidence:     0.86,
		SubCategory:    "structural.assistant_prefix",
		MatchedPattern: "assistant_prefix_injection",
		MatchedContent: snippet,
		Action:         security.ActionBlock,
		Message:        "Assistant-prefix injection: chat-template tokens at message tail",
		Source:         "OWASP LLM01 taxonomy",
	}
}

// detectEncodingDensity flags messages whose body is dominated by
// base64 / hex / random-looking token runs. The normalizer already
// attempted a decode; this heuristic catches the case where the
// payload didn't decode cleanly but the density is still a signal
// (adversarial suffix smuggling, GCG-style tails).
func detectEncodingDensity(content string) *security.Finding {
	if len(content) < 40 {
		return nil
	}
	longRun := 0
	curRun := 0
	for i := 0; i < len(content); i++ {
		c := content[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '-' || c == '_' {
			curRun++
			if curRun > longRun {
				longRun = curRun
			}
			continue
		}
		curRun = 0
	}
	// A single uninterrupted alphanumeric run of 80+ chars is almost
	// never organic prose — it's a base64 blob, hex dump, or
	// adversarial suffix.
	if longRun < 80 {
		return nil
	}
	severity := security.SeverityMedium
	confidence := 0.65
	if longRun >= 200 {
		severity = security.SeverityHigh
		confidence = 0.80
	}
	return &security.Finding{
		ThreatType:     security.ThreatJailbreak,
		DetectorName:   "jailbreak",
		Severity:       severity,
		Score:          severityToScore(severity),
		Confidence:     confidence,
		SubCategory:    "structural.encoding_density",
		MatchedPattern: "encoding_density",
		MatchedContent: "[encoded run of " + itoa(longRun) + " chars]",
		Action:         security.ActionWarn,
		Message:        "High encoding density — likely smuggled payload or adversarial suffix",
		Source:         "Zou et al. 2023 (GCG) + general encoding-smuggle taxonomy",
	}
}

// detectMetaInstructionCluster looks for co-occurrences of imperative
// verbs ("ignore", "forget", ...) with meta-targets ("instructions",
// "rules", ...) within a short window. Catches phrasings the regex
// patterns missed because word order was unusual.
func detectMetaInstructionCluster(content string) *security.Finding {
	lc := strings.ToLower(content)
	for _, verb := range metaImperatives {
		vi := strings.Index(lc, verb)
		if vi < 0 {
			continue
		}
		// Look ahead up to 40 chars for a meta-target.
		end := vi + len(verb) + 40
		if end > len(lc) {
			end = len(lc)
		}
		window := lc[vi:end]
		for _, target := range metaTargets {
			if strings.Contains(window, target) {
				return &security.Finding{
					ThreatType:     security.ThreatJailbreak,
					DetectorName:   "jailbreak",
					Severity:       security.SeverityHigh,
					Score:          severityToScore(security.SeverityHigh),
					Confidence:     0.78,
					SubCategory:    "structural.meta_imperative",
					MatchedPattern: verb + "_near_" + target,
					MatchedContent: window,
					Action:         security.ActionBlock,
					Message:        "Meta-instruction cluster: " + verb + " ... " + target,
					Source:         "derived from Wei et al. 2023 \"Jailbroken\"",
				}
			}
		}
	}
	return nil
}

// itoa is a tiny non-strconv helper so we don't import strconv for a
// single number. Positive ints only; fine for our use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
