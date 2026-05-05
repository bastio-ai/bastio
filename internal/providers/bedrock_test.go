package providers

import "testing"

func TestBedrockName(t *testing.T) {
	if NewBedrockClient().Name() != ProviderBedrock {
		t.Fatal("bedrock client must report ProviderBedrock")
	}
}

func TestBedrock_ConvertSystemPromptExtraction(t *testing.T) {
	c := NewBedrockClient()
	req := &ChatRequest{
		Model: "anthropic.claude-3-5-sonnet-20241022-v2:0",
		Messages: []Message{
			{Role: "system", Content: "be concise"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
	}

	model, msgs, system, err := c.convert(req)
	if err != nil {
		t.Fatalf("convert err: %v", err)
	}
	if model != req.Model {
		t.Fatalf("model: want %q got %q", req.Model, model)
	}
	if len(system) != 1 {
		t.Fatalf("system blocks: want 1 got %d", len(system))
	}
	if len(msgs) != 2 {
		t.Fatalf("messages: want 2 (system stripped) got %d", len(msgs))
	}
}

func TestBedrock_ConvertMissingModel(t *testing.T) {
	c := NewBedrockClient()
	_, _, _, err := c.convert(&ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error when model id is missing")
	}
}

func TestBedrock_ClientFor_RejectsMalformedKey(t *testing.T) {
	c := NewBedrockClient()
	if _, err := c.clientFor(t.Context(), "not-well-formed"); err == nil {
		t.Fatal("expected error on malformed apiKey")
	}
}
