package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGemini_ChatNonStreaming(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-goog-api-key") != "test-gemini-key" {
			t.Errorf("expected x-goog-api-key header, got %s", r.Header.Get("x-goog-api-key"))
		}
		if r.URL.Path != "/models/gemini-1.5-pro:generateContent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req geminiGenerateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode req: %v", err)
		}
		if len(req.Contents) != 1 || req.Contents[0].Role != "user" {
			t.Errorf("unexpected contents: %+v", req.Contents)
		}

		resp := geminiGenerateContentResponse{
			Candidates: []struct {
				Content struct {
					Role  string       `json:"role"`
					Parts []geminiPart `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			}{
				{
					Content: struct {
						Role  string       `json:"role"`
						Parts []geminiPart `json:"parts"`
					}{
						Role:  "model",
						Parts: []geminiPart{{Text: "Hello from Gemini!"}},
					},
					FinishReason: "STOP",
				},
			},
			UsageMetadata: &struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			}{
				PromptTokenCount:     5,
				CandidatesTokenCount: 10,
				TotalTokenCount:      15,
			},
			ModelVersion: "gemini-1.5-pro",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewGeminiClientWithBaseURL(ts.URL)
	if client.Name() != ProviderGemini {
		t.Fatalf("expected provider %s, got %s", ProviderGemini, client.Name())
	}

	req := &ChatRequest{
		Model: "gemini-1.5-pro",
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	}

	resp, err := client.Chat(context.Background(), req, "test-gemini-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Hello from Gemini!" {
		t.Errorf("expected content 'Hello from Gemini!', got %q", resp.Content)
	}
	if resp.InputTokens != 5 || resp.OutputTokens != 10 {
		t.Errorf("unexpected token counts: in=%d out=%d", resp.InputTokens, resp.OutputTokens)
	}
}

func TestGemini_ChatStreaming(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("missing api key")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"chunk 1\"}],\"role\":\"model\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"chunk 2\"}],\"role\":\"model\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer ts.Close()

	client := NewGeminiClientWithBaseURL(ts.URL)
	req := &ChatRequest{
		Model: "gemini-1.5-flash",
		Messages: []Message{
			{Role: "user", Content: "Stream test"},
		},
		Stream: true,
	}

	ch, err := client.ChatStream(context.Background(), req, "test-key")
	if err != nil {
		t.Fatalf("stream err: %v", err)
	}

	var chunks []string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("chunk err: %v", chunk.Error)
		}
		if chunk.Done {
			break
		}
		chunks = append(chunks, string(chunk.Data))
	}

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestGemini_MultimodalAndSystem(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req geminiGenerateContentRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req.SystemInstruction == nil || len(req.SystemInstruction.Parts) == 0 {
			t.Errorf("expected system instruction")
		} else if req.SystemInstruction.Parts[0].Text != "You are a helpful assistant." {
			t.Errorf("unexpected system instruction: %s", req.SystemInstruction.Parts[0].Text)
		}

		if len(req.Contents) != 1 || len(req.Contents[0].Parts) != 2 {
			t.Fatalf("expected 1 content with 2 parts (text + image), got %+v", req.Contents)
		}
		if req.Contents[0].Parts[1].InlineData == nil || req.Contents[0].Parts[1].InlineData.MimeType != "image/png" {
			t.Errorf("expected inline image data")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(geminiGenerateContentResponse{
			Candidates: []struct {
				Content struct {
					Role  string       `json:"role"`
					Parts []geminiPart `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			}{
				{
					Content: struct {
						Role  string       `json:"role"`
						Parts []geminiPart `json:"parts"`
					}{
						Role:  "model",
						Parts: []geminiPart{{Text: "Image analyzed"}},
					},
					FinishReason: "STOP",
				},
			},
		})
	}))
	defer ts.Close()

	client := NewGeminiClientWithBaseURL(ts.URL)
	req := &ChatRequest{
		Model: "gemini-1.5-pro",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{
				Role:    "user",
				Content: "What is this image?",
				Images: []Image{
					{MimeType: "image/png", Data: "iVBORw0KGgoAAAANSUhEUg=="},
				},
			},
		},
	}

	resp, err := client.Chat(context.Background(), req, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Image analyzed" {
		t.Errorf("expected 'Image analyzed', got %q", resp.Content)
	}
}
