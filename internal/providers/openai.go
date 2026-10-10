package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// OpenAIClient implements the Client interface for OpenAI.
type OpenAIClient struct {
	baseURL    string
	httpClient *http.Client
	provider   Provider
}

// NewOpenAIClient creates a new OpenAI client.
func NewOpenAIClient() *OpenAIClient {
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return NewOpenAIClientWithBaseURL(base)
}

func NewOpenAIClientWithBaseURL(base string) *OpenAIClient {
	return &OpenAIClient{baseURL: strings.TrimRight(base, "/"), httpClient: providerHTTPClient()}
}

func (c *OpenAIClient) Name() Provider {
	if c.provider != "" {
		return c.provider
	}
	return ProviderOpenAI
}

func (c *OpenAIClient) outboundModel(model string) string {
	if c.Name() == ProviderOpenRouter {
		return strings.TrimPrefix(model, "openrouter/")
	}
	return strings.TrimPrefix(model, "openai/")
}

// Chat sends a non-streaming chat completion request.
func (c *OpenAIClient) Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error) {
	copyReq := *req
	copyReq.Model = c.outboundModel(req.Model)
	req = &copyReq
	// Use raw body if available (pass-through proxy mode)
	var body []byte
	if req.Raw != nil {
		// Ensure stream is false
		var m map[string]json.RawMessage
		if err := json.Unmarshal(req.Raw, &m); err == nil {
			m["stream"] = json.RawMessage(`false`)
			if req.Model != "" {
				m["model"], _ = json.Marshal(c.outboundModel(req.Model))
			}
			body, _ = json.Marshal(m)
		}
	}
	if body == nil {
		openaiReq := toOpenAIRequest(req)
		openaiReq.Stream = false
		var err error
		body, err = json.Marshal(openaiReq)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	forwardRequestHeaders(httpReq.Header, req.Headers)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

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
		return nil, &ProviderError{Provider: c.Name(), Status: resp.StatusCode, Body: respBody, Headers: resp.Header.Clone()}
	}

	var openaiResp openAIChatResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	result := &ChatResponse{
		ID:    openaiResp.ID,
		Model: openaiResp.Model,
		Raw:   respBody,
	}

	if len(openaiResp.Choices) > 0 {
		result.Content = openaiResp.Choices[0].Message.Content
		result.Role = openaiResp.Choices[0].Message.Role
		result.FinishReason = openaiResp.Choices[0].FinishReason
	}

	if openaiResp.Usage != nil {
		result.InputTokens = openaiResp.Usage.PromptTokens
		result.OutputTokens = openaiResp.Usage.CompletionTokens
	}

	return result, nil
}

// ChatStream sends a streaming chat completion request.
func (c *OpenAIClient) ChatStream(ctx context.Context, req *ChatRequest, apiKey string) (<-chan StreamChunk, error) {
	copyReq := *req
	copyReq.Model = c.outboundModel(req.Model)
	req = &copyReq
	var body []byte
	if req.Raw != nil {
		// Ensure stream is true and include usage
		var m map[string]json.RawMessage
		if err := json.Unmarshal(req.Raw, &m); err == nil {
			m["stream"] = json.RawMessage(`true`)
			if req.Model != "" {
				m["model"], _ = json.Marshal(c.outboundModel(req.Model))
			}
			var opts map[string]json.RawMessage
			_ = json.Unmarshal(m["stream_options"], &opts)
			if opts == nil {
				opts = make(map[string]json.RawMessage)
			}
			opts["include_usage"] = json.RawMessage(`true`)
			m["stream_options"], _ = json.Marshal(opts)
			body, _ = json.Marshal(m)
		}
	}
	if body == nil {
		openaiReq := toOpenAIRequest(req)
		openaiReq.Stream = true
		openaiReq.StreamOptions = &openAIStreamOptions{IncludeUsage: true}
		var err error
		body, err = json.Marshal(openaiReq)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	forwardRequestHeaders(httpReq.Header, req.Headers)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &ProviderError{Provider: c.Name(), Status: resp.StatusCode, Body: body, Headers: resp.Header.Clone()}
	}

	return ReadSSEStream(ctx, resp.Body, "[DONE]"), nil
}

// OpenAI-specific types

type openAIChatRequest struct {
	Model         string               `json:"model"`
	Messages      []openAIMessage      `json:"messages"`
	MaxTokens     *int                 `json:"max_tokens,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	Stream        bool                 `json:"stream"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
}

// openAIMessage carries either a plain-string content (when no
// images) or a content-parts array (multimodal). The OpenAI Chat
// Completions API accepts both shapes for `content`; using `any`
// keeps the JSON output the spec wants.
type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// openAITextPart and openAIImagePart are the two part types we emit
// when a message has Images. OpenAI's spec lists more (input_audio,
// file refs, etc.); we only need text + image_url for chat use.
type openAITextPart struct {
	Type string `json:"type"` // always "text"
	Text string `json:"text"`
}

type openAIImagePart struct {
	Type     string                  `json:"type"` // always "image_url"
	ImageURL openAIImagePartImageURL `json:"image_url"`
}

type openAIImagePartImageURL struct {
	URL string `json:"url"` // data: URL with base64 payload
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func toOpenAIRequest(req *ChatRequest) *openAIChatRequest {
	msgs := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		// No images → plain string content. Cheaper for the model
		// and matches the most common request shape.
		if len(m.Images) == 0 {
			msgs[i] = openAIMessage{Role: m.Role, Content: m.Content}
			continue
		}
		// Multimodal: content is a parts array. Text first, then
		// each image as image_url with a data: URL. Order matches
		// what users typed (text + their attached images).
		parts := make([]any, 0, 1+len(m.Images))
		if m.Content != "" {
			parts = append(parts, openAITextPart{Type: "text", Text: m.Content})
		}
		for _, img := range m.Images {
			parts = append(parts, openAIImagePart{
				Type: "image_url",
				ImageURL: openAIImagePartImageURL{
					URL: "data:" + img.MimeType + ";base64," + img.Data,
				},
			})
		}
		msgs[i] = openAIMessage{Role: m.Role, Content: parts}
	}
	return &openAIChatRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}
}
