package gateway

import (
	"encoding/json"
	"strings"

	"github.com/bastio-ai/bastio/internal/security"
)

type textTransform func(string) string

// mapContent visits textual content while preserving images, file references,
// cache controls, signatures, tool IDs and other provider metadata verbatim.
func mapContent(raw json.RawMessage, transform textTransform) (json.RawMessage, bool) {
	return mapContentPolicy(raw, transform, transform)
}

func mapContentPolicy(raw json.RawMessage, transform, thinking textTransform) (json.RawMessage, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		next := transform(text)
		if next == text {
			return raw, false
		}
		out, _ := json.Marshal(next)
		return out, true
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil && parts != nil {
		changed := false
		for i, part := range parts {
			next, ch := mapContentPolicy(part, transform, thinking)
			parts[i], changed = next, changed || ch
		}
		if !changed {
			return raw, false
		}
		out, _ := json.Marshal(parts)
		return out, true
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return raw, false
	}
	var kind string
	_ = json.Unmarshal(obj["type"], &kind)
	changed := false
	for _, key := range []string{"text", "content", "output", "arguments", "refusal", "thinking", "reasoning", "summary"} {
		value, exists := obj[key]
		if !exists {
			continue
		}
		apply := transform
		if kind == "thinking" && key == "thinking" {
			apply = thinking
		}
		next, ch := mapContentPolicy(value, apply, thinking)
		obj[key], changed = next, changed || ch
	}
	// Claude tool-use input is arbitrary user data, unlike the outer
	// provider envelope where changing IDs or type discriminators breaks APIs.
	if kind == "tool_use" {
		if input, exists := obj["input"]; exists {
			next, ch := mapDataStrings(input, transform)
			obj["input"], changed = next, changed || ch
		}
	}
	// Plain-text document sources are scannable; base64 sources are opaque.
	if kind == "document" {
		var source map[string]json.RawMessage
		if json.Unmarshal(obj["source"], &source) == nil {
			var sourceType string
			_ = json.Unmarshal(source["type"], &sourceType)
			if sourceType == "text" {
				next, ch := mapContent(source["data"], transform)
				if ch {
					source["data"] = next
					obj["source"], _ = json.Marshal(source)
					changed = true
				}
			}
		}
	}
	if !changed {
		return raw, false
	}
	out, _ := json.Marshal(obj)
	return out, true
}

func mapDataStrings(raw json.RawMessage, transform textTransform) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil && obj != nil {
		changed := false
		for k, v := range obj {
			next, ch := mapDataStrings(v, transform)
			obj[k], changed = next, changed || ch
		}
		if !changed {
			return raw, false
		}
		out, _ := json.Marshal(obj)
		return out, true
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil && list != nil {
		changed := false
		for i, v := range list {
			next, ch := mapDataStrings(v, transform)
			list[i], changed = next, changed || ch
		}
		if !changed {
			return raw, false
		}
		out, _ := json.Marshal(list)
		return out, true
	}
	return mapContent(raw, transform)
}

// mapInput covers Chat Completions, Claude Messages, Responses, legacy
// completions, embeddings and reranking without rewriting instructions.
func mapInput(body []byte, transform textTransform) ([]byte, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil || obj == nil {
		return body, false
	}
	_, googleContents := obj["contents"]
	var googleContent map[string]json.RawMessage
	_ = json.Unmarshal(obj["content"], &googleContent)
	if googleContents || googleContent["parts"] != nil {
		return mapGoogleContent(body, transform)
	}
	changed := false
	// Claude batches carry full Messages requests under requests[].params.
	var batch []map[string]json.RawMessage
	if json.Unmarshal(obj["requests"], &batch) == nil && batch != nil {
		for _, request := range batch {
			if params, exists := request["params"]; exists {
				next, ch := mapInput(params, transform)
				request["params"], changed = next, changed || ch
			} else if request["content"] != nil || request["contents"] != nil {
				raw, _ := json.Marshal(request)
				next, ch := mapGoogleContent(raw, transform)
				if ch {
					_ = json.Unmarshal(next, &request)
					changed = true
				}
			}
		}
		if changed {
			obj["requests"], _ = json.Marshal(batch)
		}
	}
	for _, key := range []string{"messages", "input"} {
		value, exists := obj[key]
		if !exists {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(value, &items) != nil {
			next, ch := mapContent(value, transform)
			obj[key], changed = next, changed || ch
			continue
		}
		listChanged := false
		for i, raw := range items {
			var item map[string]json.RawMessage
			if json.Unmarshal(raw, &item) == nil && item != nil {
				var role string
				_ = json.Unmarshal(item["role"], &role)
				if role == "system" || role == "developer" {
					continue
				}
				if key == "messages" && role != "user" && role != "tool" && role != "assistant" {
					continue
				}
			}
			// Restored assistant text and tool arguments can return as history.
			// Re-sanitize ordinary data; signed thinking must remain reusable.
			var next json.RawMessage
			var ch bool
			if item != nil {
				next, ch = mapNativeOutputPolicy(raw, transform, func(text string) string { return text })
			} else {
				next, ch = mapContent(raw, transform)
			}
			items[i], listChanged = next, listChanged || ch
		}
		if listChanged {
			obj[key], _ = json.Marshal(items)
			changed = true
		}
	}
	for _, key := range []string{"state", "questions"} {
		if raw, exists := obj[key]; exists {
			next, ch := mapDataStrings(raw, transform)
			obj[key], changed = next, changed || ch
		}
	}
	for _, key := range []string{"prompt", "query", "documents"} {
		if raw, exists := obj[key]; exists {
			next, ch := mapContent(raw, transform)
			obj[key], changed = next, changed || ch
		}
	}
	if !changed {
		return body, false
	}
	out, _ := json.Marshal(obj)
	return out, true
}

func extractInputText(body []byte) string {
	var texts []string
	mapInput(body, func(text string) string {
		if text != "" {
			texts = append(texts, text)
		}
		return text
	})
	return strings.Join(texts, "\n")
}

func sanitizeInput(body []byte, engine *security.Engine, action security.Action, tm *security.TokenMap) ([]byte, bool) {
	if engine == nil {
		return body, false
	}
	return mapInput(body, func(text string) string { return engine.SanitizeWithMap(text, action, tm) })
}
