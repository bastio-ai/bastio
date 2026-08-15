package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeepSeek_Chat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-deepseek-key" {
			t.Errorf("expected Bearer test-deepseek-key, got %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := deepSeekChatResponse{
			ID:    "deepseek-chat-123",
			Model: "deepseek-chat",
			Choices: []struct {
				Message      deepSeekMessage `json:"message"`
				FinishReason string          `json:"finish_reason"`
			}{
				{
					Message: deepSeekMessage{
						Role:    "assistant",
						Content: "Hello from DeepSeek!",
					},
					FinishReason: "stop",
				},
			},
			Usage: &struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewDeepSeekClientWithBaseURL(ts.URL)
	if client.Name() != ProviderDeepSeek {
		t.Fatalf("expected provider %s, got %s", ProviderDeepSeek, client.Name())
	}

	req := &ChatRequest{
		Model: "deepseek-chat",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	resp, err := client.Chat(context.Background(), req, "test-deepseek-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Hello from DeepSeek!" {
		t.Errorf("expected content 'Hello from DeepSeek!', got %q", resp.Content)
	}
	if resp.InputTokens != 10 || resp.OutputTokens != 20 {
		t.Errorf("unexpected tokens: in=%d, out=%d", resp.InputTokens, resp.OutputTokens)
	}
}

func TestDeepSeek_ChatStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"deep\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"seek\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer ts.Close()

	client := NewDeepSeekClientWithBaseURL(ts.URL)
	req := &ChatRequest{
		Model: "deepseek-reasoner",
		Messages: []Message{
			{Role: "user", Content: "Solve math problem"},
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

	if count != 2 {
		t.Errorf("expected 2 chunks, got %d", count)
	}
}
