package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
)

// Streams whose output steps only observe pass through unchanged, and the
// streamed text is scanned after the stream ends. Streams on the normalized
// (non-native) retry path get the same hold-back and scan-after behavior as
// native streams.

// collectRecorded drains the recorder until it holds a trace and enough
// threat rows satisfy done. The scan after a pass-through stream records
// off the request goroutine, so the rows can arrive after the handler
// returned.
func collectRecorded(t *testing.T, rec *observability.Recorder, done func([]*observability.ThreatEvent) bool) ([]*observability.TraceRecord, []*observability.ThreatEvent) {
	t.Helper()
	var traces []*observability.TraceRecord
	var threats []*observability.ThreatEvent
	deadline := time.Now().Add(15 * time.Second)
	for {
		traces = append(traces, rec.PendingTraces()...)
		threats = append(threats, rec.PendingThreats()...)
		if len(traces) > 0 && done(threats) {
			return traces, threats
		}
		if time.Now().After(deadline) {
			return traces, threats
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func hasExfilRow(rows []*observability.ThreatEvent) bool { return len(exfilRows(rows)) > 0 }

type streamRoute struct {
	name     string
	register func(url string) providers.Client
	model    string
}

func streamRoutes() []streamRoute {
	return []streamRoute{
		{"native openai", func(u string) providers.Client { return providers.NewOpenAIClientWithBaseURL(u) }, "gpt-4o-mini"},
		{"normalized deepseek", func(u string) providers.Client { return providers.NewDeepSeekClientWithBaseURL(u) }, "deepseek-chat"},
	}
}

func streamUpstream(t *testing.T, stream string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func streamChat(h *Handler, w http.ResponseWriter, model string) {
	h.ChatCompletions(w, nativeRequest(http.MethodPost, "/v1/chat/completions", `{"model":"`+model+`","stream":true,"messages":[{"role":"user","content":"Show the status badge."}]}`))
}

// FIX 1: observe-only output steps on a stream: the client gets the stream
// unchanged and unbuffered, and the trace still records the finding with
// source response and the step's real non-blocking action.
func TestPassThroughStreamRecordsResponseFindings(t *testing.T) {
	stream := sseChunk("Here is the badge: ") + sseChunk(holdbackImageExfil) + "data: [DONE]\n\n"
	for _, strategy := range []security.Action{security.ActionWarn, security.ActionLogOnly} {
		for _, route := range streamRoutes() {
			t.Run(string(strategy)+"/"+route.name, func(t *testing.T) {
				srv := streamUpstream(t, stream)
				reg := providers.NewRegistry()
				reg.Register(route.register(srv.URL))
				rec := bufferingRecorder()
				h := responseFindingHandler(reg, exfilProfile(strategy), rec)
				rr := httptest.NewRecorder()
				streamChat(h, rr, route.model)
				if rr.Code != http.StatusOK {
					t.Fatalf("status %d, want 200: %s", rr.Code, rr.Body.String())
				}
				if got := rr.Header().Get("X-Bastio-Stream-Buffered"); got != "" {
					t.Fatalf("observe-only stream held back: %q", got)
				}
				if !strings.Contains(rr.Body.String(), "QWxsIG9m") || !strings.Contains(rr.Body.String(), "Here is the badge") {
					t.Fatalf("observe-only stream changed: %s", rr.Body.String())
				}
				traces, threats := collectRecorded(t, rec, hasExfilRow)
				rows := exfilRows(threats)
				if len(rows) != 1 {
					t.Fatalf("got %d exfil threat rows, want 1", len(rows))
				}
				if rows[0].Source != "response" || rows[0].Action != string(strategy) {
					t.Fatalf("row source=%q action=%q, want response/%s", rows[0].Source, rows[0].Action, strategy)
				}
				if len(traces) != 1 || !traces[0].ThreatDetected || rows[0].TraceID != traces[0].ID {
					t.Fatalf("trace does not carry the response finding: %+v", traces)
				}
				if traces[0].SecurityAction == "block" || traces[0].Status != "ok" || traces[0].HTTPStatus != 200 {
					t.Fatalf("observe-only stream recorded as status=%q http=%d action=%q", traces[0].Status, traces[0].HTTPStatus, traces[0].SecurityAction)
				}
			})
		}
	}
}

// FIX 1: the accumulated text is bounded; the trace notes the truncation.
func TestPassThroughStreamScanIsBounded(t *testing.T) {
	filler := strings.Repeat("lorem ipsum ", 700) // ~8 KB per chunk
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString(sseChunk(filler))
	}
	b.WriteString("data: [DONE]\n\n")
	for _, route := range streamRoutes() {
		t.Run(route.name, func(t *testing.T) {
			srv := streamUpstream(t, b.String())
			reg := providers.NewRegistry()
			reg.Register(route.register(srv.URL))
			rec := bufferingRecorder()
			h := responseFindingHandler(reg, exfilProfile(security.ActionLogOnly), rec)
			rr := httptest.NewRecorder()
			streamChat(h, rr, route.model)
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
			}
			var traces []*observability.TraceRecord
			deadline := time.Now().Add(15 * time.Second)
			for len(traces) == 0 && time.Now().Before(deadline) {
				traces = append(traces, rec.PendingTraces()...)
				time.Sleep(5 * time.Millisecond)
			}
			if len(traces) != 1 {
				t.Fatalf("got %d traces, want 1", len(traces))
			}
			if traces[0].Tags["response_scan_truncated"] != "true" {
				t.Fatalf("truncated response scan not noted on the trace: %v", traces[0].Tags)
			}
		})
	}
}

// FIX 2: an enforcing output step holds the normalized-path stream back:
// block returns the block error with no leaked content, mask rewrites.
func TestNormalizedStreamHoldsBackEnforcingScan(t *testing.T) {
	stream := sseChunk("Here is the badge: ") + sseChunk(holdbackImageExfil) + "data: [DONE]\n\n"
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
			srv := streamUpstream(t, stream)
			reg := providers.NewRegistry()
			reg.Register(providers.NewDeepSeekClientWithBaseURL(srv.URL))
			rec := bufferingRecorder()
			h := responseFindingHandler(reg, exfilProfile(tc.strategy), rec)
			rr := httptest.NewRecorder()
			streamChat(h, rr, "deepseek-chat")
			if rr.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d: %s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if rr.Header().Get("X-Bastio-Stream-Buffered") == "" {
				t.Fatal("stream was not held back")
			}
			// The exfil step's redact keeps the text today (same on the native
			// path); rewriting itself is covered by the secrets test below.
			if tc.wantStatus == http.StatusForbidden && (strings.Contains(rr.Body.String(), "QWxsIG9m") || strings.Contains(rr.Body.String(), "Here is the badge")) {
				t.Fatalf("blocked stream leaked content: %s", rr.Body.String())
			}
			if tc.wantStatus == http.StatusOK && (!strings.Contains(rr.Body.String(), "Here is the badge") || !strings.Contains(rr.Body.String(), "[DONE]")) {
				t.Fatalf("masked stream lost content or its terminator: %s", rr.Body.String())
			}
			traces, threats := collectRecorded(t, rec, hasExfilRow)
			rows := exfilRows(threats)
			if len(rows) != 1 || rows[0].Source != "response" || rows[0].Action != tc.wantAction {
				t.Fatalf("exfil rows %+v, want one response/%s row", rows, tc.wantAction)
			}
			if len(traces) != 1 || !traces[0].ThreatDetected {
				t.Fatalf("trace does not carry the response finding: %+v", traces)
			}
			if tc.wantStatus == http.StatusForbidden && (traces[0].SecurityAction != "block" || traces[0].Status != "blocked" || traces[0].HTTPStatus != 403) {
				t.Fatalf("blocked stream recorded as status=%q http=%d action=%q", traces[0].Status, traces[0].HTTPStatus, traces[0].SecurityAction)
			}
			if tc.wantStatus == http.StatusOK && traces[0].SecurityAction == "block" {
				t.Fatal("masked stream recorded as a block")
			}
		})
	}
}

// FIX 2: a mask step rewrites a secret even when the stream splits it
// across chunks, because the held-back channel is scanned as one text.
func TestNormalizedStreamMasksSplitSecret(t *testing.T) {
	stream := sseChunk("key: "+testAWSKey[:8]) + sseChunk(testAWSKey[8:]+" done") + "data: [DONE]\n\n"
	srv := streamUpstream(t, stream)
	reg := providers.NewRegistry()
	reg.Register(providers.NewDeepSeekClientWithBaseURL(srv.URL))
	profile := observeOnlyOutputProfile()
	profile.SecretsStrategy = security.ActionMask
	rec := bufferingRecorder()
	h := responseFindingHandler(reg, profile, rec)
	rr := httptest.NewRecorder()
	streamChat(h, rr, "deepseek-chat")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), testAWSKey[8:]) {
		t.Fatalf("split secret leaked: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), " done") || !strings.Contains(rr.Body.String(), "[DONE]") {
		t.Fatalf("masked stream lost content: %s", rr.Body.String())
	}
	traces, threats := collectRecorded(t, rec, func(rows []*observability.ThreatEvent) bool { return len(rows) > 0 })
	if len(traces) != 1 || len(threats) == 0 || threats[0].Source != "response" || threats[0].Action != "mask" {
		t.Fatalf("traces %d, threats %+v: want a response/mask row", len(traces), threats)
	}
}

// FIX 2 guard: observe-only output on the normalized path still forwards
// each chunk as it arrives, before the upstream finishes.
func TestNormalizedStreamObservePassesThroughIncrementally(t *testing.T) {
	chunks := []string{sseChunk("Here "), sseChunk("is the badge: "), sseChunk(holdbackImageExfil)}
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			f.Flush()
			<-gate
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		f.Flush()
	}))
	defer srv.Close()
	reg := providers.NewRegistry()
	reg.Register(providers.NewDeepSeekClientWithBaseURL(srv.URL))
	h := responseFindingHandler(reg, exfilProfile(security.ActionLogOnly), bufferingRecorder())
	w := newFlushWriter()
	done := make(chan struct{})
	go func() {
		streamChat(h, w, "deepseek-chat")
		close(done)
	}()
	for i, want := range []string{"Here ", "is the badge: ", "QWxsIG9m"} {
		select {
		case got := <-w.flushes:
			if !strings.Contains(got, want) {
				t.Fatalf("chunk %d flushed as %q, want it to contain %q", i, got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("chunk %d not flushed before upstream finished: stream was held back", i)
		}
		gate <- struct{}{}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish")
	}
	if got := w.Header().Get("X-Bastio-Stream-Buffered"); got != "" {
		t.Fatalf("observe-only stream marked buffered: %q", got)
	}
}
