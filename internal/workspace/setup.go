package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type SetupStep struct {
	ID       string `json:"id"`
	Complete bool   `json:"complete"`
	Detail   string `json:"detail"`
}
type WorkspaceSetupStatus struct {
	MultiUser             bool        `json:"multi_user"`
	Steps                 []SetupStep `json:"steps"`
	Completed             int         `json:"completed"`
	Total                 int         `json:"total"`
	Ready                 bool        `json:"ready"`
	Members               int         `json:"members"`
	PendingInvitations    int         `json:"pending_invitations"`
	ReadyKnowledge        int         `json:"ready_knowledge"`
	EmployeeConversations int         `json:"employee_conversations"`
}
type privacyReviewInput struct {
	StoreConversationContent bool `json:"store_conversation_content"`
	RetentionDays            int  `json:"retention_days"`
	SpendCapCents            *int `json:"spend_cap_cents"`
}

func privacyReview(st *Settings) privacyReviewInput {
	return privacyReviewInput{st.StoreConversationContent, st.RetentionDays, st.SpendCapCents}
}
func privacyFingerprint(value privacyReviewInput) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (h *Handler) reviewWorkspacePrivacy(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var input privacyReviewInput
	if decodeMCP(r, &input) != nil {
		writeError(w, 400, "invalid privacy review")
		return
	}
	cid := customerIDFromCtx(r.Context())
	st, err := h.store.GetSettings(r.Context(), cid)
	if err != nil {
		writeError(w, 503, "workspace policy unavailable")
		return
	}
	if privacyFingerprint(input) != privacyFingerprint(privacyReview(st)) {
		writeError(w, 409, "Privacy or limits changed. Refresh and review the current settings.")
		return
	}
	_, err = h.store.pool.Exec(r.Context(), `INSERT INTO workspace_setup_review(customer_id,privacy_fingerprint) VALUES($1,$2) ON CONFLICT(customer_id) DO UPDATE SET privacy_fingerprint=excluded.privacy_fingerprint,reviewed_at=now()`, cid, privacyFingerprint(input))
	if err != nil {
		writeError(w, 503, "review could not be saved")
		return
	}
	h.audit(r, "setup.privacy_reviewed", AuditTarget{Type: "settings", ID: cid.String()}, nil)
	writeJSON(w, 200, map[string]bool{"reviewed": true})
}
func (h *Handler) workspaceSetupStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cid := r.Context(), customerIDFromCtx(r.Context())
	st, err := h.store.EnsureSettings(ctx, cid)
	if err != nil {
		writeError(w, 503, "workspace setup unavailable")
		return
	}
	checked := false
	allowed := st.AllowedModels
	for _, name := range workspaceProviders {
		key, configured := h.configuredWorkspaceProvider(ctx, cid, name)
		if !configured {
			continue
		}
		rows, e := h.store.pool.Query(ctx, `SELECT model FROM workspace_model_checks WHERE customer_id=$1 AND provider=$2 AND credential_fingerprint=$3 AND success AND checked_at>now()-interval '7 days'`, cid, name, credentialFingerprint(cid, name, key))
		if e != nil {
			writeError(w, 503, "model readiness unavailable")
			return
		}
		for rows.Next() {
			var model string
			if rows.Scan(&model) != nil {
				continue
			}

			for _, entry := range allowed {
				if entry.Provider == name && entry.Model == model {
					checked = true
				}
			}
		}
		rows.Close()
	}
	var privacyReviewed bool
	err = h.store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_setup_review WHERE customer_id=$1 AND privacy_fingerprint=$2)`, cid, privacyFingerprint(privacyReview(st))).Scan(&privacyReviewed)
	if err != nil {
		writeError(w, 503, "privacy review unavailable")
		return
	}
	var multiUser bool
	if err = h.store.pool.QueryRow(ctx, `SELECT to_regclass('workspace_members') IS NOT NULL AND to_regclass('workspace_invitations') IS NOT NULL`).Scan(&multiUser); err != nil {
		writeError(w, 503, "workspace setup unavailable")
		return
	}
	if !multiUser {
		h.singleTenantSetup(w, r, cid, checked, privacyReviewed)
		return
	}
	var members, invites, groups, assistants, knowledge, chats int
	err = h.store.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM workspace_members WHERE customer_id=$1 AND role='member'),
 (SELECT count(*) FROM workspace_invitations WHERE customer_id=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now()),
 (SELECT count(*) FROM workspace_access_groups g WHERE customer_id=$1 AND EXISTS(SELECT 1 FROM workspace_access_group_members m WHERE m.customer_id=$1 AND m.group_id=g.id AND EXISTS(SELECT 1 FROM workspace_members member WHERE member.customer_id=$1 AND member.user_id=m.user_id AND member.role='member'))),
 (SELECT count(*) FROM workspace_assistants a WHERE customer_id=$1 AND archived_at IS NULL),
 (SELECT count(*) FROM workspace_knowledge_sources WHERE customer_id=$1 AND status='ready' AND archived_at IS NULL),
 (SELECT count(DISTINCT c.id) FROM workspace_conversations c JOIN workspace_messages m ON m.customer_id=$1 AND m.conversation_id=c.id JOIN workspace_members member ON member.customer_id=$1 AND member.user_id=c.user_id WHERE c.customer_id=$1 AND NOT c.is_setup_test AND member.role='member' AND m.role='assistant' AND m.error IS NULL AND m.finish_reason NOT IN ('blocked','budget_exceeded','security_scan_unavailable','error'))`, cid).Scan(&members, &invites, &groups, &assistants, &knowledge, &chats)
	if err != nil {
		writeError(w, 503, "workspace setup evidence unavailable")
		return
	}
	var employeeIDs []string
	memberRows, e := h.store.pool.Query(ctx, `SELECT user_id FROM workspace_members WHERE customer_id=$1 AND role='member'`, cid)
	if e != nil {
		writeError(w, 503, "member permissions unavailable")
		return
	}
	for memberRows.Next() {
		var uid string
		if memberRows.Scan(&uid) == nil {
			employeeIDs = append(employeeIDs, uid)
		}
	}
	memberRows.Close()
	sharedAssistant := false
	for _, uid := range employeeIDs {
		employeeCtx := withWorkspaceUser(WithRole(ctx, RoleMember), uid)
		visible, e := h.store.ListAssistants(employeeCtx, cid)
		if e != nil {
			writeError(w, 503, "assistant sharing unavailable")
			return
		}
		for _, assistant := range visible {
			ok, e := h.store.canAccess(employeeCtx, cid, assistant.ID, "assistant")
			if e != nil {
				writeError(w, 503, "assistant sharing unavailable")
				return
			}
			if ok {
				sharedAssistant = true
				break
			}
		}
	}
	out := WorkspaceSetupStatus{MultiUser: true, Members: members, PendingInvitations: invites, ReadyKnowledge: knowledge, EmployeeConversations: chats, Steps: []SetupStep{
		{"models", checked, "Test an approved model with your current provider key. Checks expire after seven days and reset when credentials change."},
		{"privacy", privacyReviewed, "Review conversation storage, retention and spending limits."},
		{"team", members > 0, "Invite employees. Pending invitations do not count as joined members."},
		{"groups", groups > 0, "Organize joined employees into groups such as Legal or Marketing."},
		{"assistant", assistants > 0 && sharedAssistant, "Create an assistant, attach approved knowledge, and review who can use it."},
		{"preview", chats > 0, "Preview an employee’s access, then let them send their first message."},
	}}
	for _, step := range out.Steps {
		if step.Complete {
			out.Completed++
		}
	}
	out.Total = len(out.Steps)
	out.Ready = out.Completed == out.Total
	writeJSON(w, 200, out)
}
func (h *Handler) previewWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	cid, uid := customerIDFromCtx(r.Context()), r.URL.Query().Get("user_id")
	var role Role
	var email string
	if uid == "" || len(uid) > 256 {
		writeError(w, 400, "choose a workspace member")
		return
	}
	if err := h.store.pool.QueryRow(r.Context(), `SELECT role,email FROM workspace_members WHERE customer_id=$1 AND user_id=$2`, cid, uid).Scan(&role, &email); err != nil {
		writeError(w, 404, "workspace member not found")
		return
	}
	// Preview is read-only and uses the recipient's actual role and groups;
	// it never creates a session or grants the admin that person's credentials.
	ctx := WithRole(r.Context(), role)
	ctx = withWorkspaceUser(ctx, uid)
	assistants, err := h.store.ListAssistants(ctx, cid)
	if err != nil {
		writeError(w, 503, "assistant permissions unavailable")
		return
	}
	knowledge, err := h.store.ListKnowledgeSources(ctx, cid)
	if err != nil {
		writeError(w, 503, "knowledge permissions unavailable")
		return
	}
	allowed, err := h.store.EffectiveAllowedModels(ctx, cid, uid)
	if err != nil {
		writeError(w, 503, "model policy unavailable")
		return
	}
	type item struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	bots, documents, integrations := []item{}, []item{}, []item{}
	for _, a := range assistants {
		ok, e := h.store.canAccess(ctx, cid, a.ID, "assistant")
		if e != nil {
			writeError(w, 503, "assistant permissions unavailable")
			return
		}
		if ok {
			bots = append(bots, item{a.ID, a.Name})
		}
	}
	for _, k := range knowledge {
		ok, e := h.store.canAccess(ctx, cid, k.ID, "knowledge")
		if e != nil {
			writeError(w, 503, "knowledge permissions unavailable")
			return
		}
		if ok {
			documents = append(documents, item{k.ID, k.Name})
		}
	}
	args := []any{cid}
	filter := accessSQL(ctx, &args, "mcp", "c.id")
	rows, err := h.store.pool.Query(ctx, `SELECT id,name FROM workspace_mcp_connections c WHERE customer_id=$1 AND revoked_at IS NULL AND scope='workspace'`+filter+` ORDER BY name`, args...)
	if err != nil {
		writeError(w, 503, "integration permissions unavailable")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var entry item
		if rows.Scan(&entry.ID, &entry.Name) != nil {
			writeError(w, 503, "integration permissions unavailable")
			return
		}
		integrations = append(integrations, entry)
	}
	st, err := h.store.GetSettings(ctx, cid)
	if err != nil {
		writeError(w, 503, "privacy policy unavailable")
		return
	}
	h.audit(r, "setup.member_previewed", AuditTarget{Type: "member", ID: uid}, nil)
	writeJSON(w, 200, struct {
		UserID                   string         `json:"user_id"`
		Email                    string         `json:"email"`
		Role                     Role           `json:"role"`
		Assistants               []item         `json:"assistants"`
		Knowledge                []item         `json:"knowledge"`
		Integrations             []item         `json:"integrations"`
		AllowedModels            []AllowedModel `json:"allowed_models"`
		StoreConversationContent bool           `json:"store_conversation_content"`
		PreviewedAt              time.Time      `json:"previewed_at"`
	}{uid, email, role, bots, documents, integrations, allowed, st.StoreConversationContent, time.Now().UTC()})
}

func withWorkspaceUser(ctx context.Context, uid string) context.Context {
	return context.WithValue(ctx, ctxUserID, uid)
}

func (h *Handler) singleTenantSetup(w http.ResponseWriter, r *http.Request, cid uuid.UUID, checked, reviewed bool) {
	var assistants, knowledge, chats int
	err := h.store.pool.QueryRow(r.Context(), `SELECT (SELECT count(*) FROM workspace_assistants WHERE customer_id=$1 AND archived_at IS NULL),(SELECT count(*) FROM workspace_knowledge_sources WHERE customer_id=$1 AND status='ready' AND archived_at IS NULL),(SELECT count(*) FROM workspace_messages m JOIN workspace_conversations c ON c.id=m.conversation_id AND c.customer_id=$1 WHERE m.customer_id=$1 AND NOT c.is_setup_test AND m.role='assistant' AND m.error IS NULL AND m.finish_reason NOT IN ('blocked','error','budget_exceeded','security_scan_unavailable'))`, cid).Scan(&assistants, &knowledge, &chats)
	if err != nil {
		writeError(w, 503, "setup evidence unavailable")
		return
	}
	out := WorkspaceSetupStatus{ReadyKnowledge: knowledge, EmployeeConversations: chats, Steps: []SetupStep{{"models", checked, "Test and approve a model using the current provider credential."}, {"privacy", reviewed, "Review conversation storage, retention and spending limits."}, {"assistant", assistants > 0, "Create an assistant and attach approved knowledge."}, {"preview", chats > 0, "Open your workspace and receive your first protected answer."}}}
	for _, step := range out.Steps {
		if step.Complete {
			out.Completed++
		}
	}
	out.Total = len(out.Steps)
	out.Ready = out.Completed == out.Total
	writeJSON(w, 200, out)
}
