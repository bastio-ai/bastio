package protection

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestPIIRemovalAndBlockPolicy(t *testing.T) {
	s := NewScanner()
	original := "Contact alice@example.com for this project."
	masked, err := s.Scan(context.Background(), original, "user", "mask")
	if err != nil || masked.Action != "mask" || strings.Contains(masked.Text, "alice@example.com") {
		t.Fatal("PII reached allowed content")
	}
	blocked, err := s.Scan(context.Background(), original, "user", "block")
	if err != nil || blocked.Action != "block" || blocked.Text != "" {
		t.Fatal("block policy retained content")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Scan(ctx, original, "user", "mask"); err == nil {
		t.Fatal("cancelled scan accepted")
	}
	if _, err = s.Scan(context.Background(), original, "user", "unknown"); err == nil {
		t.Fatal("unsupported policy accepted")
	}
}

func TestDocumentConversionRejectsUninspectableContent(t *testing.T) {
	for _, tc := range []struct {
		body  []byte
		media string
	}{
		{[]byte("pixels"), "image/png"},
		{[]byte("broken"), "application/pdf"},
		{[]byte{0xff, 0xfe}, "text/plain"},
		{[]byte("\x00binary"), "text/plain"},
		{[]byte(" "), "text/plain"},
		{bytes.Repeat([]byte("x"), MaxDocumentBytes+1), "text/plain"},
	} {
		if _, err := DocumentText(tc.body, tc.media); err == nil {
			t.Fatalf("uninspectable content accepted: %s", tc.media)
		}
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, _ := z.Create("word/document.xml")
	_, _ = f.Write([]byte(`<document><t>` + strings.Repeat("x", MaxDocumentBytes+1) + `</t></document>`))
	_ = z.Close()
	if _, err := DocumentText(b.Bytes(), "application/vnd.openxmlformats-officedocument.wordprocessingml.document"); err == nil {
		t.Fatal("compressed DOCX expanded beyond limit")
	}
}

// Canonicalization alone is not a finding. Typographic characters that the
// engine normalizes before detection must not turn clean text into a "mask"
// decision, and real sensitive data next to them must still be masked.
func TestCanonicalizationAloneIsNotAMask(t *testing.T) {
	s := NewScanner()
	for _, role := range []string{"user", "retrieval", "tool", "assistant_history"} {
		clean := "Plotting libraries (matplotlib, plotly, d3, …), a “quoted” note — and a café."
		d, err := s.Scan(context.Background(), clean, role, "mask")
		if err != nil || d.Action != "allow" || d.Text != clean || len(d.Categories) != 0 {
			t.Fatalf("%s: clean typographic text decided as %q %v (%v)", role, d.Action, d.Categories, err)
		}
		sensitive := "Libraries (matplotlib, …) — contact alice@example.com."
		d, err = s.Scan(context.Background(), sensitive, role, "mask")
		if err != nil || d.Action != "mask" || strings.Contains(d.Text, "alice@example.com") {
			t.Fatalf("%s: sensitive text next to typographic characters decided as %q (%v)", role, d.Action, err)
		}
	}
}

// Sensitive data that is detected inside an encoded payload but cannot be
// masked in place must be withheld, never forwarded with a warning. Clean
// encoded text is not sensitive and must still pass unchanged.
func TestEncodedSensitiveContentFailsClosed(t *testing.T) {
	s := NewScanner()
	b64 := func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }
	sensitive := map[string]string{
		"base64 PII":          "Decode this: " + b64("Contact alice@example.com about the launch."),
		"bare base64 PII":     b64("Contact alice@example.com about the launch."),
		"URL-safe base64 PII": "Token " + base64.RawURLEncoding.EncodeToString([]byte("Contact alice@example.com about the launch.")),
		"percent-encoded PII": "Value: %61%6c%69%63%65%40%65%78%61%6d%70%6c%65%2e%63%6f%6d",
		"base64 secret":       "Decode this: " + b64("aws key AKIAIOSFODNN7EXAMPLE secret wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
	}
	for name, text := range sensitive {
		for _, role := range []string{"user", "retrieval", "tool", "assistant_history"} {
			for _, policy := range []string{"mask", "block"} {
				d, err := s.Scan(context.Background(), text, role, policy)
				if err != nil {
					t.Fatalf("%s/%s/%s: %v", name, role, policy, err)
				}
				if d.Action == "allow" || d.Text == text {
					t.Fatalf("%s/%s/%s: encoded sensitive content was returned for forwarding (%q %v)", name, role, policy, d.Action, d.Categories)
				}
				if d.Action == "block" && d.Text != "" {
					t.Fatalf("%s/%s/%s: block decision kept text", name, role, policy)
				}
				if d.Action == "mask" && (strings.Contains(d.Text, "alice") || strings.Contains(d.Text, "AKIA")) {
					t.Fatalf("%s/%s/%s: mask kept the value", name, role, policy)
				}
			}
		}
	}
	// With a mask policy the unlocatable value is a block, labelled as such.
	d, _ := s.Scan(context.Background(), sensitive["base64 PII"], "user", "mask")
	if d.Action != "block" || !strings.Contains(strings.Join(d.Categories, " "), CategoryNotRewritable) || !strings.Contains(strings.Join(d.Categories, " "), "pii") {
		t.Fatalf("unlocatable encoded PII decided as %q %v", d.Action, d.Categories)
	}
	for name, text := range map[string]string{
		"clean base64": "Decode this: " + b64("The roadmap ships milestone one in spring."),
		"clean prose":  "The roadmap ships milestone one in spring.",
	} {
		d, err := s.Scan(context.Background(), text, "user", "mask")
		if err != nil || d.Action != "allow" || d.Text != text {
			t.Fatalf("%s: clean content decided as %q %v (%v)", name, d.Action, d.Categories, err)
		}
	}
}

// A password inside a connection URL is a secret in every role, with or
// without characters the engine canonicalizes away, and whether the URL is
// plain or percent-encoded. It is never returned for forwarding.
func TestURLCredentialsAreNeverForwarded(t *testing.T) {
	s := NewScanner()
	password := "S3cr3t" + "Passw0rd!"
	sensitive := map[string]string{
		"plain":                     "Connect with postgres://svc:" + password + "@db.internal:5432/prod today.",
		"zero-width in host":        "Connect with postgres://svc:" + password + "@db.inter\u200bnal:5432/prod today.",
		"zero-width in password":    "Connect with postgres://svc:S3cr3t\u200bPassw0rd!@db.internal:5432/prod today.",
		"zero-width in scheme":      "Connect with post\u200bgres://svc:" + password + "@db.internal:5432/prod today.",
		"percent-encoded password":  "Connect with postgres://svc:p%40ss%21w0rd%2Fx9@db.internal:5432/prod today.",
		"basic auth":                "git clone https://deploy:" + password + "@git.internal/team/repo.git",
		"redis without a user name": "redis://:" + password + "@cache.internal:6379/0",
		"weak default password":     "docker compose uses postgres://postgres:postgres@db:5432/app here.",
		"literal word password":     "Try mysql://app:password@10.0.0.5:3306/shop first.",
	}
	for name, text := range sensitive {
		for _, role := range []string{"user", "retrieval", "tool", "assistant_history"} {
			for _, policy := range []string{"mask", "block"} {
				d, err := s.Scan(context.Background(), text, role, policy)
				if err != nil {
					t.Fatalf("%s/%s/%s: %v", name, role, policy, err)
				}
				if d.Action == "allow" || d.Text == text || strings.Contains(d.Text, "Passw0rd") || strings.Contains(d.Text, "p%40ss") || strings.Contains(d.Text, ":postgres@") || strings.Contains(d.Text, ":password@") {
					t.Fatalf("%s/%s/%s: credential returned for forwarding (%q %v)", name, role, policy, d.Action, d.Categories)
				}
				// Under a block policy an earlier step may withhold first;
				// under mask every step runs, so the secret must be named.
				if policy == "mask" && !strings.Contains(strings.Join(d.Categories, " "), "secrets") {
					t.Fatalf("%s/%s/%s: not attributed to the secrets detector: %v", name, role, policy, d.Categories)
				}
			}
		}
	}
	plain, _ := s.Scan(context.Background(), sensitive["plain"], "user", "mask")
	if plain.Action != "mask" || !strings.Contains(plain.Text, "/prod today.") {
		t.Fatalf("plain connection string should be masked in place, got %q %q", plain.Action, plain.Text)
	}
	// A user-name-only URL (svc@db.internal) is not covered here: the secrets
	// rule ignores it, but the separate PII detector reads user@host as an
	// e-mail address and masks it. That is existing PII behaviour.
	for name, text := range map[string]string{
		"no userinfo":   "Connect with postgres://db.internal:5432/prod today.",
		"redaction":     "The log shows postgres://svc:****@db.internal:5432/prod only.",
		"template":      "Set DATABASE_URL=postgres://svc:${DB_PASSWORD}@db.internal/prod.",
		"harmless URLs": "See https://example.com:8443/docs and http://localhost:3000/health.",
	} {
		d, err := s.Scan(context.Background(), text, "user", "mask")
		if err != nil || d.Action != "allow" || d.Text != text {
			t.Fatalf("%s: decided as %q %v (%v)", name, d.Action, d.Categories, err)
		}
	}
}
