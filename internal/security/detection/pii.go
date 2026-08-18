package detection

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/bastio-ai/bastio/internal/security"
)

// luhnValid runs the Luhn checksum over the digits in s. Used to
// filter false-positive credit-card matches: any 13–19 digit string
// can look like a card, but real cards pass Luhn — tracking numbers,
// order IDs, and UUIDs almost never do.
func luhnValid(s string) bool {
	var sum, count int
	alt := false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		n := int(c - '0')
		count++
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	if count < 13 || count > 19 {
		return false
	}
	return sum%10 == 0
}

// ibanValid runs the mod-97 check defined by ISO 13616. An IBAN is
// valid when the rearranged letter-to-digit form is divisible by 97.
// Conservative: we require the 2-letter country code + 2 check digits
// + at least 11 more chars (Norway's minimum length).
func ibanValid(s string) bool {
	clean := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "-", ""))
	if len(clean) < 15 || len(clean) > 34 {
		return false
	}
	// Move first 4 chars to the end, then letters → A=10, B=11, …
	rearranged := clean[4:] + clean[:4]
	var digits strings.Builder
	digits.Grow(len(rearranged) * 2)
	for _, r := range rearranged {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteByte(byte(r))
		case r >= 'A' && r <= 'Z':
			fmt.Fprintf(&digits, "%d", int(r-'A')+10)
		default:
			return false
		}
	}
	// Process in chunks of 9 digits to avoid overflow.
	rem := 0
	src := digits.String()
	for i := 0; i < len(src); i += 9 {
		end := i + 9
		if end > len(src) {
			end = len(src)
		}
		chunk := fmt.Sprintf("%d%s", rem, src[i:end])
		var v int
		fmt.Sscanf(chunk, "%d", &v)
		rem = v % 97
	}
	return rem == 1
}

// PIIDetector detects personally identifiable information in content.
type PIIDetector struct {
	patterns []piiPattern
}

type piiPattern struct {
	regex    *regexp.Regexp
	piiType  string
	severity security.Severity
	maskFn   func(string) string
	// validate runs after regex match. Return false to suppress a
	// match — used for checksummed types (credit card, IBAN) to drop
	// look-alike strings that can't be real. Nil = always accept.
	validate func(string) bool
}

// NewPIIDetector creates a PII detector with standard patterns.
func NewPIIDetector() *PIIDetector {
	d := &PIIDetector{}
	d.initPatterns()
	return d
}

func (d *PIIDetector) Name() string { return "pii" }

func (d *PIIDetector) Detect(ctx context.Context, content string) ([]security.Finding, error) {
	var findings []security.Finding

	for _, p := range d.patterns {
		matches := p.regex.FindAllString(content, -1)
		for _, match := range matches {
			if p.validate != nil && !p.validate(match) {
				continue
			}
			masked := match
			if p.maskFn != nil {
				masked = p.maskFn(match)
			}

			findings = append(findings, security.Finding{
				ThreatType:     security.ThreatPII,
				DetectorName:   "pii",
				Severity:       p.severity,
				Score:          severityToScore(p.severity),
				Confidence:     0.90,
				MatchedPattern: p.piiType,
				MatchedContent: masked, // Store masked version, not raw PII
				Action:         security.ActionMask,
				Message:        fmt.Sprintf("PII detected: %s", p.piiType),
			})
		}
	}

	return findings, nil
}

// Mask replaces all PII in content with one-way lossy masks
// (e.g. ***-**-6789). The original values cannot be recovered.
// Respects per-pattern validate hooks so look-alike matches (order
// IDs that look like credit cards, random 9-digit strings) pass
// through unchanged.
func (d *PIIDetector) Mask(content string) string {
	result := content
	for _, p := range d.patterns {
		replace := func(match string) string {
			if p.validate != nil && !p.validate(match) {
				return match
			}
			if p.maskFn != nil {
				return p.maskFn(match)
			}
			return "[REDACTED]"
		}
		result = p.regex.ReplaceAllStringFunc(result, replace)
	}
	return result
}

// Redact is a deprecated alias for Mask kept for back-compat with
// existing call sites and tests. Prefer Mask in new code.
func (d *PIIDetector) Redact(content string) string { return d.Mask(content) }

// TokenizeInto replaces each detected PII span with a reversible
// placeholder, writing entries into the provided TokenMap. Repeated
// values collapse onto the same placeholder so the LLM sees consistent
// references across message turns when one map spans the whole
// conversation. A nil map is a no-op and returns content unchanged.
func (d *PIIDetector) TokenizeInto(content string, tm *security.TokenMap) string {
	if tm == nil {
		return content
	}
	result := content
	for _, p := range d.patterns {
		piiType := p.piiType
		validate := p.validate
		result = p.regex.ReplaceAllStringFunc(result, func(match string) string {
			if validate != nil && !validate(match) {
				return match
			}
			return tm.Add(piiType, match)
		})
	}
	return result
}

// Tokenize is a convenience wrapper that creates a fresh TokenMap. Use
// TokenizeInto when you need to accumulate placeholders across multiple
// calls (e.g. every user message in a conversation).
func (d *PIIDetector) Tokenize(content string, style security.TokenStyle) (string, *security.TokenMap) {
	tm := security.NewTokenMap(style)
	return d.TokenizeInto(content, tm), tm
}

func (d *PIIDetector) initPatterns() {
	d.patterns = []piiPattern{
		// SSN (critical). Accepts dashed (123-45-6789), space-separated
		// (123 45 6789), and bare 9-digit (123456789) forms. The bare form
		// over-matches occasionally (tracking numbers, account numbers)
		// — acceptable for a security default since masking on a false
		// positive is safer than missing a real SSN.
		{
			regex:    regexp.MustCompile(`\b\d{3}[- ]?\d{2}[- ]?\d{4}\b`),
			piiType:  "ssn",
			severity: security.SeverityCritical,
			maskFn: func(s string) string {
				digits := regexp.MustCompile(`\d`).FindAllString(s, -1)
				if len(digits) >= 4 {
					return "***-**-" + strings.Join(digits[len(digits)-4:], "")
				}
				return "[SSN]"
			},
		},
		// Credit card numbers (critical) — 13-19 digits with optional
		// separators. Luhn-validated to suppress look-alikes: order
		// IDs, UUID fragments, and arbitrary numeric strings are
		// overwhelmingly likely to fail the checksum while real cards
		// always pass.
		{
			regex:    regexp.MustCompile(`\b(?:\d{4}[-\s]?){3,4}\d{1,4}\b`),
			piiType:  "credit_card",
			severity: security.SeverityCritical,
			validate: luhnValid,
			maskFn: func(s string) string {
				digits := regexp.MustCompile(`\d`).FindAllString(s, -1)
				if len(digits) >= 4 {
					last4 := strings.Join(digits[len(digits)-4:], "")
					return "****-****-****-" + last4
				}
				return "[CREDIT_CARD]"
			},
		},
		// Email addresses (high)
		{
			regex:    regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
			piiType:  "email",
			severity: security.SeverityHigh,
			maskFn: func(s string) string {
				parts := strings.SplitN(s, "@", 2)
				if len(parts) == 2 && len(parts[0]) > 1 {
					return string(parts[0][0]) + "***@" + parts[1]
				}
				return "[EMAIL]"
			},
		},
		// Phone numbers (high) — US format
		{
			regex:    regexp.MustCompile(`\b(?:\+?1[-.\s]?)?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}\b`),
			piiType:  "phone",
			severity: security.SeverityHigh,
			maskFn: func(s string) string {
				digits := regexp.MustCompile(`\d`).FindAllString(s, -1)
				if len(digits) >= 4 {
					last4 := strings.Join(digits[len(digits)-4:], "")
					return "***-***-" + last4
				}
				return "[PHONE]"
			},
		},
		// IPv4 addresses (medium)
		{
			regex:    regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`),
			piiType:  "ip_address",
			severity: security.SeverityMedium,
			maskFn: func(s string) string {
				parts := strings.Split(s, ".")
				if len(parts) == 4 {
					return parts[0] + ".*.*." + parts[3]
				}
				return "[IP]"
			},
		},
		// API key patterns (high). Kept for back-compat — real coverage
		// lives in the dedicated SecretsDetector which has per-provider
		// prefixes and entropy gating.
		{
			regex:    regexp.MustCompile(`(?i)\b(?:sk|pk|api[_-]?key|token|secret)[_-]?[a-zA-Z0-9]{20,}\b`),
			piiType:  "api_key",
			severity: security.SeverityHigh,
			maskFn: func(s string) string {
				if len(s) > 8 {
					return s[:8] + "..." + "[REDACTED]"
				}
				return "[API_KEY]"
			},
		},

		// IBAN (International Bank Account Number) — ISO 13616.
		// 2 letters country code + 2 check digits + up to 30 BBAN
		// characters. mod-97 validation eliminates look-alikes.
		{
			regex:    regexp.MustCompile(`\b[A-Z]{2}\d{2}(?:[ -]?[A-Z0-9]){11,30}\b`),
			piiType:  "iban",
			severity: security.SeverityHigh,
			validate: ibanValid,
			maskFn: func(s string) string {
				clean := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "-", ""))
				if len(clean) < 4 {
					return "[IBAN]"
				}
				return clean[:4] + strings.Repeat("*", len(clean)-8) + clean[len(clean)-4:]
			},
		},

		// UK National Insurance number — two prefix letters, six
		// digits, one suffix letter (A/B/C/D). The prefix has its own
		// forbidden combinations; we keep the regex conservative and
		// rely on the letter layout to filter most accidental matches.
		{
			regex:    regexp.MustCompile(`\b(?i)[A-CEGHJ-PR-TW-Z][A-CEGHJ-NPR-TW-Z]\s?\d{2}\s?\d{2}\s?\d{2}\s?[A-D]\b`),
			piiType:  "uk_nino",
			severity: security.SeverityHigh,
			maskFn: func(s string) string {
				clean := strings.ToUpper(strings.ReplaceAll(s, " ", ""))
				if len(clean) < 4 {
					return "[UK_NINO]"
				}
				return clean[:2] + strings.Repeat("*", len(clean)-4) + clean[len(clean)-2:]
			},
		},

		// Canadian SIN — 9 digits, Luhn-checked.
		{
			regex:    regexp.MustCompile(`\b\d{3}[- ]?\d{3}[- ]?\d{3}\b`),
			piiType:  "canadian_sin",
			severity: security.SeverityHigh,
			validate: luhnValid,
			maskFn: func(s string) string {
				digits := regexp.MustCompile(`\d`).FindAllString(s, -1)
				if len(digits) >= 3 {
					return "***-***-" + strings.Join(digits[len(digits)-3:], "")
				}
				return "[SIN]"
			},
		},

		// EU VAT — country prefix + 8-12 digits/letters. The VIES
		// rules differ per country; we accept the common shape and
		// leave deep validation to the cloud tier.
		{
			regex:    regexp.MustCompile(`\b(?:AT|BE|BG|CY|CZ|DE|DK|EE|EL|ES|FI|FR|GB|HR|HU|IE|IT|LT|LU|LV|MT|NL|PL|PT|RO|SE|SI|SK|XI)\s?[A-Z0-9]{8,12}\b`),
			piiType:  "eu_vat",
			severity: security.SeverityMedium,
			maskFn: func(s string) string {
				clean := strings.ToUpper(strings.ReplaceAll(s, " ", ""))
				if len(clean) < 4 {
					return "[VAT]"
				}
				return clean[:2] + strings.Repeat("*", len(clean)-2)
			},
		},

		// Aadhaar (India) — 12-digit identifier, commonly written
		// as three groups of four. High false-positive potential if
		// validation isn't applied; the Verhoeff checksum lives in
		// the cloud tier. Kept at Medium severity in OSS.
		{
			regex:    regexp.MustCompile(`\b[2-9]\d{3}[- ]?\d{4}[- ]?\d{4}\b`),
			piiType:  "aadhaar",
			severity: security.SeverityMedium,
			maskFn: func(s string) string {
				digits := regexp.MustCompile(`\d`).FindAllString(s, -1)
				if len(digits) >= 4 {
					return "XXXX-XXXX-" + strings.Join(digits[len(digits)-4:], "")
				}
				return "[AADHAAR]"
			},
		},

		// Danish CPR (personnummer). Hyphenated and space-separated
		// forms only — the hyphenless 10-digit form collides with
		// order IDs and is left to the extension-era paste detector.
		// Day is anchored 01-31 so random digit runs don't fire.
		{
			regex:    regexp.MustCompile(`\b(0[1-9]|[12]\d|3[01])\d{4}-\d{4}\b`),
			piiType:  "danish_cpr",
			severity: security.SeverityHigh,
			maskFn: func(s string) string {
				if len(s) >= 4 {
					return s[:2] + "****-****"
				}
				return "[CPR]"
			},
		},
		{
			regex:    regexp.MustCompile(`\b(0[1-9]|[12]\d|3[01])\s+\d{2}\s+\d{2}\s+\d{4}\b`),
			piiType:  "danish_cpr",
			severity: security.SeverityHigh,
			maskFn: func(s string) string {
				return "[CPR]"
			},
		},
	}
}
