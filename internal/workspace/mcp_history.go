package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type mcpHistoryContent struct {
	Input       json.RawMessage `json:"input,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
	InputState  string          `json:"input_state"`
	OutputState string          `json:"output_state"`
}
type mcpHistoryCapture struct {
	handler        *Handler
	policy         MCPHistoryPolicy
	epoch          int64
	conversationID uuid.UUID
	connectionID   uuid.UUID
	createdAt      time.Time
	content        mcpHistoryContent
}

func (h *Handler) prepareMCPHistory(r *http.Request, t *mcpToolTrace, c MCPConnection, conversationID uuid.UUID) {
	t.trace.Tags["mcp.history.mode"] = "metadata_only"
	t.trace.Tags["mcp.history.state"] = "not_retained"
	if h.store == nil || h.obsRecorder == nil {
		return
	}
	if privateChat(r.Context()) {
		t.trace.Tags["mcp.history.reason"] = "temporary_chat"
		return
	}
	p, err := h.store.mcpHistoryPolicy(r.Context(), t.trace.CustomerID)
	if err != nil {
		t.trace.Tags["mcp.history.reason"] = "policy_unavailable"
		return
	}
	if p.Mode != "sanitized" || !p.CaptureAll && !slices.Contains(p.ConnectionIDs, c.ID) {
		return
	}
	var allowed bool
	var days int
	var epoch int64
	err = h.store.pool.QueryRow(r.Context(), `SELECT store_conversation_content,retention_days,mcp_history_epoch FROM workspace_settings WHERE customer_id=$1`, t.trace.CustomerID).Scan(&allowed, &days, &epoch)
	if err != nil {
		t.trace.Tags["mcp.history.reason"] = "policy_unavailable"
		return
	}
	if !allowed {
		t.trace.Tags["mcp.history.reason"] = "workspace_no_store"
		return
	}
	if conversationID != uuid.Nil {
		var stored bool
		err = h.store.pool.QueryRow(r.Context(), `SELECT content_stored FROM workspace_conversations WHERE customer_id=$1 AND id=$2`, t.trace.CustomerID, conversationID).Scan(&stored)
		if err != nil || !stored {
			t.trace.Tags["mcp.history.reason"] = "temporary_chat"
			return
		}
	}
	if h.mcpCipher == nil {
		t.trace.Tags["mcp.history.reason"] = "encryption_unavailable"
		return
	}
	p.RetentionDays = min(p.RetentionDays, days)
	t.history = &mcpHistoryCapture{handler: h, policy: p, epoch: epoch, connectionID: c.ID, conversationID: conversationID, createdAt: t.trace.StartedAt, content: mcpHistoryContent{InputState: "not_recorded", OutputState: "not_recorded"}}
	t.trace.Tags["mcp.history.mode"] = "sanitized"
	t.trace.Tags["mcp.history.state"] = "pending"
	t.trace.Tags["mcp.history.retention_days"] = strconv.Itoa(p.RetentionDays)
}

// Only call after traffic inspection succeeds. Arguments/results blocked by
// policy never reach the history sanitizer or any persistent payload store.
func (t *mcpToolTrace) captureHistoryPayload(ctx context.Context, direction string, raw json.RawMessage) {
	if t.history == nil || t.done {
		return
	}
	protected, err := sanitizeMCPHistory(ctx, raw)
	state := "sanitized"
	if err != nil {
		state = "withheld"
		protected = nil
	}
	if len(raw) > mcpHistoryLimit {
		state = "size_limit"
	}
	if direction == "input" {
		t.history.content.Input = protected
		t.history.content.InputState = state
	}
	if direction == "output" {
		t.history.content.Output = protected
		t.history.content.OutputState = state
	}
}
func (t *mcpToolTrace) retainHistory() {
	if t.history == nil {
		return
	}
	h := t.history
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if t.trace.Tags["security.stage"] == "arguments" {
		h.content.InputState = "withheld"
	}
	if t.trace.Tags["security.stage"] == "result" {
		h.content.OutputState = "withheld"
	}
	if t.trace.Tags["remote_action"] == "not_run" {
		h.content.OutputState = "not_run"
	}
	// Policy/settings/connection row locks serialize writes with revocation and
	// deletion. A policy change during a tool call discards its buffered copy.
	tx, err := h.handler.store.pool.Begin(ctx)
	if err != nil {
		t.historyFailed("storage_unavailable")
		return
	}
	defer tx.Rollback(ctx)
	var mode string
	var version int64
	var captureAll bool
	var ids []uuid.UUID
	var days int
	err = tx.QueryRow(ctx, `SELECT mode,version,capture_all,connection_ids,retention_days FROM workspace_mcp_history_policy WHERE customer_id=$1 FOR SHARE`, t.trace.CustomerID).Scan(&mode, &version, &captureAll, &ids, &days)
	if err != nil || mode != "sanitized" || version != h.policy.Version || !captureAll && !slices.Contains(ids, h.connectionID) {
		t.historyFailed("policy_changed")
		return
	}
	var allowed bool
	var workspaceDays int
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT store_conversation_content,retention_days,mcp_history_epoch FROM workspace_settings WHERE customer_id=$1 FOR SHARE`, t.trace.CustomerID).Scan(&allowed, &workspaceDays, &epoch)
	if err != nil || !allowed || epoch != h.epoch {
		t.historyFailed("storage_policy_changed")
		return
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT revoked_at IS NULL FROM workspace_mcp_connections WHERE customer_id=$1 AND id=$2 FOR SHARE`, t.trace.CustomerID, h.connectionID).Scan(&active)
	if err != nil || !active {
		t.historyFailed("integration_revoked")
		return
	}
	if h.conversationID != uuid.Nil {
		var stored bool
		err = tx.QueryRow(ctx, `SELECT content_stored FROM workspace_conversations WHERE customer_id=$1 AND id=$2 FOR SHARE`, t.trace.CustomerID, h.conversationID).Scan(&stored)
		if err != nil || !stored {
			t.historyFailed("temporary_chat")
			return
		}
	}
	plain, err := json.Marshal(h.content)
	if err != nil {
		t.historyFailed("capture_unavailable")
		return
	}
	encrypted, err := h.handler.mcpCipher.Encrypt(string(plain), t.trace.CustomerID.String(), "workspace-mcp-history:"+t.trace.ID.String())
	if err != nil {
		t.historyFailed("encryption_unavailable")
		return
	}
	sealed, err := json.Marshal(encrypted)
	if err != nil {
		t.historyFailed("encryption_unavailable")
		return
	}
	expires := h.createdAt.Add(time.Duration(min(h.policy.RetentionDays, days, workspaceDays)) * 24 * time.Hour)
	if !expires.After(time.Now()) {
		t.historyFailed("expired")
		return
	}
	var conversation any
	if h.conversationID != uuid.Nil {
		conversation = h.conversationID
	}
	_, err = tx.Exec(ctx, `INSERT INTO workspace_mcp_history(customer_id,trace_id,connection_id,conversation_id,created_at,expires_at,encrypted_content) VALUES($1,$2,$3,$4,$5,$6,$7)`, t.trace.CustomerID, t.trace.ID, h.connectionID, conversation, h.createdAt, expires, sealed)
	if err != nil || tx.Commit(ctx) != nil {
		t.historyFailed("storage_unavailable")
		return
	}
	t.trace.Tags["mcp.history.state"] = "retained"
	t.trace.Tags["mcp.history.expires_at"] = expires.UTC().Format(time.RFC3339)
}
func (t *mcpToolTrace) historyFailed(reason string) {
	t.trace.Tags["mcp.history.state"] = "not_retained"
	t.trace.Tags["mcp.history.reason"] = reason
}

type MCPHistoryResponse struct {
	State     string             `json:"state"`
	ExpiresAt *time.Time         `json:"expires_at,omitempty"`
	Content   *mcpHistoryContent `json:"content,omitempty"`
}

func (h *Handler) getMCPHistoryContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, 400, "invalid trace ID")
		return
	}
	cid, ctx := customerIDFromCtx(r.Context()), r.Context()
	// Recipients must be current workspace members AND have integration access.
	// General trace/observation APIs never contain these payloads.
	tx, err := h.store.pool.Begin(ctx)
	if err != nil {
		writeError(w, 503, "history unavailable")
		return
	}
	defer tx.Rollback(ctx)
	p := defaultMCPHistoryPolicy()
	err = tx.QueryRow(ctx, `SELECT mode,retention_days,capture_all,connection_ids,viewer_user_ids,viewer_group_ids,version FROM workspace_mcp_history_policy WHERE customer_id=$1 FOR SHARE`, cid).Scan(&p.Mode, &p.RetentionDays, &p.CaptureAll, &p.ConnectionIDs, &p.ViewerUserIDs, &p.ViewerGroupIDs, &p.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 200, MCPHistoryResponse{State: "not_retained"})
		return
	}
	if err != nil {
		writeError(w, 503, "history policy unavailable")
		return
	}
	if !RoleFromCtx(ctx).AtLeast(RoleAdmin) {
		var viewer bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE customer_id=$1 AND user_id=$2) AND ($2=ANY($3::text[]) OR EXISTS(SELECT 1 FROM workspace_access_group_members gm JOIN workspace_access_groups g ON g.customer_id=gm.customer_id AND g.id=gm.group_id WHERE gm.customer_id=$1 AND gm.user_id=$2 AND gm.group_id=ANY($4::uuid[])))`, cid, userIDFromCtx(ctx), p.ViewerUserIDs, p.ViewerGroupIDs).Scan(&viewer)
		if err != nil || !viewer {
			writeJSON(w, 200, MCPHistoryResponse{State: "restricted"})
			return
		}
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT store_conversation_content FROM workspace_settings WHERE customer_id=$1 FOR SHARE`, cid).Scan(&allowed)
	if err != nil || !allowed || p.Mode != "sanitized" {
		writeJSON(w, 200, MCPHistoryResponse{State: "not_retained"})
		return
	}
	var connectionID uuid.UUID
	var conversationID *uuid.UUID
	var expires time.Time
	var sealed []byte
	err = tx.QueryRow(ctx, `SELECT connection_id,conversation_id,expires_at,encrypted_content FROM workspace_mcp_history WHERE customer_id=$1 AND trace_id=$2`, cid, id).Scan(&connectionID, &conversationID, &expires, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 200, MCPHistoryResponse{State: "not_retained"})
		return
	}
	if err != nil {
		writeError(w, 503, "history unavailable")
		return
	}
	// Lock the current integration in the same transaction as the policy.
	args := []any{cid, connectionID}
	access := ""
	if !RoleFromCtx(ctx).AtLeast(RoleAdmin) {
		args = append(args, userIDFromCtx(ctx))
		access = " AND ((scope='workspace'" + accessSQL(ctx, &args, "mcp", "c.id") + ") OR (scope='personal' AND owner_user_id=$3))"
	}
	var connectionAllowed bool
	err = tx.QueryRow(ctx, `SELECT TRUE FROM workspace_mcp_connections c WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`+access+` FOR SHARE`, args...).Scan(&connectionAllowed)
	if err != nil || !connectionAllowed {
		writeJSON(w, 200, MCPHistoryResponse{State: "restricted"})
		return
	}

	if !p.CaptureAll && !slices.Contains(p.ConnectionIDs, connectionID) {
		writeJSON(w, 200, MCPHistoryResponse{State: "not_retained"})
		return
	}
	if conversationID != nil {
		var stored bool
		err = tx.QueryRow(ctx, `SELECT content_stored FROM workspace_conversations WHERE customer_id=$1 AND id=$2 FOR SHARE`, cid, *conversationID).Scan(&stored)
		if err != nil || !stored {
			writeJSON(w, 200, MCPHistoryResponse{State: "not_retained"})
			return
		}
	}

	if !expires.After(time.Now()) {
		writeJSON(w, 200, MCPHistoryResponse{State: "expired", ExpiresAt: &expires})
		return
	}

	if h.mcpCipher == nil {
		writeError(w, 503, "encrypted history unavailable")
		return
	}
	var encrypted encryption.Encrypted
	if json.Unmarshal(sealed, &encrypted) != nil {
		writeError(w, 503, "history unavailable")
		return
	}
	plain, err := h.mcpCipher.Decrypt(&encrypted, cid.String(), "workspace-mcp-history:"+id.String())
	var content mcpHistoryContent
	if err != nil || json.Unmarshal([]byte(plain), &content) != nil {
		writeError(w, 503, "history unavailable")
		return
	}
	if tx.Commit(ctx) != nil {
		writeError(w, 503, "history unavailable")
		return
	}
	h.audit(r, "mcp.history_viewed", AuditTarget{Type: "trace", ID: id.String()}, nil)
	writeJSON(w, 200, MCPHistoryResponse{State: "sanitized", ExpiresAt: &expires, Content: &content})
}
