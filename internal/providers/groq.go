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

// GroqClient implements the Client interface for Groq.
type GroqClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewGroqClient creates a new Groq client.
func NewGroqClient() *GroqClient {
	base := os.Getenv("GROQ_BASE_URL")
	if base == "" {
		base = "https://api.groq.com/openai/v1"
	}
	return &GroqClient{
		baseURL:    strings.TrimRight(base, "/"),
		httpClient: &http.Client{},
	}
}

// NewGroqClientWithBaseURL creates a new Groq client with a custom base URL.
func NewGroqClientWithBaseURL(baseURL string) *GroqClient {
	return &GroqClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

func (c *GroqClient) Name() Provider { return ProviderGroq }

// Chat sends a non-streaming chat completion request to Groq.
func (c *GroqClient) Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error) {
	body, err := buildOpenAICompatBody(req, false, false)
	if err != nil {
		return nil, fmt.Errorf("build groq request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create groq request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send groq request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read groq response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("groq error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var groqResp openAIChatResponse
	if err := json.Unmarshal(respBody, &groqResp); err != nil {
		return nil, fmt.Errorf("unmarshal groq response: %w", err)
	}

	result := &ChatResponse{
		ID:    groqResp.ID,
		Model: groqResp.Model,
		Raw:   respBody,
	}

	if len(groqResp.Choices) > 0 {
		result.Content = groqResp.Choices[0].Message.Content
		result.Role = groqResp.Choices[0].Message.Role
		result.FinishReason = groqResp.Choices[0].FinishReason
	}

	if groqResp.Usage != nil {
		result.InputTokens = groqResp.Usage.PromptTokens
		result.OutputTokens = groqResp.Usage.CompletionTokens
	}

	return result, nil
}

// ChatStream sends a streaming chat completion request to Groq.
func (c *GroqClient) ChatStream(ctx context.Context, req *ChatRequest, apiKey string) (<-chan StreamChunk, error) {
	body, err := buildOpenAICompatBody(req, true, true)
	if err != nil {
		return nil, fmt.Errorf("build groq stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create groq stream request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send groq stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("groq error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return ReadSSEStream(ctx, resp.Body, "[DONE]"), nil
}
