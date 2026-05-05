package security

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bastio-ai/bastio/internal/security/overlay"
	"github.com/bastio-ai/bastio/internal/security/plugin"
)

// RunOverlayPlugins executes every plugin detector listed on the
// active overlay (taken from ctx) against content and returns the
// collected findings. Plugin failures — registry lookups, factory
// errors, panics inside Detect — are recovered and logged; a broken
// plugin can never fail the primary detection pipeline.
//
// Returns nil when no overlay is attached to the context, or when the
// overlay references no plugin detectors. The common case on the hot
// path is the nil return — most tenants won't use plugins.
//
// Callers merge the returned findings into their per-message response
// and upgrade the overall action when any finding carries
// ActionBlock. See detect_handler.go for the reference integration.
func RunOverlayPlugins(ctx context.Context, content string) []Finding {
	active, ok := overlay.FromContext(ctx)
	if !ok || active.Snapshot == nil || len(active.Snapshot.PluginDetectors) == 0 {
		return nil
	}
	return runOverlayPluginsWith(ctx, content, active.Snapshot.PluginDetectors, plugin.Default)
}

// runOverlayPluginsWith is the testable core. Accepts an explicit
// registry so tests can inject fakes without touching the
// process-wide plugin.Default.
func runOverlayPluginsWith(
	ctx context.Context,
	content string,
	refs []overlay.PluginDetectorRef,
	reg *plugin.Registry,
) []Finding {
	if reg == nil || len(refs) == 0 {
		return nil
	}
	var out []Finding
	for _, ref := range refs {
		out = append(out, runOnePlugin(ctx, content, ref, reg)...)
	}
	return out
}

// runOnePlugin builds and invokes a single plugin, recovering from
// panics. Returning a naked slice allows the caller to append freely
// — nil is the "nothing to merge" signal.
func runOnePlugin(
	ctx context.Context,
	content string,
	ref overlay.PluginDetectorRef,
	reg *plugin.Registry,
) (findings []Finding) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Warn("overlay plugin: panic during Detect",
				"plugin", ref.Name, "panic", fmt.Sprint(rec))
			findings = nil
		}
	}()
	det, err := reg.Build(ref.Name, ref.Config)
	if err != nil {
		slog.Warn("overlay plugin: build failed", "plugin", ref.Name, "error", err)
		return nil
	}
	raw, err := det.Detect(ctx, content)
	if err != nil {
		slog.Warn("overlay plugin: detect failed", "plugin", ref.Name, "error", err)
		return nil
	}
	for _, item := range raw {
		f, ok := item.(Finding)
		if !ok {
			// Plugin emitted something we don't recognise as a Finding.
			// Drop it silently — logging at WARN here would spam once
			// per request for a misbehaving plugin.
			continue
		}
		if f.DetectorName == "" {
			f.DetectorName = "plugin:" + ref.Name
		}
		findings = append(findings, f)
	}
	return findings
}

// BlockActionFromFindings returns true if any finding carries
// ActionBlock — signal to the caller that the overall verdict
// should be upgraded to block. Kept here so the rule is in one
// place and both the detect handler and any future gateway
// integration apply it consistently.
func BlockActionFromFindings(findings []Finding) bool {
	for _, f := range findings {
		if f.Action == ActionBlock {
			return true
		}
	}
	return false
}
