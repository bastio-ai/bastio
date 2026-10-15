package workspace

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MCPHistoryPolicy struct {
	Mode           string      `json:"mode"`
	RetentionDays  int         `json:"retention_days"`
	CaptureAll     bool        `json:"capture_all"`
	ConnectionIDs  []uuid.UUID `json:"connection_ids"`
	ViewerUserIDs  []string    `json:"viewer_user_ids"`
	ViewerGroupIDs []uuid.UUID `json:"viewer_group_ids"`
	Version        int64       `json:"version"`
}
type mcpHistoryPolicyResponse struct {
	Policy              MCPHistoryPolicy `json:"policy"`
	StorageAllowed      bool             `json:"storage_allowed"`
	EncryptionAvailable bool             `json:"encryption_available"`
	TracingAvailable    bool             `json:"tracing_available"`
}

func defaultMCPHistoryPolicy() MCPHistoryPolicy {
	return MCPHistoryPolicy{Mode: "metadata_only", RetentionDays: 7, ConnectionIDs: []uuid.UUID{}, ViewerUserIDs: []string{}, ViewerGroupIDs: []uuid.UUID{}}
}
func (s *Store) mcpHistoryPolicy(ctx context.Context, cid uuid.UUID) (MCPHistoryPolicy, error) {
	p := defaultMCPHistoryPolicy()
	err := s.pool.QueryRow(ctx, `SELECT mode,retention_days,capture_all,connection_ids,viewer_user_ids,viewer_group_ids,version FROM workspace_mcp_history_policy WHERE customer_id=$1`, cid).Scan(&p.Mode, &p.RetentionDays, &p.CaptureAll, &p.ConnectionIDs, &p.ViewerUserIDs, &p.ViewerGroupIDs, &p.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	return p, err
}
func (h *Handler) getMCPHistoryPolicy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cid := customerIDFromCtx(r.Context())
	p, err := h.store.mcpHistoryPolicy(r.Context(), cid)
	if err != nil {
		writeError(w, 503, "MCP history policy unavailable")
		return
	}
	st, err := h.store.EnsureSettings(r.Context(), cid)
	if err != nil {
		writeError(w, 503, "workspace storage policy unavailable")
		return
	}
	writeJSON(w, 200, mcpHistoryPolicyResponse{p, st.StoreConversationContent, h.mcpCipher != nil, h.obsRecorder != nil})
}
func validMCPHistoryPolicy(p MCPHistoryPolicy) bool {
	return (p.Mode == "metadata_only" || p.Mode == "sanitized") && p.RetentionDays >= 1 && p.RetentionDays <= 90 && len(p.ConnectionIDs) <= 1000 && len(p.ViewerUserIDs) <= 1000 && len(p.ViewerGroupIDs) <= 100
}
func (h *Handler) putMCPHistoryPolicy(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	var p MCPHistoryPolicy
	if decodeMCP(r, &p) != nil || !validMCPHistoryPolicy(p) {
		writeError(w, 400, "choose metadata_only or sanitized history and retention between 1 and 90 days")
		return
	}
	if p.Mode == "sanitized" && !p.CaptureAll && len(p.ConnectionIDs) == 0 {
		writeError(w, 400, "select integrations to capture, or explicitly enable all integrations")
		return
	}
	if p.Mode == "sanitized" && h.obsRecorder == nil {
		writeError(w, 503, "trace recording is unavailable")
		return
	}
	if p.Mode == "sanitized" && h.mcpCipher == nil {
		writeError(w, 503, "encrypted history storage is unavailable")
		return
	}
	if p.ConnectionIDs == nil {
		p.ConnectionIDs = []uuid.UUID{}
	}
	if p.ViewerUserIDs == nil {
		p.ViewerUserIDs = []string{}
	}
	if p.ViewerGroupIDs == nil {
		p.ViewerGroupIDs = []uuid.UUID{}
	}
	cid, ctx := customerIDFromCtx(r.Context()), r.Context()
	tx, err := h.store.pool.Begin(ctx)
	if err != nil {
		writeError(w, 503, "history settings unavailable")
		return
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO workspace_mcp_history_policy(customer_id) VALUES($1) ON CONFLICT DO NOTHING`, cid)
	if err != nil {
		writeError(w, 503, "history settings unavailable")
		return
	}
	var version int64
	if err = tx.QueryRow(ctx, `SELECT version FROM workspace_mcp_history_policy WHERE customer_id=$1 FOR UPDATE`, cid).Scan(&version); err != nil {
		writeError(w, 503, "history settings unavailable")
		return
	}
	// Optimistic versioning avoids overwriting another administrator's edits.
	if p.Version != version && !(p.Version == 0 && version == 1) {
		writeError(w, 409, "history settings changed; reload before saving")
		return
	}
	var allowed bool
	var days int
	if err = tx.QueryRow(ctx, `SELECT store_conversation_content,retention_days FROM workspace_settings WHERE customer_id=$1 FOR SHARE`, cid).Scan(&allowed, &days); err != nil {
		writeError(w, 503, "storage policy unavailable")
		return
	}
	if p.Mode == "sanitized" && !allowed {
		writeError(w, 400, "workspace no-storage policy must allow content storage before enabling MCP history")
		return
	}
	for i, id := range p.ConnectionIDs {
		var found uuid.UUID
		if id == uuid.Nil || slices.Contains(p.ConnectionIDs[:i], id) || tx.QueryRow(ctx, `SELECT id FROM workspace_mcp_connections WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL FOR SHARE`, cid, id).Scan(&found) != nil {
			writeError(w, 400, "select active integrations in this workspace")
			return
		}
	}
	for i, id := range p.ViewerGroupIDs {
		var found uuid.UUID
		if id == uuid.Nil || slices.Contains(p.ViewerGroupIDs[:i], id) || tx.QueryRow(ctx, `SELECT id FROM workspace_access_groups WHERE customer_id=$1 AND id=$2 FOR SHARE`, cid, id).Scan(&found) != nil {
			writeError(w, 400, "select groups in this workspace")
			return
		}
	}
	for i, id := range p.ViewerUserIDs {
		var found string
		if id == "" || slices.Contains(p.ViewerUserIDs[:i], id) || tx.QueryRow(ctx, `SELECT user_id FROM workspace_members WHERE customer_id=$1 AND user_id=$2 FOR SHARE`, cid, id).Scan(&found) != nil {
			writeError(w, 400, "select current workspace members")
			return
		}
	}
	_, err = tx.Exec(ctx, `UPDATE workspace_mcp_history_policy SET mode=$2,retention_days=$3,capture_all=$4,connection_ids=$5,viewer_user_ids=$6,viewer_group_ids=$7,version=version+1,updated_at=now() WHERE customer_id=$1`, cid, p.Mode, p.RetentionDays, p.CaptureAll, p.ConnectionIDs, p.ViewerUserIDs, p.ViewerGroupIDs)
	if err == nil {
		if p.Mode == "metadata_only" {
			_, err = tx.Exec(ctx, `DELETE FROM workspace_mcp_history WHERE customer_id=$1`, cid)
		} else {
			_, err = tx.Exec(ctx, `DELETE FROM workspace_mcp_history WHERE customer_id=$1 AND NOT $2 AND NOT(connection_id=ANY($3))`, cid, p.CaptureAll, p.ConnectionIDs)
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE workspace_mcp_history SET expires_at=LEAST(expires_at,created_at+make_interval(days=>$2)) WHERE customer_id=$1`, cid, min(p.RetentionDays, days))
			}
		}
	}
	if err != nil || tx.Commit(ctx) != nil {
		writeError(w, 503, "history settings could not be saved")
		return
	}
	h.audit(r, "mcp.history_policy_updated", AuditTarget{Type: "settings", ID: cid.String()}, map[string]any{"mode": p.Mode, "retention_days": p.RetentionDays, "capture_all": p.CaptureAll, "integrations": len(p.ConnectionIDs), "reviewers": len(p.ViewerUserIDs), "groups": len(p.ViewerGroupIDs)})
	h.getMCPHistoryPolicy(w, r)
}
func (h *Handler) deleteMCPHistory(w http.ResponseWriter, r *http.Request) {
	// Bump the version under lock so calls started before deletion cannot recreate history.
	cid, ctx := customerIDFromCtx(r.Context()), r.Context()
	tx, err := h.store.pool.Begin(ctx)
	if err != nil {
		writeError(w, 503, "history unavailable")
		return
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO workspace_mcp_history_policy(customer_id) VALUES($1) ON CONFLICT DO NOTHING`, cid)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE workspace_mcp_history_policy SET version=version+1 WHERE customer_id=$1`, cid)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `DELETE FROM workspace_mcp_history WHERE customer_id=$1`, cid)
	}
	if err != nil || tx.Commit(ctx) != nil {
		writeError(w, 503, "history could not be deleted")
		return
	}
	h.audit(r, "mcp.history_deleted", AuditTarget{Type: "settings", ID: cid.String()}, nil)
	w.WriteHeader(204)
}

// Expiry is enforced on every read. This bounded sweep physically deletes
// expired ciphertext within a minute while a server is running; no CH archive.
func (h *Handler) RunMCPHistoryMaintenance(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		sweep, cancel := context.WithTimeout(ctx, 10*time.Second)
		for i := 0; i < 100; i++ {
			tag, err := h.store.pool.Exec(sweep, `DELETE FROM workspace_mcp_history WHERE (customer_id,trace_id) IN (SELECT customer_id,trace_id FROM workspace_mcp_history WHERE expires_at<=now() LIMIT 1000)`)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("MCP history expiry sweep failed; will retry")
				}
				break
			}
			if tag.RowsAffected() < 1000 {
				break
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
