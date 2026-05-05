package workspace

import (
	"strings"
	"testing"
)

func TestVectorLiteralBasic(t *testing.T) {
	t.Parallel()
	got := vectorLiteral([]float32{0.1, 0.2, 0.3})
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("missing brackets: %q", got)
	}
	if !strings.Contains(got, "0.1") || !strings.Contains(got, "0.2") || !strings.Contains(got, "0.3") {
		t.Fatalf("missing components: %q", got)
	}
	// pgvector parses commas literally — no spaces, no quotes.
	if strings.Contains(got, " ") || strings.Contains(got, "'") {
		t.Fatalf("invalid format: %q", got)
	}
}

func TestVectorLiteralEmpty(t *testing.T) {
	t.Parallel()
	if got := vectorLiteral(nil); got != "" {
		t.Fatalf("nil → empty string, got %q", got)
	}
	if got := vectorLiteral([]float32{}); got != "" {
		t.Fatalf("empty slice → empty string, got %q", got)
	}
}

func TestVectorLiteralRoundtripViaCommaSplit(t *testing.T) {
	t.Parallel()
	in := []float32{1.5, -2.25, 0, 0.0001}
	s := vectorLiteral(in)
	// Strip brackets and split on commas; check we get the same count
	// of numeric tokens. Simple structural check — exact float
	// formatting is platform-dependent.
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	parts := strings.Split(inner, ",")
	if len(parts) != len(in) {
		t.Fatalf("token count mismatch: %d vs %d (%q)", len(parts), len(in), s)
	}
}

// TestVectorLiteralLargeDim mirrors the production embedding size to
// catch any quadratic behavior in the formatter (a 1536-dim vector
// is the OpenAI text-embedding-3-small default).
func TestVectorLiteralLargeDim(t *testing.T) {
	t.Parallel()
	v := make([]float32, 1536)
	for i := range v {
		v[i] = float32(i) / 1000
	}
	s := vectorLiteral(v)
	if len(s) < 1536 {
		t.Fatalf("output suspiciously short for 1536-dim: %d chars", len(s))
	}
	// Bracket pair + 1535 commas + 1536 numbers — exact count is fragile
	// but commas should be exactly len(v)-1.
	if got := strings.Count(s, ","); got != len(v)-1 {
		t.Fatalf("comma count: got %d, want %d", got, len(v)-1)
	}
}
