package detection

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strings"

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
// It also covers the rendering channel: a model steered by injected
// content emits a markdown image, an HTML <img>/<iframe>, or a link
// whose URL carries conversation data. The client renders it and the
// fetch delivers the data to the attacker's host. Those rules only run
// when the content is tagged security.RoleAssistant (the live model
// response) so tool-call arguments, MCP descriptions and user input
// that legitimately contain URLs are never judged by them.
//
// Distinct from PII: PII protects the end user; exfil protects the
// operator/provider. Different patterns, different severity model.
type ExfilDetector struct {
	patterns     []exfilPattern
	urlRefs      []urlRefPattern
	trustedHosts map[string]struct{}
}

type exfilPattern struct {
	regex      *regexp.Regexp
	category   string
	severity   security.Severity
	confidence float64
}

// urlRefPattern locates a URL in a construct a client may render.
// tagGroup is the capture holding an HTML tag name (0 when the pattern
// is markdown); bangGroup marks markdown image syntax.
type urlRefPattern struct {
	regex     *regexp.Regexp
	urlGroup  int
	tagGroup  int
	bangGroup int
	markdown  bool
}

// ExfilOption configures an ExfilDetector at construction.
type ExfilOption func(*ExfilDetector)

// WithTrustedURLHosts exempts hosts from the rendered-URL rules. Match is
// exact and case-insensitive, like the gateway's image-URL allowlist.
func WithTrustedURLHosts(hosts []string) ExfilOption {
	return func(d *ExfilDetector) {
		for _, h := range hosts {
			h = strings.ToLower(strings.TrimSpace(h))
			if h != "" {
				d.trustedHosts[h] = struct{}{}
			}
		}
	}
}

// ParseHostList splits a comma-separated host list (the format of
// BASTIO_IMAGE_URL_ALLOWLIST) into trimmed, lowercased entries.
func ParseHostList(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if h := strings.ToLower(strings.TrimSpace(p)); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func NewExfilDetector(opts ...ExfilOption) *ExfilDetector {
	d := &ExfilDetector{trustedHosts: map[string]struct{}{}}
	d.initPatterns()
	for _, opt := range opts {
		opt(d)
	}
	return d
}

func (d *ExfilDetector) Name() string { return "exfil" }

func (d *ExfilDetector) Detect(ctx context.Context, content string) ([]security.Finding, error) {
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
	if security.RoleFromContext(ctx) == security.RoleAssistant {
		findings = append(findings, d.detectRenderedURLs(ctx, content)...)
	}
	return findings, nil
}

// Markers that replace what a mask/redact step removes.
const (
	exfilLinkMarker    = "[link removed]"
	exfilPatternMarker = "[removed]"
)

// MaskContext implements security.ContextSanitizer: it removes what Detect
// would flag. Each rendered URL that carries data (same rules and context
// as Detect: assistant role, the request's token placeholders) is replaced
// by "[link removed]"; the surrounding markdown or HTML stays in place, and
// since the marker holds a space it no longer parses as a fetchable or
// clickable destination. Disclosure phrases are replaced by "[removed]".
// It repeats until the text is stable, so nothing Detect flags remains.
func (d *ExfilDetector) MaskContext(ctx context.Context, content string) string {
	for range 4 {
		next := d.maskOnce(ctx, content)
		if next == content {
			break
		}
		content = next
	}
	return content
}

type exfilSpan struct {
	start, end int
	marker     string
}

// RewriteSpans implements security.SpanSanitizer with the spans one
// MaskContext pass replaces.
func (d *ExfilDetector) RewriteSpans(ctx context.Context, content string) []security.RewriteSpan {
	spans := d.maskSpans(ctx, content)
	out := make([]security.RewriteSpan, 0, len(spans))
	for _, s := range spans {
		out = append(out, security.RewriteSpan{Start: s.start, End: s.end, Replacement: s.marker})
	}
	return out
}

func (d *ExfilDetector) maskOnce(ctx context.Context, content string) string {
	spans := d.maskSpans(ctx, content)
	if len(spans) == 0 {
		return content
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var b strings.Builder
	last := 0
	for _, s := range spans {
		if s.start < last {
			continue // overlaps a span already replaced
		}
		b.WriteString(content[last:s.start])
		b.WriteString(s.marker)
		last = s.end
	}
	b.WriteString(content[last:])
	return b.String()
}

func (d *ExfilDetector) maskSpans(ctx context.Context, content string) []exfilSpan {
	var spans []exfilSpan
	for _, p := range d.patterns {
		for _, m := range p.regex.FindAllStringIndex(content, -1) {
			spans = append(spans, exfilSpan{m[0], m[1], exfilPatternMarker})
		}
	}
	if security.RoleFromContext(ctx) == security.RoleAssistant && strings.Contains(content, "//") {
		hasImageRef := strings.Contains(content, "![")
		var placeholders []string
		if tm, ok := security.TokenMapFromContext(ctx); ok && tm != nil && tm.Size() > 0 {
			placeholders = tm.Placeholders()
		}
		for _, ref := range d.urlRefs {
			// Unlike Detect, no per-text URL cap: every flagged URL goes.
			for _, m := range ref.regex.FindAllStringSubmatchIndex(content, -1) {
				start, end := m[2*ref.urlGroup], m[2*ref.urlGroup+1]
				autoFetch := false
				switch {
				case ref.tagGroup > 0:
					tag := strings.ToLower(content[m[2*ref.tagGroup]:m[2*ref.tagGroup+1]])
					switch {
					case isAutoFetchTag(tag):
						autoFetch = true
					case isClickTag(tag):
					default:
						continue
					}
				case ref.bangGroup > 0:
					autoFetch = m[2*ref.bangGroup+1] > m[2*ref.bangGroup]
				default:
					autoFetch = hasImageRef
				}
				if _, ok := d.assessRenderedURL(content[start:end], autoFetch, ref.markdown, placeholders); ok {
					spans = append(spans, exfilSpan{start, end, exfilLinkMarker})
				}
			}
		}
	}
	return spans
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

	d.urlRefs = []urlRefPattern{
		// Inline markdown: ![alt](url "title") and [text](url).
		// A destination may be wrapped in <...>; otherwise it runs to
		// whitespace or ")" and may itself contain "<" / ">".
		{regex: regexp.MustCompile(`(!?)\[[^\]\n]{0,400}\]\(\s*(<[^>\s]+>|[^\s)]+)`), urlGroup: 2, bangGroup: 1, markdown: true},
		// Reference definitions: [id]: url — the target of ![x][id] or [x][id].
		{regex: regexp.MustCompile(`(?m)^[ \t]{0,3}\[[^\]\n]{1,200}\]:[ \t]*(<[^>\s]+>|\S+)`), urlGroup: 1, markdown: true},
		// HTML tags with a URL-bearing attribute. Only the first such
		// attribute in a tag is examined.
		{regex: regexp.MustCompile(`(?i)<\s*([a-z]+)\b[^>]{0,800}?\s(?:src|href|data|action|poster|background|srcset|formaction)\s*=\s*["']?([^"'\s>]+)`), urlGroup: 2, tagGroup: 1},
	}
}

// Thresholds for the rendered-URL rules. An auto-fetched resource leaks
// with zero clicks, so images are judged on weaker evidence than links.
// The base64 floor sits above 44-char Google Docs IDs and 46-char
// Unsplash ixid values; the hex floor sits above 64-char SHA-256 /
// S3 X-Amz-Signature values.
const (
	maxRenderedURLs      = 64
	imageBase64BlobMin   = 48
	linkBase64BlobMin    = 96
	imageHexBlobMin      = 72
	linkHexBlobMin       = 128
	blobEntropyMin       = 4.5
	proseMinLen          = 40
	proseMinSpaces       = 5
	renderedURLMaxReport = 80
)

// isAutoFetchTag reports tags a renderer fetches without user interaction.
func isAutoFetchTag(tag string) bool {
	switch tag {
	case "img", "image", "iframe", "frame", "script", "link", "source", "video",
		"audio", "embed", "object", "input", "track", "body", "table", "td", "picture":
		return true
	}
	return false
}

// isClickTag reports tags that need a user action before anything is sent.
func isClickTag(tag string) bool {
	switch tag {
	case "a", "area", "form", "button":
		return true
	}
	return false
}

func (d *ExfilDetector) detectRenderedURLs(ctx context.Context, content string) []security.Finding {
	// Every URL the rules care about carries "//" (scheme or
	// protocol-relative). Plain prose pays one substring search.
	if !strings.Contains(content, "//") {
		return nil
	}
	hasImageRef := strings.Contains(content, "![")
	var placeholders []string
	if tm, ok := security.TokenMapFromContext(ctx); ok && tm != nil && tm.Size() > 0 {
		placeholders = tm.Placeholders()
	}

	var findings []security.Finding
	seen := 0
	for _, ref := range d.urlRefs {
		for _, m := range ref.regex.FindAllStringSubmatchIndex(content, maxRenderedURLs) {
			if seen >= maxRenderedURLs {
				return findings
			}
			seen++
			rawURL := content[m[2*ref.urlGroup]:m[2*ref.urlGroup+1]]
			autoFetch := false
			switch {
			case ref.tagGroup > 0:
				tag := strings.ToLower(content[m[2*ref.tagGroup]:m[2*ref.tagGroup+1]])
				switch {
				case isAutoFetchTag(tag):
					autoFetch = true
				case isClickTag(tag):
				default:
					continue
				}
			case ref.bangGroup > 0:
				autoFetch = m[2*ref.bangGroup+1] > m[2*ref.bangGroup]
			default:
				// A reference definition renders as an image when any
				// image-reference syntax exists in the same output.
				autoFetch = hasImageRef
			}
			if f, ok := d.assessRenderedURL(rawURL, autoFetch, ref.markdown, placeholders); ok {
				findings = append(findings, f)
			}
		}
	}
	return findings
}

// assessRenderedURL decides whether one rendered URL carries data.
func (d *ExfilDetector) assessRenderedURL(rawURL string, autoFetch, markdown bool, placeholders []string) (security.Finding, bool) {
	if strings.HasPrefix(rawURL, "<") && strings.HasSuffix(rawURL, ">") {
		rawURL = rawURL[1 : len(rawURL)-1]
	}
	lower := strings.ToLower(rawURL)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
	case strings.HasPrefix(lower, "//"):
		rawURL = "https:" + rawURL
	default:
		// data:, mailto:, relative paths and fragments reach no third party.
		return security.Finding{}, false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return security.Finding{}, false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return security.Finding{}, false
	}
	if _, trusted := d.trustedHosts[host]; trusted {
		return security.Finding{}, false
	}

	parts := urlDataParts(u)
	signal := ""
	// A tokenized placeholder inside a URL is restored to the real value
	// before the client renders it, so the URL would carry that value.
	for _, ph := range placeholders {
		if strings.Contains(rawURL, ph) || anyContains(parts, ph) {
			signal = "tokenized_value"
			break
		}
	}

	b64Min, hexMin := linkBase64BlobMin, linkHexBlobMin
	if autoFetch {
		b64Min, hexMin = imageBase64BlobMin, imageHexBlobMin
	}
	if signal == "" {
		for _, part := range parts {
			if hasEncodedBlob(part, b64Min, hexMin) {
				signal = "encoded_payload"
				break
			}
			// Sentences in an image URL have no rendering purpose. Search
			// links legitimately carry prose, so links are exempt.
			if autoFetch && looksLikeProse(part) {
				signal = "conversation_text"
				break
			}
		}
	}
	if signal == "" {
		return security.Finding{}, false
	}

	kind := "link"
	severity, confidence := security.SeverityMedium, 0.8
	if autoFetch {
		kind = "image"
		severity, confidence = security.SeverityHigh, 0.9
	}
	if signal == "tokenized_value" {
		severity, confidence = security.SeverityCritical, 0.95
	}
	syntax := "html"
	if markdown {
		syntax = "markdown"
	}
	category := syntax + "_" + kind + "_exfil"
	// Report the destination only. The query, path and fragment are the
	// leaked data; persisting them would copy it into the threat log.
	shown := u.Scheme + "://" + u.Host + "/…"
	if len(shown) > renderedURLMaxReport {
		shown = shown[:renderedURLMaxReport] + "…"
	}
	return security.Finding{
		ThreatType:     security.ThreatType("exfil"),
		DetectorName:   "exfil",
		SubCategory:    host,
		Severity:       severity,
		Score:          severityToScore(severity),
		Confidence:     confidence,
		MatchedPattern: category,
		MatchedContent: shown,
		Action:         security.ActionBlock,
		Message:        "Potential data exfiltration in response: rendered " + kind + " URL to " + host + " carries " + strings.ReplaceAll(signal, "_", " "),
	}, true
}

func anyContains(parts []string, needle string) bool {
	for _, p := range parts {
		if strings.Contains(p, needle) {
			return true
		}
	}
	return false
}

// urlDataParts returns the decoded places a URL can carry data: each
// query value, each path segment and the fragment.
func urlDataParts(u *url.URL) []string {
	var parts []string
	for _, values := range u.Query() {
		parts = append(parts, values...)
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	if u.Fragment != "" {
		parts = append(parts, u.Fragment)
	}
	return parts
}

// hasEncodedBlob reports whether s holds a run that looks like encoded
// data: base64/base64url with mixed case, digits and high entropy, or a
// long hex string. Slugs, UUIDs, commit SHAs, SHA-256 digests and
// document IDs fall below these bars.
func hasEncodedBlob(s string, b64Min, hexMin int) bool {
	if len(s) < b64Min && len(s) < hexMin {
		return false
	}
	start := -1
	for i := 0; i <= len(s); i++ {
		if i < len(s) && isBase64Char(s[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if blobRun(s[start:i], b64Min, hexMin) {
				return true
			}
			start = -1
		}
	}
	return false
}

func isBase64Char(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '_' || c == '-' || c == '='
}

func blobRun(run string, b64Min, hexMin int) bool {
	run = strings.TrimRight(run, "=")
	if len(run) >= hexMin && isHex(run) {
		return true
	}
	if len(run) < b64Min {
		return false
	}
	var upper, lowerCase, digit bool
	for i := 0; i < len(run); i++ {
		c := run[i]
		switch {
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= 'a' && c <= 'z':
			lowerCase = true
		case c >= '0' && c <= '9':
			digit = true
		}
	}
	return upper && lowerCase && digit && shannonEntropy(run) >= blobEntropyMin
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// looksLikeProse reports a decoded URL component that reads as a
// sentence: long, several spaces, mostly letters.
func looksLikeProse(s string) bool {
	if len(s) < proseMinLen || strings.Count(s, " ") < proseMinSpaces {
		return false
	}
	letters := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127 {
			letters++
		}
	}
	return letters*10 >= len(s)*6
}
