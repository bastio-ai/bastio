package security

import (
	"os"
	"strings"
	"testing"
)

// The code default and the column default for tool_injection_strategy
// must agree, so a profile behaves the same whether it comes from
// DefaultProfile() (no row) or from a freshly inserted row.
func TestToolInjectionStrategyDefaultsAgree(t *testing.T) {
	if got := DefaultProfile().ToolInjectionStrategy; got != ActionWarn {
		t.Fatalf("DefaultProfile().ToolInjectionStrategy = %q, want warn", got)
	}
	if got := normalizeTextThreatStrategy("", ActionWarn); got != ActionWarn {
		t.Fatalf("empty column normalizes to %q, want warn", got)
	}
	for in, want := range map[string]Action{"block": ActionBlock, "warn": ActionWarn, "log_only": ActionLogOnly, "mask": ActionWarn, "bogus": ActionWarn} {
		if got := normalizeTextThreatStrategy(in, ActionWarn); got != want {
			t.Errorf("normalizeTextThreatStrategy(%q) = %q, want %q", in, got, want)
		}
	}
	sql, err := os.ReadFile("../../pkg/database/migrations/050_tool_injection_strategy.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if !strings.Contains(string(sql), "tool_injection_strategy TEXT NOT NULL DEFAULT 'warn'") {
		t.Fatal("migration 050 column default must be 'warn' to match DefaultProfile()")
	}
	// The shared knob that workspace ingest reads stays strict.
	if got := DefaultProfile().IndirectInjectionStrategy; got != ActionBlock {
		t.Fatalf("indirect_injection_strategy default changed to %q", got)
	}
}
