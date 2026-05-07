package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllama_ChatPassThrough(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-1","model":"llama3.2",
			"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5}
		}`))
	}))
	defer srv.Close()

	c := NewOllamaClientWithBaseURL(srv.URL)

	raw := []byte(`{"model":"llama3.2","messages":[{"role":"user","content":"hello"}]}`)
	resp, err := c.Chat(context.Background(), &ChatRequest{Raw: raw}, "")
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path: want /v1/chat/completions got %q", gotPath)
	}
	if gotAuth != "" {
		t.Fatalf("auth header should be empty when no key is provided, got %q", gotAuth)
	}
	if gotBody["stream"] != false {
		t.Fatalf("stream must be forced to false for non-streaming Chat, got %v", gotBody["stream"])
	}
	if resp.Content != "hi" || resp.InputTokens != 10 || resp.OutputTokens != 5 {
		t.Fatalf("response: %+v", resp)
	}
}

func TestOllama_ChatStreamSetsUsage(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewOllamaClientWithBaseURL(srv.URL)

	stream, err := c.ChatStream(context.Background(), &ChatRequest{
		Raw: []byte(`{"model":"llama3.2","messages":[{"role":"user","content":"hi"}]}`),
	}, "secret")
	if err != nil {
		t.Fatalf("ChatStream err: %v", err)
	}

	// Drain stream to let SSE parser advance.
	for c := range stream {
		if c.Done {
			break
		}
	}

	if gotBody["stream"] != true {
		t.Fatalf("stream must be true when streaming, got %v", gotBody["stream"])
	}
	opts, ok := gotBody["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Fatalf("stream_options.include_usage must be true, got %v", gotBody["stream_options"])
	}
}

func TestOllama_ChatStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c := NewOllamaClientWithBaseURL(srv.URL)
	_, err := c.ChatStream(context.Background(), &ChatRequest{
		Raw: []byte(`{"model":"x","messages":[]}`),
	}, "")
	if err == nil || !strings.Contains(err.Error(), "ollama error") {
		t.Fatalf("expected ollama error, got %v", err)
	}
}
