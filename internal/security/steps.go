package security

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Direction indicates whether a step list runs on the request path (user
// → model) or the response path (model → user). Profiles hold separate
// ordered lists for each direction so asymmetric policies stay terse
// — e.g. block injection on input, redact PII on both.
type Direction string

const (
	DirectionInput  Direction = "input"
	DirectionOutput Direction = "output"
)

// Step binds a detector to the strategy and options the profile wants
// applied when that detector fires. It is the declarative unit of a
// profile's input/output pipeline — inspired by Mastra's processor list
// but compiled down to Bastio's existing Detector / Sanitizer machinery.
//
// Strategy is one of ActionBlock, ActionMask, ActionTokenize, ActionWarn,
// ActionLogOnly. Threshold is the weighted score (Score × Confidence)
// above which the strategy fires; zero means any finding fires.
type Step struct {
	Detector  string            `json:"detector"`
	Strategy  Action            `json:"strategy"`
	Threshold float64           `json:"threshold,omitempty"`
	Options   map[string]string `json:"options,omitempty"`
}

// StepResult is what a single Step produced when RunSteps executed it.
// Callers that need an aggregate view should look at StepsResult.
//
// Skipped is set on steps that were configured but not executed because
// an earlier step short-circuited the pipeline (block). The playground
// renders these dimmed so users can see the full intended policy even
// when the first blocking step ends the run — hiding them would make
// it look like the profile has fewer steps than it does.
//
// Threshold echoes the step's configured threshold. Persisting it in
// the result (rather than relying on clients to cross-reference the
// request) means "fired=false at score 0.74" immediately explains
// itself: the trace shows "score 0.74 / threshold 0.80".
type StepResult struct {
	Detector  string        `json:"detector"`
	Strategy  Action        `json:"strategy"`
	Fired     bool          `json:"fired"`
	Skipped   bool          `json:"skipped,omitempty"`
	Action    Action        `json:"action"`    // effective action after threshold + strategy
	Score     float64       `json:"score"`     // max weighted score across findings
	Threshold float64       `json:"threshold"` // step threshold this result was compared against
	Findings  []Finding     `json:"findings,omitempty"`
	Duration  time.Duration `json:"duration"`
}

// StepsResult aggregates RunSteps output. Action is the terminal action
// — block short-circuits; otherwise the strongest rewrite wins (tokenize
// > mask > warn > pass). SanitizedContent reflects the content after all
// rewrite steps have run in order.
type StepsResult struct {
	Direction        Direction    `json:"direction"`
	Steps            []StepResult `json:"steps"`
	Action           Action       `json:"action"`
	ShouldBlock      bool         `json:"should_block"`
	SanitizedContent string       `json:"sanitized_content,omitempty"`
	TokenMap         *TokenMap    `json:"-"`
	// Transforms applied by the canonicalization stage (e.g.
	// "base64-decoded", "fold-homoglyphs"). Empty when canonicalization
	// was disabled or produced no changes.
	Transforms []string      `json:"transforms,omitempty"`
	Duration   time.Duration `json:"duration"`
}

// RunOptions configures a RunSteps call. Optional — nil is valid.
//
// Canonicalize turns on the preprocessing stage that decodes
// base64/hex/url-encoded payloads and folds homoglyphs before
// detectors see the content. Role identifies the message provenance
// so the indirect-injection detector can apply stricter thresholds
// to tool/retrieval/memory content than to raw user input.
type RunOptions struct {
	Canonicalize bool
	Role         string
	// Suppressions are false-positive skips applied after each detector
	// returns, before the step's threshold and strategy run.
	Suppressions []PatternSuppression
}

// RunSteps executes the ordered step list against content, honoring each
// step's strategy. Detectors are looked up by name from the engine's
// registered set; unknown detectors are skipped with a warn log so a
// typo in config doesn't silently pass traffic.
//
// Execution is sequential because rewrites compose: a later redact step
// must see the output of an earlier tokenize step. This is a departure
// from Engine.Scan's parallel model — intentional, because parallel
// rewrites can't be composed without race-prone coordination.
//
// Block short-circuits: once a step chooses ActionBlock, subsequent
// steps are not run (the request is going to be rejected; further work
// is wasted).
func (e *Engine) RunSteps(ctx context.Context, content string, steps []Step, opts *RunOptions) *StepsResult {
	start := time.Now()
	res := &StepsResult{
		SanitizedContent: content,
		Action:           ActionPass,
	}

	if len(steps) == 0 || content == "" {
		res.Duration = time.Since(start)
		return res
	}

	byName := map[string]Detector{}
	for _, d := range e.detectors {
		byName[d.Name()] = d
	}

	// Canonicalization runs once per call, before any detector. The
	// canonical text is what detectors see; the original is preserved
	// in `current` until a rewrite step replaces it, so mask/tokenize
	// output still matches what the user typed surface-wise.
	detectTarget := content
	if opts != nil && opts.Canonicalize {
		c := Canonicalize(content)
		if c.Changed() {
			detectTarget = c.Canonical
			res.Transforms = append(res.Transforms, c.Transforms...)
		}
	}
	if opts != nil && opts.Role != "" {
		ctx = WithRole(ctx, opts.Role)
	}

	current := content

	for idx, step := range steps {
		stepStart := time.Now()
		out := StepResult{
			Detector:  step.Detector,
			Strategy:  step.Strategy,
			Threshold: step.Threshold,
			Action:    ActionPass,
		}

		det, ok := byName[step.Detector]
		if !ok {
			slog.Warn("security step references unknown detector", "detector", step.Detector)
			out.Duration = time.Since(stepStart)
			res.Steps = append(res.Steps, out)
			continue
		}

		findings, err := det.Detect(ctx, detectTarget)
		if err != nil {
			slog.Warn("detector error in step", "detector", step.Detector, "error", err)
			out.Duration = time.Since(stepStart)
			res.Steps = append(res.Steps, out)
			continue
		}

		if opts != nil {
			findings = filterSuppressed(findings, opts.Suppressions)
		}
		setWeightedScores(findings)
		out.Findings = findings
		maxScore := 0.0
		for _, f := range findings {
			if w := f.Score * f.Confidence; w > maxScore {
				maxScore = w
			}
		}
		out.Score = maxScore

		if len(findings) == 0 || maxScore < step.Threshold {
			out.Duration = time.Since(stepStart)
			res.Steps = append(res.Steps, out)
			continue
		}

		out.Fired = true

		switch step.Strategy {
		case ActionBlock:
			out.Action = ActionBlock
			res.Action = ActionBlock
			res.ShouldBlock = true
			out.Duration = time.Since(stepStart)
			res.Steps = append(res.Steps, out)
			// Surface the remaining configured steps as skipped so the
			// playground can render the whole intended pipeline. We do
			// NOT execute them — matching production semantics where a
			// blocked request never touches downstream detectors.
			for _, remaining := range steps[idx+1:] {
				res.Steps = append(res.Steps, StepResult{
					Detector:  remaining.Detector,
					Strategy:  remaining.Strategy,
					Threshold: remaining.Threshold,
					Skipped:   true,
					Action:    ActionPass,
				})
			}
			res.Duration = time.Since(start)
			return res

		case ActionMask, ActionRedact:
			out.Action = ActionMask
			if s, ok := det.(Sanitizer); ok {
				current = s.Mask(current)
				detectTarget = current
			}
			elevate(res, ActionMask)

		case ActionTokenize:
			out.Action = ActionTokenize
			if s, ok := det.(Sanitizer); ok {
				if res.TokenMap == nil {
					res.TokenMap = NewTokenMap(TokenStyleAngle)
				}
				current = s.TokenizeInto(current, res.TokenMap)
				detectTarget = current
			}
			elevate(res, ActionTokenize)

		case ActionWarn:
			out.Action = ActionWarn
			elevate(res, ActionWarn)

		case ActionLogOnly, ActionLog:
			out.Action = ActionLogOnly
			// record-only; leaves res.Action untouched so it can still
			// be promoted by a later step.

		default:
			// unset strategy defaults to log-only so misconfig fails safe
			// (detection still recorded, traffic passes unmodified).
			out.Action = ActionLogOnly
		}

		out.Duration = time.Since(stepStart)
		res.Steps = append(res.Steps, out)
	}

	res.SanitizedContent = current
	res.Duration = time.Since(start)
	return res
}

// elevate promotes res.Action only upward along the strength ordering:
// pass < log_only < warn < mask < tokenize < block. Lower-ranked steps
// never downgrade a stronger action chosen earlier in the list.
func elevate(res *StepsResult, to Action) {
	if rank(to) > rank(res.Action) {
		res.Action = to
	}
}

func rank(a Action) int {
	switch a {
	case ActionPass:
		return 0
	case ActionLog, ActionLogOnly:
		return 1
	case ActionWarn:
		return 2
	case ActionMask, ActionRedact:
		return 3
	case ActionTokenize:
		return 4
	case ActionBlock:
		return 5
	}
	return 0
}

// DefaultInputSteps returns a sensible input pipeline for profiles
// that haven't customized one. Order matters:
//
//  1. indirect_injection — fires only on non-user roles; lands first
//     so bad tool/retrieval content never reaches downstream detectors
//     that might rewrite and forward it.
//  2. injection — user-role prompt injection; blocks high-confidence hits.
//  3. jailbreak — warn at 0.6, block at 0.8. Mid-confidence hits
//     (fiction framing, hypotheticals) surface as findings without
//     dropping the request; high-confidence DAN-style hits block.
//  4. secrets — mask before forwarding; distinct from PII.
//  5. topic_policy — per-customer rules (no-op unless enabled + rows).
//  6. pii — last because rewrites change the text other detectors saw.
func DefaultInputSteps() []Step {
	steps := []Step{
		{Detector: "indirect_injection", Strategy: ActionBlock},
		{Detector: "injection", Strategy: ActionBlock, Threshold: 0.72},
	}
	steps = append(steps, jailbreakSteps(ActionBlock, 0.6)...)
	return append(steps,
		Step{Detector: "secrets", Strategy: ActionMask},
		Step{Detector: "topic_policy", Strategy: ActionWarn},
		Step{Detector: "pii", Strategy: ActionMask},
	)
}

// DefaultOutputSteps returns the default response pipeline: scan
// model output for exfiltration attempts (system prompt reveal,
// echoed secrets) and mask any PII the model generated.
func DefaultOutputSteps() []Step {
	return []Step{
		{Detector: "exfil", Strategy: ActionBlock},
		{Detector: "secrets", Strategy: ActionMask},
		{Detector: "pii", Strategy: ActionMask},
	}
}

// StepsFromLegacyProfile derives input/output step lists from the flat
// Profile fields that predate the declarative shape. Used when a profile
// row was persisted before the step columns existed, so the gateway's
// behavior is unchanged. Remove when all rows have been migrated.
//
// Ordering mirrors DefaultInputSteps: indirect_injection (trust-boundary
// guard) → injection → jailbreak → secrets → topic_policy → pii.
// Response-side adds exfil + secrets + pii (when scan_response is set).
// Strategies are read from the profile's per-detector columns; the
// fallback when the column is empty or unknown is the detector's
// safest default (block for text threats, mask for PII/secrets, warn
// for jailbreak).
func StepsFromLegacyProfile(p Profile) (input []Step, output []Step) {
	if p.IndirectInjectionEnabled {
		input = append(input, Step{
			Detector: "indirect_injection",
			Strategy: strategyOr(p.IndirectInjectionStrategy, ActionBlock),
		})
	}
	if p.InjectionEnabled {
		threshold := p.InjectionThreshold
		if threshold == 0 {
			threshold = 0.72
		}
		input = append(input, Step{
			Detector:  "injection",
			Strategy:  strategyOr(p.InjectionStrategy, ActionBlock),
			Threshold: threshold,
		})
	}
	if p.JailbreakEnabled {
		threshold := p.JailbreakThreshold
		if threshold == 0 {
			threshold = 0.6
		}
		input = append(input, jailbreakSteps(strategyOr(p.JailbreakStrategy, ActionWarn), threshold)...)
	}
	if p.SecretsEnabled {
		input = append(input, Step{
			Detector: "secrets",
			Strategy: strategyOr(p.SecretsStrategy, ActionMask),
		})
	}
	if p.TopicPolicyEnabled {
		// Strategy on the step is a floor; topic rules carry their
		// own action per-pattern via security_patterns.action.
		input = append(input, Step{Detector: "topic_policy", Strategy: ActionWarn})
	}
	if p.PIIEnabled {
		strategy := p.PIIAction
		if strategy == "" {
			strategy = ActionMask
		}
		input = append(input, Step{Detector: "pii", Strategy: strategy})
	}

	// Output pipeline — scan only if the operator asked for it, to
	// avoid doubling the gateway's egress latency for customers that
	// only care about inbound protection.
	if p.OutputExfilEnabled {
		output = append(output, Step{
			Detector: "exfil",
			Strategy: strategyOr(p.OutputExfilStrategy, ActionBlock),
		})
	}
	if p.SecretsEnabled {
		output = append(output, Step{
			Detector: "secrets",
			Strategy: strategyOr(p.SecretsStrategy, ActionMask),
		})
	}
	if p.PIIEnabled && p.PIIScanResponse {
		strategy := p.PIIAction
		if strategy == "" {
			strategy = ActionMask
		}
		output = append(output, Step{Detector: "pii", Strategy: strategy})
	}
	return input, output
}

// strategyOr returns the configured strategy when set, otherwise the
// fallback. Keeps the step-building code readable without a ladder of
// if-statements for every detector.
func strategyOr(configured, fallback Action) Action {
	if configured == "" {
		return fallback
	}
	return configured
}

const (
	jailbreakWarnThreshold  = 0.6
	jailbreakBlockThreshold = 0.8
)

// jailbreakSteps emits the jailbreak pipeline for a profile strategy.
// Block is dual-band: warn at the configured (or default 0.6) threshold
// so mid-confidence hits still create findings, then block at 0.8.
// Warn and log_only stay a single step at the configured threshold.
func jailbreakSteps(strategy Action, threshold float64) []Step {
	if threshold <= 0 {
		threshold = jailbreakWarnThreshold
	}
	if strategy != ActionBlock {
		return []Step{{Detector: "jailbreak", Strategy: strategy, Threshold: threshold}}
	}
	warnAt := threshold
	if warnAt >= jailbreakBlockThreshold {
		return []Step{{Detector: "jailbreak", Strategy: ActionBlock, Threshold: warnAt}}
	}
	return []Step{
		{Detector: "jailbreak", Strategy: ActionWarn, Threshold: warnAt},
		{Detector: "jailbreak", Strategy: ActionBlock, Threshold: jailbreakBlockThreshold},
	}
}

// Validate reports whether a step list is internally consistent. Called
// from the API handler that persists profiles so bad configs never reach
// the hot path. Returns nil on success; a descriptive error otherwise.
func ValidateSteps(steps []Step) error {
	for i, s := range steps {
		if s.Detector == "" {
			return fmt.Errorf("step %d: detector is required", i)
		}
		switch s.Strategy {
		case ActionBlock, ActionMask, ActionRedact, ActionTokenize, ActionWarn, ActionLogOnly, ActionLog, "":
			// ok — empty strategy falls back to log-only at runtime
		default:
			return fmt.Errorf("step %d: invalid strategy %q", i, s.Strategy)
		}
		if s.Threshold < 0 || s.Threshold > 1 {
			return fmt.Errorf("step %d: threshold %v must be in [0, 1]", i, s.Threshold)
		}
	}
	return nil
}
