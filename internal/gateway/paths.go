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
	for _, prefix := range []string{"/openrouter/", "/xai/", "/openai/", "/anthropic/"} {
		if strings.HasPrefix(path, prefix) {
			return "/" + strings.TrimPrefix(path, prefix)
		}
	}
	if strings.HasPrefix(path, "/guard/") {
		_, endpoint, found := strings.Cut(strings.TrimPrefix(path, "/guard/"), "/v1/")
		if !found {
			return ""
		}
		return "/" + endpoint
	}
	return path
}

func IsGenerationEndpoint(endpoint string) bool {
	switch endpoint {
	case "/chat/completions", "/completions", "/responses", "/responses/compact", "/messages", "/messages/batches", "/batches", "/embeddings", "/moderations", "/rerank", "/images/generations", "/images/edits", "/images/variations", "/audio/speech", "/audio/transcriptions", "/audio/translations", "/videos":
		return true
	default:
		return false
	}
}

func IsGenerationGatewayPath(path string) bool { return IsGenerationEndpoint(ProviderEndpoint(path)) }

// InputText and OutputText are shared by cloud security middleware so new
// native API routes cannot silently bypass its semantic classifiers.
func InputText(raw []byte) string { return extractInputText(raw) }
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
	mapNativeOutput(raw, func(text string) string {
		if text != "" {
			parts = append(parts, text)
		}
		return text
	})
	return strings.Join(parts, "\n")
}
