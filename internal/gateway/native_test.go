package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/google/uuid"
)

type nativeTestProfile struct{ profile *security.Profile }

func (s *nativeTestProfile) GetDefault(_ context.Context, _ uuid.UUID) (*security.Profile, error) {
	return s.profile, nil
}

func nativeRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r.WithContext(auth.WithInfo(r.Context(), &auth.APIKeyInfo{CustomerID: uuid.New()}))
}

func TestOpenRouterNativeAPIAndChatRouting(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "router-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer router-secret" {
			t.Error("wrong credential")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "openrouter/anthropic/") {
			t.Error("Bastio prefix leaked")
		}
		if r.Method == "POST" && (!strings.Contains(string(body), `"model":"anthropic/claude-sonnet-4.5"`) || !strings.Contains(string(body), `"zdr":true`)) {
			t.Errorf("OpenRouter options lost: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "upstream-123")
		w.Header().Set("Set-Cookie", "private-session")
		_, _ = w.Write([]byte(`{"id":"completion-123","choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer server.Close()
	reg := providers.NewRegistry()
	reg.Register(providers.NewOpenRouterClientWithBaseURL(server.URL))
	h := NewHandler(reg, nil, nil, nil, nil, "fail-open")
	body := `{"model":"openrouter/anthropic/claude-sonnet-4.5","messages":[{"role":"user","content":"hello"}],"provider":{"zdr":true}}`
	r := nativeRequest("POST", "/v1/chat/completions", body)
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, r)
	if rr.Code != 200 || rr.Header().Get("X-Request-Id") != "upstream-123" || rr.Header().Get("Set-Cookie") != "" {
		t.Fatalf("contract: %d %s", rr.Code, rr.Body.String())
	}
}

func TestNativeErrorsAndAncillaryEndpointsPreserved(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "router-secret")
	const errorBody = `{"error":{"code":402,"message":"credits exhausted","metadata":{"provider_name":"Anthropic"}}}`
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(402)
		_, _ = w.Write([]byte(errorBody))
	}))
	defer server.Close()
	reg := providers.NewRegistry()
	reg.Register(providers.NewOpenRouterClientWithBaseURL(server.URL))
	h := NewHandler(reg, nil, nil, nil, nil, "fail-open")
	for _, endpoint := range []string{"/models", "/models/anthropic/claude-sonnet-4.5/endpoints", "/generation", "/responses", "/embeddings", "/key", "/credits", "/providers"} {
		rr := httptest.NewRecorder()
		h.NativeAPI(providers.ProviderOpenRouter, endpoint)(rr, nativeRequest("GET", "/v1/openrouter"+endpoint+"?id=123&proxy_id=", ""))
		if rr.Code != 402 || rr.Body.String() != errorBody || rr.Header().Get("Retry-After") != "30" || gotPath != endpoint || gotQuery != "id=123" {
			t.Fatalf("%s contract lost: %d %s path=%s query=%s", endpoint, rr.Code, rr.Body.String(), gotPath, gotQuery)
		}
	}
}

func TestStructuredPIIInputsAndNativeOutputs(t *testing.T) {
	engine := security.NewEngine(detection.NewPIIDetector())
	for _, body := range []string{
		`{"messages":[{"role":"user","content":[{"type":"text","text":"SSN 123-45-6789","cache_control":{"type":"ephemeral"}},{"type":"image_url","image_url":{"url":"data:image/png;base64,keep"}}]}]}`,
		`{"messages":[{"role":"tool","tool_call_id":"keep","content":"SSN 123-45-6789"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"keep","content":[{"type":"text","text":"SSN 123-45-6789"}]}]}]}`,
		`{"input":[{"role":"user","content":[{"type":"input_text","text":"SSN 123-45-6789"}]},{"type":"function_call_output","call_id":"keep","output":"SSN 123-45-6789"}]}`,
		`{"input":["SSN 123-45-6789","another 123-45-6789"]}`,
		`{"messages":[{"role":"assistant","content":"SSN 123-45-6789"}]}`,
		`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"keep","name":"lookup","input":{"ssn":"123-45-6789"}}]}]}`,
		`{"messages":[{"role":"assistant","tool_calls":[{"id":"keep","type":"function","function":{"name":"lookup","arguments":"{\"ssn\":\"123-45-6789\"}"}}]}]}`,
		`{"input":[{"type":"function_call","call_id":"keep","name":"lookup","arguments":"{\"ssn\":\"123-45-6789\"}"}]}`,
		`{"requests":[{"custom_id":"keep","params":{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"SSN 123-45-6789"}]}}]}`,
	} {
		tm := security.NewTokenMap(security.TokenStyleAngle)
		out, changed := sanitizeInput([]byte(body), engine, security.ActionTokenize, tm)
		if !changed || tm.Size() != 1 || strings.Contains(string(out), "123-45-6789") || !json.Valid(out) {
			t.Fatalf("structured input leak: %s -> %s", body, out)
		}
		if !strings.Contains(extractInputText([]byte(body)), "123-45-6789") {
			t.Fatal("input not scanned")
		}
		if strings.Contains(body, "keep") && !strings.Contains(string(out), "keep") {
			t.Fatal("provider metadata mutated")
		}
	}
	tm := security.NewTokenMap(security.TokenStyleAngle)
	token := tm.Add("ssn", "123-45-6789")
	profile := security.DefaultProfile()
	profile.PIIScanResponse = false
	profile.PIIRestoreResponse = true
	ctx := security.WithTokenMap(security.WithProfile(context.Background(), &profile), tm)
	h := &Handler{security: engine}
	for _, output := range []string{
		`{"content":[{"type":"text","text":"TOKEN"}]}`,
		`{"output":[{"type":"message","content":[{"type":"output_text","text":"TOKEN"}]}]}`,
		`{"choices":[{"message":{"tool_calls":[{"function":{"arguments":"TOKEN"}}]}}]}`,
	} {
		body := strings.ReplaceAll(output, "TOKEN", token)
		out, blocked, _ := h.postProcessResponse(ctx, []byte(body))
		if blocked || !strings.Contains(string(out), "123-45-6789") {
			t.Fatalf("native restoration failed: %s -> %s", body, out)
		}
	}
}

func TestClaudeNativeSSEAndSplitPIIProtection(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "claude-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":9}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"SSN 123-\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"45-6789\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	reg := providers.NewRegistry()
	reg.Register(providers.NewAnthropicClientWithBaseURL(server.URL))
	h := NewHandler(reg, nil, security.NewEngine(detection.NewPIIDetector()), nil, nil, "fail-open")
	rr := httptest.NewRecorder()
	h.NativeAPI(providers.ProviderAnthropic, "/messages")(rr, nativeRequest("POST", "/v1/messages", `{"model":"claude-sonnet-4-5","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	out := rr.Body.String()
	if rr.Code != 200 || !strings.Contains(out, "event: message_stop") || strings.Contains(out, "data: [DONE]") || strings.Contains(out, "123-") || strings.Contains(out, "45-6789") {
		t.Fatalf("SSE contract/PII leak: %d %s", rr.Code, out)
	}
}

func TestNativeProxyScopeAndProviderConflicts(t *testing.T) {
	h := NewHandler(providers.NewRegistry(), nil, nil, nil, nil, "fail-open")
	info := &auth.APIKeyInfo{CustomerID: uuid.New(), Scopes: []string{"proxy:" + uuid.NewString()}}
	r := nativeRequest("GET", "/v1/openrouter/models", "")
	r.Header.Set("X-Bastio-Proxy-Id", uuid.NewString())
	if _, err := h.nativeRoute(r, info, providers.ProviderOpenRouter, ""); err == nil {
		t.Fatal("proxy-scoped key crossed scope")
	}
	r.Header.Set("X-Bastio-Provider", "openai")
	if _, err := h.nativeRoute(r, &auth.APIKeyInfo{}, providers.ProviderOpenRouter, ""); err == nil {
		t.Fatal("conflicting provider accepted")
	}
}

func TestNativeRegisteredRoutesAndMultipart(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "router-secret")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("XAI_API_KEY", "xai-secret")
	var gotPath, gotMethod string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	reg := providers.NewRegistry()
	reg.Register(providers.NewOpenRouterClientWithBaseURL(server.URL))
	reg.Register(providers.NewOpenAIClientWithBaseURL(server.URL))
	reg.Register(providers.NewXAIClientWithBaseURL(server.URL))
	h := NewHandler(reg, nil, nil, nil, nil, "fail-open")
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithInfo(r.Context(), &auth.APIKeyInfo{CustomerID: uuid.New()})))
		})
	})
	router.Route("/v1", h.RegisterNativeRoutes)
	for _, tt := range []struct{ path, endpoint, method string }{
		{"/v1/openrouter/chat/completions", "/chat/completions", "POST"},
		{"/v1/xai/chat/completions", "/chat/completions", "POST"},
		{"/v1/openrouter/models/anthropic/claude-sonnet-4.5/endpoints", "/models/anthropic/claude-sonnet-4.5/endpoints", "GET"},
		{"/v1/openrouter/keys/key-hash", "/keys/key-hash", "PATCH"},
		{"/v1/responses/rsp_123/cancel", "/responses/rsp_123/cancel", "POST"},
		{"/v1/models/model-id", "/models/model-id", "GET"},
	} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(tt.method, tt.path, nil))
		if rr.Code != 200 || gotPath != tt.endpoint || gotMethod != tt.method {
			t.Fatalf("routing %s: %d path=%s method=%s", tt.path, rr.Code, gotPath, gotMethod)
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "test.wav")
	_, _ = part.Write([]byte{0, 1, 2, 255, 10})
	_ = writer.WriteField("model", "transcription-model")
	_ = writer.Close()
	r := httptest.NewRequest("POST", "/v1/openai/audio/transcriptions", bytes.NewReader(body.Bytes()))
	r.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, r)
	if rr.Code != 200 || !bytes.Equal(gotBody, body.Bytes()) || gotPath != "/audio/transcriptions" {
		t.Fatal("multipart body contract lost")
	}
}

func TestNativeBufferedStreamRestoresSplitTokenAndRejectsTruncation(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	for _, truncate := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&input)
			text := input.Messages[0].Content
			parts := []string{text[:len(text)/2], text[len(text)/2:]}
			w.Header().Set("Content-Type", "text/event-stream")
			for _, part := range parts {
				payload, _ := json.Marshal(struct {
					Choices []struct {
						Index int `json:"index"`
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}{Choices: []struct {
					Index int `json:"index"`
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				}{{Delta: struct {
					Content string `json:"content"`
				}{Content: part}}}})
				_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
			}
			if !truncate {
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}
		}))
		reg := providers.NewRegistry()
		reg.Register(providers.NewOpenAIClientWithBaseURL(server.URL))
		profile := security.DefaultProfile()
		profile.PIIAction = security.ActionTokenize
		h := NewHandler(reg, nil, security.NewEngine(detection.NewPIIDetector()), &nativeTestProfile{profile: &profile}, nil, "fail-open")
		rr := httptest.NewRecorder()
		h.ChatCompletions(rr, nativeRequest("POST", "/v1/chat/completions", `{"model":"test","stream":true,"messages":[{"role":"user","content":"123-45-6789"}]}`))
		if truncate {
			if rr.Code != 502 {
				t.Fatalf("truncated stream succeeded: %d %s", rr.Code, rr.Body.String())
			}
		} else if rr.Code != 200 || !strings.Contains(rr.Body.String(), "123-45-6789") {
			t.Fatalf("split-token restoration failed: %d %s", rr.Code, rr.Body.String())
		}
		server.Close()
	}
}

func TestNativeImageAllowlist(t *testing.T) {
	for _, body := range []string{
		`{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://untrusted.example/image.png"}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://untrusted.example/image.png"}}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"image_url","image_url":{"url":"https://untrusted.example/image.png"}}]}]}]}`,
	} {
		if _, allowed := validateImageContent([]byte(body), []string{"trusted.example"}); allowed {
			t.Fatalf("untrusted image accepted: %s", body)
		}
		body = strings.ReplaceAll(body, "untrusted.example", "trusted.example")
		if rejected, allowed := validateImageContent([]byte(body), []string{"trusted.example"}); !allowed {
			t.Fatalf("trusted image rejected: %s", rejected)
		}
	}
}

func TestRequestTokenMapsDoNotRestoreProviderStoredTokens(t *testing.T) {
	previous := security.NewRequestTokenMap(security.TokenStyleAngle)
	previousToken := previous.Add("ssn", "123-45-6789")
	current := security.NewRequestTokenMap(security.TokenStyleAngle)
	currentToken := current.Add("ssn", "987-65-4321")
	if currentToken == previousToken {
		t.Fatal("request namespaces collide")
	}
	restored, n := current.Restore(previousToken + " " + currentToken)
	if n != 1 || restored != previousToken+" 987-65-4321" {
		t.Fatalf("provider-stored token restored using another request: %s", restored)
	}
}

func TestOpenRouterReasoningDetailChannels(t *testing.T) {
	channels := make(map[string]string)
	for _, raw := range []string{
		`{"choices":[{"index":0,"delta":{"reasoning_details":[{"index":0,"type":"reasoning.text","text":"SSN 123-"}]}}]}`,
		`{"choices":[{"index":0,"delta":{"reasoning_details":[{"index":0,"type":"reasoning.text","text":"45-6789"}]}}]}`,
	} {
		mapStreamDeltas([]byte(raw), func(key, value string) string { channels[key] += value; return value })
	}
	if len(channels) != 1 || channels["choice:0:reasoning:0:text"] != "SSN 123-45-6789" {
		t.Fatalf("reasoning fragments not joined: %v", channels)
	}
}

func TestClaudeSignedThinkingIsReusableWithPIIRestoration(t *testing.T) {
	tm := security.NewTokenMap(security.TokenStyleAngle)
	token := tm.Add("ssn", "123-45-6789")
	profile := security.DefaultProfile()
	profile.PIIScanResponse = true
	profile.PIIRestoreResponse = true
	ctx := security.WithTokenMap(security.WithProfile(context.Background(), &profile), tm)
	r := nativeRequest("POST", "/v1/anthropic/messages", "").WithContext(ctx)
	h := NewHandler(providers.NewRegistry(), nil, security.NewEngine(detection.NewPIIDetector()), nil, nil, "fail-open")
	body := []byte(`{"content":[{"type":"thinking","thinking":"` + token + `","signature":"keep-signature"},{"type":"text","text":"` + token + `"}]}`)
	out, blocked, reason := h.postProcessNativeResponse(r, body)
	var result struct {
		Content []struct {
			Thinking  string `json:"thinking"`
			Text      string `json:"text"`
			Signature string `json:"signature"`
		} `json:"content"`
	}
	_ = json.Unmarshal(out, &result)
	if blocked || len(result.Content) != 2 || result.Content[0].Thinking != token || result.Content[0].Signature != "keep-signature" || result.Content[1].Text != "123-45-6789" {
		t.Fatalf("signed content or normal restoration broken: %s %s", out, reason)
	}
	body = []byte(`{"content":[{"type":"thinking","thinking":"SSN 123-45-6789","signature":"keep-signature"}]}`)
	if _, blocked, _ := h.postProcessNativeResponse(r, body); !blocked {
		t.Fatal("policy rewrote signed thinking instead of blocking")
	}
	stream := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"" + token + "\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"keep-signature\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	rr := httptest.NewRecorder()
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
	_, _, _, status := h.forwardNativeStream(rr, r, resp, &nativeMetadata{})
	if status != "ok" || rr.Body.String() != stream {
		t.Fatalf("signed stream changed: %s %s", status, rr.Body.String())
	}
}
