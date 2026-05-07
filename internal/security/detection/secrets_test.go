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
