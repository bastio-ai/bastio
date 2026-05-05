package workspace

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// whoami answers "who am I in this workspace and what can I do?".
// The dashboard reads this on first load to know which admin
// affordances to render. Including the role string + a few
// derived booleans so the frontend doesn't re-implement the
// hierarchy. user_id and email come along so the user-detail
// drawer / "you" labels in the team table can match without
// extra round trips.
func (h *Handler) whoami(w http.ResponseWriter, r *http.Request) {
	role := RoleFromCtx(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":    userIDFromCtx(r.Context()),
		"email":      userEmailFromCtx(r.Context()),
		"role":       string(role),
		"can_admin":  role.AtLeast(RoleAdmin),
		"can_send":   role.AtLeast(RoleMember),
		"is_owner":   role == RoleOwner,
	})
}

// ChangeMemberRoleRequest is the PATCH body for
// /v1/workspace/members/{userID}/role.
//
// `role` accepts admin / member / viewer. Promoting to "owner"
// requires the dedicated transfer flow (see transferOwnership).
type ChangeMemberRoleRequest struct {
	Role string `json:"role"`
}

func (h *Handler) changeMemberRole(w http.ResponseWriter, r *http.Request) {
	targetUserID := strings.TrimSpace(chi.URLParam(r, "userID"))
	if targetUserID == "" {
		writeError(w, http.StatusBadRequest, "userID is required")
		return
	}
	var body ChangeMemberRoleRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	next := strings.TrimSpace(body.Role)
	switch next {
	case "admin", "member", "viewer":
		// ok
	case "owner":
		// Promoting to owner can't happen here — every workspace has
		// exactly one. Use POST /owner/transfer.
		writeError(w, http.StatusBadRequest,
			"to assign owner, use POST /v1/workspace/owner/transfer")
		return
	default:
		writeError(w, http.StatusBadRequest, "role must be admin, member, or viewer")
		return
	}

	cid := customerIDFromCtx(r.Context())

	// Guard: don't let an admin demote the owner. Only the owner
	// themselves can give up ownership (via transfer).
	current, err := h.store.GetMemberRole(r.Context(), cid, targetUserID)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	if current == "owner" {
		writeError(w, http.StatusForbidden,
			"cannot change the owner's role — owner must transfer ownership first")
		return
	}

	if err := h.store.SetMemberRole(r.Context(), cid, targetUserID, next); err != nil {
		notFoundOr500(w, err)
		return
	}
	// Snapshot the target's email for the audit row so a later
	// removal of the user (or email change) doesn't leave the audit
	// pointing at "unknown user".
	targetEmail := lookupMemberEmail(r.Context(), h.store, cid, targetUserID)
	h.audit(r, "member.role_changed",
		AuditTarget{Type: "member", ID: targetUserID, Label: targetEmail},
		map[string]any{"old_role": current, "new_role": next})
	w.WriteHeader(http.StatusNoContent)
}

// TransferOwnershipRequest is the body for POST /v1/workspace/owner/transfer.
//
// The current owner picks an existing member (any role) to promote.
// On success the previous owner becomes admin and the new owner is
// the only role='owner' row in the workspace.
type TransferOwnershipRequest struct {
	NewOwnerUserID string `json:"new_owner_user_id"`
}

func (h *Handler) transferOwnership(w http.ResponseWriter, r *http.Request) {
	var body TransferOwnershipRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	next := strings.TrimSpace(body.NewOwnerUserID)
	if next == "" {
		writeError(w, http.StatusBadRequest, "new_owner_user_id is required")
		return
	}
	caller := userIDFromCtx(r.Context())
	if caller == next {
		writeError(w, http.StatusBadRequest, "you are already the owner")
		return
	}
	if caller == "" {
		writeError(w, http.StatusInternalServerError, "caller user id missing from context")
		return
	}
	cid := customerIDFromCtx(r.Context())
	if err := h.store.TransferOwnership(r.Context(), cid, caller, next); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "target user is not a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	newOwnerEmail := lookupMemberEmail(r.Context(), h.store, cid, next)
	h.audit(r, "owner.transferred",
		AuditTarget{Type: "member", ID: next, Label: newOwnerEmail},
		map[string]any{"previous_owner_user_id": caller})
	w.WriteHeader(http.StatusNoContent)
}
