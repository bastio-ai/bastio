// Package llmpipeline holds the orchestration steps the gateway and
// the workspace handler share when running an LLM request through the
// security pipeline. Both transports (HTTP-shaped /v1/chat/completions
// and the workspace's session-authed chat) need:
//
//   - the same per-detector preflight scan against the user message
//   - the same cost-estimation rate table (so traces and message rows
//     never disagree on $0.0024 vs $0)
//   - the same trace + analytics + threat-row write at end of turn
//   - the same body-truncation bound on persisted request/response
//
// Before this package, both code paths kept their own copies of all
// four. The duplication drifted: the gateway's cost table priced
// claude-3-5-sonnet but missed claude-3-7; the workspace's table
// covered claude-3-7 but used different rates than the gateway. This
// package is the single source of truth.
//
// Out of scope deliberately: provider resolution, SSE writes, request
// parsing, response-side PII restoration, overlay plugin orchestration.
// Those differ enough between the two transports that a unified
// abstraction would be a leaky one. Keep them in their respective
// callers.
package llmpipeline

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/normalize"
)

// PreflightOptions describes the scan request both transports build
// before sending the user message to the provider. The fields that
// genuinely differ between transports are SkipSanitization and Role
// (the rest mirror the profile + identity).
//
// SkipSanitization=true: the gateway tokenizes per-message itself —
// the engine should not rewrite content, just report findings.
//
// SkipSanitization=false: the workspace lets the engine sanitize and
// uses ScanResult.SanitizedContent as the persisted message body.
type PreflightOptions struct {
	Engine  *security.Engine
	Profile *security.Profile
	Content string

	CustomerID uuid.UUID
	EndUserID  string
	IPAddress  string
	UserAgent  string
	SessionID  string

	SkipSanitization bool
	Role             string // matches ScanRequest.Role; defaults to security.RoleUser
}

// PreflightScan runs the user-message scan exactly the way both
// gateway and workspace need. Returns nil when Engine or Profile is
// nil — the caller should treat this as "no policy enforcement;
// proceed as-is".
//
// Steps are derived from the profile (see security.StepsFromLegacyProfile)
// so per-detector strategies edited in the dashboard apply uniformly
// to every transport. The legacy parallel-aggregate path that ignores
// profile edits is never used.
func PreflightScan(ctx context.Context, opts PreflightOptions) *security.ScanResult {
	if opts.Engine == nil || opts.Profile == nil {
		return nil
	}
	role := opts.Role
	if role == "" {
		role = security.RoleUser
	}
	steps, _ := security.StepsFromLegacyProfile(*opts.Profile)
	return opts.Engine.Scan(ctx, &security.ScanRequest{
		Content:          opts.Content,
		CustomerID:       opts.CustomerID.String(),
		EndUserID:        opts.EndUserID,
		IPAddress:        opts.IPAddress,
		UserAgent:        opts.UserAgent,
		SessionID:        opts.SessionID,
		PIIAction:        opts.Profile.PIIAction,
		TokenStyle:       opts.Profile.PIITokenStyle,
		SkipSanitization: opts.SkipSanitization,
		Steps:            steps,
		Canonicalize:     opts.Profile.CanonicalizeEnabled,
		// Session-aware detectors that are profile-gated read this
		// flag; default-off so upgrades don't change behavior.
		RateAnomalyEnabled: opts.Profile.RateAnomalyEnabled,
		Normalize: normalize.Options{
			Unicode: opts.Profile.NormalizeUnicode,
			Decode:  opts.Profile.NormalizeDecode,
		},
		Role: role,
	})
}

// ChatTraceInput collects the per-turn signals the trace + analytics
// + threat-catalog writes need. One struct, two callers — the
// transport-specific bits (Path, TraceName, Environment, APIKeyID,
// SessionID) flow through as plain fields.
type ChatTraceInput struct {
	Recorder *observability.Recorder // nil → no-op (OSS without ClickHouse)

	TraceID    uuid.UUID
	CustomerID uuid.UUID
	APIKeyID   uuid.UUID // gateway only; workspace passes uuid.Nil

	Method string // "POST" for both transports today
	Path   string // canonicalized request path

	Provider string
	Model    string

	StartedAt    time.Time
	CompletedAt  time.Time
	InputTokens  int
	OutputTokens int
	CostCents    float64

	// Status maps to TraceRecord.Status (e.g. "ok" | "blocked" |
	// "error" | "rate_limited" | a finish_reason from the provider).
	// HTTPStatus is computed from Status by the gateway; workspace
	// leaves it zero.
	Status     string
	HTTPStatus uint16

	EndUserID string
	SessionID string
	IPHash    string
	UserAgent string

	Environment string
	Release     string
	TraceName   string
	Tags        map[string]string

	RequestBody  string
	ResponseBody string

	// ScanResult is the preflight result. When non-nil and findings
	// are present, RecordChatTrace also fans out to RecordThreats.
	ScanResult *security.ScanResult
}

// RecordChatTrace writes the trace, analytics row, and (when scan
// findings are present) the threat catalog rows. No-op when Recorder
// is nil. Both transports call this at end-of-turn — no other code
// path should write to RecordTrace/RecordAnalytics/RecordThreats for
// chat completion flows; if it does, the threat catalog will
// double-count.
func RecordChatTrace(in ChatTraceInput) {
	if in.Recorder == nil {
		return
	}
	threatTypes := []string{}
	var threatScore float32
	securityAction := "pass"
	threatDetected := false
	if in.ScanResult != nil {
		threatScore = float32(in.ScanResult.ThreatScore)
		for _, t := range in.ScanResult.ThreatTypes {
			threatTypes = append(threatTypes, string(t))
		}
		if in.ScanResult.ShouldBlock {
			securityAction = "block"
		} else if string(in.ScanResult.Action) != "" {
			securityAction = string(in.ScanResult.Action)
		}
		threatDetected = len(in.ScanResult.Findings) > 0
	}
	durMs := uint32(in.CompletedAt.Sub(in.StartedAt).Milliseconds())
	method := in.Method
	if method == "" {
		method = "POST"
	}
	trace := &observability.TraceRecord{
		ID:             in.TraceID,
		CustomerID:     in.CustomerID,
		APIKeyID:       in.APIKeyID,
		Method:         method,
		Path:           in.Path,
		Provider:       in.Provider,
		Model:          in.Model,
		StartedAt:      in.StartedAt,
		CompletedAt:    in.CompletedAt,
		DurationMs:     durMs,
		InputTokens:    uint32(in.InputTokens),
		OutputTokens:   uint32(in.OutputTokens),
		TotalTokens:    uint32(in.InputTokens + in.OutputTokens),
		CostCents:      in.CostCents,
		Status:         in.Status,
		HTTPStatus:     in.HTTPStatus,
		ThreatDetected: threatDetected,
		ThreatTypes:    threatTypes,
		ThreatScore:    threatScore,
		SecurityAction: securityAction,
		EndUserID:      in.EndUserID,
		SessionID:      in.SessionID,
		Environment:    in.Environment,
		Release:        in.Release,
		TraceName:      in.TraceName,
		Tags:           in.Tags,
		RequestBody:    TruncateBody(in.RequestBody),
		ResponseBody:   TruncateBody(in.ResponseBody),
	}
	in.Recorder.RecordTrace(trace)
	in.Recorder.RecordAnalytics(trace)
	if in.ScanResult != nil && len(in.ScanResult.Findings) > 0 {
		in.Recorder.RecordThreats(in.TraceID, in.CustomerID, uuid.Nil, in.ScanResult, in.EndUserID, in.IPHash, in.UserAgent)
	}
}

// EstimateCostCents returns approximate cost in cents with fractional
// precision (so a $0.001 charge is not silently rounded to zero in
// the trace). Single source of truth — gateway and workspace
// previously kept divergent rate tables; reconciled here.
//
// Returned value is float64 cents. Callers that persist into integer
// columns (workspace_messages.cost_cents) should int(...) at write time.
func EstimateCostCents(model string, inputTokens, outputTokens int) float64 {
	inRate, outRate := pricingPer1MTokens(model)
	if inRate == 0 && outRate == 0 {
		return 0
	}
	c := (float64(inputTokens)*inRate + float64(outputTokens)*outRate) / 1_000_000
	if c < 0 {
		return 0
	}
	return c
}

// pricingPer1MTokens returns input/output rates in CENTS per 1M tokens.
// Order matters — more-specific prefixes/contains must come first
// (gpt-4o-mini before gpt-4o; o1-mini before o1).
func pricingPer1MTokens(model string) (inputCents, outputCents float64) {
	switch {
	case strings.Contains(model, "gpt-4o-mini"), strings.Contains(model, "gpt-5.4-mini"):
		return 15, 60
	case strings.Contains(model, "gpt-4o"), strings.Contains(model, "gpt-5.4"):
		return 250, 1000
	case strings.HasPrefix(model, "gpt-4-turbo"):
		return 1000, 3000
	case strings.HasPrefix(model, "gpt-4"):
		return 3000, 6000
	case strings.HasPrefix(model, "gpt-3.5"):
		return 50, 150
	case strings.HasPrefix(model, "o1-mini"):
		return 300, 1200
	case strings.HasPrefix(model, "o1"):
		return 1500, 6000
	case strings.Contains(model, "claude-haiku"), strings.Contains(model, "claude-3-haiku"):
		return 25, 125
	case strings.Contains(model, "claude-sonnet"),
		strings.Contains(model, "claude-3-5-sonnet"),
		strings.Contains(model, "claude-3.5-sonnet"),
		strings.Contains(model, "claude-3-7"):
		return 300, 1500
	case strings.Contains(model, "claude-opus"), strings.Contains(model, "claude-3-opus"):
		return 1500, 7500
	default:
		return 0, 0
	}
}

// TruncateBody clips to 256KB for the trace row's RequestBody /
// ResponseBody columns. Anything past the limit is replaced with a
// marker so consumers know the body was clipped.
const truncateLimit = 256 * 1024

func TruncateBody(s string) string {
	if len(s) <= truncateLimit {
		return s
	}
	return s[:truncateLimit] + "\n\n... (truncated)"
}
