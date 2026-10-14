package workspace

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/google/uuid"
)

type mcpTraceKey struct{}

type mcpTraceRecorder interface {
	RecordTrace(*observability.TraceRecord)
	RecordObservation(*observability.ObservationRecord)
	RecordThreatEvent(*observability.ThreatEvent)
}

// MCP telemetry is operational metadata only, even when conversation storage
// is enabled. Never pass arguments, results, endpoints or remote errors here.
type mcpToolTrace struct {
	recorder mcpTraceRecorder
	trace    observability.TraceRecord
	spans    []*observability.ObservationRecord
	stage    *observability.ObservationRecord
	findings map[string]*observability.ThreatEvent
	done     bool
}

func newMCPToolTrace(recorder mcpTraceRecorder, cid uuid.UUID, uid string, connectionID uuid.UUID, tool, source string, conversationID uuid.UUID) *mcpToolTrace {
	t := &mcpToolTrace{recorder: recorder, findings: map[string]*observability.ThreatEvent{}}
	t.trace = observability.TraceRecord{
		ID: uuid.New(), CustomerID: cid, Method: "POST", Provider: "mcp",
		Path:      "/v1/workspace/mcp/connections/" + connectionID.String() + "/call",
		StartedAt: time.Now().UTC(), Status: "error", HTTPStatus: 500,
		ErrorMessage: "Tool request did not complete.", EndUserID: uid,
		Environment: "workspace", TraceName: "workspace.mcp." + tool,
		SecurityAction: "pass", ThreatTypes: []string{},
		Tags: map[string]string{"mcp.connection_id": connectionID.String(), "mcp.tool": tool, "mcp.source": source, "mcp.outcome": "incomplete", "remote_action": "not_run"},
	}
	if conversationID != uuid.Nil {
		t.trace.SessionID = conversationID.String()
	}
	return t
}

func (h *Handler) beginMCPTrace(w http.ResponseWriter, r *http.Request, c MCPConnection, tool, source string, conversationID uuid.UUID) (*http.Request, *mcpToolTrace) {
	var recorder mcpTraceRecorder
	if h.obsRecorder != nil {
		recorder = h.obsRecorder
	}
	t := newMCPToolTrace(recorder, customerIDFromCtx(r.Context()), userIDFromCtx(r.Context()), c.ID, tool, source, conversationID)
	if recorder != nil {
		w.Header().Set("X-Bastio-Trace-Id", t.trace.ID.String())
	}
	return r.WithContext(context.WithValue(r.Context(), mcpTraceKey{}, t)), t
}

func mcpTraceFromContext(ctx context.Context) *mcpToolTrace {
	t, _ := ctx.Value(mcpTraceKey{}).(*mcpToolTrace)
	return t
}

func mcpAuditTraceID(r *http.Request) string {
	if t := mcpTraceFromContext(r.Context()); t != nil && t.recorder != nil {
		return t.trace.ID.String()
	}
	return ""
}

func (t *mcpToolTrace) startStage(kind, name string) {
	t.endStage("ok", "")
	t.stage = observability.NewSpan(t.trace.ID, t.trace.CustomerID, kind, name).WithEnv("workspace").Start().Record()
	if kind == "tool" {
		t.stage.ToolName = t.trace.Tags["mcp.tool"]
	}
	t.spans = append(t.spans, t.stage)
}

func (t *mcpToolTrace) endStage(status, message string) {
	if t.stage == nil {
		return
	}
	t.stage.CompletedAt = time.Now().UTC()
	t.stage.DurationMs = uint32(t.stage.CompletedAt.Sub(t.stage.StartedAt).Milliseconds())
	t.stage.Status, t.stage.ErrorMessage = status, message
	if message != "" {
		t.stage.StatusMessage = message
	}
	t.stage = nil
}

func (t *mcpToolTrace) outcome(status, outcome string, httpStatus uint16, message string) {
	t.trace.Status, t.trace.HTTPStatus, t.trace.ErrorMessage = status, httpStatus, message
	t.trace.Tags["mcp.outcome"] = outcome
	if status == "blocked" {
		t.trace.SecurityAction = "block"
	}
	t.endStage(status, message)
}

func (t *mcpToolTrace) inspectionFailure(err error, categories []string, stage string, httpStatus uint16) {
	failure := mcpSecurityFailure(err, categories, stage)
	t.trace.Tags["security.stage"] = stage
	for _, key := range []string{"code", "rules", "blocking_detectors"} {
		switch value := failure[key].(type) {
		case string:
			t.trace.Tags["security."+key] = value
		case []string:
			t.trace.Tags["security."+key] = strings.Join(value, ",")
		}
	}
	t.outcome("blocked", stage+"_withheld", httpStatus, failure["reason"].(string))
}

// Capture only bounded, built-in taxonomy and numerical detector signals.
// Matched text, custom policy labels/expressions and payload keys are excluded.
func (t *mcpToolTrace) captureScan(cid uuid.UUID, result *security.ScanResult) {
	if t.done || cid != t.trace.CustomerID || result == nil || t.stage == nil || t.stage.Type != "guardrail" {
		return
	}
	for _, f := range result.Findings {
		category := string(f.ThreatType)
		if !safeMCPRule.MatchString(category) {
			continue
		}
		rule := ""
		switch f.DetectorName {
		case "pii", "jailbreak", "injection", "indirect_injection", "secrets", "exfil", "output_exfil":
			candidate := f.SubCategory
			if candidate == "" {
				candidate = f.MatchedPattern
			}
			if safeMCPRule.MatchString(candidate) {
				rule = candidate
			}
		}
		detector := category
		switch f.DetectorName {
		case "pii", "jailbreak", "injection", "indirect_injection", "secrets", "exfil", "output_exfil", "topic", "rate_anomaly":
			detector = f.DetectorName
		}
		action := f.Action
		if effective, ok := result.DetectorActions[f.DetectorName]; ok {
			action = effective
		}
		key := t.stage.Name + ":" + detector + ":" + category + ":" + rule + ":" + string(action)
		if old := t.findings[key]; old != nil {
			if float32(f.Score) > old.Score {
				old.Score = float32(f.Score)
			}
			continue
		}
		if len(t.findings) >= 64 {
			continue
		}
		t.findings[key] = &observability.ThreatEvent{
			ID: uuid.New(), TraceID: t.trace.ID, CustomerID: cid, ThreatType: category,
			ThreatSubtype: rule, Severity: string(f.Severity), Score: float32(f.Score),
			Action: string(action), DetectorName: detector, Confidence: float32(f.Confidence),
			EndUserID: t.trace.EndUserID, Source: t.stage.Name, DetectedAt: time.Now().UTC(),
		}
	}
	if float32(result.ThreatScore) > t.trace.ThreatScore {
		t.trace.ThreatScore = float32(result.ThreatScore)
	}
	// Keep the strongest observed action; later clean fields must not erase it.
	actionRank := map[string]int{"": 0, "pass": 0, "log": 1, "log_only": 1, "warn": 2, "redact": 3, "mask": 3, "tokenize": 3, "block": 4}
	if actionRank[string(result.Action)] > actionRank[t.trace.SecurityAction] {
		t.trace.SecurityAction = string(result.Action)
	}
}

func (t *mcpToolTrace) complete() {
	if t.done {
		return
	}
	t.done = true
	t.endStage(t.trace.Status, t.trace.ErrorMessage)
	t.trace.CompletedAt = time.Now().UTC()
	t.trace.DurationMs = uint32(t.trace.CompletedAt.Sub(t.trace.StartedAt).Milliseconds())
	for _, event := range t.findings {
		t.trace.ThreatTypes = append(t.trace.ThreatTypes, event.ThreatType)
	}
	t.trace.ThreatTypes = uniqueMCPDiagnostics(t.trace.ThreatTypes)
	t.trace.ThreatDetected = len(t.findings) > 0
	if t.recorder == nil {
		return
	}
	for _, span := range t.spans {
		t.recorder.RecordObservation(span)
	}
	for _, event := range t.findings {
		t.recorder.RecordThreatEvent(event)
	}
	t.recorder.RecordTrace(&t.trace) // RecordTrace already enqueues analytics once.
}

// Security spans expose decisions, never inspected text.
func (t *mcpToolTrace) inspected(categories []string) {
	if t.stage == nil {
		return
	}
	t.stage.StatusMessage = "Security checks passed. Tool arguments and results are not stored."
	if len(categories) > 0 {
		t.stage.StatusMessage = "Security findings: " + strings.Join(uniqueMCPDiagnostics(slices.Clone(categories)), ", ") + ". Tool arguments and results are not stored."
	}
}
