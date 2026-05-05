package workspace

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// AuditEntry is one durable row in workspace_audit_log. Snapshot
// fields (actor_email, actor_role, target_label) are captured at
// write time and never re-resolved — the audit log is a historical
// record, not a join into the current state. See migration 028 for
// the rationale.
type AuditEntry struct {
	ID           uuid.UUID       `json:"id"`
	CustomerID   uuid.UUID       `json:"customer_id"`
	ActorUserID  string          `json:"actor_user_id"`
	ActorEmail   string          `json:"actor_email"`
	ActorRole    string          `json:"actor_role"`
	Action       string          `json:"action"`
	TargetType   string          `json:"target_type"`
	TargetID     string          `json:"target_id"`
	TargetLabel  string          `json:"target_label"`
	Metadata     json.RawMessage `json:"metadata"`
	IPAddress    string          `json:"ip_address"`
	UserAgent    string          `json:"user_agent"`
	CreatedAt    time.Time       `json:"created_at"`
}

// lookupMemberEmail fetches a member's email for an audit snapshot.
// Returns "" on miss so audit-write doesn't fail just because the
// target was already removed.
func lookupMemberEmail(ctx context.Context, store *Store, customerID uuid.UUID, userID string) string {
	if store == nil {
		return ""
	}
	const q = `SELECT email FROM workspace_members
WHERE customer_id = $1 AND user_id = $2 LIMIT 1`
	var email string
	_ = store.pool.QueryRow(ctx, q, customerID, userID).Scan(&email)
	return email
}

// listAudit handles GET /v1/workspace/audit. Admin-only (route is
// gated in handler.go). Returns reverse-chronological audit rows
// for this workspace, capped at 50 by default. Paginate with
// ?before=<audit_id> + ?action=<filter>.
func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	limit := intQuery(r, "limit", 50)
	action := r.URL.Query().Get("action")

	var before uuid.UUID
	if v := r.URL.Query().Get("before"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			before = id
		}
	}

	rows, err := h.store.ListAudit(r.Context(), cid, action, before, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": rows})
}

// AuditTarget describes the thing an action acted on. Empty fields
// are fine — some actions (settings.updated) operate on the
// workspace itself and have no specific target.
type AuditTarget struct {
	Type  string // "member" | "assistant" | "knowledge" | "domain" | "invitation" | "settings" | "slug" | "conversation"
	ID    string // user_id, uuid, slug name, etc.
	Label string // snapshot — email, assistant name, etc.
}

// auditCrossUserRead writes a privacy audit row when a privileged
// caller (admin/viewer/owner) reads a conversation that belongs to
// someone else. No-ops when the caller is the owner — same-user
// reads are not interesting and would flood the log.
//
// We snapshot the *owner's* email as target_label so the audit row
// reads "alice viewed bob@acme.com's thread" even after bob is
// removed from the workspace. Best-effort lookup; empty label is
// still useful (target_id is the conversation UUID).
func (h *Handler) auditCrossUserRead(r *http.Request, action string, conversationID uuid.UUID, ownerUserID string) {
	callerID := userIDFromCtx(r.Context())
	if callerID == ownerUserID {
		return
	}
	cid := customerIDFromCtx(r.Context())
	ownerEmail := lookupMemberEmail(r.Context(), h.store, cid, ownerUserID)
	h.audit(r, action, AuditTarget{
		Type:  "conversation",
		ID:    conversationID.String(),
		Label: ownerEmail,
	}, map[string]any{
		"owner_user_id": ownerUserID,
	})
}

// audit writes one audit row. Best-effort: logs and swallows on
// error so an audit failure can't 500 the actual user-visible
// action. (We'd rather lose an audit row than break a removal
// flow.) Pulls actor identity + IP from the request context.
//
// metadata can be nil — empty JSON object is persisted.
func (h *Handler) audit(r *http.Request, action string, target AuditTarget, metadata map[string]any) {
	if h.store == nil {
		return
	}
	cid := customerIDFromCtx(r.Context())
	actorID := userIDFromCtx(r.Context())
	actorEmail := userEmailFromCtx(r.Context())
	actorRole := string(RoleFromCtx(r.Context()))

	var metaJSON json.RawMessage
	if metadata == nil {
		metaJSON = json.RawMessage(`{}`)
	} else {
		b, err := json.Marshal(metadata)
		if err != nil {
			b = []byte(`{}`)
		}
		metaJSON = b
	}

	entry := AuditEntry{
		CustomerID:  cid,
		ActorUserID: actorID,
		ActorEmail:  actorEmail,
		ActorRole:   actorRole,
		Action:      action,
		TargetType:  target.Type,
		TargetID:    target.ID,
		TargetLabel: target.Label,
		Metadata:    metaJSON,
		IPAddress:   r.RemoteAddr,
		UserAgent:   r.UserAgent(),
	}
	if err := h.store.WriteAudit(r.Context(), entry); err != nil {
		// Audit-write failures are noisy in logs but never fail
		// the originating request. They're a security signal worth
		// surfacing — broken audit + active mutations is exactly
		// the situation a malicious actor would exploit, so make
		// this log line obvious.
		slog.Error("workspace audit write failed",
			"customer_id", cid,
			"action", action,
			"actor_user_id", actorID,
			"error", err,
		)
	}
}
