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
	changed := false
	// Claude batches carry full Messages requests under requests[].params.
	var batch []map[string]json.RawMessage
	if json.Unmarshal(obj["requests"], &batch) == nil && batch != nil {
		for _, request := range batch {
			if params, exists := request["params"]; exists {
				next, ch := mapInput(params, transform)
				request["params"], changed = next, changed || ch
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
				var role, kind string
				_ = json.Unmarshal(item["role"], &role)
				_ = json.Unmarshal(item["type"], &kind)
				if role == "system" || role == "developer" || role == "assistant" {
					continue
				}
				if key == "messages" && role != "user" && role != "tool" {
					continue
				}
				// Responses supports role-less function_call_output and text parts.
				if key == "input" && role == "" && kind == "function_call" {
					continue
				}
			}
			next, ch := mapContent(raw, transform)
			items[i], listChanged = next, listChanged || ch
		}
		if listChanged {
			obj[key], _ = json.Marshal(items)
			changed = true
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
