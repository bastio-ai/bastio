package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/bastio-ai/bastio/pkg/cache"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
	"github.com/bastio-ai/bastio/pkg/database"
)

// Handler exposes the /governance/* HTTP API consumed by the Bastio
// Governance browser extension. Mount it on the OSS server under /v1.
type Handler struct {
	installs    *InstallStore
	events      *EventStore
	policies    *PolicyStore
	webhooks    *WebhookStore
	domains     *DomainStore
	deliverer   *WebhookDeliverer
	riverClient *river.Client[pgx.Tx]
	scim        *SCIMStore
	domainList  []string
}

// SetSCIMStore wires the SCIM identity store. When set, governance event
// rendering and pilot-report breakdowns can join external_user_id values
// against SCIM-pushed users to surface group/department metadata.
func (h *Handler) SetSCIMStore(s *SCIMStore) {
	h.scim = s
}

// SetRiverClient wires River for durable webhook delivery. When set, webhook
// firing enqueues a `governance.webhook_delivery` job (durable across server
// restarts) instead of spawning an in-process goroutine. The cmd/worker
// process must register `WebhookDeliveryWorker` for jobs to be processed.
//
// Without a River client, webhook delivery falls back to goroutine + retry,
// which is fine for OSS deployments without a separate worker process.
func (h *Handler) SetRiverClient(c *river.Client[pgx.Tx]) {
	h.riverClient = c
}

// NewHandler wires the persistent stores. The default public-AI domain list
// is bundled here for v1; later versions may load it from PG.
func NewHandler(db *database.DB, ch *clickhouse.CH, c *cache.Cache) *Handler {
	whStore := NewWebhookStore(db.Pool)
	return &Handler{
		installs:   NewInstallStore(db.Pool),
		events:     NewEventStore(ch, c),
		policies:   NewPolicyStore(db.Pool),
		webhooks:   whStore,
		domains:    NewDomainStore(db.Pool),
		deliverer:  NewWebhookDeliverer(whStore),
		domainList: defaultDomainList(),
	}
}

// Routes returns a Chi subrouter with the governance endpoints.
//
//	POST /events       — telemetry ingestion (HMAC-authenticated, idempotent)
//	POST /heartbeat    — extension liveness ping (HMAC-authenticated)
//	POST /classify     — async ML classifier hook (HMAC-authenticated; OSS fallback)
//	GET  /domain-list  — public list of tracked AI tools (unauthenticated)
//	GET  /policy       — current org policy (HMAC-authenticated)
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/domain-list", h.domainListHandler)
	r.Post("/events", h.handleSigned(h.eventsHandler))
	r.Post("/heartbeat", h.handleSigned(h.heartbeatHandler))
	r.Post("/classify", h.handleSigned(h.classifyHandler))
	r.Get("/policy", h.handleSignedReadOnly(h.policyHandler))
	return r
}

// signedHandler is invoked after the HMAC has been verified. The Installation
// row is supplied so handlers can use customer_id without re-querying.
type signedHandler func(w http.ResponseWriter, r *http.Request, body []byte, inst *Installation, hdr AuthHeader)

// handleSigned reads + buffers the body, verifies HMAC, then dispatches.
// Body is at most 64KB to keep memory bounded; replay protection happens
// inside VerifyHMAC.
func (h *Handler) handleSigned(next signedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r, 64*1024)
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		hdrVal := r.Header.Get(authHeaderName)
		parsed, err := parseAuthHeader(hdrVal)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid auth header")
			return
		}
		inst, err := h.installs.LookupByOrg(r.Context(), parsed.OrgID)
		if err != nil {
			if errors.Is(err, ErrInstallationNotFound) {
				writeError(w, http.StatusUnauthorized, "unknown organization")
				return
			}
			slog.Error("install lookup", "error", err)
			writeError(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		// Re-verify with the resolved secret.
		// (parseAuthHeader already validated structure; VerifyHMAC checks ts + signature.)
		r2 := r.Clone(r.Context())
		r2.Body = io.NopCloser(bytes.NewReader(body))
		fullHdr, err := VerifyHMAC(r2, body, inst.InstallationSecret)
		if err != nil {
			slog.Warn("hmac verify failed", "org", parsed.OrgID, "err", err)
			writeError(w, http.StatusUnauthorized, "signature verification failed")
			return
		}
		next(w, r, body, inst, fullHdr)
	}
}

// handleSignedReadOnly is the same flow but for GET endpoints — body is empty.
func (h *Handler) handleSignedReadOnly(next signedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hdrVal := r.Header.Get(authHeaderName)
		parsed, err := parseAuthHeader(hdrVal)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid auth header")
			return
		}
		inst, err := h.installs.LookupByOrg(r.Context(), parsed.OrgID)
		if err != nil {
			if errors.Is(err, ErrInstallationNotFound) {
				writeError(w, http.StatusUnauthorized, "unknown organization")
				return
			}
			slog.Error("install lookup", "error", err)
			writeError(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		fullHdr, err := VerifyHMAC(r, nil, inst.InstallationSecret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "signature verification failed")
			return
		}
		next(w, r, nil, inst, fullHdr)
	}
}

// eventsHandler ingests one governance event. Returns 200 OK with
// {duplicate: true} if the event_id was already seen — never 409.
func (h *Handler) eventsHandler(w http.ResponseWriter, r *http.Request, body []byte, inst *Installation, _ AuthHeader) {
	var ev EventPayload
	if err := json.Unmarshal(body, &ev); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if ev.EventID == "" || ev.OccurredAt == "" {
		writeError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	dup, err := h.events.AlreadySeen(r.Context(), ev.EventID)
	if err != nil {
		slog.Error("dedup check", "error", err)
	}
	if dup {
		writeJSON(w, http.StatusOK, map[string]any{"duplicate": true})
		return
	}

	if err := h.events.WriteEvent(r.Context(), inst.CustomerID, ev); err != nil {
		slog.Error("write event", "error", err)
		writeError(w, http.StatusInternalServerError, "ingest failed")
		return
	}

	// Fire webhooks asynchronously. With River wired (cmd/worker running),
	// each delivery becomes a durable job that survives server restarts and
	// gets retried automatically. Without River, the deliverer falls back
	// to in-process goroutines.
	if h.deliverer != nil {
		hooks, _ := h.webhooks.List(r.Context(), inst.CustomerID)
		for _, hook := range hooks {
			if !triggerMatches(hook.Trigger, ev) {
				continue
			}
			EnqueueWebhook(r.Context(), h.riverClient, h.deliverer, hook, ev)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"accepted": true})
}

// heartbeatHandler updates last-seen for a given install_id.
func (h *Handler) heartbeatHandler(w http.ResponseWriter, r *http.Request, body []byte, inst *Installation, _ AuthHeader) {
	var hb HeartbeatPayload
	if err := json.Unmarshal(body, &hb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.events.WriteHeartbeat(r.Context(), inst.CustomerID, inst.OrgID, hb); err != nil {
		slog.Warn("write heartbeat", "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// classifyHandler runs the server-side classifier. When PRESIDIO_URL is
// configured (Cloud and any OSS deploy that opted in), text is analyzed by
// Microsoft Presidio's PII detector. Otherwise the regex+entropy heuristic
// in classifyFallback handles the request — same response shape, weaker
// signal. Either way the call is async from the extension's perspective:
// the local block decision already happened in <30ms; this enriches the
// dashboard event with a higher-quality severity / reasoning.
func (h *Handler) classifyHandler(w http.ResponseWriter, r *http.Request, body []byte, _ *Installation, _ AuthHeader) {
	var req ClassifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp := classifyWithPresidioOrFallback(r.Context(), req)
	writeJSON(w, http.StatusOK, resp)
}

// policyHandler returns the org's current policy. Reads governance_policies
// for the customer; falls back to defaults if the row hasn't been created yet.
// Also returns the merged domain-override list so the extension can refresh
// its watched-tools list without a separate HMAC call.
func (h *Handler) policyHandler(w http.ResponseWriter, r *http.Request, _ []byte, inst *Installation, _ AuthHeader) {
	pol, err := h.policies.Get(r.Context(), inst.CustomerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "policy lookup failed")
		return
	}
	domainOverrides, err := h.domains.List(r.Context(), inst.CustomerID)
	if err != nil {
		slog.Warn("domain overrides list", "err", err)
	}
	overrideDomains := make([]string, 0, len(domainOverrides))
	for _, d := range domainOverrides {
		overrideDomains = append(overrideDomains, d.Domain)
	}

	out := map[string]any{
		"default_policy": map[Severity]string{
			SeverityLow:    pol.SeverityLow,
			SeverityMedium: pol.SeverityMedium,
			SeverityHigh:   pol.SeverityHigh,
		},
		"custom_keywords":     pol.CustomKeywords,
		"custom_regex_packs":  pol.CustomRegexPacks,
		"override_enabled":    pol.OverrideEnabled,
		"pseudonymize_pii":    pol.PseudonymizePII,
		"domain_overrides":    overrideDomains,
		"redirect_target":     pol.RedirectTarget,
	}
	writeJSON(w, http.StatusOK, out)
}

// domainListHandler returns the public AI-tool allowlist. Unauthenticated.
func (h *Handler) domainListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=21600") // 6h
	writeJSON(w, http.StatusOK, DomainList{
		Domains: h.domainList,
		Etag:    "v1",
		Updated: time.Now().UTC(),
	})
}

// classifyFallback is the deterministic regex+entropy classifier used by OSS
// and the warm-up path before a trained model is wired in. Cloud overrides
// this route via WithAPIExtension once the proprietary classifier ships.
//
// Strategy: combine the local hit list with shape signals from the excerpt
// itself to promote/demote ambiguous cases. The extension calls this off the
// critical path — its 30ms local block decision already happened. We are
// *enriching*, not gating.
func classifyFallback(req ClassifyRequest) ClassifyResponse {
	piiCount, secretCount, codeCount := 0, 0, 0
	highSignal := false
	for _, hit := range req.Layer3Hits {
		switch {
		case strings.HasPrefix(hit, "secret."):
			secretCount++
			highSignal = true
		case hit == "pii.ssn" || hit == "pii.card" || hit == "pii.iban":
			piiCount++
			highSignal = true
		case strings.HasPrefix(hit, "pii."):
			piiCount++
		case strings.HasPrefix(hit, "code."):
			codeCount++
		}
	}

	// Multi-PII cluster is a strong signal even without secrets.
	if piiCount >= 3 {
		highSignal = true
	}
	// Secret + code signal almost always means an exfil attempt.
	codeWithSecret := secretCount > 0 && codeCount > 0

	// Excerpt shape: long high-entropy passages tend to be data dumps.
	excerptEntropy := shannonEntropyServer(req.TextExcerpt)
	longHighEntropy := len(req.TextExcerpt) > 500 && excerptEntropy >= 4.4

	switch {
	case highSignal || codeWithSecret:
		return ClassifyResponse{
			Severity:   SeverityHigh,
			Confidence: 0.92,
			Reasoning:  "high-severity local rule or secret+code combination",
		}
	case piiCount >= 2:
		return ClassifyResponse{
			Severity:   SeverityHigh,
			Confidence: 0.78,
			Reasoning:  "multiple PII types co-occurring",
		}
	case piiCount == 1 || longHighEntropy:
		return ClassifyResponse{
			Severity:   SeverityMedium,
			Confidence: 0.68,
			Reasoning:  "single PII hit or high-entropy data-dump shape",
		}
	default:
		return ClassifyResponse{
			Severity:   SeverityLow,
			Confidence: 0.45,
			Reasoning:  "weak signals; consider promoting if customer keywords match",
		}
	}
}

func shannonEntropyServer(s string) float64 {
	if s == "" {
		return 0
	}
	counts := map[rune]int{}
	n := 0
	for _, ch := range s {
		counts[ch]++
		n++
	}
	if n == 0 {
		return 0
	}
	var h float64
	for _, c := range counts {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

func readBody(r *http.Request, max int64) ([]byte, error) {
	limited := io.LimitReader(r.Body, max+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > max {
		return nil, errors.New("body too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	return buf, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// allow context type to compile; not currently used
var _ = context.Background
