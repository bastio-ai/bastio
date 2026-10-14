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

func TestNativeToolPlanningAndContinuation(t *testing.T) {
	for _, name := range []Provider{ProviderOpenAI, ProviderAnthropic, ProviderGemini, ProviderOpenRouter} {
		t.Run(string(name), func(t *testing.T) {
			calls := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]json.RawMessage
				json.NewDecoder(r.Body).Decode(&body)
				if len(body["tools"]) == 0 {
					t.Error("function definitions missing")
				}
				if calls == 2 {
					raw, _ := json.Marshal(body)
					var decoded any
					json.Unmarshal(raw, &decoded)
					// Wire formats differ, but the approved call ID and protected result must survive.
					if !jsonContains(raw, "call-test") || !jsonContains(raw, "protected result") {
						t.Error("tool continuation missing call identity or output")
					}
				}
				switch name {
				case ProviderOpenAI:
					if r.URL.Path != "/responses" || string(body["store"]) != "false" || string(body["parallel_tool_calls"]) != "false" {
						t.Error("Responses privacy or parallel guard missing")
					}
					if calls == 1 {
						io.WriteString(w, `{"id":"response","model":"gpt-6.1-sol","status":"completed","output":[{"type":"reasoning","id":"reasoning-1","encrypted_content":"opaque-model-state","summary":[]},{"type":"function_call","call_id":"call-test","name":"bastio_search","arguments":"{\"query\":\"hello\"}"}],"usage":{"input_tokens":8,"output_tokens":4}}`)
					} else {
						if !jsonContains(body["input"], "opaque-model-state") {
							t.Error("reasoning state lost")
						}
						io.WriteString(w, `{"id":"response2","model":"gpt-6.1-sol","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Done"}]}],"usage":{"input_tokens":8,"output_tokens":4}}`)
					}
				case ProviderAnthropic:
					if !jsonContains(body["tool_choice"], "disable_parallel_tool_use") {
						t.Error("parallel guard missing")
					}
					io.WriteString(w, `{"id":"reply","model":"claude","role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"call-test","name":"bastio_search","input":{"query":"hello"}}],"usage":{"input_tokens":8,"output_tokens":4}}`)
				case ProviderGemini:
					io.WriteString(w, `{"modelVersion":"gemini","candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call-test","name":"bastio_search","args":{"query":"hello"}},"thoughtSignature":"opaque-gemini-signature"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":4}}`)
					if calls == 2 && !jsonContains(body["contents"], "opaque-gemini-signature") {
						t.Error("Gemini signature lost")
					}
				default:
					if string(body["parallel_tool_calls"]) != "false" {
						t.Error("parallel guard missing")
					}
					io.WriteString(w, `{"id":"reply","model":"openai/gpt","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call-test","type":"function","function":{"name":"bastio_search","arguments":"{\"query\":\"hello\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":4}}`)
				}
			}))
			defer ts.Close()
			var client Client
			switch name {
			case ProviderOpenAI:
				client = NewOpenAIClientWithBaseURL(ts.URL)
			case ProviderAnthropic:
				client = NewAnthropicClientWithBaseURL(ts.URL)
			case ProviderGemini:
				client = NewGeminiClientWithBaseURL(ts.URL)
			default:
				client = NewOpenRouterClientWithBaseURL(ts.URL)
			}
			max := 512
			req := &ChatRequest{Model: "test-model", Messages: []Message{{Role: "system", Content: "Help"}, {Role: "user", Content: "Search"}}, MaxTokens: &max, Tools: []ToolDefinition{{Name: "bastio_search", Description: "Search", Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)}}}
			first, err := client.Chat(context.Background(), req, "fixture-key")
			if err != nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != "bastio_search" || first.InputTokens != 8 {
				t.Fatalf("tool parse failed %v %+v", err, first)
			}
			req.Messages = append(req.Messages, Message{Role: "assistant", ToolCalls: first.ToolCalls, ProviderState: first.ProviderState}, Message{Role: "tool", ToolCallID: first.ToolCalls[0].ID, ToolName: first.ToolCalls[0].Name, Content: "protected result"})
			if _, err = client.Chat(context.Background(), req, "fixture-key"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func jsonContains(raw []byte, value string) bool { return strings.Contains(string(raw), value) }
