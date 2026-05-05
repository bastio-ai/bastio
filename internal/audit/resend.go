package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/pkg/email"
)

// handleResend re-emits a Shadow AI Audit activation URL when a
// prospect has lost the original email. Always returns 200 with the
// same neutral body — whether the email is unknown, the audit is
// already claimed, or the cooldown is active. Information leakage about
// which addresses have pending audits is the single biggest threat
// here; everything is fielded behind the neutral response.
//
// The flow:
//
//  1. Decode + validate the email shape.
//  2. Ask the store to rotate the claim token (returns nil for unknown
//     email, claimed audit, expired audit, or cooldown active).
//  3. If rotated: dispatch the resend email asynchronously and log.
//  4. Always: return {"status":"ok"} with 200.
//
// Rate limiting at the chi router level remains the first line of
// defense against bulk enumeration; the per-audit cooldown is the
// per-target defense.
func (s *Service) handleResend(w http.ResponseWriter, r *http.Request) {
	var body ResendRequest
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		// Invalid request body is the one case we surface — it's a
		// developer mistake, not a probe vector. Email enumeration
		// hides behind 200s further down.
		writeJSONErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	body.ContactEmail = strings.ToLower(strings.TrimSpace(body.ContactEmail))
	if !looksLikeEmail(body.ContactEmail) {
		// Same neutral 200. A junk email isn't enumeration but we
		// don't want client code branching on bad-email-vs-no-audit.
		writeJSON(w, http.StatusOK, ResendResponse{Status: "ok"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	res, err := s.store.RotateClaimTokenByEmail(ctx, body.ContactEmail, s.cfg.ClaimTokenTTL)
	if err != nil {
		// Internal error path — log it but still return neutral 200.
		// The prospect will retry; the operator sees the log.
		slog.Error("audit: resend rotate failed",
			"contact_email", body.ContactEmail, "error", err)
		writeJSON(w, http.StatusOK, ResendResponse{Status: "ok"})
		return
	}
	if res == nil {
		// Unknown email, claimed audit, expired audit, or cooldown.
		// Indistinguishable to the caller by design.
		writeJSON(w, http.StatusOK, ResendResponse{Status: "ok"})
		return
	}

	activationURL := s.cfg.PublicBaseURL + "/activate?token=" + res.ClaimToken
	s.sendResendEmailAsync(body.ContactEmail, res.ContactName, activationURL)

	writeJSON(w, http.StatusOK, ResendResponse{Status: "ok"})
}

// sendResendEmailAsync detaches the SendGrid call from the handler
// response — same pattern as sendAuditStartedEmailAsync. Failures are
// logged at WARN; the prospect can retry resend on the cooldown
// boundary if the email never arrives.
func (s *Service) sendResendEmailAsync(toEmail, toName, activationURL string) {
	if s.emailer == nil {
		// No email client configured — operators self-hosting without
		// SENDGRID_API_KEY won't fire resends. The neutral-200 contract
		// holds; we just don't send anything. Log so the operator sees
		// the rotation happened.
		slog.Info("audit: resend rotated (no email client)",
			"to", toEmail)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg := email.AuditResend(toName, activationURL)
		msg.To = toEmail
		if err := s.emailer.Send(ctx, msg); err != nil {
			slog.Warn("audit: resend email failed",
				"to", toEmail, "error", err)
			return
		}
		slog.Info("audit: resend email sent", "to", toEmail)
	}()
}
