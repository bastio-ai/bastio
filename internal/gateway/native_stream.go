package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
)

// mapStreamDeltas gives each independently streamed text/argument channel a
// stable identity. Model output split across network/SSE chunks must be
// scanned and restored as one text, not as unrelated partial strings.
func mapStreamDeltas(raw []byte, transform func(string, string) string) ([]byte, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return raw, false
	}
	changed := false
	rewrite := func(m map[string]json.RawMessage, key, channel string) {
		var text string
		if json.Unmarshal(m[key], &text) != nil {
			return
		}
		next := transform(channel, text)
		if next != text {
			m[key], _ = json.Marshal(next)
			changed = true
		}
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(obj["choices"], &choices) == nil {
		for _, choice := range choices {
			var delta map[string]json.RawMessage
			if json.Unmarshal(choice["delta"], &delta) != nil || delta == nil {
				continue
			}
			prefix := "choice:" + string(choice["index"]) + ":"
			for _, key := range []string{"content", "reasoning", "reasoning_content", "refusal"} {
				rewrite(delta, key, prefix+key)
			}
			var details []map[string]json.RawMessage
			if json.Unmarshal(delta["reasoning_details"], &details) == nil && details != nil {
				for _, detail := range details {
					for _, key := range []string{"text", "summary"} {
						rewrite(detail, key, prefix+"reasoning:"+string(detail["index"])+":"+key)
					}
				}
				delta["reasoning_details"], _ = json.Marshal(details)
			}
			var calls []map[string]json.RawMessage
			if json.Unmarshal(delta["tool_calls"], &calls) == nil {
				for _, call := range calls {
					var fn map[string]json.RawMessage
					if json.Unmarshal(call["function"], &fn) == nil && fn != nil {
						rewrite(fn, "arguments", prefix+"tool:"+string(call["index"]))
						call["function"], _ = json.Marshal(fn)
					}
				}
				delta["tool_calls"], _ = json.Marshal(calls)
			}
			choice["delta"], _ = json.Marshal(delta)
		}
		if changed {
			obj["choices"], _ = json.Marshal(choices)
		}
	}
	var kind string
	_ = json.Unmarshal(obj["type"], &kind)
	if strings.HasPrefix(kind, "response.") && strings.HasSuffix(kind, ".delta") {
		prefix := strings.TrimSuffix(kind, ".delta") + ":" + string(obj["output_index"]) + ":" + string(obj["content_index"])
		rewrite(obj, "delta", prefix)
	}
	if kind == "content_block_delta" {
		var delta map[string]json.RawMessage
		if json.Unmarshal(obj["delta"], &delta) == nil && delta != nil {
			for _, key := range []string{"text", "thinking", "partial_json"} {
				rewrite(delta, key, "block:"+string(obj["index"])+":"+key)
			}
			if changed {
				obj["delta"], _ = json.Marshal(delta)
			}
		}
	}
	if !changed {
		return raw, false
	}
	out, _ := json.Marshal(obj)
	return out, true
}

func (h *Handler) forwardNativeStream(w http.ResponseWriter, r *http.Request, resp *http.Response, metadata *nativeMetadata) ([]byte, int, int, string) {
	profile, _ := security.ProfileFromContext(r.Context())
	tm, hasTM := security.TokenMapFromContext(r.Context())
	needScan := profile != nil && profile.PIIScanResponse && h.security != nil
	needRestore := profile != nil && profile.PIIRestoreResponse && hasTM
	// Complete response policies can block and must run before releasing any
	// bytes. Buffering also closes PII/token boundaries split between events.
	buffered := needScan || needRestore
	if buffered {
		w.Header().Set("X-Bastio-Stream-Buffered", "security-policy")
	}
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRouteError(w, &routeError{502, "streaming not supported"})
		return nil, 0, 0, "error"
	}
	var frames []providers.SSEFrame
	var recorded []byte
	var size, inTokens, outTokens int
	status := "ok"
	terminal := false
	committed := false
	emit := func(frame providers.SSEFrame) error {
		if !committed {
			w.WriteHeader(resp.StatusCode)
			committed = true
		}
		if _, err := w.Write(frame.Raw); err != nil {
			return err
		}
		flusher.Flush()
		if len(recorded) < 64<<10 {
			recorded = append(recorded, frame.Raw...)
		}
		return nil
	}
	err := providers.ReadSSE(r.Context(), resp.Body, func(frame providers.SSEFrame) error {
		in, out := nativeUsage(frame.Data)
		if cost := nativeCost(frame.Data); cost != nil {
			metadata.CostCents = cost
		}
		if in > 0 {
			inTokens = in
		}
		if out > 0 {
			outTokens = out
		}
		var probe struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(frame.Data, &probe)
		if string(frame.Data) == "[DONE]" || frame.Event == "message_stop" || probe.Type == "message_stop" || probe.Type == "response.completed" || probe.Type == "response.failed" || probe.Type == "response.incomplete" {
			terminal = true
		}
		if frame.Event == "error" || probe.Type == "error" || probe.Type == "response.failed" || (len(probe.Error) > 0 && string(probe.Error) != "null") {
			status = "error"
		}
		if buffered {
			size += len(frame.Raw)
			if size > 32<<20 {
				return fmt.Errorf("stream exceeds security buffer limit")
			}
			frames = append(frames, frame)
			return nil
		}
		if h.streamSecrets != nil {
			signedBlocked := false
			data, changed := mapNativeOutputPolicy(frame.Data, h.streamSecrets.Mask, func(text string) string {
				signedBlocked = signedBlocked || h.streamSecrets.Mask(text) != text
				return text
			})
			if signedBlocked {
				return fmt.Errorf("security policy cannot rewrite signed thinking content")
			}
			if changed {
				frame.Raw = providers.RewriteSSEData(frame, data)
			}
		}
		return emit(frame)
	})
	if err != nil {
		if !committed {
			metadata.HTTPStatus = 502
		}
		if !committed && r.Context().Err() == nil {
			writeRouteError(w, err)
		}
		return recorded, inTokens, outTokens, "error"
	}
	endpoint := ProviderEndpoint(r.URL.Path)
	if !terminal && (endpoint == "/messages" || endpoint == "/responses" || endpoint == "/chat/completions") {
		if !committed {
			metadata.HTTPStatus = 502
		}
		if !committed {
			writeRouteError(w, &routeError{502, "provider stream ended before its terminal event"})
		}
		return recorded, inTokens, outTokens, "error"
	}
	if !buffered {
		return recorded, inTokens, outTokens, status
	}
	channels := make(map[string]string)
	for _, frame := range frames {
		mapStreamDeltas(frame.Data, func(key, text string) string { channels[key] += text; return text })
	}
	for key, text := range channels {
		var next, reason string
		var blocked bool
		if strings.HasPrefix(key, "block:") && strings.HasSuffix(key, ":thinking") {
			next, blocked, reason = processSignedThinking(r, h.security, text, tm, needScan)
		} else {
			next, blocked, reason = processAssistantText(r.Context(), h.security, text, tm, needScan, needRestore)
		}
		if blocked {
			metadata.HTTPStatus = 403
			writeRouteError(w, &routeError{403, reason})
			return nil, inTokens, outTokens, "blocked"
		}
		if h.streamSecrets != nil {
			masked := h.streamSecrets.Mask(next)
			if masked != next && strings.HasPrefix(key, "block:") && strings.HasSuffix(key, ":thinking") {
				metadata.HTTPStatus = 403
				writeRouteError(w, &routeError{403, "security policy cannot rewrite signed thinking content"})
				return nil, inTokens, outTokens, "blocked"
			}
			next = masked
		}
		channels[key] = next
	}
	seen := make(map[string]bool)
	for i, frame := range frames {
		hasDelta := false
		data, changed := mapStreamDeltas(frame.Data, func(key, _ string) string {
			hasDelta = true
			if seen[key] {
				return ""
			}
			seen[key] = true
			return channels[key]
		})
		// Completed Responses and Messages content blocks also carry output.
		// Delta events were already processed across chunk boundaries above.
		var probe struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(frame.Data, &probe)
		if !hasDelta && !strings.HasSuffix(probe.Type, ".delta") && probe.Type != "content_block_delta" {
			if next, blocked, reason := h.postProcessNativeResponse(r, data); blocked {
				metadata.HTTPStatus = 403
				writeRouteError(w, &routeError{403, reason})
				return nil, inTokens, outTokens, "blocked"
			} else if next != nil {
				data, changed = next, true
			}
		}
		if changed {
			frames[i].Raw = providers.RewriteSSEData(frame, data)
		}
	}
	// Nothing is committed until every frame has passed the complete policy.
	for _, frame := range frames {
		if err := emit(frame); err != nil {
			return recorded, inTokens, outTokens, "error"
		}
	}
	return recorded, inTokens, outTokens, status
}
