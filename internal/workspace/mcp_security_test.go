package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/google/uuid"
)

func TestMCPInspectionSafeDiagnostics(t *testing.T) {
	cid := uuid.New()
	profile := security.DefaultProfile()
	profile.PIIAction = security.ActionBlock
	profile.CanonicalizeEnabled = false
	profile.JailbreakThreshold = 0.5
	engine := security.NewEngine(detection.NewPIIDetector(), detection.NewJailbreakDetector(), detection.NewIndirectInjectionDetector(), detection.NewInjectionDetector(), detection.NewSecretsDetector())
	for _, tc := range []struct{ name, raw, code, rule, secret string }{
		{"pii", `{"email":"private@example.test"}`, "policy_block", "pii:email", "private@example.test"},
		{"key", `{"private@example.test":"value"}`, "policy_block", "pii:email", "private@example.test"},
		{"numeric", `{"card":4111111111111111}`, "policy_block", "pii:credit_card", "4111111111111111"},
		{"encoded", `{"text":"` + strings.Repeat("Abcdef", 45) + ` contact private@example.test"}`, "policy_block", "jailbreak:structural.encoding_density", strings.Repeat("Abcdef", 45)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cats, err := inspectMCPPayload(context.Background(), engine, mcpProfileSnapshot{cid, &profile}, cid, json.RawMessage(tc.raw), security.RoleRetrieval, 0)
			if err == nil {
				t.Fatal("expected withheld content")
			}
			body := mcpSecurityFailure(err, cats, "result")
			encoded, _ := json.Marshal(body)
			if body["code"] != tc.code || body["remote_action"] != "completed" || !strings.Contains(string(encoded), tc.rule) {
				t.Fatalf("missing diagnostic %s", encoded)
			}
			if tc.name == "encoded" && !strings.Contains(string(encoded), `"blocking_detectors":["pii"]`) {
				t.Fatalf("warning confused with blocker: %s", encoded)
			}
			if strings.Contains(string(encoded), tc.secret) {
				t.Fatal("withheld content leaked")
			}
		})
	}
	profile.PIIAction = security.ActionMask
	_, cats, err := inspectMCPPayload(context.Background(), engine, mcpProfileSnapshot{cid, &profile}, cid, json.RawMessage(`{"card":4111111111111111}`), security.RoleUser, 0)
	body := mcpSecurityFailure(err, cats, "arguments")
	if body["code"] != "numeric_rewrite" || body["remote_action"] != "not_run" {
		t.Fatalf("numeric rewrite misreported %v", body)
	}
}
func TestMCPInspectionFailureDoesNotLeakScannerErrors(t *testing.T) {
	body := mcpSecurityFailure(errors.New("database password private-secret"), nil, "result")
	encoded, _ := json.Marshal(body)
	if body["code"] != "inspection_unavailable" || strings.Contains(string(encoded), "private-secret") {
		t.Fatal(string(encoded))
	}
	body = mcpSecurityFailure(errors.New("mcp content exceeds inspection limits"), nil, "result")
	if body["code"] != "inspection_limit" {
		t.Fatal(body)
	}
	decision := &IngestScanDecision{Categories: []string{"injection"}, Result: &security.ScanResult{Findings: []security.Finding{{DetectorName: "custom", MatchedPattern: "private-secret", MatchedContent: "private-secret", Message: "private-secret"}}}}
	body = mcpSecurityFailure(mcpBlockedInspection(decision, "policy_block", "Policy blocks content."), decision.Categories, "result")
	encoded, _ = json.Marshal(body)
	if strings.Contains(string(encoded), "private-secret") {
		t.Fatal("custom content leaked")
	}
}
