package detection

import (
	"context"
	"strings"
	"testing"
)

// Test fixtures below split well-known token prefixes (e.g. "gh"+"p_")
// across string concatenations so source-level secret scanners (GitHub
// Push Protection, gitleaks) don't false-positive on this file. Runtime
// values are unchanged and the detector still matches them.

func TestSecretsDetector_AWSKey(t *testing.T) {
	d := NewSecretsDetector()
	findings, _ := d.Detect(context.Background(), "my key is AKIAIOSFODNN7EXAMPLE please help")
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].MatchedPattern != "aws_access_key" {
		t.Errorf("want aws_access_key, got %s", findings[0].MatchedPattern)
	}
}

func TestSecretsDetector_GitHubPAT(t *testing.T) {
	d := NewSecretsDetector()
	findings, _ := d.Detect(context.Background(), "token: gh"+"p_1234567890abcdefghijklmnopqrstuvwxyz")
	if len(findings) == 0 {
		t.Fatal("expected github_pat finding")
	}
}

func TestSecretsDetector_OpenAIKey(t *testing.T) {
	d := NewSecretsDetector()
	findings, _ := d.Detect(context.Background(), "sk-abcdef1234567890ABCDEF please use this key")
	if len(findings) == 0 {
		t.Fatal("expected openai_api_key finding")
	}
}

func TestSecretsDetector_MaskReplacesSecrets(t *testing.T) {
	d := NewSecretsDetector()
	masked := d.Mask("key=AKIAIOSFODNN7EXAMPLE and another gh" + "p_1234567890abcdefghijklmnopqrstuvwxyz")
	if strings.Contains(masked, "AKIA") {
		t.Errorf("AWS key leaked after mask: %q", masked)
	}
	if strings.Contains(masked, "gh"+"p_1234567890") {
		t.Errorf("GitHub PAT leaked after mask: %q", masked)
	}
}

func TestSecretsDetector_LowEntropyAssignmentIgnored(t *testing.T) {
	d := NewSecretsDetector()
	// "hello world example text" is low entropy; shouldn't count as
	// a secret even though the shape matches `SECRET=...`.
	findings, _ := d.Detect(context.Background(), "SECRET=helloworldhelloworldhelloworld")
	for _, f := range findings {
		if f.MatchedPattern == "generic_secret_assignment" {
			t.Errorf("low-entropy value shouldn't fire generic secret rule: %+v", f)
		}
	}
}

func TestSecretsDetector_HighEntropyAssignmentFires(t *testing.T) {
	d := NewSecretsDetector()
	// Random-looking value.
	findings, _ := d.Detect(context.Background(), "SECRET=Xk7pQ2vL9mN4aT6bZ1cYwRf8d")
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "generic_secret_assignment" {
			found = true
		}
	}
	if !found {
		t.Error("expected generic secret assignment to fire on high-entropy value")
	}
}

func TestSecretsDetector_PrivateKeyHeader(t *testing.T) {
	d := NewSecretsDetector()
	findings, _ := d.Detect(context.Background(), "here is my key\n-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n")
	if len(findings) == 0 {
		t.Fatal("expected private_key finding")
	}
}

func TestSecretsDetector_ExtensionCatalog(t *testing.T) {
	d := NewSecretsDetector()
	azureKey := "AccountKey=" + strings.Repeat("A", 86) + "=="
	npmTok := "npm_" + strings.Repeat("a", 36)
	sendgrid := "SG." + strings.Repeat("x", 22) + "." + strings.Repeat("y", 43)
	pypi := "pypi-AgEIcHlwaS5vcmcC" + strings.Repeat("z", 40)
	tailscale := "tskey-auth-" + strings.Repeat("a", 16) + "-" + strings.Repeat("b", 32)
	twilio := "AC" + strings.Repeat("a", 32)
	discord := "https://discord.com/api/webhooks/123456789012345678/" + strings.Repeat("W", 60)
	heroku := "heroku_api_key=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	cases := []struct {
		content string
		kind    string
	}{
		{"sts key ASIAIOSFODNN7EXAMPLE in logs", "aws_access_key"},
		{azureKey + " in the connection string", "azure_storage_key"},
		{"uri mongodb+srv://user:pass@cluster0.mongodb.net/app", "mongodb_uri"},
		{"sid " + twilio, "twilio_sid"},
		{"key " + sendgrid, "sendgrid_key"},
		{"hook " + discord, "discord_webhook"},
		{"auth " + npmTok, "npm_token"},
		{"auth " + tailscale, "tailscale_key"},
		{"auth " + pypi, "pypi_token"},
		{heroku, "heroku_api_key"},
	}
	for _, tt := range cases {
		t.Run(tt.kind, func(t *testing.T) {
			findings, err := d.Detect(context.Background(), tt.content)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			found := false
			for _, f := range findings {
				if f.MatchedPattern == tt.kind {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("want %s in %+v", tt.kind, findings)
			}
			masked := d.Mask(tt.content)
			if masked == tt.content {
				t.Errorf("mask left content unchanged for %s", tt.kind)
			}
		})
	}
}

func TestSecretsDetector_AnthropicNotClassifiedAsOpenAI(t *testing.T) {
	d := NewSecretsDetector()
	key := "sk-ant-" + strings.Repeat("a", 40)
	findings, _ := d.Detect(context.Background(), "key "+key)
	for _, f := range findings {
		if f.MatchedPattern == "openai_api_key" {
			t.Fatalf("sk-ant key classified as openai: %+v", findings)
		}
	}
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "anthropic_api_key" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected anthropic_api_key, got %+v", findings)
	}
}
