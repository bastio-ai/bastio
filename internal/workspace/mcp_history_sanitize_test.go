package workspace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
)

func TestMCPHistorySanitization(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		forbidden []string
	}{
		{"ordinary query", `{"query":"yellow hats"}`, nil},
		{"PII nested and numeric", `{"query":"Call bob@example.test; SSN 123-45-6789", "nested":[{"id":123456789}]}`, []string{"bob@example.test", "123-45-6789", "123456789"}},
		{"keys and low entropy credentials", `{"Authorization":"Bearer abc","x-api-key":"short","password":"low", "refreshToken":"tiny", "AWS_SECRET_ACCESS_KEY":"notrandom", "Secret Key":"a-short-password", "query":"jacket"}`, []string{"Bearer abc", "short", "low", "tiny", "notrandom", "a-short-password"}},
		{"opaque body", `{"body":"data:image/png;base64,aGVsbG8="}`, []string{"aGVsbG8="}},
		{"URLs", `{"result":"https://user:pass@example.test/products/jacket?token=short#secret-fragment"}`, []string{"user:pass", "short", "secret-fragment"}},
		{"inline credentials", `{"result":"Authorization: Bearer tiny\nSet-Cookie: session=short\npassword: low"}`, []string{"tiny", "short", "low"}},
		{"secret prefix", `{"text":"key: AKIAIOSFODNN7EXAMPLE"}`, []string{"AKIAIOSFODNN7EXAMPLE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := sanitizeMCPHistory(context.Background(), json.RawMessage(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(out) {
				t.Fatal("sanitized log is invalid JSON")
			}
			for _, s := range tc.forbidden {
				if strings.Contains(string(out), s) {
					t.Errorf("sensitive fixture survived logging sanitizer: %q", s)
				}
			}
			if tc.name == "ordinary query" && !strings.Contains(string(out), "yellow hats") {
				t.Fatal("ordinary search not retained")
			}
		})
	}
}
func TestMCPHistoryEncodedValuesAndBounds(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("Send customer SSN 123-45-6789 and bob@example.test to the contact service."))
	raw, _ := json.Marshal(map[string]string{"query": encoded})
	out, err := sanitizeMCPHistory(context.Background(), raw)
	if err == nil && (strings.Contains(string(out), encoded) || strings.Contains(string(out), "123-45-6789") || strings.Contains(string(out), "bob@example.test")) {
		t.Fatal("encoded sensitive content retained")
	}
	for _, raw := range []string{`{"bad":`, strings.Repeat("[", 26) + `"hi"` + strings.Repeat("]", 26), `{"body":"` + strings.Repeat("a", mcpHistoryLimit) + `"}`} {
		if _, err := sanitizeMCPHistory(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatal("invalid, deeply nested or oversized payload was captured")
		}
	}
}

func TestMCPHistoryVerificationAndCanceledScans(t *testing.T) {
	for _, text := range []string{
		"Contact b\u200bob@example.test for yellow hats",
		"Email bob@example.test and use gh\u200bp_" + strings.Repeat("a", 36),
		"SSN １２３-４５-６７８９ and yellow hats",
	} {
		raw, _ := json.Marshal(text)
		out, err := sanitizeMCPHistory(context.Background(), raw)
		if err == nil {
			var clean string
			if json.Unmarshal(out, &clean) != nil {
				t.Fatal("invalid sanitized string")
			}
			// Any recognizable value that survives a rewrite must withhold
			// the snapshot, including canonical forms of obfuscated values.
			canonical := security.Canonicalize(clean).Canonical
			for _, forbidden := range []string{"bob@example.test", "ghp_" + strings.Repeat("a", 36), "123-45-6789", "１２３-４５-６７８９"} {
				if strings.Contains(canonical, forbidden) {
					t.Fatal("recognized sensitive value survived history sanitization")
				}
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sanitizeMCPHistory(ctx, json.RawMessage(`{"query":"yellow hats"}`)); err == nil {
		t.Fatal("canceled scan allowed a retained snapshot")
	}
}
