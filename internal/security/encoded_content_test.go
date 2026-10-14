package security_test

import (
	"context"
	"encoding/base64"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"testing"
)

func TestEncodedInspectionPreservesCallerRequest(t *testing.T) {
	p := security.DefaultProfile()
	e := security.NewEngine(detection.NewPIIDetector(), detection.NewSecretsDetector(), detection.NewInjectionDetector(), detection.NewJailbreakDetector(), detection.NewIndirectInjectionDetector())
	for _, text := range []string{"https://example.test/search?q=lampe%20til%20natbordet", base64.StdEncoding.EncodeToString([]byte("An ordinary bedside lamp with an adjustable shade."))} {
		req := &security.ScanRequest{Content: text, Steps: p.Input, Canonicalize: true, Role: security.RoleRetrieval}
		result := e.Scan(context.Background(), req)
		if req.Content != text {
			t.Fatal("scanner mutated the caller's content")
		}
		if result.ShouldBlock || result.SanitizedContent != text {
			t.Fatal("inspection-only decoded text escaped into forwarded content")
		}
	}
}
