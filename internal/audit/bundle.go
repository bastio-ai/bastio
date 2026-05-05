package audit

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// handleBundleDownload returns the MDM bundle ZIP for a one-shot
// download token. Three checks before producing bytes:
//
//  1. Token resolves to a known pending audit.
//  2. Token hasn't been used (bundle_used_at IS NULL).
//  3. Audit isn't expired.
//
// On success, mark the audit's bundle_used_at and stream the bundle.
// On any failure, return 410 Gone — the client treats every failure
// the same (the link doesn't work) and we don't leak whether the token
// was ever issued.
func (s *Service) handleBundleDownload(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	auditID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid audit id", http.StatusBadRequest)
		return
	}

	rawToken := r.URL.Query().Get("token")
	if rawToken == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	pending, err := s.store.PendingAuditByBundleToken(r.Context(), rawToken)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "link expired or invalid", http.StatusGone)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if pending.ID != auditID {
		// Token doesn't match the audit ID in the URL — treat as
		// invalid. (Don't expose a different audit's bundle.)
		http.Error(w, "link expired or invalid", http.StatusGone)
		return
	}
	if pending.BundleUsedAt != nil {
		http.Error(w, "link expired or invalid", http.StatusGone)
		return
	}
	if time.Now().After(pending.ExpiresAt) {
		http.Error(w, "link expired or invalid", http.StatusGone)
		return
	}

	// Mark consumed BEFORE streaming. If the actual write fails the
	// row stays consumed — slightly worse than a retry-friendly state
	// but vastly better than letting an attacker probe the bundle for
	// info (timing-side-channel via partial download).
	ok, err := s.store.MarkBundleDownloaded(r.Context(), pending.ID)
	if err != nil || !ok {
		http.Error(w, "link expired or invalid", http.StatusGone)
		return
	}

	// Bundle generation is delegated to governance.Handler. We pass
	// the placeholder customer's id; the existing bundle generator
	// builds the MDM artifacts scoped to that customer's installation.
	// PublicBaseURL is threaded through so self-hosted operators get
	// the correct event-ingestion URL embedded in MDM templates.
	bundle, contentType, err := s.gov.BuildAnonymousMDMBundle(
		r.Context(),
		pending.CustomerID,
		pending.MDMFormat,
		pending.CompanyName,
		s.cfg.PublicBaseURL,
	)
	if err != nil {
		// Don't unwind the consumed flag on bundle generation
		// failure — the token is single-use either way; a retry
		// would just hit the same failure. Caller files an issue.
		http.Error(w, "bundle generation failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition",
		`attachment; filename="bastio-shadow-ai-audit.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(bundle)
}
