package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGroq_Chat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-groq-key" {
			t.Errorf("expected Bearer test-groq-key, got %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := openAIChatResponse{
			ID:    "groq-chat-123",
			Model: "llama-3.3-70b-versatile",
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{
						Role:    "assistant",
						Content: "Fast response from Groq!",
					},
					FinishReason: "stop",
				},
			},
			Usage: &struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			}{
				PromptTokens:     8,
				CompletionTokens: 16,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewGroqClientWithBaseURL(ts.URL)
	if client.Name() != ProviderGroq {
		t.Fatalf("expected provider %s, got %s", ProviderGroq, client.Name())
	}

	req := &ChatRequest{
		Model: "llama-3.3-70b-versatile",
		Messages: []Message{
			{Role: "user", Content: "Fast test"},
		},
	}

	resp, err := client.Chat(context.Background(), req, "test-groq-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Fast response from Groq!" {
		t.Errorf("expected 'Fast response from Groq!', got %q", resp.Content)
	}
	if resp.InputTokens != 8 || resp.OutputTokens != 16 {
		t.Errorf("unexpected token usage: in=%d, out=%d", resp.InputTokens, resp.OutputTokens)
	}
}

func TestGroq_ChatStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"groq\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer ts.Close()

	client := NewGroqClientWithBaseURL(ts.URL)
	req := &ChatRequest{
		Model: "llama3-8b-8192",
		Messages: []Message{
			{Role: "user", Content: "Stream"},
		},
		Stream: true,
	}

	ch, err := client.ChatStream(context.Background(), req, "key")
	if err != nil {
		t.Fatalf("unexpected stream err: %v", err)
	}

	var count int
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("chunk err: %v", chunk.Error)
		}
		if chunk.Done {
			break
		}
		count++
	}

	if count != 1 {
		t.Errorf("expected 1 chunk, got %d", count)
	}
}
