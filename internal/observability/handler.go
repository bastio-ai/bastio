package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/pkg/clickhouse"
	"github.com/bastio-ai/bastio/pkg/tenant"
)

// DefaultCustomerID is the seeded OSS single-tenant customer, mirrored
// from pkg/tenant. Kept for backward compatibility; the handlers in
// this package MUST resolve the tenant from request context via
// tenantIDFromCtx so cloud (multi-tenant) requests are correctly scoped.
var DefaultCustomerID = tenant.DefaultOSSID.String()

// tenantIDFromCtx reads the request-bound customer from context.
// OSSMiddleware sets DefaultOSSID in single-tenant deployments; cloud's
// auth middleware overrides with the real session-bound customer.
func tenantIDFromCtx(ctx context.Context) string {
	if id, err := tenant.FromContext(ctx); err == nil {
		return id.String()
	}
	return DefaultCustomerID
}

// Handler serves observability API endpoints.
type Handler struct {
	ch *clickhouse.CH
}

// NewHandler creates a new observability handler.
func NewHandler(ch *clickhouse.CH) *Handler {
	return &Handler{ch: ch}
}

// Routes returns the Chi router for observability endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/traces", h.ListTraces)
	r.Get("/traces/{id}", h.GetTrace)
	r.Get("/traces/{id}/threats", h.ListTraceThreats)
	r.Post("/traces/{id}/scores", h.CreateScore)
	r.Get("/traces/{id}/scores", h.ListScores)
	r.Get("/sessions", h.ListSessions)
	r.Get("/sessions/{id}", h.GetSession)
	r.Get("/threats", h.ListThreats)
	r.Get("/threats/{id}", h.GetThreat)
	r.Get("/analytics/overview", h.AnalyticsOverview)
	r.Get("/analytics/users", h.UserAnalytics)
	r.Get("/users", h.UserAnalytics)
	r.Get("/users/{id}", h.GetUser)
	return r
}

// ListTraces queries traces from ClickHouse.
func (h *Handler) ListTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	query := `SELECT
		toString(id), toString(customer_id), toString(proxy_id),
		method, path, provider, model,
		started_at, completed_at, duration_ms,
		input_tokens, output_tokens, total_tokens, cost_cents,
		status, threat_detected, threat_types, threat_score, security_action,
		end_user_id, session_id, environment, release, trace_name, tags
	FROM bastio.traces
	WHERE customer_id = toUUID(?)`

	args := []any{tenantIDFromCtx(r.Context())}

	// Exact-match filters.
	for _, f := range []struct{ param, column string }{
		{"status", "status"},
		{"provider", "provider"},
		{"model", "model"},
		{"end_user_id", "end_user_id"},
		{"proxy_id", "proxy_id"},
		{"environment", "environment"},
		{"release", "release"},
		{"trace_name", "trace_name"},
	} {
		if v := q.Get(f.param); v != "" {
			if f.param == "proxy_id" {
				query += " AND " + f.column + " = toUUID(?)"
			} else {
				query += " AND " + f.column + " = ?"
			}
			args = append(args, v)
		}
	}

	// Tag filters: `?tag=key:value` pairs. Repeat the param for AND-ing
	// multiple tags, e.g. ?tag=feature:checkout&tag=tier:pro.
	for _, raw := range q["tag"] {
		if raw == "" {
			continue
		}
		sep := strings.Index(raw, ":")
		if sep <= 0 {
			continue
		}
		query += " AND tags[?] = ?"
		args = append(args, raw[:sep], raw[sep+1:])
	}

	// Time range (RFC3339 or any ClickHouse-parseable datetime).
	if v := q.Get("from"); v != "" {
		query += " AND started_at >= parseDateTime64BestEffort(?)"
		args = append(args, v)
	}
	if v := q.Get("to"); v != "" {
		query += " AND started_at <= parseDateTime64BestEffort(?)"
		args = append(args, v)
	}

	// Free-text search on path (ClickHouse doesn't have ILIKE; use
	// positionCaseInsensitive which is indexed-friendly).
	if v := q.Get("search"); v != "" {
		query += " AND positionCaseInsensitive(path, ?) > 0"
		args = append(args, v)
	}

	query += " ORDER BY started_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.ch.Conn.Query(r.Context(), query, args...)
	if err != nil {
		slog.Error("query traces failed", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"query traces: %s"}`, err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var traces []map[string]any
	for rows.Next() {
		var (
			id, customerID, proxyID                 string
			method, path, provider, model           string
			startedAt, completedAt                  time.Time
			durationMs                              uint32
			inputTokens, outputTokens               uint32
			totalTokens                             uint32
			costCents                               float64
			status                                  string
			threatDetected                          bool
			threatTypes                             []string
			threatScore                             float32
			securityAction                          string
			endUserID, sessionID                    string
			environment, release, traceName         string
			tags                                    map[string]string
		)

		if err := rows.Scan(
			&id, &customerID, &proxyID, &method, &path, &provider, &model,
			&startedAt, &completedAt, &durationMs,
			&inputTokens, &outputTokens, &totalTokens, &costCents,
			&status, &threatDetected, &threatTypes, &threatScore, &securityAction,
			&endUserID, &sessionID, &environment, &release, &traceName, &tags,
		); err != nil {
			slog.Error("scan trace row", "error", err)
			continue
		}

		traces = append(traces, map[string]any{
			"id": id, "customer_id": customerID, "proxy_id": proxyID,
			"method": method, "path": path, "provider": provider, "model": model,
			"started_at":   startedAt.UTC().Format(time.RFC3339Nano),
			"completed_at": completedAt.UTC().Format(time.RFC3339Nano),
			"duration_ms": durationMs,
			"input_tokens": inputTokens, "output_tokens": outputTokens,
			"total_tokens": totalTokens, "cost_cents": costCents,
			"status": status, "threat_detected": threatDetected,
			"threat_types": threatTypes, "threat_score": threatScore,
			"security_action": securityAction, "end_user_id": endUserID,
			"session_id":  sessionID,
			"environment": environment,
			"release":     release,
			"trace_name":  traceName,
			"tags":        tags,
		})
	}

	if traces == nil {
		traces = []map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(traces)
}

// splitNonEmpty splits s on sep and drops empty segments. Used to parse
// comma-separated multi-value filters (e.g. severity=critical,high).
func splitNonEmpty(s, sep string) []string {
	raw := strings.Split(s, sep)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// threatSortColumns maps the ?sort= query param to a ClickHouse ORDER BY
// expression. Keys are the allow-listed values; anything else falls back
// to detected_at. Severity is ordered semantically (critical → low), not
// alphabetically.
var threatSortColumns = map[string]string{
	"detected_at": "detected_at",
	"score":       "score",
	"confidence":  "confidence",
	"severity":    "multiIf(severity = 'critical', 0, severity = 'high', 1, severity = 'medium', 2, severity = 'low', 3, 4)",
}

// buildThreatsQuery constructs the parameterised SELECT for ListThreats.
// Pure function so it can be unit-tested without a live ClickHouse client.
// customerID is always the first positional arg, guaranteeing the tenant
// WHERE clause is present on every path.
func buildThreatsQuery(customerID string, q url.Values) (sql string, args []any) {
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	var b strings.Builder
	b.WriteString(`SELECT
		toString(id), toString(trace_id), threat_type, severity, score, action_taken,
		detector_name, matched_pattern, matched_content, confidence,
		end_user_id, ip_address, user_agent, details, detected_at,
		threat_subtype, source
	FROM bastio.security_threat_logs
	WHERE customer_id = toUUID(?)`)

	args = []any{customerID}

	for _, f := range []struct {
		param, column string
		// Some filters accept comma-separated multi-values and render
		// as `column IN (?,?,...)`. Single-value filters stay =?.
		multi bool
	}{
		{"severity", "severity", true},
		{"threat_type", "threat_type", true},
		{"threat_subtype", "threat_subtype", true},
		{"detector_name", "detector_name", false},
		{"action_taken", "action_taken", false},
		{"end_user_id", "end_user_id", false},
		{"ip_address", "ip_address", false},
	} {
		v := q.Get(f.param)
		if v == "" {
			continue
		}
		if f.multi && strings.Contains(v, ",") {
			parts := splitNonEmpty(v, ",")
			if len(parts) == 0 {
				continue
			}
			b.WriteString(" AND ")
			b.WriteString(f.column)
			b.WriteString(" IN (")
			for i, p := range parts {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString("?")
				args = append(args, p)
			}
			b.WriteString(")")
			continue
		}
		b.WriteString(" AND ")
		b.WriteString(f.column)
		b.WriteString(" = ?")
		args = append(args, v)
	}

	if v := q.Get("from"); v != "" {
		b.WriteString(" AND detected_at >= parseDateTime64BestEffort(?)")
		args = append(args, v)
	}
	if v := q.Get("to"); v != "" {
		b.WriteString(" AND detected_at <= parseDateTime64BestEffort(?)")
		args = append(args, v)
	}

	if v := q.Get("search"); v != "" {
		b.WriteString(" AND (positionCaseInsensitive(matched_pattern, ?) > 0 OR positionCaseInsensitive(matched_content, ?) > 0)")
		args = append(args, v, v)
	}

	sortCol, ok := threatSortColumns[q.Get("sort")]
	if !ok {
		sortCol = threatSortColumns["detected_at"]
	}
	direction := "DESC"
	if strings.EqualFold(q.Get("order"), "asc") {
		direction = "ASC"
	}
	b.WriteString(" ORDER BY ")
	b.WriteString(sortCol)
	b.WriteString(" ")
	b.WriteString(direction)
	b.WriteString(", detected_at DESC LIMIT ? OFFSET ?")
	args = append(args, limit, offset)

	return b.String(), args
}

// ListThreats queries threat events from ClickHouse with dynamic filters.
// Query construction lives in buildThreatsQuery; this wrapper handles
// execution, row scanning, and response encoding.
func (h *Handler) ListThreats(w http.ResponseWriter, r *http.Request) {
	query, args := buildThreatsQuery(tenantIDFromCtx(r.Context()), r.URL.Query())

	rows, err := h.ch.Conn.Query(r.Context(), query, args...)
	if err != nil {
		slog.Error("query threats failed", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"query threats: %s"}`, err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var threats []map[string]any
	for rows.Next() {
		var (
			id, traceID, threatType, severity string
			score                             float32
			actionTaken, detectorName         string
			matchedPattern, matchedContent    string
			confidence                        float32
			endUserID, ipAddress, userAgent   string
			details                           map[string]string
			detectedAt                        time.Time
			threatSubtype, source             string
		)

		if err := rows.Scan(
			&id, &traceID, &threatType, &severity, &score, &actionTaken,
			&detectorName, &matchedPattern, &matchedContent, &confidence,
			&endUserID, &ipAddress, &userAgent, &details, &detectedAt,
			&threatSubtype, &source,
		); err != nil {
			slog.Error("scan threat row", "error", err)
			continue
		}

		threats = append(threats, map[string]any{
			"id": id, "trace_id": traceID, "threat_type": threatType,
			"severity": severity, "score": score, "action_taken": actionTaken,
			"weighted_score": weightedThreatScore(score, confidence),
			"detector_name":  detectorName, "matched_pattern": matchedPattern,
			"matched_content": matchedContent, "confidence": confidence,
			"end_user_id": endUserID, "ip_address": ipAddress,
			"user_agent": userAgent, "details": details,
			"detected_at":    detectedAt.UTC().Format(time.RFC3339Nano),
			"threat_subtype": threatSubtype,
			"source":         source,
		})
	}

	if threats == nil {
		threats = []map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(threats)
}

// weightedThreatScore derives score × confidence clamped to [0, 1] —
// the value the security engine's threshold check compares against.
// security_threat_logs has a fixed column list without a weighted_score
// column, so the API layer computes it from the two stored columns
// instead of requiring a ClickHouse migration.
func weightedThreatScore(score, confidence float32) float32 {
	w := score * confidence
	if w < 0 {
		return 0
	}
	if w > 1 {
		return 1
	}
	return w
}

// GetThreat returns a single threat event by id, scoped to the current
// tenant. A threat id that belongs to a different customer returns 404 —
// the tenant boundary is the WHERE clause, not the URL.
func (h *Handler) GetThreat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, `{"error":"invalid threat id"}`, http.StatusBadRequest)
		return
	}

	var (
		threatID, traceID, threatType, severity string
		score                                   float32
		actionTaken, detectorName               string
		matchedPattern, matchedContent          string
		confidence                              float32
		endUserID, ipAddress, userAgent         string
		details                                 map[string]string
		detectedAt                              time.Time
		threatSubtype, source                   string
	)

	err := h.ch.Conn.QueryRow(r.Context(), `
		SELECT toString(id), toString(trace_id), threat_type, severity, score, action_taken,
			detector_name, matched_pattern, matched_content, confidence,
			end_user_id, ip_address, user_agent, details, detected_at,
			threat_subtype, source
		FROM bastio.security_threat_logs
		WHERE customer_id = toUUID(?) AND id = toUUID(?)
		LIMIT 1
	`, tenantIDFromCtx(r.Context()), id).Scan(
		&threatID, &traceID, &threatType, &severity, &score, &actionTaken,
		&detectorName, &matchedPattern, &matchedContent, &confidence,
		&endUserID, &ipAddress, &userAgent, &details, &detectedAt,
		&threatSubtype, &source,
	)
	if err != nil {
		http.Error(w, `{"error":"threat not found"}`, http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":              threatID,
		"trace_id":        traceID,
		"threat_type":     threatType,
		"severity":        severity,
		"score":           score,
		"weighted_score":  weightedThreatScore(score, confidence),
		"action_taken":    actionTaken,
		"detector_name":   detectorName,
		"matched_pattern": matchedPattern,
		"matched_content": matchedContent,
		"confidence":      confidence,
		"end_user_id":     endUserID,
		"ip_address":      ipAddress,
		"user_agent":      userAgent,
		"details":         details,
		"detected_at":     detectedAt.UTC().Format(time.RFC3339Nano),
		"threat_subtype":  threatSubtype,
		"source":          source,
	})
}

// AnalyticsOverview returns aggregate metrics for the dashboard. Accepts
// optional ?from=&to= (RFC3339) to scope the window; defaults to the
// ClickHouse TTL bound (all retained data) if absent.
func (h *Handler) AnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	from := q.Get("from")
	to := q.Get("to")

	// Build a reusable WHERE clause with optional time range.
	rangeClause := ""
	rangeArgs := []any{}
	if from != "" {
		rangeClause += " AND timestamp >= parseDateTime64BestEffort(?)"
		rangeArgs = append(rangeArgs, from)
	}
	if to != "" {
		rangeClause += " AND timestamp <= parseDateTime64BestEffort(?)"
		rangeArgs = append(rangeArgs, to)
	}
	threatRange := ""
	threatRangeArgs := []any{}
	if from != "" {
		threatRange += " AND detected_at >= parseDateTime64BestEffort(?)"
		threatRangeArgs = append(threatRangeArgs, from)
	}
	if to != "" {
		threatRange += " AND detected_at <= parseDateTime64BestEffort(?)"
		threatRangeArgs = append(threatRangeArgs, to)
	}

	var totalReqs, totalThreats, totalBlocked uint64
	var totalCost float64
	var avgDuration float64

	overviewArgs := append([]any{tenantIDFromCtx(r.Context())},rangeArgs...)
	_ = h.ch.Conn.QueryRow(ctx, `
		SELECT
			count() AS total_requests,
			countIf(threat_detected = true) AS total_threats,
			countIf(status = 'blocked') AS total_blocked,
			sum(cost_cents) AS total_cost,
			avg(duration_ms) AS avg_duration
		FROM bastio.analytics_request_logs
		WHERE customer_id = toUUID(?)`+rangeClause,
		overviewArgs...).Scan(&totalReqs, &totalThreats, &totalBlocked, &totalCost, &avgDuration)

	requestsByHour := []map[string]any{}
	hourlyArgs := append([]any{tenantIDFromCtx(r.Context())},rangeArgs...)
	if rows, err := h.ch.Conn.Query(ctx, `
		SELECT toStartOfHour(timestamp) AS hour, count() AS cnt
		FROM bastio.analytics_request_logs
		WHERE customer_id = toUUID(?)`+rangeClause+`
		GROUP BY hour ORDER BY hour ASC
	`, hourlyArgs...); err == nil {
		defer rows.Close()
		for rows.Next() {
			var hour time.Time
			var cnt uint64
			if rows.Scan(&hour, &cnt) == nil {
				requestsByHour = append(requestsByHour, map[string]any{
					"hour":  hour.UTC().Format(time.RFC3339),
					"count": cnt,
				})
			}
		}
	}

	var threatsByType []map[string]any
	threatsArgs := append([]any{tenantIDFromCtx(r.Context())},threatRangeArgs...)
	if rows, err := h.ch.Conn.Query(ctx, `
		SELECT threat_type, count() AS cnt
		FROM bastio.security_threat_logs
		WHERE customer_id = toUUID(?)`+threatRange+`
		GROUP BY threat_type ORDER BY cnt DESC LIMIT 10
	`, threatsArgs...); err == nil {
		defer rows.Close()
		for rows.Next() {
			var tt string
			var cnt uint64
			if rows.Scan(&tt, &cnt) == nil {
				threatsByType = append(threatsByType, map[string]any{"type": tt, "count": cnt})
			}
		}
	}

	var topModels []map[string]any
	modelsArgs := append([]any{tenantIDFromCtx(r.Context())},rangeArgs...)
	if rows, err := h.ch.Conn.Query(ctx, `
		SELECT model, count() AS cnt, sum(cost_cents) AS cost
		FROM bastio.analytics_request_logs
		WHERE customer_id = toUUID(?)`+rangeClause+`
		GROUP BY model ORDER BY cnt DESC LIMIT 10
	`, modelsArgs...); err == nil {
		defer rows.Close()
		for rows.Next() {
			var m string
			var cnt uint64
			var cost float64
			if rows.Scan(&m, &cnt, &cost) == nil {
				topModels = append(topModels, map[string]any{"model": m, "count": cnt, "cost_cents": cost})
			}
		}
	}

	if threatsByType == nil {
		threatsByType = []map[string]any{}
	}
	if topModels == nil {
		topModels = []map[string]any{}
	}

	result := map[string]any{
		"total_requests":   totalReqs,
		"total_threats":    totalThreats,
		"total_blocked":    totalBlocked,
		"total_cost_cents": totalCost,
		"avg_duration_ms":  avgDuration,
		"requests_by_hour": requestsByHour,
		"threats_by_type":  threatsByType,
		"top_models":       topModels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
