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

// handleStart is the anonymous wedge entry point. Validates the form,
// provisions a placeholder customer + pending audit + governance
// installation, returns the bundle download + activation URLs, and
// fires off a best-effort confirmation email to the contact.
//
// Rate limiting + body-size cap are responsibilities of the caller's
// chi middleware stack — this handler trusts that the request reached
// it within reasonable bounds.
func (s *Service) handleStart(w http.ResponseWriter, r *http.Request) {
	var body StartRequest
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	body.ContactEmail = strings.ToLower(strings.TrimSpace(body.ContactEmail))
	if !looksLikeEmail(body.ContactEmail) {
		writeJSONErr(w, http.StatusBadRequest, ErrInvalidEmail.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Generate the per-installation HMAC ingest secret here so the
	// bundle the operator downloads embeds the same value the server
	// stores. 32 random bytes URL-safe base64-encoded.
	ingestKey, err := generateRawToken(32)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "ingest key: "+err.Error())
		return
	}

	res, err := s.store.Provision(ctx, body, s.cfg, ingestKey)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "provision: "+err.Error())
		return
	}

	bundleURL := s.cfg.PublicBaseURL + "/v1/audit/" + res.AuditID.String() +
		"/bundle.zip?token=" + res.BundleToken
	activationURL := s.cfg.PublicBaseURL + "/activate?token=" + res.ClaimToken

	s.sendAuditStartedEmailAsync(body, bundleURL, activationURL, res.ExpiresAt)

	writeJSON(w, http.StatusCreated, StartResponse{
		AuditID:           res.AuditID,
		ActivationURL:     activationURL,
		BundleDownloadURL: bundleURL,
		ExpiresAt:         res.ExpiresAt,
	})
}

// sendAuditStartedEmailAsync dispatches the welcome email in a
// detached goroutine. SendGrid latency must never block the audit
// start response. Failures are logged at WARN; the activation +
// bundle URLs are also in the JSON response so a non-emailed prospect
// still has the links if they kept the page open.
func (s *Service) sendAuditStartedEmailAsync(req StartRequest, bundleURL, activationURL string, expires time.Time) {
	if s.emailer == nil || req.ContactEmail == "" {
		return
	}
	to := req.ContactEmail
	name := req.ContactName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg := email.AuditStarted(name, bundleURL, activationURL, expires)
		msg.To = to
		if err := s.emailer.Send(ctx, msg); err != nil {
			slog.Warn("audit: started email failed",
				"to", to, "error", err)
			return
		}
		slog.Info("audit: started email sent", "to", to)
	}()
}

// looksLikeEmail is a deliberately-loose check — RFC-perfect email
// validation is famously hopeless. Trim, require an @ with content
// before and after, period before TLD. SendGrid validates the rest.
func looksLikeEmail(s string) bool {
	if len(s) < 5 || len(s) > 254 {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at < 1 || at >= len(s)-3 {
		return false
	}
	if !strings.Contains(s[at:], ".") {
		return false
	}
	return true
}

// =============================================================================
// JSON helpers
// =============================================================================

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

