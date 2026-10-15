package observability

import (
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/google/uuid"
	"testing"
)

func TestRecordThreatsUsesEffectivePolicyAction(t *testing.T) {
	for _, action := range []security.Action{security.ActionPass, security.ActionWarn, security.ActionMask, security.ActionBlock} {
		t.Run(string(action), func(t *testing.T) {
			r := &Recorder{threatCh: make(chan *ThreatEvent, 1)}
			result := &security.ScanResult{
				Findings:        []security.Finding{{DetectorName: "jailbreak", Action: security.ActionBlock}},
				DetectorActions: map[string]security.Action{"jailbreak": action},
			}
			r.RecordThreats(uuid.New(), uuid.New(), uuid.New(), result, "", "", "")
			if got := (<-r.threatCh).Action; got != string(action) {
				t.Fatalf("recorded %s, want %s", got, action)
			}
		})
	}
	// Older/non-step callers preserve their explicitly supplied action.
	r := &Recorder{threatCh: make(chan *ThreatEvent, 1)}
	r.RecordThreats(uuid.New(), uuid.New(), uuid.New(), &security.ScanResult{Findings: []security.Finding{{Action: security.ActionMask}}}, "", "", "")
	if got := (<-r.threatCh).Action; got != "mask" {
		t.Fatal(got)
	}
}
