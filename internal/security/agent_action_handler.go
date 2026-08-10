package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/bastio-ai/bastio/pkg/cache"
)

// AgentActionHandler exposes POST /v1/guardrails/agent-action for autonomous agent
// action inspection, ensuring tool calls and executed commands comply with security policies.
type AgentActionHandler struct {
	cache *cache.Cache
}

func NewAgentActionHandler() *AgentActionHandler {
	return &AgentActionHandler{}
}

func (h *AgentActionHandler) SetCache(c *cache.Cache) {
	h.cache = c
}

func (h *AgentActionHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/agent-action", h.InspectAction)
	return r
}

type AgentActionRequest struct {
	ToolName  string                 `json:"tool_name"`
	Arguments map[string]interface{} `json:"arguments"`
	AgentRole string                 `json:"agent_role,omitempty"`
	Context   string                 `json:"context,omitempty"`
}

type AgentActionResponse struct {
	Allowed            bool                   `json:"allowed"`
	Action             string                 `json:"action"` // "allow", "block", "flag"
	RiskScore          float64                `json:"risk_score"`
	Reasons            []string               `json:"reasons,omitempty"`
	SanitizedArguments map[string]interface{} `json:"sanitized_arguments,omitempty"`
}

var (
	dangerousCmds  = regexp.MustCompile(`(?i)\b(rm\s+-rf|chmod\s+777|chown\s+root|mkfs|dd\s+if=|:\(\)\{:\|:\&\};:|wget\s+.*\|\s*bash|curl\s+.*\|\s*sh|eval\(|exec\(|nc\s+-e|netcat|ncat)\b`)
	sensitiveFiles = regexp.MustCompile(`(?i)(/etc/passwd|/etc/shadow|\.ssh/id_|/\.aws/credentials|/\.env)`)
	sqlInjection   = regexp.MustCompile(`(?i)\b(DROP\s+TABLE|DELETE\s+FROM|UNION\s+SELECT|ALTER\s+TABLE|TRUNCATE\s+TABLE|1=1|1\s*=\s*1)\b`)
	pathTraversal  = regexp.MustCompile(`(\.\./\.\./|\.\.\\\.\.\\)`)
)

func (h *AgentActionHandler) InspectAction(w http.ResponseWriter, r *http.Request) {
	var req AgentActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.ToolName == "" {
		http.Error(w, `{"error":"tool_name field is required"}`, http.StatusBadRequest)
		return
	}

	var cacheKey string
	if h.cache != nil && !h.cache.ShouldBypass(r.Context(), "bastio-agent-guardrails-v1", r.URL.Path) {
		reqBytes, _ := json.Marshal(req)
		hasher := sha256.New()
		hasher.Write(reqBytes)
		cacheKey = "devapi:action:" + hex.EncodeToString(hasher.Sum(nil))

		var cachedResp AgentActionResponse
		if found, _ := h.cache.Get(r.Context(), cacheKey, &cachedResp); found {
			_, _ = h.cache.Incr(r.Context(), "cache:hits")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Bastio-Cache", "HIT")
			if !cachedResp.Allowed {
				w.WriteHeader(http.StatusForbidden)
			} else {
				w.WriteHeader(http.StatusOK)
			}
			_ = json.NewEncoder(w).Encode(cachedResp)
			return
		}
	}

	var reasons []string
	var maxRisk float64 = 0.0

	argBytes, _ := json.Marshal(req.Arguments)
	argStr := string(argBytes)

	if dangerousCmds.MatchString(argStr) {
		maxRisk = max(maxRisk, 0.95)
		reasons = append(reasons, "Dangerous system command execution detected in tool arguments")
	}

	if sensitiveFiles.MatchString(argStr) || pathTraversal.MatchString(argStr) {
		maxRisk = max(maxRisk, 0.90)
		reasons = append(reasons, "Unauthorized file path or path traversal detected in tool arguments")
	}

	if strings.Contains(strings.ToLower(req.ToolName), "sql") || strings.Contains(strings.ToLower(req.ToolName), "db") || strings.Contains(strings.ToLower(req.ToolName), "query") {
		if sqlInjection.MatchString(argStr) {
			maxRisk = max(maxRisk, 0.98)
			reasons = append(reasons, "SQL Injection attempt detected in database tool arguments")
		}
	}

	action := "allow"
	allowed := true
	if maxRisk >= 0.80 {
		action = "block"
		allowed = false
	} else if maxRisk >= 0.40 {
		action = "flag"
		allowed = true
	}

	res := AgentActionResponse{
		Allowed:            allowed,
		Action:             action,
		RiskScore:          maxRisk,
		Reasons:            reasons,
		SanitizedArguments: req.Arguments,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Bastio-Cache", "MISS")
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(res)

	if h.cache != nil && cacheKey != "" {
		_, _ = h.cache.Incr(r.Context(), "cache:misses")
		_ = h.cache.Set(r.Context(), cacheKey, res, 1*time.Hour)
	}
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
