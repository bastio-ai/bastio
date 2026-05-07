package security

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bastio-ai/bastio/internal/security/normalize"
	"github.com/bastio-ai/bastio/internal/security/session"
)

// SessionDetector is the session-aware detection hook. Implemented by
// detection.CrescendoDetector; kept as an interface so engine doesn't
// take a runtime dependency on the detection package (which would
// introduce a cycle — detection already imports this package).
type SessionDetector interface {
	DetectWithSession(ctx context.Context, sessionID, content string, currentScore float64) ([]Finding, error)
}

// Severity levels for threat findings.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Action determines what happens when a threat is detected.
type Action string

const (
	ActionPass     Action = "pass"
	ActionBlock    Action = "block"
	ActionWarn     Action = "warn"
	ActionMask     Action = "mask"     // lossy one-way rewrite (e.g. ***-**-6789)
	ActionTokenize Action = "tokenize" // reversible placeholders (<PII_SSN_1>)
	ActionRedact   Action = "redact"   // deprecated alias; treat as mask
	ActionLog      Action = "log"
	ActionLogOnly  Action = "log_only" // detection-only, no rewrite
)

// IsMaskLike reports whether an action implies a one-way lossy rewrite.
func (a Action) IsMaskLike() bool { return a == ActionMask || a == ActionRedact }

// ThreatType categorizes detected threats.
type ThreatType string

const (
	ThreatInjection ThreatType = "injection"
	ThreatPII       ThreatType = "pii"
	ThreatJailbreak ThreatType = "jailbreak"
)

// Finding represents a single threat detection result.
type Finding struct {
	ThreatType   ThreatType `json:"threat_type"`
	DetectorName string     `json:"detector_name"`
	Severity     Severity   `json:"severity"`
	Score        float64    `json:"score"`      // 0.0 (safe) to 1.0 (critical threat)
	Confidence   float64    `json:"confidence"` // detector confidence 0.0-1.0
	// SubCategory is the taxonomy slot within a detector's space, using
	// dotted notation: "persona.dan", "extraction.meta", "encoding.base64",
	// "fingerprint.skeleton_key". This is what the dashboard filters and
	// what analytics groups by. Empty means the detector hasn't been
	// migrated to the taxonomy yet; we don't backfill — MatchedPattern
	// stays the fallback label for those.
	SubCategory    string `json:"sub_category,omitempty"`
	MatchedPattern string `json:"matched_pattern,omitempty"`
	MatchedContent string `json:"matched_content,omitempty"`
	Action         Action `json:"action"`
	Message        string `json:"message"`
	// Source is an optional provenance note for the pattern — e.g. the
	// paper or advisory that described the attack. Shown in the detail
	// view; never used for matching decisions.
	Source string `json:"source,omitempty"`
}

// ScanRequest is the input to the security engine.
type ScanRequest struct {
	Content    string            // Text to scan (user message content)
	CustomerID string            // For per-customer config
	ProxyID    string            // For per-proxy config
	EndUserID  string            // End user of the AI app
	IPAddress  string
	UserAgent  string
	Metadata   map[string]string
	// PIIAction overrides how PII findings are handled. Unset falls back
	// to ActionMask. Valid values: ActionMask, ActionTokenize, ActionBlock,
	// ActionWarn, ActionLogOnly.
	PIIAction Action
	// TokenStyle selects the placeholder shape when PIIAction==ActionTokenize.
	// Unset defaults to TokenStyleAngle.
	TokenStyle TokenStyle
	// SkipSanitization tells the engine to compute the final Action but not
	// to rewrite Content. Callers that sanitize per-message (so one TokenMap
	// spans the full conversation) set this to avoid redundant work on the
	// concatenated content.
	SkipSanitization bool
	// Steps opts into the step-based decision path — the same pipeline
	// the playground and SDK adapters use via RunSteps. When set, the
	// engine honors per-step Strategy and Threshold values instead of
	// deriving block/warn/mask from each finding's own Action field.
	// Callers building steps from a security profile should pass them
	// here so profile edits actually flow through the gateway.
	Steps []Step
	// Canonicalize enables the preprocessing stage (unicode/homoglyph/
	// base64 decode) when using the step path. Ignored when Steps is nil.
	Canonicalize bool
	// Role tags the message provenance for role-aware detectors like
	// indirect_injection. Ignored when Steps is nil.
	Role string
	// SessionID plumbs X-Bastio-Session-Id through to session-aware
	// detectors (Crescendo). Empty when the caller isn't session-scoped;
	// those detectors short-circuit when it's unset.
	SessionID string
	// Normalize configures the preprocessing pass that runs before
	// detectors. Zero value = both Unicode and Decode on, matching the
	// profile's defaults. Callers that want raw content (benchmarks,
	// specialised tools) can set one or both to false *and* flip the
	// sentinel via DisableNormalize.
	Normalize normalize.Options
	// DisableNormalize short-circuits the preprocessing pass entirely.
	// Needed because `Normalize: {false, false}` is indistinguishable
	// from zero-value "unspecified, use secure default". Security-
	// critical callers should leave this false.
	DisableNormalize bool
}

// ScanResult is the output of a full security scan.
type ScanResult struct {
	Findings     []Finding     `json:"findings"`
	ThreatScore  float64       `json:"threat_score"` // Aggregate 0.0-1.0
	Action       Action        `json:"action"`       // Final recommended action
	ShouldBlock  bool          `json:"should_block"`
	ThreatTypes  []ThreatType  `json:"threat_types"`
	ScanDuration time.Duration `json:"scan_duration"`
	// SanitizedContent holds the content with PII masked or tokenized.
	// Only populated when PIIAction is ActionMask or ActionTokenize.
	SanitizedContent string `json:"sanitized_content,omitempty"`
	// TokenMap is the reversible placeholder → original mapping used when
	// PIIAction == ActionTokenize. Never serialized — the map itself is PII.
	TokenMap *TokenMap `json:"-"`
}

// Sanitizer is implemented by detectors that can rewrite their own
// findings. The engine calls Mask/TokenizeInto after detection when the
// scan request asks for a rewrite action. TokenizeInto accepts an
// existing TokenMap so the gateway can accumulate placeholders across
// multiple messages in one conversation (repeated values collapse to a
// single placeholder).
type Sanitizer interface {
	Mask(content string) string
	TokenizeInto(content string, tm *TokenMap) string
}

// Detector is the interface all threat detectors implement.
type Detector interface {
	Name() string
	Detect(ctx context.Context, content string) ([]Finding, error)
}

// Engine orchestrates all detectors and aggregates results.
type Engine struct {
	detectors       []Detector
	sessionDetector SessionDetector
	sessionStore    session.Store
}

// NewEngine creates a security engine with the given detectors.
func NewEngine(detectors ...Detector) *Engine {
	return &Engine{detectors: detectors}
}

// SetSessionAware wires the session-history buffer (Crescendo input)
// into the engine. Safe to call nil for either argument on startups
// where Redis isn't configured.
func (e *Engine) SetSessionAware(store session.Store, detector SessionDetector) {
	e.sessionStore = store
	e.sessionDetector = detector
}

// Scan runs all detectors against the content and returns aggregated results.
//
// When req.Steps is set, Scan delegates to the step-based pipeline
// (RunSteps) — the same path used by /v1/detect and the SDK adapters.
// This is the preferred mode for new callers because it honors the
// profile's per-detector Strategy and Threshold columns. The legacy
// parallel-aggregate path is preserved for callers that haven't been
// migrated yet; it treats each finding's intrinsic Action field as
// the source of truth, which ignores profile edits.
func (e *Engine) Scan(ctx context.Context, req *ScanRequest) *ScanResult {
	start := time.Now()
	result := &ScanResult{
		Action: ActionPass,
	}

	if req.Content == "" {
		result.ScanDuration = time.Since(start)
		return result
	}

	// Layer 1 — normalization. Collapse Unicode tricks + optional
	// encoding decode BEFORE any detector sees the text. Findings
	// derived from the flags that fired are appended later so the
	// combination bonus can lift them into block territory.
	normResult := applyNormalize(req)
	if normResult.Normalized != req.Content {
		req.Content = normResult.Normalized
	}

	if len(req.Steps) > 0 {
		r := e.scanViaSteps(ctx, req, start)
		r.Findings = append(r.Findings, normalizeFindings(normResult)...)
		// Re-aggregate so synthetic flag findings influence the final
		// Action. Steps path already set ShouldBlock from the declared
		// strategies; we only want to *lift* action severity, never
		// downgrade a block.
		mergeNormalizeIntoResult(r, normResult)
		e.applySessionAware(ctx, req, r)
		return r
	}

	// Run all detectors in parallel
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, d := range e.detectors {
		wg.Add(1)
		go func(detector Detector) {
			defer wg.Done()

			findings, err := detector.Detect(ctx, req.Content)
			if err != nil {
				slog.Warn("detector error",
					"detector", detector.Name(),
					"error", err,
				)
				return
			}

			mu.Lock()
			result.Findings = append(result.Findings, findings...)
			mu.Unlock()
		}(d)
	}

	wg.Wait()

	// Append synthetic findings for normalization flags that fired
	// so the aggregate step sees them alongside detector findings.
	result.Findings = append(result.Findings, normalizeFindings(normResult)...)

	// Aggregate results. PIIAction influences whether PII findings count
	// toward the auto-block threshold: mask/tokenize/warn/log_only mean
	// "PII alone does not justify blocking".
	e.aggregate(result, req.PIIAction)

	// Session-aware layer after aggregation so Crescendo gets the
	// max scored signal from this turn as its "currentScore".
	e.applySessionAware(ctx, req, result)

	// Apply PII sanitization if the profile asks for it. Skipped on block:
	// the request is going to be rejected anyway, and we don't want to
	// materialize placeholders we'll never restore.
	if !result.ShouldBlock && hasPII(result.Findings) {
		e.applyPIIAction(req, result)
	}

	result.ScanDuration = time.Since(start)

	return result
}

// scanViaSteps is the step-based implementation of Scan, invoked when
// the caller provides req.Steps. Result shape is identical to the
// legacy Scan output so downstream consumers (observability, threat
// recording, response rendering) don't care which path produced it.
//
// Semantics differ from the legacy path in two important ways:
//
//   - Strategy comes from the step, not the finding. A profile with
//     jailbreak_strategy=warn produces Action=warn and ShouldBlock=false
//     even for findings whose intrinsic Action is block. This is the
//     whole point of the unification — profile edits actually take
//     effect at the gateway.
//
//   - Block short-circuits. Once a step blocks, remaining steps are
//     reported as Skipped in the underlying StepsResult but their
//     findings do not appear in ScanResult.Findings. Acceptable because
//     a blocked request was rejected end-to-end; tracking later-step
//     threats for a request that never went to the model is diagnostic
//     sugar we don't need in OSS. The per-step detail is still visible
//     in /v1/detect's playground response.
func (e *Engine) scanViaSteps(ctx context.Context, req *ScanRequest, start time.Time) *ScanResult {
	opts := &RunOptions{
		Canonicalize: req.Canonicalize,
		Role:         req.Role,
	}
	stepsRes := e.RunSteps(ctx, req.Content, req.Steps, opts)

	result := &ScanResult{
		Action:       stepsRes.Action,
		ShouldBlock:  stepsRes.ShouldBlock,
		TokenMap:     stepsRes.TokenMap,
		ScanDuration: time.Since(start),
	}

	// Per-message sanitization is the gateway's responsibility (one
	// TokenMap spans a whole conversation). Expose the canonicalized/
	// rewritten content only when the caller asked for inline sanitize
	// — mirrors the legacy Scan contract.
	if !req.SkipSanitization {
		result.SanitizedContent = stepsRes.SanitizedContent
	}

	// Flatten findings + compute aggregate score / threat types. Match
	// the legacy Scan output so dashboards show the same shape.
	maxScore := 0.0
	typesSeen := map[ThreatType]bool{}
	for _, s := range stepsRes.Steps {
		if s.Skipped {
			continue
		}
		for _, f := range s.Findings {
			result.Findings = append(result.Findings, f)
			typesSeen[f.ThreatType] = true
			if w := f.Score * f.Confidence; w > maxScore {
				maxScore = w
			}
		}
	}
	for t := range typesSeen {
		result.ThreatTypes = append(result.ThreatTypes, t)
	}
	result.ThreatScore = maxScore

	return result
}

// applyPIIAction updates result.Action to reflect the configured PII
// handling and, unless SkipSanitization is set, also populates
// SanitizedContent / TokenMap by running the detector's sanitizer
// against req.Content. Callers that sanitize per-message (gateway
// messages[]) should set SkipSanitization so the engine only decides
// the action.
func (e *Engine) applyPIIAction(req *ScanRequest, result *ScanResult) {
	action := req.PIIAction
	if action == "" {
		action = ActionMask
	}
	switch action {
	case ActionMask, ActionRedact:
		result.Action = ActionMask
		if req.SkipSanitization {
			return
		}
		result.SanitizedContent = e.SanitizeWithMap(req.Content, ActionMask, nil)
	case ActionTokenize:
		result.Action = ActionTokenize
		if req.SkipSanitization {
			return
		}
		tm := NewTokenMap(req.TokenStyle)
		result.SanitizedContent = e.SanitizeWithMap(req.Content, ActionTokenize, tm)
		result.TokenMap = tm
	case ActionWarn:
		if result.Action == ActionPass || result.Action == ActionLog {
			result.Action = ActionWarn
		}
	case ActionLogOnly:
		// No rewrite, no action upgrade. Detection-only mode.
	}
}

// SanitizeWithMap runs the first Sanitizer-implementing detector against
// content. For ActionTokenize the caller provides a TokenMap so multiple
// calls can share one map across messages; nil is accepted for mask.
// Returns the content unchanged when no sanitizer is registered or the
// action is neither mask nor tokenize.
func (e *Engine) SanitizeWithMap(content string, action Action, tm *TokenMap) string {
	for _, d := range e.detectors {
		s, ok := d.(Sanitizer)
		if !ok {
			continue
		}
		switch action {
		case ActionMask, ActionRedact:
			return s.Mask(content)
		case ActionTokenize:
			if tm == nil {
				tm = NewTokenMap(TokenStyleAngle)
			}
			return s.TokenizeInto(content, tm)
		default:
			return content
		}
	}
	return content
}

func hasPII(findings []Finding) bool {
	for _, f := range findings {
		if f.ThreatType == ThreatPII {
			return true
		}
	}
	return false
}

// aggregate computes the final threat score and action from all findings.
//
// ThreatScore reflects every finding so dashboards see the full picture.
// The block decision uses a separate score that excludes PII unless the
// profile explicitly asks to block on PII — mask/tokenize/warn/log_only
// all mean "PII alone should not reject the request".
func (e *Engine) aggregate(result *ScanResult, piiAction Action) {
	if len(result.Findings) == 0 {
		return
	}

	piiBlocks := piiAction == ActionBlock

	typesSeen := map[ThreatType]bool{}
	maxScoreAll := 0.0
	maxScoreForBlock := 0.0
	hasBlock := false

	for _, f := range result.Findings {
		typesSeen[f.ThreatType] = true

		weighted := f.Score * f.Confidence
		if weighted > maxScoreAll {
			maxScoreAll = weighted
		}

		countsForBlock := f.ThreatType != ThreatPII || piiBlocks
		if countsForBlock {
			if weighted > maxScoreForBlock {
				maxScoreForBlock = weighted
			}
			if f.Action == ActionBlock {
				hasBlock = true
			}
		}
	}

	for t := range typesSeen {
		result.ThreatTypes = append(result.ThreatTypes, t)
	}

	combinationBonus := 0.0
	if len(typesSeen) >= 3 {
		combinationBonus = 0.2
	} else if len(typesSeen) >= 2 {
		combinationBonus = 0.1
	}

	result.ThreatScore = min(maxScoreAll+combinationBonus, 1.0)
	blockScore := min(maxScoreForBlock+combinationBonus, 1.0)
	result.ShouldBlock = hasBlock || blockScore >= 0.8

	switch {
	case result.ShouldBlock:
		result.Action = ActionBlock
	case blockScore >= 0.5:
		result.Action = ActionWarn
	case blockScore > 0:
		result.Action = ActionLog
	}
}
