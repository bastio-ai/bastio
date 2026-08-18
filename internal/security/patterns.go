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

const maxTopicPatternsPerProfile = 50
const maxTopicPatternLen = 256

// PatternDTO is the dashboard representation of a topic-policy rule.
type PatternDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PatternType string `json:"pattern_type"`
	Pattern     string `json:"pattern"`
	Action      string `json:"action"`
	Severity    string `json:"severity"`
}

// SetTopicInvalidator registers a cache-bust callback fired after
// pattern writes so the topic detector reloads on the next scan.
func (h *ProfileHandler) SetTopicInvalidator(fn func(uuid.UUID)) {
	h.invalidateTopic = fn
}

func (h *ProfileHandler) topicRoutes(r chi.Router) {
	r.Get("/profiles/{id}/patterns", h.ListPatterns)
	r.Post("/profiles/{id}/patterns", h.CreatePattern)
	r.Delete("/profiles/{id}/patterns/{patternID}", h.DeletePattern)
}

// ListPatterns returns active topic-policy patterns for a profile.
func (h *ProfileHandler) ListPatterns(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	customerID := tenantIDFromCtx(r.Context())
	if !h.profileOwned(r, profileID, customerID) {
		http.Error(w, `{"error":"profile not found"}`, http.StatusNotFound)
		return
	}
	rows, err := h.db.Query(r.Context(), `
		SELECT id::text, name, pattern_type, pattern, action, severity
		FROM security_patterns
		WHERE customer_id = $1::uuid AND profile_id = $2::uuid AND is_active = true
		ORDER BY created_at ASC
	`, customerID, profileID)
	if err != nil {
		slog.Error("list security patterns failed", "error", err)
		http.Error(w, `{"error":"query patterns failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := make([]PatternDTO, 0)
	for rows.Next() {
		var p PatternDTO
		if err := rows.Scan(&p.ID, &p.Name, &p.PatternType, &p.Pattern, &p.Action, &p.Severity); err != nil {
			continue
		}
		out = append(out, p)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

type createPatternRequest struct {
	Name        string `json:"name"`
	PatternType string `json:"pattern_type"`
	Pattern     string `json:"pattern"`
	Action      string `json:"action"`
	Severity    string `json:"severity"`
}

// CreatePattern adds a keyword or regex topic rule.
func (h *ProfileHandler) CreatePattern(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	customerID := tenantIDFromCtx(r.Context())
	if !h.profileOwned(r, profileID, customerID) {
		http.Error(w, `{"error":"profile not found"}`, http.StatusNotFound)
		return
	}

	var req createPatternRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	req.Pattern = strings.TrimSpace(req.Pattern)
	req.Name = strings.TrimSpace(req.Name)
	req.PatternType = strings.ToLower(strings.TrimSpace(req.PatternType))
	if req.PatternType == "" {
		req.PatternType = "keyword"
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Action == "" {
		req.Action = "warn"
	}
	req.Severity = strings.ToLower(strings.TrimSpace(req.Severity))
	if req.Severity == "" {
		req.Severity = "medium"
	}
	if err := validateTopicPattern(req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = req.Pattern
		if len(req.Name) > 64 {
			req.Name = req.Name[:64]
		}
	}

	var n int
	if err := h.db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM security_patterns
		WHERE customer_id = $1::uuid AND profile_id = $2::uuid AND is_active = true
	`, customerID, profileID).Scan(&n); err != nil {
		slog.Error("count security patterns failed", "error", err)
		http.Error(w, `{"error":"create pattern failed"}`, http.StatusInternalServerError)
		return
	}
	if n >= maxTopicPatternsPerProfile {
		http.Error(w, `{"error":"pattern limit reached"}`, http.StatusBadRequest)
		return
	}

	var id string
	err := h.db.QueryRow(r.Context(), `
		INSERT INTO security_patterns (customer_id, profile_id, name, pattern_type, pattern, action, severity)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7)
		RETURNING id::text
	`, customerID, profileID, req.Name, req.PatternType, req.Pattern, req.Action, req.Severity).Scan(&id)
	if err != nil {
		slog.Error("insert security pattern failed", "error", err)
		http.Error(w, `{"error":"create pattern failed"}`, http.StatusInternalServerError)
		return
	}

	if _, err := h.db.Exec(r.Context(), `
		UPDATE security_profiles
		SET topic_policy_enabled = true, updated_at = now()
		WHERE id = $1::uuid AND customer_id = $2::uuid AND topic_policy_enabled = false
	`, profileID, customerID); err != nil {
		slog.Error("enable topic policy after pattern create failed", "error", err)
	}

	h.bumpTopicCache(customerID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(PatternDTO{
		ID:          id,
		Name:        req.Name,
		PatternType: req.PatternType,
		Pattern:     req.Pattern,
		Action:      req.Action,
		Severity:    req.Severity,
	})
}

// DeletePattern deactivates a topic rule. Must be scoped to the tenant.
func (h *ProfileHandler) DeletePattern(w http.ResponseWriter, r *http.Request) {
	customerID := tenantIDFromCtx(r.Context())
	patternID := chi.URLParam(r, "patternID")
	if _, err := uuid.Parse(patternID); err != nil {
		http.Error(w, `{"error":"pattern not found"}`, http.StatusNotFound)
		return
	}
	tag, err := h.db.Exec(r.Context(), `
		UPDATE security_patterns SET is_active = false
		WHERE id = $1::uuid AND customer_id = $2::uuid
	`, patternID, customerID)
	if err != nil {
		slog.Error("delete security pattern failed", "error", err)
		http.Error(w, `{"error":"delete pattern failed"}`, http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, `{"error":"pattern not found"}`, http.StatusNotFound)
		return
	}
	h.bumpTopicCache(customerID)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"deleted"}`)
}

func (h *ProfileHandler) profileOwned(r *http.Request, profileID, customerID string) bool {
	if _, err := uuid.Parse(profileID); err != nil {
		return false
	}
	var n int
	err := h.db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM security_profiles
		WHERE id = $1::uuid AND customer_id = $2::uuid
	`, profileID, customerID).Scan(&n)
	return err == nil && n == 1
}

func (h *ProfileHandler) bumpTopicCache(customerID string) {
	if h.invalidateTopic == nil {
		return
	}
	id, err := uuid.Parse(customerID)
	if err != nil {
		return
	}
	h.invalidateTopic(id)
}

func validateTopicPattern(req createPatternRequest) error {
	if req.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	if len(req.Pattern) > maxTopicPatternLen {
		return fmt.Errorf("pattern too long")
	}
	switch req.PatternType {
	case "keyword", "regex":
	default:
		return fmt.Errorf("pattern_type must be keyword or regex")
	}
	switch req.Action {
	case "block", "warn", "log", "log_only":
	default:
		return fmt.Errorf("action must be block, warn, or log")
	}
	switch req.Severity {
	case "low", "medium", "high", "critical":
	default:
		return fmt.Errorf("severity must be low, medium, high, or critical")
	}
	if req.PatternType == "regex" {
		if _, err := regexp.Compile("(?i)" + req.Pattern); err != nil {
			return fmt.Errorf("invalid regex")
		}
	}
	return nil
}
