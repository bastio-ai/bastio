package providers

import (
	"os"
	"strings"
)

// XAIClient uses xAI's OpenAI-compatible Chat Completions API for Grok.
type XAIClient struct{ *OpenAIClient }

func NewXAIClient() *XAIClient {
	base := os.Getenv("XAI_BASE_URL")
	if base == "" {
		base = "https://api.x.ai/v1"
	}
	return NewXAIClientWithBaseURL(base)
}

func NewXAIClientWithBaseURL(base string) *XAIClient {
	return &XAIClient{&OpenAIClient{baseURL: strings.TrimRight(base, "/"), httpClient: providerHTTPClient(), provider: ProviderXAI}}
}
