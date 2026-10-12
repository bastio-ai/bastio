package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNativeProviderContracts(t *testing.T) {
	for _, provider := range []Provider{ProviderOpenAI, ProviderAnthropic, ProviderOpenRouter, ProviderXAI} {
		t.Run(string(provider), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/responses/rsp_123/cancel" || r.URL.Query().Get("include") != "usage" {
					t.Errorf("URL lost: %s", r.URL)
				}
				if r.Header.Get("X-Bastio-Provider") != "" || r.Header.Get("Cookie") != "" {
					t.Error("internal credentials leaked")
				}
				if r.Header.Get("Idempotency-Key") != "retry-123" || r.Header.Get("HTTP-Referer") != "https://app.example" {
					t.Error("API headers lost")
				}
				if provider == ProviderAnthropic {
					if r.Header.Get("X-Api-Key") != "provider-secret" || r.Header.Get("Anthropic-Version") != "2023-06-01" || r.Header.Get("Anthropic-Beta") != "test-beta" {
						t.Error("Claude headers lost")
					}
				} else if r.Header.Get("Authorization") != "Bearer provider-secret" {
					t.Error("wrong provider credential")
				}
				data, _ := io.ReadAll(r.Body)
				if string(data) != `{"model":"anthropic/claude-sonnet-4.5","provider":{"zdr":true},"reasoning":{"effort":"high"}}` {
					t.Errorf("body changed: %s", data)
				}
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(402)
				_, _ = w.Write([]byte(`{"error":{"code":402,"metadata":{"reason":"credits"}}}`))
			}))
			defer server.Close()
			var client NativeClient
			switch provider {
			case ProviderOpenAI:
				client = NewOpenAIClientWithBaseURL(server.URL)
			case ProviderAnthropic:
				client = NewAnthropicClientWithBaseURL(server.URL)
			case ProviderOpenRouter:
				client = NewOpenRouterClientWithBaseURL(server.URL)
			case ProviderXAI:
				client = NewXAIClientWithBaseURL(server.URL)
			}
			resp, err := client.Forward(context.Background(), &NativeRequest{
				Method: "POST", Path: "/responses/rsp_123/cancel", Query: url.Values{"include": {"usage"}},
				Headers: http.Header{"Authorization": {"Bearer bastio-secret"}, "Cookie": {"session=secret"}, "X-Bastio-Provider": {"openrouter"}, "Idempotency-Key": {"retry-123"}, "Http-Referer": {"https://app.example"}, "Anthropic-Beta": {"test-beta"}},
				Body:    []byte(`{"model":"anthropic/claude-sonnet-4.5","provider":{"zdr":true},"reasoning":{"effort":"high"}}`),
			}, "provider-secret")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 402 || resp.Header.Get("Retry-After") != "2" {
				t.Fatalf("provider error contract lost: %v", resp)
			}
		})
	}
}

func TestOpenAIChatFallbackChangesWireModelAndPreservesStreamOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model         string                     `json:"model"`
			StreamOptions map[string]json.RawMessage `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "fallback-model" || string(body.StreamOptions["include_obfuscation"]) != "false" {
			t.Errorf("model/options lost: %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	client := NewOpenAIClientWithBaseURL(server.URL)
	stream, err := client.ChatStream(context.Background(), &ChatRequest{Model: "fallback-model", Raw: []byte(`{"model":"original","stream_options":{"include_obfuscation":false}}`)}, "key")
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
	}
}

func TestSSEPreservesNativeEventsAndMultilinePayloads(t *testing.T) {
	input := ": processing\r\nevent: content_block_delta\r\nid: 42\r\ndata: {\r\ndata: \"text\":\"hi\"}\r\n\r\nevent: message_stop\ndata: {\"type\":\"message_stop\"}"
	var frames []SSEFrame
	err := ReadSSE(context.Background(), strings.NewReader(input), func(f SSEFrame) error { frames = append(frames, f); return nil })
	if err != nil || len(frames) != 2 {
		t.Fatalf("parse: %v frames=%d", err, len(frames))
	}
	if frames[0].Event != "content_block_delta" || frames[0].ID != "42" || string(frames[0].Data) != "{\n\"text\":\"hi\"}" || len(frames[0].Comments) != 1 {
		t.Fatalf("event lost: %+v", frames[0])
	}
	if frames[1].Event != "message_stop" {
		t.Fatal("unterminated terminal event dropped")
	}
	if rewritten := string(RewriteSSEData(frames[0], []byte(`{"text":"safe"}`))); !strings.Contains(rewritten, "event: content_block_delta") || !strings.Contains(rewritten, "id: 42") || !strings.Contains(rewritten, ": processing") {
		t.Fatalf("metadata lost: %s", rewritten)
	}
}

func TestStreamCancellationUnblocksChannelProducer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := io.NopCloser(strings.NewReader(strings.Repeat("data: {}\n\n", 100)))
	stream := ReadSSEStream(ctx, body, "[DONE]")
	cancel()
	for range stream {
	}
}

func TestNativeCallerPathCannotChangeAuthority(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if !strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Query().Get("redirect") != "https://other.example" {
			t.Errorf("configured API base lost: %s", r.URL)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	client := NewOpenRouterClientWithBaseURL(upstream.URL + "/v1")
	for _, path := range []string{"/@other.example", "/https://other.example", "/%2f%2fother.example"} {
		resp, err := client.Forward(context.Background(), &NativeRequest{Method: http.MethodGet, Path: path, Query: url.Values{"redirect": {"https://other.example"}}}, "secret")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	for _, path := range []string{"//other.example", "/../key", "https://other.example", "/key?redirect=1"} {
		if _, err := client.Forward(context.Background(), &NativeRequest{Method: http.MethodGet, Path: path}, "secret"); err == nil {
			t.Fatalf("unsafe path accepted: %s", path)
		}
	}
	if requests != 3 {
		t.Fatalf("unexpected outbound requests: %d", requests)
	}
}
