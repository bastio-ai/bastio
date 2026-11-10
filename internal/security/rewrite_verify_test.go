package security

import (
	"strings"
	"testing"
)

// The offset map must reproduce the real pipelines byte for byte, or
// spans found on the view would land on the wrong original bytes.
func TestMappedViewsMatchPipelines(t *testing.T) {
	inputs := []string{
		"",
		"plain ascii text",
		"tab\tand  double  spaces",
		"zero\u200bwidth\u200djoiner\u2060word",
		"\uff53\uff4b\uff0d full-width \uff21\uff22\uff23",
		"Cyrillic \u043e and Greek \u03bf and \u0456",
		"ligature \ufb01ne and \u2026 ellipsis\u00a0nbsp",
		"combining e\u0301 and e\u200b\u0301 split",
		"soft\u00adhyphen and BOM\ufeff",
	}
	for _, in := range inputs {
		m, ok := viewCanonical.mapped(in)
		if !ok || m.text != canonicalUnicode(in) {
			t.Errorf("canonical map %q: ok=%v got %q want %q", in, ok, m.text, canonicalUnicode(in))
		}
		if len(m.src) != len(m.text) {
			t.Errorf("canonical map %q: %d ranges for %d bytes", in, len(m.src), len(m.text))
		}
		g, ok := viewGateway.mapped(in)
		if !ok || g.text != gatewayView(in) {
			t.Errorf("gateway map %q: ok=%v got %q want %q", in, ok, g.text, gatewayView(in))
		}
		for i, r := range g.src {
			if r.start < 0 || r.end > len(in) || r.start >= r.end || (i > 0 && r.start < g.src[i-1].start) {
				t.Fatalf("gateway map %q: bad range %d %+v", in, i, r)
			}
		}
	}
	if g, _ := viewGateway.mapped("a\u00a0\u00a0b"); g.text != "a b" {
		t.Fatalf("gateway view does not collapse whitespace: %q", g.text)
	}
}

// A span on the canonical view covers the hidden characters inside it.
func TestSpliceCoversHiddenCharacters(t *testing.T) {
	original := "key: ab\u200bcd\uff45f end"
	m, ok := viewCanonical.mapped(original)
	if !ok || m.text != "key: abcdef end" {
		t.Fatalf("mapped = %q ok=%v", m.text, ok)
	}
	start := strings.Index(m.text, "abcdef")
	out, changed := m.splice(original, []RewriteSpan{{Start: start, End: start + len("abcdef"), Replacement: "[X]"}})
	if !changed || out != "key: [X] end" {
		t.Fatalf("splice = %q changed=%v", out, changed)
	}
}
