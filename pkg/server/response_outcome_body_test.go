package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
)

// replacingWrapper stands in for an outer middleware that masks the
// answer: it delivers a rewritten body and reports it.
func replacingWrapper(next http.Handler, delivered string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, outcome := WithResponseOutcome(r.Context())
		defer outcome.Finish()
		captured := httptest.NewRecorder()
		next.ServeHTTP(captured, r.WithContext(ctx))
		outcome.ReplaceBody([]byte(delivered))
		w.WriteHeader(captured.Code)
		_, _ = w.Write([]byte(delivered))
	})
}

func TestResponseOutcomeReplaceBodyRecordsDeliveredBody(t *testing.T) {
	const delivered = `{"choices":[{"message":{"role":"assistant","content":"A [MASKED] answer."}}]}`
	for _, route := range outcomeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			rec := observability.NewRecorderWithOptions(nil, observability.RecorderOptions{BufferSize: 64})
			rr := httptest.NewRecorder()
			replacingWrapper(outcomeHandler(t, route, rec), delivered).ServeHTTP(rr, outcomeRequest(route.body))
			traces := rec.PendingTraces()
			if len(traces) != 1 {
				t.Fatalf("got %d traces, want 1", len(traces))
			}
			if traces[0].ResponseBody != delivered {
				t.Fatalf("trace body %q, want the delivered body", traces[0].ResponseBody)
			}
			if traces[0].Status != "ok" {
				t.Fatalf("status %q, want ok (replaced, not blocked)", traces[0].Status)
			}
		})
	}
}

// OmitContent still wins: a replaced body is not stored either.
func TestResponseOutcomeReplaceBodyRespectsOmitContent(t *testing.T) {
	route := outcomeRoutes()[0]
	rec := observability.NewRecorderWithOptions(nil, observability.RecorderOptions{BufferSize: 64})
	r := outcomeRequest(route.body)
	r = r.WithContext(gatewaypolicy.With(r.Context(), gatewaypolicy.Policy{OmitContent: true}))
	replacingWrapper(outcomeHandler(t, route, rec), "A [MASKED] answer.").ServeHTTP(httptest.NewRecorder(), r)
	traces := rec.PendingTraces()
	if len(traces) != 1 {
		t.Fatalf("got %d traces, want 1", len(traces))
	}
	if strings.Contains(traces[0].ResponseBody, "MASKED") {
		t.Fatalf("omit-content trace stored the replaced body: %q", traces[0].ResponseBody)
	}
}
