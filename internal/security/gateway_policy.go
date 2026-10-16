package security

import (
	"context"

	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
)

// ObserveGatewayResult preserves findings while removing every enforcement
// side effect. Called after plugins as well as the core engine.
func ObserveGatewayResult(ctx context.Context, result *ScanResult, original string) {
	if result == nil || !gatewaypolicy.From(ctx).MonitorOnly {
		return
	}
	result.ShouldBlock = false
	result.RewriteUnavailable = false
	result.TokenMap = nil
	result.SanitizedContent = original
	if len(result.Findings) > 0 {
		result.Action = ActionLogOnly
	}
	for i := range result.Findings {
		result.Findings[i].Action = ActionLogOnly
	}
	for detector := range result.DetectorActions {
		result.DetectorActions[detector] = ActionLogOnly
	}
}

// MetadataOnlyResult detaches a result before persistence; live inspection
// still sees the full content, but trace/threat storage receives no matched data.
func MetadataOnlyResult(result *ScanResult) *ScanResult {
	if result == nil {
		return nil
	}
	copy := *result
	copy.SanitizedContent = ""
	copy.TokenMap = nil
	copy.Findings = append([]Finding(nil), result.Findings...)
	for i := range copy.Findings {
		copy.Findings[i].MatchedContent = ""
		copy.Findings[i].Message = ""
	}
	return &copy
}
