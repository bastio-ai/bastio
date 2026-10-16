package observability

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// Derived from operational metadata, never prompt content. Keep the matching
// dashboard traceType helper in sync; imported rows may not have a provider.
const traceTypeSQL = `multiIf(provider = 'mcp', 'mcp', provider = 'ollama' OR path = 'local/observe', 'local', positionCaseInsensitive(path, '/embeddings') > 0, 'embedding', provider = 'browser-extension', 'extension', provider != '', 'llm', 'other')`

func appendTraceExplorerFilters(query string, args []any, q url.Values) (string, []any) {
	if values := splitNonEmpty(q.Get("status"), ","); len(values) > 0 {
		query += " AND status IN (" + strings.TrimSuffix(strings.Repeat("?,", len(values)), ",") + ")"
		for _, value := range values {
			args = append(args, value)
		}
	}
	if value := q.Get("trace_name_contains"); value != "" {
		query += " AND positionCaseInsensitive(trace_name, ?) > 0"
		args = append(args, value)
	}
	if kind := q.Get("trace_type"); kind != "" {
		query += " AND " + traceTypeSQL + " = ?"
		args = append(args, kind)
	}
	switch q.Get("security") {
	case "threat":
		query += " AND threat_detected = true"
	case "clean":
		query += " AND threat_detected = false"
	}
	return query, args
}

// TraceFacets returns small, metadata-only suggestion lists for the selected
// tenant and time window, rather than downloading hundreds of full trace rows.
func (h *Handler) TraceFacets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := `SELECT groupUniqArray(200)(trace_name), groupUniqArray(100)(provider), groupUniqArray(200)(model), groupUniqArray(100)(environment) FROM bastio.traces WHERE customer_id = toUUID(?)`
	args := []any{tenantIDFromCtx(r.Context())}
	for _, bound := range []struct{ param, comparison string }{{"from", ">="}, {"to", "<="}} {
		if value := q.Get(bound.param); value != "" {
			query += " AND started_at " + bound.comparison + " parseDateTime64BestEffort(?)"
			args = append(args, value)
		}
	}
	if environment := q.Get("environment"); environment != "" {
		query += " AND environment = ?"
		args = append(args, environment)
	}
	names, providers, models, environments := []string{}, []string{}, []string{}, []string{}
	if err := h.ch.Conn.QueryRow(r.Context(), query, args...).Scan(&names, &providers, &models, &environments); err != nil {
		http.Error(w, `{"error":"failed to load trace filter suggestions"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Names        []string `json:"names"`
		Providers    []string `json:"providers"`
		Models       []string `json:"models"`
		Environments []string `json:"environments"`
	}{names, providers, models, environments})
}
