package overlay_test

import (
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security/overlay"
)

func TestOverlaySnapshotValidate(t *testing.T) {
	f := 0.5
	strat := "block"
	piiAct := "mask"

	cases := []struct {
		name    string
		snap    overlay.OverlaySnapshot
		wantErr string // substring; empty = must succeed
	}{
		{
			name: "empty snapshot with current schema version",
			snap: overlay.OverlaySnapshot{SchemaVersion: overlay.CurrentSchemaVersion},
		},
		{
			name:    "wrong schema version",
			snap:    overlay.OverlaySnapshot{SchemaVersion: 99},
			wantErr: "unsupported schema version",
		},
		{
			name: "valid detector overrides",
			snap: overlay.OverlaySnapshot{
				SchemaVersion: overlay.CurrentSchemaVersion,
				DetectorOverrides: overlay.DetectorOverrides{
					Injection: &overlay.DetectorOverride{Threshold: &f, Strategy: &strat},
					PII:       &overlay.PIIOverride{Action: &piiAct},
				},
			},
		},
		{
			name: "out-of-range threshold rejected",
			snap: overlay.OverlaySnapshot{
				SchemaVersion: overlay.CurrentSchemaVersion,
				DetectorOverrides: overlay.DetectorOverrides{
					Injection: &overlay.DetectorOverride{Threshold: ptrF(1.5)},
				},
			},
			wantErr: "threshold must be in [0,1]",
		},
		{
			name: "invalid strategy for text-threat detector",
			snap: overlay.OverlaySnapshot{
				SchemaVersion: overlay.CurrentSchemaVersion,
				DetectorOverrides: overlay.DetectorOverrides{
					Injection: &overlay.DetectorOverride{Strategy: ptrS("mask")},
				},
			},
			wantErr: "strategy must be one of",
		},
		{
			name: "secrets may use mask strategy",
			snap: overlay.OverlaySnapshot{
				SchemaVersion: overlay.CurrentSchemaVersion,
				DetectorOverrides: overlay.DetectorOverrides{
					Secrets: &overlay.DetectorOverride{Strategy: ptrS("mask")},
				},
			},
		},
		{
			name: "pattern missing name rejected",
			snap: overlay.OverlaySnapshot{
				SchemaVersion: overlay.CurrentSchemaVersion,
				AdditionalPatterns: []overlay.PatternRule{
					{PatternType: "regex", Pattern: "foo"},
				},
			},
			wantErr: "name is required",
		},
		{
			name: "pattern with unknown type rejected",
			snap: overlay.OverlaySnapshot{
				SchemaVersion: overlay.CurrentSchemaVersion,
				AdditionalPatterns: []overlay.PatternRule{
					{Name: "x", PatternType: "fuzzy", Pattern: "foo"},
				},
			},
			wantErr: "pattern_type must be",
		},
		{
			name: "access rule missing value rejected",
			snap: overlay.OverlaySnapshot{
				SchemaVersion: overlay.CurrentSchemaVersion,
				AdditionalAccessRules: []overlay.AccessRule{
					{RuleType: "ip_allowlist"},
				},
			},
			wantErr: "value is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.snap.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func ptrF(v float64) *float64 { return &v }
func ptrS(v string) *string   { return &v }
