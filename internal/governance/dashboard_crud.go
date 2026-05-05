package governance

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// =====================================================================
// Overrides feed — surfaces all overridden events for IT audit
// =====================================================================

func (h *Handler) dashboardOverrides(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 100)
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := h.events.RecentEvents(r.Context(), customerID, limit, "", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "overrides query failed")
		return
	}
	out := make([]EventRow, 0, len(rows))
	for _, ev := range rows {
		if ev.Action == string(ActionOverridden) {
			out = append(out, ev)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"overrides": out, "count": len(out)})
}

// =====================================================================
// Installations
// =====================================================================

// dashboardMDMBundle returns a zip with all MDM artifacts (Chrome managed
// storage, Intune ADMX/ADML, Jamf .mobileconfig, README). Re-creates the
// install secret on demand from the live row — does NOT regenerate it.
//
// This means: after an install is created, IT can re-download the bundle
// any time (the secret is shown again here because it's the same row).
// If you need a fresh secret, revoke the install and create a new one.
func (h *Handler) dashboardMDMBundle(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	rows, err := h.installs.ListByCustomer(r.Context(), customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup")
		return
	}
	var inst *Installation
	for i := range rows {
		if rows[i].ID == id {
			inst = &rows[i]
			break
		}
	}
	if inst == nil {
		writeError(w, http.StatusNotFound, "installation not found")
		return
	}
	// Note: ListByCustomer returns "<redacted>" for the secret. To regen
	// the bundle we need the actual stored secret. Look it up by org_id.
	full, err := h.installs.LookupByOrg(r.Context(), inst.OrgID.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup secret")
		return
	}
	backendURL := strings.TrimSuffix(extractBackendURL(r), "/")
	zipBytes, err := BuildMDMBundle(full.OrgID, backendURL, "<see-creation-response>", full.InstallationSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bundle build failed")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="bastio-governance-mdm-bundle.zip"`)
	_, _ = w.Write(zipBytes)
}

// extractBackendURL infers the public-facing URL of the bastio server from
// the request. Used as the default backend_url in the MDM bundle template.
// IT can override before deploying.
func extractBackendURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func (h *Handler) dashboardRevokeInstallation(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.installs.Revoke(r.Context(), customerID, id); err != nil {
		if errors.Is(err, ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// =====================================================================
// Policy
// =====================================================================

func (h *Handler) dashboardGetPolicy(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	pol, err := h.policies.Get(r.Context(), customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "policy lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, pol)
}

type updatePolicyReq struct {
	SeverityLow      string             `json:"severity_low"`
	SeverityMedium   string             `json:"severity_medium"`
	SeverityHigh     string             `json:"severity_high"`
	CustomKeywords   []string           `json:"custom_keywords"`
	CustomRegexPacks []RegexPack        `json:"custom_regex_packs"`
	RedirectTarget   *RedirectTargetPG  `json:"redirect_target"`
	OverrideEnabled  bool               `json:"override_enabled"`
	PseudonymizePII  bool               `json:"pseudonymize_pii"`
}

func (h *Handler) dashboardUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	var body updatePolicyReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Validate regex packs compile and aren't catastrophic. RE2 (Go's
	// engine) is linear-time, so ReDoS is structurally impossible — but
	// we still cap pattern length and complexity to be polite.
	for _, pack := range body.CustomRegexPacks {
		if len(pack.Pattern) > 500 {
			writeError(w, http.StatusBadRequest, "regex pattern too long")
			return
		}
		if _, err := regexp.Compile(pack.Pattern); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid regex %q: %v", pack.ID, err))
			return
		}
		if pack.Severity != "low" && pack.Severity != "medium" && pack.Severity != "high" {
			writeError(w, http.StatusBadRequest, "invalid pack severity")
			return
		}
	}

	p := &CustomerPolicy{
		CustomerID:       customerID,
		SeverityLow:      body.SeverityLow,
		SeverityMedium:   body.SeverityMedium,
		SeverityHigh:     body.SeverityHigh,
		CustomKeywords:   body.CustomKeywords,
		CustomRegexPacks: body.CustomRegexPacks,
		RedirectTarget:   body.RedirectTarget,
		OverrideEnabled:  body.OverrideEnabled,
		PseudonymizePII:  body.PseudonymizePII,
	}
	if err := h.policies.Upsert(r.Context(), p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// =====================================================================
// Webhooks
// =====================================================================

func (h *Handler) dashboardListWebhooks(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	hooks, err := h.webhooks.List(r.Context(), customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "webhooks list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": hooks})
}

type createWebhookReq struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Format  string `json:"format"`
	Trigger string `json:"trigger"`
}

func (h *Handler) dashboardCreateWebhook(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	var body createWebhookReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !strings.HasPrefix(body.URL, "https://") {
		writeError(w, http.StatusBadRequest, "url must be https")
		return
	}
	if body.Name == "" {
		body.Name = "default"
	}
	hook, err := h.webhooks.Create(r.Context(), customerID, body.Name, body.URL,
		WebhookFormat(body.Format), WebhookTrigger(body.Trigger))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, hook)
}

func (h *Handler) dashboardDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.webhooks.Delete(r.Context(), customerID, id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// dashboardTestWebhook fires a synthetic high-severity event at the webhook
// so IT can validate the URL before an actual incident.
func (h *Handler) dashboardTestWebhook(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	hook, err := h.webhooks.LookupByID(r.Context(), id)
	if err != nil || hook.CustomerID != customerID {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	synthetic := EventPayload{
		EventID:              "test-" + uuid.NewString(),
		UserID:               "test-user",
		OccurredAt:           time.Now().UTC().Format(time.RFC3339),
		SourceDomain:         "chatgpt.com",
		RuleIDs:              []string{"secret.test"},
		Severity:             SeverityHigh,
		Action:               ActionBlocked,
		CharCountIntercepted: 1234,
		Browser:              "chrome",
		BrowserVersion:       "129.0",
		ExtensionVersion:     "0.1.0",
	}
	go h.deliverer.deliverWithRetry(*hook, synthetic)
	writeJSON(w, http.StatusOK, map[string]any{"queued": true})
}

// =====================================================================
// Domain overrides
// =====================================================================

func (h *Handler) dashboardListDomains(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	rows, err := h.domains.List(r.Context(), customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "domain list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"overrides": rows})
}

type addDomainReq struct {
	Domain string `json:"domain"`
	Label  string `json:"label"`
}

func (h *Handler) dashboardAddDomain(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	var body addDomainReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Domain = strings.ToLower(strings.TrimSpace(body.Domain))
	if body.Domain == "" || strings.Contains(body.Domain, " ") || strings.HasPrefix(body.Domain, "http") {
		writeError(w, http.StatusBadRequest, "domain must be a bare hostname")
		return
	}
	d, err := h.domains.Add(r.Context(), customerID, body.Domain, body.Label)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (h *Handler) dashboardDeleteDomain(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.domains.Delete(r.Context(), customerID, id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// =====================================================================
// Pilot report (HTML + PDF)
// =====================================================================

func (h *Handler) dashboardPilotReport(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	report, err := h.buildPilotReport(r.Context(), customerID)
	if err != nil {
		slog.Error("build pilot report", "err", err)
		writeError(w, http.StatusInternalServerError, "report build failed")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) dashboardPilotReportPDF(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	report, err := h.buildPilotReport(r.Context(), customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "report build failed")
		return
	}
	pdf, err := renderPilotReportPDF(report)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pdf render failed")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="bastio-shadow-ai-audit.pdf"`)
	_, _ = w.Write(pdf)
}
