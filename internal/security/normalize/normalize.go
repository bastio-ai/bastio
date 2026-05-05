// Package normalize preprocesses content before detectors run. It
// collapses the obvious evasion tricks — Unicode confusables, invisible
// characters, homoglyphs, base64/ROT13/leet smuggling — into a single
// canonical form that regex detectors can reliably match against.
//
// Every transformation is reversible in principle but we don't rehydrate
// — the original text still reaches the LLM provider untouched. We only
// rewrite the copy passed to detectors.
//
// Design notes:
//   - Fast path: pure ASCII with no suspicious markers returns unchanged
//     in a single pass, ~zero allocation. This is the 99% case.
//   - Decode pass is optional (profile-toggled). It appends decoded
//     variants to the normalized text rather than replacing, so the
//     original attack phrasing still matches alongside the decoded form.
//   - Flags describe what was done so detectors can surface findings
//     like "encoding.base64" with provenance.
package normalize

import (
	"encoding/base64"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Options controls which transformations run. Zero value is all-off;
// use DefaultOptions() for the secure default.
type Options struct {
	// Unicode enables NFKC normalization, invisible-char stripping,
	// homoglyph folding, and whitespace collapse.
	Unicode bool
	// Decode enables the base64 / ROT13 / leetspeak detection pass.
	// Heavier than Unicode; disable for translation or coding tools
	// where legitimate base64 content would otherwise trigger noise.
	Decode bool
}

// DefaultOptions turns everything on — secure by default.
func DefaultOptions() Options {
	return Options{Unicode: true, Decode: true}
}

// Flag is a stable identifier for a transformation that fired.
// Detectors can emit findings tagged with these so analytics can
// show "N% of blocked requests used base64 smuggling".
type Flag string

const (
	FlagNFKC                Flag = "nfkc"
	FlagInvisiblesStripped  Flag = "invisibles.stripped"
	FlagHomoglyphFolded     Flag = "homoglyph.folded"
	FlagWhitespaceCollapsed Flag = "whitespace.collapsed"
	FlagBase64Decoded       Flag = "decoded.base64"
	FlagROT13Decoded        Flag = "decoded.rot13"
	FlagLeetFolded          Flag = "decoded.leet"
)

// Result is what Normalize returns.
type Result struct {
	// Original is the input as received.
	Original string
	// Normalized is the canonical form for regex matching. When Decode
	// fires, decoded variants are appended with "\n[decoded:<kind>] ..."
	// separators so detectors see both the original payload and the
	// decoded plaintext in one pass.
	Normalized string
	// Flags records every transformation that actually fired. Empty on
	// the fast path (clean ASCII input).
	Flags []Flag
}

// HasFlag reports whether f is set on the result.
func (r Result) HasFlag(f Flag) bool {
	for _, x := range r.Flags {
		if x == f {
			return true
		}
	}
	return false
}

// Normalize runs the preprocessing pipeline. Never panics; on any
// internal failure it returns the original text with no flags, so
// detectors still get something to work with.
func Normalize(text string, opts Options) Result {
	r := Result{Original: text, Normalized: text}
	if text == "" {
		return r
	}

	// Fast path: pure ASCII, no suspicious markers — skip Unicode
	// normalization entirely. About 90% of real gateway traffic.
	if opts.Unicode && isFastPathSafe(text) {
		if opts.Decode {
			return runDecodePass(r)
		}
		return r
	}

	if opts.Unicode {
		r = runUnicodePass(r)
	}
	if opts.Decode {
		r = runDecodePass(r)
	}
	return r
}

// isFastPathSafe returns true when the text is plain printable ASCII
// with no control characters and no runs of whitespace that need
// collapsing. Such input can't be evading anything through Unicode
// tricks, so skip the full pipeline.
func isFastPathSafe(text string) bool {
	if len(text) > 2048 {
		return false
	}
	prevSpace := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			return false
		}
		if c >= 0x7f {
			return false
		}
		if c == ' ' || c == '\t' {
			if prevSpace {
				return false // consecutive whitespace needs collapsing
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
	}
	return true
}

func runUnicodePass(r Result) Result {
	text := r.Normalized

	// 1. NFKC: collapse compatibility decompositions (fullwidth → ASCII,
	// math alphanumerics → ASCII, ligatures → components).
	n := norm.NFKC.String(text)
	if n != text {
		r.Flags = append(r.Flags, FlagNFKC)
		text = n
	}

	// 2. Strip invisible chars: zero-width space/joiner, BOM, RTL/LTR
	// overrides, soft hyphens, directional isolates.
	stripped, invisiblesHit := stripInvisibles(text)
	if invisiblesHit {
		r.Flags = append(r.Flags, FlagInvisiblesStripped)
		text = stripped
	}

	// 3. Homoglyph fold: Cyrillic/Greek/fullwidth look-alikes → ASCII.
	folded, homoglyphHit := foldHomoglyphs(text)
	if homoglyphHit {
		r.Flags = append(r.Flags, FlagHomoglyphFolded)
		text = folded
	}

	// 4. Whitespace collapse: defeats "i g n o r e" character-split
	// evasions. Preserves newlines so many-shot structure survives.
	ws, wsHit := collapseWhitespace(text)
	if wsHit {
		r.Flags = append(r.Flags, FlagWhitespaceCollapsed)
		text = ws
	}

	r.Normalized = text
	return r
}

func stripInvisibles(text string) (string, bool) {
	var b strings.Builder
	b.Grow(len(text))
	hit := false
	for _, c := range text {
		if isInvisible(c) {
			hit = true
			continue
		}
		b.WriteRune(c)
	}
	if !hit {
		return text, false
	}
	return b.String(), true
}

func isInvisible(c rune) bool {
	if c < 0x80 {
		return false
	}
	switch c {
	case 0x00ad, // soft hyphen
		0x200b, // zero-width space
		0x200c, // zero-width non-joiner
		0x200d, // zero-width joiner
		0x200e, // LTR mark
		0x200f, // RTL mark
		0x202a, // LRE
		0x202b, // RLE
		0x202c, // PDF
		0x202d, // LRO
		0x202e, // RLO
		0x2060, // word joiner
		0x2061, // function application
		0x2062, // invisible times
		0x2063, // invisible separator
		0x2064, // invisible plus
		0x2066, // LRI
		0x2067, // RLI
		0x2068, // FSI
		0x2069, // PDI
		0xfeff: // BOM / zero-width no-break space
		return true
	}
	// Variation selectors.
	if c >= 0xfe00 && c <= 0xfe0f {
		return true
	}
	if c >= 0xe0100 && c <= 0xe01ef {
		return true
	}
	// Format category catches stragglers.
	if unicode.In(c, unicode.Cf) {
		return true
	}
	return false
}

// homoglyphs maps common look-alike codepoints to their ASCII twins.
// Not exhaustive — we target the specific glyphs attackers actually
// use, drawn from the Unicode Confusables list (UAX #39).
var homoglyphs = map[rune]rune{
	// Cyrillic → Latin.
	0x0430: 'a', 0x0410: 'A',
	0x0435: 'e', 0x0415: 'E',
	0x043e: 'o', 0x041e: 'O',
	0x0440: 'p', 0x0420: 'P',
	0x0441: 'c', 0x0421: 'C',
	0x0443: 'y', 0x0423: 'Y',
	0x0445: 'x', 0x0425: 'X',
	0x0432: 'B',
	0x043a: 'k', 0x041a: 'K',
	0x043c: 'M',
	0x043d: 'H',
	0x0442: 'T',
	// Greek.
	0x03b1: 'a', 0x0391: 'A',
	0x03bf: 'o', 0x039f: 'O',
	0x03c1: 'p', 0x03a1: 'P',
	0x03ba: 'k', 0x039a: 'K',
	0x03b2: 'B',
	0x0395: 'E',
	0x0396: 'Z',
	0x0397: 'H',
	0x0399: 'I',
	0x039c: 'M',
	0x039d: 'N',
	0x03a4: 'T',
	0x03a5: 'Y',
	0x03a7: 'X',
	// Fullwidth punctuation that NFKC missed.
	0xff0e: '.',
	0xff1a: ':',
	0xff1b: ';',
	0xff0c: ',',
	0xff1f: '?',
	0xff01: '!',
}

func foldHomoglyphs(text string) (string, bool) {
	// Quick reject — all-ASCII can't contain homoglyphs.
	if utf8.RuneCountInString(text) == len(text) {
		return text, false
	}
	var b strings.Builder
	b.Grow(len(text))
	hit := false
	for _, c := range text {
		if r, ok := homoglyphs[c]; ok {
			b.WriteRune(r)
			hit = true
			continue
		}
		b.WriteRune(c)
	}
	if !hit {
		return text, false
	}
	return b.String(), true
}

// spaceRunes enumerates the space variants treated as whitespace for
// collapse. NBSP and typographic spaces matter because attackers use
// them to split tokens ("i g n o r e").
var spaceRunes = map[rune]struct{}{
	' ':    {},
	'\t':   {},
	0x00a0: {}, // NBSP
	0x1680: {}, // Ogham space
	0x2000: {}, // en quad
	0x2001: {}, // em quad
	0x2002: {}, // en space
	0x2003: {}, // em space
	0x2004: {}, // three-per-em
	0x2005: {}, // four-per-em
	0x2006: {}, // six-per-em
	0x2007: {}, // figure space
	0x2008: {}, // punctuation space
	0x2009: {}, // thin space
	0x200a: {}, // hair space
	0x202f: {}, // narrow NBSP
	0x205f: {}, // medium math space
	0x3000: {}, // ideographic space
}

func collapseWhitespace(text string) (string, bool) {
	var b strings.Builder
	b.Grow(len(text))
	prevSpace := false
	hit := false
	for _, c := range text {
		_, isSpace := spaceRunes[c]
		if isSpace {
			if prevSpace {
				hit = true
				continue
			}
			if c != ' ' {
				hit = true
			}
			b.WriteRune(' ')
			prevSpace = true
			continue
		}
		b.WriteRune(c)
		prevSpace = false
	}
	if !hit {
		return text, false
	}
	return b.String(), true
}

// runDecodePass appends decoded variants of suspected-encoded runs to
// the normalized text. We never replace — the original must still be
// matchable, and we want the encoding flag to fire alongside content
// findings on the decoded plaintext.
func runDecodePass(r Result) Result {
	appended := make([]string, 0, 3)

	if decoded, ok := tryDecodeBase64(r.Normalized); ok {
		r.Flags = append(r.Flags, FlagBase64Decoded)
		appended = append(appended, "[decoded:base64] "+decoded)
	}
	if decoded, ok := tryDecodeROT13(r.Normalized); ok {
		r.Flags = append(r.Flags, FlagROT13Decoded)
		appended = append(appended, "[decoded:rot13] "+decoded)
	}
	if decoded, ok := tryFoldLeet(r.Normalized); ok {
		r.Flags = append(r.Flags, FlagLeetFolded)
		appended = append(appended, "[decoded:leet] "+decoded)
	}

	if len(appended) > 0 {
		r.Normalized = r.Normalized + "\n" + strings.Join(appended, "\n")
	}
	return r
}

// tryDecodeBase64 scans for base64-like runs of 20+ chars and decodes
// them. Returns the concatenated decoded plaintexts only if they're
// printable ASCII with ≥3 alpha chars — pure-binary decodes are noise.
func tryDecodeBase64(text string) (string, bool) {
	runs := extractBase64Runs(text, 20)
	if len(runs) == 0 {
		return "", false
	}
	var out []string
	for _, run := range runs {
		decoded, err := base64.StdEncoding.DecodeString(padBase64(run))
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(padBase64(run))
			if err != nil {
				continue
			}
		}
		s := string(decoded)
		if !looksLikePlaintext(s) {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, " "), true
}

func extractBase64Runs(text string, minLen int) []string {
	var runs []string
	var b strings.Builder
	flush := func() {
		if b.Len() >= minLen {
			runs = append(runs, b.String())
		}
		b.Reset()
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '-' || c == '_' {
			b.WriteByte(c)
			continue
		}
		flush()
	}
	flush()
	return runs
}

func padBase64(s string) string {
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return s
}

func looksLikePlaintext(s string) bool {
	if len(s) < 4 {
		return false
	}
	alpha := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
		if c > 0x7e {
			return false
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			alpha++
		}
	}
	return alpha >= 3
}

// tryDecodeROT13 applies ROT13 and returns the result only if it looks
// more like English prose than the input.
func tryDecodeROT13(text string) (string, bool) {
	if len(text) < 20 {
		return "", false
	}
	rotated := rot13(text)
	if !moreEnglishLike(rotated, text) {
		return "", false
	}
	return rotated, true
}

func rot13(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteRune('a' + (c-'a'+13)%26)
		case c >= 'A' && c <= 'Z':
			b.WriteRune('A' + (c-'A'+13)%26)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

var englishStopwords = []string{" the ", " and ", " your ", " you ", " ignore ", " previous ", " system ", " prompt ", " instructions "}

func moreEnglishLike(candidate, original string) bool {
	c := strings.ToLower(" " + candidate + " ")
	o := strings.ToLower(" " + original + " ")
	cHits, oHits := 0, 0
	for _, w := range englishStopwords {
		if strings.Contains(c, w) {
			cHits++
		}
		if strings.Contains(o, w) {
			oHits++
		}
	}
	return cHits >= 2 && cHits > oHits
}

// tryFoldLeet replaces common leet substitutions back to letters. Only
// fires when the input has a dense-enough concentration of digit-for-
// letter substitutions and the fold produces more English words than
// the original.
func tryFoldLeet(text string) (string, bool) {
	digits := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '0' || c == '1' || c == '3' || c == '4' || c == '5' || c == '7' {
			digits++
		}
	}
	// ≥3 leet-candidate digits and ≥5% density.
	if digits < 3 || digits*20 < len(text) {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(text))
	leet := map[byte]byte{'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't'}
	for i := 0; i < len(text); i++ {
		if r, ok := leet[text[i]]; ok {
			b.WriteByte(r)
			continue
		}
		b.WriteByte(text[i])
	}
	folded := b.String()
	if !moreEnglishLike(folded, text) {
		return "", false
	}
	return folded, true
}
