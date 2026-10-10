package providers

import (
	"context"
	"net/http"
	"os"
	"strings"
)

// OpenRouter uses OpenAI-shaped Chat Completions without translating model
// slugs or stripping OpenRouter routing, reasoning, plugin and BYOK fields.
type OpenRouterClient struct{ *OpenAIClient }

func NewOpenRouterClient() *OpenRouterClient {
	base := os.Getenv("OPENROUTER_BASE_URL")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	return NewOpenRouterClientWithBaseURL(base)
}

func NewOpenRouterClientWithBaseURL(base string) *OpenRouterClient {
	return &OpenRouterClient{&OpenAIClient{baseURL: strings.TrimRight(base, "/"), httpClient: providerHTTPClient(), provider: ProviderOpenRouter}}
}

func (c *OpenRouterClient) Name() Provider { return ProviderOpenRouter }

func (c *OpenRouterClient) Forward(ctx context.Context, req *NativeRequest, apiKey string) (*http.Response, error) {
	return forwardNative(ctx, c.httpClient, c.baseURL, ProviderOpenRouter, req, apiKey)
}
