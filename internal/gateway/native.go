package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/overlay"
)

type routeError struct {
	status  int
	message string
}

func (e *routeError) Error() string { return e.message }

type nativeRoute struct {
	provider providers.Provider
	proxyID  uuid.UUID
	key      string
	model    string
}

func (h *Handler) nativeRoute(r *http.Request, info *auth.APIKeyInfo, requested providers.Provider, model string) (*nativeRoute, error) {
	selected := requested
	if header := r.Header.Get("X-Bastio-Provider"); header != "" {
		if requested != "" && requested != providers.Provider(header) {
			return nil, &routeError{400, "provider header conflicts with API route"}
		}
		selected = providers.Provider(header)
	}
	proxyID := chi.URLParam(r, "proxyId")
	if proxyID == "" {
		proxyID = r.Header.Get("X-Bastio-Proxy-Id")
	}
	if proxyID == "" {
		proxyID = r.URL.Query().Get("proxy_id")
	}
	var scoped []string
	for _, scope := range info.Scopes {
		if strings.HasPrefix(scope, "proxy:") {
			scoped = append(scoped, strings.TrimPrefix(scope, "proxy:"))
		}
	}
	if proxyID == "" && len(scoped) == 1 {
		proxyID = scoped[0]
	}
	if len(scoped) > 0 {
		allowed := false
		for _, id := range scoped {
			if id == proxyID {
				allowed = true
			}
		}
		if !allowed {
			return nil, &routeError{403, "API key is restricted to its configured proxies"}
		}
	}
	route := &nativeRoute{model: model}
	if proxyID != "" {
		id, err := uuid.Parse(proxyID)
		if err != nil {
			return nil, &routeError{400, "invalid proxy ID"}
		}
		if h.proxies == nil {
			return nil, &routeError{404, "proxy not found"}
		}
		p, err := h.proxies.GetByID(r.Context(), info.CustomerID, id)
		if err != nil || p.CustomerID != info.CustomerID || !p.IsActive {
			return nil, &routeError{404, "proxy not found"}
		}
		if selected != "" && selected != p.TargetProvider {
			return nil, &routeError{400, "provider conflicts with configured proxy"}
		}
		selected, route.proxyID = p.TargetProvider, id
		if route.model == "" {
			route.model = p.TargetModel
		}
	}
	if selected == "" {
		selected = inferProvider(route.model)
	}
	route.provider = selected
	if h.nativeKeyResolver != nil {
		route.key = h.nativeKeyResolver(r, selected)
	}
	if _, err := h.providers.Get(selected); err != nil {
		return nil, &routeError{400, "unknown provider"}
	}
	if h.proxies != nil {
		// Default credentials are tenant-scoped. Explicit proxy credentials
		// take precedence; unrelated proxies must not be chosen arbitrarily.
		route.key, _ = h.proxies.ResolveProviderKey(r.Context(), info.CustomerID, route.proxyID, selected)
	}
	if route.key == "" {
		switch selected {
		case providers.ProviderXAI:
			route.key = os.Getenv("XAI_API_KEY")
		case providers.ProviderOpenRouter:
			route.key = os.Getenv("OPENROUTER_API_KEY")
		case providers.ProviderOpenAI:
			route.key = os.Getenv("OPENAI_API_KEY")
		case providers.ProviderAnthropic:
			route.key = os.Getenv("ANTHROPIC_API_KEY")
		}
	}
	return route, nil
}

func writeRouteError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if e, ok := err.(*routeError); ok {
		status = e.status
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		Error: struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}{Type: "gateway_error", Message: err.Error()},
	})
}

// NativeAPI preserves provider API routes beneath an explicit provider base
// or proxy base, including new endpoints without schema-specific translations.
func (h *Handler) NativeAPI(provider providers.Provider, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoint := path
		if endpoint == "" {
			endpoint = "/" + chi.URLParam(r, "*")
		}
		if strings.HasSuffix(endpoint, "/*") {
			endpoint = strings.TrimSuffix(endpoint, "*") + chi.URLParam(r, "*")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			if _, oversized := err.(*http.MaxBytesError); oversized {
				writeRouteError(w, &routeError{413, "request exceeds gateway body limit"})
				return
			}
			writeRouteError(w, &routeError{400, "failed to read request"})
			return
		}
		h.forwardNativeAPI(w, r, provider, endpoint, body)
	}
}

func (h *Handler) forwardNativeAPI(w http.ResponseWriter, r *http.Request, provider providers.Provider, endpoint string, body []byte) {
	start := time.Now()
	info, ok := auth.FromContext(r.Context())
	if !ok {
		writeRouteError(w, &routeError{401, "unauthorized"})
		return
	}
	var obj map[string]json.RawMessage
	isJSON := len(body) > 0 && (r.Header.Get("Content-Type") == "" || strings.Contains(r.Header.Get("Content-Type"), "json"))
	if isJSON && (json.Unmarshal(body, &obj) != nil || obj == nil) {
		writeRouteError(w, &routeError{400, "invalid JSON request body"})
		return
	}
	var model string
	_ = json.Unmarshal(obj["model"], &model)
	route, err := h.nativeRoute(r, info, provider, model)
	if err != nil {
		writeRouteError(w, err)
		return
	}
	var fallbacks []string
	_ = json.Unmarshal(obj["fallback_models"], &fallbacks)
	if len(fallbacks) == 0 {
		for _, value := range strings.Split(r.Header.Get("X-Bastio-Fallback-Models"), ",") {
			if value = strings.TrimSpace(value); value != "" {
				fallbacks = append(fallbacks, value)
			}
		}
	}
	if len(fallbacks) > 0 && route.provider == providers.ProviderOpenRouter && endpoint != "/chat/completions" {
		writeRouteError(w, &routeError{400, "Bastio fallback_models on native OpenRouter routes require Chat Completions"})
		return
	}
	if len(fallbacks) > 0 && route.provider != providers.ProviderOpenRouter {
		writeRouteError(w, &routeError{400, "use the shared chat route for Bastio model fallbacks, or OpenRouter native models routing"})
		return
	}
	client, err := h.providers.Native(route.provider)
	if err != nil {
		writeRouteError(w, err)
		return
	}
	if route.key == "" && !(route.provider == providers.ProviderOpenRouter && r.Method == http.MethodGet && (strings.HasPrefix(endpoint, "/models") || endpoint == "/providers")) {
		writeRouteError(w, &routeError{502, "no provider credential configured"})
		return
	}
	// Native Messages must go to Claude or OpenRouter, never OpenAI's Chat
	// endpoint. Explicit provider bases retain all other endpoint semantics.
	traceID := uuid.New()
	w.Header().Set("X-Bastio-Trace-Id", traceID.String())
	w.Header().Set("X-Bastio-Request-Id", traceID.String())
	profile, snap, ident, profileErr := h.resolveProfileWithOverlay(r.Context(), info.CustomerID, route.proxyID)
	if (profileErr != nil || h.security == nil) && h.failClosed() && !gatewaypolicy.From(r.Context()).MonitorOnly {
		h.writeFailClosedBlock(w, traceID, "security scan unavailable")
		return
	}
	ctx := security.WithProfile(r.Context(), profile)
	if snap != nil {
		ctx = overlay.WithActive(ctx, snap, ident)
	}
	r = r.WithContext(ctx)
	if rejected, allowed := validateImageContent(body, h.imageURLAllowlist); !allowed {
		writeRouteError(w, &routeError{403, "image URL is outside trusted hosts: " + rejected})
		return
	}
	scan := h.runSecurityScan(r, info, extractInputText(body), traceID, profile)
	status := "ok"
	var responseBody []byte
	var inTokens, outTokens int
	metadata := &nativeMetadata{HTTPStatus: 200}
	defer func() {
		h.recordTrace(r.Context(), recordInput{TraceID: traceID, ProxyID: route.proxyID, Method: r.Method, HTTPStatus: metadata.HTTPStatus, CostCents: metadata.CostCents, Info: info, Model: route.model, Provider: string(route.provider), Path: r.URL.Path,
			Start: start, Status: status, Scan: scan, ReqBody: body, RespBody: responseBody, InputTokens: inTokens, OutputTokens: outTokens,
			Headers: r.Header, IPAddress: r.RemoteAddr, UserAgent: r.UserAgent()})
	}()
	if scan != nil && scan.ShouldBlock {
		metadata.HTTPStatus = 403
		status = "blocked"
		responseBody = blockedResponseBody(traceID, scan.ThreatTypes, scan.ThreatScore)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write(responseBody)
		return
	}
	if scan != nil && (scan.Action == security.ActionMask || scan.Action == security.ActionTokenize) {
		var tm *security.TokenMap
		if scan.Action == security.ActionTokenize {
			tm = security.NewRequestTokenMap(profile.PIITokenStyle)
		}
		body, _ = sanitizeInput(body, h.security, scan.Action, tm)
		if tm != nil && tm.Size() > 0 {
			r = r.WithContext(security.WithTokenMap(r.Context(), tm))
		}
	}
	if isJSON {
		_ = json.Unmarshal(body, &obj)
		_, hasModel := obj["model"]
		_, hasModels := obj["models"]
		if route.model != "" && (hasModel || (!hasModels && IsGenerationEndpoint(endpoint) && endpoint != "/batches" && endpoint != "/messages/batches")) {
			model = route.model
			if route.provider == providers.ProviderOpenRouter {
				model = strings.TrimPrefix(model, "openrouter/")
			}
			if route.provider == providers.ProviderOpenAI {
				model = strings.TrimPrefix(model, "openai/")
			}
			if route.provider == providers.ProviderAnthropic {
				model = strings.TrimPrefix(model, "anthropic/")
			}
			obj["model"], _ = json.Marshal(model)
		}
		if len(fallbacks) > 0 {
			if hasModels {
				writeRouteError(w, &routeError{400, "models and fallback_models cannot both define a fallback chain"})
				return
			}
			chain := []string{}
			if model != "" {
				chain = append(chain, model)
			}
			for _, fallback := range fallbacks {
				chain = append(chain, strings.TrimPrefix(fallback, "openrouter/"))
			}
			obj["models"], _ = json.Marshal(chain)
			obj["route"] = json.RawMessage(`"fallback"`)
			delete(obj, "model")
		}
		delete(obj, "fallback_models")
		body, _ = json.Marshal(obj)
	}
	query := r.URL.Query()
	query.Del("proxy_id")
	resp, err := client.Forward(r.Context(), &providers.NativeRequest{Method: r.Method, Path: endpoint, Query: query, Headers: r.Header, Body: body}, route.key)
	if err != nil {
		status = "error"
		metadata.HTTPStatus = 502
		writeRouteError(w, err)
		return
	}
	defer resp.Body.Close()
	metadata.HTTPStatus = uint16(resp.StatusCode)
	copyProviderResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Bastio-Latency-Ms", fmt.Sprint(time.Since(start).Milliseconds()))
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		responseBody, inTokens, outTokens, status = h.forwardNativeStream(w, r, resp, metadata)
		if gatewaypolicy.From(r.Context()).MonitorOnly {
			scan = mergeMonitorScans(scan, h.runSecurityScanFrom(r, info, OutputText(responseBody), traceID, profile, "response"))
		}
		return
	}
	responseBody, err = io.ReadAll(resp.Body)
	if err != nil {
		status = "error"
		writeRouteError(w, err)
		return
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.Contains(resp.Header.Get("Content-Type"), "json") {
		inTokens, outTokens = nativeUsage(responseBody)
		metadata.CostCents = nativeCost(responseBody)
		var reported struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(responseBody, &reported) == nil && reported.Model != "" {
			route.model = reported.Model
		}
		if gatewaypolicy.From(r.Context()).MonitorOnly {
			scan = mergeMonitorScans(scan, h.runSecurityScanFrom(r, info, OutputText(responseBody), traceID, profile, "response"))
		}
		processed, blocked, reason := h.postProcessNativeResponse(r, responseBody)
		if blocked {
			status = "blocked"
			metadata.HTTPStatus = 403
			writeRouteError(w, &routeError{403, reason})
			return
		}
		if processed != nil {
			responseBody = processed
		}
	}
	if resp.StatusCode >= 400 {
		status = "error"
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(responseBody)
}

func copyProviderResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		k := strings.ToLower(key)
		if k == "content-type" || k == "retry-after" || k == "request-id" || k == "x-request-id" ||
			k == "content-disposition" || k == "content-range" || k == "accept-ranges" || k == "etag" || k == "last-modified" ||
			k == "openai-processing-ms" || k == "openai-version" || k == "anthropic-organization-id" ||
			k == "cache-control" || strings.HasPrefix(k, "x-ratelimit-") || strings.HasPrefix(k, "anthropic-ratelimit-") || strings.HasPrefix(k, "x-openrouter-") {
			dst[key] = append([]string(nil), values...)
		}
	}
}

func nativeUsage(raw []byte) (int, int) {
	var obj struct {
		Usage struct {
			Input      int `json:"input_tokens"`
			Output     int `json:"output_tokens"`
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
		Response json.RawMessage `json:"response"`
		Message  json.RawMessage `json:"message"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return 0, 0
	}
	in, out := obj.Usage.Input+obj.Usage.Prompt, obj.Usage.Output+obj.Usage.Completion
	if len(obj.Response) > 0 {
		return nativeUsage(obj.Response)
	}
	if len(obj.Message) > 0 {
		return nativeUsage(obj.Message)
	}
	return in, out
}

type nativeMetadata struct {
	HTTPStatus uint16
	CostCents  *float64
}

func nativeCost(raw []byte) *float64 {
	var obj struct {
		Usage struct {
			Cost *float64 `json:"cost"`
		} `json:"usage"`
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	if obj.Usage.Cost != nil {
		cents := *obj.Usage.Cost * 100
		return &cents
	}
	if len(obj.Response) > 0 {
		return nativeCost(obj.Response)
	}
	return nil
}

// tryNativeChat routes real native providers through a wire-preserving
// transport; legacy/mocked clients still use the normalized chat pipeline.
func (h *Handler) tryNativeChat(w http.ResponseWriter, r *http.Request, body []byte, model string) bool {
	info, ok := auth.FromContext(r.Context())
	if !ok {
		return false
	}
	candidate := inferProvider(model)
	if header := r.Header.Get("X-Bastio-Provider"); header != "" {
		candidate = providers.Provider(header)
	}
	if _, err := h.providers.Native(candidate); err != nil && r.Header.Get("X-Bastio-Proxy-Id") == "" && len(info.Scopes) == 0 {
		return false
	}
	var fallback struct {
		Models []string `json:"fallback_models"`
	}
	_ = json.Unmarshal(body, &fallback)
	route, err := h.nativeRoute(r, info, "", model)
	if err != nil {
		writeRouteError(w, err)
		return true
	}
	// OpenRouter owns its fallback chain; direct provider chains retain the
	// normalized retry path so they can switch providers before commitment.
	if route.provider != providers.ProviderOpenRouter && (len(fallback.Models) > 0 || r.Header.Get("X-Bastio-Fallback-Models") != "") {
		return false
	}
	if _, err := h.providers.Native(route.provider); err != nil {
		return false
	}
	if route.provider == providers.ProviderAnthropic {
		return false
	} // Chat-shaped Claude calls still require translation.
	h.forwardNativeAPI(w, r, "", "/chat/completions", body)
	return true
}

// mapNativeOutput covers native Messages/Responses and OpenAI assistant text,
// including reasoning and tool arguments, leaving provider metadata untouched.
func mapNativeOutput(raw json.RawMessage, transform textTransform) (json.RawMessage, bool) {
	return mapNativeOutputPolicy(raw, transform, transform)
}

func mapNativeOutputPolicy(raw json.RawMessage, transform, thinking textTransform) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return raw, false
	}
	var kind string
	_ = json.Unmarshal(obj["type"], &kind)
	changed := false
	if answers, ok := obj["answers"]; ok {
		next, ch := mapDataStrings(answers, transform)
		obj["answers"], changed = next, changed || ch
	}
	for _, key := range []string{"content", "output", "text", "response", "arguments", "refusal", "reasoning", "reasoning_content", "reasoning_details", "thinking", "partial_json"} {
		if value, exists := obj[key]; exists {
			apply := transform
			if (kind == "thinking" || kind == "thinking_delta") && key == "thinking" {
				apply = thinking
			}
			next, ch := mapContentPolicy(value, apply, thinking)
			obj[key], changed = next, changed || ch
		}
	}
	for _, key := range []string{"choices", "message", "response", "content_block", "item", "part", "delta"} {
		value, exists := obj[key]
		if !exists {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(value, &items) == nil && items != nil {
			listChanged := false
			for i, item := range items {
				next, ch := mapNativeOutputPolicy(item, transform, thinking)
				items[i], listChanged = next, listChanged || ch
			}
			if listChanged {
				obj[key], _ = json.Marshal(items)
				changed = true
			}
		} else {
			next, ch := mapNativeOutputPolicy(value, transform, thinking)
			if key == "delta" && !ch {
				next, ch = mapContentPolicy(value, transform, thinking)
			}
			obj[key], changed = next, changed || ch
		}
	}
	if value, exists := obj["tool_calls"]; exists {
		var calls []map[string]json.RawMessage
		if json.Unmarshal(value, &calls) == nil {
			listChanged := false
			for _, call := range calls {
				next, ch := mapContent(call["function"], transform)
				if ch {
					call["function"] = next
					listChanged = true
				}
			}
			if listChanged {
				obj["tool_calls"], _ = json.Marshal(calls)
				changed = true
			}
		}
	}
	if !changed {
		return raw, false
	}
	out, _ := json.Marshal(obj)
	return out, true
}

func (h *Handler) postProcessNativeResponse(r *http.Request, raw []byte) ([]byte, bool, string) {
	if gatewaypolicy.From(r.Context()).MonitorOnly {
		return nil, false, ""
	}
	profile, ok := security.ProfileFromContext(r.Context())
	if !ok {
		return nil, false, ""
	}
	tm, hasTM := security.TokenMapFromContext(r.Context())
	needScan, needRestore := profile.PIIScanResponse && h.security != nil, hasTM && profile.PIIRestoreResponse
	if !needScan && !needRestore {
		return nil, false, ""
	}
	blocked, reason := false, ""
	out, changed := mapNativeOutputPolicy(raw, func(text string) string {
		next, block, why := processAssistantText(r.Context(), h.security, text, tm, needScan, needRestore)
		if block {
			blocked, reason = true, why
		}
		return next
	}, func(text string) string {
		_, block, why := processSignedThinking(r, h.security, text, tm, needScan)
		if block {
			blocked, reason = true, why
		}
		return text
	})
	if blocked {
		return nil, true, reason
	}
	if changed {
		return out, false, ""
	}
	return nil, false, ""
}

// RegisterNativeRoutes installs provider-native APIs inside the caller's authenticated gateway group.
func (h *Handler) RegisterNativeRoutes(r chi.Router) {
	r.Post("/observe", h.Observe)
	r.Handle("/guard/{proxyId}/v1/*", h.NativeAPI("", ""))
	r.Handle("/typesafe/*", h.NativeAPI(providers.ProviderTypeSafe, ""))
	r.Handle("/openrouter/*", h.NativeAPI(providers.ProviderOpenRouter, ""))
	r.Handle("/xai/*", h.NativeAPI(providers.ProviderXAI, ""))
	r.Handle("/openai/*", h.NativeAPI(providers.ProviderOpenAI, ""))
	r.Handle("/anthropic/*", h.NativeAPI(providers.ProviderAnthropic, ""))
	r.Handle("/responses", h.NativeAPI("", "/responses"))
	r.Handle("/responses/*", h.NativeAPI("", "/responses/*"))
	r.Handle("/embeddings", h.NativeAPI("", "/embeddings"))
	r.Handle("/models", h.NativeAPI("", "/models"))
	r.Handle("/models/*", h.NativeAPI("", "/models/*"))
	r.Post("/messages", h.NativeAPI(providers.ProviderAnthropic, "/messages"))
	r.Post("/messages/count_tokens", h.NativeAPI(providers.ProviderAnthropic, "/messages/count_tokens"))
}

// Claude signatures cover thinking text, including tokenized values. Restore
// regular output while leaving thinking reusable in subsequent API requests.
func processSignedThinking(r *http.Request, engine *security.Engine, text string, tm *security.TokenMap, needScan bool) (string, bool, string) {
	next, blocked, reason := processAssistantText(r.Context(), engine, text, tm, needScan, false)
	if !blocked && next != text {
		return text, true, "security policy cannot rewrite signed thinking content"
	}
	return text, blocked, reason
}
