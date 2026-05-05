package workspace

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/pkg/email"
)

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListMembers(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": rows})
}

// SetMemberBudgetsRequest is the PATCH body for setting per-user
// caps. Both fields are *int — nil = leave alone, non-nil with 0 or
// positive value = set, with explicit 0 meaning "no calls allowed".
//
// To CLEAR a previously-set cap, pass a JSON null (decodes to a nil
// pointer in Go). The API doesn't distinguish "leave alone" from
// "clear" via separate verbs — clients use null for clear.
type SetMemberBudgetsRequest struct {
	MonthlyTokenLimit *int `json:"monthly_token_limit"`
	DailyRateLimit    *int `json:"daily_rate_limit"`
}

func (h *Handler) setMemberBudgets(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userID is required")
		return
	}
	var body SetMemberBudgetsRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	cid := customerIDFromCtx(r.Context())
	if err := h.store.SetMemberBudgets(r.Context(), cid, userID,
		body.MonthlyTokenLimit, body.DailyRateLimit); err != nil {
		notFoundOr500(w, err)
		return
	}
	targetEmail := lookupMemberEmail(r.Context(), h.store, cid, userID)
	h.audit(r, "member.budgets_changed",
		AuditTarget{Type: "member", ID: userID, Label: targetEmail},
		map[string]any{
			"monthly_token_limit": body.MonthlyTokenLimit,
			"daily_rate_limit":    body.DailyRateLimit,
		})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userID is required")
		return
	}
	cid := customerIDFromCtx(r.Context())
	// Snapshot email + role BEFORE the delete — once the row's gone
	// the audit can only show "unknown user".
	targetEmail := lookupMemberEmail(r.Context(), h.store, cid, userID)
	prevRole, _ := h.store.GetMemberRole(r.Context(), cid, userID)
	if err := h.store.RemoveMember(r.Context(), cid, userID); err != nil {
		notFoundOr500(w, err)
		return
	}
	h.audit(r, "member.removed",
		AuditTarget{Type: "member", ID: userID, Label: targetEmail},
		map[string]any{"role_at_removal": prevRole})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listInvitations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListInvitations(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": rows})
}

// CreateInvitationRequest is the POST body for /invitations.
type CreateInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// CreateInvitationResponse returns the bearer token exactly once. Storing
// only the SHA-256 hash means the token cannot be retrieved later — the
// dashboard must surface it immediately to the inviter.
type CreateInvitationResponse struct {
	Invitation *Invitation `json:"invitation"`
	Token      string      `json:"token"`
}

func (h *Handler) createInvitation(w http.ResponseWriter, r *http.Request) {
	var body CreateInvitationRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	if body.Email == "" || !strings.Contains(body.Email, "@") {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if !validRole(body.Role) {
		writeError(w, http.StatusBadRequest, "role must be one of owner, admin, member, viewer")
		return
	}

	// Hard seat enforcement. workspace_settings.seat_limit is owned by
	// the cloud billing webhook (mirrors Stripe subscription seats);
	// OSS-self-hosted operators can edit the row directly. Counting
	// pending invites alongside active members prevents the inviter
	// from queuing N over-cap invitations and having them all accepted
	// later, which would land the workspace silently above the cap.
	consumed, limit, err := h.store.CountConsumedSeats(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if consumed >= limit {
		writeStructuredError(w, http.StatusPaymentRequired,
			"seat_limit_reached",
			"seat limit reached — upgrade your subscription to invite more members",
			map[string]any{"consumed": consumed, "limit": limit})
		return
	}

	token, err := generateBearerToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inviter := userEmailFromCtx(r.Context())
	var invitedBy *string
	if inviter != "" {
		invitedBy = &inviter
	}

	inv, err := h.store.CreateInvitation(
		r.Context(),
		customerIDFromCtx(r.Context()),
		body.Email,
		body.Role,
		invitedBy,
		7*24*time.Hour,
		token,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.sendInvitationEmailAsync(customerIDFromCtx(r.Context()), inv, inviter, token)
	h.audit(r, "invitation.created",
		AuditTarget{Type: "invitation", ID: inv.ID.String(), Label: inv.Email},
		map[string]any{"role": inv.Role, "expires_at": inv.ExpiresAt})
	writeJSON(w, http.StatusCreated, CreateInvitationResponse{Invitation: inv, Token: token})
}

// sendInvitationEmailAsync fires the workspace-invitation email in a
// detached goroutine. Failures are logged at WARN; the bearer token
// also lives in the createInvitation response, so the inviter can hand
// it to the recipient manually if SendGrid drops the message. Skips
// silently when no email client or public base URL is configured —
// OSS-default and self-hosters without SendGrid still get a useful
// response, they just don't get automatic email delivery.
func (h *Handler) sendInvitationEmailAsync(customerID uuid.UUID, inv *Invitation, inviter, rawToken string) {
	base := h.inviteAcceptBaseURL()
	if h.emailer == nil || base == "" {
		return
	}
	to := inv.Email
	role := inv.Role
	expires := inv.ExpiresAt
	acceptURL := base + "/accept-invite?token=" + rawToken

	// Resolve a friendly company name for the email subject. Fall back
	// gracefully — the email is still useful without it.
	companyName := h.lookupCompanyName(customerID)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg := email.WorkspaceInvitation("", inviter, companyName, role, acceptURL, expires)
		msg.To = to
		if err := h.emailer.Send(ctx, msg); err != nil {
			slog.Warn("workspace: invitation email failed",
				"to", to, "error", err)
			return
		}
		slog.Info("workspace: invitation email sent", "to", to)
	}()
}

// lookupCompanyName returns the customer's display name for use in
// invitation email subject + body. Best-effort; an empty string is
// fine — the email template substitutes "a Bastio workspace".
func (h *Handler) lookupCompanyName(customerID uuid.UUID) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var name string
	if err := h.store.pool.QueryRow(ctx,
		`SELECT name FROM customers WHERE id = $1`, customerID).Scan(&name); err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

// AcceptInvitationRequest is the POST body for /invitations/accept.
// The token is the bearer secret emailed to the invited address.
type AcceptInvitationRequest struct {
	Token string `json:"token"`
}

// acceptInvitation lets a signed-in user redeem a bearer-token
// invitation. The endpoint is intentionally not customer-scoped — the
// invitation row carries the inviter's customer_id and the handler
// binds the user there, regardless of which customer the user's
// session currently sits in. (Cloud lets a user belong to multiple
// customers' workspaces; their session's "current" customer is
// switched separately.)
//
// The session must be authenticated. In OSS-default mode without auth
// middleware the default user/email apply, which means OSS dev can
// only accept invitations issued to the default-user email — that's
// fine; multi-tenant invitations are a cloud-shaped feature.
func (h *Handler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var body AcceptInvitationRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	body.Token = strings.TrimSpace(body.Token)
	if body.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	userID := userIDFromCtx(r.Context())
	userEmail := userEmailFromCtx(r.Context())
	if userID == "" || userEmail == "" {
		// Auth middleware should always populate both. Fail loudly
		// rather than binding an empty user_id into workspace_members.
		writeError(w, http.StatusUnauthorized, "must be signed in to accept invitation")
		return
	}

	res, err := h.store.AcceptInvitation(r.Context(), body.Token, userID, userEmail)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeStructuredError(w, http.StatusGone,
				"invitation_not_found",
				"invitation link is invalid or expired", nil)
		case errors.Is(err, ErrInvitationExpired):
			writeStructuredError(w, http.StatusGone,
				"invitation_expired",
				"invitation link has expired — ask the workspace owner to send a new one", nil)
		case errors.Is(err, ErrInvitationRevoked):
			writeStructuredError(w, http.StatusGone,
				"invitation_revoked",
				"invitation has been revoked", nil)
		case errors.Is(err, ErrInvitationConsumed):
			writeStructuredError(w, http.StatusGone,
				"invitation_consumed",
				"invitation has already been accepted", nil)
		case errors.Is(err, ErrInvitationEmailMismatch):
			writeStructuredError(w, http.StatusForbidden,
				"invitation_email_mismatch",
				"this invitation was issued to a different email address", nil)
		case errors.Is(err, ErrSeatLimitReached):
			writeStructuredError(w, http.StatusPaymentRequired,
				"seat_limit_reached",
				"the workspace is at its seat limit — ask the owner to upgrade", nil)
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	h.audit(r, "invitation.accepted",
		AuditTarget{Type: "member", ID: userIDFromCtx(r.Context()), Label: res.Email},
		map[string]any{"role": res.Role})
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.RevokeInvitation(r.Context(), customerIDFromCtx(r.Context()), id); err != nil {
		notFoundOr500(w, err)
		return
	}
	h.audit(r, "invitation.revoked",
		AuditTarget{Type: "invitation", ID: id.String(), Label: ""},
		nil)
	w.WriteHeader(http.StatusNoContent)
}

func validRole(s string) bool {
	switch s {
	case "owner", "admin", "member", "viewer":
		return true
	}
	return false
}

func generateBearerToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
