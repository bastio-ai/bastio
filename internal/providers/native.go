package providers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// NativeRequest retains the provider's API contract, including non-chat APIs.
// Path is relative to the configured provider /v1 base, never an upstream URL.
type NativeRequest struct {
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    []byte
}

type NativeClient interface {
	Forward(context.Context, *NativeRequest, string) (*http.Response, error)
}

// ProviderError retains the upstream API error for retry-capable chat clients.
type ProviderError struct {
	Provider Provider
	Status   int
	Body     []byte
	Headers  http.Header
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s error (status %d): %s", e.Provider, e.Status, e.Body)
}

// Native selects a wire transport and applies decorators that explicitly
// support native APIs. Chat-only decorators cannot normalize these requests.
func (r *Registry) Native(provider Provider) (NativeClient, error) {
	c, ok := r.clients[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
	n, ok := c.(NativeClient)
	if !ok {
		return nil, fmt.Errorf("provider %s has no native API transport", provider)
	}
	if r.decorator != nil {
		if decorated, ok := r.decorator(provider, c).(NativeClient); ok {
			return decorated, nil
		}
	}
	return n, nil
}

func providerHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

// Only API headers cross the trust boundary; Bastio credentials, cookies,
// internal routing metadata and hop-by-hop headers never reach the provider.
func forwardRequestHeaders(dst, src http.Header) {
	for key, values := range src {
		k := strings.ToLower(key)
		allowed := k == "content-type" || k == "accept" || k == "idempotency-key" ||
			k == "range" || k == "if-none-match" || k == "if-modified-since" || k == "if-match" ||
			k == "http-referer" || k == "x-title" || k == "anthropic-version" ||
			k == "anthropic-beta" || k == "x-anthropic-beta" ||
			k == "openai-beta" || k == "openai-organization" || k == "openai-project" ||
			strings.HasPrefix(k, "x-openrouter-")
		if allowed {
			dst[key] = append([]string(nil), values...)
		}
	}
}

func forwardNative(ctx context.Context, client *http.Client, base string, provider Provider, req *NativeRequest, apiKey string) (*http.Response, error) {
	if !strings.HasPrefix(req.Path, "/") || strings.Contains(req.Path, "..") || strings.ContainsAny(req.Path, "?#\\") || strings.HasPrefix(req.Path, "//") {
		return nil, fmt.Errorf("invalid provider API path")
	}
	// Parse only the trusted server-configured base as a URL. Caller-controlled
	// resource paths and queries cannot alter its scheme, authority or userinfo.
	r, err := http.NewRequestWithContext(ctx, req.Method, base, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}
	if (r.URL.Scheme != "https" && r.URL.Scheme != "http") || r.URL.Host == "" || r.URL.User != nil {
		return nil, fmt.Errorf("invalid configured provider base URL")
	}
	r.URL.Path = strings.TrimRight(r.URL.Path, "/") + req.Path
	r.URL.RawPath = ""
	r.URL.RawQuery = req.Query.Encode()
	forwardRequestHeaders(r.Header, req.Headers)
	if r.Header.Get("Content-Type") == "" && len(req.Body) > 0 {
		r.Header.Set("Content-Type", "application/json")
	}
	if provider == ProviderAnthropic {
		r.Header.Set("x-api-key", apiKey)
		if r.Header.Get("anthropic-version") == "" {
			r.Header.Set("anthropic-version", "2023-06-01")
		}
	} else {
		r.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("send provider request: %w", err)
	}
	return resp, nil
}

func (c *OpenAIClient) Forward(ctx context.Context, req *NativeRequest, apiKey string) (*http.Response, error) {
	return forwardNative(ctx, c.httpClient, c.baseURL, c.Name(), req, apiKey)
}

func (c *AnthropicClient) Forward(ctx context.Context, req *NativeRequest, apiKey string) (*http.Response, error) {
	return forwardNative(ctx, c.httpClient, c.baseURL, c.Name(), req, apiKey)
}
