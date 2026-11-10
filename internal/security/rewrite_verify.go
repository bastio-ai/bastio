package security

import (
	"context"
	"encoding/hex"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/bastio-ai/bastio/internal/security/normalize"
)

// Rewrite verification.
//
// Detectors match a canonical view of the text (NFKC, invisible
// characters stripped, homoglyphs folded), but mask/redact rewrite the
// original text with the detector's own patterns. When the original hides
// a match behind something canonicalization removes — a zero-width
// character or a full-width letter inside a key — the detector fires and
// the rewrite silently changes nothing. After a rewrite step the engine
// therefore re-runs the detector on the canonical view of the rewritten
// text; if it still fires, it maps the canonical spans the sanitizer
// would replace back to original byte ranges and replaces those (one
// attempt, no loop). If the detector still fires after that, the rewrite
// is unapplied and the caller fails closed.

// RewriteSpan is one replacement a sanitizer makes: content[Start:End]
// becomes Replacement. Offsets are byte offsets into the scanned content.
type RewriteSpan struct {
	Start, End  int
	Replacement string
}

// SpanSanitizer is optionally implemented by sanitizers that can report
// where their mask applies. The engine uses it to apply a mask found on
// the canonical view to the matching bytes of the original text.
// Detectors without it fail closed when their rewrite does not apply.
type SpanSanitizer interface {
	RewriteSpans(ctx context.Context, content string) []RewriteSpan
}

// srcRange is the original byte range one byte of a mapped view came from.
type srcRange struct{ start, end int }

// mappedText is a transformed view of a source string that records, for
// every byte of text, the source byte range that produced it. All stages
// are order-preserving, so ranges are monotonic.
type mappedText struct {
	text string
	src  []srcRange
}

func identityMapped(s string) mappedText {
	src := make([]srcRange, len(s))
	for i := range src {
		src[i] = srcRange{i, i + 1}
	}
	return mappedText{text: s, src: src}
}

// nfkc applies NFKC segment by segment. Each output byte maps to the
// union of the source ranges of the input segment it came from, so
// compositions across characters stay covered.
func (m mappedText) nfkc() mappedText {
	var it norm.Iter
	it.InitString(norm.NFKC, m.text)
	var b strings.Builder
	b.Grow(len(m.text))
	src := make([]srcRange, 0, len(m.src))
	// A decomposition (ligature, ellipsis) is emitted over several Next
	// calls that do not advance Pos; its bytes stay pending until Pos
	// moves and then all map to the consumed input range.
	segStart, pending := 0, 0
	for !it.Done() {
		seg := it.Next()
		b.Write(seg)
		pending += len(seg)
		if p1 := it.Pos(); p1 > segStart {
			r := srcRange{m.src[segStart].start, m.src[p1-1].end}
			for range pending {
				src = append(src, r)
			}
			segStart, pending = p1, 0
		}
	}
	if pending > 0 {
		// Cannot happen once Done; keep the map length consistent and let
		// the caller's pipeline comparison reject it.
		return mappedText{}
	}
	return mappedText{text: b.String(), src: src}
}

// perRune rewrites each rune with f; keep=false drops it.
func (m mappedText) perRune(f func(r rune) (out rune, keep bool)) mappedText {
	var b strings.Builder
	b.Grow(len(m.text))
	src := make([]srcRange, 0, len(m.src))
	for i := 0; i < len(m.text); {
		r, size := utf8.DecodeRuneInString(m.text[i:])
		out, keep := f(r)
		if keep {
			rng := srcRange{m.src[i].start, m.src[i+size-1].end}
			n, _ := b.WriteRune(out)
			for range n {
				src = append(src, rng)
			}
		}
		i += size
	}
	return mappedText{text: b.String(), src: src}
}

// canonicalStages mirrors canonicalUnicode (canonical.go).
func (m mappedText) canonicalStages() mappedText {
	return m.nfkc().perRune(func(r rune) (rune, bool) {
		if isCanonicalInvisible(r) {
			return 0, false
		}
		if f, ok := homoglyphMap[r]; ok {
			return f, true
		}
		return r, true
	})
}

// normalizeStages mirrors normalize's Unicode pass (runUnicodePass).
func (m mappedText) normalizeStages() mappedText {
	m = m.nfkc().perRune(func(r rune) (rune, bool) {
		if normalize.IsInvisible(r) {
			return 0, false
		}
		if f, ok := normalize.FoldRune(r); ok {
			return f, true
		}
		return r, true
	})
	var prevSpace bool
	return m.perRune(func(r rune) (rune, bool) {
		if normalize.IsCollapsibleSpace(r) {
			if prevSpace {
				return 0, false
			}
			prevSpace = true
			return ' ', true
		}
		prevSpace = false
		return r, true
	})
}

// rewriteView names the canonical view a rewrite is verified against.
type rewriteView int

const (
	// viewCanonical is the mappable part of Canonicalize — what RunSteps
	// detectors see, minus the decoded-payload suffix.
	viewCanonical rewriteView = iota
	// viewGateway is normalize's Unicode pass followed by canonicalUnicode
	// — what the gateway scan saw for text it later rewrites field by field
	// (SanitizeDecisions).
	viewGateway
)

// detectText returns the text the detector is re-run on. Both views
// exclude decoded payloads (base64, URL-encoded, hex): a finding that only
// exists once a payload is decoded is not a missed rewrite of visible
// text, and finishEncodedScan already owns it — it withholds the content
// with RewriteUnavailable ("could not be safely rewritten"). Verifying the
// suffix here would pre-empt that contract with a generic block.
func (v rewriteView) detectText(s string) string {
	if v == viewCanonical {
		return canonicalUnicode(s)
	}
	return gatewayView(s)
}

func gatewayView(s string) string {
	n := normalize.Normalize(s, normalize.Options{Unicode: true}).Normalized
	return canonicalUnicode(n)
}

// mapped builds the offset map for the mappable part of the view and
// checks it reproduces the real pipeline exactly; false means the text
// cannot be mapped (callers fail closed).
func (v rewriteView) mapped(s string) (mappedText, bool) {
	m := identityMapped(s)
	if v == viewGateway {
		if n := normalize.Normalize(s, normalize.Options{Unicode: true}).Normalized; n != s {
			m = m.normalizeStages()
			if m.text != n {
				return mappedText{}, false
			}
		}
	}
	want := canonicalUnicode(m.text)
	m = m.canonicalStages()
	if m.text != want {
		return mappedText{}, false
	}
	return m, true
}

// splice applies spans found on m.text to the original text m maps from.
// Each span covers the full original byte range of its first through last
// canonical byte, including invisible characters inside it.
func (m mappedText) splice(original string, spans []RewriteSpan) (string, bool) {
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].Start != spans[j].Start {
			return spans[i].Start < spans[j].Start
		}
		return spans[i].End > spans[j].End
	})
	var b strings.Builder
	b.Grow(len(original))
	last, applied := 0, false
	for _, s := range spans {
		if s.Start < 0 || s.End > len(m.src) || s.Start >= s.End {
			continue
		}
		start, end := m.src[s.Start].start, m.src[s.End-1].end
		if start < last || end > len(original) {
			continue // overlaps a span already replaced
		}
		b.WriteString(original[last:start])
		b.WriteString(s.Replacement)
		last, applied = end, true
	}
	b.WriteString(original[last:])
	return b.String(), applied
}

// rewriteCheck carries what verification needs about one fired step.
type rewriteCheck struct {
	det          Detector
	threshold    float64
	classes      map[string]bool // finding classes the step fired on; nil = any
	suppressions []PatternSuppression
	view         rewriteView
}

func findingClass(f Finding) string {
	return f.DetectorName + "\x00" + f.MatchedPattern + "\x00" + f.SubCategory
}

func findingClasses(findings []Finding) map[string]bool {
	out := make(map[string]bool, len(findings))
	for _, f := range findings {
		out[findingClass(f)] = true
	}
	return out
}

// stillFires re-runs the detector on view text. A detector error counts
// as firing: verification fails closed.
func (c rewriteCheck) stillFires(ctx context.Context, view string) bool {
	findings, err := c.det.Detect(ctx, view)
	if err != nil {
		return true
	}
	findings = filterSuppressed(findings, c.suppressions)
	for _, f := range findings {
		if f.Score*f.Confidence < c.threshold {
			continue
		}
		if c.classes == nil || c.classes[findingClass(f)] {
			return true
		}
	}
	return false
}

// verifyOutcome is what verification concluded about one rewrite step.
type verifyOutcome int

const (
	// outcomeApplied: nothing the step fired on remains.
	outcomeApplied verifyOutcome = iota
	// outcomeMaskFallback: a tokenize step missed an obfuscated value and
	// the located span was masked one-way instead (not restorable).
	outcomeMaskFallback
	// outcomeEncodedMasked: the value existed only inside an encoded
	// payload (base64, hex, %-escapes); that payload was replaced whole.
	outcomeEncodedMasked
	// outcomeUnverifiable: visible text still carries the value and it
	// could not be located on the original bytes.
	outcomeUnverifiable
	// outcomeEncodedUnmasked: the value exists only inside an encoded
	// payload that could not be located.
	outcomeEncodedUnmasked
)

// maxEncodedCandidates bounds the decoded-payload checks per step.
const maxEncodedCandidates = 8

// percentToken is a whitespace/quote-delimited token holding %-escapes;
// percentValue is the narrower URL component (query value, path segment)
// so "q=<value>&page=2" loses only the value.
var (
	percentToken = regexp.MustCompile(`[^\s"'<>]*%[0-9A-Fa-f]{2}[^\s"'<>]*`)
	percentValue = regexp.MustCompile(`[^\s"'<>&=?#/]*%[0-9A-Fa-f]{2}[^\s"'<>&=?#/]*`)
	// base64Run matches standard and URL-safe base64 (Gmail bodies,
	// JWT-style segments) the way the normalize decoder extracts runs.
	base64Run = regexp.MustCompile(`[A-Za-z0-9+/_-]{20,}={0,2}`)
)

// fullText is the complete view the scan matched, including the decoded
// payload suffix Canonicalize appends.
func (v rewriteView) fullText(s string) string {
	if v == viewCanonical {
		return Canonicalize(s).Canonical
	}
	// The gateway scan saw normalize's Unicode + decode pass, then
	// Canonicalize.
	return Canonicalize(normalize.Normalize(s, normalize.DefaultOptions()).Normalized).Canonical
}

// verify checks rewritten (the sanitizer's output). prefixGap says the
// per-character view differed from the text the sanitizer saw, so the
// visible text needs re-checking; the decoded-payload suffix is checked
// whenever the view has one. Bounded: one visible re-check + one remap +
// one re-check, then one decoded check + at most maxEncodedCandidates
// payload checks + one re-check. No loops over the whole text.
func (c rewriteCheck) verify(ctx context.Context, rewritten string, prefixGap, tokenize bool) (string, verifyOutcome) {
	kept, outcome := rewritten, outcomeApplied
	if prefixGap && c.stillFires(ctx, c.view.detectText(rewritten)) {
		ss, ok := c.det.(SpanSanitizer)
		if !ok {
			return rewritten, outcomeUnverifiable
		}
		m, ok := c.view.mapped(rewritten)
		if !ok {
			return rewritten, outcomeUnverifiable
		}
		// Spans carry one-way mask replacements, so a tokenize step that
		// lands here becomes a mask: a placeholder for the canonical form
		// would restore text the user never sent.
		remapped, changed := m.splice(rewritten, ss.RewriteSpans(ctx, m.text))
		if !changed || c.stillFires(ctx, c.view.detectText(remapped)) {
			return rewritten, outcomeUnverifiable
		}
		kept = remapped
		if tokenize {
			outcome = outcomeMaskFallback
		}
	}

	full := c.view.fullText(kept)
	if full == c.view.detectText(kept) || !c.stillFires(ctx, full) {
		return kept, outcome
	}
	m, ok := c.view.mapped(kept)
	if !ok {
		return kept, outcomeEncodedUnmasked
	}
	placeholder := "[REDACTED_ENCODED_" + strings.ToUpper(c.det.Name()) + "]"
	var spans []RewriteSpan
	for _, cand := range encodedCandidates(m.text) {
		// Candidates come narrowest first; a wider token overlapping an
		// accepted span would only remove more surrounding text.
		if overlapsAny(spans, cand.start, cand.end) {
			continue
		}
		if c.stillFires(ctx, gatewayView(cand.decoded)) {
			spans = append(spans, RewriteSpan{Start: cand.start, End: cand.end, Replacement: placeholder})
		}
	}
	blobbed, changed := m.splice(kept, spans)
	if !changed || c.stillFires(ctx, c.view.fullText(blobbed)) {
		return kept, outcomeEncodedUnmasked
	}
	return blobbed, outcomeEncodedMasked
}

type encodedCandidate struct {
	start, end int
	decoded    string
}

// encodedCandidates finds the payloads the scan's decoders decode
// (%-escaped URL components, falling back to the whole token; standard and
// URL-safe base64; hex) with their decoded text, narrowest first so the
// splice keeps as much surrounding text as possible. Bounded by
// maxEncodedCandidates.
func encodedCandidates(text string) []encodedCandidate {
	var out []encodedCandidate
	add := func(start, end int, decoded string) {
		if len(out) < maxEncodedCandidates {
			out = append(out, encodedCandidate{start, end, decoded})
		}
	}
	for _, re := range []*regexp.Regexp{percentValue, percentToken} {
		for _, m := range re.FindAllStringIndex(text, maxEncodedCandidates) {
			if decoded, ok := normalize.DecodeURLText(text[m[0]:m[1]]); ok {
				add(m[0], m[1], decoded)
			}
		}
	}
	for _, m := range base64Run.FindAllStringIndex(text, maxEncodedCandidates) {
		if decoded, ok := tryBase64(text[m[0]:m[1]]); ok {
			add(m[0], m[1], decoded)
		}
	}
	for _, m := range hexCandidate.FindAllStringIndex(text, maxEncodedCandidates) {
		if b, err := hex.DecodeString(strings.ReplaceAll(text[m[0]:m[1]], " ", "")); err == nil && looksMeaningful(string(b)) {
			add(m[0], m[1], string(b))
		}
	}
	return out
}

// decisionResult reports what sanitizeDecisions could not do.
type decisionResult struct {
	// failed: rewrites that could not be applied (callers fail closed).
	failed []string
	// encoded: detectors whose finding sat only in an encoded payload that
	// was replaced whole with a placeholder.
	encoded []string
	// maskedInstead: tokenize steps that fell back to a one-way mask.
	maskedInstead []string
}

// sanitizeDecisions is SanitizeDecisions with a context and a report of
// what could not be rewritten. After each rewrite it verifies the result
// against the gateway view when that view differs from the text the
// sanitizer saw or carries a decoded payload.
func (e *Engine) sanitizeDecisions(ctx context.Context, content string, steps []Step, actions map[string]Action, tm *TokenMap, response bool) (string, decisionResult) {
	byName := make(map[string]Detector, len(e.detectors))
	for _, det := range e.detectors {
		byName[det.Name()] = det
	}
	var res decisionResult
	for _, step := range steps {
		action := actions[step.Detector]
		if action != ActionTokenize && !action.IsMaskLike() {
			continue
		}
		det, ok := byName[step.Detector]
		if !ok {
			res.failed = append(res.failed, step.Detector)
			continue
		}
		sanitizer, ok := det.(Sanitizer)
		if !ok {
			res.failed = append(res.failed, step.Detector)
			continue
		}
		before := content
		// Secrets are never tokenized: a reversible placeholder would let
		// the response restore path put the secret back. The API and the
		// migration 007 CHECK reject secrets_strategy=tokenize; this keeps
		// any other route to that value (overlays, code-built profiles)
		// from reaching the TokenMap.
		tokenize := action == ActionTokenize && !response && step.Detector != "secrets"
		if tokenize {
			content = sanitizer.TokenizeInto(content, tm)
		} else {
			content = sanitizer.Mask(content)
		}
		view := gatewayView(before)
		prefixGap := view != before
		// No gap between what the scan matched and what the sanitizer saw
		// and no decoded payload: nothing to verify (the common case).
		if !prefixGap && viewGateway.fullText(before) == view {
			continue
		}
		check := rewriteCheck{det: det, threshold: step.Threshold, view: viewGateway}
		next, outcome := check.verify(ctx, content, prefixGap, tokenize)
		content = next
		switch outcome {
		case outcomeMaskFallback:
			res.maskedInstead = append(res.maskedInstead, step.Detector)
		case outcomeUnverifiable:
			res.failed = append(res.failed, step.Detector)
		case outcomeEncodedMasked:
			res.encoded = append(res.encoded, step.Detector)
		case outcomeEncodedUnmasked:
			// Callers forward what this returns, so an encoded value that
			// could not be located fails closed for every detector here.
			res.failed = append(res.failed, step.Detector)
		}
	}
	return content, res
}

// failsClosedWhenEncoded reports whether a value found only inside an
// encoded payload that cannot be masked must withhold the content:
// secrets always, exfiltration on the response side (data leaving).
// PII and request-side exfil are forwarded and reported as warn.
func failsClosedWhenEncoded(detector string, response bool) bool {
	return detector == "secrets" || (detector == "exfil" && response)
}

// blockUnverifiedInput fails a request-side scan closed when the caller
// will rewrite the original text itself (SkipSanitization) and the
// declared rewrites cannot be applied to it: the request is blocked with
// the same result shape a block strategy produces, rather than forwarded
// with what the engine believes is still a secret.
func (e *Engine) blockUnverifiedInput(ctx context.Context, req *ScanRequest, result *ScanResult, original string) {
	if !req.SkipSanitization || result.ShouldBlock {
		return
	}
	// RunSteps forwards (as warn) a value it found only in an encoded
	// payload it could not locate; a caller that rewrites the original
	// itself must not forward it.
	if len(result.UnmaskedEncoded) > 0 {
		blockRewriteUnavailable(result, result.UnmaskedEncoded)
		return
	}
	if original == req.Content {
		return
	}
	if result.Action != ActionTokenize && !result.Action.IsMaskLike() {
		return
	}
	_, dr := e.sanitizeDecisions(ctx, original, req.Steps, result.DetectorActions, NewTokenMap(TokenStyleAngle), req.Role == RoleAssistant)
	if len(dr.failed) == 0 {
		for _, d := range dr.maskedInstead {
			if result.DetectorActions[d] == ActionTokenize {
				result.DetectorActions[d] = ActionMask
			}
		}
		result.EncodedMasked = appendUnique(result.EncodedMasked, dr.encoded...)
		return
	}
	blockRewriteUnavailable(result, dr.failed)
}

// blockRewriteUnavailable withholds content because the configured rewrite
// could not be applied safely, recording block for those detectors.
func blockRewriteUnavailable(result *ScanResult, failed []string) {
	result.ShouldBlock = true
	result.Action = ActionBlock
	// The tenant configured a rewrite, not a block: callers report this as
	// "could not be safely rewritten, withheld" (RewriteUnavailable).
	result.RewriteUnavailable = true
	if result.DetectorActions == nil {
		result.DetectorActions = make(map[string]Action)
	}
	for _, d := range failed {
		result.DetectorActions[d] = ActionBlock
		result.BlockingDetectors = append(result.BlockingDetectors, d)
	}
}

func appendUnique(dst []string, values ...string) []string {
	for _, v := range values {
		if !slices.Contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}

func overlapsAny(spans []RewriteSpan, start, end int) bool {
	for _, s := range spans {
		if start < s.End && s.Start < end {
			return true
		}
	}
	return false
}
