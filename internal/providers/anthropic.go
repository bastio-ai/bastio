package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnthropicClient implements the Client interface for Anthropic.
type AnthropicClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewAnthropicClient creates a new Anthropic client.
func NewAnthropicClient() *AnthropicClient {
	return &AnthropicClient{
		baseURL:    "https://api.anthropic.com/v1",
		httpClient: &http.Client{},
	}
}

func (c *AnthropicClient) Name() Provider { return ProviderAnthropic }

// Chat sends a non-streaming Anthropic Messages request.
func (c *AnthropicClient) Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error) {
	var body []byte
	if req.Raw != nil {
		var m map[string]any
		if err := json.Unmarshal(req.Raw, &m); err == nil {
			m["stream"] = false
			body, _ = json.Marshal(m)
		}
	}
	if body == nil {
		anthropicReq := toAnthropicRequest(req)
		anthropicReq.Stream = false
		var err error
		body, err = json.Marshal(anthropicReq)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var anthropicResp anthropicMessagesResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	result := &ChatResponse{
		ID:           anthropicResp.ID,
		Model:        anthropicResp.Model,
		Role:         anthropicResp.Role,
		FinishReason: anthropicResp.StopReason,
		Raw:          respBody,
	}

	// Extract text content
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			result.Content += block.Text
		}
	}

	result.InputTokens = anthropicResp.Usage.InputTokens
	result.OutputTokens = anthropicResp.Usage.OutputTokens

	return result, nil
}

// ChatStream sends a streaming Anthropic Messages request.
func (c *AnthropicClient) ChatStream(ctx context.Context, req *ChatRequest, apiKey string) (<-chan StreamChunk, error) {
	var body []byte
	if req.Raw != nil {
		var m map[string]any
		if err := json.Unmarshal(req.Raw, &m); err == nil {
			m["stream"] = true
			body, _ = json.Marshal(m)
		}
	}
	if body == nil {
		anthropicReq := toAnthropicRequest(req)
		anthropicReq.Stream = true
		var err error
		body, err = json.Marshal(anthropicReq)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, string(body))
	}

	// Anthropic uses "event: message_stop" as the done signal, but we pass raw chunks
	return ReadSSEStream(ctx, resp.Body, "message_stop"), nil
}

// Anthropic-specific types

type anthropicMessagesRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	Stream      bool               `json:"stream"`
}

// anthropicMessage carries either a plain-string content (no images)
// or a content-blocks array (multimodal). Anthropic's API accepts
// both shapes; using any keeps the JSON output valid in either form.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// anthropicTextBlock and anthropicImageBlock are the multimodal
// content blocks. Anthropic's spec also has tool_use, tool_result,
// document, and a few others — we don't need them for chat.
type anthropicTextBlock struct {
	Type string `json:"type"` // always "text"
	Text string `json:"text"`
}

type anthropicImageBlock struct {
	Type   string                    `json:"type"` // always "image"
	Source anthropicImageBlockSource `json:"source"`
}

type anthropicImageBlockSource struct {
	Type      string `json:"type"`       // always "base64"
	MediaType string `json:"media_type"` // image/png, image/jpeg, etc.
	Data      string `json:"data"`       // base64-encoded raw bytes
}

type anthropicMessagesResponse struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Role       string `json:"role"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func toAnthropicRequest(req *ChatRequest) *anthropicMessagesRequest {
	var system string
	var msgs []anthropicMessage

	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		// No images → plain string content. Most messages.
		if len(m.Images) == 0 {
			msgs = append(msgs, anthropicMessage{Role: m.Role, Content: m.Content})
			continue
		}
		// Multimodal: content is a blocks array. Per Anthropic's
		// docs, image blocks should come BEFORE text for best
		// results, but ordering after text also works.
		blocks := make([]any, 0, 1+len(m.Images))
		for _, img := range m.Images {
			blocks = append(blocks, anthropicImageBlock{
				Type: "image",
				Source: anthropicImageBlockSource{
					Type:      "base64",
					MediaType: img.MimeType,
					Data:      img.Data,
				},
			})
		}
		if m.Content != "" {
			blocks = append(blocks, anthropicTextBlock{Type: "text", Text: m.Content})
		}
		msgs = append(msgs, anthropicMessage{Role: m.Role, Content: blocks})
	}

	maxTokens := 4096
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	return &anthropicMessagesRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   maxTokens,
		System:      system,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}
}
