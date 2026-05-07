package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/pkg/tenant"
)

// DashboardRoutes are the read-only views consumed by the IT operator's
// Governance section of the bastio.com dashboard. Mounted INSIDE the
// dashboard middleware group in pkg/server/server.go (cloud layers session
// auth on top; OSS leaves them open per the no-auth-in-OSS-dashboard rule).
func (h *Handler) DashboardRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/overview", h.dashboardOverview)
	r.Get("/events", h.dashboardEvents)
	r.Get("/overrides", h.dashboardOverrides)
	r.Get("/deployments", h.dashboardDeployments)
	r.Get("/installations", h.dashboardInstallations)
	r.Post("/installations", h.dashboardCreateInstallation)
	r.Delete("/installations/{id}", h.dashboardRevokeInstallation)
	r.Get("/installations/{id}/mdm-bundle.zip", h.dashboardMDMBundle)

	r.Get("/policy", h.dashboardGetPolicy)
	r.Put("/policy", h.dashboardUpdatePolicy)

	r.Get("/webhooks", h.dashboardListWebhooks)
	r.Post("/webhooks", h.dashboardCreateWebhook)
	r.Delete("/webhooks/{id}", h.dashboardDeleteWebhook)
	r.Post("/webhooks/{id}/test", h.dashboardTestWebhook)

	r.Get("/domain-overrides", h.dashboardListDomains)
	r.Post("/domain-overrides", h.dashboardAddDomain)
	r.Delete("/domain-overrides/{id}", h.dashboardDeleteDomain)

	r.Get("/pilot-report", h.dashboardPilotReport)
	r.Get("/pilot-report.pdf", h.dashboardPilotReportPDF)

	r.Post("/scim-token", h.dashboardCreateSCIMToken)
	r.Delete("/scim-token", h.dashboardRevokeSCIMToken)
	return r
}

func (h *Handler) dashboardCreateSCIMToken(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	if h.scim == nil {
		writeError(w, http.StatusServiceUnavailable, "scim not configured")
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Label == "" {
		body.Label = "default"
	}
	plain, err := randomB64URL(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rand")
		return
	}
	id, err := h.scim.CreateToken(r.Context(), customerID, body.Label, plain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           id,
		"label":        body.Label,
		"bearer_token": plain,
		"endpoint":     extractBackendURL(r) + "/scim/v2",
		"warning":      "the bearer token is shown only once; paste it into your IdP's SCIM provisioning settings now",
	})
}

func (h *Handler) dashboardRevokeSCIMToken(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	if h.scim == nil {
		writeError(w, http.StatusServiceUnavailable, "scim not configured")
		return
	}
	if err := h.scim.RevokeToken(r.Context(), customerID); err != nil {
		if errors.Is(err, ErrSCIMNotFound) {
			writeError(w, http.StatusNotFound, "no token to revoke")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// OverviewSummary is the response body for /overview.
type OverviewSummary struct {
	WindowDays         int                  `json:"window_days"`
	TotalEvents        int64                `json:"total_events"`
	BySeverity         map[string]int64     `json:"by_severity"`
	ByAction           map[string]int64     `json:"by_action"`
	UniqueUsers        int64                `json:"unique_users"`
	UniqueDomains      int64                `json:"unique_domains"`
	TopDomains         []DomainCount        `json:"top_domains"`
	TopRules           []RuleCount          `json:"top_rules"`
}

type DomainCount struct {
	Domain string `json:"domain"`
	Count  int64  `json:"count"`
}

type RuleCount struct {
	RuleID string `json:"rule_id"`
	Count  int64  `json:"count"`
}

// EventRow is the public shape returned by /events.
type EventRow struct {
	EventID              string    `json:"event_id"`
	OccurredAt           time.Time `json:"occurred_at"`
	UserID               string    `json:"user_id"`
	SourceDomain         string    `json:"source_domain"`
	RuleIDs              []string  `json:"rule_ids"`
	Severity             string    `json:"severity"`
	Action               string    `json:"action"`
	CharCount            int32     `json:"char_count_intercepted"`
	Browser              string    `json:"browser"`
	BrowserVersion       string    `json:"browser_version"`
	ExtensionVersion     string    `json:"extension_version"`
	RedirectTargetLabel  string    `json:"redirect_target_label,omitempty"`
	OverrideJustification string   `json:"override_justification,omitempty"`
}

type DeploymentRow struct {
	OrgID            uuid.UUID `json:"org_id"`
	InstallID        string    `json:"install_id"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	Browser          string    `json:"browser"`
	BrowserVersion   string    `json:"browser_version"`
	ExtensionVersion string    `json:"extension_version"`
}

func (h *Handler) dashboardOverview(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	windowDays := parseIntDefault(r.URL.Query().Get("window_days"), 7)
	if windowDays < 1 || windowDays > 90 {
		windowDays = 7
	}

	summary, err := h.events.Overview(r.Context(), customerID, windowDays)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "overview query failed")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) dashboardEvents(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 50)
	if limit < 1 || limit > 500 {
		limit = 50
	}
	severity := r.URL.Query().Get("severity")
	domain := r.URL.Query().Get("source_domain")

	rows, err := h.events.RecentEvents(r.Context(), customerID, limit, severity, domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "events query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": rows, "limit": limit})
}

func (h *Handler) dashboardDeployments(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	rows, err := h.events.Deployments(r.Context(), customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "deployments query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": rows})
}

func (h *Handler) dashboardInstallations(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	rows, err := h.installs.ListByCustomer(r.Context(), customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "installations query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installations": rows})
}

// dashboardCreateInstallation generates an MDM bundle: a fresh org_id +
// installation_token + installation_secret returned in the response body.
// The secret is shown ONCE — IT must store it in their MDM tooling.
func (h *Handler) dashboardCreateInstallation(w http.ResponseWriter, r *http.Request) {
	customerID := requireTenant(w, r)
	if customerID == uuid.Nil {
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Label == "" {
		body.Label = "default"
	}

	plainToken, err := randomB64URL(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rand token")
		return
	}
	plainSecret, err := randomB64URL(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rand secret")
		return
	}

	inst, err := h.installs.Create(r.Context(), customerID, body.Label, plainToken, plainSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create installation")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                  inst.ID,
		"org_id":              inst.OrgID,
		"label":               inst.Label,
		"installation_token":  plainToken,
		"installation_secret": plainSecret,
		"managed_storage_config": map[string]any{
			"backend_url":         "https://bastio.local", // operator must override
			"org_id":              inst.OrgID,
			"installation_token":  plainToken,
			"installation_secret": plainSecret,
			"telemetry_endpoint":  "/v1/governance/events",
			"override_enabled":    true,
		},
		"warning": "the installation_secret is shown only once; store it in your MDM bundle",
	})
}

func requireTenant(w http.ResponseWriter, r *http.Request) uuid.UUID {
	id, err := tenant.FromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "tenant missing")
		return uuid.Nil
	}
	return id
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// Overview queries the daily MV for KPI counts in the requested window.
// Falls back to a raw scan of governance_events if the MV isn't populated yet
// (newly migrated deployments).
func (s *EventStore) Overview(ctx context.Context, customerID uuid.UUID, windowDays int) (*OverviewSummary, error) {
	if s.ch == nil {
		return &OverviewSummary{WindowDays: windowDays, BySeverity: map[string]int64{}, ByAction: map[string]int64{}}, nil
	}

	out := &OverviewSummary{
		WindowDays:    windowDays,
		BySeverity:    map[string]int64{},
		ByAction:      map[string]int64{},
		TopDomains:    []DomainCount{},
		TopRules:      []RuleCount{},
	}

	since := time.Now().UTC().AddDate(0, 0, -windowDays)

	// total + by severity + by action
	rows, err := s.ch.Conn.Query(ctx, `
SELECT severity, action, count() AS n
FROM bastio.governance_events
WHERE customer_id = ? AND occurred_at >= ?
GROUP BY severity, action`, customerID, since)
	if err != nil {
		return nil, fmt.Errorf("overview group: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sev, act string
		var n uint64
		if err := rows.Scan(&sev, &act, &n); err != nil {
			return nil, err
		}
		out.TotalEvents += int64(n)
		out.BySeverity[sev] += int64(n)
		out.ByAction[act] += int64(n)
	}

	// unique users + domains. ClickHouse `uniq()` returns UInt64;
	// scan into uint64 temps and cast — the driver rejects a
	// direct int64 target ("converting UInt64 to *int64 is unsupported").
	{
		var uniqUsers, uniqDomains uint64
		if err := s.ch.Conn.QueryRow(ctx, `
SELECT uniq(user_id), uniq(source_domain)
FROM bastio.governance_events
WHERE customer_id = ? AND occurred_at >= ?`,
			customerID, since,
		).Scan(&uniqUsers, &uniqDomains); err != nil {
			return nil, fmt.Errorf("overview uniques: %w", err)
		}
		out.UniqueUsers = int64(uniqUsers)
		out.UniqueDomains = int64(uniqDomains)
	}

	// top domains
	dr, err := s.ch.Conn.Query(ctx, `
SELECT source_domain, count() AS n
FROM bastio.governance_events
WHERE customer_id = ? AND occurred_at >= ?
GROUP BY source_domain
ORDER BY n DESC
LIMIT 10`, customerID, since)
	if err != nil {
		return nil, fmt.Errorf("overview top domains: %w", err)
	}
	defer dr.Close()
	for dr.Next() {
		var d string
		var n uint64
		if err := dr.Scan(&d, &n); err != nil {
			return nil, err
		}
		out.TopDomains = append(out.TopDomains, DomainCount{Domain: d, Count: int64(n)})
	}

	// top rules
	rr, err := s.ch.Conn.Query(ctx, `
SELECT arrayJoin(rule_ids) AS rule, count() AS n
FROM bastio.governance_events
WHERE customer_id = ? AND occurred_at >= ?
GROUP BY rule
ORDER BY n DESC
LIMIT 10`, customerID, since)
	if err != nil {
		return nil, fmt.Errorf("overview top rules: %w", err)
	}
	defer rr.Close()
	for rr.Next() {
		var rule string
		var n uint64
		if err := rr.Scan(&rule, &n); err != nil {
			return nil, err
		}
		out.TopRules = append(out.TopRules, RuleCount{RuleID: rule, Count: int64(n)})
	}

	return out, nil
}

// RecentEvents returns the most recent events with optional severity / domain
// filters. Capped at 500 rows; pagination via cursor (occurred_at) is a v1.1 add.
func (s *EventStore) RecentEvents(ctx context.Context, customerID uuid.UUID, limit int, severity, domain string) ([]EventRow, error) {
	if s.ch == nil {
		return nil, nil
	}
	// Enforce the cap the doc comment above already promises. Without
	// this clamp the limit flowed straight into a make([]EventRow, ...,
	// limit) call, which CodeQL flagged as DoS-able if a hostile caller
	// sent a huge value.
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	q := `
SELECT event_id, occurred_at, user_id, source_domain, rule_ids, severity, action,
       char_count_intercepted, browser, browser_version, extension_version,
       redirect_target_label, override_justification
FROM bastio.governance_events
WHERE customer_id = ?`
	args := []any{customerID}
	if severity != "" {
		q += " AND severity = ?"
		args = append(args, severity)
	}
	if domain != "" {
		q += " AND source_domain = ?"
		args = append(args, domain)
	}
	q += " ORDER BY occurred_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.ch.Conn.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("recent events: %w", err)
	}
	defer rows.Close()

	out := make([]EventRow, 0, limit)
	for rows.Next() {
		var ev EventRow
		if err := rows.Scan(
			&ev.EventID, &ev.OccurredAt, &ev.UserID, &ev.SourceDomain, &ev.RuleIDs,
			&ev.Severity, &ev.Action, &ev.CharCount, &ev.Browser, &ev.BrowserVersion,
			&ev.ExtensionVersion, &ev.RedirectTargetLabel, &ev.OverrideJustification,
		); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

// aggregateByDepartment groups event counts by SCIM-pushed department.
// Returns the top 10 departments by event volume so pilot reports stay
// scannable. Users without a department mapping are aggregated into
// "(unmapped)" — a useful signal that the customer's SCIM sync is
// incomplete or that the extension's external_user_id is drifting from
// SCIM userName.
func (s *EventStore) aggregateByDepartment(ctx context.Context, customerID uuid.UUID, windowDays int, userToDept map[string]string) []DepartmentCount {
	if s.ch == nil {
		return nil
	}
	since := time.Now().UTC().AddDate(0, 0, -windowDays)
	rows, err := s.ch.Conn.Query(ctx, `
SELECT user_id, count() AS n
FROM bastio.governance_events
WHERE customer_id = ? AND occurred_at >= ?
GROUP BY user_id`, customerID, since)
	if err != nil {
		return nil
	}
	defer rows.Close()

	deptCounts := map[string]int64{}
	for rows.Next() {
		var userID string
		var n uint64
		if err := rows.Scan(&userID, &n); err != nil {
			continue
		}
		dept := userToDept[userID]
		if dept == "" {
			dept = "(unmapped)"
		}
		deptCounts[dept] += int64(n)
	}

	out := make([]DepartmentCount, 0, len(deptCounts))
	for d, n := range deptCounts {
		out = append(out, DepartmentCount{Department: d, Count: n})
	}
	// Sort descending by count
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Count > out[j-1].Count; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// Deployments returns one row per active install, with last-seen-at.
// ReplacingMergeTree dedups behind us; we run FINAL to force the merge.
func (s *EventStore) Deployments(ctx context.Context, customerID uuid.UUID) ([]DeploymentRow, error) {
	if s.ch == nil {
		return nil, nil
	}

	rows, err := s.ch.Conn.Query(ctx, `
SELECT org_id, install_id, last_seen_at, browser, browser_version, extension_version
FROM bastio.governance_heartbeats FINAL
WHERE customer_id = ?
ORDER BY last_seen_at DESC
LIMIT 500`, customerID)
	if err != nil {
		return nil, fmt.Errorf("deployments: %w", err)
	}
	defer rows.Close()

	out := []DeploymentRow{}
	for rows.Next() {
		var d DeploymentRow
		if err := rows.Scan(&d.OrgID, &d.InstallID, &d.LastSeenAt, &d.Browser, &d.BrowserVersion, &d.ExtensionVersion); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ListByCustomer returns active installation rows for a tenant. Used by the
// dashboard "Installations" view to show what MDM bundles have been issued.
func (s *InstallStore) ListByCustomer(ctx context.Context, customerID uuid.UUID) ([]Installation, error) {
	const q = `
SELECT id, customer_id, org_id, installation_token_hash, '<redacted>', label, created_at, revoked_at
FROM governance_installations
WHERE customer_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC
LIMIT 100`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	defer rows.Close()

	out := []Installation{}
	for rows.Next() {
		inst := Installation{}
		var tokenHash string
		if err := rows.Scan(
			&inst.ID, &inst.CustomerID, &inst.OrgID, &tokenHash, &inst.InstallationSecret,
			&inst.Label, &inst.CreatedAt, &inst.RevokedAt,
		); err != nil {
			return nil, err
		}
		inst.InstallationToken = tokenHash
		out = append(out, inst)
	}
	return out, nil
}

func randomB64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := cryptoRand(b); err != nil {
		return "", err
	}
	return base64URLEncode(b), nil
}
