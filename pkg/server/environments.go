package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bastio-ai/bastio/pkg/tenant"
)

const maxEnvironmentsPerCustomer = 25

var environmentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

type environmentRow struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type createEnvironmentRequest struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request) {
	customerID := dashboardCustomerID(r)
	rows, err := s.db.Pool.Query(r.Context(), `
SELECT id, name, kind, description, created_at
FROM environments
WHERE customer_id = $1
ORDER BY
  CASE kind WHEN 'production' THEN 0 WHEN 'staging' THEN 1 WHEN 'development' THEN 2 ELSE 3 END,
  lower(name)`, customerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to list environments"})
		return
	}
	defer rows.Close()

	environments := make([]environmentRow, 0)
	for rows.Next() {
		var item environmentRow
		if err := rows.Scan(&item.ID, &item.Name, &item.Kind, &item.Description, &item.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to read environments"})
			return
		}
		environments = append(environments, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to list environments"})
		return
	}
	writeJSON(w, http.StatusOK, environments)
}

func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request) {
	if role, ok := r.Context().Value(RoleKey).(Role); ok && role != RoleOwner && role != RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
		return
	}

	var body createEnvironmentRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	body.Name = strings.ToLower(strings.TrimSpace(body.Name))
	body.Kind = strings.ToLower(strings.TrimSpace(body.Kind))
	body.Description = strings.TrimSpace(body.Description)
	if !environmentNamePattern.MatchString(body.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must start with a letter and use only lowercase letters, numbers, hyphens, or underscores"})
		return
	}
	if body.Kind == "" {
		body.Kind = "custom"
	}
	if body.Kind != "production" && body.Kind != "staging" && body.Kind != "development" && body.Kind != "custom" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid environment kind"})
		return
	}
	if len(body.Description) > 240 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "description must be 240 characters or fewer"})
		return
	}

	customerID := dashboardCustomerID(r)
	var count int
	if err := s.db.Pool.QueryRow(r.Context(), `SELECT count(*) FROM environments WHERE customer_id = $1`, customerID).Scan(&count); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to validate environment limit"})
		return
	}
	if count >= maxEnvironmentsPerCustomer {
		writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("workspace limit of %d environments reached", maxEnvironmentsPerCustomer)})
		return
	}

	createdBy, _ := r.Context().Value(UserIDKey).(string)
	var item environmentRow
	err := s.db.Pool.QueryRow(r.Context(), `
INSERT INTO environments (customer_id, name, kind, description, created_by)
VALUES ($1, $2, $3, $4, NULLIF($5, ''))
RETURNING id, name, kind, description, created_at`, customerID, body.Name, body.Kind, body.Description, createdBy).
		Scan(&item.ID, &item.Name, &item.Kind, &item.Description, &item.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "environments_customer_name_key") || strings.Contains(err.Error(), "duplicate key") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "an environment with that name already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to create environment"})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func dashboardCustomerID(r *http.Request) uuid.UUID {
	if id, ok := r.Context().Value(CustomerIDKey).(uuid.UUID); ok && id != uuid.Nil {
		return id
	}
	return tenant.DefaultOSSID
}
