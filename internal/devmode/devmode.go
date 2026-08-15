package devmode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/bastio-ai/bastio/pkg/tenant"
)

// Config configures the zero-dependency local dev mode server.
type Config struct {
	Port         int
	UpstreamURL  string
	SecurityMode string // "fail-open" or "fail-closed"
	LogLevel     string
}

// DefaultConfig returns reasonable defaults for dev mode.
func DefaultConfig() Config {
	return Config{
		Port:         4000,
		SecurityMode: "fail-open",
		LogLevel:     "info",
	}
}

// TraceRingBuffer is a thread-safe in-memory ring buffer holding recent traces and threat events.
type TraceRingBuffer struct {
	mu           sync.RWMutex
	capacity     int
	traces       []*observability.TraceRecord
	threats      []*observability.ThreatEvent
	totalTraces  int64
	totalBlocked int64
	totalThreats int64
}

// NewTraceRingBuffer creates a ring buffer with the specified capacity.
func NewTraceRingBuffer(capacity int) *TraceRingBuffer {
	if capacity <= 0 {
		capacity = 500
	}
	return &TraceRingBuffer{
		capacity: capacity,
		traces:   make([]*observability.TraceRecord, 0, capacity),
		threats:  make([]*observability.ThreatEvent, 0, capacity*2),
	}
}

// RecordTrace records a trace record into the ring buffer.
func (b *TraceRingBuffer) RecordTrace(rec *observability.TraceRecord) {
	if rec == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.totalTraces++
	if rec.Status == "blocked" || rec.ThreatDetected {
		b.totalBlocked++
	}

	if len(b.traces) >= b.capacity {
		b.traces = b.traces[1:]
	}
	b.traces = append(b.traces, rec)
}

// RecordThreatEvent records a threat finding event.
func (b *TraceRingBuffer) RecordThreatEvent(ev *observability.ThreatEvent) {
	if ev == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.totalThreats++
	if len(b.threats) >= b.capacity*2 {
		b.threats = b.threats[1:]
	}
	b.threats = append(b.threats, ev)
}

// ListTraces returns up to limit traces (most recent first).
func (b *TraceRingBuffer) ListTraces(limit int) []*observability.TraceRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()

	n := len(b.traces)
	if limit <= 0 || limit > n {
		limit = n
	}

	out := make([]*observability.TraceRecord, 0, limit)
	for i := n - 1; i >= n-limit; i-- {
		out = append(out, b.traces[i])
	}
	return out
}

// GetTrace retrieves a trace by ID.
func (b *TraceRingBuffer) GetTrace(id uuid.UUID) *observability.TraceRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for i := len(b.traces) - 1; i >= 0; i-- {
		if b.traces[i].ID == id {
			return b.traces[i]
		}
	}
	return nil
}

// ListThreats returns recent threat events.
func (b *TraceRingBuffer) ListThreats(limit int) []*observability.ThreatEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	n := len(b.threats)
	if limit <= 0 || limit > n {
		limit = n
	}

	out := make([]*observability.ThreatEvent, 0, limit)
	for i := n - 1; i >= n-limit; i-- {
		out = append(out, b.threats[i])
	}
	return out
}

// AnalyticsOverview returns summary metrics.
func (b *TraceRingBuffer) AnalyticsOverview() map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var totalLatency int64
	for _, t := range b.traces {
		totalLatency += int64(t.DurationMs)
	}
	var avgLatency float64
	if len(b.traces) > 0 {
		avgLatency = float64(totalLatency) / float64(len(b.traces))
	}

	return map[string]any{
		"total_traces":    b.totalTraces,
		"blocked_traces":  b.totalBlocked,
		"total_threats":   b.totalThreats,
		"buffered_traces": len(b.traces),
		"avg_latency_ms":  avgLatency,
	}
}

// DevServer is the self-contained zero-dependency local dev server.
type DevServer struct {
	cfg           Config
	engine        *security.Engine
	providers     *providers.Registry
	traces        *TraceRingBuffer
	httpSrv       *http.Server
	router        chi.Router
	responseCache sync.Map
}

// BuildDefaultSecurityEngine constructs the security engine with all standalone detectors.
func BuildDefaultSecurityEngine() *security.Engine {
	return security.NewEngine(
		detection.NewInjectionDetector(),
		detection.NewPIIDetector(),
		detection.NewJailbreakDetector(),
		detection.NewSecretsDetector(),
		detection.NewIndirectInjectionDetector(),
		detection.NewExfilDetector(),
		detection.NewTopicPolicyDetector(nil, nil, 0),
	)
}

// BuildDefaultProviderRegistry builds the standard provider registry with all client implementations.
func BuildDefaultProviderRegistry() *providers.Registry {
	reg := providers.NewRegistry()
	reg.Register(providers.NewOpenAIClient())
	reg.Register(providers.NewAnthropicClient())
	reg.Register(providers.NewBedrockClient())
	reg.Register(providers.NewOllamaClient())
	reg.Register(providers.NewGeminiClient())
	reg.Register(providers.NewDeepSeekClient())
	reg.Register(providers.NewGroqClient())
	return reg
}

// NewServer initializes a new DevServer.
func NewServer(cfg Config) *DevServer {
	if cfg.Port <= 0 {
		cfg.Port = 4000
	}
	if cfg.SecurityMode == "" {
		cfg.SecurityMode = "fail-open"
	}

	engine := BuildDefaultSecurityEngine()
	providerReg := BuildDefaultProviderRegistry()
	traceBuf := NewTraceRingBuffer(500)

	s := &DevServer{
		cfg:       cfg,
		engine:    engine,
		providers: providerReg,
		traces:    traceBuf,
	}

	s.setupRoutes()
	return s
}

func (s *DevServer) setupRoutes() {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(90 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	}))

	r.Get("/health", s.handleHealth)
	r.Get("/ready", s.handleReady)
	r.Get("/", s.handleRoot)

	r.Route("/v1", func(r chi.Router) {
		r.Get("/", s.handleRoot)
		r.Post("/chat/completions", s.handleChatCompletions)
		r.Post("/guard/{proxyId}/v1/messages", s.handleAnthropicMessages)
		r.Post("/messages", s.handleAnthropicMessages)
		r.Post("/detect", s.handleDetect)
		r.Get("/traces", s.handleListTraces)
		r.Get("/traces/{id}", s.handleGetTrace)
		r.Get("/threats", s.handleListThreats)
		r.Get("/analytics/overview", s.handleAnalyticsOverview)
		r.Get("/config", s.handleConfig)
	})

	s.router = r
}

// Handler returns the HTTP handler for testing.
func (s *DevServer) Handler() http.Handler {
	return s.router
}

// Start begins listening on the configured port.
func (s *DevServer) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("bastio dev server listening", "addr", addr, "port", s.cfg.Port, "upstream", s.cfg.UpstreamURL)
		if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		slog.Info("dev server shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpSrv.Shutdown(shutdownCtx)
}

// Close closes the server.
func (s *DevServer) Close() error {
	if s.httpSrv != nil {
		return s.httpSrv.Close()
	}
	return nil
}

func (s *DevServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "healthy",
		"mode":       "dev",
		"postgres":   "in-memory",
		"redis":      "in-memory",
		"clickhouse": "in-memory",
	})
}

func (s *DevServer) handleReady(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ready",
		"mode":   "dev",
	})
}

func (s *DevServer) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    "bastio",
		"version": "dev",
		"mode":    "zero-dependency-local",
	})
}

func (s *DevServer) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"port":          s.cfg.Port,
		"upstream_url":  s.cfg.UpstreamURL,
		"security_mode": s.cfg.SecurityMode,
		"storage":       "in-memory",
		"detectors":     []string{"injection", "pii", "jailbreak", "secrets", "indirect_injection", "exfil"},
		"providers":     []string{"openai", "anthropic", "gemini", "deepseek", "groq", "bedrock", "ollama"},
	})
}

func (s *DevServer) handleListTraces(w http.ResponseWriter, r *http.Request) {
	traces := s.traces.ListTraces(50)
	writeJSON(w, http.StatusOK, map[string]any{
		"traces": traces,
		"total":  len(traces),
	})
}

func (s *DevServer) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid trace id"}`, http.StatusBadRequest)
		return
	}
	tr := s.traces.GetTrace(id)
	if tr == nil {
		http.Error(w, `{"error":"trace not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *DevServer) handleListThreats(w http.ResponseWriter, r *http.Request) {
	threats := s.traces.ListThreats(50)
	writeJSON(w, http.StatusOK, map[string]any{
		"threats": threats,
		"total":   len(threats),
	})
}

func (s *DevServer) handleAnalyticsOverview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.traces.AnalyticsOverview())
}

func (s *DevServer) handleDetect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content   string `json:"content"`
		EndUserID string `json:"end_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	scanReq := &security.ScanRequest{
		Content:   req.Content,
		EndUserID: req.EndUserID,
	}
	result := s.engine.Scan(r.Context(), scanReq)

	writeJSON(w, http.StatusOK, result)
}

func (s *DevServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	traceID := uuid.New()
	w.Header().Set("X-Bastio-Trace-Id", traceID.String())
	w.Header().Set("X-Bastio-Request-Id", traceID.String())

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request"}`, http.StatusBadRequest)
		return
	}

	var chatReq struct {
		Model          string            `json:"model"`
		FallbackModels []string          `json:"fallback_models"`
		Stream         bool              `json:"stream"`
		Messages       []json.RawMessage `json:"messages"`
	}
	_ = json.Unmarshal(body, &chatReq)

	userContent := extractUserContentFromRaw(chatReq.Messages)

	// Scan prompt content
	var scanResult *security.ScanResult
	if userContent != "" {
		scanResult = s.engine.Scan(r.Context(), &security.ScanRequest{Content: userContent})
	}

	if scanResult != nil && scanResult.ShouldBlock {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusForbidden)
		blockedObj := map[string]any{
			"error": map[string]any{
				"message":      "Request blocked by Bastio AI Security",
				"type":         "security_block",
				"threat_types": scanResult.ThreatTypes,
				"threat_score": scanResult.ThreatScore,
				"trace_id":     traceID.String(),
			},
		}
		blockedBody, _ := json.Marshal(blockedObj)
		_, _ = w.Write(blockedBody)

		s.recordDevTrace(traceID, chatReq.Model, "openai", r.URL.Path, start, "blocked", scanResult, body, blockedBody)
		return
	}

	// Mode A: If UpstreamURL is configured, reverse proxy to the upstream
	if s.cfg.UpstreamURL != "" {
		s.proxyToUpstream(w, r, body, traceID, chatReq.Model, start, scanResult)
		return
	}

	// In-memory Response Cache check (for non-streaming requests)
	var cacheKey string
	if !chatReq.Stream {
		cacheKey = fmt.Sprintf("llm_resp:%x", sha256.Sum256(body))
		if cached, ok := s.responseCache.Load(cacheKey); ok {
			cachedBytes := cached.([]byte)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Bastio-Cache", "HIT")
			w.Header().Set("X-Bastio-Trace-Id", traceID.String())
			w.Header().Set("X-Bastio-Latency-Ms", fmt.Sprintf("%d", time.Since(start).Milliseconds()))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cachedBytes)
			s.recordDevTrace(traceID, chatReq.Model, "cache", r.URL.Path, start, "ok", scanResult, body, cachedBytes)
			return
		}
	}

	// Mode B: Use provider mesh
	candidateModels := append([]string{chatReq.Model}, chatReq.FallbackModels...)
	var lastErr error
	var handled bool
	var selectedModel = chatReq.Model
	var selectedProvider = "openai"

	for i, m := range candidateModels {
		providerName := inferDevProvider(m)
		client, err := s.providers.Get(providerName)
		if err != nil {
			lastErr = err
			continue
		}

		apiKey := resolveDevAPIKey(r, providerName)
		selectedModel = m
		selectedProvider = string(providerName)

		if chatReq.Stream {
			pReq := &providers.ChatRequest{
				Model:  m,
				Stream: true,
				Raw:    body,
			}
			stream, err := client.ChatStream(r.Context(), pReq, apiKey)
			if err != nil {
				lastErr = err
				if i < len(candidateModels)-1 {
					slog.Warn("provider error, trying fallback", "model", sanitizeLog(m), "fallback", sanitizeLog(candidateModels[i+1]), "error", err)
					continue
				}
				break
			}
			if m != chatReq.Model {
				w.Header().Set("X-Bastio-Fallback-Used", m)
			}
			s.streamResponse(w, stream, traceID, selectedModel, selectedProvider, start, scanResult, body)
			handled = true
			break
		} else {
			pReq := &providers.ChatRequest{
				Model:  m,
				Stream: false,
				Raw:    body,
			}
			resp, err := client.Chat(r.Context(), pReq, apiKey)
			if err != nil {
				lastErr = err
				if i < len(candidateModels)-1 {
					slog.Warn("provider error, trying fallback", "model", sanitizeLog(m), "fallback", sanitizeLog(candidateModels[i+1]), "error", err)
					continue
				}
				break
			}
			if m != chatReq.Model {
				w.Header().Set("X-Bastio-Fallback-Used", m)
			}
			if cacheKey != "" {
				w.Header().Set("X-Bastio-Cache", "MISS")
				s.responseCache.Store(cacheKey, resp.Raw)
			}
			s.syncResponse(w, resp, traceID, selectedModel, selectedProvider, start, scanResult, body)
			handled = true
			break
		}
	}

	if !handled {
		errMsg := "provider failed"
		if lastErr != nil {
			errMsg = lastErr.Error()
		}
		writeJSONError(w, errMsg, http.StatusBadGateway)
		s.recordDevTrace(traceID, selectedModel, selectedProvider, r.URL.Path, start, "error", scanResult, body, []byte(errMsg))
	}
}

func (s *DevServer) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	traceID := uuid.New()
	w.Header().Set("X-Bastio-Trace-Id", traceID.String())
	w.Header().Set("X-Bastio-Request-Id", traceID.String())

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)

	var sb strings.Builder
	for _, m := range req.Messages {
		if m.Role == "user" {
			var str string
			if err := json.Unmarshal(m.Content, &str); err == nil {
				if str != "" {
					sb.WriteString(str)
					sb.WriteString("\n")
				}
				continue
			}
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(m.Content, &blocks); err == nil {
				for _, b := range blocks {
					if b.Type == "text" && b.Text != "" {
						sb.WriteString(b.Text)
						sb.WriteString("\n")
					}
				}
			}
		}
	}
	userContent := sb.String()

	var scanResult *security.ScanResult
	if userContent != "" {
		scanResult = s.engine.Scan(r.Context(), &security.ScanRequest{Content: userContent})
	}

	if scanResult != nil && scanResult.ShouldBlock {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusForbidden)
		blockedObj := map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":     "security_block",
				"message":  "Request blocked by Bastio AI Security",
				"trace_id": traceID.String(),
			},
		}
		blockedBody, _ := json.Marshal(blockedObj)
		_, _ = w.Write(blockedBody)

		s.recordDevTrace(traceID, req.Model, "anthropic", r.URL.Path, start, "blocked", scanResult, body, blockedBody)
		return
	}

	if s.cfg.UpstreamURL != "" {
		s.proxyToUpstream(w, r, body, traceID, req.Model, start, scanResult)
		return
	}

	client, err := s.providers.Get(providers.ProviderAnthropic)
	if err != nil {
		writeJSONError(w, "anthropic provider not available", http.StatusBadGateway)
		return
	}

	apiKey := resolveDevAPIKey(r, providers.ProviderAnthropic)
	pReq := &providers.ChatRequest{
		Model:  req.Model,
		Stream: req.Stream,
		Raw:    body,
	}

	if req.Stream {
		stream, err := client.ChatStream(r.Context(), pReq, apiKey)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadGateway)
			s.recordDevTrace(traceID, req.Model, "anthropic", r.URL.Path, start, "error", scanResult, body, []byte(err.Error()))
			return
		}
		s.streamResponse(w, stream, traceID, req.Model, "anthropic", start, scanResult, body)
	} else {
		resp, err := client.Chat(r.Context(), pReq, apiKey)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadGateway)
			s.recordDevTrace(traceID, req.Model, "anthropic", r.URL.Path, start, "error", scanResult, body, []byte(err.Error()))
			return
		}
		s.syncResponse(w, resp, traceID, req.Model, "anthropic", start, scanResult, body)
	}
}

func (s *DevServer) proxyToUpstream(w http.ResponseWriter, r *http.Request, body []byte, traceID uuid.UUID, model string, start time.Time, scan *security.ScanResult) {
	targetURL, err := url.Parse(s.cfg.UpstreamURL)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("invalid upstream URL: %s", err), http.StatusInternalServerError)
		return
	}

	upstreamPath := targetURL.ResolveReference(&url.URL{
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
	})

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamPath.String(), bytes.NewReader(body))
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to create upstream request: %s", err), http.StatusInternalServerError)
		return
	}

	for k, v := range r.Header {
		if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Content-Length") {
			continue
		}
		outReq.Header[k] = v
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(outReq)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("upstream error: %s", err), http.StatusBadGateway)
		s.recordDevTrace(traceID, model, "upstream", r.URL.Path, start, "error", scan, body, []byte(err.Error()))
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)

	s.recordDevTrace(traceID, model, "upstream", r.URL.Path, start, "ok", scan, body, respBody)
}

func (s *DevServer) syncResponse(w http.ResponseWriter, resp *providers.ChatResponse, traceID uuid.UUID, model, provider string, start time.Time, scan *security.ScanResult, reqBody []byte) {
	latencyMs := time.Since(start).Milliseconds()
	w.Header().Set("X-Bastio-Latency-Ms", fmt.Sprintf("%d", latencyMs))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var respBytes []byte
	if resp.Raw != nil {
		respBytes = resp.Raw
	} else {
		respObj := map[string]any{
			"id":            resp.ID,
			"model":         resp.Model,
			"content":       resp.Content,
			"role":          resp.Role,
			"finish_reason": resp.FinishReason,
			"usage": map[string]any{
				"prompt_tokens":     resp.InputTokens,
				"completion_tokens": resp.OutputTokens,
			},
		}
		respBytes, _ = json.Marshal(respObj)
	}

	_, _ = w.Write(respBytes)
	s.recordDevTrace(traceID, model, provider, "/v1/chat/completions", start, "ok", scan, reqBody, respBytes)
}

func (s *DevServer) streamResponse(w http.ResponseWriter, stream <-chan providers.StreamChunk, traceID uuid.UUID, model, provider string, start time.Time, scan *security.ScanResult, reqBody []byte) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Bastio-Latency-Ms", fmt.Sprintf("%d", time.Since(start).Milliseconds()))

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	for chunk := range stream {
		if chunk.Error != nil {
			slog.Error("stream error", "error", chunk.Error)
			break
		}
		if chunk.Done {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}
		buf.Write(chunk.Data)
		buf.WriteByte('\n')
		fmt.Fprintf(w, "data: %s\n\n", chunk.Data)
		flusher.Flush()
	}

	s.recordDevTrace(traceID, model, provider, "/v1/chat/completions", start, "ok", scan, reqBody, buf.Bytes())
}

func (s *DevServer) recordDevTrace(traceID uuid.UUID, model, provider, path string, start time.Time, status string, scan *security.ScanResult, reqBody, respBody []byte) {
	dur := uint32(time.Since(start).Milliseconds())
	var threatDetected bool
	var threatTypes []string
	var threatScore float32
	var action string

	if scan != nil {
		threatDetected = len(scan.Findings) > 0
		threatScore = float32(scan.ThreatScore)
		action = string(scan.Action)
		for _, t := range scan.ThreatTypes {
			threatTypes = append(threatTypes, string(t))
		}
	}

	tr := &observability.TraceRecord{
		ID:             traceID,
		CustomerID:     tenant.DefaultOSSID,
		Method:         "POST",
		Path:           path,
		Provider:       provider,
		Model:          model,
		StartedAt:      start,
		CompletedAt:    time.Now(),
		DurationMs:     dur,
		Status:         status,
		ThreatDetected: threatDetected,
		ThreatTypes:    threatTypes,
		ThreatScore:    threatScore,
		SecurityAction: action,
		RequestBody:    string(reqBody),
		ResponseBody:   string(respBody),
		Environment:    "dev",
	}

	s.traces.RecordTrace(tr)

	if scan != nil {
		for _, f := range scan.Findings {
			s.traces.RecordThreatEvent(&observability.ThreatEvent{
				ID:             uuid.New(),
				TraceID:        traceID,
				CustomerID:     tenant.DefaultOSSID,
				ThreatType:     string(f.ThreatType),
				Severity:       string(f.Severity),
				Score:          float32(f.Score),
				Action:         string(f.Action),
				DetectorName:   f.DetectorName,
				MatchedPattern: f.MatchedPattern,
				MatchedContent: f.MatchedContent,
				Confidence:     float32(f.Confidence),
				DetectedAt:     time.Now().UTC(),
			})
		}
	}
}

func inferDevProvider(model string) providers.Provider {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(m, "gpt") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4") || strings.HasPrefix(m, "text-embedding") || strings.HasPrefix(m, "openai/"):
		return providers.ProviderOpenAI
	case strings.HasPrefix(m, "claude") || strings.HasPrefix(m, "anthropic/"):
		return providers.ProviderAnthropic
	case strings.HasPrefix(m, "gemini") || strings.HasPrefix(m, "google/") || strings.HasPrefix(m, "vertex/"):
		return providers.ProviderGemini
	case strings.HasPrefix(m, "deepseek") || strings.HasPrefix(m, "deepseek/"):
		return providers.ProviderDeepSeek
	case strings.HasPrefix(m, "groq/") || strings.Contains(m, "groq"):
		return providers.ProviderGroq
	case strings.HasPrefix(m, "ollama/") || strings.HasPrefix(m, "local/"):
		return providers.ProviderOllama
	case strings.HasPrefix(m, "bedrock/") || strings.HasPrefix(m, "amazon."):
		return providers.ProviderBedrock
	default:
		return providers.ProviderOpenAI
	}
}

func resolveDevAPIKey(r *http.Request, provider providers.Provider) string {
	// First check Authorization header / x-api-key
	if authH := r.Header.Get("Authorization"); authH != "" {
		token := strings.TrimPrefix(authH, "Bearer ")
		token = strings.TrimSpace(token)
		if token != "" && !strings.HasPrefix(token, "bastio-") {
			return token
		}
	}
	if apiKeyH := r.Header.Get("x-api-key"); apiKeyH != "" {
		return apiKeyH
	}

	// Check environment variables
	switch provider {
	case providers.ProviderOpenAI:
		return os.Getenv("OPENAI_API_KEY")
	case providers.ProviderAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY")
	case providers.ProviderGemini:
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			key = os.Getenv("GOOGLE_API_KEY")
		}
		return key
	case providers.ProviderDeepSeek:
		return os.Getenv("DEEPSEEK_API_KEY")
	case providers.ProviderGroq:
		return os.Getenv("GROQ_API_KEY")
	}
	return ""
}

func extractUserContentFromRaw(messages []json.RawMessage) string {
	var sb strings.Builder
	for _, raw := range messages {
		var msg struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.Role != "user" {
			continue
		}
		var str string
		if err := json.Unmarshal(msg.Content, &str); err == nil {
			sb.WriteString(str)
			sb.WriteString("\n")
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Content, &parts); err == nil {
			for _, p := range parts {
				if p.Type == "text" && p.Text != "" {
					sb.WriteString(p.Text)
					sb.WriteString("\n")
				}
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"message": message,
		},
	})
}

func sanitizeLog(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\r", "")
}
