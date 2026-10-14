package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ListedModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Model discovery shares provider transports and credential resolution with
// inference. A reported model still needs a successful inference check.
func DiscoverChatModels(ctx context.Context, registry *Registry, provider Provider, key string) ([]ListedModel, string, error) {
	client, err := registry.Native(provider)
	if err != nil {
		return []ListedModel{}, "catalog", nil
	}
	response, err := client.Forward(ctx, &NativeRequest{Method: http.MethodGet, Path: "/models", Headers: http.Header{"Accept": []string{"application/json"}}}, key)
	if err != nil {
		return nil, "provider", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return nil, "provider", err
	}
	if response.StatusCode != 200 {
		return nil, "provider", &ProviderError{Provider: provider, Status: response.StatusCode, Body: raw, Headers: response.Header.Clone()}
	}
	models, err := ParseChatModels(provider, raw)
	return models, "provider", err
}

func ParseChatModels(provider Provider, raw []byte) ([]ListedModel, error) {
	var input struct {
		Data []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			DisplayName  string `json:"display_name"`
			Architecture struct {
				Outputs []string `json:"output_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if len(raw) > 8<<20 || json.Unmarshal(raw, &input) != nil || input.Data == nil {
		return nil, fmt.Errorf("invalid provider model list")
	}
	out := []ListedModel{}
	seen := map[string]bool{}
	for _, item := range input.Data {
		if item.ID == "" || seen[item.ID] {
			continue
		}
		if provider == ProviderOpenAI {
			id := strings.ToLower(item.ID)
			chat := strings.HasPrefix(id, "gpt-") || strings.HasPrefix(id, "chatgpt-") || strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") || strings.HasPrefix(id, "o4")
			if !chat {
				continue
			}
			for _, special := range []string{"audio", "realtime", "transcrib", "tts", "image", "codex", "embedding", "instruct", "search", "-pro", "gpt-live"} {
				if strings.Contains(id, special) {
					chat = false
				}
			}
			if !chat {
				continue
			}
		}
		if len(item.Architecture.Outputs) > 0 {
			text := false
			for _, format := range item.Architecture.Outputs {
				if format == "text" {
					text = true
				}
			}
			if !text {
				continue
			}
		}
		name := item.Name
		if name == "" {
			name = item.DisplayName
		}
		out = append(out, ListedModel{item.ID, name})
		seen[item.ID] = true
	}
	return out, nil
}

// Reasoning controls vary by model; do not send "none" to every GPT-6 model.
// Snapshots retain the alias capability. Unrecognized families use defaults.
func HelloReasoningEffort(model string) string {
	matches := func(id string) bool {
		if model == id {
			return true
		}
		if !strings.HasPrefix(model, id+"-") {
			return false
		}
		_, err := time.Parse("2006-01-02", strings.TrimPrefix(model, id+"-"))
		return err == nil
	}
	for _, id := range []string{"gpt-6-luna", "gpt-6-sol", "gpt-5.6", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra"} {
		if matches(id) {
			return "none"
		}
	}
	for _, id := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
		if matches(id) {
			return "low"
		}
	}
	for _, id := range []string{"gpt-5", "gpt-5-nano", "gpt-5-mini"} {
		if matches(id) {
			return "minimal"
		}
	}
	return ""
}
