package gateway

import (
	"context"
	"net/http"
	"sync"

	"github.com/bastio-ai/bastio/internal/security"
)

// responseFindingSource marks findings from the response scan in the trace
// and threat log, as monitor mode already does ("input" is the request side).
const responseFindingSource = "response"

type responseFindingsKey struct{}

// responseFindings collects what the enforcing response scan found during
// one request, so recordTrace can write it next to the input findings.
// The scan runs in several places (per assistant text, per stream channel,
// per completed frame) that only share the request context.
type responseFindings struct {
	mu     sync.Mutex
	result *security.ScanResult
	seen   map[string]bool
}

// withResponseFindings attaches a collector to the request unless one is
// already there (ChatCompletions hands its request to forwardNativeAPI).
func withResponseFindings(r *http.Request) *http.Request {
	if responseFindingsFrom(r.Context()) != nil {
		return r
	}
	c := &responseFindings{seen: make(map[string]bool)}
	return r.WithContext(context.WithValue(r.Context(), responseFindingsKey{}, c))
}

func responseFindingsFrom(ctx context.Context) *responseFindings {
	c, _ := ctx.Value(responseFindingsKey{}).(*responseFindings)
	return c
}

// add records one response scan. Each finding carries the action its step
// actually took (the scan's DetectorActions), because the trace merge drops
// DetectorActions so input and output strategies for the same detector do
// not overwrite each other. The same text can be scanned twice (stream
// deltas and the completed event), so repeated findings are kept once.
func (c *responseFindings) add(scan *security.ScanResult) {
	if c == nil || scan == nil || len(scan.Findings) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.result == nil {
		c.result = &security.ScanResult{}
	}
	added := false
	for _, f := range scan.Findings {
		if effective, ok := scan.DetectorActions[f.DetectorName]; ok {
			f.Action = effective
		}
		f.Source = responseFindingSource
		key := f.DetectorName + "\x00" + string(f.ThreatType) + "\x00" + f.SubCategory + "\x00" + f.MatchedPattern + "\x00" + f.MatchedContent + "\x00" + string(f.Action)
		if c.seen[key] {
			continue
		}
		c.seen[key] = true
		c.result.Findings = append(c.result.Findings, f)
		added = true
	}
	if !added {
		return
	}
	for _, t := range scan.ThreatTypes {
		if !containsThreatType(c.result.ThreatTypes, t) {
			c.result.ThreatTypes = append(c.result.ThreatTypes, t)
		}
	}
	for _, d := range scan.BlockingDetectors {
		if !containsString(c.result.BlockingDetectors, d) {
			c.result.BlockingDetectors = append(c.result.BlockingDetectors, d)
		}
	}
	if scan.ThreatScore > c.result.ThreatScore {
		c.result.ThreatScore = scan.ThreatScore
	}
	c.result.ShouldBlock = c.result.ShouldBlock || scan.ShouldBlock
	c.result.Action = security.StrongerAction(c.result.Action, scan.Action)
}

func (c *responseFindings) snapshot() *security.ScanResult {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.result == nil {
		return nil
	}
	out := *c.result
	out.Findings = append([]security.Finding(nil), c.result.Findings...)
	out.ThreatTypes = append([]security.ThreatType(nil), c.result.ThreatTypes...)
	out.BlockingDetectors = append([]string(nil), c.result.BlockingDetectors...)
	return &out
}

// withResponseScan returns the scan to record for a request whose response
// was scanned: the input findings unchanged (actions resolved from the input
// DetectorActions, as the threat log would), followed by the response
// findings. blocked is true when the request ended blocked; with response
// findings present that block came from the response side, so the trace
// action is block even when a guard outside the engine (signed thinking,
// rewrite unavailable) refused it. The input result is not modified.
func withResponseScan(input, output *security.ScanResult, blocked bool) *security.ScanResult {
	if output == nil || len(output.Findings) == 0 {
		return input
	}
	merged := security.ScanResult{}
	if input != nil {
		merged = *input
		merged.Findings = make([]security.Finding, 0, len(input.Findings)+len(output.Findings))
		for _, f := range input.Findings {
			if effective, ok := input.DetectorActions[f.DetectorName]; ok {
				f.Action = effective
			}
			merged.Findings = append(merged.Findings, f)
		}
		merged.ThreatTypes = append([]security.ThreatType(nil), input.ThreatTypes...)
		merged.BlockingDetectors = append([]string(nil), input.BlockingDetectors...)
	}
	// Every finding now carries its own action; a shared per-detector map
	// would let one side's strategy relabel the other side's rows.
	merged.DetectorActions = nil
	merged.Findings = append(merged.Findings, output.Findings...)
	for _, t := range output.ThreatTypes {
		if !containsThreatType(merged.ThreatTypes, t) {
			merged.ThreatTypes = append(merged.ThreatTypes, t)
		}
	}
	for _, d := range output.BlockingDetectors {
		if !containsString(merged.BlockingDetectors, d) {
			merged.BlockingDetectors = append(merged.BlockingDetectors, d)
		}
	}
	if output.ThreatScore > merged.ThreatScore {
		merged.ThreatScore = output.ThreatScore
	}
	merged.ShouldBlock = merged.ShouldBlock || output.ShouldBlock || blocked
	merged.Action = security.StrongerAction(merged.Action, output.Action)
	if merged.ShouldBlock {
		merged.Action = security.ActionBlock
	}
	return &merged
}
