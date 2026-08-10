package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/internal/security/overlay"
	"github.com/bastio-ai/bastio/pkg/cache"
	"github.com/bastio-ai/bastio/pkg/tenant"
)

type FiredThreat struct {
	DetectorName   string
	Severity       string
	Score          float32
	MatchedPattern string
	MatchedContent string
	Message        string
}

// TraceEvent represents a completed detection event passed to observability.
type TraceEvent struct {
	ID             uuid.UUID
	CustomerID     uuid.UUID
	ProxyID        uuid.UUID
	Method         string
	Path           string
	Provider       string
	Model          string
	StartedAt      time.Time
	CompletedAt    time.Time
	DurationMs     uint32
	Status         string
	HTTPStatus     uint16
	ThreatDetected bool
	ThreatTypes    []string
	ThreatScore    float32
	SecurityAction string
	RequestBody    string
	ResponseBody   string
	TraceName      string
	FiredThreats   []FiredThreat
}

// DetectHandler exposes POST /v1/detect, the endpoint the TypeScript
// SDKs (Mastra processor, Vercel AI middleware) and the dashboard
// playground use to run a profile's step list against ad-hoc messages.
// Unlike the gateway proxy it doesn't touch an upstream LLM — just
// returns what each step decided.
type DetectHandler struct {
	engine        *Engine
	lookup        ProfileLookup
	db            *pgxpool.Pool
	tenant        func(ctx context.Context) uuid.UUID
	overlayLoader *overlay.Loader
	overlayStore  *overlay.Store
	shadowDedup   *overlay.ShadowEventDeduper
	onTrace       func(ev TraceEvent)
	cache         *cache.Cache
}

// SetCache injects the Redis cache instance for fast response caching.
func (h *DetectHandler) SetCache(c *cache.Cache) {
	h.cache = c
}

// SetTraceListener installs a listener called when a detection trace completes.
func (h *DetectHandler) SetTraceListener(fn func(ev TraceEvent)) {
	h.onTrace = fn
}

// NewDetectHandler wires the engine and profile lookup the handler uses
// for every request. The tenant resolver defaults to the OSS single-tenant
// constant; cloud replaces it via SetTenantResolver.
func NewDetectHandler(engine *Engine, lookup ProfileLookup, db *pgxpool.Pool) *DetectHandler {
	return &DetectHandler{
		engine: engine,
		lookup: lookup,
		db:     db,
		// Read tenant from request context (OSSMiddleware default in
		// single-tenant; cloud auth override in multi-tenant). Falls
		// back to DefaultOSSID only when no middleware ran.
		tenant: func(ctx context.Context) uuid.UUID {
			if id, err := tenant.FromContext(ctx); err == nil {
				return id
			}
			return tenant.DefaultOSSID
		},
	}
}

// SetOverlayLoader installs the tenant-policy-overlay loader. When set,
// every detect request loads the active overlay for the calling tenant
// (and the proxy id on the request, when present) and applies it on top
// of the base profile. A nil loader disables overlay integration
// entirely — the handler behaves exactly as before.
func (h *DetectHandler) SetOverlayLoader(l *overlay.Loader) {
	h.overlayLoader = l
}

// SetOverlayStore installs the store used by the async shadow-mode
// runner to persist divergence events. Shadow mode is a no-op unless
// both a loader and a store are configured. Nil is permitted.
//
// A default ShadowEventDeduper is installed alongside the store so
// high-volume tenants don't flood the shadow-events table with the
// same divergence repeated thousands of times. Callers wanting a
// different window can use SetShadowDeduper to override.
func (h *DetectHandler) SetOverlayStore(s *overlay.Store) {
	h.overlayStore = s
	if h.shadowDedup == nil {
		h.shadowDedup = overlay.NewShadowEventDeduper(overlay.DefaultShadowDedupWindow)
	}
}

// SetShadowDeduper overrides the default deduper. Useful for tests
// that want no dedup (pass a deduper with window 0 or nil) and for
// deployments that want to tune the coalescing window.
func (h *DetectHandler) SetShadowDeduper(d *overlay.ShadowEventDeduper) {
	h.shadowDedup = d
}

// SetTenantResolver swaps the function used to resolve the calling tenant.
// Cloud injects a session/context-aware resolver here.
func (h *DetectHandler) SetTenantResolver(fn func(context.Context) uuid.UUID) {
	h.tenant = fn
}

// Routes returns the handler's Chi router.
func (h *DetectHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Detect)
	return r
}

// detectMessage is one turn in the input the handler scans. Role is
// informational — the engine runs the same detectors regardless — but
// it's preserved so the response can echo the original shape.
type detectMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// detectRequest is the JSON body shape. `profile` selects a named
// security profile; empty means "default". `direction` picks the input
// vs output step list; empty defaults to "input".
//
// `source` is an optional tag: when set to "playground", the handler
// persists a row into playground_runs after responding so the dashboard
// history panel can list it. SDK callers omit this, keeping their
// production traffic out of the history table.
type detectRequest struct {
	Messages    []detectMessage `json:"messages"`
	Profile     string          `json:"profile,omitempty"`
	Direction   Direction       `json:"direction,omitempty"`
	InlineSteps []Step          `json:"steps,omitempty"` // override; the playground uses this
	Source      string          `json:"source,omitempty"`
	ProxyID     string          `json:"proxy_id,omitempty"`
	BypassCache bool            `json:"bypass_cache,omitempty"`
}

// detectMessageResult reports what happened to a single message.
type detectMessageResult struct {
	Role             string        `json:"role"`
	Original         string        `json:"original"`
	SanitizedContent string        `json:"sanitized_content"`
	Action           Action        `json:"action"`
	ShouldBlock      bool          `json:"should_block"`
	Steps            []StepResult  `json:"steps"`
}

// detectResponse is the envelope; the SDK maps this onto Mastra/Vercel
// processor return shapes.
type detectResponse struct {
	Profile     string                 `json:"profile"`
	Direction   Direction              `json:"direction"`
	Action      Action                 `json:"action"`
	ShouldBlock bool                   `json:"should_block"`
	Messages    []detectMessageResult  `json:"messages"`
}

// Detect runs the selected step list against each message. Steps run
// sequentially per message but messages are independent; the aggregate
// action is the strongest across all messages.
func (h *DetectHandler) Detect(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	var req detectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		http.Error(w, `{"error":"messages is required"}`, http.StatusBadRequest)
		return
	}
	if req.Direction == "" {
		req.Direction = DirectionInput
	}
	if req.Direction != DirectionInput && req.Direction != DirectionOutput {
		http.Error(w, `{"error":"direction must be input or output"}`, http.StatusBadRequest)
		return
	}

	steps := req.InlineSteps
	profileName := req.Profile
	if profileName == "" {
		profileName = "default"
	}
	// Default to canonicalization on for inline-step callers (the
	// playground's "try a step list" path) — the profile's flag
	// overrides this when we resolve one.
	canonicalize := true

	// proxyID is needed by both the active and shadow overlay lookups.
	proxyID := uuid.Nil
	if req.ProxyID != "" {
		if id, perr := uuid.Parse(req.ProxyID); perr == nil {
			proxyID = id
		}
	}

	bypassHeader := strings.EqualFold(r.Header.Get("Cache-Control"), "no-cache") ||
		strings.EqualFold(r.Header.Get("X-Bastio-Bypass-Cache"), "true") ||
		r.Header.Get("X-Bastio-Bypass-Cache") == "1" ||
		strings.EqualFold(r.Header.Get("X-Cache-Bypass"), "true") ||
		r.Header.Get("X-Cache-Bypass") == "1"
	bypass := req.BypassCache || bypassHeader

	var cacheKey string
	if h.cache != nil && !bypass && req.Source != "playground" && len(req.InlineSteps) == 0 && !h.cache.ShouldBypass(r.Context(), "bastio-detect-v1", r.URL.Path) {
		reqBytes, _ := json.Marshal(req)
		hasher := sha256.New()
		hasher.Write([]byte(h.tenant(r.Context()).String()))
		hasher.Write([]byte(":"))
		hasher.Write(reqBytes)
		cacheKey = "devapi:detect:" + hex.EncodeToString(hasher.Sum(nil))

		var cachedResp detectResponse
		if found, _ := h.cache.Get(r.Context(), cacheKey, &cachedResp); found {
			_, _ = h.cache.Incr(r.Context(), "cache:hits")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Bastio-Cache", "HIT")
			_ = json.NewEncoder(w).Encode(cachedResp)

			if h.onTrace != nil {
				respBytes, _ := json.Marshal(cachedResp)
				h.onTrace(TraceEvent{
					ID:             uuid.New(),
					CustomerID:     h.tenant(r.Context()),
					ProxyID:        proxyID,
					Method:         r.Method,
					Path:           r.URL.Path,
					Provider:       "developer-api",
					Model:          "bastio-detect-v1",
					StartedAt:      startTime,
					CompletedAt:    time.Now(),
					DurationMs:     uint32(time.Since(startTime).Milliseconds()),
					Status:         "ok",
					HTTPStatus:     200,
					ThreatDetected: cachedResp.ShouldBlock || cachedResp.Action == ActionBlock,
					SecurityAction: string(cachedResp.Action),
					RequestBody:    string(reqBytes),
					ResponseBody:   string(respBytes),
					TraceName:      "POST /v1/detect (cache hit)",
				})
			}
			return
		}
	}

	// Captured for the async shadow runner. Only populated on the
	// profile-resolved path (inline-step playground callers don't have
	// a tenant policy to shadow against).
	var baseProfile Profile
	var haveBaseProfile bool
	var activeIdent overlay.Identity

	if len(steps) == 0 {
		p, err := h.resolveProfile(r.Context(), profileName)
		if err != nil {
			slog.Error("detect: resolve profile", "profile", profileName, "error", err)
			http.Error(w, `{"error":"profile lookup failed"}`, http.StatusInternalServerError)
			return
		}
		baseProfile = p
		haveBaseProfile = true
		// Apply the active tenant policy overlay (if any) on top of the
		// base profile. Loader returns (nil, zero Identity, nil) when no
		// overlay exists — the common case. A DB/Redis error is logged
		// and we fall through to the base profile (fail open — never
		// block detection on overlay infrastructure).
		if h.overlayLoader != nil {
			snap, ident, oerr := h.overlayLoader.LoadActive(r.Context(), h.tenant(r.Context()), proxyID)
			if oerr != nil {
				slog.Warn("detect: overlay load failed", "error", oerr)
			} else if snap != nil {
				merged := ApplyOverlay(&p, snap, ident)
				p = *merged
				activeIdent = ident
				// Attach the snapshot + identity to the request context
				// so runtime detectors (topic, plugins) can honour the
				// overlay's additions.
				r = r.WithContext(overlay.WithActive(r.Context(), snap, ident))
			}
		}
		if req.Direction == DirectionOutput {
			steps = p.Output
		} else {
			steps = p.Input
		}
		canonicalize = p.CanonicalizeEnabled
	} else if err := ValidateSteps(steps); err != nil {
		http.Error(w, jsonErr(err.Error()), http.StatusBadRequest)
		return
	}

	resp := detectResponse{
		Profile:   profileName,
		Direction: req.Direction,
		Action:    ActionPass,
	}

	for _, m := range req.Messages {
		stepsRes := h.engine.RunSteps(r.Context(), m.Content, steps, &RunOptions{
			Canonicalize: canonicalize,
			Role:         m.Role,
		})
		msgRes := detectMessageResult{
			Role:             m.Role,
			Original:         m.Content,
			SanitizedContent: stepsRes.SanitizedContent,
			Action:           stepsRes.Action,
			ShouldBlock:      stepsRes.ShouldBlock,
			Steps:            stepsRes.Steps,
		}

		// Overlay plugin detectors run after the core engine. Their
		// findings upgrade the message verdict when any carries
		// ActionBlock. Plugin failures are logged inside
		// RunOverlayPlugins — never propagate here.
		if pluginFindings := RunOverlayPlugins(r.Context(), m.Content); len(pluginFindings) > 0 {
			if BlockActionFromFindings(pluginFindings) {
				msgRes.ShouldBlock = true
				if rank(ActionBlock) > rank(msgRes.Action) {
					msgRes.Action = ActionBlock
				}
			}
		}

		resp.Messages = append(resp.Messages, msgRes)
		if rank(msgRes.Action) > rank(resp.Action) {
			resp.Action = msgRes.Action
		}
		if msgRes.ShouldBlock {
			resp.ShouldBlock = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if bypass {
		w.Header().Set("X-Bastio-Cache", "BYPASS")
	} else {
		w.Header().Set("X-Bastio-Cache", "MISS")
	}
	_ = json.NewEncoder(w).Encode(resp)

	if h.cache != nil && cacheKey != "" && !bypass {
		_, _ = h.cache.Incr(r.Context(), "cache:misses")
		_ = h.cache.Set(r.Context(), cacheKey, resp, 1*time.Hour)
	}

	if h.onTrace != nil {
		threatDetected := resp.ShouldBlock || resp.Action == ActionBlock
		var threatTypes []string
		var firedThreats []FiredThreat
		var maxScore float32
		for _, msg := range resp.Messages {
			for _, step := range msg.Steps {
				if step.Fired {
					threatTypes = append(threatTypes, step.Detector)
					if float32(step.Score) > maxScore {
						maxScore = float32(step.Score)
					}
					sev := "high"
					if step.Score >= 0.8 {
						sev = "critical"
					} else if step.Score < 0.5 {
						sev = "medium"
					}
					matchedPattern := "override"
					matchedContent := msg.Original
					if len(step.Findings) > 0 {
						matchedPattern = step.Findings[0].MatchedPattern
						matchedContent = step.Findings[0].MatchedContent
					}
					firedThreats = append(firedThreats, FiredThreat{
						DetectorName:   step.Detector,
						Severity:       sev,
						Score:          float32(step.Score),
						MatchedPattern: matchedPattern,
						MatchedContent: matchedContent,
						Message:        fmt.Sprintf("%s detector fired", step.Detector),
					})
				}
			}
		}

		status := "ok"
		if resp.ShouldBlock || resp.Action == ActionBlock {
			status = "blocked"
		}

		reqBytes, _ := json.Marshal(req)
		respBytes, _ := json.Marshal(resp)

		h.onTrace(TraceEvent{
			ID:             uuid.New(),
			CustomerID:     h.tenant(r.Context()),
			ProxyID:        proxyID,
			Method:         r.Method,
			Path:           r.URL.Path,
			Provider:       "developer-api",
			Model:          "bastio-detect-v1",
			StartedAt:      startTime,
			CompletedAt:    time.Now(),
			DurationMs:     uint32(time.Since(startTime).Milliseconds()),
			Status:         status,
			HTTPStatus:     200,
			ThreatDetected: threatDetected,
			ThreatTypes:    threatTypes,
			ThreatScore:    maxScore,
			SecurityAction: string(resp.Action),
			RequestBody:    string(reqBytes),
			ResponseBody:   string(respBytes),
			TraceName:      "POST /v1/detect",
			FiredThreats:   firedThreats,
		})
	}

	// Playground runs are persisted for the dashboard history panel
	// after the response is written, so the client never waits on DB
	// latency. Best-effort: a failed insert is logged but doesn't
	// affect the detection result the caller already received.
	if req.Source == "playground" && h.db != nil {
		// Use a background context so a caller disconnect doesn't kill
		// the insert; the detection work is already done.
		go h.persistPlaygroundRun(context.Background(), r.Context(), req, resp)
	}

	// Shadow mode: if a shadow overlay is configured for this tenant,
	// re-run detection with the shadow effective profile and record any
	// divergence. Runs entirely after the response is written so the
	// client's request latency is unaffected. A panic or error in the
	// shadow path never reaches the caller — it's logged and dropped.
	if haveBaseProfile && h.overlayLoader != nil && h.overlayStore != nil {
		customerID := h.tenant(r.Context())
		primaryResults := append([]detectMessageResult(nil), resp.Messages...)
		msgs := append([]detectMessage(nil), req.Messages...)
		go h.runShadow(customerID, proxyID, baseProfile, activeIdent, msgs, primaryResults, req.Direction, canonicalize)
	}
}

// runShadow is the async body of the shadow-mode runner. It loads the
// shadow overlay (if any), applies it on top of the base profile, and
// re-runs detection for each message. Divergences from the primary
// result are recorded as tenant_policy_overlay_shadow_events rows.
//
// Invariants:
//   - Never mutates user-facing state.
//   - Never panics out of the goroutine — recovered and logged.
//   - Uses a background context with a short timeout so client
//     cancellation or slow DBs can't leak goroutines.
func (h *DetectHandler) runShadow(
	customerID, proxyID uuid.UUID,
	base Profile,
	activeIdent overlay.Identity,
	inputs []detectMessage,
	primary []detectMessageResult,
	direction Direction,
	canonicalize bool,
) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("overlay shadow: panic in runner", "panic", fmt.Sprint(rec))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snap, shadowIdent, err := h.overlayLoader.LoadShadow(ctx, customerID, proxyID)
	if err != nil {
		slog.Warn("overlay shadow: load failed", "error", err)
		return
	}
	if snap == nil {
		return
	}

	shadowProfile := ApplyOverlay(&base, snap, shadowIdent)
	if shadowProfile == nil {
		return
	}

	var steps []Step
	if direction == DirectionOutput {
		steps = shadowProfile.Output
	} else {
		steps = shadowProfile.Input
	}

	var activeVersionPtr *uuid.UUID
	if activeIdent.VersionID != uuid.Nil {
		id := activeIdent.VersionID
		activeVersionPtr = &id
	}

	for i, m := range inputs {
		if i >= len(primary) {
			break
		}
		shadowRes := h.engine.RunSteps(ctx, m.Content, steps, &RunOptions{
			Canonicalize: canonicalize,
			Role:         m.Role,
		})
		div, ok := overlay.Classify(
			string(primary[i].Action), string(shadowRes.Action),
			primary[i].ShouldBlock, shadowRes.ShouldBlock,
		)
		if !ok {
			continue
		}
		// Dedup identical divergences within the configured window so a
		// noisy shadow version can't flood the events table. Skipped
		// entirely when no deduper is configured (nil-safe).
		if !h.shadowDedup.ShouldRecord(shadowIdent.VersionID, div) {
			continue
		}
		detail := shadowDetail(primary[i], shadowRes)
		event := &overlay.ShadowEvent{
			CustomerID:      customerID,
			OverlayID:       shadowIdent.OverlayID,
			ShadowVersionID: shadowIdent.VersionID,
			ActiveVersionID: activeVersionPtr,
			Divergence:      div,
			ActiveAction:    string(primary[i].Action),
			ShadowAction:    string(shadowRes.Action),
			Detail:          detail,
		}
		if err := h.overlayStore.InsertShadowEvent(ctx, event); err != nil {
			slog.Warn("overlay shadow: insert event failed", "error", err)
		}
	}
}

// shadowDetail builds a small JSON payload summarizing what differed
// between the primary and shadow runs for a single message. Enough
// context for an operator to understand "why" without storing the full
// step list twice.
func shadowDetail(primary detectMessageResult, shadowRes *StepsResult) json.RawMessage {
	payload := map[string]any{
		"primary_fired":        firedDetectors(primary.Steps),
		"shadow_fired":         firedDetectors(shadowRes.Steps),
		"primary_should_block": primary.ShouldBlock,
		"shadow_should_block":  shadowRes.ShouldBlock,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// persistPlaygroundRun records one detect invocation into
// playground_runs. firstMessage is used because the playground always
// sends a single message per run; if the handler ever accepts multi-
// message playground runs, collapse them here.
func (h *DetectHandler) persistPlaygroundRun(
	bgCtx context.Context,
	tenantCtx context.Context,
	req detectRequest,
	resp detectResponse,
) {
	if len(resp.Messages) == 0 {
		return
	}
	msg := resp.Messages[0]

	customerID := h.tenant(tenantCtx)

	var proxyID any
	if req.ProxyID != "" {
		proxyID = req.ProxyID
	}

	fired := firedDetectors(msg.Steps)

	stepsJSON, err := json.Marshal(msg.Steps)
	if err != nil {
		slog.Warn("playground run: marshal steps", "error", err)
		return
	}

	_, err = h.db.Exec(bgCtx, `
		INSERT INTO playground_runs (
			customer_id, profile_name, proxy_id, direction,
			prompt, sanitized_content,
			action, should_block,
			fired_detectors, steps, duration_ns
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		customerID, resp.Profile, proxyID, string(resp.Direction),
		msg.Original, msg.SanitizedContent,
		string(msg.Action), msg.ShouldBlock,
		fired, stepsJSON, stepsTotalDuration(msg.Steps).Nanoseconds(),
	)
	if err != nil {
		slog.Warn("playground run: insert", "error", err)
	}
}

// firedDetectors extracts the unique detector names that actually
// fired. Used to denormalize the list so the history panel can filter
// without scanning the steps JSONB.
func firedDetectors(steps []StepResult) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		if !s.Fired || s.Skipped {
			continue
		}
		if seen[s.Detector] {
			continue
		}
		seen[s.Detector] = true
		out = append(out, s.Detector)
	}
	return out
}

// stepsTotalDuration sums per-step durations. The engine already
// produces per-step timings; aggregating client-side keeps the handler
// honest about what the run actually cost.
func stepsTotalDuration(steps []StepResult) time.Duration {
	var total time.Duration
	for _, s := range steps {
		total += s.Duration
	}
	return total
}

// resolveProfile falls back to the code default when a named profile
// doesn't exist, so SDK users aren't blocked by first-run empty DBs.
func (h *DetectHandler) resolveProfile(ctx context.Context, name string) (Profile, error) {
	if h.db == nil {
		return DefaultProfile(), nil
	}
	customerID := h.tenant(ctx)

	// Named lookup; default-name takes the ProfileLookup fast path.
	if name == "default" && h.lookup != nil {
		p, err := h.lookup.GetDefault(ctx, customerID)
		if err != nil {
			return Profile{}, err
		}
		if p == nil {
			return DefaultProfile(), nil
		}
		return *p, nil
	}

	var (
		canonicalizeEnabled       bool
		injectionEnabled          bool
		injectionThreshold        float32
		jailbreakEnabled          bool
		jailbreakThreshold        float32
		piiEnabled                bool
		piiAction                 string
		piiScanResponse           bool
		piiRestoreResponse        bool
		piiTokenStyle             string
		secretsEnabled            bool
		indirectInjectionEnabled  bool
		outputExfilEnabled        bool
		topicPolicyEnabled        bool
		injectionStrategy         string
		jailbreakStrategy         string
		secretsStrategy           string
		indirectInjectionStrategy string
		outputExfilStrategy       string
	)
	err := h.db.QueryRow(ctx, `
		SELECT canonicalize_enabled,
			injection_enabled, injection_threshold,
			jailbreak_enabled, jailbreak_threshold,
			pii_enabled, pii_action, pii_scan_response, pii_restore_response, pii_token_style,
			secrets_enabled, indirect_injection_enabled, output_exfil_enabled, topic_policy_enabled,
			injection_strategy, jailbreak_strategy, secrets_strategy,
			indirect_injection_strategy, output_exfil_strategy
		FROM security_profiles
		WHERE customer_id = $1 AND name = $2
		LIMIT 1
	`, customerID, name).Scan(
		&canonicalizeEnabled,
		&injectionEnabled, &injectionThreshold,
		&jailbreakEnabled, &jailbreakThreshold,
		&piiEnabled, &piiAction, &piiScanResponse, &piiRestoreResponse, &piiTokenStyle,
		&secretsEnabled, &indirectInjectionEnabled, &outputExfilEnabled, &topicPolicyEnabled,
		&injectionStrategy, &jailbreakStrategy, &secretsStrategy,
		&indirectInjectionStrategy, &outputExfilStrategy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DefaultProfile(), nil
		}
		return Profile{}, fmt.Errorf("query named profile: %w", err)
	}

	action := Action(piiAction)
	if action == ActionRedact {
		action = ActionMask
	}
	style := TokenStyle(piiTokenStyle)
	if style != TokenStyleAngle && style != TokenStyleCurly {
		style = TokenStyleAngle
	}
	p := Profile{
		CanonicalizeEnabled:       canonicalizeEnabled,
		InjectionEnabled:          injectionEnabled,
		InjectionThreshold:        float64(injectionThreshold),
		JailbreakEnabled:          jailbreakEnabled,
		JailbreakThreshold:        float64(jailbreakThreshold),
		PIIEnabled:                piiEnabled,
		PIIAction:                 action,
		PIIScanResponse:           piiScanResponse,
		PIIRestoreResponse:        piiRestoreResponse,
		PIITokenStyle:             style,
		SecretsEnabled:            secretsEnabled,
		IndirectInjectionEnabled:  indirectInjectionEnabled,
		OutputExfilEnabled:        outputExfilEnabled,
		TopicPolicyEnabled:        topicPolicyEnabled,
		InjectionStrategy:         normalizeStrategy(injectionStrategy, ActionBlock),
		JailbreakStrategy:         normalizeStrategy(jailbreakStrategy, ActionWarn),
		SecretsStrategy:           normalizeStrategy(secretsStrategy, ActionMask),
		IndirectInjectionStrategy: normalizeStrategy(indirectInjectionStrategy, ActionBlock),
		OutputExfilStrategy:       normalizeStrategy(outputExfilStrategy, ActionBlock),
	}
	p.Input, p.Output = StepsFromLegacyProfile(p)
	return p, nil
}

func jsonErr(msg string) string {
	// minimal escape for the error passthrough — profile names and
	// validator messages are ASCII, but be defensive.
	b, _ := json.Marshal(struct {
		Err string `json:"error"`
	}{Err: strings.TrimSpace(msg)})
	return string(b)
}
