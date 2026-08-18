package security

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var suppressionDetectorRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// SuppressionDTO is the dashboard representation of an allowed pattern.
type SuppressionDTO struct {
	ID       string `json:"id"`
	Detector string `json:"detector"`
	Pattern  string `json:"pattern"`
}

func (h *ProfileHandler) suppressionRoutes(r chi.Router) {
	r.Get("/profiles/{id}/suppressions", h.ListSuppressions)
	r.Post("/profiles/{id}/suppressions", h.CreateSuppression)
	r.Delete("/profiles/{id}/suppressions/{suppressionID}", h.DeleteSuppression)
}

// ListSuppressions returns allowed detector patterns for a profile.
func (h *ProfileHandler) ListSuppressions(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	customerID := tenantIDFromCtx(r.Context())
	if !h.profileOwned(r, profileID, customerID) {
		http.Error(w, `{"error":"profile not found"}`, http.StatusNotFound)
		return
	}
	rows, err := h.db.Query(r.Context(), `
		SELECT id::text, detector, pattern
		FROM security_suppressions
		WHERE customer_id = $1::uuid AND profile_id = $2::uuid
		ORDER BY created_at ASC
	`, customerID, profileID)
	if err != nil {
		slog.Error("list security suppressions failed", "error", err)
		http.Error(w, `{"error":"query suppressions failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := make([]SuppressionDTO, 0)
	for rows.Next() {
		var s SuppressionDTO
		if err := rows.Scan(&s.ID, &s.Detector, &s.Pattern); err != nil {
			continue
		}
		out = append(out, s)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

type createSuppressionRequest struct {
	Detector string `json:"detector"`
	Pattern  string `json:"pattern"`
}

// CreateSuppression records a false-positive skip for a detector pattern.
func (h *ProfileHandler) CreateSuppression(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	customerID := tenantIDFromCtx(r.Context())
	if !h.profileOwned(r, profileID, customerID) {
		http.Error(w, `{"error":"profile not found"}`, http.StatusNotFound)
		return
	}

	var req createSuppressionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	req.Detector = strings.ToLower(strings.TrimSpace(req.Detector))
	req.Pattern = strings.TrimSpace(req.Pattern)
	if err := validateSuppression(req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	var n int
	if err := h.db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM security_suppressions
		WHERE customer_id = $1::uuid AND profile_id = $2::uuid
		  AND NOT (detector = $3 AND pattern = $4)
	`, customerID, profileID, req.Detector, req.Pattern).Scan(&n); err != nil {
		slog.Error("count security suppressions failed", "error", err)
		http.Error(w, `{"error":"create suppression failed"}`, http.StatusInternalServerError)
		return
	}
	if n >= maxSuppressionsPerProfile {
		http.Error(w, `{"error":"suppression limit reached"}`, http.StatusBadRequest)
		return
	}

	var id string
	err := h.db.QueryRow(r.Context(), `
		INSERT INTO security_suppressions (customer_id, profile_id, detector, pattern)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		ON CONFLICT (customer_id, profile_id, detector, pattern) DO UPDATE
		SET pattern = EXCLUDED.pattern
		RETURNING id::text
	`, customerID, profileID, req.Detector, req.Pattern).Scan(&id)
	if err != nil {
		slog.Error("insert security suppression failed", "error", err)
		http.Error(w, `{"error":"create suppression failed"}`, http.StatusInternalServerError)
		return
	}

	if req.Detector == "topic_policy" {
		if _, err := h.db.Exec(r.Context(), `
			UPDATE security_patterns SET is_active = false
			WHERE customer_id = $1::uuid AND profile_id = $2::uuid AND is_active = true
			  AND (lower(pattern) = lower($3) OR lower(name) = lower($3))
		`, customerID, profileID, req.Pattern); err != nil {
			slog.Error("deactivate topic pattern after suppress failed", "error", err)
		}
		h.bumpTopicCache(customerID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(SuppressionDTO{
		ID:       id,
		Detector: req.Detector,
		Pattern:  req.Pattern,
	})
}

// DeleteSuppression removes an allowed pattern. Must be scoped to the tenant.
func (h *ProfileHandler) DeleteSuppression(w http.ResponseWriter, r *http.Request) {
	customerID := tenantIDFromCtx(r.Context())
	suppressionID := chi.URLParam(r, "suppressionID")
	if _, err := uuid.Parse(suppressionID); err != nil {
		http.Error(w, `{"error":"suppression not found"}`, http.StatusNotFound)
		return
	}
	tag, err := h.db.Exec(r.Context(), `
		DELETE FROM security_suppressions
		WHERE id = $1::uuid AND customer_id = $2::uuid
	`, suppressionID, customerID)
	if err != nil {
		slog.Error("delete security suppression failed", "error", err)
		http.Error(w, `{"error":"delete suppression failed"}`, http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, `{"error":"suppression not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"deleted"}`)
}

func validateSuppression(req createSuppressionRequest) error {
	if req.Detector == "" {
		return fmt.Errorf("detector is required")
	}
	if !suppressionDetectorRE.MatchString(req.Detector) {
		return fmt.Errorf("invalid detector")
	}
	if req.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	if len(req.Pattern) > maxSuppressionPatternLen {
		return fmt.Errorf("pattern too long")
	}
	return nil
}
