package gateway

import (
	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
)

func TestMonitoringPreservesNativeInputOutputAndStillFindsPII(t *testing.T) {
	const email = "synthetic.person@example.com"
	for _, stream := range []bool{false, true} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), email) {
				t.Error("monitoring rewrote original input")
			}
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\""+email+"\"}}]}\n\ndata: [DONE]\n\n")
			} else {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"choices":[{"message":{"content":"`+email+`"}}]}`)
			}
		}))
		registry := providers.NewRegistry()
		registry.Register(providers.NewOpenRouterClientWithBaseURL(upstream.URL))
		profile := security.DefaultProfile()
		profile.PIIAction = security.ActionBlock
		h := NewHandler(registry, nil, security.NewEngine(detection.NewPIIDetector()), &nativeTestProfile{&profile}, nil, "fail-closed")
		h.SetNativeKeyResolver(func(*http.Request, providers.Provider) string { return "upstream-test-key" })
		body := `{"model":"test","messages":[{"role":"user","content":"` + email + `"}]}`
		// Header-only attempts cannot weaken enforcement.
		r := nativeRequest("POST", "/v1/openrouter/chat/completions", body)
		r.Header.Set("X-Bastio-Monitor-Only", "true")
		w := httptest.NewRecorder()
		h.NativeAPI(providers.ProviderOpenRouter, "/chat/completions")(w, r)
		if w.Code != 403 {
			t.Fatal("untrusted header bypassed enforcement", w.Code)
		}
		r = nativeRequest("POST", "/v1/openrouter/chat/completions", body)
		r = r.WithContext(gatewaypolicy.With(r.Context(), gatewaypolicy.Policy{MonitorOnly: true, OmitContent: true}))
		result := h.runSecurityScan(r, func() *auth.APIKeyInfo { info, _ := auth.FromContext(r.Context()); return info }(), email, uuid.New(), &profile)
		if result == nil || len(result.Findings) == 0 || result.ShouldBlock || result.Action != security.ActionLogOnly {
			t.Fatal("monitoring lost findings or enforced", result)
		}
		if result.Findings[0].Source != "input" {
			t.Fatal("request finding lost its direction", result.Findings[0].Source)
		}
		output := h.runSecurityScanFrom(r, func() *auth.APIKeyInfo { info, _ := auth.FromContext(r.Context()); return info }(), email, uuid.New(), &profile, "response")
		if output == nil || len(output.Findings) == 0 || output.Findings[0].Source != "response" {
			t.Fatal("response finding lost its direction")
		}
		merged := mergeMonitorScans(result, output)
		if len(merged.Findings) != 2 || merged.Findings[0].Source != "input" || merged.Findings[1].Source != "response" {
			t.Fatal("merged trace cannot distinguish request from response")
		}
		w = httptest.NewRecorder()
		h.NativeAPI(providers.ProviderOpenRouter, "/chat/completions")(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), email) {
			t.Fatal("monitoring altered native response", w.Code, w.Body.String())
		}
		if stream && w.Header().Get("X-Bastio-Stream-Buffered") != "" {
			t.Fatal("monitoring buffered streaming output")
		}
		upstream.Close()
	}
}

func TestTypeSafeNativeStateAndAnswers(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer typesafe-upstream-test" {
			t.Fatal("wrong native destination or credential")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"state"`) || !strings.Contains(string(body), `"questions"`) {
			t.Fatal("decision contract lost")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"jev-latest","answers":{"q":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":12,"output_tokens":3}}`)
	}))
	defer upstream.Close()
	registry := providers.NewRegistry()
	registry.Register(providers.NewTypeSafeClientWithBaseURL(upstream.URL + "/v1"))
	h := NewHandler(registry, nil, security.NewEngine(detection.NewPIIDetector()), nil, nil, "fail-open")
	h.SetNativeKeyResolver(func(*http.Request, providers.Provider) string { return "typesafe-upstream-test" })
	r := nativeRequest("POST", "/v1/typesafe/systemone", `{"model":"jev-latest","state":{"text":"hello"},"questions":{"q":{"type":"noul","instructions":"greeting?"}}}`)
	r = r.WithContext(gatewaypolicy.With(r.Context(), gatewaypolicy.Policy{MonitorOnly: true, OmitContent: true}))
	w := httptest.NewRecorder()
	h.NativeAPI(providers.ProviderTypeSafe, "/systemone")(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"answers"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	in, out := nativeUsage(w.Body.Bytes())
	if in != 12 || out != 3 {
		t.Fatal("decision usage lost", in, out)
	}
	if got := InputText([]byte(`{"state":{"text":"hello"},"questions":{"q":{"instructions":"question"}}}`)); !strings.Contains(got, "hello") || !strings.Contains(got, "question") {
		t.Fatal("decision input not inspected", got)
	}
	if got := OutputText(w.Body.Bytes()); !strings.Contains(got, "yes") {
		t.Fatal("decision answers not inspected", got)
	}
}

func TestLocalObservationAuthorizationAndBoundedText(t *testing.T) {
	registry := providers.NewRegistry()
	registry.Register(providers.NewOllamaClient())
	h := NewHandler(registry, nil, security.NewEngine(detection.NewPIIDetector()), nil, nil, "fail-closed")
	// No registered inference provider: this endpoint must only inspect.
	r := httptest.NewRequest("POST", "/v1/observe", strings.NewReader(`{"request":"{\"input\":\"synthetic@example.test\"}","http_status":200}`))
	w := httptest.NewRecorder()
	h.Observe(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated observation accepted", w.Code)
	}
	r = nativeRequest("POST", "/v1/observe", `{"model":"local-test","request":"{\"input\":\"synthetic@example.test\"}","http_status":200}`)
	w = httptest.NewRecorder()
	h.Observe(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "observation_only") {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := localObservationText(`{"input":"truncated@example.test`, true); !strings.Contains(got, "truncated@example.test") {
		t.Fatal("truncated input skipped")
	}
	if got := localObservationText("{\"response\":\"hello \"}\n{\"response\":\"world\"}\n", false); got != "hello world" {
		t.Fatal("local stream skipped", got)
	}
}
