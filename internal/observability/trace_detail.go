package observability

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GetTrace returns a single trace plus its span hierarchy (observations
// ordered by started_at, each carrying parent_id + depth for waterfall
// rendering). Customer scope comes from the request tenant; a trace id
// that belongs to a different tenant returns 404.
func (h *Handler) GetTrace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, `{"error":"invalid trace id"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	customerID := tenantIDFromCtx(ctx)

	var (
		traceID, custID, proxyID           string
		method, path, provider, model      string
		startedAt, completedAt             time.Time
		durationMs                         uint32
		inputTokens, outputTokens          uint32
		totalTokens                        uint32
		costCents                          float64
		status, errorMessage               string
		httpStatus                         uint16
		threatDetected                     bool
		threatTypes                        []string
		threatScore                        float32
		securityAction                     string
		endUserID, sessionID               string
		requestBody, responseBody          string
		environment, release, traceName    string
		tags                               map[string]string
	)
	err := h.ch.Conn.QueryRow(ctx, `
		SELECT toString(id), toString(customer_id), toString(proxy_id),
			method, path, provider, model,
			started_at, completed_at, duration_ms,
			input_tokens, output_tokens, total_tokens, cost_cents,
			status, error_message, http_status,
			threat_detected, threat_types, threat_score, security_action,
			end_user_id, session_id,
			request_body, response_body,
			environment, release, trace_name, tags
		FROM bastio.traces
		WHERE customer_id = toUUID(?) AND id = toUUID(?)
		LIMIT 1
	`, customerID, id).Scan(
		&traceID, &custID, &proxyID,
		&method, &path, &provider, &model,
		&startedAt, &completedAt, &durationMs,
		&inputTokens, &outputTokens, &totalTokens, &costCents,
		&status, &errorMessage, &httpStatus,
		&threatDetected, &threatTypes, &threatScore, &securityAction,
		&endUserID, &sessionID,
		&requestBody, &responseBody,
		&environment, &release, &traceName, &tags,
	)
	if err != nil {
		http.Error(w, `{"error":"trace not found"}`, http.StatusNotFound)
		return
	}

	spans := h.queryObservations(ctx, customerID, id)

	resp := map[string]any{
		"trace": map[string]any{
			"id":              traceID,
			"customer_id":     custID,
			"proxy_id":        proxyID,
			"method":          method,
			"path":            path,
			"provider":        provider,
			"model":           model,
			"started_at":      startedAt.UTC().Format(time.RFC3339Nano),
			"completed_at":    completedAt.UTC().Format(time.RFC3339Nano),
			"duration_ms":     durationMs,
			"input_tokens":    inputTokens,
			"output_tokens":   outputTokens,
			"total_tokens":    totalTokens,
			"cost_cents":      costCents,
			"status":          status,
			"error_message":   errorMessage,
			"http_status":     httpStatus,
			"threat_detected": threatDetected,
			"threat_types":    threatTypes,
			"threat_score":    threatScore,
			"security_action": securityAction,
			"end_user_id":     endUserID,
			"session_id":      sessionID,
			"request_body":    requestBody,
			"response_body":   responseBody,
			"environment":     environment,
			"release":         release,
			"trace_name":      traceName,
			"tags":            tags,
		},
		"spans": spans,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) queryObservations(ctx chiContext, customerID, traceID string) []map[string]any {
	rows, err := h.ch.Conn.Query(ctx, `
		SELECT toString(id), toString(trace_id), ifNull(toString(parent_id), ''),
			type, name, depth,
			started_at, completed_at, duration_ms,
			input_tokens, output_tokens, model,
			input, output, status, error_message,
			model_parameters, prompt_id, prompt_name, prompt_version,
			tool_name, tool_input, tool_output, status_message, cost_cents
		FROM bastio.observations
		WHERE customer_id = toUUID(?) AND trace_id = toUUID(?)
		ORDER BY started_at ASC
	`, customerID, traceID)
	if err != nil {
		slog.Error("query observations", "error", err, "trace_id", traceID)
		return []map[string]any{}
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var (
			id, tid, parentID                           string
			obsType, name                               string
			depth                                       uint8
			startedAt, completedAt                      time.Time
			durationMs                                  uint32
			inputTokens, outputTokens                   uint32
			model, input, output, status                string
			errorMessage                                string
			modelParameters, promptID, promptName       string
			promptVersion                               uint32
			toolName, toolInput, toolOutput, statusMsg  string
			costCents                                   float64
		)
		if err := rows.Scan(
			&id, &tid, &parentID, &obsType, &name, &depth,
			&startedAt, &completedAt, &durationMs,
			&inputTokens, &outputTokens, &model,
			&input, &output, &status, &errorMessage,
			&modelParameters, &promptID, &promptName, &promptVersion,
			&toolName, &toolInput, &toolOutput, &statusMsg, &costCents,
		); err != nil {
			slog.Error("scan observation", "error", err)
			continue
		}
		out = append(out, map[string]any{
			"id":               id,
			"trace_id":         tid,
			"parent_id":        parentID,
			"type":             obsType,
			"name":             name,
			"depth":            depth,
			"started_at":       startedAt.UTC().Format(time.RFC3339Nano),
			"completed_at":     completedAt.UTC().Format(time.RFC3339Nano),
			"duration_ms":      durationMs,
			"input_tokens":     inputTokens,
			"output_tokens":    outputTokens,
			"model":            model,
			"input":            input,
			"output":           output,
			"status":           status,
			"error_message":    errorMessage,
			"model_parameters": modelParameters,
			"prompt_id":        promptID,
			"prompt_name":      promptName,
			"prompt_version":   promptVersion,
			"tool_name":        toolName,
			"tool_input":       toolInput,
			"tool_output":      toolOutput,
			"status_message":   statusMsg,
			"cost_cents":       costCents,
		})
	}
	return out
}

// ListTraceThreats returns per-detector threat rows for a single trace.
// Feeds the "Security" tab cascade in the dashboard: every detector that
// fired, its severity, confidence, and the matched content snippet.
func (h *Handler) ListTraceThreats(w http.ResponseWriter, r *http.Request) {
	traceID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(traceID); err != nil {
		http.Error(w, `{"error":"invalid trace id"}`, http.StatusBadRequest)
		return
	}

	rows, err := h.ch.Conn.Query(r.Context(), `
		SELECT toString(id), toString(trace_id), threat_type, severity, score, action_taken,
			detector_name, matched_pattern, matched_content, confidence,
			end_user_id, ip_address, detected_at
		FROM bastio.security_threat_logs
		WHERE customer_id = toUUID(?) AND trace_id = toUUID(?)
		ORDER BY
			multiIf(severity = 'critical', 0, severity = 'high', 1, severity = 'medium', 2, severity = 'low', 3, 4) ASC,
			score DESC,
			detected_at ASC
	`, tenantIDFromCtx(r.Context()), traceID)
	if err != nil {
		slog.Error("query trace threats", "error", err, "trace_id", traceID)
		http.Error(w, `{"error":"query trace threats failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	threats := []map[string]any{}
	for rows.Next() {
		var (
			id, tid, threatType, severity string
			score                         float32
			actionTaken, detectorName     string
			matchedPattern, matchedCont   string
			confidence                    float32
			endUserID, ipAddress          string
			detectedAt                    time.Time
		)
		if err := rows.Scan(&id, &tid, &threatType, &severity, &score, &actionTaken,
			&detectorName, &matchedPattern, &matchedCont, &confidence,
			&endUserID, &ipAddress, &detectedAt); err != nil {
			continue
		}
		threats = append(threats, map[string]any{
			"id":               id,
			"trace_id":         tid,
			"threat_type":      threatType,
			"severity":         severity,
			"score":            score,
			"action_taken":     actionTaken,
			"detector_name":    detectorName,
			"matched_pattern":  matchedPattern,
			"matched_content":  matchedCont,
			"confidence":       confidence,
			"end_user_id":      endUserID,
			"ip_address":       ipAddress,
			"detected_at":      detectedAt.UTC().Format(time.RFC3339Nano),
		})
	}

	writeJSON(w, http.StatusOK, threats)
}

// CreateScore attaches a post-hoc score or label to a trace. Supports
// numeric ("accuracy":0.82), categorical ("sentiment":"positive"), or
// boolean ("passed_qa":true). Body:
//
//	{
//	  "name": "accuracy",
//	  "value_type": "numeric" | "categorical" | "boolean",
//	  "numeric_value": 0.82,         // when value_type=numeric
//	  "string_value": "positive",    // when value_type=categorical/boolean
//	  "comment": "optional note",
//	  "evaluator": "human:alice" | "llm:gpt-4o" | "rule:..."
//	}
func (h *Handler) CreateScore(w http.ResponseWriter, r *http.Request) {
	traceID := chi.URLParam(r, "id")
	parsedTrace, err := uuid.Parse(traceID)
	if err != nil {
		http.Error(w, `{"error":"invalid trace id"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Name         string   `json:"name"`
		ValueType    string   `json:"value_type"`
		NumericValue *float64 `json:"numeric_value,omitempty"`
		StringValue  string   `json:"string_value,omitempty"`
		Comment      string   `json:"comment,omitempty"`
		Evaluator    string   `json:"evaluator,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	switch req.ValueType {
	case "numeric":
		if req.NumericValue == nil {
			http.Error(w, `{"error":"numeric_value required when value_type=numeric"}`, http.StatusBadRequest)
			return
		}
	case "categorical", "boolean":
		if req.StringValue == "" {
			http.Error(w, `{"error":"string_value required when value_type=categorical|boolean"}`, http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, `{"error":"value_type must be numeric|categorical|boolean"}`, http.StatusBadRequest)
		return
	}

	id := uuid.New()
	now := time.Now().UTC()
	var numericArg any
	if req.NumericValue != nil {
		numericArg = *req.NumericValue
	}

	if err := h.ch.Conn.Exec(r.Context(), `
		INSERT INTO bastio.trace_scores
		(id, trace_id, customer_id, name, value_type, numeric_value, string_value, comment, evaluator, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		id.String(), parsedTrace.String(), tenantIDFromCtx(r.Context()),
		req.Name, req.ValueType, numericArg, req.StringValue, req.Comment, req.Evaluator, now,
	); err != nil {
		slog.Error("insert trace score", "error", err, "trace_id", traceID)
		http.Error(w, `{"error":"insert score failed"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":            id.String(),
		"trace_id":      parsedTrace.String(),
		"name":          req.Name,
		"value_type":    req.ValueType,
		"numeric_value": req.NumericValue,
		"string_value":  req.StringValue,
		"comment":       req.Comment,
		"evaluator":     req.Evaluator,
		"created_at":    now.Format(time.RFC3339Nano),
	})
}

// ListScores returns all scores attached to a trace, newest first.
func (h *Handler) ListScores(w http.ResponseWriter, r *http.Request) {
	traceID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(traceID); err != nil {
		http.Error(w, `{"error":"invalid trace id"}`, http.StatusBadRequest)
		return
	}

	rows, err := h.ch.Conn.Query(r.Context(), `
		SELECT toString(id), toString(trace_id),
			name, value_type, numeric_value, string_value, comment, evaluator, created_at
		FROM bastio.trace_scores
		WHERE customer_id = toUUID(?) AND trace_id = toUUID(?)
		ORDER BY created_at DESC
	`, tenantIDFromCtx(r.Context()), traceID)
	if err != nil {
		slog.Error("query scores", "error", err, "trace_id", traceID)
		http.Error(w, `{"error":"query scores failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	scores := []map[string]any{}
	for rows.Next() {
		var (
			id, tid                            string
			name, valueType, stringValue       string
			numericValue                       *float64
			comment, evaluator                 string
			createdAt                          time.Time
		)
		if err := rows.Scan(&id, &tid, &name, &valueType, &numericValue, &stringValue, &comment, &evaluator, &createdAt); err != nil {
			slog.Error("scan score", "error", err)
			continue
		}
		scores = append(scores, map[string]any{
			"id":            id,
			"trace_id":      tid,
			"name":          name,
			"value_type":    valueType,
			"numeric_value": numericValue,
			"string_value":  stringValue,
			"comment":       comment,
			"evaluator":     evaluator,
			"created_at":    createdAt.Format(time.RFC3339Nano),
		})
	}

	writeJSON(w, http.StatusOK, scores)
}

// UserAnalytics returns per-end-user cost / latency / threat rollups.
// The end-user is the application's user (passed via X-End-User-Id), not
// a Bastio dashboard user. Rows without an end_user_id are excluded.
func (h *Handler) UserAnalytics(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	orderBy := r.URL.Query().Get("order_by")
	switch orderBy {
	case "cost", "requests", "threats", "latency":
	default:
		orderBy = "cost"
	}
	orderExpr := map[string]string{
		"cost":     "total_cost_cents",
		"requests": "total_requests",
		"threats":  "total_threats",
		"latency":  "avg_duration_ms",
	}[orderBy]

	// Join analytics aggregates with threat counts in the application
	// layer to avoid a cross-table JOIN (ClickHouse prefers sequential
	// queries for simple rollups).
	rows, err := h.ch.Conn.Query(r.Context(), `
		SELECT end_user_id,
			count() AS total_requests,
			sum(cost_cents) AS total_cost_cents,
			avg(duration_ms) AS avg_duration_ms,
			max(timestamp) AS last_request_at
		FROM bastio.analytics_request_logs
		WHERE customer_id = toUUID(?) AND end_user_id != ''
		GROUP BY end_user_id
		ORDER BY `+orderExpr+` DESC
		LIMIT ?
	`, tenantIDFromCtx(r.Context()), limit)
	if err != nil {
		slog.Error("query user analytics", "error", err)
		http.Error(w, `{"error":"query user analytics failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type userRow struct {
		EndUserID      string
		TotalRequests  uint64
		TotalCostCents float64
		AvgDurationMs  float64
		LastRequestAt  time.Time
	}
	var users []userRow
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.EndUserID, &u.TotalRequests, &u.TotalCostCents, &u.AvgDurationMs, &u.LastRequestAt); err != nil {
			continue
		}
		users = append(users, u)
	}
	rows.Close()

	// Fetch threat counts per end_user_id in one query.
	threatCount := map[string]uint64{}
	if trows, err := h.ch.Conn.Query(r.Context(), `
		SELECT end_user_id, count() AS cnt
		FROM bastio.security_threat_logs
		WHERE customer_id = toUUID(?) AND end_user_id != ''
		GROUP BY end_user_id
	`, tenantIDFromCtx(r.Context())); err == nil {
		defer trows.Close()
		for trows.Next() {
			var uid string
			var cnt uint64
			if trows.Scan(&uid, &cnt) == nil {
				threatCount[uid] = cnt
			}
		}
	}

	result := make([]map[string]any, 0, len(users))
	for _, u := range users {
		result = append(result, map[string]any{
			"end_user_id":      u.EndUserID,
			"total_requests":   u.TotalRequests,
			"total_cost_cents": u.TotalCostCents,
			"avg_duration_ms":  u.AvgDurationMs,
			"total_threats":    threatCount[u.EndUserID],
			"last_request_at":  u.LastRequestAt.UTC().Format(time.RFC3339Nano),
		})
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// chiContext is aliased so the generic queryObservations signature
// doesn't force importing context in every caller file.
type chiContext = interface {
	Deadline() (time.Time, bool)
	Done() <-chan struct{}
	Err() error
	Value(any) any
}
