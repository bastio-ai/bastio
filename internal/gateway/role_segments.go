package gateway

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
)

// roleSegments holds request text the end user did not type, split by
// provenance so each part is scanned with the right security role.
//
//   - tool: tool and function results (OpenAI role tool/function,
//     Claude tool_result blocks, Responses function_call_output, Gemini
//     functionResponse). This is where indirect prompt injection arrives
//     in agent and MCP traffic.
//   - assistant: assistant turns replayed as history. The client
//     supplies them, so they can be forged, and they can repeat injected
//     text from an earlier tool result.
//
// System and developer messages are deliberately not collected; see
// the comment on extractRoleSegments.
//
// Every leaf collected here is also visited by the rewrite path
// (mapInput, mapGoogleContent), so a mask or tokenize decision from these
// scans is always applied to the outbound body.
type roleSegments struct {
	tool      []string
	assistant []string
}

// segmentScan is one supplementary scan of non-user request text.
type segmentScan struct {
	role      string
	text      string
	detectors []string
}

// Detector subsets for supplementary scans. Strategies and thresholds
// come from the profile; these only choose which steps apply.
//
// On every request route, tool results and replayed history get the
// indirect-injection guard plus secrets and PII handling; the user scan
// covers only what the end user sent (see userTextExcluding). The direct
// injection and jailbreak detectors are left out of these parts: they are
// tuned for someone typing at the model, and documents, web pages and API
// responses routinely contain imperative or security-themed prose.
func untrustedDetectors() []string {
	return security.AgentContextDetectors()
}

func roleOnlyDetectors() []string {
	return []string{"indirect_injection"}
}

func (s roleSegments) scans(detectors []string) []segmentScan {
	var out []segmentScan
	if len(s.tool) > 0 {
		out = append(out, segmentScan{role: security.RoleTool, text: strings.Join(s.tool, "\n"), detectors: detectors})
	}
	if len(s.assistant) > 0 {
		out = append(out, segmentScan{role: security.RoleAssistantHistory, text: strings.Join(s.assistant, "\n"), detectors: detectors})
	}
	return out
}

// nativeScanInputs splits a provider-native request body into the user
// text (full input pipeline) and the role-tagged supplementary scans.
func nativeScanInputs(body []byte) (string, []segmentScan) {
	segs := extractRoleSegments(body)
	return userTextExcluding(inputTexts(body), segs), segs.scans(untrustedDetectors())
}

// batchScanInputs is nativeScanInputs for one batch item, whose visited
// text also includes Claude signed thinking and Gemini request shapes.
func batchScanInputs(raw []byte, p providers.Provider) (string, []segmentScan) {
	segs := extractRoleSegments(raw)
	return userTextExcluding(batchInputTexts(raw, p), segs), segs.scans(untrustedDetectors())
}

// userTextExcluding returns the request text that did not come from a
// tool result or a replayed assistant turn: all is every text field the
// rewrite path visits, in order; the tool and assistant leaves found by
// extractRoleSegments are removed once per occurrence. Anything the
// classifier does not recognise stays in the user scan, so an unknown
// shape keeps the full input pipeline rather than losing coverage.
func userTextExcluding(all []string, segs roleSegments) string {
	if len(segs.tool) == 0 && len(segs.assistant) == 0 {
		return strings.Join(all, "\n")
	}
	skip := make(map[string]int, len(segs.tool)+len(segs.assistant))
	for _, t := range segs.tool {
		skip[t]++
	}
	for _, t := range segs.assistant {
		skip[t]++
	}
	kept := make([]string, 0, len(all))
	for _, t := range all {
		if skip[t] > 0 {
			skip[t]--
			continue
		}
		kept = append(kept, t)
	}
	return strings.Join(kept, "\n")
}

func joinSegmentText(segs []segmentScan) string {
	var parts []string
	for _, seg := range segs {
		if seg.text != "" {
			parts = append(parts, seg.text)
		}
	}
	return strings.Join(parts, "\n")
}

// mayHoldNonUserContent is a byte-level pre-check so user-only requests
// skip the JSON walk. A false positive only costs one parse. Gemini
// bodies ("contents") are always walked because their history role is
// "model", which also appears as an ordinary request key elsewhere.
func mayHoldNonUserContent(raw []byte) bool {
	return bytes.Contains(raw, []byte(`"assistant"`)) ||
		bytes.Contains(raw, []byte(`"tool`)) ||
		bytes.Contains(raw, []byte(`"function`)) ||
		bytes.Contains(raw, []byte(`_output"`)) ||
		bytes.Contains(raw, []byte(`"contents"`)) ||
		bytes.Contains(raw, []byte(`"parts"`))
}

// extractRoleSegmentsFromMessages classifies an OpenAI- or Claude-shape
// messages array.
func extractRoleSegmentsFromMessages(messages []json.RawMessage) roleSegments {
	var segs roleSegments
	for _, raw := range messages {
		if !mayHoldNonUserContent(raw) {
			continue
		}
		segs.classify(raw)
	}
	return segs
}

// extractRoleSegments classifies a provider-native request body: Chat
// Completions and Claude Messages (messages), Responses (input), Gemini
// (contents) and Claude batch creates (requests[].params).
//
// System and developer content (messages with those roles, Claude's
// top-level system, Gemini systemInstruction, Responses instructions)
// is not scanned. It is operator-authored, so injection and jailbreak
// rules misfire on ordinary instructions there. The rewrite path
// (mapInput) also leaves it untouched to keep instructions and
// prompt-cache prefixes byte-identical, so a mask or tokenize decision
// on it could not be honored and the trace would claim a rewrite that
// never happened.
func extractRoleSegments(body []byte) roleSegments {
	var segs roleSegments
	if !mayHoldNonUserContent(body) {
		return segs
	}
	segs.walkBody(body, 0)
	return segs
}

func (s *roleSegments) walkBody(body []byte, depth int) {
	if depth > 2 {
		return
	}
	var req struct {
		Messages []json.RawMessage `json:"messages"`
		Input    json.RawMessage   `json:"input"`
		Contents []json.RawMessage `json:"contents"`
		Requests []struct {
			Params  json.RawMessage `json:"params"`
			Request json.RawMessage `json:"request"`
		} `json:"requests"`
	}
	if json.Unmarshal(body, &req) != nil {
		return
	}
	for _, m := range req.Messages {
		s.classify(m)
	}
	var items []json.RawMessage
	if len(req.Input) > 0 && json.Unmarshal(req.Input, &items) == nil {
		for _, item := range items {
			s.classify(item)
		}
	}
	for _, c := range req.Contents {
		s.classify(c)
	}
	for _, r := range req.Requests {
		if len(r.Params) > 0 {
			s.walkBody(r.Params, depth+1)
		}
		if len(r.Request) > 0 {
			s.walkBody(r.Request, depth+1)
		}
	}
}

// classify routes one message or input item into a bucket.
func (s *roleSegments) classify(raw json.RawMessage) {
	var item struct {
		Role      string            `json:"role"`
		Type      string            `json:"type"`
		Content   json.RawMessage   `json:"content"`
		Output    json.RawMessage   `json:"output"`
		Arguments json.RawMessage   `json:"arguments"`
		Parts     []json.RawMessage `json:"parts"`
		ToolCalls []struct {
			Function json.RawMessage `json:"function"`
		} `json:"tool_calls"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	switch {
	case item.Role == "tool" || item.Role == "function":
		s.tool = collectText(s.tool, item.Content)
		s.collectGeminiParts(item.Parts, true)
	case item.Type == "function_call_output" || item.Type == "custom_tool_call_output":
		s.tool = collectText(s.tool, item.Output)
	case item.Type == "function_call":
		// Responses: a replayed model tool call; its arguments are history.
		s.assistant = collectText(s.assistant, item.Arguments)
	case item.Role == "assistant" || item.Role == "model":
		s.assistant = collectText(s.assistant, item.Content)
		for _, call := range item.ToolCalls {
			s.assistant = collectText(s.assistant, call.Function)
		}
		s.collectGeminiParts(item.Parts, false)
	case item.Role == "user" || item.Role == "":
		// Claude returns tool output inside user turns as tool_result
		// blocks; Gemini as functionResponse parts.
		var blocks []struct {
			Type    string          `json:"type"`
			Content json.RawMessage `json:"content"`
		}
		if len(item.Content) > 0 && item.Content[0] == '[' && json.Unmarshal(item.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "tool_result" {
					s.tool = collectText(s.tool, b.Content)
				}
			}
		}
		s.collectGeminiParts(item.Parts, true)
	}
}

// collectGeminiParts reads Gemini parts. functionResponse payloads are
// tool output; plain text parts count only for model/function turns
// (user text is covered by the user scan). Thought parts are skipped.
func (s *roleSegments) collectGeminiParts(parts []json.RawMessage, toolTurn bool) {
	for _, raw := range parts {
		var part struct {
			Text             string          `json:"text"`
			Thought          bool            `json:"thought"`
			FunctionResponse json.RawMessage `json:"functionResponse"`
			FunctionCall     json.RawMessage `json:"functionCall"`
		}
		if json.Unmarshal(raw, &part) != nil {
			continue
		}
		if len(part.FunctionCall) > 0 && !toolTurn {
			// A replayed model function call; args are history.
			var fc struct {
				Args json.RawMessage `json:"args"`
			}
			if json.Unmarshal(part.FunctionCall, &fc) == nil && len(fc.Args) > 0 {
				mapDataStrings(fc.Args, func(t string) string {
					if t != "" {
						s.assistant = append(s.assistant, t)
					}
					return t
				})
			}
			continue
		}
		if len(part.FunctionResponse) > 0 {
			var fr struct {
				Response json.RawMessage `json:"response"`
			}
			if json.Unmarshal(part.FunctionResponse, &fr) == nil && len(fr.Response) > 0 {
				mapDataStrings(fr.Response, func(t string) string {
					if t != "" {
						s.tool = append(s.tool, t)
					}
					return t
				})
			}
			continue
		}
		if part.Text == "" || part.Thought {
			continue
		}
		if toolTurn {
			// A role=user Gemini turn's text is user input, already scanned.
			continue
		}
		s.assistant = append(s.assistant, part.Text)
	}
}

// collectText appends every text field of a content value (string,
// parts array or block) without visiting signed thinking, which the
// rewrite path never changes.
func collectText(dst []string, raw json.RawMessage) []string {
	if len(raw) == 0 {
		return dst
	}
	collect := func(t string) string {
		if t != "" {
			dst = append(dst, t)
		}
		return t
	}
	mapContentPolicy(raw, collect, func(t string) string { return t })
	return dst
}

// mergeScanResults folds src into dst. Block wins, then the strongest
// rewrite; findings, threat types and blocking detectors accumulate.
func mergeScanResults(dst, src *security.ScanResult) *security.ScanResult {
	if src == nil {
		return dst
	}
	if dst == nil {
		return src
	}
	dst.Findings = append(dst.Findings, src.Findings...)
	for _, t := range src.ThreatTypes {
		if !containsThreatType(dst.ThreatTypes, t) {
			dst.ThreatTypes = append(dst.ThreatTypes, t)
		}
	}
	for _, d := range src.BlockingDetectors {
		if !containsString(dst.BlockingDetectors, d) {
			dst.BlockingDetectors = append(dst.BlockingDetectors, d)
		}
	}
	if len(src.DetectorActions) > 0 {
		if dst.DetectorActions == nil {
			dst.DetectorActions = make(map[string]security.Action, len(src.DetectorActions))
		}
		for name, action := range src.DetectorActions {
			dst.DetectorActions[name] = security.StrongerAction(dst.DetectorActions[name], action)
		}
	}
	if src.ThreatScore > dst.ThreatScore {
		dst.ThreatScore = src.ThreatScore
	}
	dst.ShouldBlock = dst.ShouldBlock || src.ShouldBlock
	dst.RewriteUnavailable = dst.RewriteUnavailable || src.RewriteUnavailable
	for _, d := range src.EncodedMasked {
		if !containsString(dst.EncodedMasked, d) {
			dst.EncodedMasked = append(dst.EncodedMasked, d)
		}
	}
	dst.Action = security.StrongerAction(dst.Action, src.Action)
	dst.ScanDuration += src.ScanDuration
	return dst
}

func containsThreatType(list []security.ThreatType, t security.ThreatType) bool {
	for _, v := range list {
		if v == t {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
