package observability

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// ListSessions groups traces by session_id for the /sessions list page.
// A session is every trace sharing the same non-empty session_id within a
// customer scope. Aggregates: trace count, token totals, cost, duration
// span, threat count, end-user.
//
// Query params:
//
//	from, to       — RFC3339 time range
//	end_user_id    — exact match on the owning end-user
//	search         — case-insensitive substring over session_id
//	limit, offset  — pagination (default 50, max 200)
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	query := `SELECT
		session_id,
		count() AS trace_count,
		countIf(threat_detected) AS threat_count,
		countIf(status = 'blocked') AS blocked_count,
		countIf(status = 'error') AS error_count,
		sum(input_tokens) AS total_input_tokens,
		sum(output_tokens) AS total_output_tokens,
		sum(total_tokens) AS total_tokens,
		sum(cost_cents) AS total_cost_cents,
		min(started_at) AS first_started_at,
		max(completed_at) AS last_completed_at,
		sum(duration_ms) AS total_duration_ms,
		anyLast(end_user_id) AS end_user_id
	FROM bastio.traces
	WHERE customer_id = toUUID(?) AND session_id != ''`

	args := []any{tenantIDFromCtx(r.Context())}

	if v := q.Get("end_user_id"); v != "" {
		query += " AND end_user_id = ?"
		args = append(args, v)
	}
	if v := q.Get("environment"); v != "" {
		query += " AND environment = ?"
		args = append(args, v)
	}
	if v := q.Get("from"); v != "" {
		query += " AND started_at >= parseDateTime64BestEffort(?)"
		args = append(args, v)
	}
	if v := q.Get("to"); v != "" {
		query += " AND started_at <= parseDateTime64BestEffort(?)"
		args = append(args, v)
	}
	if v := q.Get("search"); v != "" {
		query += " AND positionCaseInsensitive(session_id, ?) > 0"
		args = append(args, v)
	}

	query += " GROUP BY session_id ORDER BY last_completed_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.ch.Conn.Query(r.Context(), query, args...)
	if err != nil {
		slog.Error("query sessions", "error", err)
		http.Error(w, `{"error":"query sessions failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sessions := []map[string]any{}
	for rows.Next() {
		var (
			sessionID                                       string
			traceCount, threatCount, blockedCount, errorCnt uint64
			totalInputTokens, totalOutputTokens             uint64
			totalTokens                                     uint64
			totalCostCents                                  float64
			firstStartedAt, lastCompletedAt                 time.Time
			totalDurationMs                                 uint64
			endUserID                                       string
		)
		if err := rows.Scan(&sessionID, &traceCount, &threatCount, &blockedCount, &errorCnt,
			&totalInputTokens, &totalOutputTokens, &totalTokens, &totalCostCents,
			&firstStartedAt, &lastCompletedAt, &totalDurationMs, &endUserID); err != nil {
			slog.Error("scan session row", "error", err)
			continue
		}
		sessions = append(sessions, map[string]any{
			"id":                  sessionID,
			"trace_count":         traceCount,
			"threat_count":        threatCount,
			"blocked_count":       blockedCount,
			"error_count":         errorCnt,
			"input_tokens":        totalInputTokens,
			"output_tokens":       totalOutputTokens,
			"total_tokens":        totalTokens,
			"total_cost_cents":    totalCostCents,
			"first_started_at":    firstStartedAt.UTC().Format(time.RFC3339Nano),
			"last_completed_at":   lastCompletedAt.UTC().Format(time.RFC3339Nano),
			"total_duration_ms":   totalDurationMs,
			"wall_clock_ms":       uint64(lastCompletedAt.Sub(firstStartedAt).Milliseconds()),
			"end_user_id":         endUserID,
		})
	}

	writeJSON(w, http.StatusOK, sessions)
}

// GetSession returns the session header (same aggregates as list) plus
// every trace in the session ordered by started_at. Trace bodies are
// omitted — the UI fetches them on demand via GET /traces/{id}.
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
		return
	}

	// Header aggregates.
	var (
		traceCount, threatCount, blockedCount, errorCnt uint64
		totalInputTokens, totalOutputTokens             uint64
		totalTokens                                     uint64
		totalCostCents                                  float64
		firstStartedAt, lastCompletedAt                 time.Time
		totalDurationMs                                 uint64
		endUserID                                       string
	)
	err := h.ch.Conn.QueryRow(r.Context(), `
		SELECT
			count() AS trace_count,
			countIf(threat_detected) AS threat_count,
			countIf(status = 'blocked') AS blocked_count,
			countIf(status = 'error') AS error_count,
			sum(input_tokens), sum(output_tokens), sum(total_tokens), sum(cost_cents),
			min(started_at), max(completed_at), sum(duration_ms),
			anyLast(end_user_id)
		FROM bastio.traces
		WHERE customer_id = toUUID(?) AND session_id = ?
	`, tenantIDFromCtx(r.Context()), sessionID).Scan(
		&traceCount, &threatCount, &blockedCount, &errorCnt,
		&totalInputTokens, &totalOutputTokens, &totalTokens, &totalCostCents,
		&firstStartedAt, &lastCompletedAt, &totalDurationMs, &endUserID,
	)
	if err != nil || traceCount == 0 {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	// Trace list (no bodies).
	rows, err := h.ch.Conn.Query(r.Context(), `
		SELECT toString(id), method, path, provider, model,
			started_at, completed_at, duration_ms,
			input_tokens, output_tokens, total_tokens, cost_cents,
			status, http_status, threat_detected, threat_types, threat_score, security_action,
			end_user_id
		FROM bastio.traces
		WHERE customer_id = toUUID(?) AND session_id = ?
		ORDER BY started_at ASC
	`, tenantIDFromCtx(r.Context()), sessionID)
	if err != nil {
		slog.Error("query session traces", "error", err, "session_id", sessionID)
		http.Error(w, `{"error":"query session traces failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	traces := []map[string]any{}
	for rows.Next() {
		var (
			id, method, path, provider, model string
			startedAt, completedAt            time.Time
			durationMs                        uint32
			inputTokens, outputTokens         uint32
			totalToks                         uint32
			costCents                         float64
			status                            string
			httpStatus                        uint16
			threatDetected                    bool
			threatTypes                       []string
			threatScore                       float32
			securityAction, traceEndUser      string
		)
		if err := rows.Scan(&id, &method, &path, &provider, &model,
			&startedAt, &completedAt, &durationMs,
			&inputTokens, &outputTokens, &totalToks, &costCents,
			&status, &httpStatus, &threatDetected, &threatTypes, &threatScore, &securityAction,
			&traceEndUser); err != nil {
			continue
		}
		traces = append(traces, map[string]any{
			"id":              id,
			"method":          method,
			"path":            path,
			"provider":        provider,
			"model":           model,
			"started_at":      startedAt.UTC().Format(time.RFC3339Nano),
			"completed_at":    completedAt.UTC().Format(time.RFC3339Nano),
			"duration_ms":     durationMs,
			"input_tokens":    inputTokens,
			"output_tokens":   outputTokens,
			"total_tokens":    totalToks,
			"cost_cents":      costCents,
			"status":          status,
			"http_status":     httpStatus,
			"threat_detected": threatDetected,
			"threat_types":    threatTypes,
			"threat_score":    threatScore,
			"security_action": securityAction,
			"end_user_id":     traceEndUser,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session": map[string]any{
			"id":                sessionID,
			"trace_count":       traceCount,
			"threat_count":      threatCount,
			"blocked_count":     blockedCount,
			"error_count":       errorCnt,
			"input_tokens":      totalInputTokens,
			"output_tokens":     totalOutputTokens,
			"total_tokens":      totalTokens,
			"total_cost_cents":  totalCostCents,
			"first_started_at":  firstStartedAt.UTC().Format(time.RFC3339Nano),
			"last_completed_at": lastCompletedAt.UTC().Format(time.RFC3339Nano),
			"total_duration_ms": totalDurationMs,
			"wall_clock_ms":     uint64(lastCompletedAt.Sub(firstStartedAt).Milliseconds()),
			"end_user_id":       endUserID,
		},
		"traces": traces,
	})
}
