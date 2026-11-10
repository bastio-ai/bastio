package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
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
	// Hold the stream back only when the scan can change what the client
	// receives (block or rewrite). Observe-only output steps (warn, log_only)
	// pass through event by event; their text is scanned after the stream.
	needScan := h.security != nil && outputScanCanChangeResponse(profile)
	needRestore := profile != nil && profile.PIIRestoreResponse && hasTM
	monitorOnly := gatewaypolicy.From(r.Context()).MonitorOnly
	// Complete response policies can block and must run before releasing any
	// bytes. Buffering also closes PII/token boundaries split between events.
	buffered := (needScan || needRestore) && !monitorOnly
	if buffered {
		w.Header().Set("X-Bastio-Stream-Buffered", "security-policy")
	}
	// Monitor mode scans the recorded response itself (forwardNativeAPI).
	var observer *streamObserver
	if !buffered && !monitorOnly && h.security != nil && hasOutputSteps(profile) {
		observer = newStreamObserver()
		defer func() { responseFindingsFrom(r.Context()).observe(observer) }()
	}
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRouteError(w, &routeError{status: 502, message: "streaming not supported"})
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
	endpoint := ProviderEndpoint(r.URL.Path)
	// A held-back stream still generating after the grace period commits
	// its headers and sends keep-alives; once committed, a block or a
	// failure ends it with an in-stream error event instead of a status.
	var keepAlive *holdBackKeepAlive
	if buffered {
		keepAlive = h.startHoldBackKeepAlive(r.Context(), w, flusher, resp.StatusCode)
	}
	endHeld := func(code, message string) {
		_, _ = w.Write(inStreamErrorEvent(endpoint, code, message))
		flusher.Flush()
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
		if h.streamSecrets != nil && !gatewaypolicy.From(r.Context()).MonitorOnly {
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
				frame.Data = data // the scan after the stream sees what the client got
			}
		}
		if err := emit(frame); err != nil {
			return err
		}
		if observer != nil {
			observer.addFrame(frame.Data)
		}
		return nil
	})
	if keepAlive != nil {
		committed = keepAlive.halt()
	}
	if err != nil {
		if !committed {
			metadata.HTTPStatus = 502
		}
		if r.Context().Err() == nil {
			if !committed {
				writeRouteError(w, err)
			} else if buffered {
				endHeld("gateway_error", err.Error())
			}
		}
		return recorded, inTokens, outTokens, "error"
	}
	if !terminal && (endpoint == "/messages" || endpoint == "/responses" || endpoint == "/chat/completions") {
		if !committed {
			metadata.HTTPStatus = 502
			writeRouteError(w, &routeError{status: 502, message: "provider stream ended before its terminal event"})
		} else if buffered {
			endHeld("gateway_error", "provider stream ended before its terminal event")
		}
		return recorded, inTokens, outTokens, "error"
	}
	if !buffered {
		return recorded, inTokens, outTokens, status
	}
	payloads := make([][]byte, len(frames))
	for i, frame := range frames {
		payloads[i] = frame.Data
	}
	// Observe-only steps are scanned too: they cannot change the release,
	// and the trace then records them as it does for unbuffered streams.
	released, blocked, reason := h.scanHeldBackStream(r, payloads, tm, h.security != nil && hasOutputSteps(profile), needRestore)
	if blocked {
		if committed {
			// Headers went out with the keep-alives: end the stream with
			// an error event, no model content and no terminal event.
			responseFindingsFrom(r.Context()).markBlockedInStream()
			endHeld("response_blocked", reason)
			return nil, inTokens, outTokens, "blocked"
		}
		metadata.HTTPStatus = 403
		writeRouteError(w, &routeError{status: 403, message: reason})
		return nil, inTokens, outTokens, "blocked"
	}
	for i, data := range released {
		if data != nil {
			frames[i].Raw = providers.RewriteSSEData(frames[i], data)
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

// scanHeldBackStream runs the complete response policy over the SSE data
// payloads of a held-back stream: each delta channel is scanned and
// restored as one text (the first event of a channel carries its full
// processed text, later ones carry ""), and events without deltas are
// post-processed whole. It returns the rewritten payload per event (nil
// when unchanged), or blocked with the reason. Shared by the native and the
// normalized stream paths.
func (h *Handler) scanHeldBackStream(r *http.Request, payloads [][]byte, tm *security.TokenMap, needScan, needRestore bool) ([][]byte, bool, string) {
	monitorOnly := gatewaypolicy.From(r.Context()).MonitorOnly
	channels := make(map[string]string)
	for _, data := range payloads {
		mapStreamDeltas(data, func(key, text string) string { channels[key] += text; return text })
	}
	// Done/completed events repeat the channel text; the memo lets each
	// distinct text be scanned once (valid because both use the same
	// needScan/needRestore as postProcessNativeResponse).
	memo := make(map[string]processedText)
	for key, text := range channels {
		var next, reason string
		var blocked bool
		signed := strings.HasPrefix(key, "block:") && strings.HasSuffix(key, ":thinking")
		if signed {
			next, blocked, reason = processSignedThinking(r, h.security, text, tm, needScan)
		} else {
			next, blocked, reason = processAssistantText(r.Context(), h.security, text, tm, needScan, needRestore)
			memo[text] = processedText{next, blocked, reason}
		}
		if blocked {
			return nil, true, reason
		}
		if h.streamSecrets != nil && !monitorOnly {
			masked := h.streamSecrets.Mask(next)
			if masked != next && signed {
				return nil, true, "security policy cannot rewrite signed thinking content"
			}
			next = masked
		}
		channels[key] = next
	}
	out := make([][]byte, len(payloads))
	seen := make(map[string]bool)
	for i, payload := range payloads {
		hasDelta := false
		data, changed := mapStreamDeltas(payload, func(key, _ string) string {
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
		_ = json.Unmarshal(payload, &probe)
		if !hasDelta && !strings.HasSuffix(probe.Type, ".delta") && probe.Type != "content_block_delta" {
			if next, blocked, reason := h.postProcessNativeResponseMemo(r, data, memo); blocked {
				return nil, true, reason
			} else if next != nil {
				data, changed = next, true
			}
		}
		if changed {
			out[i] = data
		}
	}
	return out, false, ""
}
