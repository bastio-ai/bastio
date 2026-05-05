package workspace

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/llmpipeline"
	"github.com/bastio-ai/bastio/internal/security"
)

// scanUserMessage runs the workspace's pre-flight security scan against
// the user's prompt. Mirrors what the gateway does on every API
// request — same engine, same profile lookup, same step list. Workspace
// doesn't have proxies, so ProxyID stays empty; the scan still resolves
// the customer's default profile and walks every detector configured
// on it.
//
// Returns nil + nil error when the security pipeline isn't wired
// (engine OR profile lookup is nil — typical for OSS-standalone
// without security configured). The caller treats nil as "no
// scan performed; proceed".
//
// Workspace differs from gateway on one knob: SkipSanitization=false.
// The gateway runs its own per-message tokenization upstream of the
// scan; the workspace lets the engine sanitize directly so
// ScanResult.SanitizedContent is what the persisted message body uses.
func (h *Handler) scanUserMessage(ctx context.Context, customerID uuid.UUID, userID, ipHash, userAgent, content string) (*security.ScanResult, *security.Profile, error) {
	if h.secEngine == nil || h.secProfiles == nil {
		return nil, nil, nil
	}
	profile, err := h.secProfiles.GetDefault(ctx, customerID)
	if err != nil {
		// Fail open — a profile-lookup hiccup shouldn't lock chat.
		// Return nil so the caller proceeds without scanning. A
		// production deployment with strict policy can flip this to
		// fail-closed via a server option later.
		return nil, nil, nil
	}
	res := llmpipeline.PreflightScan(ctx, llmpipeline.PreflightOptions{
		Engine:           h.secEngine,
		Profile:          profile,
		Content:          content,
		CustomerID:       customerID,
		EndUserID:        userID,
		IPAddress:        ipHash,
		UserAgent:        userAgent,
		SkipSanitization: false,
		Role:             security.RoleUser,
	})
	// Diagnostic: log every preflight outcome so we can see whether
	// the input scan caught a finding and whether ShouldBlock fired.
	// Useful for tracing why workspace messages get past the gate
	// when the trace later shows a "block" finding.
	if res != nil {
		slog.Info("workspace preflight",
			"customer_id", customerID,
			"content_len", len(content),
			"action", res.Action,
			"should_block", res.ShouldBlock,
			"threat_score", res.ThreatScore,
			"findings", len(res.Findings))
	} else {
		slog.Info("workspace preflight returned nil",
			"customer_id", customerID,
			"content_len", len(content))
	}
	return res, profile, nil
}

// chatTraceInput is the workspace-shape view of the per-turn signals
// the recorder needs. Kept as a thin local wrapper so the call sites
// stay readable; the actual writes happen in
// llmpipeline.RecordChatTrace.
type chatTraceInput struct {
	customerID     uuid.UUID
	conversationID uuid.UUID // becomes SessionID — every message in a convo groups together
	traceID        uuid.UUID
	userID         string
	ipHash         string
	userAgent      string
	provider       string
	model          string
	startedAt      time.Time
	completedAt    time.Time
	inputTokens    int
	outputTokens   int
	costCents      float64
	finishReason   string
	requestBody    string // user prompt — what was sent to the model
	responseBody   string // assistant content — what came back
	scanResult     *security.ScanResult
}

// recordChatTrace writes the workspace-shape trace via the shared
// llmpipeline. Conversation-as-session: every workspace turn in the
// same conversation shares a SessionID so /sessions groups them the
// same way the gateway groups by X-Session-Id.
func (h *Handler) recordChatTrace(in chatTraceInput) {
	if h.obsRecorder == nil {
		return
	}
	llmpipeline.RecordChatTrace(llmpipeline.ChatTraceInput{
		Recorder:     h.obsRecorder,
		TraceID:      in.traceID,
		CustomerID:   in.customerID,
		Method:       "POST",
		Path:         "/v1/workspace/conversations/" + in.conversationID.String() + "/messages",
		Provider:     in.provider,
		Model:        in.model,
		StartedAt:    in.startedAt,
		CompletedAt:  in.completedAt,
		InputTokens:  in.inputTokens,
		OutputTokens: in.outputTokens,
		CostCents:    in.costCents,
		Status:       in.finishReason,
		EndUserID:    in.userID,
		SessionID:    in.conversationID.String(),
		IPHash:       in.ipHash,
		UserAgent:    in.userAgent,
		// Tag origin so /traces UI can distinguish workspace traffic
		// from gateway traffic without re-deriving from Path.
		Environment:  "workspace",
		TraceName:    "workspace.chat",
		RequestBody:  in.requestBody,
		ResponseBody: in.responseBody,
		ScanResult:   in.scanResult,
	})
}

// requestSignals pulls the bits of the HTTP request the security
// scanner + threat catalog want. Centralized so non-streaming and
// streaming paths derive them the same way.
func requestSignals(r *http.Request) (ipHash, userAgent string) {
	if r == nil {
		return "", ""
	}
	return r.RemoteAddr, r.UserAgent()
}
