package gateway

import (
	"encoding/json"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/overlay"
	"net/http"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
	"github.com/google/uuid"
)

// Observe inspects local inference without exposing a private model server to
// Cloud. It performs no inference and never stores content. The proxy scope
// must explicitly authorize the tenant's local-model integration.
func (h *Handler) Observe(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.FromContext(r.Context())
	if !ok {
		writeRouteError(w, &routeError{401, "unauthorized"})
		return
	}
	var body struct {
		Model      string `json:"model"`
		Request    string `json:"request"`
		Response   string `json:"response"`
		HTTPStatus uint16 `json:"http_status"`
		DurationMs uint32 `json:"duration_ms"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 160<<10)).Decode(&body) != nil || len(body.Request) > 64<<10 || len(body.Response) > 64<<10 || len(body.Model) > 256 {
		writeRouteError(w, &routeError{400, "invalid local AI observation"})
		return
	}
	route, err := h.nativeRoute(r, info, providers.ProviderOllama, body.Model)
	if err != nil {
		writeRouteError(w, err)
		return
	}
	policy := gatewaypolicy.From(r.Context())
	policy.MonitorOnly = true
	policy.OmitContent = true
	r = r.WithContext(gatewaypolicy.With(r.Context(), policy))
	profile, snapshot, identity, _ := h.resolveProfileWithOverlay(r.Context(), info.CustomerID, route.proxyID)
	ctx := security.WithProfile(r.Context(), profile)
	if snapshot != nil {
		ctx = overlay.WithActive(ctx, snapshot, identity)
	}
	r = r.WithContext(ctx)
	traceID := uuid.New()
	scan := h.runSecurityScan(r, info, localObservationText(body.Request, true), traceID, profile)
	scan = mergeMonitorScans(scan, h.runSecurityScanFrom(r, info, localObservationText(body.Response, false), traceID, profile, "response"))
	status := "ok"
	if body.HTTPStatus >= 400 {
		status = "error"
	}
	duration := time.Duration(body.DurationMs) * time.Millisecond
	if duration > 10*time.Minute {
		duration = 10 * time.Minute
	}
	h.recordTrace(r.Context(), recordInput{TraceID: traceID, ProxyID: route.proxyID, Info: info, Method: "POST", Path: "local/observe", Provider: "ollama", Model: body.Model, Start: time.Now().Add(-duration), Status: status, HTTPStatus: body.HTTPStatus, Scan: scan, Headers: r.Header})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Bastio-Trace-Id", traceID.String())
	_ = json.NewEncoder(w).Encode(struct {
		TraceID string `json:"trace_id"`
		Mode    string `json:"mode"`
	}{traceID.String(), "observation_only"})
}

// Oversized captures can end inside a JSON string. Inspect the available
// prefix instead of silently skipping it; NDJSON is used by local streams.
func localObservationText(raw string, input bool) string {
	if input && json.Valid([]byte(raw)) {
		return InputText([]byte(raw))
	}
	if !input {
		if text := OutputText([]byte(raw)); text != "" {
			return text
		}
		var chunks []string
		for _, line := range strings.Split(raw, "\n") {
			if json.Valid([]byte(line)) {
				chunks = append(chunks, OutputText([]byte(line)))
			}
		}
		if text := strings.Join(chunks, ""); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return raw
}
