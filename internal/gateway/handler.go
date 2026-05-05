package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/llmpipeline"
	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/proxy"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/bastio-ai/bastio/internal/security/overlay"

	"net/url"
	"os"
)

// Handler manages the gateway request pipeline.
type Handler struct {
	providers     *providers.Registry
	proxies       *proxy.Service
	security      *security.Engine
	profiles      security.ProfileLookup
	recorder      *observability.Recorder
	securityMode  string // "fail-open" or "fail-closed"
	overlayLoader *overlay.Loader
	// streamSecrets is the inline-streaming secrets scanner. The
	// full security engine is too heavy to call per chunk (multi-
	// detector, profile lookup, overlay merge); this is the
	// regex/entropy detector subset that runs on each delta as
	// it arrives. Catches the headline streaming-output attack:
	// model regurgitates an API key it learned during training,
	// or echoes a secret that was injected through RAG / tool
	// results. Mask-only — there's no way to "block" mid-stream
	// without aborting the SSE connection, which is a worse UX
	// than [REDACTED] in the chunk.
	streamSecrets *detection.SecretsDetector
	// imageURLAllowlist constrains hosts that can appear in
	// `image_url` parts of OpenAI multimodal messages. When
	// empty (default), every host is accepted — matches existing
	// behaviour. When non-empty, requests whose multimodal
	// content references a host outside the list are rejected
	// at the gateway. This is the closed-form, no-OCR slice of
	// multimodal protection: closes the URL-injection vector
	// (split-content, tracking-pixel exfil, phishing host) without
	// requiring the full pixel-content pipeline.
	imageURLAllowlist []string
}

// NewHandler creates a new gateway handler. A nil profiles lookup is
// valid — the handler falls back to security.DefaultProfile().
func NewHandler(providerRegistry *providers.Registry, proxySvc *proxy.Service, secEngine *security.Engine, profiles security.ProfileLookup, recorder *observability.Recorder, securityMode string) *Handler {
	return &Handler{
		providers:         providerRegistry,
		proxies:           proxySvc,
		security:          secEngine,
		profiles:          profiles,
		recorder:          recorder,
		securityMode:      securityMode,
		streamSecrets:     detection.NewSecretsDetector(),
		imageURLAllowlist: parseImageURLAllowlist(os.Getenv("BASTIO_IMAGE_URL_ALLOWLIST")),
	}
}

// SetOverlayLoader installs the tenant-policy-overlay loader. When set,
// gateway requests load the active overlay for the calling customer
// and apply it on top of the base profile, matching the behaviour of
// DetectHandler. Nil disables overlay integration on this handler.
func (h *Handler) SetOverlayLoader(l *overlay.Loader) {
	h.overlayLoader = l
}

// resolveProfileWithOverlay returns the merged security profile for
// a customer plus the overlay snapshot + identity (when one is
// active). Callers attach the snapshot to the request context so
// runtime detectors (topic, plugins) honour the overlay's additions.
//
// proxyID scopes the overlay lookup. Per-proxy overlays take
// precedence over customer-wide; pass uuid.Nil when the caller has
// no proxy context available (ChatCompletions resolves its proxy
// later via model-based inference — a follow-up restructure could
// move that earlier to enable per-proxy overlays there too).
//
// The error return carries any base-profile load failure. Overlay
// load failures fail open — they are logged and the base profile is
// used, because blocking gateway traffic on overlay infrastructure
// would be a worse failure mode than ignoring it.
func (h *Handler) resolveProfileWithOverlay(ctx context.Context, customerID, proxyID uuid.UUID) (*security.Profile, *overlay.OverlaySnapshot, overlay.Identity, error) {
	base, err := h.resolveBaseProfile(ctx, customerID)
	merged, snap, ident := h.applyOverlay(ctx, customerID, proxyID, base)
	return merged, snap, ident, err
}

func (h *Handler) resolveBaseProfile(ctx context.Context, customerID uuid.UUID) (*security.Profile, error) {
	if h.profiles == nil {
		p := security.DefaultProfile()
		return &p, nil
	}
	p, err := h.profiles.GetDefault(ctx, customerID)
	if err != nil {
		slog.Warn("load security profile failed; using defaults", "customer_id", customerID, "error", err)
		d := security.DefaultProfile()
		return &d, err
	}
	if p == nil {
		d := security.DefaultProfile()
		return &d, nil
	}
	return p, nil
}

// applyOverlay merges the active tenant policy overlay (if any) onto
// base and returns the merged profile plus the snapshot + identity.
// Loader and DB errors fail open — base is returned unchanged and the
// failure is logged. Never blocks gateway traffic on overlay
// infrastructure.
//
// proxyID: per-proxy overlays are preferred when proxyID is non-Nil;
// the loader falls back to customer-wide overlays if no per-proxy
// match exists. uuid.Nil restricts the lookup to customer-wide only.
func (h *Handler) applyOverlay(ctx context.Context, customerID, proxyID uuid.UUID, base *security.Profile) (*security.Profile, *overlay.OverlaySnapshot, overlay.Identity) {
	if h.overlayLoader == nil || base == nil {
		return base, nil, overlay.Identity{}
	}
	snap, ident, err := h.overlayLoader.LoadActive(ctx, customerID, proxyID)
	if err != nil {
		slog.Warn("gateway: overlay load failed", "customer_id", customerID, "error", err)
		return base, nil, overlay.Identity{}
	}
	if snap == nil {
		return base, nil, overlay.Identity{}
	}
	return security.ApplyOverlay(base, snap, ident), snap, ident
}

// failClosed reports whether the handler is configured to block
// requests when the security engine can't reach a decision (engine
// misconfigured, profile lookup errored, etc.). Controlled by
// BASTIO_SECURITY_MODE. Centralizing this string match here keeps
// the handler callsites readable.
func (h *Handler) failClosed() bool {
	return h.securityMode == "fail-closed"
}

// writeFailClosedBlock responds with 503 Service Unavailable and a
// JSON body explaining that the request was blocked because security
// evaluation couldn't run. Used only in fail-closed mode — fail-open
// proceeds instead.
func (h *Handler) writeFailClosedBlock(w http.ResponseWriter, traceID uuid.UUID, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	body := fmt.Sprintf(
		`{"error":"request blocked: security scan unavailable","trace_id":%q,"reason":%q,"mode":"fail-closed"}`,
		traceID.String(), reason,
	)
	_, _ = w.Write([]byte(body))
}

// ChatCompletions handles OpenAI-compatible /v1/chat/completions requests.
// The proxy is resolved from the default proxy for the customer, or from query param.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	info, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request"}`, http.StatusBadRequest)
		return
	}

	// Parse to determine model and streaming mode
	var chatReq struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &chatReq); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Extract user content for security scanning
	var fullReq struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Messages []json.RawMessage `json:"messages"`
	}
	json.Unmarshal(body, &fullReq)
	userContent := extractUserContent(fullReq.Messages)

	// Image-URL allowlist. Closed-form, no-OCR slice of multimodal
	// protection: validates that any `image_url` part references a
	// host the operator has trusted. Configured via
	// BASTIO_IMAGE_URL_ALLOWLIST. When unset, every host is allowed
	// (back-compat). See validateImageURLs for the rules.
	if rejected, ok := validateImageURLs(fullReq.Messages, h.imageURLAllowlist); !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "image_url_not_allowed",
				"message": "image_url host is outside the workspace's trusted-hosts allowlist",
				"url":     rejected,
			},
		})
		return
	}

	// Load the security profile once per request; downstream code (scan,
	// response restore, response scan) reads the same settings via ctx.
	// A load error in fail-closed mode blocks the request — the caller
	// asked for strict enforcement and we can't honor that without a
	// profile.
	traceID := uuid.New()
	w.Header().Set("X-Bastio-Trace-Id", traceID.String())
	// ChatCompletions doesn't know which proxy yet — proxy is resolved
	// later from the model name via resolveClient. Pass uuid.Nil so the
	// loader returns the customer-wide overlay. Per-proxy overlays for
	// this route require moving proxy resolution up (follow-up item).
	profile, overlaySnap, overlayIdent, profileErr := h.resolveProfileWithOverlay(r.Context(), info.CustomerID, uuid.Nil)
	if profileErr != nil && h.failClosed() {
		h.writeFailClosedBlock(w, traceID, "security profile load failed")
		return
	}
	if h.security == nil && h.failClosed() {
		h.writeFailClosedBlock(w, traceID, "security engine not configured")
		return
	}
	ctx := security.WithProfile(r.Context(), profile)
	if overlaySnap != nil {
		// Attach the active overlay so detectors (topic, plugins) can
		// honour its additions at runtime.
		ctx = overlay.WithActive(ctx, overlaySnap, overlayIdent)
	}
	r = r.WithContext(ctx)

	// Security scan
	scanResult := h.runSecurityScan(r, info, userContent, traceID, profile)
	if scanResult != nil && scanResult.ShouldBlock {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		blockedBody := fmt.Sprintf(`{"error":"request blocked by security policy","trace_id":%q,"threat_types":%s,"threat_score":%.2f}`,
			traceID.String(), mustJSON(scanResult.ThreatTypes), scanResult.ThreatScore)
		_, _ = w.Write([]byte(blockedBody))
		h.recordTrace(recordInput{
			TraceID:   traceID,
			Info:      info,
			Model:     chatReq.Model,
			Provider:  "openai",
			Path:      r.URL.Path,
			Start:     start,
			Status:    "blocked",
			Scan:      scanResult,
			ReqBody:   body,
			RespBody:  []byte(blockedBody),
			Headers:   r.Header,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
		})
		return
	}

	// Apply per-user-message PII sanitization to the outbound body. The
	// provider only sees masked values or placeholders; originals stay in
	// the TokenMap on ctx so the response path can restore them.
	if scanResult != nil && (scanResult.Action == security.ActionMask || scanResult.Action == security.ActionTokenize) {
		var tm *security.TokenMap
		if scanResult.Action == security.ActionTokenize {
			tm = security.NewTokenMap(profile.PIITokenStyle)
		}
		if rewritten, ok := sanitizeOutboundBody(body, h.security, scanResult.Action, tm); ok {
			body = rewritten
		}
		if tm != nil && tm.Size() > 0 {
			r = r.WithContext(security.WithTokenMap(r.Context(), tm))
		}
	}

	// Resolve provider from model name or proxy config
	provider, providerKey, err := h.resolveProvider(r, info, chatReq.Model, body)
	if err != nil {
		slog.Error("resolve provider failed", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	req := &providers.ChatRequest{
		Model:  chatReq.Model,
		Stream: chatReq.Stream,
		Raw:    body,
	}

	// Add timing header
	w.Header().Set("X-Bastio-Request-Id", traceID.String())

	var respBody []byte
	var inTok, outTok int
	var status string
	if chatReq.Stream {
		respBody, inTok, outTok, status = h.handleStream(w, r, provider, req, providerKey, start, traceID, scanResult)
	} else {
		respBody, inTok, outTok, status = h.handleSync(w, r, provider, req, providerKey, start, traceID, scanResult)
	}

	// Recorder buffers this trace for batched insertion.
	h.recordTrace(recordInput{
		TraceID:      traceID,
		Info:         info,
		Model:        chatReq.Model,
		Provider:     string(provider.Name()),
		Path:         r.URL.Path,
		Start:        start,
		Status:       status,
		Scan:         scanResult,
		ReqBody:      body,
		RespBody:     respBody,
		InputTokens:  inTok,
		OutputTokens: outTok,
		Headers:      r.Header,
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
	})
}

// AnthropicMessages handles Anthropic-compatible /v1/guard/{proxyId}/v1/messages requests.
func (h *Handler) AnthropicMessages(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	info, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	proxyIDStr := chi.URLParam(r, "proxyId")
	proxyID, err := uuid.Parse(proxyIDStr)
	if err != nil {
		http.Error(w, `{"error":"invalid proxy ID"}`, http.StatusBadRequest)
		return
	}

	p, err := h.proxies.GetByID(r.Context(), proxyID)
	if err != nil {
		http.Error(w, `{"error":"proxy not found"}`, http.StatusNotFound)
		return
	}

	if p.CustomerID != info.CustomerID {
		http.Error(w, `{"error":"proxy not found"}`, http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request"}`, http.StatusBadRequest)
		return
	}

	var chatReq struct {
		Stream bool `json:"stream"`
	}
	json.Unmarshal(body, &chatReq)

	// Trace id created up front so the security-scan block can surface it
	// in block responses and telemetry, mirroring ChatCompletions.
	anthropicTraceID := uuid.New()
	w.Header().Set("X-Bastio-Request-Id", anthropicTraceID.String())
	w.Header().Set("X-Bastio-Trace-Id", anthropicTraceID.String())

	// Inbound security scan — same pipeline as /v1/chat/completions so
	// Anthropic-gateway traffic is covered by the same detectors,
	// profiles, and tenant policy overlays. Previous versions of this
	// handler forwarded the request upstream with zero inbound scanning;
	// see the A1 follow-up in the overlay plan for context.
	// AnthropicMessages has the proxy in the URL, so per-proxy overlays
	// apply here naturally. The loader falls back to customer-wide when
	// no per-proxy overlay exists.
	profile, overlaySnap, overlayIdent, profileErr := h.resolveProfileWithOverlay(r.Context(), info.CustomerID, proxyID)
	if profileErr != nil && h.failClosed() {
		h.writeFailClosedBlock(w, anthropicTraceID, "security profile load failed")
		return
	}
	if h.security == nil && h.failClosed() {
		h.writeFailClosedBlock(w, anthropicTraceID, "security engine not configured")
		return
	}
	ctx := security.WithProfile(r.Context(), profile)
	if overlaySnap != nil {
		ctx = overlay.WithActive(ctx, overlaySnap, overlayIdent)
	}
	r = r.WithContext(ctx)

	userContent := extractAnthropicUserContent(body)
	scanResult := h.runSecurityScan(r, info, userContent, anthropicTraceID, profile)
	if scanResult != nil && scanResult.ShouldBlock {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		blockedBody := fmt.Sprintf(
			`{"error":"request blocked by security policy","trace_id":%q,"threat_types":%s,"threat_score":%.2f}`,
			anthropicTraceID.String(), mustJSON(scanResult.ThreatTypes), scanResult.ThreatScore,
		)
		_, _ = w.Write([]byte(blockedBody))
		h.recordTrace(recordInput{
			TraceID:   anthropicTraceID,
			Info:      info,
			Model:     "",
			Provider:  string(providers.ProviderAnthropic),
			Path:      r.URL.Path,
			Start:     start,
			Status:    "blocked",
			Scan:      scanResult,
			ReqBody:   body,
			RespBody:  []byte(blockedBody),
			Headers:   r.Header,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
		})
		return
	}

	providerKey, err := h.proxies.ResolveProviderKey(r.Context(), info.CustomerID, proxyID, providers.ProviderAnthropic)
	if err != nil {
		http.Error(w, `{"error":"no anthropic key configured"}`, http.StatusBadGateway)
		return
	}

	client, err := h.providers.Get(providers.ProviderAnthropic)
	if err != nil {
		http.Error(w, `{"error":"anthropic provider not available"}`, http.StatusBadGateway)
		return
	}

	req := &providers.ChatRequest{
		Stream: chatReq.Stream,
		Raw:    body,
	}

	var respBody []byte
	var inTok, outTok int
	var status string
	if chatReq.Stream {
		respBody, inTok, outTok, status = h.handleStream(w, r, client, req, providerKey, start, anthropicTraceID, scanResult)
	} else {
		respBody, inTok, outTok, status = h.handleSync(w, r, client, req, providerKey, start, anthropicTraceID, scanResult)
	}

	h.recordTrace(recordInput{
		TraceID:      anthropicTraceID,
		Info:         info,
		Model:        "", // Anthropic body carries model; parsed on the provider side.
		Provider:     string(providers.ProviderAnthropic),
		Path:         r.URL.Path,
		Start:        start,
		Status:       status,
		Scan:         scanResult,
		ReqBody:      body,
		RespBody:     respBody,
		InputTokens:  inTok,
		OutputTokens: outTok,
		Headers:      r.Header,
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
	})
}

// handleSync proxies a non-streaming chat request and also returns the
// response bytes + token usage so the caller can persist a full trace.
// The status is "ok" on success, "error" if the provider call failed.
func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request, client providers.Client, req *providers.ChatRequest, apiKey string, start time.Time, traceID uuid.UUID, scan *security.ScanResult) ([]byte, int, int, string) {
	info, _ := auth.FromContext(r.Context())
	env := r.Header.Get("X-Bastio-Environment")
	genSpan := observability.NewSpan(traceID, customerIDFromInfo(info), "generation", fmt.Sprintf("%s.chat", client.Name())).
		WithEnv(env).
		Start()
	resp, err := client.Chat(r.Context(), req, apiKey)
	if err != nil {
		slog.Error("provider request failed", "provider", client.Name(), "error", err)
		errBody := writeProviderError(w, string(client.Name()), err)
		genSpan.Finish("error", err.Error())
		if h.recorder != nil {
			gr := genSpan.Record()
			gr.Model = req.Model
			gr.Input = truncate(string(req.Raw), maxSpanBodyChars)
			gr.ModelParameters = extractModelParameters(req.Raw)
			h.recorder.RecordObservation(gr)
		}
		return []byte(errBody), 0, 0, "error"
	}
	genSpan.Finish("ok", "")
	if h.recorder != nil {
		gr := genSpan.Record()
		gr.Model = resolveSpanModel(resp.Model, req.Model)
		gr.InputTokens = uint32(resp.InputTokens)
		gr.OutputTokens = uint32(resp.OutputTokens)
		gr.Input = truncate(string(req.Raw), maxSpanBodyChars)
		gr.Output = truncate(string(resp.Raw), maxSpanBodyChars)
		gr.ModelParameters = extractModelParameters(req.Raw)
		gr.CostCents = llmpipeline.EstimateCostCents(gr.Model, resp.InputTokens, resp.OutputTokens)
		h.recorder.RecordObservation(gr)
	}

	latencyMs := time.Since(start).Milliseconds()
	w.Header().Set("X-Bastio-Latency-Ms", fmt.Sprintf("%d", latencyMs))
	w.Header().Set("Content-Type", "application/json")

	// Post-process the provider body: optional response-side PII scan
	// (mask model-emitted PII) and, when tokenize mode is active, restore
	// placeholders back to the originals the caller owns.
	if resp.Raw != nil {
		if processed, blocked, reason := h.postProcessResponse(r.Context(), resp.Raw); blocked {
			errBody := fmt.Sprintf(`{"error":"response blocked by security policy","trace_id":%q,"reason":%q}`, traceID.String(), reason)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(errBody))
			return []byte(errBody), resp.InputTokens, resp.OutputTokens, "blocked"
		} else if processed != nil {
			resp.Raw = processed
		}
	}

	envelope := buildBastioEnvelope(r.Context(), traceID, start, string(client.Name()), scan)

	// Append `bastio` as the final field of the upstream body, preserving
	// byte order so clients see the original OpenAI/Anthropic shape first.
	// Falls back to raw bytes if the body isn't a JSON object (error text,
	// chunked transfer oddity) — X-Bastio-* headers still carry essentials.
	if resp.Raw != nil {
		if merged, ok := appendBastioField(resp.Raw, envelope); ok {
			_, _ = w.Write(merged)
			return resp.Raw, resp.InputTokens, resp.OutputTokens, "ok"
		}
		_, _ = w.Write(resp.Raw)
		return resp.Raw, resp.InputTokens, resp.OutputTokens, "ok"
	}

	// Synthesised response path — no upstream bytes to preserve, so marshal
	// an ordered struct with bastio last via json.RawMessage.
	envBytes, _ := json.Marshal(envelope)
	out := []byte(fmt.Sprintf(
		`{"id":%q,"model":%q,"content":%q,"role":%q,"finish_reason":%q,"usage":{"prompt_tokens":%d,"completion_tokens":%d},"bastio":%s}`,
		resp.ID, resp.Model, resp.Content, resp.Role, resp.FinishReason,
		resp.InputTokens, resp.OutputTokens, envBytes,
	))
	_, _ = w.Write(out)
	return out, resp.InputTokens, resp.OutputTokens, "ok"
}

// appendBastioField appends `,"bastio":<envelope>` just before the last
// closing brace of the upstream JSON object, preserving the original
// field order. Returns (nil, false) if the body isn't a JSON object.
func appendBastioField(raw []byte, envelope map[string]any) ([]byte, bool) {
	end := len(raw)
	for end > 0 {
		c := raw[end-1]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			end--
			continue
		}
		break
	}
	if end == 0 || raw[end-1] != '}' {
		return nil, false
	}
	// Detect whether the object has any existing fields (to decide on a
	// leading comma). Scan backward from just inside the closing brace.
	hasContent := false
	for i := end - 2; i >= 0; i-- {
		c := raw[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		if c != '{' {
			hasContent = true
		}
		break
	}
	envBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, false
	}
	out := make([]byte, 0, len(raw)+len(envBytes)+16)
	out = append(out, raw[:end-1]...)
	if hasContent {
		out = append(out, ',')
	}
	out = append(out, []byte(`"bastio":`)...)
	out = append(out, envBytes...)
	out = append(out, '}')
	return out, true
}

// handleStream proxies a streaming chat request and accumulates each SSE
// chunk into a buffer so the full response can be persisted for the trace
// detail view. Token usage is parsed from the OpenAI-shape final chunk
// when present.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request, client providers.Client, req *providers.ChatRequest, apiKey string, start time.Time, traceID uuid.UUID, scan *security.ScanResult) ([]byte, int, int, string) {
	info, _ := auth.FromContext(r.Context())
	env := r.Header.Get("X-Bastio-Environment")
	genSpan := observability.NewSpan(traceID, customerIDFromInfo(info), "generation", fmt.Sprintf("%s.chat.stream", client.Name())).
		WithEnv(env).
		Start()
	stream, err := client.ChatStream(r.Context(), req, apiKey)
	if err != nil {
		slog.Error("provider stream failed", "provider", client.Name(), "error", err)
		errBody := writeProviderError(w, string(client.Name()), err)
		genSpan.Finish("error", err.Error())
		if h.recorder != nil {
			gr := genSpan.Record()
			gr.Model = req.Model
			gr.Input = truncate(string(req.Raw), maxSpanBodyChars)
			gr.ModelParameters = extractModelParameters(req.Raw)
			h.recorder.RecordObservation(gr)
		}
		return []byte(errBody), 0, 0, "error"
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Bastio-Latency-Ms", fmt.Sprintf("%d", time.Since(start).Milliseconds()))

	flusher, ok := w.(http.Flusher)
	if !ok {
		errBody := `{"error":"streaming not supported"}`
		http.Error(w, errBody, http.StatusInternalServerError)
		return []byte(errBody), 0, 0, "error"
	}

	// Build a StreamRestorer from the request-scoped TokenMap so
	// placeholders emitted in delta.content are restored before the
	// caller sees them. Response-side scanning on streams is deferred
	// to a later release — tracking in the plan's §5.3.
	var restorer *security.StreamRestorer
	if profile, ok := security.ProfileFromContext(r.Context()); ok && profile.PIIRestoreResponse {
		if tm, hasTM := security.TokenMapFromContext(r.Context()); hasTM {
			restorer = security.NewStreamRestorer(tm)
		}
	}

	var buf []byte
	var inputTokens, outputTokens int
	// Finish the generation span once we leave the stream loop.
	defer func() {
		if h.recorder == nil {
			return
		}
		genSpan.Finish("ok", "")
		gr := genSpan.Record()
		gr.Model = req.Model
		gr.InputTokens = uint32(inputTokens)
		gr.OutputTokens = uint32(outputTokens)
		gr.Input = truncate(string(req.Raw), maxSpanBodyChars)
		gr.Output = truncate(string(buf), maxSpanBodyChars)
		gr.ModelParameters = extractModelParameters(req.Raw)
		gr.CostCents = llmpipeline.EstimateCostCents(gr.Model, inputTokens, outputTokens)
		h.recorder.RecordObservation(gr)
	}()
	for chunk := range stream {
		if chunk.Error != nil {
			slog.Error("stream error", "error", chunk.Error)
			break
		}

		if chunk.Done {
			// Emit any residual held by the stream restorer before the
			// final bastio event so the caller sees all restored bytes.
			if tail := flushStreamRestorer(restorer); len(tail) > 0 {
				fmt.Fprintf(w, "data: %s\n\n", tail)
				flusher.Flush()
			}
			// Emit one final Bastio event before [DONE] so clients that
			// iterate SSE events can pick up trace metadata. OpenAI SDKs
			// ignore unrecognized event payloads.
			envelope := buildBastioEnvelope(r.Context(), traceID, start, string(client.Name()), scan)
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{"bastio": envelope}))
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}

		data := chunk.Data
		if restorer != nil {
			data = rewriteStreamChunk(data, restorer)
		}
		// In-flight secrets scan. Runs AFTER restoration so the
		// scanner sees the same content the client would. Mask-
		// in-place: a leaked API key shows as `[REDACTED]` to
		// the client. Only the secrets detector runs here (other
		// detectors need full-document context or are too slow
		// per-chunk); the post-flight outbound scan in the
		// non-streaming path covers the rest.
		if h.streamSecrets != nil {
			data = scanStreamChunkForSecrets(data, h.streamSecrets)
		}

		buf = append(buf, data...)
		buf = append(buf, '\n')
		// Sniff OpenAI-shape usage on the final chunk.
		if in, out, ok := parseUsageFromSSE(data); ok {
			if in > 0 {
				inputTokens = in
			}
			if out > 0 {
				outputTokens = out
			}
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	return buf, inputTokens, outputTokens, "ok"
}

// parseImageURLAllowlist normalizes the comma-separated env-var
// value into a clean slice of hostnames. Whitespace + empties are
// dropped; hostnames are lowercased so the runtime check can be
// case-insensitive.
//
// Empty slice = "no allowlist configured" = accept any host. That
// preserves existing behaviour for deployments that don't opt in.
func parseImageURLAllowlist(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		host := strings.ToLower(strings.TrimSpace(p))
		if host != "" {
			out = append(out, host)
		}
	}
	return out
}

// validateImageURLs walks the inbound messages, finds every
// `image_url` content part, and rejects requests whose hosts are
// outside the configured allowlist.
//
// Rules:
//
//   - Empty allowlist = allow everything. The function returns
//     nil immediately. No JSON parse cost when the feature isn't
//     configured.
//   - Data URLs (`data:image/...;base64,...`) are always allowed.
//     The bytes are inline; there's no remote host to validate.
//   - Any URL whose host doesn't match an allowlist entry causes
//     the request to fail with a typed error; the caller turns it
//     into a 403 with a structured body.
//
// Hostname match is exact, lowercase, no wildcard support yet —
// keep the implementation simple for v2.0; v2.1 can grow
// `*.example.com` glob support if real customers ask.
func validateImageURLs(messages []json.RawMessage, allowlist []string) (string, bool) {
	if len(allowlist) == 0 {
		return "", true
	}
	allowed := make(map[string]struct{}, len(allowlist))
	for _, h := range allowlist {
		allowed[h] = struct{}{}
	}
	for _, raw := range messages {
		var msg struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		// String-content messages have no image_url parts.
		if len(msg.Content) == 0 || msg.Content[0] != '[' {
			continue
		}
		var parts []json.RawMessage
		if err := json.Unmarshal(msg.Content, &parts); err != nil {
			continue
		}
		for _, p := range parts {
			var part struct {
				Type     string `json:"type"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			}
			if err := json.Unmarshal(p, &part); err != nil {
				continue
			}
			if part.Type != "image_url" || part.ImageURL.URL == "" {
				continue
			}
			u := part.ImageURL.URL
			// Inline data URLs bypass the allowlist — the bytes are
			// in the request body, no remote host is contacted.
			if strings.HasPrefix(strings.ToLower(u), "data:") {
				continue
			}
			parsed, err := url.Parse(u)
			if err != nil || parsed.Host == "" {
				return u, false
			}
			host := strings.ToLower(parsed.Host)
			// Strip an optional port — allowlists are about hosts,
			// not specific ports.
			if i := strings.Index(host, ":"); i >= 0 {
				host = host[:i]
			}
			if _, ok := allowed[host]; !ok {
				return u, false
			}
		}
	}
	return "", true
}

// scanStreamChunkForSecrets runs the regex/entropy secrets detector
// over the chunk's `delta.content` and replaces matches with mask
// markers. Same JSON-traversal shape as rewriteStreamChunk but the
// rewrite step is `detector.Mask(content)` instead of restoration.
//
// The streaming output scan only runs the secrets detector — running
// the full engine per chunk would blow the latency budget (multiple
// detectors, profile lookup, overlay merge). For other categories
// (PII, jailbreak echo, exfil patterns), the input-side tokenize-
// restore loop and the non-streaming post-flight scan handle them.
//
// Mask-only: there is no "block this chunk and abort" — once a chunk
// is in the SSE stream, the only safe rewrite that preserves stream
// integrity is in-place substitution. A leaked API key emerges as
// `[REDACTED]` to the client.
//
// Cross-chunk-split secrets (e.g. a key whose first half lands in
// chunk N and second half in chunk N+1) are caught by the
// observation pipeline's full-buffer scan after the stream
// completes, not in-flight. v2.1 will add a buffered streaming
// scanner that handles cross-chunk patterns inline.
func scanStreamChunkForSecrets(data []byte, det *detection.SecretsDetector) []byte {
	if det == nil {
		return data
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return data
	}
	choicesRaw, ok := envelope["choices"]
	if !ok {
		return data
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(choicesRaw, &choices); err != nil {
		return data
	}

	changed := false
	for i, c := range choices {
		var choice map[string]json.RawMessage
		if err := json.Unmarshal(c, &choice); err != nil {
			continue
		}
		deltaRaw, ok := choice["delta"]
		if !ok {
			continue
		}
		var delta map[string]json.RawMessage
		if err := json.Unmarshal(deltaRaw, &delta); err != nil {
			continue
		}
		contentRaw, ok := delta["content"]
		if !ok {
			continue
		}
		var content string
		if err := json.Unmarshal(contentRaw, &content); err != nil {
			continue
		}
		safe := det.Mask(content)
		if safe == content {
			continue
		}
		cb, err := json.Marshal(safe)
		if err != nil {
			continue
		}
		delta["content"] = cb
		nd, err := json.Marshal(delta)
		if err != nil {
			continue
		}
		choice["delta"] = nd
		nc, err := json.Marshal(choice)
		if err != nil {
			continue
		}
		choices[i] = nc
		changed = true
	}
	if !changed {
		return data
	}
	newChoices, err := json.Marshal(choices)
	if err != nil {
		return data
	}
	envelope["choices"] = newChoices
	out, err := json.Marshal(envelope)
	if err != nil {
		return data
	}
	return out
}

// rewriteStreamChunk intercepts an OpenAI-shape SSE chunk, feeds
// choices[].delta.content through the restorer, and returns the
// rewritten payload. When the restorer holds bytes back, delta.content
// shortens (or empties) for the current event; the held bytes emerge
// on a later Write or during Flush.
func rewriteStreamChunk(data []byte, r *security.StreamRestorer) []byte {
	if r == nil {
		return data
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return data
	}
	choicesRaw, ok := envelope["choices"]
	if !ok {
		return data
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(choicesRaw, &choices); err != nil {
		return data
	}

	changed := false
	for i, c := range choices {
		var choice map[string]json.RawMessage
		if err := json.Unmarshal(c, &choice); err != nil {
			continue
		}
		deltaRaw, ok := choice["delta"]
		if !ok {
			continue
		}
		var delta map[string]json.RawMessage
		if err := json.Unmarshal(deltaRaw, &delta); err != nil {
			continue
		}
		contentRaw, ok := delta["content"]
		if !ok {
			continue
		}
		var content string
		if err := json.Unmarshal(contentRaw, &content); err != nil {
			continue
		}
		safe := string(r.Write([]byte(content)))
		if safe == content {
			continue
		}
		cb, err := json.Marshal(safe)
		if err != nil {
			continue
		}
		delta["content"] = cb
		nd, err := json.Marshal(delta)
		if err != nil {
			continue
		}
		choice["delta"] = nd
		nc, err := json.Marshal(choice)
		if err != nil {
			continue
		}
		choices[i] = nc
		changed = true
	}
	if !changed {
		return data
	}
	newChoices, err := json.Marshal(choices)
	if err != nil {
		return data
	}
	envelope["choices"] = newChoices
	out, err := json.Marshal(envelope)
	if err != nil {
		return data
	}
	return out
}

// flushStreamRestorer returns a synthesized SSE payload carrying any
// bytes the restorer held back at stream end. The shape matches an
// OpenAI streaming delta so existing client SDKs pick it up without
// special-casing. Returns an empty slice when there is nothing to emit.
func flushStreamRestorer(r *security.StreamRestorer) []byte {
	if r == nil {
		return nil
	}
	tail := r.Flush()
	if len(tail) == 0 {
		return nil
	}
	out, err := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]string{"content": string(tail)}},
		},
	})
	if err != nil {
		return nil
	}
	return out
}

// parseUsageFromSSE extracts prompt_tokens / completion_tokens from an
// OpenAI-style streaming chunk. Non-usage chunks return (0, 0, false).
func parseUsageFromSSE(data []byte) (int, int, bool) {
	var payload struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Usage == nil {
		return 0, 0, false
	}
	return payload.Usage.PromptTokens, payload.Usage.CompletionTokens, true
}

// buildBastioEnvelope returns the per-request metadata injected into
// gateway responses alongside the upstream provider body. Fields that
// would be zero-valued (no threat) are omitted to keep the envelope
// small for non-suspicious traffic.
//
// PII token counts (placeholders issued and restored) come from the
// request-scoped TokenMap on ctx. Raw values are never included — only
// counts, which is safe for dashboards and customer-visible envelopes.
func buildBastioEnvelope(ctx context.Context, traceID uuid.UUID, start time.Time, provider string, scan *security.ScanResult) map[string]any {
	env := map[string]any{
		"trace_id":        traceID.String(),
		"duration_ms":     time.Since(start).Milliseconds(),
		"provider":        provider,
		"security_action": "pass",
	}
	if scan != nil {
		if scan.Action != "" {
			env["security_action"] = string(scan.Action)
		}
		if scan.ThreatScore > 0 {
			env["threat_score"] = scan.ThreatScore
		}
		if len(scan.ThreatTypes) > 0 {
			types := make([]string, len(scan.ThreatTypes))
			for i, t := range scan.ThreatTypes {
				types[i] = string(t)
			}
			env["threat_types"] = types
		}
	}
	if tm, ok := security.TokenMapFromContext(ctx); ok {
		if n := tm.Size(); n > 0 {
			env["pii_tokens_used"] = n
		}
		if n := tm.RestoredCount(); n > 0 {
			env["pii_tokens_restored"] = n
		}
	}
	return env
}

// resolveProvider determines which provider client and API key to use.
func (h *Handler) resolveProvider(r *http.Request, info *auth.APIKeyInfo, model string, body []byte) (providers.Client, string, error) {
	// Determine provider from model name
	provider := inferProvider(model)

	client, err := h.providers.Get(provider)
	if err != nil {
		return nil, "", fmt.Errorf("provider %s not available", provider)
	}

	// Look up the first active proxy for this customer + provider
	proxies, err := h.proxies.ListByCustomer(r.Context(), info.CustomerID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list proxies: %w", err)
	}

	var proxyID uuid.UUID
	for _, p := range proxies {
		if p.TargetProvider == provider && p.IsActive {
			proxyID = p.ID
			break
		}
	}

	providerKey, err := h.proxies.ResolveProviderKey(r.Context(), info.CustomerID, proxyID, provider)
	if err != nil {
		return nil, "", fmt.Errorf("no %s key configured: %w", provider, err)
	}

	return client, providerKey, nil
}

// inferProvider guesses the provider from a model name.
func inferProvider(model string) providers.Provider {
	switch {
	case len(model) >= 3 && (model[:3] == "gpt" || model[:2] == "o1" || model[:2] == "o3" || model[:2] == "o4"):
		return providers.ProviderOpenAI
	case len(model) >= 6 && model[:6] == "claude":
		return providers.ProviderAnthropic
	default:
		return providers.ProviderOpenAI // default
	}
}

// runSecurityScan runs the security engine against request content.
// Returns nil if no engine is configured or scan fails in fail-open mode.
// Also emits a "guardrail" observation so the SpanTree in the dashboard
// shows the security stage as a first-class step with its own timing,
// detector count, and status. The profile supplies PII handling policy;
// SkipSanitization is always true here because the gateway sanitizes
// per-message (so one TokenMap spans the conversation).
//
// Threat-row writes are intentionally deferred to recordTrace (via
// llmpipeline.RecordChatTrace) so gateway and workspace produce
// exactly one threat row per finding, from a single code path.
func (h *Handler) runSecurityScan(r *http.Request, info *auth.APIKeyInfo, content string, traceID uuid.UUID, profile *security.Profile) *security.ScanResult {
	if h.security == nil || content == "" {
		return nil
	}

	env := r.Header.Get("X-Bastio-Environment")
	span := observability.NewSpan(traceID, info.CustomerID, "guardrail", "security.scan").
		WithEnv(env).
		Start()

	result := llmpipeline.PreflightScan(r.Context(), llmpipeline.PreflightOptions{
		Engine:           h.security,
		Profile:          profile,
		Content:          content,
		CustomerID:       info.CustomerID,
		EndUserID:        r.Header.Get("X-End-User-Id"),
		IPAddress:        r.RemoteAddr,
		UserAgent:        r.UserAgent(),
		SessionID:        r.Header.Get("X-Bastio-Session-Id"),
		SkipSanitization: true, // gateway tokenizes per-message itself
		Role:             security.RoleUser,
	})

	// Overlay plugin detectors run after the core scan and fold their
	// findings into the same ScanResult so the caller's block/warn
	// logic (status + observability + trace) applies uniformly to core
	// and plugin-sourced findings. Plugin failures are contained
	// inside RunOverlayPlugins — never propagate here. If there's no
	// active overlay or the overlay lists no plugins, this is a no-op.
	if pluginFindings := security.RunOverlayPlugins(r.Context(), content); len(pluginFindings) > 0 {
		if result == nil {
			// Core scan returned nil (edge case: content-shaped early
			// return further up would have skipped us entirely, so
			// this only hits for engines that Scan to nil but left
			// plugins to contribute).
			result = &security.ScanResult{}
		}
		result.Findings = append(result.Findings, pluginFindings...)
		if security.BlockActionFromFindings(pluginFindings) {
			result.ShouldBlock = true
			result.Action = security.ActionBlock
		}
	}

	status := "ok"
	msg := ""
	if result != nil && result.ShouldBlock {
		status = "blocked"
		msg = "blocked by security policy"
	}
	span.Finish(status, msg)
	rec := span.Record()
	rec.Input = truncate(content, maxSpanBodyChars)
	if result != nil {
		rec.StatusMessage = summarizeScan(result)
	}
	if h.recorder != nil {
		h.recorder.RecordObservation(rec)
	}

	if result != nil && len(result.Findings) > 0 {
		slog.Info("security scan complete",
			"trace_id", traceID,
			"threat_score", result.ThreatScore,
			"findings", len(result.Findings),
			"action", result.Action,
			"duration_ms", result.ScanDuration.Milliseconds(),
		)
	}

	return result
}

// summarizeScan renders a short human-readable status message from a scan
// result: "injection:high(0.72), pii:medium(0.35)". Empty string when the
// scan produced no findings.
func summarizeScan(result *security.ScanResult) string {
	if result == nil || len(result.Findings) == 0 {
		return ""
	}
	parts := make([]string, 0, len(result.Findings))
	for _, f := range result.Findings {
		parts = append(parts, fmt.Sprintf("%s:%s(%.2f)", f.ThreatType, f.Severity, f.Score))
	}
	return strings.Join(parts, ", ")
}

// maxSpanBodyChars mirrors maxBodyChars but at a lower cap — span inputs
// are often prompts alone so we don't need the full request envelope.
const maxSpanBodyChars = 64 * 1024

// recordInput bundles everything the recorder needs for a single trace
// row. Grouped so the hot-path call site stays readable as more fields
// accumulate (bodies, token counts, end-user context, session id).
//
// IPAddress + UserAgent feed the threat-row enrichment when scan
// findings fire. Before threat-recording moved into recordTrace, the
// inline path read these from r.RemoteAddr / r.UserAgent() directly;
// keep them on the input so the new path doesn't drop the columns.
type recordInput struct {
	TraceID      uuid.UUID
	Info         *auth.APIKeyInfo
	Model        string
	Provider     string
	Path         string
	Start        time.Time
	Status       string
	Scan         *security.ScanResult
	ReqBody      []byte
	RespBody     []byte
	InputTokens  int
	OutputTokens int
	Headers      http.Header
	IPAddress    string
	UserAgent    string
}

// recordTrace enqueues a trace + analytics + threat rows. Non-blocking;
// events are dropped if the recorder buffer is saturated. Delegates to
// llmpipeline.RecordChatTrace so gateway and workspace write the same
// shape from one code path — no drift, no double-writes.
func (h *Handler) recordTrace(in recordInput) {
	if h.recorder == nil {
		return
	}
	now := time.Now()
	endUserID := ""
	sessionID := ""
	environment := ""
	release := ""
	traceName := ""
	var tags map[string]string
	if in.Headers != nil {
		endUserID = in.Headers.Get("X-End-User-Id")
		sessionID = in.Headers.Get("X-Session-Id")
		environment = in.Headers.Get("X-Bastio-Environment")
		release = in.Headers.Get("X-Bastio-Release")
		traceName = in.Headers.Get("X-Bastio-Trace-Name")
		tags = tagsFromHeaders(in.Headers)
	}
	llmpipeline.RecordChatTrace(llmpipeline.ChatTraceInput{
		Recorder:     h.recorder,
		TraceID:      in.TraceID,
		CustomerID:   in.Info.CustomerID,
		APIKeyID:     in.Info.ID,
		Method:       "POST",
		Path:         in.Path,
		Provider:     in.Provider,
		Model:        in.Model,
		StartedAt:    in.Start,
		CompletedAt:  now,
		InputTokens:  in.InputTokens,
		OutputTokens: in.OutputTokens,
		CostCents:    llmpipeline.EstimateCostCents(in.Model, in.InputTokens, in.OutputTokens),
		Status:       in.Status,
		HTTPStatus:   statusToHTTPCode(in.Status),
		EndUserID:    endUserID,
		SessionID:    sessionID,
		IPHash:       in.IPAddress,
		UserAgent:    in.UserAgent,
		Environment:  environment,
		Release:      release,
		TraceName:    traceName,
		Tags:         tags,
		RequestBody:  string(in.ReqBody),
		ResponseBody: string(in.RespBody),
		ScanResult:   in.Scan,
	})
}

// truncate clips s to n chars with a tail marker. Used by span-input
// truncation (64KiB cap, smaller than the trace bodies' 256KiB cap
// which lives in llmpipeline.TruncateBody).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n...[truncated]"
}

func statusToHTTPCode(status string) uint16 {
	switch status {
	case "ok":
		return 200
	case "blocked":
		return 403
	case "rate_limited":
		return 429
	case "error":
		return 502
	default:
		return 200
	}
}

// Cost estimation moved to internal/llmpipeline.EstimateCostCents.

// postProcessResponse applies response-side PII handling to an
// OpenAI-shape chat completion body: optional scan-for-new-PII (mask
// or block) followed by restoration of placeholders from the
// request-scoped TokenMap. Returns (newBody, false, "") when the body
// was rewritten, (nil, false, "") when no change was needed, and
// (nil, true, reason) when the response must be blocked.
//
// Order is scan → restore. Scanning runs on the tokenized text so
// placeholders are opaque to the PII regex — only genuinely new PII
// (hallucination / RAG leak) is flagged.
func (h *Handler) postProcessResponse(ctx context.Context, raw []byte) ([]byte, bool, string) {
	profile, ok := security.ProfileFromContext(ctx)
	if !ok {
		return nil, false, ""
	}
	tm, hasTM := security.TokenMapFromContext(ctx)
	needScan := profile.PIIScanResponse && h.security != nil
	needRestore := hasTM && profile.PIIRestoreResponse
	if !needScan && !needRestore {
		return nil, false, ""
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, false, ""
	}
	choicesRaw, exists := envelope["choices"]
	if !exists {
		return nil, false, ""
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(choicesRaw, &choices); err != nil {
		return nil, false, ""
	}

	changed := false
	for i, craw := range choices {
		var choice map[string]json.RawMessage
		if err := json.Unmarshal(craw, &choice); err != nil {
			continue
		}
		mraw, exists := choice["message"]
		if !exists {
			continue
		}
		var message map[string]json.RawMessage
		if err := json.Unmarshal(mraw, &message); err != nil {
			continue
		}

		contentChanged, blocked, reason := rewriteMessageContent(ctx, h.security, message, tm, needScan, needRestore)
		if blocked {
			return nil, true, reason
		}
		if !contentChanged {
			continue
		}

		newMessage, err := json.Marshal(message)
		if err != nil {
			continue
		}
		choice["message"] = newMessage
		newChoice, err := json.Marshal(choice)
		if err != nil {
			continue
		}
		choices[i] = newChoice
		changed = true
	}

	if !changed {
		return nil, false, ""
	}
	newChoices, err := json.Marshal(choices)
	if err != nil {
		return nil, false, ""
	}
	envelope["choices"] = newChoices
	out, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, ""
	}
	return out, false, ""
}

// rewriteMessageContent rewrites the `content` string field (and
// `tool_calls[].function.arguments` when present) of an OpenAI-shape
// assistant message in-place. Returns whether anything changed, whether
// the caller must block, and the block reason. The message map is
// mutated directly; callers re-marshal.
func rewriteMessageContent(ctx context.Context, engine *security.Engine, message map[string]json.RawMessage, tm *security.TokenMap, needScan, needRestore bool) (changed, blocked bool, reason string) {
	if raw, ok := message["content"]; ok {
		var content string
		if err := json.Unmarshal(raw, &content); err == nil {
			next, blk, why := processAssistantText(ctx, engine, content, tm, needScan, needRestore)
			if blk {
				return false, true, why
			}
			if next != content {
				cb, err := json.Marshal(next)
				if err == nil {
					message["content"] = cb
					changed = true
				}
			}
		}
	}

	if raw, ok := message["tool_calls"]; ok {
		var toolCalls []json.RawMessage
		if err := json.Unmarshal(raw, &toolCalls); err == nil {
			tcChanged := false
			for i, tc := range toolCalls {
				var tcobj map[string]json.RawMessage
				if err := json.Unmarshal(tc, &tcobj); err != nil {
					continue
				}
				fnRaw, ok := tcobj["function"]
				if !ok {
					continue
				}
				var fn map[string]json.RawMessage
				if err := json.Unmarshal(fnRaw, &fn); err != nil {
					continue
				}
				argsRaw, ok := fn["arguments"]
				if !ok {
					continue
				}
				var args string
				if err := json.Unmarshal(argsRaw, &args); err != nil {
					continue
				}
				next, blk, why := processAssistantText(ctx, engine, args, tm, needScan, needRestore)
				if blk {
					return false, true, why
				}
				if next == args {
					continue
				}
				ab, err := json.Marshal(next)
				if err != nil {
					continue
				}
				fn["arguments"] = ab
				nfn, err := json.Marshal(fn)
				if err != nil {
					continue
				}
				tcobj["function"] = nfn
				ntc, err := json.Marshal(tcobj)
				if err != nil {
					continue
				}
				toolCalls[i] = ntc
				tcChanged = true
			}
			if tcChanged {
				ntcb, err := json.Marshal(toolCalls)
				if err == nil {
					message["tool_calls"] = ntcb
					changed = true
				}
			}
		}
	}

	return changed, false, ""
}

// processAssistantText runs the scan-then-restore pipeline on a single
// assistant-emitted string. Response-side scan always masks (no one to
// send placeholders to); a block request propagates up.
func processAssistantText(ctx context.Context, engine *security.Engine, text string, tm *security.TokenMap, needScan, needRestore bool) (string, bool, string) {
	if needScan && engine != nil {
		scan := engine.Scan(ctx, &security.ScanRequest{
			Content:   text,
			PIIAction: security.ActionMask,
		})
		if scan != nil && scan.ShouldBlock {
			return text, true, "pii detected in response"
		}
		if scan != nil && scan.SanitizedContent != "" && scan.SanitizedContent != text {
			text = scan.SanitizedContent
		}
	}
	if needRestore && tm != nil {
		restored, n := tm.Restore(text)
		if n > 0 {
			text = restored
		}
	}
	return text, false, ""
}

// sanitizeOutboundBody rewrites user-role messages in an OpenAI-shape
// chat completions body, replacing each message's string content with
// the sanitized form (masked or tokenized). Non-string content fields
// (multimodal content arrays) are left untouched — this is a known v1
// limitation. Returns the rewritten body and true when at least one
// message was changed; the original body and false otherwise.
func sanitizeOutboundBody(body []byte, engine *security.Engine, action security.Action, tm *security.TokenMap) ([]byte, bool) {
	if engine == nil {
		return body, false
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return body, false
	}
	rawMsgs, ok := envelope["messages"]
	if !ok {
		return body, false
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(rawMsgs, &msgs); err != nil {
		return body, false
	}

	changed := false
	for i, raw := range msgs {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		var role string
		if err := json.Unmarshal(msg["role"], &role); err != nil || role != "user" {
			continue
		}
		var content string
		if err := json.Unmarshal(msg["content"], &content); err != nil {
			// Structured multimodal content — skip for v1.
			continue
		}
		sanitized := engine.SanitizeWithMap(content, action, tm)
		if sanitized == content {
			continue
		}
		cb, err := json.Marshal(sanitized)
		if err != nil {
			continue
		}
		msg["content"] = cb
		newMsg, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		msgs[i] = newMsg
		changed = true
	}
	if !changed {
		return body, false
	}

	newMsgsBytes, err := json.Marshal(msgs)
	if err != nil {
		return body, false
	}
	envelope["messages"] = newMsgsBytes
	out, err := json.Marshal(envelope)
	if err != nil {
		return body, false
	}
	return out, true
}

// extractUserContent pulls the text content from chat messages for security scanning.
func extractUserContent(messages []json.RawMessage) string {
	var parts []string
	for _, raw := range messages {
		var msg struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil {
			if msg.Role == "user" {
				parts = append(parts, msg.Content)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// extractAnthropicUserContent pulls scannable text from an Anthropic
// Messages-API request body.
//
// Anthropic messages differ from OpenAI in two ways:
//   - Each message's `content` may be a plain string OR an array of
//     content blocks ({type: "text", text: "..."}, plus non-text block
//     types we skip).
//   - The system prompt lives at the top level in `system`, not inside
//     the messages array. It's operator-authored — not user input —
//     so the inbound scan deliberately ignores it.
//
// Non-text blocks (images, tool results) are skipped; they're not
// something the text-based detectors can meaningfully evaluate. If/when
// multimodal detection lands, this extractor is the right place to
// surface image/tool content too.
func extractAnthropicUserContent(body []byte) string {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	var parts []string
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		// Try string first — the common case for simple prompts.
		var asString string
		if err := json.Unmarshal(m.Content, &asString); err == nil {
			if asString != "" {
				parts = append(parts, asString)
			}
			continue
		}
		// Fall back to content blocks — the multimodal / rich shape.
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err == nil {
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					parts = append(parts, b.Text)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// customerIDFromInfo returns the customer id on an APIKeyInfo or the zero
// UUID when the auth context wasn't populated — keeps the generation-span
// builders safe to call from error paths where info may be nil.
func customerIDFromInfo(info *auth.APIKeyInfo) uuid.UUID {
	if info == nil {
		return uuid.Nil
	}
	return info.CustomerID
}

// extractModelParameters pulls the generation knobs (temperature, top_p,
// max_tokens, ...) out of a chat-completions request body and returns
// them as a compact JSON string suitable for persisting on the span's
// model_parameters column. Returns "" when the body isn't a JSON object
// or no recognised parameters are present.
func extractModelParameters(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	keep := []string{
		"temperature", "top_p", "top_k", "max_tokens", "max_completion_tokens",
		"presence_penalty", "frequency_penalty", "stop", "seed",
		"response_format", "tools", "tool_choice", "reasoning_effort",
	}
	out := make(map[string]json.RawMessage, len(keep))
	for _, k := range keep {
		if v, ok := probe[k]; ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// resolveSpanModel prefers the provider-reported model (e.g. the exact
// snapshot gpt-4o-2024-08-06) over the requested alias (gpt-4o) so the
// span accurately reflects what the user's tokens were billed against.
func resolveSpanModel(respModel, reqModel string) string {
	if respModel != "" {
		return respModel
	}
	return reqModel
}

// tagsFromHeaders extracts `X-Bastio-Tag-*` headers into a map. The header
// suffix is lowercased to give canonical tag keys regardless of how the
// client capitalized them. Example: `X-Bastio-Tag-Feature: checkout` →
// `{"feature": "checkout"}`. Returns nil when no tags were sent so the
// recorder can distinguish "no tags" from "empty tags".
func tagsFromHeaders(h http.Header) map[string]string {
	const prefix = "X-Bastio-Tag-"
	var out map[string]string
	for k, v := range h {
		if len(v) == 0 || len(k) <= len(prefix) {
			continue
		}
		if !strings.EqualFold(k[:len(prefix)], prefix) {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[strings.ToLower(k[len(prefix):])] = v[0]
	}
	return out
}

// providerStatusRE pulls a 3-digit status code out of error strings
// like `openai error (status 401): {...}`. Used by writeProviderError
// to propagate the upstream HTTP status to the client instead of
// flattening every provider failure to 502.
var providerStatusRE = regexp.MustCompile(`status (\d{3})`)

// writeProviderError serializes a provider failure as a properly
// JSON-encoded response. Replaces the older `fmt.Sprintf` path that
// embedded raw err.Error() (often containing a JSON body with quotes
// and newlines) directly into a JSON string, producing malformed
// payloads that Caddy fronted as 502 with no body.
//
// Behavior:
//   - extracts the upstream HTTP status code from the error message
//     (provider clients format errors as "<provider> error (status NNN): ...")
//   - falls back to 502 if no status is parseable
//   - returns `{"error": {"message": "...", "provider": "..."}}` JSON
//     with the matching status code on the response.
func writeProviderError(w http.ResponseWriter, providerName string, err error) string {
	msg := err.Error()
	status := http.StatusBadGateway
	if m := providerStatusRE.FindStringSubmatch(msg); len(m) == 2 {
		if code, parseErr := strconv.Atoi(m[1]); parseErr == nil && code >= 400 && code < 600 {
			status = code
		}
	}
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message":  msg,
			"provider": providerName,
			"type":     "provider_error",
		},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	return string(body)
}
