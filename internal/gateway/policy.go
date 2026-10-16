package gateway

import (
	"context"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
)

func (h *Handler) recordGatewayObservation(ctx context.Context, observation *observability.ObservationRecord) {
	if gatewaypolicy.From(ctx).OmitContent {
		copy := *observation
		copy.Input, copy.Output, copy.ToolInput, copy.ToolOutput = "", "", "", ""
		copy.ModelParameters, copy.ErrorMessage, copy.StatusMessage = "", "", ""
		observation = &copy
	}
	h.recorder.RecordObservation(observation)
}

func mergeMonitorScans(input, output *security.ScanResult) *security.ScanResult {
	if input == nil {
		return output
	}
	if output == nil {
		return input
	}
	input.Findings = append(input.Findings, output.Findings...)
	input.ThreatTypes = append(input.ThreatTypes, output.ThreatTypes...)
	if output.ThreatScore > input.ThreatScore {
		input.ThreatScore = output.ThreatScore
	}
	if len(input.Findings) > 0 {
		input.Action = security.ActionLogOnly
	}
	return input
}
