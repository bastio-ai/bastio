package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/gateway"
	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/google/uuid"
)

// A middleware wrapping the gateway handlers can replace the answer after
// the handler produced it. ResponseOutcome lets it report that, so the
// trace row matches what the client received.

type outcomeProfiles struct{ p security.Profile }

func (s outcomeProfiles) GetDefault(context.Context, uuid.UUID) (*security.Profile, error) {
	p := s.p
	return &p, nil
}

const outcomeReply = `{"id":"c","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"A synthetic, harmless answer."},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`

type outcomeRoute struct {
	name     string
	register func(url string) providers.Client
	serve    func(h *gateway.Handler) http.Handler
	body     string
}

func outcomeRoutes() []outcomeRoute {
	return []outcomeRoute{
		{
			name:     "chat completions, normalized",
			register: func(u string) providers.Client { return providers.NewDeepSeekClientWithBaseURL(u) },
			serve:    func(h *gateway.Handler) http.Handler { return http.HandlerFunc(h.ChatCompletions) },
			body:     `{"model":"deepseek-chat","messages":[{"role":"user","content":"Say something."}]}`,
		},
		{
			name:     "native openai",
			register: func(u string) providers.Client { return providers.NewOpenAIClientWithBaseURL(u) },
			serve: func(h *gateway.Handler) http.Handler {
				return h.NativeAPI(providers.ProviderOpenAI, "/chat/completions")
			},
			body: `{"model":"gpt-4o","messages":[{"role":"user","content":"Say something."}]}`,
		},
	}
}

func outcomeHandler(t *testing.T, route outcomeRoute, rec *observability.Recorder) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(outcomeReply))
	}))
	t.Cleanup(upstream.Close)
	reg := providers.NewRegistry()
	reg.Register(route.register(upstream.URL))
	profile := security.DefaultProfile()
	profile.PIIScanResponse = false
	h := gateway.NewHandler(reg, nil, security.NewEngine(detection.NewPIIDetector()), outcomeProfiles{profile}, rec, "fail-open")
	h.SetNativeKeyResolver(func(*http.Request, providers.Provider) string { return "upstream-test-key" })
	return route.serve(h)
}

// blockingWrapper stands in for an outer output-scan middleware: it buffers
// the handler's answer, then replaces it with a 403 when block is set.
func blockingWrapper(next http.Handler, block bool, beforeFinish func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, outcome := WithResponseOutcome(r.Context())
		defer outcome.Finish()
		captured := httptest.NewRecorder()
		next.ServeHTTP(captured, r.WithContext(ctx))
		if beforeFinish != nil {
			beforeFinish()
		}
		if block {
			outcome.Block("synthetic-output-engine", http.StatusForbidden)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"blocked by output policy"}`))
			return
		}
		for k, v := range captured.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(captured.Code)
		_, _ = w.Write(captured.Body.Bytes())
	})
}

func outcomeRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r.WithContext(auth.WithInfo(r.Context(), &auth.APIKeyInfo{ID: uuid.New(), CustomerID: uuid.New()}))
}

func TestResponseOutcomeBlockRecordsBlockedTrace(t *testing.T) {
	for _, route := range outcomeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			rec := observability.NewRecorderWithOptions(nil, observability.RecorderOptions{BufferSize: 64})
			var beforeFinish []*observability.TraceRecord
			wrapped := blockingWrapper(outcomeHandler(t, route, rec), true, func() { beforeFinish = rec.PendingTraces() })
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, outcomeRequest(route.body))
			if rr.Code != http.StatusForbidden {
				t.Fatalf("status %d, want 403", rr.Code)
			}
			if len(beforeFinish) != 0 {
				t.Fatalf("trace recorded before the wrapper decided: %+v", beforeFinish[0])
			}
			traces := rec.PendingTraces()
			if len(traces) != 1 {
				t.Fatalf("got %d traces, want 1", len(traces))
			}
			tr := traces[0]
			if tr.Status != "blocked" || tr.HTTPStatus != http.StatusForbidden || tr.SecurityAction != "block" {
				t.Fatalf("trace status=%q http=%d action=%q, want blocked/403/block", tr.Status, tr.HTTPStatus, tr.SecurityAction)
			}
			if tr.Tags["response_blocked_by"] != "synthetic-output-engine" {
				t.Fatalf("blocking engine not tagged: %v", tr.Tags)
			}
		})
	}
}

func TestResponseOutcomeWithoutBlockKeepsTrace(t *testing.T) {
	for _, route := range outcomeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			rec := observability.NewRecorderWithOptions(nil, observability.RecorderOptions{BufferSize: 64})
			rr := httptest.NewRecorder()
			blockingWrapper(outcomeHandler(t, route, rec), false, nil).ServeHTTP(rr, outcomeRequest(route.body))
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d, want 200: %s", rr.Code, rr.Body.String())
			}
			traces := rec.PendingTraces()
			if len(traces) != 1 {
				t.Fatalf("got %d traces, want 1", len(traces))
			}
			if tr := traces[0]; tr.Status != "ok" || tr.HTTPStatus != http.StatusOK || tr.SecurityAction == "block" {
				t.Fatalf("trace status=%q http=%d action=%q, want ok/200/not block", tr.Status, tr.HTTPStatus, tr.SecurityAction)
			}
		})
	}
}

// Without the hook the handler records at once, as before.
func TestNoResponseOutcomeRecordsImmediately(t *testing.T) {
	route := outcomeRoutes()[0]
	rec := observability.NewRecorderWithOptions(nil, observability.RecorderOptions{BufferSize: 64})
	rr := httptest.NewRecorder()
	outcomeHandler(t, route, rec).ServeHTTP(rr, outcomeRequest(route.body))
	if traces := rec.PendingTraces(); len(traces) != 1 || traces[0].Status != "ok" {
		t.Fatalf("traces %+v, want one ok trace", traces)
	}
}

// Finish is idempotent, and a trace recorded after Finish (a handler that
// outlived its wrapper) is written at once with the reported outcome.
func TestResponseOutcomeFinishSemantics(t *testing.T) {
	route := outcomeRoutes()[0]
	rec := observability.NewRecorderWithOptions(nil, observability.RecorderOptions{BufferSize: 64})
	h := outcomeHandler(t, route, rec)
	ctx, outcome := WithResponseOutcome(context.Background())
	outcome.Block("synthetic-output-engine", http.StatusUnavailableForLegalReasons)
	outcome.Finish()
	outcome.Finish()
	r := outcomeRequest(route.body)
	h.ServeHTTP(httptest.NewRecorder(), r.WithContext(mergeAuth(ctx, r)))
	traces := rec.PendingTraces()
	if len(traces) != 1 {
		t.Fatalf("got %d traces, want 1", len(traces))
	}
	if traces[0].Status != "blocked" || traces[0].HTTPStatus != http.StatusUnavailableForLegalReasons {
		t.Fatalf("late trace status=%q http=%d, want blocked/451", traces[0].Status, traces[0].HTTPStatus)
	}
}

func mergeAuth(ctx context.Context, r *http.Request) context.Context {
	info, _ := auth.FromContext(r.Context())
	return auth.WithInfo(ctx, info)
}
