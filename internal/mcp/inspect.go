package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
)

var dangerousAction = regexp.MustCompile(`(?i)(\brm\s+(-[a-z]*r[a-z]*f|-[a-z]*f[a-z]*r)\b|\b(drop|truncate)\s+(table|database)\b|\bmkfs\b|\b(curl|wget)\b[^\n]*\|\s*(sh|bash)\b|/etc/shadow|\.\.[/\\]\.\.[/\\])`)

// Inspect is shared by the actual proxy and the dashboard sandbox. No tool is executed.
func Inspect(ctx context.Context, engine *security.Engine, req InspectRequest) Decision {
	start := time.Now()
	d := Decision{Action: "allowed", Categories: []string{}, Arguments: req.Arguments}
	if slices.Contains(req.Policy.BlockedTools, req.ToolName) || (len(req.Policy.AllowedTools) > 0 && !slices.Contains(req.Policy.AllowedTools, req.ToolName)) {
		d.Action = "blocked"
		d.Categories = append(d.Categories, "tool_policy")
	} else if containsDangerousAction(req.Arguments, 0) {
		d.Action = "blocked"
		d.Categories = append(d.Categories, "destructive_action")
	} else if engine != nil {
		result := engine.Scan(ctx, &security.ScanRequest{Content: string(req.Arguments), PIIAction: security.ActionMask})
		if result != nil {
			for _, category := range result.ThreatTypes {
				d.Categories = append(d.Categories, string(category))
			}
			if result.ShouldBlock {
				d.Action = "blocked"
			} else if result.SanitizedContent != "" && result.SanitizedContent != string(req.Arguments) {
				if json.Valid([]byte(result.SanitizedContent)) {
					d.Action = "sanitized"
					d.Arguments = json.RawMessage(result.SanitizedContent)
				} else {
					d.Action = "blocked"
					d.Categories = append(d.Categories, "invalid_sanitized_arguments")
				}
			} else if len(result.Findings) > 0 {
				d.Action = "warned"
			}
		}
	}
	if d.Action == "blocked" {
		d.Arguments = nil
	}
	d.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	return d
}

// Inspect decoded string values too: JSON unicode escapes must not bypass
// command/SQL checks. Deeply nested arguments fail closed instead of recursing.
func containsDangerousAction(raw json.RawMessage, depth int) bool {
	if depth > 32 {
		return true
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case '"':
		var value string
		if json.Unmarshal(raw, &value) == nil {
			return dangerousAction.MatchString(value)
		}
	case '{':
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) == nil {
			for _, value := range values {
				if containsDangerousAction(value, depth+1) {
					return true
				}
			}
		}
	case '[':
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) == nil {
			for _, value := range values {
				if containsDangerousAction(value, depth+1) {
					return true
				}
			}
		}
	}
	return false
}
