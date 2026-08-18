package detection

import (
	"context"
	"math"
	"regexp"
	"strings"

	"github.com/bastio-ai/bastio/internal/security"
)

// SecretsDetector catches API keys, cloud credentials, and private
// material that developers paste into LLMs more often than anyone
// admits. Distinct from the PII detector's toy "api_key" rule because:
//
//   - Real providers have stable, documented prefixes (sk-, ghp_,
//     AKIA, xoxb-) that we can match with low false-positive rate.
//   - Generic high-entropy tokens (random UUIDs, hashes) require
//     entropy-based detection rather than regex alone.
//   - Masking a secret is lossy but the safer default; there's no
//     legitimate reason to forward an API key verbatim to an LLM.
type SecretsDetector struct {
	prefixes []secretPattern
}

type secretPattern struct {
	regex      *regexp.Regexp
	kind       string
	severity   security.Severity
	confidence float64
	// minEntropy gates whether a matched candidate is treated as a
	// real secret. Well-known prefixes (AKIA, sk-, ghp_) get 0.0 —
	// the prefix alone is enough. Generic heuristic matches ("token"
	// keyword) need the value to look random.
	minEntropy float64
	// maskFn renders the finding for logs. We never log the full
	// secret — first 4 chars + length gives enough for a SIEM to
	// deduplicate without leaking the credential.
	maskFn func(string) string
}

// NewSecretsDetector builds the detector with the canonical prefix
// library. Regexes compile once at startup.
func NewSecretsDetector() *SecretsDetector {
	d := &SecretsDetector{}
	d.initPatterns()
	return d
}

func (d *SecretsDetector) Name() string { return "secrets" }

func (d *SecretsDetector) Detect(_ context.Context, content string) ([]security.Finding, error) {
	var findings []security.Finding

	for _, p := range d.prefixes {
		matches := p.regex.FindAllString(content, -1)
		for _, m := range matches {
			if p.minEntropy > 0 && shannonEntropy(m) < p.minEntropy {
				continue
			}
			findings = append(findings, security.Finding{
				ThreatType:     security.ThreatType("secret"),
				DetectorName:   "secrets",
				Severity:       p.severity,
				Score:          severityToScore(p.severity),
				Confidence:     p.confidence,
				MatchedPattern: p.kind,
				MatchedContent: p.maskFn(m),
				Action:         security.ActionMask,
				Message:        "Secret detected: " + p.kind,
			})
		}
	}

	return findings, nil
}

// Mask one-way replaces every match with a short tag that keeps the
// kind visible (so the LLM sees "[REDACTED_AWS_KEY]" instead of a
// real key). Irreversible — secrets should never be recoverable.
func (d *SecretsDetector) Mask(content string) string {
	result := content
	for _, p := range d.prefixes {
		tag := "[REDACTED_" + strings.ToUpper(p.kind) + "]"
		result = p.regex.ReplaceAllString(result, tag)
	}
	return result
}

// TokenizeInto is a pass-through: we do not produce reversible
// placeholders for secrets. Restoring an API key in the response
// would defeat the whole point.
func (d *SecretsDetector) TokenizeInto(content string, _ *security.TokenMap) string {
	return d.Mask(content)
}

func (d *SecretsDetector) initPatterns() {
	maskPrefix := func(n int) func(string) string {
		return func(s string) string {
			if len(s) <= n {
				return "[REDACTED]"
			}
			return s[:n] + "…"
		}
	}

	d.prefixes = []secretPattern{
		// AWS — 20-char access keys (AKIA long-lived, ASIA STS) and 40-char secrets.
		{
			regex:      regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
			kind:       "aws_access_key",
			severity:   security.SeverityCritical,
			confidence: 0.98,
			maskFn:     maskPrefix(4),
		},
		{
			regex:      regexp.MustCompile(`\b(?i:aws_secret_access_key|aws_secret)\s*[=:]\s*['"]?([A-Za-z0-9/+=]{40})['"]?`),
			kind:       "aws_secret_key",
			severity:   security.SeverityCritical,
			confidence: 0.95,
			maskFn:     maskPrefix(8),
		},

		// GitHub — classic PAT plus ghs/gho/ghr/ghu prefixes.
		{
			regex:      regexp.MustCompile(`\b(?:ghp|ghs|gho|ghr|ghu)_[A-Za-z0-9]{36,}\b`),
			kind:       "github_pat",
			severity:   security.SeverityCritical,
			confidence: 0.99,
			maskFn:     maskPrefix(6),
		},
		{
			regex:      regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{82}\b`),
			kind:       "github_fine_grained_pat",
			severity:   security.SeverityCritical,
			confidence: 0.99,
			maskFn:     maskPrefix(12),
		},

		// Anthropic before the generic sk- rule so sk-ant-* is not
		// classified as an OpenAI key.
		{
			regex:      regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{40,}\b`),
			kind:       "anthropic_api_key",
			severity:   security.SeverityCritical,
			confidence: 0.98,
			maskFn:     maskPrefix(8),
		},
		// OpenAI user keys (sk- + alphanumerics) and project keys.
		// Body of the user-key rule is [A-Za-z0-9] so sk-ant-* stays
		// on the Anthropic rule (Go's RE2 has no lookahead).
		{
			regex:      regexp.MustCompile(`\bsk-proj-[A-Za-z0-9_-]{20,}\b`),
			kind:       "openai_api_key",
			severity:   security.SeverityCritical,
			confidence: 0.98,
			maskFn:     maskPrefix(8),
		},
		{
			regex:      regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`),
			kind:       "openai_api_key",
			severity:   security.SeverityCritical,
			confidence: 0.95,
			maskFn:     maskPrefix(6),
		},

		// Stripe (test + live).
		{
			regex:      regexp.MustCompile(`\b(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{24,}\b`),
			kind:       "stripe_key",
			severity:   security.SeverityCritical,
			confidence: 0.98,
			maskFn:     maskPrefix(10),
		},

		// Slack — bot, user, app, and session tokens.
		{
			regex:      regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
			kind:       "slack_token",
			severity:   security.SeverityCritical,
			confidence: 0.97,
			maskFn:     maskPrefix(7),
		},

		// Google API keys (AIza prefix, 39 chars).
		{
			regex:      regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
			kind:       "gcp_api_key",
			severity:   security.SeverityHigh,
			confidence: 0.9,
			maskFn:     maskPrefix(6),
		},

		// JWTs — three base64 segments separated by dots. Long enough
		// to skip short accidental matches on unrelated dotted text.
		{
			regex:      regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
			kind:       "jwt",
			severity:   security.SeverityHigh,
			confidence: 0.9,
			maskFn:     maskPrefix(8),
		},

		// Private keys — header line is distinctive enough alone.
		{
			regex:      regexp.MustCompile(`-----BEGIN\s+(?:RSA|EC|DSA|OPENSSH|PGP|ENCRYPTED)?\s*PRIVATE\s+KEY-----`),
			kind:       "private_key",
			severity:   security.SeverityCritical,
			confidence: 0.99,
			maskFn:     func(_ string) string { return "[PRIVATE_KEY_HEADER]" },
		},

		// Azure Storage account keys — distinctive AccountKey= prefix
		// plus 86-char base64 payload ending in ==.
		{
			regex:      regexp.MustCompile(`\bAccountKey=[A-Za-z0-9+/]{86}==`),
			kind:       "azure_storage_key",
			severity:   security.SeverityCritical,
			confidence: 0.98,
			maskFn:     maskPrefix(16),
		},
		// MongoDB connection URIs (standard + SRV). Credential lives in
		// the authority section; mask the whole URI.
		{
			regex:      regexp.MustCompile(`\bmongodb(?:\+srv)?:\/\/[^\s'"<>]{15,}`),
			kind:       "mongodb_uri",
			severity:   security.SeverityCritical,
			confidence: 0.95,
			maskFn:     func(_ string) string { return "mongodb://[REDACTED]" },
		},
		{
			regex:      regexp.MustCompile(`\bAC[a-f0-9]{32}\b`),
			kind:       "twilio_sid",
			severity:   security.SeverityHigh,
			confidence: 0.92,
			maskFn:     maskPrefix(6),
		},
		{
			regex:      regexp.MustCompile(`\bSG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}\b`),
			kind:       "sendgrid_key",
			severity:   security.SeverityCritical,
			confidence: 0.98,
			maskFn:     maskPrefix(6),
		},
		{
			regex:      regexp.MustCompile(`(?:^|[^A-Za-z0-9.-])https?://(?:ptb\.|canary\.)?discord(?:app)?\.com/api/webhooks/\d{17,20}/[A-Za-z0-9_-]{60,}`),
			kind:       "discord_webhook",
			severity:   security.SeverityHigh,
			confidence: 0.97,
			maskFn:     func(_ string) string { return "https://discord.com/api/webhooks/[REDACTED]" },
		},
		{
			regex:      regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`),
			kind:       "npm_token",
			severity:   security.SeverityCritical,
			confidence: 0.98,
			maskFn:     maskPrefix(8),
		},
		{
			regex:      regexp.MustCompile(`\btskey-(?:auth|api|client)-[A-Za-z0-9]{16,}-[A-Za-z0-9]{32,}\b`),
			kind:       "tailscale_key",
			severity:   security.SeverityCritical,
			confidence: 0.98,
			maskFn:     maskPrefix(12),
		},
		{
			regex:      regexp.MustCompile(`\bpypi-AgEIcHlwaS5vcmcC[A-Za-z0-9_-]{40,}`),
			kind:       "pypi_token",
			severity:   security.SeverityCritical,
			confidence: 0.99,
			maskFn:     maskPrefix(12),
		},
		{
			regex:      regexp.MustCompile(`(?i)\bheroku[_-]?api[_-]?key[\s:=]+[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\b`),
			kind:       "heroku_api_key",
			severity:   security.SeverityCritical,
			confidence: 0.97,
			maskFn:     maskPrefix(16),
		},

		// Generic "SECRET=" style assignment with high-entropy value —
		// only fires above the entropy threshold so we don't flag
		// bash exports like SECRET=hello or URLs.
		{
			regex:      regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|password|passwd|pwd|token|bearer)\s*[=:]\s*['"]?([A-Za-z0-9/+=_-]{20,})['"]?`),
			kind:       "generic_secret_assignment",
			severity:   security.SeverityHigh,
			confidence: 0.7,
			minEntropy: 3.5,
			maskFn:     maskPrefix(8),
		},
	}
}

// shannonEntropy computes bits per character for the value portion of
// a candidate. ~3.5 corresponds to "looks random" — enough to
// distinguish `API_KEY=hello` (low entropy) from a real secret.
func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	var e float64
	n := float64(len(s))
	for _, c := range counts {
		p := float64(c) / n
		e -= p * math.Log2(p)
	}
	return e
}
