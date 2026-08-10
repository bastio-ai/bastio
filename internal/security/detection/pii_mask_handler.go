package detection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/cache"
)

// PIIMaskHandler provides standalone API endpoints for PII and Secret masking,
// tokenization, and unmasking without requiring an upstream LLM proxy call.
type PIIMaskHandler struct {
	detector *PIIDetector
	cache    *cache.Cache
}

// NewPIIMaskHandler initializes the PII mask handler with default detector rules.
func NewPIIMaskHandler() *PIIMaskHandler {
	return &PIIMaskHandler{
		detector: NewPIIDetector(),
	}
}

// SetCache injects the Redis cache for fast sub-ms masking lookups.
func (h *PIIMaskHandler) SetCache(c *cache.Cache) {
	h.cache = c
}

// Routes registers /v1/pii/mask and /v1/pii/unmask endpoints.
func (h *PIIMaskHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/mask", h.Mask)
	r.Post("/unmask", h.Unmask)
	return r
}

type PIIMaskRequest struct {
	Text        string `json:"text"`
	Mode        string `json:"mode,omitempty"` // "tokenize" (default), "mask", "redact"
	BypassCache bool   `json:"bypass_cache,omitempty"`
}

type PIIMaskResponse struct {
	OriginalText  string            `json:"original_text"`
	ProcessedText string            `json:"processed_text"`
	Tokens        map[string]string `json:"tokens,omitempty"`
	DetectedTypes []string          `json:"detected_types"`
}

type PIIUnmaskRequest struct {
	Text   string            `json:"text"`
	Tokens map[string]string `json:"tokens"`
}

type PIIUnmaskResponse struct {
	UnmaskedText string `json:"unmasked_text"`
}

// Mask inspects input text, detects PII/Secrets, and masks or tokenizes them.
func (h *PIIMaskHandler) Mask(w http.ResponseWriter, r *http.Request) {
	var req PIIMaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, `{"error":"text field is required"}`, http.StatusBadRequest)
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "tokenize"
	}

	bypassHeader := strings.EqualFold(r.Header.Get("Cache-Control"), "no-cache") ||
		strings.EqualFold(r.Header.Get("X-Bastio-Bypass-Cache"), "true") ||
		r.Header.Get("X-Bastio-Bypass-Cache") == "1" ||
		strings.EqualFold(r.Header.Get("X-Cache-Bypass"), "true") ||
		r.Header.Get("X-Cache-Bypass") == "1"
	bypass := req.BypassCache || bypassHeader

	var cacheKey string
	if h.cache != nil && !bypass && !h.cache.ShouldBypass(r.Context(), "bastio-pii-v1", r.URL.Path) {
		hasher := sha256.New()
		hasher.Write([]byte(mode + ":" + req.Text))
		cacheKey = "devapi:pii:mask:" + hex.EncodeToString(hasher.Sum(nil))

		var cachedResp PIIMaskResponse
		if found, _ := h.cache.Get(r.Context(), cacheKey, &cachedResp); found {
			_, _ = h.cache.Incr(r.Context(), "cache:hits")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Bastio-Cache", "HIT")
			_ = json.NewEncoder(w).Encode(cachedResp)
			return
		}
	}

	res := PIIMaskResponse{
		OriginalText:  req.Text,
		DetectedTypes: []string{},
	}

	switch mode {
	case "mask", "redact":
		res.ProcessedText = h.detector.Mask(req.Text)
	case "tokenize":
		fallthrough
	default:
		tm := security.NewTokenMap(security.TokenStyleAngle)
		res.ProcessedText = h.detector.TokenizeInto(req.Text, tm)
		tokens := make(map[string]string)
		for _, ph := range tm.Placeholders() {
			orig, _ := tm.Restore(ph)
			tokens[ph] = orig
		}
		res.Tokens = tokens
	}

	w.Header().Set("Content-Type", "application/json")
	if bypass {
		w.Header().Set("X-Bastio-Cache", "BYPASS")
	} else {
		w.Header().Set("X-Bastio-Cache", "MISS")
	}
	_ = json.NewEncoder(w).Encode(res)

	if h.cache != nil && cacheKey != "" && !bypass {
		_, _ = h.cache.Incr(r.Context(), "cache:misses")
		_ = h.cache.Set(r.Context(), cacheKey, res, 1*time.Hour)
	}
}

// Unmask takes tokenized text and a token map to restore the original sensitive values.
func (h *PIIMaskHandler) Unmask(w http.ResponseWriter, r *http.Request) {
	var req PIIUnmaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.Text == "" || req.Tokens == nil {
		http.Error(w, `{"error":"text and tokens fields are required"}`, http.StatusBadRequest)
		return
	}

	unmasked := req.Text
	for ph, orig := range req.Tokens {
		unmasked = strings.ReplaceAll(unmasked, ph, orig)
	}
	res := PIIUnmaskResponse{
		UnmaskedText: unmasked,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
