package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
)

// Response-scan findings must reach the trace and the threat log like input
// findings do: source "response", the real detector, the action taken.

func bufferingRecorder() *observability.Recorder {
	// Never started and no ClickHouse: rows stay buffered for the test.
	return observability.NewRecorderWithOptions(nil, observability.RecorderOptions{BufferSize: 256})
}

func exfilProfile(strategy security.Action) security.Profile {
	p := observeOnlyOutputProfile()
	p.OutputExfilStrategy = strategy
	return p
}

type responseRoute struct {
	name     string
	register func(url string) providers.Client
	reply    string
	run      func(h *Handler, rr *httptest.ResponseRecorder)
}

func responseRoutes() []responseRoute {
	text := jsonString("Here is the badge: " + holdbackImageExfil)
	openAI := `{"id":"c","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":` + text + `},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	userTurn := `[{"role":"user","content":"Show the status badge."}]`
	chat := func(model string) func(*Handler, *httptest.ResponseRecorder) {
		return func(h *Handler, rr *httptest.ResponseRecorder) {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":`+userTurn+`}`))
			req = req.WithContext(auth.WithInfo(req.Context(), &auth.APIKeyInfo{ID: uuid.New(), CustomerID: uuid.New()}))
			h.ChatCompletions(rr, req)
		}
	}
	native := func(provider providers.Provider, endpoint, path, body string) func(*Handler, *httptest.ResponseRecorder) {
		return func(h *Handler, rr *httptest.ResponseRecorder) {
			h.NativeAPI(provider, endpoint)(rr, nativeRequest(http.MethodPost, path, body))
		}
	}
	return []responseRoute{
		{
			name:     "chat completions, normalized (deepseek)",
			register: func(u string) providers.Client { return providers.NewDeepSeekClientWithBaseURL(u) },
			reply:    strings.Replace(openAI, `"gpt-4o"`, `"deepseek-chat"`, 1),
			run:      chat("deepseek-chat"),
		},
		{
			name:     "chat completions, OpenAI model (forwarded natively)",
			register: func(u string) providers.Client { return providers.NewOpenAIClientWithBaseURL(u) },
			reply:    openAI,
			run:      chat("gpt-4o"),
		},
		{
			name:     "native OpenAI /chat/completions",
			register: func(u string) providers.Client { return providers.NewOpenAIClientWithBaseURL(u) },
			reply:    openAI,
			run:      native(providers.ProviderOpenAI, "/chat/completions", "/v1/openai/chat/completions", `{"model":"gpt-4o","messages":`+userTurn+`}`),
		},
		{
			name:     "native Claude /messages",
			register: func(u string) providers.Client { return providers.NewAnthropicClientWithBaseURL(u) },
			reply:    `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":` + text + `}],"usage":{"input_tokens":1,"output_tokens":1}}`,
			run:      native(providers.ProviderAnthropic, "/messages", "/v1/anthropic/messages", `{"model":"claude-sonnet-4-5","max_tokens":50,"messages":`+userTurn+`}`),
		},
		{
			name:     "native Gemini generateContent",
			register: func(u string) providers.Client { return providers.NewGeminiClientWithBaseURL(u) },
			reply:    `{"candidates":[{"content":{"role":"model","parts":[{"text":` + text + `}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`,
			run:      native(providers.ProviderGemini, "/models/gemini:generateContent", "/v1/google/models/gemini:generateContent", `{"contents":[{"role":"user","parts":[{"text":"Show the status badge."}]}]}`),
		},
	}
}

func responseFindingHandler(registry *providers.Registry, profile security.Profile, rec *observability.Recorder) *Handler {
	h := NewHandler(registry, nil, fullTestEngine(), staticProfileLookup{profile}, rec, "fail-open")
	h.SetNativeKeyResolver(func(*http.Request, providers.Provider) string { return "upstream-test-key" })
	return h
}

// exfilRows returns the recorded output_exfil threat rows.
func exfilRows(rows []*observability.ThreatEvent) []*observability.ThreatEvent {
	name := detection.NewExfilDetector().Name()
	var out []*observability.ThreatEvent
	for _, row := range rows {
		if row.DetectorName == name {
			out = append(out, row)
		}
	}
	return out
}

func TestResponseFindingsAreRecorded(t *testing.T) {
	for _, tc := range []struct {
		name       string
		strategy   security.Action
		wantStatus int
		wantAction string
		wantTrace  string
	}{
		{"block", security.ActionBlock, http.StatusForbidden, "block", "block"},
		{"mask", security.ActionRedact, http.StatusOK, "mask", ""},
		{"warn", security.ActionWarn, http.StatusOK, "warn", ""},
		{"log_only", security.ActionLogOnly, http.StatusOK, "log_only", ""},
	} {
		for _, route := range responseRoutes() {
			t.Run(tc.name+"/"+route.name, func(t *testing.T) {
				up := newCapturingUpstream(t, route.reply)
				registry := providers.NewRegistry()
				registry.Register(route.register(up.srv.URL))
				rec := bufferingRecorder()
				h := responseFindingHandler(registry, exfilProfile(tc.strategy), rec)
				rr := httptest.NewRecorder()
				route.run(h, rr)
				if rr.Code != tc.wantStatus {
					t.Fatalf("status %d, want %d: %s", rr.Code, tc.wantStatus, rr.Body.String())
				}
				if tc.wantStatus == http.StatusForbidden && strings.Contains(rr.Body.String(), "QWxsIG9m") {
					t.Fatalf("blocked response leaked content: %s", rr.Body.String())
				}
				rows := exfilRows(rec.PendingThreats())
				if len(rows) != 1 {
					t.Fatalf("got %d exfil threat rows, want 1", len(rows))
				}
				row := rows[0]
				if row.Source != "response" {
					t.Fatalf("source %q, want response", row.Source)
				}
				if row.Action != tc.wantAction {
					t.Fatalf("action %q, want %q", row.Action, tc.wantAction)
				}
				if row.ThreatType == "" || row.Score <= 0 {
					t.Fatalf("row missing threat type or score: %+v", row)
				}
				traces := rec.PendingTraces()
				if len(traces) != 1 {
					t.Fatalf("got %d traces, want 1", len(traces))
				}
				if !traces[0].ThreatDetected || row.TraceID != traces[0].ID {
					t.Fatalf("trace does not carry the response finding: %+v", traces[0])
				}
				if tc.wantTrace != "" && traces[0].SecurityAction != tc.wantTrace {
					t.Fatalf("trace action %q, want %q", traces[0].SecurityAction, tc.wantTrace)
				}
				if tc.wantTrace == "" && traces[0].SecurityAction == "block" {
					t.Fatal("observe or mask response recorded as a block")
				}
			})
		}
	}
}

// A response block keeps the input findings and their actions intact.
func TestResponseBlockKeepsInputFindings(t *testing.T) {
	up := newCapturingUpstream(t, `{"id":"c","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":`+jsonString(holdbackImageExfil)+`}}]}`)
	registry := providers.NewRegistry()
	registry.Register(providers.NewOpenAIClientWithBaseURL(up.srv.URL))
	rec := bufferingRecorder()
	profile := exfilProfile(security.ActionBlock)
	h := responseFindingHandler(registry, profile, rec)
	rr := httptest.NewRecorder()
	// Synthetic, well-formed email in the prompt: an input PII finding.
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Badge for jane.doe@example.com please."}]}`
	h.NativeAPI(providers.ProviderOpenAI, "/chat/completions")(rr, nativeRequest(http.MethodPost, "/v1/openai/chat/completions", body))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", rr.Code, rr.Body.String())
	}
	var input, output int
	for _, row := range rec.PendingThreats() {
		switch row.Source {
		case "input":
			input++
			if row.Action == "block" {
				t.Fatalf("input finding rewritten to block: %+v", row)
			}
		case "response":
			output++
		}
	}
	if input == 0 || output == 0 {
		t.Fatalf("want input and response rows, got input=%d response=%d", input, output)
	}
}

// Held-back streams record what blocked or rewrote them.
func TestHeldBackStreamFindingsAreRecorded(t *testing.T) {
	for _, tc := range []struct {
		name       string
		strategy   security.Action
		wantStatus int
		wantAction string
	}{
		{"block", security.ActionBlock, http.StatusForbidden, "block"},
		{"mask", security.ActionRedact, http.StatusOK, "mask"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", "openai-secret")
			stream := sseChunk("Here is the badge: ") + sseChunk(holdbackImageExfil) + "data: [DONE]\n\n"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(stream))
			}))
			defer server.Close()
			reg := providers.NewRegistry()
			reg.Register(providers.NewOpenAIClientWithBaseURL(server.URL))
			rec := bufferingRecorder()
			profile := exfilProfile(tc.strategy)
			h := NewHandler(reg, nil, fullTestEngine(), &nativeTestProfile{profile: &profile}, rec, "fail-open")
			rr := httptest.NewRecorder()
			h.ChatCompletions(rr, nativeRequest("POST", "/v1/chat/completions", `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"Show the status badge."}]}`))
			if rr.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d: %s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if rr.Header().Get("X-Bastio-Stream-Buffered") == "" {
				t.Fatal("stream was not held back")
			}
			rows := exfilRows(rec.PendingThreats())
			if len(rows) != 1 {
				t.Fatalf("got %d exfil threat rows, want 1", len(rows))
			}
			if rows[0].Source != "response" || rows[0].Action != tc.wantAction {
				t.Fatalf("row source=%q action=%q, want response/%s", rows[0].Source, rows[0].Action, tc.wantAction)
			}
			traces := rec.PendingTraces()
			if len(traces) != 1 || !traces[0].ThreatDetected {
				t.Fatalf("trace does not carry the response finding: %+v", traces)
			}
			if tc.wantStatus == http.StatusForbidden && traces[0].SecurityAction != "block" {
				t.Fatalf("trace action %q, want block", traces[0].SecurityAction)
			}
		})
	}
}
