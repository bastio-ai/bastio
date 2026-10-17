package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
)

func TestGeminiGenerationAndSynchronousBatchEmbeddingsInspectText(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "person@example.test") {
			t.Error("unprotected Google text reached provider")
		}
		if r.Header.Get("X-Goog-Api-Key") != "provider-key" {
			t.Error("Google credential missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"embedding":{"values":[0.1,0.2]},"usageMetadata":{"promptTokenCount":12}}`)
	}))
	defer upstream.Close()
	registry := providers.NewRegistry()
	registry.Register(providers.NewGeminiClientWithBaseURL(upstream.URL))
	profile := security.DefaultProfile()
	profile.PIIAction = security.ActionBlock
	h := NewHandler(registry, nil, security.NewEngine(detection.NewPIIDetector()), &nativeTestProfile{&profile}, nil, "fail-closed")
	h.SetNativeKeyResolver(func(*http.Request, providers.Provider) string { return "provider-key" })
	for _, tc := range []struct{ endpoint, body string }{
		{"/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"person@example.test"}]}]}`},
		{"/models/gemini:embedContent", `{"content":{"parts":[{"text":"person@example.test"}]}}`},
		{"/models/gemini:batchEmbedContents", `{"requests":[{"model":"models/gemini","content":{"parts":[{"text":"person@example.test"}]}}]}`},
	} {
		if !IsGenerationGatewayPath("/v1/google/v1beta" + tc.endpoint + "/") {
			t.Fatalf("Google operation bypassed gateway quota classification: %s", tc.endpoint)
		}
		rr := httptest.NewRecorder()
		h.NativeAPI(providers.ProviderGemini, tc.endpoint)(rr, nativeRequest("POST", "/v1/google"+tc.endpoint, tc.body))
		if rr.Code != 403 {
			t.Fatalf("Google PII not blocked: %d %s", rr.Code, rr.Body)
		}
		rr = httptest.NewRecorder()
		h.NativeAPI(providers.ProviderGemini, tc.endpoint)(rr, nativeRequest("POST", "/v1/google"+tc.endpoint, strings.ReplaceAll(tc.body, "person@example.test", "allowed text")))
		if rr.Code != 200 {
			t.Fatalf("Google safe input failed: %d %s", rr.Code, rr.Body)
		}
	}
	if calls != 3 {
		t.Fatalf("blocked input reached provider: %d calls", calls)
	}
	if in, out := nativeUsage([]byte(`{"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"thoughtsTokenCount":2}}`)); in != 12 || out != 5 {
		t.Fatal("Google native usage lost")
	}
}
