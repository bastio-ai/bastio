package observability

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/pkg/tenant"
)

// OTLPHandler accepts OpenTelemetry trace payloads over HTTP/JSON and
// pushes each span onto the observability Recorder's trace buffer. This
// lets any OTEL-instrumented application stream traces to Bastio without
// using a Bastio SDK. Only the JSON-over-HTTP transport is implemented;
// gRPC OTLP can be added later if demand appears.
//
// Protocol reference:
//   https://opentelemetry.io/docs/specs/otlp/#otlphttp
//   Payload: ExportTraceServiceRequest (application/json)
type OTLPHandler struct {
	recorder *Recorder
}

// NewOTLPHandler returns a handler bound to the given recorder.
func NewOTLPHandler(recorder *Recorder) *OTLPHandler {
	return &OTLPHandler{recorder: recorder}
}

// ServeHTTP handles POST /v1/traces. The request body must be an OTLP
// ExportTraceServiceRequest JSON document. The response is always 200
// with an empty ExportTraceServiceResponse body; errors during conversion
// are logged but do not fail the request, mirroring the OTLP "partial
// success" semantics.
func (h *OTLPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && ct != "application/json" {
		http.Error(w, `{"error":"only application/json is supported"}`, http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}

	var req otlpExportRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":"invalid OTLP payload"}`, http.StatusBadRequest)
		return
	}

	// Tenant comes from context (OSS middleware injects the default;
	// cloud injects the session tenant). No auth on the endpoint itself
	// in OSS — the middleware chain decides.
	customerID := tenantFromCtx(r)

	// Group spans by OTel trace id so we emit ONE TraceRecord per trace
	// (from the root span) and one ObservationRecord per span. Previously
	// every span was written as its own top-level trace, which looked
	// wrong in the UI and inflated ClickHouse row counts.
	type batch struct {
		spans         []otlpSpan
		resourceAttrs map[string]any
	}
	groups := map[uuid.UUID]*batch{}
	for _, rs := range req.ResourceSpans {
		resourceAttrs := attrMap(rs.Resource.Attributes)
		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				traceID, err := parseTraceID(span.TraceID)
				if err != nil {
					continue
				}
				g, ok := groups[traceID]
				if !ok {
					g = &batch{resourceAttrs: resourceAttrs}
					groups[traceID] = g
				}
				g.spans = append(g.spans, span)
			}
		}
	}

	ingested := 0
	for traceID, g := range groups {
		root := selectRootSpan(g.spans)
		if root != nil {
			if rec := toTraceRecord(*root, g.resourceAttrs, customerID); rec != nil {
				rec.ID = traceID
				h.recorder.RecordTrace(rec)
			}
		}
		for _, span := range g.spans {
			if obs := toObservationRecord(span, traceID, customerID); obs != nil {
				h.recorder.RecordObservation(obs)
				ingested++
			}
		}
	}

	slog.Debug("otlp traces ingested", "spans", ingested, "traces", len(groups))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

func tenantFromCtx(r *http.Request) uuid.UUID {
	if id, err := tenant.FromContext(r.Context()); err == nil {
		return id
	}
	return tenant.DefaultOSSID
}

// toTraceRecord converts a single OTLP span into a TraceRecord. Returns
// nil if required fields are missing.
func toTraceRecord(s otlpSpan, resourceAttrs map[string]any, customerID uuid.UUID) *TraceRecord {
	traceID, err := parseTraceID(s.TraceID)
	if err != nil {
		return nil
	}

	start := unixNanoToTime(s.StartTimeUnixNano)
	end := unixNanoToTime(s.EndTimeUnixNano)
	if end.IsZero() {
		end = time.Now().UTC()
	}
	durationMs := uint32(0)
	if end.After(start) {
		durationMs = uint32(end.Sub(start).Milliseconds())
	}

	spanAttrs := attrMap(s.Attributes)

	provider, _ := stringAttr(spanAttrs, "gen_ai.system", "llm.vendor", "ai.provider")
	model, _ := stringAttr(spanAttrs, "gen_ai.request.model", "llm.request.model", "ai.model")
	inTok, _ := intAttr(spanAttrs, "gen_ai.usage.input_tokens", "llm.usage.prompt_tokens")
	outTok, _ := intAttr(spanAttrs, "gen_ai.usage.output_tokens", "llm.usage.completion_tokens")
	endUser, _ := stringAttr(spanAttrs, "enduser.id", "end_user.id")

	serviceName, _ := stringAttr(resourceAttrs, "service.name")

	status := "ok"
	if s.Status.Code == statusCodeError {
		status = "error"
	}

	return &TraceRecord{
		ID:           traceID,
		CustomerID:   customerID,
		Method:       serviceName, // use service.name as a routing hint
		Path:         s.Name,
		Provider:     provider,
		Model:        model,
		StartedAt:    start,
		CompletedAt:  end,
		DurationMs:   durationMs,
		InputTokens:  uint32(inTok),
		OutputTokens: uint32(outTok),
		TotalTokens:  uint32(inTok + outTok),
		Status:       status,
		ErrorMessage: s.Status.Message,
		EndUserID:    endUser,
	}
}

// selectRootSpan returns the batch's root span: the one whose parent is
// either empty or refers to a span outside this batch. If multiple
// candidates exist (rare — multi-root is allowed by OTel but unusual for
// a single trace), the earliest-started one wins.
func selectRootSpan(spans []otlpSpan) *otlpSpan {
	inBatch := make(map[string]bool, len(spans))
	for _, s := range spans {
		inBatch[s.SpanID] = true
	}
	var root *otlpSpan
	for i := range spans {
		s := &spans[i]
		if s.ParentSpanID != "" && inBatch[s.ParentSpanID] {
			continue
		}
		if root == nil {
			root = s
			continue
		}
		if unixNanoToTime(s.StartTimeUnixNano).Before(unixNanoToTime(root.StartTimeUnixNano)) {
			root = s
		}
	}
	return root
}

// toObservationRecord converts a single OTel span into one of our span
// rows. Maps standard GenAI attributes (gen_ai.*) plus Langfuse's own
// langfuse.* conventions onto our enriched columns — that way both
// OpenLLMetry/OpenInference and Langfuse-instrumented apps light up the
// span detail view without extra SDK work.
func toObservationRecord(s otlpSpan, traceID, customerID uuid.UUID) *ObservationRecord {
	spanID, err := spanIDToUUID(s.SpanID)
	if err != nil {
		return nil
	}
	var parent *uuid.UUID
	if s.ParentSpanID != "" {
		if p, err := spanIDToUUID(s.ParentSpanID); err == nil {
			parent = &p
		}
	}
	start := unixNanoToTime(s.StartTimeUnixNano)
	end := unixNanoToTime(s.EndTimeUnixNano)
	if end.IsZero() {
		end = time.Now().UTC()
	}
	dur := uint32(0)
	if end.After(start) {
		dur = uint32(end.Sub(start).Milliseconds())
	}
	attrs := attrMap(s.Attributes)

	spanType := classifySpanType(s.Name, attrs)

	status := "ok"
	if s.Status.Code == statusCodeError {
		status = "error"
	}

	input, _ := stringAttr(attrs,
		"gen_ai.prompt", "llm.input_messages", "input.value",
		"langfuse.observation.input", "ai.prompt",
	)
	output, _ := stringAttr(attrs,
		"gen_ai.completion", "llm.output_messages", "output.value",
		"langfuse.observation.output", "ai.completion",
	)
	model, _ := stringAttr(attrs, "gen_ai.response.model", "gen_ai.request.model", "llm.request.model", "ai.model")
	inTok, _ := intAttr(attrs, "gen_ai.usage.input_tokens", "llm.usage.prompt_tokens")
	outTok, _ := intAttr(attrs, "gen_ai.usage.output_tokens", "llm.usage.completion_tokens")
	toolName, _ := stringAttr(attrs, "tool.name", "gen_ai.tool.name", "langfuse.observation.tool.name")
	toolInput, _ := stringAttr(attrs, "tool.input", "gen_ai.tool.input", "langfuse.observation.tool.input")
	toolOutput, _ := stringAttr(attrs, "tool.output", "gen_ai.tool.output", "langfuse.observation.tool.output")
	promptName, _ := stringAttr(attrs, "langfuse.prompt.name", "gen_ai.prompt.name")
	promptVersion, _ := intAttr(attrs, "langfuse.prompt.version", "gen_ai.prompt.version")
	env, _ := stringAttr(attrs, "langfuse.environment", "deployment.environment")

	return &ObservationRecord{
		ID:            spanID,
		TraceID:       traceID,
		ParentID:      parent,
		CustomerID:    customerID,
		Type:          spanType,
		Name:          s.Name,
		StartedAt:     start,
		CompletedAt:   end,
		DurationMs:    dur,
		InputTokens:   uint32(inTok),
		OutputTokens:  uint32(outTok),
		Model:         model,
		Input:         input,
		Output:        output,
		Status:        status,
		ErrorMessage:  s.Status.Message,
		StatusMessage: s.Status.Message,
		ToolName:      toolName,
		ToolInput:     toolInput,
		ToolOutput:    toolOutput,
		PromptName:    promptName,
		PromptVersion: uint32(promptVersion),
		Environment:   env,
	}
}

// classifySpanType picks one of our observation types from span metadata.
// Prefers explicit attributes (gen_ai.operation.name, langfuse.observation.type);
// falls back to name-based heuristics for less structured sources.
func classifySpanType(name string, attrs map[string]any) string {
	if v, ok := stringAttr(attrs, "langfuse.observation.type"); ok {
		return v
	}
	if v, ok := stringAttr(attrs, "gen_ai.operation.name"); ok {
		switch v {
		case "chat", "text_completion", "completion":
			return "generation"
		case "embedding", "embeddings":
			return "embedding"
		case "tool_use", "execute_tool":
			return "tool"
		}
	}
	lower := name
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c >= 'A' && c <= 'Z' {
			lower = lower[:i] + string(c+32) + lower[i+1:]
		}
	}
	switch {
	case contains(lower, "chat", "completion", "generate", "llm"):
		return "generation"
	case contains(lower, "embed"):
		return "embedding"
	case contains(lower, "retriev", "search", "query"):
		return "retrieval"
	case contains(lower, "tool"):
		return "tool"
	case contains(lower, "guard", "scan"):
		return "guardrail"
	case contains(lower, "agent"):
		return "agent"
	default:
		return "span"
	}
}

func contains(s string, needles ...string) bool {
	for _, n := range needles {
		for i := 0; i+len(n) <= len(s); i++ {
			if s[i:i+len(n)] == n {
				return true
			}
		}
	}
	return false
}

// spanIDToUUID pads an 8-byte OTel span id into a UUID by placing it in
// the low 8 bytes — stable, lossless, unique per trace.
func spanIDToUUID(s string) (uuid.UUID, error) {
	if len(s) != 16 {
		return uuid.Nil, errInvalidSpanID
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	copy(id[8:], raw)
	return id, nil
}

var errInvalidSpanID = &otlpError{"invalid span id"}

// OTLP wire types — just enough of ExportTraceServiceRequest to extract
// the fields we care about. Unknown fields are ignored.

type otlpExportRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type otlpScopeSpan struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []otlpAttribute `json:"attributes"`
	Status            otlpStatus      `json:"status"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const statusCodeError = 2 // STATUS_CODE_ERROR per OTLP proto

type otlpAttribute struct {
	Key   string        `json:"key"`
	Value otlpAnyValue  `json:"value"`
}

type otlpAnyValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
	IntValue    *string  `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
}

func attrMap(attrs []otlpAttribute) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, a := range attrs {
		switch {
		case a.Value.StringValue != nil:
			out[a.Key] = *a.Value.StringValue
		case a.Value.IntValue != nil:
			if v, err := strconv.ParseInt(*a.Value.IntValue, 10, 64); err == nil {
				out[a.Key] = v
			}
		case a.Value.DoubleValue != nil:
			out[a.Key] = *a.Value.DoubleValue
		case a.Value.BoolValue != nil:
			out[a.Key] = *a.Value.BoolValue
		}
	}
	return out
}

func stringAttr(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

func intAttr(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		switch v := m[k].(type) {
		case int64:
			return v, true
		case float64:
			return int64(v), true
		}
	}
	return 0, false
}

// parseTraceID accepts a 32-char hex OTLP trace id and returns a UUID.
// OTLP trace ids are 128 bits so the mapping is lossless.
func parseTraceID(s string) (uuid.UUID, error) {
	if len(s) != 32 {
		return uuid.Nil, errInvalidTraceID
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	copy(id[:], raw)
	return id, nil
}

var errInvalidTraceID = &otlpError{"invalid trace id"}

type otlpError struct{ msg string }

func (e *otlpError) Error() string { return e.msg }

func unixNanoToTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}
