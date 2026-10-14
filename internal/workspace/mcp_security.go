package workspace

import (
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

// Inspection diagnostics contain taxonomy identifiers only, never matched
// content, custom expressions, payload field names or scanner error details.
type mcpInspectionError struct {
	Code              string   `json:"code"`
	Reason            string   `json:"reason"`
	Categories        []string `json:"categories"`
	Rules             []string `json:"rules"`
	BlockingDetectors []string `json:"blocking_detectors"`
}

func (e *mcpInspectionError) Error() string { return e.Reason }

var safeMCPRule = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,80}$`)

func mcpBlockedInspection(decision *IngestScanDecision, code, reason string) error {
	e := &mcpInspectionError{Code: code, Reason: reason, Categories: slices.Clone(decision.Categories), Rules: []string{}}
	if decision.Result != nil {
		if decision.Result.RewriteUnavailable {
			e.Code = "encoded_rewrite"
			e.Reason = "Sensitive data uses URL encoding and could not be safely rewritten. The content was withheld."
		}
		e.BlockingDetectors = uniqueMCPDiagnostics(slices.Clone(decision.Result.BlockingDetectors))
		if !decision.Result.RewriteUnavailable && code == "policy_block" && slices.Contains(e.BlockingDetectors, "pii") {
			e.Reason = "Personal data (PII) detection is configured to block this content."
		}
		for _, f := range decision.Result.Findings {
			rule := f.SubCategory
			if rule == "" {
				rule = f.MatchedPattern
			}
			// Only built-in detector taxonomy is returned; custom policies may carry
			// customer content or expressions in their labels.
			switch f.DetectorName {
			case "pii", "jailbreak", "injection", "indirect_injection", "secrets", "output_exfil":
				if safeMCPRule.MatchString(rule) {
					e.Rules = append(e.Rules, f.DetectorName+":"+rule)
				}
			}
		}
	}
	e.Categories = uniqueMCPDiagnostics(e.Categories)
	e.Rules = uniqueMCPDiagnostics(e.Rules)
	return e
}
func uniqueMCPDiagnostics(values []string) []string {
	if values == nil {
		return []string{}
	}
	slices.Sort(values)
	values = slices.Compact(values)
	if len(values) > 16 {
		values = values[:16]
	}
	return values
}
func mcpSecurityFailure(err error, categories []string, stage string) map[string]any {
	diagnostic := &mcpInspectionError{Code: "inspection_unavailable", Reason: "Security inspection could not be completed. The content was withheld.", Categories: []string{}, Rules: []string{}}
	var blocked *mcpInspectionError
	if errors.As(err, &blocked) {
		diagnostic = blocked
	} else {
		switch err.Error() {
		case "mcp content exceeds inspection limits":
			diagnostic.Code = "inspection_limit"
			diagnostic.Reason = "The response exceeds the security inspection size or nesting limit. Narrow the request."
		case "mcp field name blocked":
			diagnostic.Code = "field_name_rewrite"
			diagnostic.Reason = "Security policy would rewrite a JSON field name, which could change the tool's data structure."
		case "invalid MCP content", "invalid MCP text", "invalid MCP object", "invalid MCP array", "invalid MCP value":
			diagnostic.Code = "invalid_content"
			diagnostic.Reason = "The tool content is not valid JSON and could not be inspected."
		}
	}
	// Stage categories are limited to the withheld payload, not earlier checks.
	cats := uniqueMCPDiagnostics(slices.Clone(categories))
	explanation := diagnostic.Reason
	if len(cats) > 0 {
		explanation += " Findings: " + strings.Join(cats, ", ") + "."
	}
	if len(diagnostic.Rules) > 0 {
		explanation += " Rules: " + strings.Join(diagnostic.Rules, ", ") + "."
	}
	message := "Tool arguments were withheld before execution. " + explanation
	remote := "not_run"
	if stage == "result" {
		message = "The tool ran, but its result was withheld. " + explanation + " The remote action was not undone; check the service before running it again."
		remote = "completed"
	}
	return map[string]any{"error": message, "reason": diagnostic.Reason, "code": diagnostic.Code, "stage": stage, "categories": cats, "rules": diagnostic.Rules, "remote_action": remote, "blocking_detectors": uniqueMCPDiagnostics(slices.Clone(diagnostic.BlockingDetectors))}
}

func (h *Handler) auditMCPInspection(r *http.Request, c MCPConnection, tool, action string, categories []string, err error, stage string) {
	failure := mcpSecurityFailure(err, categories, stage)
	h.audit(r, "mcp.tool_"+action, AuditTarget{Type: "mcp_connection", ID: c.ID.String()}, map[string]any{"tool": tool, "categories": failure["categories"], "stage": stage, "code": failure["code"], "rules": failure["rules"], "blocking_detectors": failure["blocking_detectors"]})
}
