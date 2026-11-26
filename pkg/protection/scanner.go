// Package protection exposes content inspection to application connectors
// without exposing the internal detector implementation or matched content.
package protection

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bastio-ai/bastio/internal/devmode"
	"github.com/bastio-ai/bastio/internal/mcp"
	"github.com/bastio-ai/bastio/internal/security"
)

// CategoryNotRewritable marks a block caused by detected sensitive content
// that could not be removed from the text, typically because it is encoded.
const CategoryNotRewritable = "sensitive_content_not_rewritable"

type Decision struct {
	Action     string   `json:"action"`
	Text       string   `json:"-"`
	Categories []string `json:"categories"`
}

type Scanner struct{ engine *security.Engine }

// NewScanner uses the same standalone detectors as bastio scan and dev mode.
// It does not load cloud ML sidecars or an organization's database profile.
func NewScanner() *Scanner { return &Scanner{engine: devmode.BuildDefaultSecurityEngine()} }

func (s *Scanner) Scan(ctx context.Context, text, role, piiAction string) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if s == nil || s.engine == nil {
		return Decision{}, fmt.Errorf("scanner unavailable")
	}
	if piiAction != "mask" && piiAction != "block" {
		return Decision{}, fmt.Errorf("unsupported PII policy")
	}
	steps := []security.Step{
		{Detector: "injection", Strategy: security.ActionBlock, Threshold: .72},
		{Detector: "jailbreak", Strategy: security.ActionBlock, Threshold: .6},
		{Detector: "indirect_injection", Strategy: security.ActionBlock},
		{Detector: "pii", Strategy: security.Action(piiAction)},
		{Detector: "secrets", Strategy: security.ActionMask},
	}
	r := s.engine.Scan(ctx, &security.ScanRequest{Content: text, Role: role, Steps: steps, Canonicalize: true})
	if r == nil {
		return Decision{}, fmt.Errorf("scanner produced no decision")
	}
	d := Decision{Action: "allow", Text: text, Categories: []string{}}
	seen := map[string]bool{}
	for _, f := range r.Findings {
		seen[f.DetectorName] = true
	}
	// Fail closed whenever a detector found something it was told to remove
	// but the text still carries it: a detector that cannot rewrite, a
	// rewrite that could not be verified, or a value found only inside an
	// encoded payload (base64, hex, %-escapes) that could not be located.
	// The engine records that last case as "warn" and leaves the content as
	// it was; for this adapter that is detected sensitive content, so it is
	// withheld rather than forwarded.
	notRewritable := len(r.UnappliedRewrites) > 0 || r.RewriteUnavailable || len(r.UnmaskedEncoded) > 0
	// A rewrite step that fired but left the text as it was has not removed
	// what it found either.
	if rewrote(r) && (r.SanitizedContent == "" || r.SanitizedContent == text) {
		notRewritable = true
	}
	if notRewritable {
		seen[CategoryNotRewritable] = true
	}
	for name := range seen {
		d.Categories = append(d.Categories, name)
	}
	sort.Strings(d.Categories)
	if notRewritable {
		d.Action, d.Text = "block", ""
		return d, nil
	}
	if r.ShouldBlock {
		d.Action, d.Text = "block", ""
		return d, nil
	}
	// A rewrite is reported only when a step actually rewrote something. The
	// engine canonicalizes text before detection (for example "…" becomes
	// "..."), so SanitizedContent can differ from the input with no finding
	// at all; that difference alone is not a mask decision and the caller
	// keeps its original text.
	if rewrote(r) && r.SanitizedContent != "" && r.SanitizedContent != text {
		d.Action, d.Text = "mask", r.SanitizedContent
	}
	return d, ctx.Err()
}

func rewrote(r *security.ScanResult) bool {
	for _, action := range r.DetectorActions {
		switch action {
		case security.ActionMask, security.ActionRedact, security.ActionTokenize:
			return true
		}
	}
	return false
}

type ToolDecision struct {
	Action     string          `json:"action"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Categories []string        `json:"categories"`
}

func (s *Scanner) ScanTool(ctx context.Context, name string, args json.RawMessage, blocked []string) (ToolDecision, error) {
	if s == nil || s.engine == nil || ctx.Err() != nil {
		return ToolDecision{}, fmt.Errorf("scanner unavailable")
	}
	if !json.Valid(args) {
		return ToolDecision{}, fmt.Errorf("invalid tool arguments")
	}
	d := mcp.Inspect(ctx, s.engine, mcp.InspectRequest{ToolName: name, Arguments: args, Policy: mcp.Policy{BlockedTools: blocked}})
	if err := ctx.Err(); err != nil {
		return ToolDecision{}, err
	}
	return ToolDecision{Action: d.Action, Arguments: d.Arguments, Categories: d.Categories}, nil
}
