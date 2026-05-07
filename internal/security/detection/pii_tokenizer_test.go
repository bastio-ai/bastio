package detection

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
)

func TestPIIDetector_Tokenize_RoundTrip(t *testing.T) {
	d := NewPIIDetector()

	tests := []struct {
		name  string
		input string
	}{
		{"ssn only", "My SSN is 123-45-6789 please help"},
		{"email only", "Email: john@example.com"},
		{"phone only", "Call 555-123-4567 today"},
		{"multiple types", "John (john@example.com, SSN 123-45-6789) at 192.168.1.1"},
		{"nested repeats", "SSN 123-45-6789 and again SSN 123-45-6789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenized, tm := d.Tokenize(tt.input, security.TokenStyleAngle)

			if tokenized == tt.input {
				t.Fatalf("tokenize did not rewrite content: %q", tokenized)
			}
			if strings.Contains(tokenized, "123-45-6789") {
				t.Errorf("raw SSN leaked into tokenized output: %q", tokenized)
			}
			if tm.Size() == 0 {
				t.Fatalf("expected non-empty TokenMap, got zero entries")
			}

			restored, n := tm.Restore(tokenized)
			if restored != tt.input {
				t.Errorf("round-trip failed\n  want: %q\n  got:  %q", tt.input, restored)
			}
			if n == 0 {
				t.Errorf("expected at least one restoration, got %d", n)
			}
		})
	}
}

func TestPIIDetector_Tokenize_RepeatsCollapse(t *testing.T) {
	d := NewPIIDetector()

	// Same SSN appears twice — both occurrences must map to the same
	// placeholder so the LLM sees them as the same entity.
	input := "First mention 123-45-6789, later again 123-45-6789."
	tokenized, tm := d.Tokenize(input, security.TokenStyleAngle)

	if tm.Size() != 1 {
		t.Fatalf("expected 1 unique placeholder, got %d", tm.Size())
	}
	if strings.Count(tokenized, "<PII_SSN_1>") != 2 {
		t.Errorf("expected placeholder to appear twice, got: %q", tokenized)
	}
}

func TestPIIDetector_Tokenize_CurlyStyle(t *testing.T) {
	d := NewPIIDetector()
	tokenized, tm := d.Tokenize("SSN 123-45-6789", security.TokenStyleCurly)

	if !strings.Contains(tokenized, "{{PII_SSN_1}}") {
		t.Errorf("expected curly placeholder, got: %q", tokenized)
	}
	if strings.Contains(tokenized, "<PII_") {
		t.Errorf("angle bracket form leaked into curly-style output: %q", tokenized)
	}

	restored, _ := tm.Restore(tokenized)
	if restored != "SSN 123-45-6789" {
		t.Errorf("curly round-trip failed: %q", restored)
	}
}

func TestTokenMap_Restore_UnknownPlaceholdersPassThrough(t *testing.T) {
	tm := security.NewTokenMap(security.TokenStyleAngle)
	tm.Add("ssn", "123-45-6789")

	// Model emitted a placeholder we never issued — must pass through.
	input := "Known <PII_SSN_1> unknown <PII_SSN_9> literal"
	out, n := tm.Restore(input)
	want := "Known 123-45-6789 unknown <PII_SSN_9> literal"
	if out != want {
		t.Errorf("unexpected restore\n  want: %q\n  got:  %q", want, out)
	}
	if n != 1 {
		t.Errorf("expected 1 restoration, got %d", n)
	}
}

func TestTokenMap_Restore_HandlesLongerFirst(t *testing.T) {
	// When placeholder indices exceed 9 the string <PII_SSN_1> is a
	// prefix of <PII_SSN_11>. Restore must match the longer placeholder
	// first to avoid eating characters out of the middle of it.
	tm := security.NewTokenMap(security.TokenStyleAngle)
	// Force N up to 11 by adding 11 distinct values.
	for i := range 11 {
		tm.Add("ssn", fmt.Sprintf("111-22-%04d", i))
	}

	input := "A <PII_SSN_11> B"
	out, n := tm.Restore(input)
	if n != 1 {
		t.Errorf("expected exactly 1 restoration, got %d (out=%q)", n, out)
	}
	if !strings.Contains(out, "A ") || !strings.Contains(out, " B") {
		t.Errorf("surrounding text mangled: %q", out)
	}
	if strings.Contains(out, "<PII_SSN_") {
		t.Errorf("placeholder still present after restore: %q", out)
	}
}

func TestTokenMap_NilSafe(t *testing.T) {
	var tm *security.TokenMap
	if tm.Size() != 0 {
		t.Errorf("nil Size() should be 0")
	}
	out, n := tm.Restore("<PII_SSN_1>")
	if out != "<PII_SSN_1>" || n != 0 {
		t.Errorf("nil Restore should pass-through unchanged, got %q %d", out, n)
	}
}

func TestEngine_Scan_AppliesPIIAction_Mask(t *testing.T) {
	d := NewPIIDetector()
	engine := security.NewEngine(d)

	res := engine.Scan(context.Background(), &security.ScanRequest{
		Content:   "SSN 123-45-6789",
		PIIAction: security.ActionMask,
	})

	if res.Action != security.ActionMask {
		t.Errorf("expected Action=mask, got %s", res.Action)
	}
	if res.SanitizedContent == "" {
		t.Fatal("expected SanitizedContent, got empty")
	}
	if strings.Contains(res.SanitizedContent, "123-45-6789") {
		t.Errorf("raw SSN still present in SanitizedContent: %q", res.SanitizedContent)
	}
	if res.TokenMap != nil {
		t.Errorf("mask mode must not populate TokenMap")
	}
}

func TestEngine_Scan_AppliesPIIAction_Tokenize(t *testing.T) {
	d := NewPIIDetector()
	engine := security.NewEngine(d)

	res := engine.Scan(context.Background(), &security.ScanRequest{
		Content:   "SSN 123-45-6789",
		PIIAction: security.ActionTokenize,
	})

	if res.Action != security.ActionTokenize {
		t.Errorf("expected Action=tokenize, got %s", res.Action)
	}
	if res.TokenMap == nil || res.TokenMap.Size() == 0 {
		t.Fatal("expected TokenMap populated")
	}
	if !strings.Contains(res.SanitizedContent, "<PII_SSN_1>") {
		t.Errorf("expected placeholder in SanitizedContent, got: %q", res.SanitizedContent)
	}

	restored, _ := res.TokenMap.Restore(res.SanitizedContent)
	if restored != "SSN 123-45-6789" {
		t.Errorf("round-trip via engine failed: %q", restored)
	}
}

func TestEngine_Scan_PIIAction_LogOnly_NoRewrite(t *testing.T) {
	d := NewPIIDetector()
	engine := security.NewEngine(d)

	res := engine.Scan(context.Background(), &security.ScanRequest{
		Content:   "SSN 123-45-6789",
		PIIAction: security.ActionLogOnly,
	})

	if res.SanitizedContent != "" {
		t.Errorf("log_only must not rewrite content, got: %q", res.SanitizedContent)
	}
	if res.TokenMap != nil {
		t.Errorf("log_only must not populate TokenMap")
	}
	if len(res.Findings) == 0 {
		t.Errorf("log_only still records findings")
	}
}

func TestTokenMap_Context_RoundTrip(t *testing.T) {
	tm := security.NewTokenMap(security.TokenStyleAngle)
	tm.Add("ssn", "123-45-6789")

	ctx := security.WithTokenMap(context.Background(), tm)
	got, ok := security.TokenMapFromContext(ctx)
	if !ok {
		t.Fatal("expected TokenMap on context")
	}
	if got.Size() != 1 {
		t.Errorf("expected size 1, got %d", got.Size())
	}

	// Nil map should not be attached.
	ctx2 := security.WithTokenMap(context.Background(), nil)
	if _, ok := security.TokenMapFromContext(ctx2); ok {
		t.Errorf("nil TokenMap must not be retrievable")
	}
}
