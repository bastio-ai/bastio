package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/google/uuid"
)

type capturedMCPTelemetry struct {
	traces  []*observability.TraceRecord
	spans   []*observability.ObservationRecord
	threats []*observability.ThreatEvent
}

func (c *capturedMCPTelemetry) RecordTrace(r *observability.TraceRecord) {
	c.traces = append(c.traces, r)
}
func (c *capturedMCPTelemetry) RecordObservation(r *observability.ObservationRecord) {
	c.spans = append(c.spans, r)
}
func (c *capturedMCPTelemetry) RecordThreatEvent(r *observability.ThreatEvent) {
	c.threats = append(c.threats, r)
}

func TestMCPTraceInspectsWithoutRetainingToolContent(t *testing.T) {
	for _, mode := range []string{"mask", "tokenize", "block_arguments", "block_result", "warn"} {
		t.Run(mode, func(t *testing.T) {
			cid, session := uuid.New(), uuid.New()
			recorder := &capturedMCPTelemetry{}
			trace := newMCPToolTrace(recorder, cid, "employee", uuid.New(), "search_products", "assistant", session)
			ctx := context.WithValue(context.Background(), mcpTraceKey{}, trace)
			profile := security.DefaultProfile()
			profile.PIIAction = security.Action(mode)
			if strings.HasPrefix(mode, "block") {
				profile.PIIAction = security.ActionBlock
			}
			engine := security.NewEngine(detection.NewPIIDetector())
			profiles := mcpProfileSnapshot{cid, &profile}
			arguments := json.RawMessage(`{"query":"private.person@example.test"}`)
			if mode == "block_result" {
				arguments = json.RawMessage(`{"query":"Lampe til natbordet"}`)
			}
			trace.startStage("guardrail", "mcp.arguments")
			processed, cats, err := inspectMCPPayload(ctx, engine, profiles, cid, arguments, security.RoleUser, 0)
			if mode == "block_arguments" {
				if err == nil {
					t.Fatal("argument policy did not block")
				}
				trace.inspectionFailure(err, cats, "arguments", 403)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if (mode == "mask" || mode == "tokenize") && strings.Contains(string(processed), "private.person@example.test") {
					t.Fatal("argument was not protected")
				}
				trace.inspected(cats)
				trace.startStage("tool", "mcp.search_products")
				trace.trace.Tags["remote_action"] = "completed"
				trace.startStage("guardrail", "mcp.result")
				_, cats, err = inspectMCPPayload(ctx, engine, profiles, cid, json.RawMessage(`{"content":[{"type":"text","text":"private.person@example.test"}]}`), security.RoleRetrieval, 0)
				if mode == "block_result" {
					if err == nil {
						t.Fatal("result policy did not block")
					}
					trace.inspectionFailure(err, cats, "result", 200)
				} else {
					if err != nil {
						t.Fatal(err)
					}
					trace.inspected(cats)
					trace.outcome("ok", "completed", 200, "")
				}
			}
			trace.complete()
			trace.complete() // Explicit finish plus deferred cleanup must not duplicate records.
			if len(recorder.traces) != 1 {
				t.Fatalf("trace count %d", len(recorder.traces))
			}
			got := recorder.traces[0]
			if got.CustomerID != cid || got.SessionID != session.String() || got.Provider != "mcp" || got.Environment != "workspace" || got.TraceName != "workspace.mcp.search_products" {
				t.Fatal("trace attribution incorrect")
			}
			if got.TotalTokens != 0 || got.CostCents != 0 || got.Model != "" {
				t.Fatal("tool billed as a model generation")
			}
			for _, event := range recorder.threats {
				expected := mode
				if strings.HasPrefix(mode, "block") {
					expected = "block"
				}
				if event.Action != expected {
					t.Fatalf("effective detector action=%s expected=%s", event.Action, expected)
				}
			}
			if !got.ThreatDetected || len(recorder.threats) == 0 {
				t.Fatal("security findings missing")
			}
			for _, span := range recorder.spans {
				if span.TraceID != got.ID || span.CustomerID != cid || span.CompletedAt.Before(span.StartedAt) {
					t.Fatal("invalid trace span")
				}
			}
			if strings.HasPrefix(mode, "block") && got.Status != "blocked" {
				t.Fatal("block outcome missing")
			}
			if mode == "block_arguments" && got.Tags["remote_action"] != "not_run" {
				t.Fatal("arguments block claims remote execution")
			}
			if mode == "block_result" && (got.Tags["remote_action"] != "completed" || got.Tags["security.stage"] != "result") {
				t.Fatal("result block lost remote completion")
			}
			raw, _ := json.Marshal(recorder.traces)
			spans, _ := json.Marshal(recorder.spans)
			threats, _ := json.Marshal(recorder.threats)
			for _, body := range [][]byte{raw, spans, threats} {
				if strings.Contains(string(body), "private.person") || strings.Contains(string(body), "Lampe til natbordet") {
					t.Fatal("tool content retained in telemetry")
				}
			}
		})
	}
}

func TestMCPTraceErrorsAndBoundedFindings(t *testing.T) {
	cid := uuid.New()
	recorder := &capturedMCPTelemetry{}
	trace := newMCPToolTrace(recorder, cid, "employee", uuid.New(), "search", "manual", uuid.Nil)
	trace.startStage("guardrail", "mcp.arguments")
	secret := "confidential-upload-and-credential"
	finding := security.Finding{ThreatType: security.ThreatPII, SubCategory: "email", DetectorName: "pii", Action: security.ActionMask, Score: .9, MatchedContent: secret, MatchedPattern: secret, Message: secret, Source: secret}
	result := &security.ScanResult{Action: security.ActionMask, ThreatScore: .9, Findings: []security.Finding{finding}, SanitizedContent: secret}
	trace.captureScan(uuid.New(), result)
	if len(trace.findings) != 0 {
		t.Fatal("foreign tenant's finding recorded")
	}
	for range 500 {
		trace.captureScan(cid, result)
	}
	if len(trace.findings) != 1 {
		t.Fatal("duplicate findings were not bounded")
	}
	trace.startStage("tool", "mcp.search")
	trace.trace.Tags["remote_action"] = "unknown"
	trace.outcome("error", "remote_error", 502, "MCP request failed; the remote action may have completed.")
	trace.complete()
	if len(recorder.traces) != 1 || recorder.traces[0].Status != "error" || recorder.traces[0].Tags["remote_action"] != "unknown" || recorder.traces[0].SessionID != "" {
		t.Fatal("remote error outcome incorrect")
	}
	if result.Findings[0].MatchedContent != secret {
		t.Fatal("active scan result mutated")
	}
	data, _ := json.Marshal(recorder.threats)
	if strings.Contains(string(data), secret) {
		t.Fatal("matched data leaked")
	}
	if recorder.traces[0].SecurityAction != "mask" {
		t.Fatal("later clean stage erased sanitization")
	}

	// Infrastructure details must not leak through error messages or tags.
	trace = newMCPToolTrace(recorder, cid, "employee", uuid.New(), "search", "manual", uuid.Nil)
	trace.startStage("guardrail", "mcp.result")
	trace.inspectionFailure(errors.New("upstream credential: "+secret), nil, "result", 403)
	trace.complete()
	data, _ = json.Marshal(recorder.traces)
	if strings.Contains(string(data), secret) {
		t.Fatal("scanner error leaked")
	}
	if trace.trace.Tags["security.code"] != "inspection_unavailable" {
		t.Fatal("safe failure taxonomy missing")
	}
}

func TestMCPTraceCompletionExcludesAssistantContinuation(t *testing.T) {
	recorder := &capturedMCPTelemetry{}
	trace := newMCPToolTrace(recorder, uuid.New(), "employee", uuid.New(), "search", "assistant", uuid.New())
	trace.startStage("tool", "mcp.search")
	trace.outcome("ok", "completed", 200, "")
	trace.complete()
	completed := trace.trace.CompletedAt
	// Mimic deferred cleanup after a later generation has finished.
	trace.complete()
	if !trace.trace.CompletedAt.Equal(completed) || completed.Before(trace.trace.StartedAt) || completed.After(time.Now()) || len(recorder.traces) != 1 {
		t.Fatal("tool timing or cardinality changed after completion")
	}
}
