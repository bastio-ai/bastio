package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// OllamaClient implements the Client interface against a local or remote
// Ollama server. Ollama exposes an OpenAI-compatible endpoint at
// /v1/chat/completions (see https://ollama.com/blog/openai-compatibility)
// so the wire format matches OpenAI; only the base URL and auth header
// differ.
//
// The base URL defaults to http://localhost:11434 and can be overridden
// via the OLLAMA_BASE_URL environment variable. Ollama does not require
// an API key by default; the apiKey argument is forwarded as a Bearer
// token if non-empty (useful when the server is fronted by a proxy that
// expects a shared secret).
type OllamaClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewOllamaClient constructs an Ollama client. Uses OLLAMA_BASE_URL if
// set, otherwise http://localhost:11434.
func NewOllamaClient() *OllamaClient {
	base := os.Getenv("OLLAMA_BASE_URL")
	if base == "" {
		base = "http://localhost:11434"
	}
	return &OllamaClient{baseURL: base, httpClient: &http.Client{}}
}

// NewOllamaClientWithBaseURL constructs an Ollama client pointing at a
// specific server. Useful for tests.
func NewOllamaClientWithBaseURL(baseURL string) *OllamaClient {
	return &OllamaClient{baseURL: baseURL, httpClient: &http.Client{}}
}

func (c *OllamaClient) Name() Provider { return ProviderOllama }

// Chat sends a non-streaming chat completion request.
func (c *OllamaClient) Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error) {
	body, err := buildOpenAICompatBody(req, false, false)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(ctx, body, apiKey)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var r openAIChatResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	out := &ChatResponse{ID: r.ID, Model: r.Model, Raw: respBody}
	if len(r.Choices) > 0 {
		out.Content = r.Choices[0].Message.Content
		out.Role = r.Choices[0].Message.Role
		out.FinishReason = r.Choices[0].FinishReason
	}
	if r.Usage != nil {
		out.InputTokens = r.Usage.PromptTokens
		out.OutputTokens = r.Usage.CompletionTokens
	}
	return out, nil
}

// ChatStream sends a streaming chat completion request. Ollama returns SSE
// terminated by "[DONE]" in its OpenAI-compat mode.
func (c *OllamaClient) ChatStream(ctx context.Context, req *ChatRequest, apiKey string) (<-chan StreamChunk, error) {
	body, err := buildOpenAICompatBody(req, true, true)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(ctx, body, apiKey)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("ollama error (status %d): %s", resp.StatusCode, string(b))
	}
	return ReadSSEStream(ctx, resp.Body, "[DONE]"), nil
}

func (c *OllamaClient) do(ctx context.Context, body []byte, apiKey string) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return c.httpClient.Do(httpReq)
}

// buildOpenAICompatBody produces a request body compatible with OpenAI's
// /v1/chat/completions endpoint, used by both OpenAI itself and by Ollama
// when running in OpenAI-compat mode. If req.Raw is set, it is passed
// through with the stream flag coerced.
func buildOpenAICompatBody(req *ChatRequest, stream, includeUsage bool) ([]byte, error) {
	if req.Raw != nil {
		var m map[string]any
		if err := json.Unmarshal(req.Raw, &m); err == nil {
			m["stream"] = stream
			if stream && includeUsage {
				m["stream_options"] = map[string]any{"include_usage": true}
			}
			return json.Marshal(m)
		}
	}
	oai := toOpenAIRequest(req)
	oai.Stream = stream
	if stream && includeUsage {
		oai.StreamOptions = &openAIStreamOptions{IncludeUsage: true}
	}
	return json.Marshal(oai)
}
