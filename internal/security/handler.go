package security

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastio-ai/bastio/pkg/tenant"
)

// DefaultCustomerID is the seeded OSS single-tenant customer, mirrored
// from pkg/tenant. Kept for backward compatibility with external
// callers; the handlers in this file MUST resolve the tenant from
// request context via tenantIDFromCtx so cloud (multi-tenant) requests
// are correctly scoped.
var DefaultCustomerID = tenant.DefaultOSSID.String()

// tenantIDFromCtx reads the request-bound customer from context.
// OSSMiddleware sets DefaultOSSID in single-tenant deployments; cloud's
// auth middleware overrides with the real session-bound customer.
func tenantIDFromCtx(ctx context.Context) string {
	if id, err := tenant.FromContext(ctx); err == nil {
		return id.String()
	}
	return DefaultCustomerID
}

// ProfileHandler manages security profile endpoints.
type ProfileHandler struct {
	db              *pgxpool.Pool
	invalidateTopic func(uuid.UUID)
}

// NewProfileHandler creates a new security profile handler.
func NewProfileHandler(db *pgxpool.Pool) *ProfileHandler {
	return &ProfileHandler{db: db}
}

// Routes returns the Chi router for security endpoints.
func (h *ProfileHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/profiles", h.ListProfiles)
	r.Put("/profiles/{id}", h.UpdateProfile)
	h.topicRoutes(r)
	h.suppressionRoutes(r)
	return r
}

// ListProfiles returns security profiles for the default customer.
func (h *ProfileHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT id::text, customer_id::text, proxy_id::text, name,
			injection_enabled, injection_threshold,
			pii_enabled, pii_action,
			pii_scan_response, pii_restore_response, pii_token_style,
			jailbreak_enabled, jailbreak_threshold,
			bot_detection_enabled, custom_patterns_enabled,
			canonicalize_enabled, secrets_enabled,
			indirect_injection_enabled, output_exfil_enabled, topic_policy_enabled,
			rate_anomaly_enabled,
			injection_strategy, jailbreak_strategy, secrets_strategy,
			indirect_injection_strategy, output_exfil_strategy,
			created_at::text, updated_at::text
		FROM security_profiles
		WHERE customer_id = $1::uuid
		ORDER BY created_at DESC
	`, tenantIDFromCtx(r.Context()))
	if err != nil {
		slog.Error("list security profiles failed", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"query profiles: %s"}`, err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var profiles []map[string]any
	for rows.Next() {
		var (
			id, customerID            string
			proxyID                   *string
			name                      string
			injectionEnabled          bool
			injectionThreshold        float32
			piiEnabled                bool
			piiAction                 string
			piiScanResponse           bool
			piiRestoreResponse        bool
			piiTokenStyle             string
			jailbreakEnabled          bool
			jailbreakThreshold        float32
			botDetectionEnabled       bool
			customPatternsEnabled     bool
			canonicalizeEnabled       bool
			secretsEnabled            bool
			indirectInjectionEnabled  bool
			outputExfilEnabled        bool
			topicPolicyEnabled        bool
			rateAnomalyEnabled        bool
			injectionStrategy         string
			jailbreakStrategy         string
			secretsStrategy           string
			indirectInjectionStrategy string
			outputExfilStrategy       string
			createdAt, updatedAt      string
		)
		if err := rows.Scan(
			&id, &customerID, &proxyID, &name,
			&injectionEnabled, &injectionThreshold,
			&piiEnabled, &piiAction,
			&piiScanResponse, &piiRestoreResponse, &piiTokenStyle,
			&jailbreakEnabled, &jailbreakThreshold,
			&botDetectionEnabled, &customPatternsEnabled,
			&canonicalizeEnabled, &secretsEnabled,
			&indirectInjectionEnabled, &outputExfilEnabled, &topicPolicyEnabled,
			&rateAnomalyEnabled,
			&injectionStrategy, &jailbreakStrategy, &secretsStrategy,
			&indirectInjectionStrategy, &outputExfilStrategy,
			&createdAt, &updatedAt,
		); err != nil {
			slog.Error("scan security profile row", "error", err)
			continue
		}
		profiles = append(profiles, map[string]any{
			"id":                          id,
			"customer_id":                 customerID,
			"proxy_id":                    proxyID,
			"name":                        name,
			"injection_enabled":           injectionEnabled,
			"injection_threshold":         injectionThreshold,
			"pii_enabled":                 piiEnabled,
			"pii_action":                  piiAction,
			"pii_scan_response":           piiScanResponse,
			"pii_restore_response":        piiRestoreResponse,
			"pii_token_style":             piiTokenStyle,
			"jailbreak_enabled":           jailbreakEnabled,
			"jailbreak_threshold":         jailbreakThreshold,
			"bot_detection_enabled":       botDetectionEnabled,
			"custom_patterns_enabled":     customPatternsEnabled,
			"canonicalize_enabled":        canonicalizeEnabled,
			"secrets_enabled":             secretsEnabled,
			"indirect_injection_enabled":  indirectInjectionEnabled,
			"output_exfil_enabled":        outputExfilEnabled,
			"topic_policy_enabled":        topicPolicyEnabled,
			"rate_anomaly_enabled":        rateAnomalyEnabled,
			"injection_strategy":          injectionStrategy,
			"jailbreak_strategy":          jailbreakStrategy,
			"secrets_strategy":            secretsStrategy,
			"indirect_injection_strategy": indirectInjectionStrategy,
			"output_exfil_strategy":       outputExfilStrategy,
			"created_at":                  createdAt,
			"updated_at":                  updatedAt,
		})
	}

	if profiles == nil {
		profiles = []map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

// UpdateProfile updates a security profile's settings.
func (h *ProfileHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	allowed := map[string]string{
		"injection_enabled":           "injection_enabled",
		"injection_threshold":         "injection_threshold",
		"pii_enabled":                 "pii_enabled",
		"pii_action":                  "pii_action",
		"pii_scan_response":           "pii_scan_response",
		"pii_restore_response":        "pii_restore_response",
		"pii_token_style":             "pii_token_style",
		"jailbreak_enabled":           "jailbreak_enabled",
		"jailbreak_threshold":         "jailbreak_threshold",
		"bot_detection_enabled":       "bot_detection_enabled",
		"custom_patterns_enabled":     "custom_patterns_enabled",
		"canonicalize_enabled":        "canonicalize_enabled",
		"secrets_enabled":             "secrets_enabled",
		"indirect_injection_enabled":  "indirect_injection_enabled",
		"output_exfil_enabled":        "output_exfil_enabled",
		"topic_policy_enabled":        "topic_policy_enabled",
		"rate_anomaly_enabled":        "rate_anomaly_enabled",
		"injection_strategy":          "injection_strategy",
		"jailbreak_strategy":          "jailbreak_strategy",
		"secrets_strategy":            "secrets_strategy",
		"indirect_injection_strategy": "indirect_injection_strategy",
		"output_exfil_strategy":       "output_exfil_strategy",
	}

	if v, ok := req["pii_action"]; ok {
		if s, _ := v.(string); !validPIIAction(s) {
			http.Error(w, `{"error":"pii_action must be one of: mask, tokenize, block, warn, log_only"}`, http.StatusBadRequest)
			return
		}
	}
	if v, ok := req["pii_token_style"]; ok {
		if s, _ := v.(string); !validTokenStyle(s) {
			http.Error(w, `{"error":"pii_token_style must be one of: angle, curly"}`, http.StatusBadRequest)
			return
		}
	}

	// Per-detector strategies. Text-threat detectors accept the same
	// subset (block | warn | log_only); secrets additionally accepts
	// mask. Rejecting invalid values here gives the dashboard a clear
	// 400 instead of a generic 500 from the CHECK constraint.
	for _, field := range []string{
		"injection_strategy", "jailbreak_strategy",
		"indirect_injection_strategy", "output_exfil_strategy",
	} {
		if v, ok := req[field]; ok {
			if s, _ := v.(string); !validTextThreatStrategy(s) {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, field+" must be one of: block, warn, log_only"), http.StatusBadRequest)
				return
			}
		}
	}
	if v, ok := req["secrets_strategy"]; ok {
		if s, _ := v.(string); !validSecretsStrategy(s) {
			http.Error(w, `{"error":"secrets_strategy must be one of: block, mask, warn, log_only"}`, http.StatusBadRequest)
			return
		}
	}

	// Thresholds: weighted score ∈ [0, 1]. Defensive — the DB column
	// is REAL so anything parses, but out-of-range values break the
	// step runner's semantics silently.
	for _, field := range []string{"injection_threshold", "jailbreak_threshold"} {
		if v, ok := req[field]; ok {
			n, err := toFloat(v)
			if err != nil || n < 0 || n > 1 {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, field+" must be a number between 0 and 1"), http.StatusBadRequest)
				return
			}
		}
	}

	for jsonKey, colName := range allowed {
		val, ok := req[jsonKey]
		if !ok {
			continue
		}
		query := fmt.Sprintf(
			"UPDATE security_profiles SET %s = $1 WHERE id = $2::uuid AND customer_id = $3::uuid",
			colName,
		)
		if _, err := h.db.Exec(r.Context(), query, val, idStr, tenantIDFromCtx(r.Context())); err != nil {
			slog.Error("update security profile failed", "column", colName, "error", err)
			http.Error(w, `{"error":"update failed"}`, http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"updated"}`)
}

func validPIIAction(s string) bool {
	switch Action(s) {
	case ActionMask, ActionTokenize, ActionBlock, ActionWarn, ActionLogOnly:
		return true
	}
	return false
}

func validTokenStyle(s string) bool {
	switch TokenStyle(s) {
	case TokenStyleAngle, TokenStyleCurly:
		return true
	}
	return false
}

// validTextThreatStrategy is the strategy menu for detectors whose
// finding is "the text itself is bad" — injection, jailbreak, indirect
// injection, output exfiltration. Mask/tokenize don't apply: there's
// no part of the message to salvage.
func validTextThreatStrategy(s string) bool {
	switch Action(s) {
	case ActionBlock, ActionWarn, ActionLogOnly:
		return true
	}
	return false
}

// validSecretsStrategy adds mask to the text-threat set. Tokenize is
// intentionally absent: producing a reversible placeholder for a
// secret would let the response path restore it, defeating the point.
func validSecretsStrategy(s string) bool {
	switch Action(s) {
	case ActionBlock, ActionMask, ActionWarn, ActionLogOnly:
		return true
	}
	return false
}

// toFloat accepts the JSON numeric types both openapi-fetch and raw
// clients produce (float64 from JSON, plus int/json.Number defense
// in depth) and returns a float64. Error on anything else.
func toFloat(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	}
	return 0, fmt.Errorf("not a number: %T", v)
}
