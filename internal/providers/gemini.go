package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// GeminiClient implements the Client interface for Google Gemini / Vertex AI.
type GeminiClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewGeminiClient creates a new Google Gemini client.
func NewGeminiClient() *GeminiClient {
	base := os.Getenv("GEMINI_BASE_URL")
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &GeminiClient{
		baseURL:    strings.TrimRight(base, "/"),
		httpClient: &http.Client{},
	}
}

// NewGeminiClientWithBaseURL creates a new Google Gemini client with a custom base URL.
func NewGeminiClientWithBaseURL(baseURL string) *GeminiClient {
	return &GeminiClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

func (c *GeminiClient) Name() Provider { return ProviderGemini }

// Chat sends a non-streaming chat completion request to Gemini.
func (c *GeminiClient) Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error) {
	model := normalizeGeminiModel(req.Model)
	var body []byte
	var err error

	if req.Raw != nil {
		// Check if raw is already in Gemini format
		var m map[string]any
		if err := json.Unmarshal(req.Raw, &m); err == nil {
			if _, ok := m["contents"]; ok {
				body = req.Raw
			}
		}
	}

	if body == nil {
		geminiReq := toGeminiRequest(req)
		body, err = json.Marshal(geminiReq)
		if err != nil {
			return nil, fmt.Errorf("marshal gemini request: %w", err)
		}
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create gemini request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send gemini request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gemini response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var geminiResp geminiGenerateContentResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("unmarshal gemini response: %w", err)
	}

	result := &ChatResponse{
		ID:    fmt.Sprintf("gemini-%s", model),
		Model: model,
		Role:  "assistant",
		Raw:   respBody,
	}

	if len(geminiResp.Candidates) > 0 {
		cand := geminiResp.Candidates[0]
		result.FinishReason = cand.FinishReason
		var sb strings.Builder
		for _, part := range cand.Content.Parts {
			sb.WriteString(part.Text)
		}
		result.Content = sb.String()
		if cand.Content.Role != "" {
			result.Role = cand.Content.Role
		}
	}

	if geminiResp.UsageMetadata != nil {
		result.InputTokens = geminiResp.UsageMetadata.PromptTokenCount
		result.OutputTokens = geminiResp.UsageMetadata.CandidatesTokenCount
	}

	return result, nil
}

// ChatStream sends a streaming chat completion request to Gemini.
func (c *GeminiClient) ChatStream(ctx context.Context, req *ChatRequest, apiKey string) (<-chan StreamChunk, error) {
	model := normalizeGeminiModel(req.Model)
	var body []byte
	var err error

	if req.Raw != nil {
		var m map[string]any
		if err := json.Unmarshal(req.Raw, &m); err == nil {
			if _, ok := m["contents"]; ok {
				body = req.Raw
			}
		}
	}

	if body == nil {
		geminiReq := toGeminiRequest(req)
		body, err = json.Marshal(geminiReq)
		if err != nil {
			return nil, fmt.Errorf("marshal gemini stream request: %w", err)
		}
	}

	endpoint := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", c.baseURL, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create gemini stream request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send gemini stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return ReadSSEStream(ctx, resp.Body, "[DONE]"), nil
}

func normalizeGeminiModel(m string) string {
	if m == "" {
		return "gemini-1.5-flash"
	}
	m = strings.TrimPrefix(m, "models/")
	return m
}

// Gemini request structures

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
}

type geminiGenerateContentRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"system_instruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerateContentResponse struct {
	Candidates []struct {
		Content struct {
			Role  string       `json:"role"`
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

func toGeminiRequest(req *ChatRequest) *geminiGenerateContentRequest {
	var contents []geminiContent
	var systemInstruction *geminiContent

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: msg.Content}},
			}
			continue
		}

		role := msg.Role
		if role == "assistant" {
			role = "model"
		} else if role != "user" && role != "model" {
			role = "user"
		}

		parts := make([]geminiPart, 0, 1+len(msg.Images))
		if msg.Content != "" {
			parts = append(parts, geminiPart{Text: msg.Content})
		}
		for _, img := range msg.Images {
			parts = append(parts, geminiPart{
				InlineData: &geminiInlineData{
					MimeType: img.MimeType,
					Data:     img.Data,
				},
			})
		}

		contents = append(contents, geminiContent{
			Role:  role,
			Parts: parts,
		})
	}

	var genConfig *geminiGenerationConfig
	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil {
		genConfig = &geminiGenerationConfig{
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	return &geminiGenerateContentRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		GenerationConfig:  genConfig,
	}
}
