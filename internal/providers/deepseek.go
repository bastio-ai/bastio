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

// DeepSeekClient implements the Client interface for DeepSeek.
type DeepSeekClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewDeepSeekClient creates a new DeepSeek client.
func NewDeepSeekClient() *DeepSeekClient {
	base := os.Getenv("DEEPSEEK_BASE_URL")
	if base == "" {
		base = "https://api.deepseek.com/v1"
	}
	return &DeepSeekClient{
		baseURL:    strings.TrimRight(base, "/"),
		httpClient: &http.Client{},
	}
}

// NewDeepSeekClientWithBaseURL creates a new DeepSeek client with a custom base URL.
func NewDeepSeekClientWithBaseURL(baseURL string) *DeepSeekClient {
	return &DeepSeekClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

func (c *DeepSeekClient) Name() Provider { return ProviderDeepSeek }

// Chat sends a non-streaming chat completion request to DeepSeek.
func (c *DeepSeekClient) Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error) {
	body, err := buildOpenAICompatBody(req, false, false)
	if err != nil {
		return nil, fmt.Errorf("build deepseek request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create deepseek request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send deepseek request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read deepseek response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepseek error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var deepseekResp deepSeekChatResponse
	if err := json.Unmarshal(respBody, &deepseekResp); err != nil {
		return nil, fmt.Errorf("unmarshal deepseek response: %w", err)
	}

	result := &ChatResponse{
		ID:    deepseekResp.ID,
		Model: deepseekResp.Model,
		Raw:   respBody,
	}

	if len(deepseekResp.Choices) > 0 {
		choice := deepseekResp.Choices[0]
		result.Content = choice.Message.Content
		result.Role = choice.Message.Role
		result.FinishReason = choice.FinishReason
	}

	if deepseekResp.Usage != nil {
		result.InputTokens = deepseekResp.Usage.PromptTokens
		result.OutputTokens = deepseekResp.Usage.CompletionTokens
	}

	return result, nil
}

// ChatStream sends a streaming chat completion request to DeepSeek.
func (c *DeepSeekClient) ChatStream(ctx context.Context, req *ChatRequest, apiKey string) (<-chan StreamChunk, error) {
	body, err := buildOpenAICompatBody(req, true, true)
	if err != nil {
		return nil, fmt.Errorf("build deepseek stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create deepseek stream request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send deepseek stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("deepseek error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return ReadSSEStream(ctx, resp.Body, "[DONE]"), nil
}

type deepSeekMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type deepSeekChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      deepSeekMessage `json:"message"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
