package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// PresidioClient wraps Microsoft Presidio's analyzer HTTP API. Presidio is
// the open-source PII detector that powers our server-side classifier when
// configured. When PRESIDIO_URL is unset (OSS minimal-deps deployments), the
// classifier falls back to the regex+entropy heuristic in handler.go.
//
// Presidio's analyzer endpoint accepts:
//
//	POST /analyze
//	{
//	  "text": "...",
//	  "language": "en",
//	  "entities": ["..."]   // optional; defaults to all known
//	}
//
// and returns:
//
//	[
//	  {"entity_type": "EMAIL_ADDRESS", "start": 5, "end": 24,
//	   "score": 0.95, "analysis_explanation": ...}
//	]
//
// We collapse the entity list down to a (severity, confidence, reasoning)
// tuple matching the existing ClassifyResponse shape. Latency budget is
// served by the extension's 500ms async hook; Presidio inference is
// typically 30-50ms.
type PresidioClient struct {
	url   string
	httpc *http.Client
}

// presidioEntity is one finding from Presidio /analyze.
type presidioEntity struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

// presidioRequest matches Presidio's /analyze request body.
type presidioRequest struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

var (
	presidioClient     *PresidioClient
	presidioInitOnce   sync.Once
)

// presidioFromEnv lazily constructs the client from PRESIDIO_URL. Returns
// nil when not configured — callers fall back to the rule heuristic.
func presidioFromEnv() *PresidioClient {
	presidioInitOnce.Do(func() {
		url := strings.TrimSpace(os.Getenv("PRESIDIO_URL"))
		if url == "" {
			return
		}
		presidioClient = &PresidioClient{
			url:   strings.TrimSuffix(url, "/"),
			httpc: &http.Client{Timeout: 2 * time.Second},
		}
	})
	return presidioClient
}

// Analyze posts the excerpt to Presidio and returns the highest-severity
// finding mapped onto the ClassifyResponse contract. Returns ok=false if
// Presidio isn't configured, unreachable, or returns a non-2xx; the caller
// then falls back to the rule heuristic.
func (c *PresidioClient) Analyze(ctx context.Context, excerpt string) (ClassifyResponse, bool, error) {
	body, err := json.Marshal(presidioRequest{
		Text:     excerpt,
		Language: "en",
	})
	if err != nil {
		return ClassifyResponse{}, false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/analyze", bytes.NewReader(body))
	if err != nil {
		return ClassifyResponse{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return ClassifyResponse{}, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ClassifyResponse{}, false, fmt.Errorf("presidio http %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return ClassifyResponse{}, false, err
	}

	var entities []presidioEntity
	if err := json.Unmarshal(raw, &entities); err != nil {
		return ClassifyResponse{}, false, fmt.Errorf("decode presidio response: %w", err)
	}

	return collapseEntities(entities), true, nil
}

// collapseEntities reduces a Presidio finding list to the single
// ClassifyResponse the extension expects. Entities are weighted by the kind
// of data + Presidio's own confidence score.
func collapseEntities(entities []presidioEntity) ClassifyResponse {
	if len(entities) == 0 {
		return ClassifyResponse{
			Severity:   SeverityLow,
			Confidence: 0.4,
			Reasoning:  "presidio: no PII entities detected",
		}
	}

	// High-severity entity types: financial, government identifiers,
	// medical info, secrets-shaped patterns, full names with co-located PII.
	highRisk := map[string]bool{
		"CREDIT_CARD":          true,
		"US_SSN":               true,
		"US_BANK_NUMBER":       true,
		"IBAN_CODE":            true,
		"MEDICAL_LICENSE":      true,
		"US_PASSPORT":          true,
		"US_DRIVER_LICENSE":    true,
		"US_ITIN":              true,
		"CRYPTO":               true,
		"UK_NHS":               true,
		"AU_TFN":               true,
		"AU_MEDICARE":          true,
		"IN_AADHAAR":           true,
		"IN_PAN":               true,
		"SG_NRIC_FIN":          true,
	}

	mediumRisk := map[string]bool{
		"EMAIL_ADDRESS":     true,
		"PHONE_NUMBER":      true,
		"PERSON":            true,
		"LOCATION":          true,
		"DATE_TIME":         true,
		"NRP":               true,
		"IP_ADDRESS":        true,
		"URL":               true,
	}

	var highCount, medCount int
	highTopScore := 0.0
	medTopScore := 0.0

	for _, e := range entities {
		switch {
		case highRisk[e.EntityType]:
			highCount++
			if e.Score > highTopScore {
				highTopScore = e.Score
			}
		case mediumRisk[e.EntityType]:
			medCount++
			if e.Score > medTopScore {
				medTopScore = e.Score
			}
		}
	}

	switch {
	case highCount > 0:
		return ClassifyResponse{
			Severity:   SeverityHigh,
			Confidence: clamp01(highTopScore + 0.05),
			Reasoning:  fmt.Sprintf("presidio: %d high-risk + %d medium-risk entit(y/ies) detected", highCount, medCount),
		}
	case medCount >= 3:
		return ClassifyResponse{
			Severity:   SeverityHigh,
			Confidence: 0.78,
			Reasoning:  fmt.Sprintf("presidio: %d medium-risk entities clustered (treated as high)", medCount),
		}
	case medCount >= 1:
		return ClassifyResponse{
			Severity:   SeverityMedium,
			Confidence: clamp01(medTopScore),
			Reasoning:  fmt.Sprintf("presidio: %d medium-risk entit(y/ies) detected", medCount),
		}
	default:
		return ClassifyResponse{
			Severity:   SeverityLow,
			Confidence: 0.5,
			Reasoning:  "presidio: only weak/contextual signals",
		}
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// classifyWithPresidioOrFallback is the canonical classifier entry point
// used by the handler. Wires Presidio when available; falls back to the
// existing rule heuristic otherwise.
func classifyWithPresidioOrFallback(ctx context.Context, req ClassifyRequest) ClassifyResponse {
	if cli := presidioFromEnv(); cli != nil {
		// Presidio wants the raw excerpt. Cap length to keep the
		// network round-trip bounded; Presidio is happy with arbitrary
		// text up to a few KB.
		excerpt := req.TextExcerpt
		if len(excerpt) > 8192 {
			excerpt = excerpt[:8192]
		}
		out, ok, err := cli.Analyze(ctx, excerpt)
		if ok {
			return out
		}
		// Log + fall through. Don't fail the request — extension's
		// async classifier hook is best-effort.
		_ = err
	}
	return classifyFallback(req)
}

