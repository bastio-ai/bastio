package gateway

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
)

// Synthetic values only, split so secret scanners do not flag the file.
const encEmail = "jane.testperson" + "@" + "example.com"

func encSecretKey() string {
	return strings.Join([]string{"sk", "proj", "Ab3dEf6hIj9kLm2nOp5q" + "Rs8tUv1wXy4zAb7cDe0fGh3iJk6l"}, "-")
}

func encRecordBlob() string {
	return base64.StdEncoding.EncodeToString([]byte(`{"email":"` + encEmail + `","id":7,"status":"active"}`))
}

func encSecretBlob() string {
	return base64.StdEncoding.EncodeToString([]byte("deploy with " + encSecretKey() + " please"))
}

// encDoubleEncoded is base64 that is itself percent-encoded: the scan
// decodes both layers and finds the email, but no single encoded token in
// the text decodes to it, so the payload cannot be located.
func encDoubleEncoded() string {
	var b strings.Builder
	for _, c := range []byte(base64.StdEncoding.EncodeToString([]byte("write to " + encEmail + " today"))) {
		b.WriteString("%")
		b.WriteString(strings.ToUpper(hexByte(c)))
	}
	return b.String()
}

func hexByte(c byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[c>>4], digits[c&0x0f]})
}

// sentCarries reports whether the outbound body still carries the value
// in raw, JSON-escaped or encoded form.
func sentCarries(sent string, values ...string) bool {
	for _, v := range values {
		esc, _ := json.Marshal(v)
		if strings.Contains(sent, v) || strings.Contains(sent, strings.Trim(string(esc), `"`)) {
			return true
		}
	}
	return false
}

func TestEncodedPayload_MaskedWholeOnEveryRoute(t *testing.T) {
	cases := []struct {
		name, user, tool, placeholder string
		gone                          []string
		keep                          []string
	}{
		{
			name: "base64 record in tool result", user: parityBenignUser,
			tool: "record payload: " + encRecordBlob(), placeholder: "[REDACTED_ENCODED_PII]",
			gone: []string{encRecordBlob(), encEmail}, keep: []string{"record payload:"},
		},
		{
			name: "url-encoded email in user query", user: "q=jane.testperson%40example.com&page=2", tool: "no data",
			placeholder: "q=[REDACTED_ENCODED_PII]", gone: []string{"jane.testperson%40example.com", encEmail}, keep: []string{"page=2"},
		},
		{
			name: "base64 secret in tool result", user: parityBenignUser,
			tool: "config: " + encSecretBlob(), placeholder: "[REDACTED_ENCODED_SECRETS]",
			gone: []string{encSecretBlob(), encSecretKey()}, keep: []string{"config:"},
		},
	}
	for _, route := range parityRoutes() {
		for _, c := range cases {
			t.Run(route.name+"/"+c.name, func(t *testing.T) {
				code, sent := route.run(t, security.DefaultProfile(), c.user, c.tool)
				if code != http.StatusOK {
					t.Fatalf("status %d", code)
				}
				if sentCarries(sent, c.gone...) {
					t.Fatalf("encoded value reached upstream: %s", sent)
				}
				if !strings.Contains(sent, c.placeholder) {
					t.Fatalf("placeholder %q missing upstream: %s", c.placeholder, sent)
				}
				for _, k := range c.keep {
					if !strings.Contains(sent, k) {
						t.Fatalf("surrounding text %q lost: %s", k, sent)
					}
				}
				scan := route.scan(t, security.DefaultProfile(), c.user, c.tool)
				det := "pii"
				if strings.Contains(c.placeholder, "SECRETS") {
					det = "secrets"
				}
				if scan.ShouldBlock || !scan.DetectorActions[det].IsMaskLike() {
					t.Fatalf("recorded %s action %q block=%v, want mask", det, scan.DetectorActions[det], scan.ShouldBlock)
				}
				if len(scan.EncodedMasked) == 0 {
					t.Fatal("encoded-payload reason not recorded")
				}
			})
		}
	}
}

func TestEncodedPayload_PoliciesAndBenignEncodings(t *testing.T) {
	block := security.DefaultProfile()
	block.PIIAction = security.ActionBlock
	logOnly := security.DefaultProfile()
	logOnly.PIIAction = security.ActionLogOnly
	logOnly.SecretsStrategy = security.ActionLogOnly
	tool := "record payload: " + encRecordBlob()
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"order-1234","scope":"read"}`)) + ".c2lnbmF0dXJlLWJ5dGVz"
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x10\x00\x00\x00\x10\x08\x06"))
	for _, route := range parityRoutes() {
		t.Run(route.name+"/block policy", func(t *testing.T) {
			if code, sent := route.run(t, block, parityBenignUser, tool); code != http.StatusForbidden || sent != "" {
				t.Fatalf("pii block: status %d sent %q", code, sent)
			}
		})
		t.Run(route.name+"/log_only", func(t *testing.T) {
			code, sent := route.run(t, logOnly, parityBenignUser, tool)
			if code != http.StatusOK || !strings.Contains(sent, encRecordBlob()) || strings.Contains(sent, "[REDACTED_ENCODED_") {
				t.Fatalf("log_only must forward untouched: status %d sent %s", code, sent)
			}
		})
		t.Run(route.name+"/benign encodings", func(t *testing.T) {
			code, sent := route.run(t, security.DefaultProfile(), parityBenignUser, "image "+png+" token "+jwt)
			if code != http.StatusOK || strings.Contains(sent, "[REDACTED_ENCODED_") {
				t.Fatalf("benign encodings: status %d sent %s", code, sent)
			}
			if !strings.Contains(sent, png) {
				t.Fatalf("data URI changed: %s", sent)
			}
			// A JWT is itself a secrets-detector pattern and is masked in
			// place as before; it is never blob-replaced or blocked.
			if strings.Contains(sent, jwt) && !strings.Contains(sent, "[REDACTED_JWT]") {
				t.Fatalf("unexpected JWT handling: %s", sent)
			}
		})
	}
}

// When the encoded payload cannot be located the request stays blocked,
// and the 403 says why: the content could not be safely masked.
func TestEncodedPayload_UnlocatableIsTruthful403(t *testing.T) {
	tool := "record: " + encDoubleEncoded()
	for _, route := range parityRoutes() {
		t.Run(route.name, func(t *testing.T) {
			if code, sent := route.run(t, security.DefaultProfile(), parityBenignUser, tool); code != http.StatusForbidden || sent != "" {
				t.Fatalf("status %d sent %q", code, sent)
			}
			scan := route.scan(t, security.DefaultProfile(), parityBenignUser, tool)
			if !scan.ShouldBlock || !scan.RewriteUnavailable {
				t.Fatalf("block=%v rewrite_unavailable=%v", scan.ShouldBlock, scan.RewriteUnavailable)
			}
		})
	}

	// Normalized chat route body.
	up := newCapturingUpstream(t, okCompletion)
	rr := chatThroughGateway(t, up, security.DefaultProfile(), `[{"role":"user","content":"hi"},{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":`+jsonString(tool)+`}]`)
	var chat struct {
		Error  string `json:"error"`
		Code   string `json:"code"`
		Reason string `json:"reason"`
	}
	if rr.Code != http.StatusForbidden || json.Unmarshal(rr.Body.Bytes(), &chat) != nil {
		t.Fatalf("chat: %d %s", rr.Code, rr.Body.String())
	}
	if chat.Error != "request blocked by security policy" || chat.Code != "rewrite_unavailable" || !strings.Contains(chat.Reason, "could not be safely masked") {
		t.Fatalf("chat body: %s", rr.Body.String())
	}

	// Native route body keeps {error:{type,message}} and adds error.code.
	nup := newCapturingUpstream(t, openAIReply)
	registry := providers.NewRegistry()
	registry.Register(providers.NewOpenAIClientWithBaseURL(nup.srv.URL))
	h := parityHandler(registry, security.DefaultProfile())
	nrr := httptest.NewRecorder()
	h.NativeAPI(providers.ProviderOpenAI, "/chat/completions")(nrr, nativeRequest("POST", "/v1/openai/chat/completions", openAIShape("gpt-4o", parityBenignUser, tool)))
	var native struct {
		Error  string `json:"error"`
		Code   string `json:"code"`
		Reason string `json:"reason"`
	}
	if nrr.Code != http.StatusForbidden || json.Unmarshal(nrr.Body.Bytes(), &native) != nil || native.Code != "rewrite_unavailable" || !strings.Contains(native.Reason, "could not be safely masked") {
		t.Fatalf("native: %d %s", nrr.Code, nrr.Body.String())
	}

	// Batch item error carries the code.
	r := chatRequest(t, `[]`)
	info, _ := auth.FromContext(r.Context())
	profile := security.DefaultProfile()
	b := &batchCall{h: NewHandler(providers.NewRegistry(), nil, fullTestEngine(), nil, nil, "fail-open"), r: r, info: info,
		route: &nativeRoute{provider: providers.ProviderOpenAI}, profile: &profile, trace: uuid.New(), start: time.Now()}
	_, err := b.inputItem([]byte(openAIShape("gpt-4o", parityBenignUser, tool)), batchHash("item-1"), &batchManifest{})
	var re *routeError
	if !errors.As(err, &re) || re.status != http.StatusForbidden || re.code != "rewrite_unavailable" || !strings.Contains(re.message, "could not be safely masked") {
		t.Fatalf("batch: %v", err)
	}
}
