package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
)

func TestDefaultObservedScanSlots(t *testing.T) {
	if n := defaultObservedScanSlots(); n < 1 {
		t.Fatalf("slots = %d, want >= 1", n)
	}
	h := NewHandler(providers.NewRegistry(), nil, nil, nil, nil, "fail-open")
	if cap(h.observedScanSlots) != defaultObservedScanSlots() {
		t.Fatalf("handler bound = %d", cap(h.observedScanSlots))
	}
}

// With every after-stream scan slot busy, a pass-through stream still
// reaches the client unchanged and its trace is written at once, tagged
// response_scan_skipped=busy, with no response finding and the skip
// counted. Once a slot frees up, the next stream is scanned again.
func TestPassThroughStreamScanSkippedWhenBusy(t *testing.T) {
	stream := sseChunk("Here is the badge: ") + sseChunk(holdbackImageExfil) + "data: [DONE]\n\n"
	for _, route := range streamRoutes() {
		t.Run(route.name, func(t *testing.T) {
			srv := streamUpstream(t, stream)
			reg := providers.NewRegistry()
			reg.Register(route.register(srv.URL))
			rec := bufferingRecorder()
			h := responseFindingHandler(reg, exfilProfile(security.ActionLogOnly), rec)
			h.SetObservedScanLimit(1)
			var skipped atomic.Int32
			h.SetObservedScanSkipped(func() { skipped.Add(1) })

			h.observedScanSlots <- struct{}{} // occupy the only slot
			rr := httptest.NewRecorder()
			streamChat(h, rr, route.model)
			if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "QWxsIG9m") {
				t.Fatalf("stream changed while busy: %d %s", rr.Code, rr.Body.String())
			}
			// Written synchronously: present as soon as the handler returns.
			traces := rec.PendingTraces()
			if len(traces) != 1 {
				t.Fatalf("got %d traces right after the handler, want 1", len(traces))
			}
			if traces[0].Tags["response_scan_skipped"] != "busy" {
				t.Fatalf("trace not tagged as skipped: %v", traces[0].Tags)
			}
			if len(exfilRows(rec.PendingThreats())) != 0 {
				t.Fatalf("skipped scan still produced response findings")
			}
			if skipped.Load() != 1 {
				t.Fatalf("skip counted %d times, want 1", skipped.Load())
			}

			<-h.observedScanSlots // free the slot
			rr = httptest.NewRecorder()
			streamChat(h, rr, route.model)
			traces, threats := collectRecorded(t, rec, hasExfilRow)
			if len(exfilRows(threats)) != 1 || len(traces) != 1 {
				t.Fatalf("scan did not resume: traces=%d rows=%d", len(traces), len(exfilRows(threats)))
			}
			if _, tagged := traces[0].Tags["response_scan_skipped"]; tagged {
				t.Fatalf("scanned trace tagged as skipped")
			}
			deadline := time.Now().Add(2 * time.Second)
			for len(h.observedScanSlots) != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if len(h.observedScanSlots) != 0 {
				t.Fatalf("scan slot not released")
			}
		})
	}
}
