package security_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/overlay"
	"github.com/bastio-ai/bastio/internal/security/plugin"
)

// testDetector is a minimal plugin.Detector the tests use to drive
// RunOverlayPlugins without depending on real detector factories.
type testDetector struct {
	name     string
	findings []any
	err      error
	panicMsg string
}

func (d *testDetector) Name() string { return d.name }
func (d *testDetector) Detect(_ context.Context, _ string) ([]any, error) {
	if d.panicMsg != "" {
		panic(d.panicMsg)
	}
	return d.findings, d.err
}

func TestRunOverlayPlugins_NoContextReturnsNil(t *testing.T) {
	got := security.RunOverlayPlugins(context.Background(), "hello")
	if got != nil {
		t.Fatalf("expected nil without overlay, got %+v", got)
	}
}

func TestRunOverlayPlugins_NoPluginsListedReturnsNil(t *testing.T) {
	ctx := overlay.WithActive(context.Background(), &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
	}, overlay.Identity{VersionID: uuid.New()})

	got := security.RunOverlayPlugins(ctx, "hello")
	if got != nil {
		t.Fatalf("expected nil when no plugins listed, got %+v", got)
	}
}

func TestRunOverlayPlugins_FindingFromPluginRetained(t *testing.T) {
	// The process-wide plugin.Default is shared across the binary
	// (and tests, unfortunately). Use a unique name per test to avoid
	// collisions with concurrently-running test cases.
	name := "test.plugin.finding_retained"
	if err := plugin.Register(name, func(_ json.RawMessage) (plugin.Detector, error) {
		return &testDetector{
			name: name,
			findings: []any{
				security.Finding{
					DetectorName: "", // empty on purpose — runner fills with "plugin:<name>"
					Action:       security.ActionWarn,
					Score:        0.5,
					Severity:     security.Severity("medium"),
					Message:      "from plugin",
				},
			},
		}, nil
	}); err != nil {
		t.Fatalf("register plugin: %v", err)
	}
	defer func() { _ = plugin.Default.Unregister(name) }()

	ctx := overlay.WithActive(context.Background(), &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		PluginDetectors: []overlay.PluginDetectorRef{
			{Name: name},
		},
	}, overlay.Identity{VersionID: uuid.New()})

	got := security.RunOverlayPlugins(ctx, "any content")
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].DetectorName != "plugin:"+name {
		t.Fatalf("detector_name = %q, want plugin:%s", got[0].DetectorName, name)
	}
	if got[0].Action != security.ActionWarn {
		t.Fatalf("action = %q, want warn", got[0].Action)
	}
}

func TestRunOverlayPlugins_PanicRecoveredNoOtherPluginsAffected(t *testing.T) {
	crasher := "test.plugin.crasher"
	good := "test.plugin.good"
	if err := plugin.Register(crasher, func(_ json.RawMessage) (plugin.Detector, error) {
		return &testDetector{name: crasher, panicMsg: "boom"}, nil
	}); err != nil {
		t.Fatalf("register crasher: %v", err)
	}
	defer func() { _ = plugin.Default.Unregister(crasher) }()

	if err := plugin.Register(good, func(_ json.RawMessage) (plugin.Detector, error) {
		return &testDetector{
			name: good,
			findings: []any{
				security.Finding{Action: security.ActionWarn, Message: "still good"},
			},
		}, nil
	}); err != nil {
		t.Fatalf("register good: %v", err)
	}
	defer func() { _ = plugin.Default.Unregister(good) }()

	ctx := overlay.WithActive(context.Background(), &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		PluginDetectors: []overlay.PluginDetectorRef{
			{Name: crasher},
			{Name: good},
		},
	}, overlay.Identity{VersionID: uuid.New()})

	got := security.RunOverlayPlugins(ctx, "any")
	if len(got) != 1 {
		t.Fatalf("expected 1 finding (from good plugin), got %d", len(got))
	}
	if got[0].Message != "still good" {
		t.Fatalf("finding came from the wrong plugin: %+v", got[0])
	}
}

func TestRunOverlayPlugins_BuildErrorSwallowed(t *testing.T) {
	broken := "test.plugin.build_error"
	errFactory := errors.New("factory said no")
	if err := plugin.Register(broken, func(_ json.RawMessage) (plugin.Detector, error) {
		return nil, errFactory
	}); err != nil {
		t.Fatalf("register broken: %v", err)
	}
	defer func() { _ = plugin.Default.Unregister(broken) }()

	ctx := overlay.WithActive(context.Background(), &overlay.OverlaySnapshot{
		SchemaVersion: overlay.CurrentSchemaVersion,
		PluginDetectors: []overlay.PluginDetectorRef{
			{Name: broken},
		},
	}, overlay.Identity{VersionID: uuid.New()})

	got := security.RunOverlayPlugins(ctx, "any")
	if got != nil {
		t.Fatalf("expected nil on build error, got %+v", got)
	}
}

func TestBlockActionFromFindings(t *testing.T) {
	cases := []struct {
		name     string
		findings []security.Finding
		want     bool
	}{
		{"empty", nil, false},
		{"warn only", []security.Finding{{Action: security.ActionWarn}}, false},
		{"one block among warnings", []security.Finding{
			{Action: security.ActionWarn},
			{Action: security.ActionBlock},
			{Action: security.ActionLogOnly},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := security.BlockActionFromFindings(tc.findings); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
