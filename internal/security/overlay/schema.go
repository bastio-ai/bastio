// Package overlay implements tenant-owned policy overlays that sit on
// top of the core security configuration. An overlay is additive: it
// adds patterns and access rules, and can override per-detector
// thresholds and strategies. It cannot disable a core detector — that
// capability is intentionally operator-only, via security_profiles.
//
// Loosening overrides (threshold up, strategy weakened) are permitted
// but the merge layer emits a WARN log line with overlay identity so
// operators notice them in their log stream without having to open the
// UI.
//
// The snapshot format stored in tenant_policy_overlay_versions.snapshot
// is a delta only — never a copy of core config. This keeps audit
// diffs clean, prevents drift, and means a stale snapshot can't silently
// roll back an operator-driven core-config change.
package overlay

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// CurrentSchemaVersion is the version of OverlaySnapshot written by the
// current binary. Older snapshots may be present in the DB after upgrades;
// Decode is responsible for carrying them forward.
const CurrentSchemaVersion = 1

// OverlaySnapshot is the full contents of a single
// tenant_policy_overlay_versions row. Everything except SchemaVersion
// is optional — a snapshot with nothing but a schema version is valid,
// it just means the overlay exists but contributes no runtime effect
// yet (useful as a starting point for an in-progress draft).
type OverlaySnapshot struct {
	SchemaVersion int `json:"schema_version"`

	// AdditionalPatterns are appended to whatever security_patterns rows
	// already exist for this customer. Stored here for audit / UI /
	// future-engine use; runtime enforcement lives in the topic detector
	// and is wired in Phase 2.
	AdditionalPatterns []PatternRule `json:"additional_patterns,omitempty"`

	// AdditionalAccessRules are appended to security_access_rules.
	// Stored for audit / UI; runtime enforcement wired in Phase 2.
	AdditionalAccessRules []AccessRule `json:"additional_access_rules,omitempty"`

	// DetectorOverrides selectively override per-detector threshold and
	// strategy. A nil entry means inherit the core value. There is
	// deliberately no "enabled" field — an overlay cannot turn off a
	// core detector.
	DetectorOverrides DetectorOverrides `json:"detector_overrides"`

	// PluginDetectors list third-party detectors (registered via the
	// plugin registry) that this overlay activates. Empty by default.
	// Wired in Phase 3.
	PluginDetectors []PluginDetectorRef `json:"plugin_detectors,omitempty"`
}

// DetectorOverrides holds optional per-detector overrides. Nil pointers
// mean "inherit from core". Only detectors that accept a threshold /
// strategy (i.e. the score-based ones) are listed.
type DetectorOverrides struct {
	Injection         *DetectorOverride `json:"injection,omitempty"`
	Jailbreak         *DetectorOverride `json:"jailbreak,omitempty"`
	Secrets           *DetectorOverride `json:"secrets,omitempty"`
	IndirectInjection *DetectorOverride `json:"indirect_injection,omitempty"`
	OutputExfil       *DetectorOverride `json:"output_exfil,omitempty"`
	PII               *PIIOverride      `json:"pii,omitempty"`
}

// DetectorOverride overrides threshold and/or strategy for one of the
// score-based detectors. Either field may be nil to inherit.
type DetectorOverride struct {
	Threshold *float64 `json:"threshold,omitempty"`
	Strategy  *string  `json:"strategy,omitempty"`
}

// PIIOverride is separate because PII uses Action values (mask/tokenize)
// that don't apply to the other detectors, and has a different schema
// for token style.
type PIIOverride struct {
	Action *string `json:"action,omitempty"`
}

// PatternRule mirrors the shape of the security_patterns table. Stored
// in the snapshot as value types so we can diff clean.
type PatternRule struct {
	Name        string `json:"name"`
	PatternType string `json:"pattern_type"` // regex | keyword | semantic
	Pattern     string `json:"pattern"`
	Action      string `json:"action"`   // block | warn | log
	Severity    string `json:"severity"` // low | medium | high | critical
}

// AccessRule mirrors security_access_rules.
type AccessRule struct {
	RuleType string `json:"rule_type"` // ip_allowlist | ip_blocklist | geo_allow | geo_block
	Value    string `json:"value"`
}

// PluginDetectorRef names a detector registered via the plugin registry
// and carries its opaque config.
type PluginDetectorRef struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config,omitempty"`
}

// Validate returns a non-nil error if the snapshot would be unsafe to
// store. Keep this in sync with the DB-level CHECK constraints — the
// goal is to reject bad shapes at API write time rather than letting
// them surface as a constraint violation mid-transaction.
func (s *OverlaySnapshot) Validate() error {
	if s == nil {
		return fmt.Errorf("snapshot is nil")
	}
	if s.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema version %d (current: %d)", s.SchemaVersion, CurrentSchemaVersion)
	}
	for i, p := range s.AdditionalPatterns {
		if p.Name == "" {
			return fmt.Errorf("additional_patterns[%d]: name is required", i)
		}
		switch p.PatternType {
		case "regex", "keyword", "semantic":
		default:
			return fmt.Errorf("additional_patterns[%d]: pattern_type must be regex|keyword|semantic, got %q", i, p.PatternType)
		}
		if p.Pattern == "" {
			return fmt.Errorf("additional_patterns[%d]: pattern is required", i)
		}
		switch p.Action {
		case "block", "warn", "log", "log_only":
		case "":
			// allow — merge will fall back to "warn"
		default:
			return fmt.Errorf("additional_patterns[%d]: action must be block|warn|log, got %q", i, p.Action)
		}
		switch p.Severity {
		case "low", "medium", "high", "critical", "":
		default:
			return fmt.Errorf("additional_patterns[%d]: severity must be low|medium|high|critical, got %q", i, p.Severity)
		}
	}
	for i, r := range s.AdditionalAccessRules {
		switch r.RuleType {
		case "ip_allowlist", "ip_blocklist", "geo_allow", "geo_block":
		default:
			return fmt.Errorf("additional_access_rules[%d]: rule_type must be ip_allowlist|ip_blocklist|geo_allow|geo_block, got %q", i, r.RuleType)
		}
		if r.Value == "" {
			return fmt.Errorf("additional_access_rules[%d]: value is required", i)
		}
	}
	if err := validateOverrides(s.DetectorOverrides); err != nil {
		return err
	}
	for i, p := range s.PluginDetectors {
		if p.Name == "" {
			return fmt.Errorf("plugin_detectors[%d]: name is required", i)
		}
	}
	return nil
}

func validateOverrides(o DetectorOverrides) error {
	check := func(name string, d *DetectorOverride, allowed []string) error {
		if d == nil {
			return nil
		}
		if d.Threshold != nil {
			if *d.Threshold < 0 || *d.Threshold > 1 {
				return fmt.Errorf("detector_overrides.%s.threshold must be in [0,1], got %f", name, *d.Threshold)
			}
		}
		if d.Strategy != nil && !slices.Contains(allowed, *d.Strategy) {
			return fmt.Errorf("detector_overrides.%s.strategy must be one of %v, got %q", name, allowed, *d.Strategy)
		}
		return nil
	}
	textThreat := []string{"block", "warn", "log_only"}
	if err := check("injection", o.Injection, textThreat); err != nil {
		return err
	}
	if err := check("jailbreak", o.Jailbreak, textThreat); err != nil {
		return err
	}
	if err := check("indirect_injection", o.IndirectInjection, textThreat); err != nil {
		return err
	}
	if err := check("output_exfil", o.OutputExfil, textThreat); err != nil {
		return err
	}
	// secrets gets its own allowed set (mask is valid, tokenize is not).
	if err := check("secrets", o.Secrets, []string{"block", "mask", "warn", "log_only"}); err != nil {
		return err
	}
	if o.PII != nil && o.PII.Action != nil {
		switch *o.PII.Action {
		case "block", "mask", "tokenize", "warn", "log_only":
		default:
			return fmt.Errorf("detector_overrides.pii.action must be block|mask|tokenize|warn|log_only, got %q", *o.PII.Action)
		}
	}
	return nil
}

// MaxSnapshotBytes caps the JSON-encoded size of a snapshot at the API
// boundary. Typical real-world snapshots fit in <20KB; this ceiling
// exists to prevent a pathological submission from blowing up the
// loader cache / hot-path memory.
const MaxSnapshotBytes = 256 * 1024

// Warning flags an override inside a snapshot that weakens security
// relative to a reference profile (usually the shipped defaults).
// Computed at serve time by a WarningAnalyzer injected into the
// Handler from the security package — this keeps the overlay package
// free of dependencies on security.
type Warning struct {
	// Detector is the detector name whose override triggered the
	// warning (e.g. "injection", "pii").
	Detector string `json:"detector"`
	// Field is the override field — "threshold", "strategy", "action".
	Field string `json:"field"`
	// From is the reference value. Stringified so the UI can render
	// numbers and enum strings uniformly.
	From string `json:"from"`
	// To is the override value.
	To string `json:"to"`
	// Message is a one-line plain-English summary suitable for a
	// dashboard banner.
	Message string `json:"message"`
}

// WarningAnalyzer computes loosening warnings for a snapshot. The
// handler calls it on GetVersion / GetOverlay responses when one is
// registered. Implementations live in the security package.
type WarningAnalyzer func(snap *OverlaySnapshot) []Warning

// PreviewSample is one content string to re-run through a candidate
// overlay + the current active, for the preview/validate endpoint.
type PreviewSample struct {
	Content string `json:"content"`
}

// PreviewSampleResult reports what the candidate and current-active
// effective profiles each decided for a single sample, and what kind
// of divergence (if any) that represents.
type PreviewSampleResult struct {
	Content         string `json:"content"`
	ActiveAction    string `json:"active_action"`
	CandidateAction string `json:"candidate_action"`
	ActiveBlock     bool   `json:"active_block"`
	CandidateBlock  bool   `json:"candidate_block"`
	// Divergence uses the same labels as shadow events
	// (would_block | would_allow | threshold_diff). Empty when both
	// agreed.
	Divergence string `json:"divergence,omitempty"`
}

// PreviewSummary aggregates the per-sample results for UI display:
// a quick "what changed" headline without parsing every row.
type PreviewSummary struct {
	Total          int `json:"total"`
	Matching       int `json:"matching"`
	WouldBlock     int `json:"would_block"`
	WouldAllow     int `json:"would_allow"`
	ThresholdDiff  int `json:"threshold_diff"`
}

// PreviewResult bundles the per-sample and aggregate views.
type PreviewResult struct {
	Results []PreviewSampleResult `json:"results"`
	Summary PreviewSummary        `json:"summary"`
}

// PreviewRunner re-runs detection on the given samples against the
// candidate snapshot and the currently-active overlay (if any) for
// the given customer, returning the difference. Implementations live
// in the security package and are injected into the overlay handler
// to keep the overlay package engine-free.
type PreviewRunner interface {
	Preview(ctx context.Context, customerID uuid.UUID, proxyID uuid.UUID, candidate *OverlaySnapshot, samples []PreviewSample) (*PreviewResult, error)
}
