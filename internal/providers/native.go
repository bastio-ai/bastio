package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	// BodyReader is used for inspected batch files without buffering the file.
	BodyReader    io.Reader
	ContentLength int64
	// UploadURL may only be a provider-issued resumable upload URL. The
	// Gemini transport verifies it against its configured upstream origin.
	UploadURL string
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
			strings.HasPrefix(k, "x-openrouter-") || strings.HasPrefix(k, "x-goog-upload-")
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
	var body io.Reader = bytes.NewReader(req.Body)
	if req.BodyReader != nil {
		body = req.BodyReader
	}
	r, err := http.NewRequestWithContext(ctx, req.Method, base, body)
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}
	if (r.URL.Scheme != "https" && r.URL.Scheme != "http") || r.URL.Host == "" || r.URL.User != nil {
		return nil, fmt.Errorf("invalid configured provider base URL")
	}
	path := req.Path
	if provider == ProviderGemini && (strings.HasPrefix(path, "/upload/") || strings.HasPrefix(path, "/download/")) {
		r.URL.Path = path
	} else {
		r.URL.Path = strings.TrimRight(r.URL.Path, "/") + path
	}
	r.URL.RawPath = ""
	r.URL.RawQuery = req.Query.Encode()
	if provider == ProviderGemini {
		q := r.URL.Query()
		q.Del("key")
		r.URL.RawQuery = q.Encode()
	}
	if req.BodyReader != nil {
		r.ContentLength = req.ContentLength
	}
	forwardRequestHeaders(r.Header, req.Headers)
	if r.Header.Get("Content-Type") == "" && len(req.Body) > 0 {
		r.Header.Set("Content-Type", "application/json")
	}
	if provider == ProviderAnthropic {
		r.Header.Set("x-api-key", apiKey)
		if r.Header.Get("anthropic-version") == "" {
			r.Header.Set("anthropic-version", "2023-06-01")
		}
	} else if provider == ProviderGemini {
		r.Header.Set("x-goog-api-key", apiKey)
	} else {
		r.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("send provider request: %w", err)
	}
	return resp, nil
}

func (c *GeminiClient) Forward(ctx context.Context, req *NativeRequest, apiKey string) (*http.Response, error) {
	if req.UploadURL != "" {
		base, e := url.Parse(c.baseURL)
		if e != nil {
			return nil, e
		}
		upload, e := url.Parse(req.UploadURL)
		if e != nil || upload.Scheme != base.Scheme || upload.Host != base.Host || upload.User != nil || !strings.HasPrefix(upload.Path, "/upload/") || upload.Fragment != "" {
			return nil, fmt.Errorf("untrusted provider upload URL")
		}
		copy := *req
		copy.Path = upload.Path
		copy.Query = upload.Query()
		copy.UploadURL = ""
		req = &copy
	}
	return forwardNative(ctx, providerHTTPClient(), c.baseURL, c.Name(), req, apiKey)
}

func (c *OpenAIClient) Forward(ctx context.Context, req *NativeRequest, apiKey string) (*http.Response, error) {
	return forwardNative(ctx, c.httpClient, c.baseURL, c.Name(), req, apiKey)
}

func (c *AnthropicClient) Forward(ctx context.Context, req *NativeRequest, apiKey string) (*http.Response, error) {
	return forwardNative(ctx, c.httpClient, c.baseURL, c.Name(), req, apiKey)
}
