package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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
	// observed is pass-through stream text awaiting the scan that runs
	// after the stream ended (recordTrace runs it); truncated notes that
	// the text hit maxObservedStreamText.
	observed        []string
	truncated       bool
	blockedInStream bool
}

// maxObservedStreamText bounds the pass-through stream text kept for the
// scan after the stream ends.
const maxObservedStreamText = 256 << 10

// streamObserver accumulates the assistant text of a stream that passes
// through unbuffered, per channel as the hold-back path does, so text split
// across chunks is scanned as one string. Gemini frames are incremental and
// append to one channel. Other frames without delta channels (Responses
// done/completed events, Claude message snapshots) repeat text the deltas
// already carried: they are ignored once any delta was seen, and otherwise
// only the most complete one (the longest) is kept, so the text is taken
// once and not once per event type.
type streamObserver struct {
	channels  map[string]*strings.Builder
	order     []string
	size      int
	truncated bool
	sawDelta  bool
	full      string
}

func newStreamObserver() *streamObserver {
	return &streamObserver{channels: make(map[string]*strings.Builder)}
}

func (o *streamObserver) addFrame(data []byte) {
	found := false
	mapStreamDeltas(data, func(key, text string) string {
		found = true
		o.append(key, text)
		return text
	})
	if found {
		o.sawDelta = true
		return
	}
	var probe struct {
		Candidates json.RawMessage `json:"candidates"`
	}
	_ = json.Unmarshal(data, &probe)
	if len(probe.Candidates) > 0 {
		mapNativeOutput(data, func(text string) string {
			o.append("candidates", text)
			return text
		})
		return
	}
	if o.sawDelta {
		return
	}
	// A snapshot frame can reach the same text by more than one key.
	var parts []string
	seen := make(map[string]bool)
	mapNativeOutput(data, func(text string) string {
		if text != "" && !seen[text] {
			seen[text] = true
			parts = append(parts, text)
		}
		return text
	})
	if text := strings.Join(parts, "\n"); len(text) > len(o.full) {
		if len(text) > maxObservedStreamText {
			text = text[:maxObservedStreamText]
			o.truncated = true
		}
		o.full = text
	}
}

// texts returns the accumulated text, one string per channel.
func (o *streamObserver) texts() []string {
	if o.sawDelta || len(o.order) > 0 || o.full == "" {
		out := make([]string, 0, len(o.order))
		for _, key := range o.order {
			out = append(out, o.channels[key].String())
		}
		return out
	}
	if room := maxObservedStreamText - o.size; len(o.full) > room {
		o.full = o.full[:max(room, 0)]
		o.truncated = true
	}
	return []string{o.full}
}

func (o *streamObserver) append(key, text string) {
	if text == "" {
		return
	}
	if room := maxObservedStreamText - o.size; len(text) > room {
		text = text[:max(room, 0)]
		o.truncated = true
		if text == "" {
			return
		}
	}
	b, ok := o.channels[key]
	if !ok {
		b = &strings.Builder{}
		o.channels[key] = b
		o.order = append(o.order, key)
	}
	b.WriteString(text)
	o.size += len(text)
}

// observe queues the accumulated stream text for the scan after the stream.
func (c *responseFindings) observe(o *streamObserver) {
	if c == nil || o == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, text := range o.texts() {
		if text != "" {
			c.observed = append(c.observed, text)
		}
	}
	c.truncated = c.truncated || o.truncated
}

// takeObserved returns and clears the queued stream text.
func (c *responseFindings) takeObserved() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.observed
	c.observed = nil
	return out
}

func (c *responseFindings) scanTruncated() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.truncated
}

// markBlockedInStream notes that a held-back stream was blocked after its
// headers were committed (keep-alives): the client got HTTP 200 and an
// in-stream error event, which the trace records as such.
func (c *responseFindings) markBlockedInStream() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blockedInStream = true
}

func (c *responseFindings) wasBlockedInStream() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blockedInStream
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
