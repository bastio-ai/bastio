package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bastio-ai/bastio/internal/providers"
	"strings"
)

// ProviderEndpoint removes Bastio's routing prefix, retaining native paths.
func ProviderEndpoint(path string) string {
	if !strings.HasPrefix(path, "/v1/") {
		return ""
	}
	path = strings.TrimPrefix(path, "/v1")
	for _, prefix := range []string{"/typesafe/", "/openrouter/", "/xai/", "/openai/", "/anthropic/", "/google/", "/gemini/"} {
		if strings.HasPrefix(path, prefix) {
			endpoint := "/" + strings.TrimPrefix(path, prefix)
			if strings.HasPrefix(endpoint, "/v1/") {
				endpoint = strings.TrimPrefix(endpoint, "/v1")
			}
			if strings.HasPrefix(endpoint, "/v1beta/") {
				endpoint = strings.TrimPrefix(endpoint, "/v1beta")
			}
			return endpoint
		}
	}
	if strings.HasPrefix(path, "/guard/") {
		_, endpoint, found := strings.Cut(strings.TrimPrefix(path, "/guard/"), "/v1/")
		if !found {
			return ""
		}
		return normalizeBatchPath("/" + endpoint)
	}
	return path
}

func IsGenerationEndpoint(endpoint string) bool {
	if strings.HasSuffix(endpoint, ":batchGenerateContent") || strings.HasSuffix(endpoint, ":asyncBatchEmbedContent") {
		return true
	}
	switch endpoint {
	case "/systemone", "/chat/completions", "/completions", "/responses", "/responses/compact", "/messages", "/messages/batches", "/batches", "/embeddings", "/moderations", "/rerank", "/images/generations", "/images/edits", "/images/variations", "/audio/speech", "/audio/transcriptions", "/audio/translations", "/videos":
		return true
	default:
		return false
	}
}

func IsGenerationGatewayPath(path string) bool { return IsGenerationEndpoint(ProviderEndpoint(path)) }

// IsBatchGatewayPath includes job control and file transfers, not only creates.
func IsBatchGatewayPath(path string) bool {
	endpoint := ProviderEndpoint(path)
	return endpoint == "/batches" || strings.HasPrefix(endpoint, "/batches/") || endpoint == "/messages/batches" || strings.HasPrefix(endpoint, "/messages/batches/") || endpoint == "/files" || strings.HasPrefix(endpoint, "/files/") || strings.HasPrefix(endpoint, "/upload/") || strings.HasPrefix(endpoint, "/download/") || strings.HasPrefix(endpoint, "/batch-uploads/") || strings.HasSuffix(endpoint, ":batchGenerateContent") || strings.HasSuffix(endpoint, ":asyncBatchEmbedContent")
}

// InputText and OutputText are shared by cloud security middleware so new
// native API routes cannot silently bypass its semantic classifiers.
func InputText(raw []byte) string {
	var google struct {
		Contents json.RawMessage `json:"contents"`
		Content  json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(raw, &google)
	if len(google.Contents) > 0 || (len(google.Content) > 0 && bytes.Contains(google.Content, []byte(`"parts"`))) {
		return batchInputText(raw, providers.ProviderGemini)
	}
	return extractInputText(raw)
}
func OutputText(raw []byte) string {
	if !json.Valid(raw) {
		var parts []string
		var deltaText strings.Builder
		_ = providers.ReadSSE(context.Background(), bytes.NewReader(raw), func(frame providers.SSEFrame) error {
			found := false
			mapStreamDeltas(frame.Data, func(_, text string) string { found = true; deltaText.WriteString(text); return text })
			if !found {
				mapNativeOutput(frame.Data, func(text string) string {
					if text != "" {
						parts = append(parts, text)
					}
					return text
				})
			}
			return nil
		})
		if deltaText.Len() > 0 {
			parts = append(parts, deltaText.String())
		}
		return strings.Join(parts, "\n")
	}
	var parts []string
	mapBatchOutput(raw, providers.ProviderOpenAI, func(text string) string {
		if text != "" {
			parts = append(parts, text)
		}
		return text
	})
	var google struct {
		Candidates json.RawMessage `json:"candidates"`
	}
	_ = json.Unmarshal(raw, &google)
	if len(parts) == 0 {
		mapGoogleContent(raw, func(text string) string {
			if text != "" {
				parts = append(parts, text)
			}
			return text
		})
	}
	return strings.Join(parts, "\n")
}
