package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatOutputLimitWireContracts(t *testing.T) {
	for _, provider := range []Provider{ProviderOpenAI, ProviderOpenRouter} {
		t.Run(string(provider), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				key, deprecated := "max_tokens", "max_completion_tokens"
				if provider == ProviderOpenAI {
					key, deprecated = deprecated, key
				}
				if string(body[key]) != "8192" || body[deprecated] != nil {
					t.Errorf("output allowance lost for %s", provider)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"model":"test","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`))
			}))
			defer server.Close()
			client := NewOpenAIClientWithBaseURL(server.URL)
			client.provider = provider
			limit := 8192
			if _, err := client.Chat(t.Context(), &ChatRequest{Model: "test", MaxTokens: &limit, Messages: []Message{{Role: "user", Content: "hello"}}}, "synthetic-test-key"); err != nil {
				t.Fatal(err)
			}
		})
	}
	limit := 8192
	if config := bedrockInferenceConfig(&ChatRequest{MaxTokens: &limit}); config.MaxTokens == nil || *config.MaxTokens != 8192 {
		t.Fatal("Bedrock output allowance missing")
	}
}
