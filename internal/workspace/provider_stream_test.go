package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/google/uuid"
)

func TestWorkspaceCompatibleProviderStreaming(t *testing.T) {
	cases := []struct {
		provider providers.Provider
		env      string
		model    string
		wire     string
		client   func(string) providers.Client
	}{
		{providers.ProviderOpenRouter, "OPENROUTER_API_KEY", "openrouter/x-ai/grok-4.7", "x-ai/grok-4.7", func(base string) providers.Client { return providers.NewOpenRouterClientWithBaseURL(base) }},
		{providers.ProviderXAI, "XAI_API_KEY", "xai/grok-4.7", "grok-4.7", func(base string) providers.Client { return providers.NewXAIClientWithBaseURL(base) }},
		{providers.ProviderGroq, "GROQ_API_KEY", "openai/gpt-oss-120b", "openai/gpt-oss-120b", func(base string) providers.Client { return providers.NewGroqClientWithBaseURL(base) }},
		{providers.ProviderDeepSeek, "DEEPSEEK_API_KEY", "deepseek-flash", "deepseek-flash", func(base string) providers.Client { return providers.NewDeepSeekClientWithBaseURL(base) }},
	}
	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			t.Setenv(tc.env, "workspace-provider-key")
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model  string `json:"model"`
					Stream bool   `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.URL.Path != "/chat/completions" || body.Model != tc.wire || !body.Stream || r.Header.Get("Authorization") != "Bearer workspace-provider-key" {
					t.Errorf("wrong provider request: path=%s model=%s stream=%v", r.URL.Path, body.Model, body.Stream)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n")
			}))
			defer upstream.Close()
			registry := providers.NewRegistry()
			registry.Register(tc.client(upstream.URL))
			h := NewHandler(nil, registry, nil)
			out := httptest.NewRecorder()
			result, err := h.streamProvider(context.Background(), out, out, uuid.New(), tc.provider, tc.model, []providers.Message{{Role: "user", Content: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			if result.content != "Hello world" || result.promptTokens != 11 || result.completionTokens != 5 || result.finishReason != "stop" {
				t.Fatalf("stream content/usage lost: %+v", result)
			}
			if strings.Count(out.Body.String(), "event: token") != 2 {
				t.Fatalf("workspace did not emit incremental tokens: %s", out.Body.String())
			}
		})
	}
}
