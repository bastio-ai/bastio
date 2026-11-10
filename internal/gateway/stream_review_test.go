package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
)

// --- observed stream text is taken once -------------------------------

func observedTexts(frames ...string) []string {
	o := newStreamObserver()
	for _, f := range frames {
		o.addFrame([]byte(f))
	}
	c := &responseFindings{seen: make(map[string]bool)}
	c.observe(o)
	return c.takeObserved()
}

func TestStreamObserverTakesTextOnce(t *testing.T) {
	for name, tc := range map[string]struct {
		frames []string
		want   string
	}{
		"responses with deltas and done events": {[]string{
			`{"type":"response.created","response":{"id":"r1","output":[]}}`,
			`{"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"Hello "}`,
			`{"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"world"}`,
			`{"type":"response.output_text.done","item_id":"m1","output_index":0,"content_index":0,"text":"Hello world"}`,
			`{"type":"response.content_part.done","item_id":"m1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"Hello world"}}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"id":"m1","type":"message","content":[{"type":"output_text","text":"Hello world"}]}}`,
			`{"type":"response.completed","response":{"id":"r1","output":[{"id":"m1","type":"message","content":[{"type":"output_text","text":"Hello world"}]}]}}`,
		}, "Hello world"},
		"responses without deltas": {[]string{
			`{"type":"response.output_item.done","output_index":0,"item":{"id":"m1","type":"message","content":[{"type":"output_text","text":"Hello"}]}}`,
			`{"type":"response.completed","response":{"id":"r1","output":[{"id":"m1","type":"message","content":[{"type":"output_text","text":"Hello world"}]}]}}`,
		}, "Hello world"},
		"chat completions": {[]string{
			`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"Hello world"}}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1}}`,
		}, "Hello world"},
		"claude messages": {[]string{
			`{"type":"message_start","message":{"id":"m","content":[]}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello world"}}`,
			`{"type":"message_stop"}`,
		}, "Hello world"},
		"gemini chunks": {[]string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello "}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"world"}]}}],"usageMetadata":{"promptTokenCount":1}}`,
		}, "Hello world"},
	} {
		got := strings.Join(observedTexts(tc.frames...), "|")
		if got != tc.want {
			t.Errorf("%s: observed %q, want %q", name, got, tc.want)
		}
	}
}

// A held-back Responses stream repeats its text in done/completed events;
// each distinct text is scanned once, not once per event.
func TestHeldBackStreamScansRepeatedTextOnce(t *testing.T) {
	profile := exfilProfile(security.ActionRedact)
	var calls atomic.Int32
	engine := security.NewEngine(countingDetector{inner: detection.NewExfilDetector(), calls: &calls}, detection.NewSecretsDetector(), detection.NewPIIDetector())
	h := NewHandler(providers.NewRegistry(), nil, engine, nil, nil, "fail-open")
	r := nativeRequest(http.MethodPost, "/v1/responses", "")
	r = r.WithContext(security.WithProfile(r.Context(), &profile))
	text := "A harmless synthetic answer."
	scanAssistantTextWithProfile(r.Context(), engine, text)
	once := calls.Swap(0)
	q := jsonString(text)
	payloads := [][]byte{
		[]byte(`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":` + q + `}`),
		[]byte(`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":` + q + `}`),
		[]byte(`{"type":"response.content_part.done","output_index":0,"content_index":0,"part":{"type":"output_text","text":` + q + `}}`),
		[]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","content":[{"type":"output_text","text":` + q + `}]}}`),
		[]byte(`{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":` + q + `}]}]}}`),
	}
	if _, blocked, reason := h.scanHeldBackStream(r, payloads, nil, true, false); blocked {
		t.Fatalf("blocked: %s", reason)
	}
	if got := calls.Load(); got != once {
		t.Fatalf("exfil detector ran %d times for one repeated text, want %d", got, once)
	}
}

// --- scan slots ---------------------------------------------------------

func TestObservedScanSlotsForCPUs(t *testing.T) {
	for procs, want := range map[int]int{1: 1, 2: 1, 4: 1, 5: 1, 8: 2, 16: 4} {
		if got := observedScanSlotsFor(procs); got != want {
			t.Errorf("GOMAXPROCS %d: %d slots, want %d", procs, got, want)
		}
	}
}

// --- background scan failure ---------------------------------------------

type panicDetector struct{}

func (panicDetector) Name() string { return "exfil" }
func (panicDetector) Detect(_ context.Context, content string) ([]security.Finding, error) {
	if strings.Contains(content, "SYNTHETIC-PANIC") {
		panic("synthetic detector failure")
	}
	return nil, nil
}

// A panic in the after-stream scan is recovered; the trace is still written.
func TestObservedScanPanicStillWritesTrace(t *testing.T) {
	srv := streamUpstream(t, sseChunk("SYNTHETIC-PANIC text")+"data: [DONE]\n\n")
	reg := providers.NewRegistry()
	reg.Register(providers.NewDeepSeekClientWithBaseURL(srv.URL))
	rec := bufferingRecorder()
	profile := exfilProfile(security.ActionLogOnly)
	h := NewHandler(reg, nil, security.NewEngine(panicDetector{}), staticProfileLookup{profile}, rec, "fail-open")
	h.SetNativeKeyResolver(func(*http.Request, providers.Provider) string { return "upstream-test-key" })
	rr := httptest.NewRecorder()
	streamChat(h, rr, "deepseek-chat")
	traces, _ := collectRecorded(t, rec, func([]*observability.ThreatEvent) bool { return true })
	if len(traces) != 1 {
		t.Fatalf("got %d traces after a failed scan, want 1", len(traces))
	}
	if traces[0].Tags["response_scan_failed"] != "true" {
		t.Fatalf("failed scan not noted: %v", traces[0].Tags)
	}
}

// Writing after the recorder closed must not panic.
func TestObservedScanAfterRecorderClose(t *testing.T) {
	srv := streamUpstream(t, sseChunk("Here is the badge: ")+sseChunk(holdbackImageExfil)+"data: [DONE]\n\n")
	reg := providers.NewRegistry()
	reg.Register(providers.NewDeepSeekClientWithBaseURL(srv.URL))
	rec := bufferingRecorder()
	_ = rec.Close(context.Background())
	h := responseFindingHandler(reg, exfilProfile(security.ActionLogOnly), rec)
	streamChat(h, httptest.NewRecorder(), "deepseek-chat")
	time.Sleep(200 * time.Millisecond) // let the background scan run
}

// --- held-back streams and proxy idle timeouts ----------------------------

// slowUpstream sends first, waits pause, then sends rest.
func slowUpstream(t *testing.T, first, rest string, pause time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		_, _ = io.WriteString(w, first)
		f.Flush()
		select {
		case <-time.After(pause):
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, rest)
		f.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func keepAliveHandler(reg *providers.Registry, profile security.Profile, rec *observability.Recorder) *Handler {
	h := responseFindingHandler(reg, profile, rec)
	h.SetHoldBackKeepAlive(20*time.Millisecond, 10*time.Millisecond)
	return h
}

// Finishing within the grace period keeps the 403 before any byte.
func TestHeldBackFastBlockStays403(t *testing.T) {
	for _, route := range streamRoutes() {
		t.Run(route.name, func(t *testing.T) {
			srv := streamUpstream(t, sseChunk("Here is the badge: ")+sseChunk(holdbackImageExfil)+"data: [DONE]\n\n")
			reg := providers.NewRegistry()
			reg.Register(route.register(srv.URL))
			h := responseFindingHandler(reg, exfilProfile(security.ActionBlock), bufferingRecorder())
			h.SetHoldBackKeepAlive(5*time.Second, time.Second)
			rr := httptest.NewRecorder()
			streamChat(h, rr, route.model)
			if rr.Code != http.StatusForbidden || strings.Contains(rr.Body.String(), "keepalive") {
				t.Fatalf("fast block: %d %s", rr.Code, rr.Body.String())
			}
		})
	}
}

// A block decided after headers were committed ends the stream with a
// provider-shaped error event, no model content and no done marker.
func TestHeldBackSlowBlockSendsErrorEvent(t *testing.T) {
	for _, route := range streamRoutes() {
		t.Run(route.name, func(t *testing.T) {
			srv := slowUpstream(t, sseChunk("Here is the badge: "), sseChunk(holdbackImageExfil)+"data: [DONE]\n\n", 150*time.Millisecond)
			reg := providers.NewRegistry()
			reg.Register(route.register(srv.URL))
			rec := bufferingRecorder()
			h := keepAliveHandler(reg, exfilProfile(security.ActionBlock), rec)
			rr := httptest.NewRecorder()
			streamChat(h, rr, route.model)
			body := rr.Body.String()
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d, want 200 (committed): %s", rr.Code, body)
			}
			if !strings.Contains(body, ": keepalive\n\n") {
				t.Fatalf("no keep-alive sent: %q", body)
			}
			if strings.Contains(body, "Here is the badge") || strings.Contains(body, "QWxsIG9m") || strings.Contains(body, "[DONE]") {
				t.Fatalf("blocked stream leaked content or finished normally: %q", body)
			}
			if !strings.Contains(body, `data: {"error":{"message":"`) || !strings.Contains(body, "data exfiltration detected in response") {
				t.Fatalf("no OpenAI-shaped error event: %q", body)
			}
			traces, _ := collectRecorded(t, rec, hasExfilRow)
			if len(traces) != 1 || traces[0].Status != "blocked" || traces[0].SecurityAction != "block" {
				t.Fatalf("trace %+v, want blocked", traces)
			}
			if traces[0].HTTPStatus != 200 || traces[0].Tags["response_blocked_in_stream"] != "true" {
				t.Fatalf("trace http=%d tags=%v, want 200 and response_blocked_in_stream", traces[0].HTTPStatus, traces[0].Tags)
			}
		})
	}
}

// A pass decided after keep-alives releases the full stream unchanged.
func TestHeldBackSlowPassReleasesStream(t *testing.T) {
	for _, route := range streamRoutes() {
		t.Run(route.name, func(t *testing.T) {
			srv := slowUpstream(t, sseChunk("A harmless "), sseChunk("synthetic answer.")+"data: [DONE]\n\n", 150*time.Millisecond)
			reg := providers.NewRegistry()
			reg.Register(route.register(srv.URL))
			h := keepAliveHandler(reg, exfilProfile(security.ActionBlock), bufferingRecorder())
			rr := httptest.NewRecorder()
			streamChat(h, rr, route.model)
			body := rr.Body.String()
			if rr.Code != http.StatusOK || !strings.HasPrefix(body, ": keepalive\n\n") {
				t.Fatalf("status %d, want keep-alives first: %q", rr.Code, body)
			}
			if !strings.Contains(body, "A harmless ") || !strings.Contains(body, "synthetic answer.") || !strings.Contains(body, "[DONE]") {
				t.Fatalf("stream not released in full: %q", body)
			}
			if i, j := strings.LastIndex(body, ": keepalive"), strings.Index(body, "A harmless"); i > j {
				t.Fatalf("keep-alive after content: %q", body)
			}
		})
	}
}

// The client leaving during hold-back ends the request without content.
func TestHeldBackClientDisconnect(t *testing.T) {
	for _, route := range streamRoutes() {
		t.Run(route.name, func(t *testing.T) {
			srv := slowUpstream(t, sseChunk("A harmless "), sseChunk("answer.")+"data: [DONE]\n\n", 5*time.Second)
			reg := providers.NewRegistry()
			reg.Register(route.register(srv.URL))
			rec := bufferingRecorder()
			h := keepAliveHandler(reg, exfilProfile(security.ActionBlock), rec)
			r := nativeRequest(http.MethodPost, "/v1/chat/completions", `{"model":"`+route.model+`","stream":true,"messages":[{"role":"user","content":"Hi."}]}`)
			ctx, cancel := context.WithCancel(r.Context())
			r = r.WithContext(ctx)
			w := newFlushWriter()
			done := make(chan struct{})
			go func() { h.ChatCompletions(w, r); close(done) }()
			time.Sleep(60 * time.Millisecond) // past the grace period
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not return after the client left")
			}
			if strings.Contains(w.body(), "A harmless") {
				t.Fatalf("content released to a departed client: %q", w.body())
			}
			traces, _ := collectRecorded(t, rec, func([]*observability.ThreatEvent) bool { return true })
			if len(traces) != 1 || traces[0].Status != "error" {
				t.Fatalf("traces %+v, want one error trace", traces)
			}
		})
	}
}

func TestInStreamErrorShapes(t *testing.T) {
	for endpoint, want := range map[string]string{
		"/chat/completions":                    `data: {"error":{"message":"m","type":"gateway_error","code":"response_blocked"}}` + "\n\n",
		"/messages":                            "event: error\n" + `data: {"type":"error","error":{"type":"permission_error","message":"m"}}` + "\n\n",
		"/responses":                           "event: error\n" + `data: {"type":"error","code":"response_blocked","message":"m","param":null,"error":{"type":"gateway_error","code":"response_blocked","message":"m"}}` + "\n\n",
		"/models/gemini:streamGenerateContent": `data: {"error":{"code":403,"message":"m","status":"PERMISSION_DENIED"}}` + "\n\n",
	} {
		if got := string(inStreamErrorEvent(endpoint, "response_blocked", "m")); got != want {
			t.Errorf("%s:\n got %q\nwant %q", endpoint, got, want)
		}
	}
}

// --- exfil redact -------------------------------------------------------

// Redact removes the exfiltrating URL on every route, streaming or not.
func TestExfilRedactRemovesURL(t *testing.T) {
	for _, route := range responseRoutes() {
		t.Run(route.name, func(t *testing.T) {
			up := newCapturingUpstream(t, route.reply)
			reg := providers.NewRegistry()
			reg.Register(route.register(up.srv.URL))
			rec := bufferingRecorder()
			h := responseFindingHandler(reg, exfilProfile(security.ActionRedact), rec)
			rr := httptest.NewRecorder()
			route.run(h, rr)
			assertRedacted(t, rr, rec)
		})
	}
	for _, route := range streamRoutes() {
		t.Run("stream/"+route.name, func(t *testing.T) {
			srv := streamUpstream(t, sseChunk("Here is the badge: ")+sseChunk(holdbackImageExfil)+"data: [DONE]\n\n")
			reg := providers.NewRegistry()
			reg.Register(route.register(srv.URL))
			rec := bufferingRecorder()
			h := responseFindingHandler(reg, exfilProfile(security.ActionRedact), rec)
			rr := httptest.NewRecorder()
			streamChat(h, rr, route.model)
			assertRedacted(t, rr, rec)
		})
	}
}

func assertRedacted(t *testing.T, rr *httptest.ResponseRecorder, rec *observability.Recorder) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "QWxsIG9m") || strings.Contains(body, "evil.example") {
		t.Fatalf("redacted response still carries the URL: %s", body)
	}
	if !strings.Contains(body, "[link removed]") || !strings.Contains(body, "Here is the badge") {
		t.Fatalf("redacted response lost its marker or text: %s", body)
	}
	_, threats := collectRecorded(t, rec, hasExfilRow)
	if rows := exfilRows(threats); len(rows) != 1 || rows[0].Action != "mask" {
		t.Fatalf("exfil rows %+v, want one mask row", rows)
	}
}

type nonSanitizingExfil struct{ inner *detection.ExfilDetector }

func (d nonSanitizingExfil) Name() string { return d.inner.Name() }
func (d nonSanitizingExfil) Detect(ctx context.Context, content string) ([]security.Finding, error) {
	return d.inner.Detect(ctx, content)
}

// A redact the detector cannot apply blocks rather than deliver the URL.
func TestUnappliedRedactFallsBackToBlock(t *testing.T) {
	route := responseRoutes()[0]
	up := newCapturingUpstream(t, route.reply)
	reg := providers.NewRegistry()
	reg.Register(route.register(up.srv.URL))
	rec := bufferingRecorder()
	engine := security.NewEngine(nonSanitizingExfil{detection.NewExfilDetector()}, detection.NewSecretsDetector(), detection.NewPIIDetector())
	h := NewHandler(reg, nil, engine, staticProfileLookup{exfilProfile(security.ActionRedact)}, rec, "fail-open")
	h.SetNativeKeyResolver(func(*http.Request, providers.Provider) string { return "upstream-test-key" })
	rr := httptest.NewRecorder()
	route.run(h, rr)
	if rr.Code != http.StatusForbidden || strings.Contains(rr.Body.String(), "QWxsIG9m") {
		t.Fatalf("unapplied redact delivered: %d %s", rr.Code, rr.Body.String())
	}
	if rows := exfilRows(rec.PendingThreats()); len(rows) != 1 || rows[0].Action != "block" {
		t.Fatalf("exfil rows %+v, want one block row", rows)
	}
}
