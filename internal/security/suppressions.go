package security

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxSuppressionsPerProfile = 100
const maxSuppressionPatternLen = 256

// PatternSuppression is a tenant-owned false-positive skip. Findings
// whose detector plus matched_pattern (or subcategory / matched_content)
// equal these fields are dropped before strategy evaluation.
type PatternSuppression struct {
	Detector string `json:"detector"`
	Pattern  string `json:"pattern"`
}

func filterSuppressed(findings []Finding, list []PatternSuppression) []Finding {
	if len(findings) == 0 || len(list) == 0 {
		return findings
	}
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if findingSuppressed(f, list) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func findingSuppressed(f Finding, list []PatternSuppression) bool {
	det := strings.TrimSpace(f.DetectorName)
	for _, s := range list {
		if s.Detector == "" || s.Pattern == "" {
			continue
		}
		if !strings.EqualFold(det, strings.TrimSpace(s.Detector)) {
			continue
		}
		pat := strings.TrimSpace(s.Pattern)
		if strings.EqualFold(f.MatchedPattern, pat) ||
			strings.EqualFold(f.SubCategory, pat) ||
			strings.EqualFold(strings.TrimSpace(f.MatchedContent), pat) {
			return true
		}
	}
	return false
}

func decodeSuppressionsJSON(raw []byte) []PatternSuppression {
	if len(raw) == 0 {
		return nil
	}
	var out []PatternSuppression
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func loadSuppressions(ctx context.Context, db *pgxpool.Pool, customerID, profileID uuid.UUID) []PatternSuppression {
	if db == nil || customerID == uuid.Nil || profileID == uuid.Nil {
		return nil
	}
	rows, err := db.Query(ctx, `
		SELECT detector, pattern
		FROM security_suppressions
		WHERE customer_id = $1 AND profile_id = $2
		ORDER BY created_at ASC
	`, customerID, profileID)
	if err != nil {
		slog.Warn("load security suppressions failed", "error", err)
		return nil
	}
	defer rows.Close()

	out := make([]PatternSuppression, 0)
	for rows.Next() {
		var s PatternSuppression
		if err := rows.Scan(&s.Detector, &s.Pattern); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}
