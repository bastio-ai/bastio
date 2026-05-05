package observability

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/pkg/tenant"
)

func TestParseTraceID(t *testing.T) {
	id, err := parseTraceID("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("parseTraceID err: %v", err)
	}
	want := uuid.UUID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	if id != want {
		t.Fatalf("parseTraceID: want %v got %v", want, id)
	}

	if _, err := parseTraceID("short"); err == nil {
		t.Fatal("expected error on short trace id")
	}
}

func TestUnixNanoToTime(t *testing.T) {
	got := unixNanoToTime("1600000000000000000")
	if got.IsZero() {
		t.Fatal("expected non-zero time")
	}
	if unixNanoToTime("").IsZero() == false {
		t.Fatal("expected zero time for empty string")
	}
	if unixNanoToTime("not-a-number").IsZero() == false {
		t.Fatal("expected zero time for invalid string")
	}
}

func TestAttrMap_AllTypes(t *testing.T) {
	s := "hello"
	i := "42"
	b := true
	d := 3.14
	attrs := []otlpAttribute{
		{Key: "str", Value: otlpAnyValue{StringValue: &s}},
		{Key: "int", Value: otlpAnyValue{IntValue: &i}},
		{Key: "bool", Value: otlpAnyValue{BoolValue: &b}},
		{Key: "dbl", Value: otlpAnyValue{DoubleValue: &d}},
	}
	m := attrMap(attrs)
	if m["str"] != "hello" || m["int"] != int64(42) || m["bool"] != true || m["dbl"] != 3.14 {
		t.Fatalf("attrMap: got %+v", m)
	}
}

// TestOTLPHandler_Ingests submits one span via HTTP and confirms the
// recorder receives a TraceRecord with the right fields.
func TestOTLPHandler_Ingests(t *testing.T) {
	r := &Recorder{
		traceCh:     make(chan *TraceRecord, 4),
		analyticsCh: make(chan *TraceRecord, 4),
		threatCh:    make(chan *ThreatEvent, 4),
	}
	h := NewOTLPHandler(r)

	startNs := int64(1_700_000_000_000_000_000)
	endNs := startNs + int64(42*time.Millisecond)

	provStr := "openai"
	modelStr := "gpt-4o"
	inTok := "120"
	outTok := "80"

	body := otlpExportRequest{
		ResourceSpans: []otlpResourceSpans{{
			Resource: otlpResource{Attributes: []otlpAttribute{
				{Key: "service.name", Value: otlpAnyValue{StringValue: strPtr("my-app")}},
			}},
			ScopeSpans: []otlpScopeSpan{{Spans: []otlpSpan{{
				TraceID:           "0102030405060708090a0b0c0d0e0f10",
				SpanID:            "aabbccddeeff0011",
				Name:              "chat.complete",
				StartTimeUnixNano: strconvInt(startNs),
				EndTimeUnixNano:   strconvInt(endNs),
				Attributes: []otlpAttribute{
					{Key: "gen_ai.system", Value: otlpAnyValue{StringValue: &provStr}},
					{Key: "gen_ai.request.model", Value: otlpAnyValue{StringValue: &modelStr}},
					{Key: "gen_ai.usage.input_tokens", Value: otlpAnyValue{IntValue: &inTok}},
					{Key: "gen_ai.usage.output_tokens", Value: otlpAnyValue{IntValue: &outTok}},
				},
				Status: otlpStatus{Code: 0},
			}}}},
		}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	// Inject tenant so the handler's customerID is deterministic.
	req = req.WithContext(tenant.WithID(req.Context(), tenant.DefaultOSSID))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200 got %d body=%s", rr.Code, rr.Body.String())
	}

	select {
	case rec := <-r.traceCh:
		if rec.Provider != "openai" || rec.Model != "gpt-4o" {
			t.Fatalf("provider/model: got %q/%q", rec.Provider, rec.Model)
		}
		if rec.InputTokens != 120 || rec.OutputTokens != 80 || rec.TotalTokens != 200 {
			t.Fatalf("tokens: got in=%d out=%d total=%d", rec.InputTokens, rec.OutputTokens, rec.TotalTokens)
		}
		if rec.DurationMs != 42 {
			t.Fatalf("duration_ms: want 42 got %d", rec.DurationMs)
		}
		if rec.Path != "chat.complete" {
			t.Fatalf("path: want chat.complete got %q", rec.Path)
		}
		if rec.Method != "my-app" {
			t.Fatalf("method(service.name): want my-app got %q", rec.Method)
		}
		if rec.CustomerID != tenant.DefaultOSSID {
			t.Fatalf("customer_id mismatch: %v", rec.CustomerID)
		}
	case <-time.After(time.Second):
		t.Fatal("no trace recorded within 1s")
	}
}

func TestOTLPHandler_RejectsNonJSON(t *testing.T) {
	h := NewOTLPHandler(&Recorder{})
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader([]byte(`<xml/>`)))
	req.Header.Set("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("want 415, got %d", rr.Code)
	}
}

func TestOTLPHandler_RejectsWrongMethod(t *testing.T) {
	h := NewOTLPHandler(&Recorder{})
	req := httptest.NewRequest(http.MethodGet, "/v1/traces", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rr.Code)
	}
}

func strPtr(s string) *string { return &s }

func strconvInt(n int64) string {
	return fmtInt(n)
}

func fmtInt(n int64) string {
	// Local helper to avoid importing strconv from the test file.
	// Large numbers fit in int64 so this is sufficient for fixture nanos.
	neg := n < 0
	if neg {
		n = -n
	}
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
