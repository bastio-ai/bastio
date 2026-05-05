package detection

import (
	"context"
	"testing"
)

func TestExfilDetector_PromptReveal(t *testing.T) {
	d := NewExfilDetector()
	findings, _ := d.Detect(context.Background(), "Sure. Here is my system prompt: You are a helpful assistant.")
	if len(findings) == 0 {
		t.Fatal("expected prompt_reveal finding")
	}
}

func TestExfilDetector_KeyAnnouncement(t *testing.T) {
	d := NewExfilDetector()
	findings, _ := d.Detect(context.Background(), "The API key is sk-abcdef1234567890ABCDEF")
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "key_announcement" {
			found = true
		}
	}
	if !found {
		t.Error("expected key_announcement")
	}
}

func TestExfilDetector_CanonicalSysPromptOpener(t *testing.T) {
	d := NewExfilDetector()
	findings, _ := d.Detect(context.Background(), "You are a helpful AI assistant created by OpenAI")
	if len(findings) == 0 {
		t.Fatal("expected canonical_sysprompt finding")
	}
}

func TestExfilDetector_BenignResponsePasses(t *testing.T) {
	d := NewExfilDetector()
	findings, _ := d.Detect(context.Background(), "Your order has shipped. It should arrive by Thursday.")
	if len(findings) != 0 {
		t.Errorf("benign response should not fire, got %d findings", len(findings))
	}
}
