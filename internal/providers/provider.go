package providers

import (
	"context"
	"fmt"
	"io"
)

// Provider identifies an LLM provider.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderBedrock   Provider = "bedrock"
	ProviderVertex    Provider = "vertex"
	ProviderAzure     Provider = "azure"
	ProviderOllama    Provider = "ollama"
)

// Message represents a chat message.
//
// When Images is non-empty, providers that support multimodal input
// (OpenAI gpt-4o family, Anthropic claude-3.x+, Gemini) emit a
// content-parts request body with both the text and the images.
// Providers that don't support images ignore the field and only
// emit Content — the caller is expected to have stuffed a textual
// note like "[image attached]" into Content for those models so the
// model is aware something was provided.
type Message struct {
	Role    string  `json:"role"`
	Content string  `json:"content"`
	Images  []Image `json:"images,omitempty"`
}

// Image is one image attachment carried inline with a Message.
// Data is the base64-encoded raw image bytes (no data: URL prefix).
// MimeType is the IANA type (image/png, image/jpeg, etc.).
// Filename is optional alt text used by providers that surface a
// label per image (most don't; this is informational).
type Image struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
	Filename string `json:"filename,omitempty"`
}

// ChatRequest is a normalized chat completion request.
type ChatRequest struct {
	Provider    Provider  `json:"-"`
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	Stream      bool      `json:"stream"`
	// Raw holds the original request body for pass-through proxying.
	Raw []byte `json:"-"`
}

// ChatResponse is a normalized non-streaming response.
type ChatResponse struct {
	ID           string   `json:"id"`
	Model        string   `json:"model"`
	Content      string   `json:"content"`
	Role         string   `json:"role"`
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	FinishReason string   `json:"finish_reason"`
	Raw          []byte   `json:"-"` // Original provider response for pass-through
}

// StreamChunk is a single chunk in a streaming response.
type StreamChunk struct {
	Data  []byte // Raw SSE data line (without "data: " prefix)
	Done  bool   // True if this is the final chunk
	Error error  // Non-nil if an error occurred
}

// Client is the interface all provider clients implement.
type Client interface {
	// Chat sends a non-streaming chat completion request.
	Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error)

	// ChatStream sends a streaming chat completion request.
	// The caller must read all chunks and close the reader.
	ChatStream(ctx context.Context, req *ChatRequest, apiKey string) (<-chan StreamChunk, error)

	// Name returns the provider name.
	Name() Provider
}

// Registry holds provider clients by name.
type Registry struct {
	clients   map[Provider]Client
	decorator func(Provider, Client) Client
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{clients: make(map[Provider]Client)}
}

// Register adds a provider client.
func (r *Registry) Register(client Client) {
	r.clients[client.Name()] = client
}

// Decorate installs a generic wrapping function applied to every Client
// returned by Get. Use cases: cross-cutting concerns like retries,
// per-client logging, response caching, or rate limiting that should
// transparently sit between callers and the upstream provider.
//
// fn is called with (provider, raw client) and returns the wrapped
// client. Returning the original client unchanged disables wrapping
// for that provider. nil fn clears the decorator.
//
// Set once at startup before traffic flows — Decorate is not safe to
// call concurrently with Get.
func (r *Registry) Decorate(fn func(Provider, Client) Client) {
	r.decorator = fn
}

// Get returns a provider client by name. If a decorator is installed,
// the wrapped client is returned.
func (r *Registry) Get(provider Provider) (Client, error) {
	c, ok := r.clients[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
	if r.decorator != nil {
		return r.decorator(provider, c), nil
	}
	return c, nil
}

// ReadSSEStream reads an SSE stream from a reader and sends chunks to a channel.
// Shared utility for all providers that use SSE.
func ReadSSEStream(ctx context.Context, body io.ReadCloser, doneMarker string) <-chan StreamChunk {
	ch := make(chan StreamChunk, 16)

	go func() {
		defer close(ch)
		defer body.Close()

		buf := make([]byte, 4096)
		var leftover []byte

		for {
			select {
			case <-ctx.Done():
				ch <- StreamChunk{Error: ctx.Err()}
				return
			default:
			}

			n, err := body.Read(buf)
			if n > 0 {
				data := append(leftover, buf[:n]...)
				leftover = nil

				// Process complete lines
				for {
					idx := -1
					for i := 0; i < len(data)-1; i++ {
						if data[i] == '\n' && data[i+1] == '\n' {
							idx = i
							break
						}
					}
					if idx == -1 {
						// Check for single newline terminated data lines
						for i := 0; i < len(data); i++ {
							if data[i] == '\n' {
								line := string(data[:i])
								data = data[i+1:]
								if len(line) > 6 && line[:6] == "data: " {
									payload := line[6:]
									if payload == doneMarker {
										ch <- StreamChunk{Done: true}
										return
									}
									ch <- StreamChunk{Data: []byte(payload)}
								}
								continue
							}
						}
						leftover = data
						break
					}

					block := string(data[:idx])
					data = data[idx+2:]

					// Parse SSE block
					for _, line := range splitLines(block) {
						if len(line) > 6 && line[:6] == "data: " {
							payload := line[6:]
							if payload == doneMarker {
								ch <- StreamChunk{Done: true}
								return
							}
							ch <- StreamChunk{Data: []byte(payload)}
						}
					}
				}
			}

			if err != nil {
				if err != io.EOF {
					ch <- StreamChunk{Error: err}
				}
				return
			}
		}
	}()

	return ch
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
