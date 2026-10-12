package workspace

import (
	"net/http"
	"time"
)

type EvidenceCounts struct {
	Messages            int64   `json:"messages"`
	ModelCalls          int64   `json:"model_calls"`
	PolicyBlocks        int64   `json:"policy_blocks"`
	OutputBlocks        int64   `json:"output_blocks"`
	Tokens              int64   `json:"tokens"`
	EstimatedSpendCents float64 `json:"estimated_spend_cents"`
}
type EvidenceReport struct {
	GeneratedAt     time.Time      `json:"generated_at"`
	PeriodStart     time.Time      `json:"period_start"`
	PeriodEnd       time.Time      `json:"period_end"`
	Scope           string         `json:"scope"`
	Certification   string         `json:"certification"`
	Settings        *Settings      `json:"settings"`
	Counts          EvidenceCounts `json:"counts"`
	Events          []AuditEntry   `json:"events"`
	EventsTotal     int            `json:"events_total"`
	EventsTruncated bool           `json:"events_truncated"`
}

func (h *Handler) exportEvidence(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	end := time.Now().UTC()
	start := end.AddDate(0, 0, -30)
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, 400, "from must be an RFC3339 timestamp")
			return
		}
		start = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, 400, "to must be an RFC3339 timestamp")
			return
		}
		end = t
	}
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		writeError(w, 400, "choose a date range up to 366 days")
		return
	}
	st, err := h.store.GetSettings(r.Context(), cid)
	if err != nil {
		writeError(w, 503, "settings unavailable")
		return
	}
	report := EvidenceReport{GeneratedAt: time.Now().UTC(), PeriodStart: start, PeriodEnd: end, Scope: "Workspace PostgreSQL operational records; archived messages and gateway events are excluded", Certification: "Operational evidence only; no compliance certification", Settings: st, Events: []AuditEntry{}}
	err = h.store.pool.QueryRow(r.Context(), `SELECT COUNT(*),COUNT(*) FILTER(WHERE role='assistant' AND (prompt_tokens+completion_tokens)>0),COUNT(*) FILTER(WHERE finish_reason='blocked'),COUNT(*) FILTER(WHERE finish_reason='blocked_output'),COALESCE(SUM(prompt_tokens+completion_tokens),0) FROM workspace_messages WHERE customer_id=$1 AND created_at >= $2 AND created_at < $3`, cid, start, end).Scan(&report.Counts.Messages, &report.Counts.ModelCalls, &report.Counts.PolicyBlocks, &report.Counts.OutputBlocks, &report.Counts.Tokens)
	if err != nil {
		writeError(w, 503, "usage evidence unavailable")
		return
	}
	err = h.store.pool.QueryRow(r.Context(), `SELECT COALESCE(SUM(amount_cents),0) FROM workspace_spend WHERE customer_id=$1 AND created_at >= $2 AND created_at < $3`, cid, start, end).Scan(&report.Counts.EstimatedSpendCents)
	if err != nil {
		writeError(w, 503, "spend evidence unavailable")
		return
	}
	err = h.store.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM workspace_audit_log WHERE customer_id=$1 AND created_at >= $2 AND created_at < $3`, cid, start, end).Scan(&report.EventsTotal)
	if err != nil {
		writeError(w, 503, "audit evidence unavailable")
		return
	}
	rows, err := h.store.pool.Query(r.Context(), `SELECT id,customer_id,actor_user_id,actor_email,actor_role,action,target_type,target_id,created_at FROM workspace_audit_log WHERE customer_id=$1 AND created_at >= $2 AND created_at < $3 ORDER BY created_at DESC,id DESC LIMIT 1000`, cid, start, end)
	if err != nil {
		writeError(w, 503, "audit evidence unavailable")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var e AuditEntry
		if rows.Scan(&e.ID, &e.CustomerID, &e.ActorUserID, &e.ActorEmail, &e.ActorRole, &e.Action, &e.TargetType, &e.TargetID, &e.CreatedAt) != nil {
			writeError(w, 503, "audit evidence unavailable")
			return
		}
		report.Events = append(report.Events, e)
	}
	if rows.Err() != nil {
		writeError(w, 503, "audit evidence unavailable")
		return
	}
	report.EventsTruncated = report.EventsTotal > len(report.Events)
	// Audit labels may contain user-provided titles; evidence uses IDs and actions.
	for i := range report.Events {
		report.Events[i].TargetLabel = ""
		report.Events[i].Metadata = nil
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="bastio-workspace-evidence.json"`)
	writeJSON(w, 200, report)
}
