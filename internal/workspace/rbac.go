package workspace

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// Role is a workspace member's authorization level. The hierarchy is
// owner > admin > member > viewer. Higher levels inherit lower
// levels' capabilities — an admin can do everything a member can.
type Role string

const (
	// RoleOwner is the workspace creator. There's exactly one per
	// workspace. Owner-only actions: billing, delete-workspace,
	// transfer-ownership.
	RoleOwner Role = "owner"
	// RoleAdmin manages the workspace's configuration. Can invite,
	// remove members, change roles (except promoting to owner —
	// that requires the transfer flow), edit assistants / KB /
	// security policies / domains / budgets, see all data.
	RoleAdmin Role = "admin"
	// RoleMember is the day-to-day chat user. Can send messages,
	// manage their own conversations, see shared assistants and
	// knowledge. Cannot change settings or invite others.
	RoleMember Role = "member"
	// RoleViewer is read-only. See conversations, analytics, traces
	// — for auditors / compliance reviewers / managers who need
	// visibility but shouldn't add data.
	RoleViewer Role = "viewer"
)

// rank assigns each role a comparable weight. Higher = more permissive.
// Used by RequireRole to gate access. Unknown roles get the lowest
// rank so a malformed value can never grant unintended access.
func (r Role) rank() int {
	switch r {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleMember:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// AtLeast returns true when r is r >= min in the role hierarchy.
// Convenience for handlers that want to branch on role rather than
// reject outright (e.g. members see their own conversations,
// admins see everyone's).
func (r Role) AtLeast(min Role) bool {
	return r.rank() >= min.rank()
}

// WithRole stashes the caller's role on the request context. Cloud's
// auth middleware calls this after looking up workspace_members; OSS
// tenant middleware calls it with RoleOwner.
func WithRole(ctx context.Context, role Role) context.Context {
	return context.WithValue(ctx, ctxRole, role)
}

// RoleFromCtx pulls the stashed role. Defaults to RoleViewer (the
// most restrictive role) when nothing is set — fail closed: an
// un-rolled caller gets read-only access at most. The middleware
// wrappers below catch the "no role at all" case and 403; this
// fallback is just defensive belt-and-braces.
func RoleFromCtx(ctx context.Context) Role {
	if v, ok := ctx.Value(ctxRole).(Role); ok && v != "" {
		return v
	}
	return RoleViewer
}

// ConversationAccess is the result of a per-conversation visibility
// check. The handler uses canRead to gate reads (list/get messages
// for one conversation) and canWrite to gate sends/edits/archive.
type ConversationAccess struct {
	Owner    string // user_id of the conversation's owner
	CanRead  bool
	CanWrite bool
}

// resolveConversationAccess applies the per-conversation visibility
// rules. Returns (access, ErrNotFound) when the conversation
// doesn't exist OR the caller can't read it — never leaking
// existence to unauthorized callers (a member can't probe for
// other members' conversation IDs).
//
// Rules:
//   Read: caller is the owner, OR caller's role is admin/viewer/owner
//         (i.e. anything but pure "member"). Members get privacy from
//         peers; privileged roles get governance visibility.
//   Write: caller is the owner. No exceptions — admins do NOT inject
//         into someone else's thread, even for "support". Workspace
//         feels like personal space to members.
func (h *Handler) resolveConversationAccess(
	ctx context.Context,
	conversationID uuid.UUID,
) (*ConversationAccess, error) {
	cid := customerIDFromCtx(ctx)
	callerID := userIDFromCtx(ctx)
	callerRole := RoleFromCtx(ctx)

	owner, err := h.store.ConversationOwner(ctx, cid, conversationID)
	if err != nil {
		return nil, err // ErrNotFound bubbles
	}

	access := &ConversationAccess{Owner: owner}
	access.CanRead = owner == callerID || callerRole != RoleMember
	access.CanWrite = owner == callerID

	if !access.CanRead {
		// Don't leak existence — return ErrNotFound rather than
		// 403'ing with "you can't see this one specifically".
		return nil, ErrNotFound
	}
	return access, nil
}

// requireConversationWrite is the small shorthand handlers use:
// either return a ready-to-use access record or write the right
// HTTP error directly. Returns nil on failure (caller bails).
func (h *Handler) requireConversationWrite(w http.ResponseWriter, r *http.Request, conversationID uuid.UUID) *ConversationAccess {
	access, err := h.resolveConversationAccess(r.Context(), conversationID)
	if err != nil {
		notFoundOr500(w, err)
		return nil
	}
	if !access.CanWrite {
		// CanRead but not CanWrite = admin/viewer trying to mutate
		// someone else's conversation. Honest 403 (we already
		// confirmed they can SEE it; the existence-leak concern
		// doesn't apply).
		writeError(w, http.StatusForbidden,
			"only the conversation owner can modify or send to this thread")
		return nil
	}
	return access
}

// RequireRole returns chi-compatible middleware that 403s when the
// caller's stashed role is below `min`. Use it to gate route
// groups: r.With(RequireRole(RoleAdmin)).Patch("/settings", ...).
//
// Behavior:
//   - If no role is on the context at all, 403 (defensive: any
//     auth-aware caller should have set one — missing role = bug).
//   - If the role's rank < min's rank, 403 with a clear body.
//   - Otherwise pass through.
//
// The 403 body is JSON so the dashboard can surface a useful
// message instead of a generic "request failed".
func RequireRole(min Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := r.Context().Value(ctxRole).(Role)
			if !ok || role == "" {
				writeError(w, http.StatusForbidden,
					"workspace role not resolved for this request")
				return
			}
			if !role.AtLeast(min) {
				writeError(w, http.StatusForbidden,
					"this action requires "+string(min)+" role; you are "+string(role))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
