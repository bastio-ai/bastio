package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// TypeSafe has a decision API, not Chat Completions. Preserve its native
// state/questions/answers contract; never translate it through a chat model.
type TypeSafeClient struct {
	baseURL string
	client  *http.Client
}

func NewTypeSafeClient() *TypeSafeClient {
	base := os.Getenv("TYPESAFE_BASE_URL")
	if base == "" {
		base = "https://api.typesafe.ai/v1"
	}
	return NewTypeSafeClientWithBaseURL(base)
}

func NewTypeSafeClientWithBaseURL(base string) *TypeSafeClient {
	return &TypeSafeClient{baseURL: strings.TrimRight(base, "/"), client: providerHTTPClient()}
}

func (c *TypeSafeClient) Name() Provider { return ProviderTypeSafe }
func (c *TypeSafeClient) Chat(context.Context, *ChatRequest, string) (*ChatResponse, error) {
	return nil, fmt.Errorf("typesafe requires the native systemone endpoint")
}
func (c *TypeSafeClient) ChatStream(context.Context, *ChatRequest, string) (<-chan StreamChunk, error) {
	return nil, fmt.Errorf("typesafe does not support chat streaming")
}
func (c *TypeSafeClient) Forward(ctx context.Context, req *NativeRequest, key string) (*http.Response, error) {
	if req.Method != http.MethodPost || req.Path != "/systemone" {
		return nil, fmt.Errorf("unsupported typesafe API endpoint")
	}
	return forwardNative(ctx, c.client, c.baseURL, c.Name(), req, key)
}
