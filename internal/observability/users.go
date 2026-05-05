package observability

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// GetUser returns per-end-user aggregates plus recent sessions and a
// recent trace list. Mirrors the shape of GetSession so the UI can
// render both with the same primitives (header KPIs + right-pane split).
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	endUserID := chi.URLParam(r, "id")
	if endUserID == "" {
		http.Error(w, `{"error":"missing user id"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	// Header aggregates from traces.
	var (
		totalTraces, totalThreats, totalBlocked, totalErrors uint64
		totalInputTokens, totalOutputTokens, totalTokens     uint64
		totalCostCents                                       float64
		firstSeenAt, lastSeenAt                              time.Time
		avgDurationMs                                        float64
	)
	err := h.ch.Conn.QueryRow(ctx, `
		SELECT
			count() AS total_traces,
			countIf(threat_detected) AS total_threats,
			countIf(status = 'blocked') AS total_blocked,
			countIf(status = 'error') AS total_errors,
			sum(input_tokens), sum(output_tokens), sum(total_tokens), sum(cost_cents),
			min(started_at), max(completed_at), avg(duration_ms)
		FROM bastio.traces
		WHERE customer_id = toUUID(?) AND end_user_id = ?
	`, tenantIDFromCtx(ctx), endUserID).Scan(
		&totalTraces, &totalThreats, &totalBlocked, &totalErrors,
		&totalInputTokens, &totalOutputTokens, &totalTokens, &totalCostCents,
		&firstSeenAt, &lastSeenAt, &avgDurationMs,
	)
	if err != nil || totalTraces == 0 {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	// Sessions this user participated in.
	sessions := []map[string]any{}
	if rows, err := h.ch.Conn.Query(ctx, `
		SELECT session_id,
			count() AS trace_count,
			countIf(threat_detected) AS threat_count,
			sum(total_tokens) AS total_tokens,
			sum(cost_cents) AS total_cost_cents,
			min(started_at), max(completed_at)
		FROM bastio.traces
		WHERE customer_id = toUUID(?) AND end_user_id = ? AND session_id != ''
		GROUP BY session_id
		ORDER BY max(completed_at) DESC
		LIMIT 50
	`, tenantIDFromCtx(ctx), endUserID); err == nil {
		defer rows.Close()
		for rows.Next() {
			var (
				sessionID                                string
				traceCount, threatCount                  uint64
				sessTokens                               uint64
				sessCost                                 float64
				firstStartedAt, lastCompletedAt          time.Time
			)
			if err := rows.Scan(&sessionID, &traceCount, &threatCount,
				&sessTokens, &sessCost, &firstStartedAt, &lastCompletedAt); err != nil {
				continue
			}
			sessions = append(sessions, map[string]any{
				"id":                sessionID,
				"trace_count":       traceCount,
				"threat_count":      threatCount,
				"total_tokens":      sessTokens,
				"total_cost_cents":  sessCost,
				"first_started_at":  firstStartedAt.UTC().Format(time.RFC3339Nano),
				"last_completed_at": lastCompletedAt.UTC().Format(time.RFC3339Nano),
				"end_user_id":       endUserID,
			})
		}
	}

	// Recent traces (no bodies).
	traces := []map[string]any{}
	if rows, err := h.ch.Conn.Query(ctx, `
		SELECT toString(id), method, path, provider, model,
			started_at, completed_at, duration_ms,
			input_tokens, output_tokens, total_tokens, cost_cents,
			status, http_status, threat_detected, threat_types, threat_score, security_action,
			session_id, environment, release, trace_name, tags
		FROM bastio.traces
		WHERE customer_id = toUUID(?) AND end_user_id = ?
		ORDER BY started_at DESC
		LIMIT 100
	`, tenantIDFromCtx(ctx), endUserID); err == nil {
		defer rows.Close()
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
				securityAction, sessionID         string
				environment, release, traceName   string
				tags                              map[string]string
			)
			if err := rows.Scan(&id, &method, &path, &provider, &model,
				&startedAt, &completedAt, &durationMs,
				&inputTokens, &outputTokens, &totalToks, &costCents,
				&status, &httpStatus, &threatDetected, &threatTypes, &threatScore, &securityAction,
				&sessionID, &environment, &release, &traceName, &tags); err != nil {
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
				"end_user_id":     endUserID,
				"session_id":      sessionID,
				"environment":     environment,
				"release":         release,
				"trace_name":      traceName,
				"tags":            tags,
			})
		}
	}

	// Top threats by type for this user.
	threatBreakdown := []map[string]any{}
	if rows, err := h.ch.Conn.Query(ctx, `
		SELECT threat_type, count() AS cnt, avg(score) AS avg_score
		FROM bastio.security_threat_logs
		WHERE customer_id = toUUID(?) AND end_user_id = ?
		GROUP BY threat_type
		ORDER BY cnt DESC
		LIMIT 10
	`, tenantIDFromCtx(ctx), endUserID); err == nil {
		defer rows.Close()
		for rows.Next() {
			var tt string
			var cnt uint64
			var avgScore float32
			if err := rows.Scan(&tt, &cnt, &avgScore); err != nil {
				continue
			}
			threatBreakdown = append(threatBreakdown, map[string]any{
				"threat_type": tt,
				"count":       cnt,
				"avg_score":   avgScore,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":                endUserID,
			"total_traces":      totalTraces,
			"total_threats":     totalThreats,
			"total_blocked":     totalBlocked,
			"total_errors":      totalErrors,
			"input_tokens":      totalInputTokens,
			"output_tokens":     totalOutputTokens,
			"total_tokens":      totalTokens,
			"total_cost_cents":  totalCostCents,
			"first_seen_at":     firstSeenAt.UTC().Format(time.RFC3339Nano),
			"last_seen_at":      lastSeenAt.UTC().Format(time.RFC3339Nano),
			"avg_duration_ms":   avgDurationMs,
			"session_count":     uint64(len(sessions)),
		},
		"sessions":         sessions,
		"traces":           traces,
		"threat_breakdown": threatBreakdown,
	})

	slog.Debug("served user detail", "end_user_id", endUserID)
}
