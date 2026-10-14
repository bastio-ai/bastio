package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type openAIFunctionTool struct {
	Type     string         `json:"type"`
	Function ToolDefinition `json:"function"`
}
type openAIFunctionArguments struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type openAIFunctionCall struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Function openAIFunctionArguments `json:"function"`
}

// Responses supports OpenAI's current function-capable model families. The
// ordinary chat/gateway path remains unchanged. No remote tools run here.
func (c *OpenAIClient) responsesTools(ctx context.Context, req *ChatRequest, key string) (*ChatResponse, error) {
	input := []any{}
	for _, m := range req.Messages {
		for _, state := range m.ProviderState {
			input = append(input, state)
		}
		if m.Role == "tool" {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
			continue
		}
		if m.Content != "" || len(m.Images) > 0 {
			role := m.Role
			if role == "system" {
				role = "developer"
			}
			var content any = m.Content
			if len(m.Images) > 0 {
				parts := []any{map[string]any{"type": "input_text", "text": m.Content}}
				for _, img := range m.Images {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": "data:" + img.MimeType + ";base64," + img.Data})
				}
				content = parts
			}
			input = append(input, map[string]any{"role": role, "content": content})
		}
		for _, call := range m.ToolCalls {
			input = append(input, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(call.Arguments)})
		}
	}
	tools := []any{}
	for _, t := range req.Tools {
		tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters, "strict": false})
	}
	body := map[string]any{"model": req.Model, "input": input, "tools": tools, "parallel_tool_calls": false, "store": false, "include": []string{"reasoning.encrypted_content"}, "max_output_tokens": req.MaxTokens}
	if req.ReasoningEffort != "" {
		body["reasoning"] = map[string]any{"effort": req.ReasoningEffort}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/responses", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, &ProviderError{Provider: c.Name(), Status: resp.StatusCode, Body: raw, Headers: resp.Header.Clone()}
	}
	var decoded struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status string `json:"status"`
		Output []struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("invalid Responses output")
	}
	var state struct {
		Output []json.RawMessage `json:"output"`
	}
	_ = json.Unmarshal(raw, &state)
	result := &ChatResponse{ID: decoded.ID, Model: decoded.Model, Role: "assistant", FinishReason: decoded.Status, InputTokens: decoded.Usage.Input, OutputTokens: decoded.Usage.Output, Raw: raw}
	for _, item := range state.Output {
		var kind struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(item, &kind) == nil && kind.Type == "reasoning" {
			result.ProviderState = append(result.ProviderState, item)
		}
	}
	for _, item := range decoded.Output {
		if item.Type == "function_call" {
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.Arguments)})
		}
		if item.Type == "message" {
			for _, part := range item.Content {
				if part.Type == "output_text" {
					result.Content += part.Text
				}
			}
		}
	}
	return result, nil
}
