package security

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Canonical is the output of the preprocessing stage: the canonical
// text every detector sees, plus the list of transforms that produced
// it. Findings reference Transforms so logs show "base64-decoded →
// injection detected" rather than just "injection detected".
//
// The zero value (empty Transforms, Canonical == original) represents
// "no canonicalization applied" and is a valid state — CanonicalizeEnabled
// profiles simply pass through for content that had nothing to normalize.
type Canonical struct {
	Original   string
	Canonical  string
	Transforms []string
}

// Changed reports whether canonicalization altered the content.
func (c Canonical) Changed() bool { return c.Canonical != c.Original }

// Canonicalize applies the preprocessing pipeline: Unicode NFKC
// normalization → zero-width/invisible stripping → homoglyph folding →
// optional decoding of embedded base64/hex/URL-encoded payloads. The
// result is suitable for pattern matching by every detector.
//
// Each transform runs best-effort: if a step fails or produces noise,
// we fall back to the previous stage. The original text is always
// preserved on Canonical.Original so downstream logging and
// sanitization (mask/tokenize) can operate on the user-visible form.
//
// Design notes:
//   - We decode embedded base64/hex conservatively — only when the
//     blob is long enough and looks like a real payload, not a random
//     hash or identifier that happens to match the charset.
//   - Homoglyph folding uses a small hand-curated map for the Latin
//     letters most commonly used in injection bypasses (а, е, о, і,
//     с, р, х from Cyrillic; ο from Greek). Full ICU-grade folding
//     is on the roadmap.
func Canonicalize(s string) Canonical {
	out := Canonical{Original: s, Canonical: s}
	if s == "" {
		return out
	}

	// 1. Unicode NFKC normalization — collapses compatibility forms
	//    (e.g. fullwidth letters, ligatures) into their canonical
	//    ASCII-equivalent shape where possible.
	if normed := norm.NFKC.String(out.Canonical); normed != out.Canonical {
		out.Canonical = normed
		out.Transforms = append(out.Transforms, "nfkc")
	}

	// 2. Strip invisible characters (zero-width space, joiners, BOM,
	//    RTL/LTR marks). Attackers slip these between letters to
	//    defeat regex: "i\u200Bgnore previous".
	if stripped := stripInvisible(out.Canonical); stripped != out.Canonical {
		out.Canonical = stripped
		out.Transforms = append(out.Transforms, "strip-invisible")
	}

	// 3. Fold common Cyrillic/Greek homoglyphs to Latin equivalents.
	//    Narrow on purpose — we want to catch "Ignоre" (Cyrillic о)
	//    without mangling legitimate non-English text.
	if folded := foldHomoglyphs(out.Canonical); folded != out.Canonical {
		out.Canonical = folded
		out.Transforms = append(out.Transforms, "fold-homoglyphs")
	}

	// 4. Decode embedded base64/hex/URL-encoded payloads. Appended to
	//    the canonical text with a separator so the regex can still
	//    see the original surface form plus the decoded payload.
	if decoded, applied := decodeEmbedded(out.Canonical); applied != "" {
		out.Canonical = out.Canonical + "\n\n[DECODED:" + applied + "]\n" + decoded
		out.Transforms = append(out.Transforms, applied)
	}

	return out
}

// stripInvisible removes characters that contribute no visible glyph
// but alter how strings match. Kept conservative — ordinary whitespace
// (space, tab, newline) is preserved because detectors reason about it.
func stripInvisible(s string) string {
	if !strings.ContainsAny(s, "\u200B\u200C\u200D\u200E\u200F\u202A\u202B\u202C\u202D\u202E\u2060\uFEFF\u180E\u00AD") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 0x200B, 0x200C, 0x200D, // zero-width space, non-joiner, joiner
			0x200E, 0x200F, // LTR/RTL mark
			0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // LTR/RTL embedding/override
			0x2060, 0xFEFF, 0x180E, 0x00AD: // word joiner, BOM, Mongolian vowel separator, soft hyphen
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// homoglyphMap is deliberately small — only the characters attackers
// actually use. Full-coverage Unicode confusables would require ICU
// and risks mangling legitimate international text.
var homoglyphMap = map[rune]rune{
	// Cyrillic lookalikes → Latin
	'а': 'a', 'А': 'A',
	'е': 'e', 'Е': 'E',
	'о': 'o', 'О': 'O',
	'р': 'p', 'Р': 'P',
	'с': 'c', 'С': 'C',
	'у': 'y', 'У': 'Y',
	'х': 'x', 'Х': 'X',
	'і': 'i', 'І': 'I',
	'ј': 'j', 'Ј': 'J',
	'ѕ': 's', 'Ѕ': 'S',
	// Greek lookalikes → Latin
	'ο': 'o', 'Ο': 'O',
	'α': 'a', 'Α': 'A',
	'ν': 'v', 'Ν': 'N',
	'ρ': 'p', 'Ρ': 'P',
	'τ': 't', 'Τ': 'T',
	'ι': 'i', 'Ι': 'I',
	'κ': 'k', 'Κ': 'K',
	'χ': 'x', 'Χ': 'X',
}

func foldHomoglyphs(s string) string {
	// Fast path: if the string is pure ASCII, there's nothing to fold.
	if isASCII(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if mapped, ok := homoglyphMap[r]; ok {
			b.WriteRune(mapped)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// base64Candidate matches a standalone-looking base64 blob. We require
// at least 20 chars to avoid decoding random IDs and typical minimum
// attack payloads like "ignore previous instructions" (base64: 40 chars).
// Strict character set; allow up to two '=' padding.
var base64Candidate = regexp.MustCompile(`[A-Za-z0-9+/]{20,}={0,2}`)

// hexCandidate matches standalone hex strings long enough to carry a
// short English sentence (10 chars → 5 bytes, 40 chars → 20 bytes).
var hexCandidate = regexp.MustCompile(`\b(?:[0-9a-fA-F]{2}\s?){10,}\b`)

// urlEncodedCandidate matches strings with enough %xx sequences to be
// a real encoded payload rather than a URL fragment.
var urlEncodedCandidate = regexp.MustCompile(`(?:%[0-9a-fA-F]{2}){5,}`)

// decodeEmbedded looks for the first promising encoded blob and tries
// to decode it into printable text. Returns the decoded payload and a
// short tag describing which decoder won. Empty string if nothing
// productive was found.
//
// We deliberately do not try every encoding — that explodes false
// positives. One productive decode per message is enough; the pattern
// library sees the decoded text and the engine records the transform.
func decodeEmbedded(s string) (string, string) {
	// URL encoding first — cheapest to check, very low false-positive rate.
	if urlEncodedCandidate.MatchString(s) {
		if decoded, err := url.QueryUnescape(s); err == nil && looksMeaningful(decoded) && decoded != s {
			return decoded, "url-decoded"
		}
	}

	// base64 — highest signal for real attack payloads.
	if m := base64Candidate.FindString(s); m != "" {
		if decoded, ok := tryBase64(m); ok {
			return decoded, "base64-decoded"
		}
	}

	// hex — rarer but occasionally used to hide payloads.
	if m := hexCandidate.FindString(s); m != "" {
		compact := strings.ReplaceAll(m, " ", "")
		if b, err := hex.DecodeString(compact); err == nil {
			decoded := string(b)
			if looksMeaningful(decoded) {
				return decoded, "hex-decoded"
			}
		}
	}

	return "", ""
}

func tryBase64(s string) (string, bool) {
	// Try standard first, then URL-safe variant. RawStd omits padding;
	// some attacker payloads drop it to look less suspicious.
	for _, dec := range []*base64.Encoding{
		base64.StdEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.RawURLEncoding,
	} {
		if b, err := dec.DecodeString(s); err == nil {
			decoded := string(b)
			if looksMeaningful(decoded) {
				return decoded, true
			}
		}
	}
	return "", false
}

// looksMeaningful is a coarse filter: decoded payloads must be mostly
// printable ASCII and contain at least one alphabetic character. This
// avoids appending binary garbage to the canonical form when the
// decode happened to succeed on a random-looking but non-text blob.
func looksMeaningful(s string) bool {
	if len(s) < 4 {
		return false
	}
	hasAlpha := false
	printable := 0
	total := 0
	for _, r := range s {
		total++
		if unicode.IsLetter(r) {
			hasAlpha = true
		}
		if r == '\n' || r == '\t' || r == '\r' || (r >= 0x20 && r < 0x7F) {
			printable++
		}
	}
	if !hasAlpha {
		return false
	}
	// Require >=80% printable — cuts accidental decodes of random hashes.
	return float64(printable)/float64(total) >= 0.8
}
