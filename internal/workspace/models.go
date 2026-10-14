package workspace

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var workspaceProviders = []string{"openai", "anthropic", "gemini", "deepseek", "xai", "groq", "openrouter", "bedrock", "ollama"}

type WorkspaceModel struct {
	ID          string     `json:"id"`
	Name        string     `json:"name,omitempty"`
	CheckedAt   *time.Time `json:"checked_at,omitempty"`
	CheckStatus string     `json:"check_status,omitempty"`
	ErrorCode   string     `json:"error_code,omitempty"`
}
type WorkspaceProviderModels struct {
	fingerprint  string
	Provider     string           `json:"provider"`
	Configured   bool             `json:"configured"`
	Source       string           `json:"source"`
	Models       []WorkspaceModel `json:"models"`
	DiscoveredAt *time.Time       `json:"discovered_at,omitempty"`
	ErrorCode    string           `json:"error_code,omitempty"`
}

func credentialFingerprint(cid uuid.UUID, provider, key string) string {
	// This is an identity stamp for a high-entropy API token, never a
	// password verifier. Domain separation also binds it to this tenant.
	stamp := hmac.New(sha256.New, []byte("bastio:model-readiness:"+cid.String()+":"+provider))
	_, _ = stamp.Write([]byte(key))
	return hex.EncodeToString(stamp.Sum(nil))
}
func (h *Handler) configuredWorkspaceProvider(ctx context.Context, cid uuid.UUID, name string) (string, bool) {
	if h.registry == nil {
		return "", false
	}
	client, err := h.registry.Get(providers.Provider(name))
	if err != nil {
		return "", false
	}
	key := h.resolveAPIKeyCtx(ctx, cid, providers.Provider(name))
	configured := key != ""
	if local, ok := client.(interface{ Configured() bool }); ok && name == "ollama" {
		configured = local.Configured()
	}
	return key, configured
}
func modelFailureCode(err error) string {
	var upstream *providers.ProviderError
	if errors.As(err, &upstream) {
		lower := strings.ToLower(string(upstream.Body))
		if strings.Contains(lower, "model_not_found") || strings.Contains(lower, "access to model") || strings.Contains(lower, "does not exist") || strings.Contains(lower, "not supported") {
			return "model_access_denied"
		}
		switch upstream.Status {
		case 401:
			return "provider_authentication_failed"
		case 403:
			return "provider_access_denied"
		case 402, 429:
			return "provider_usage_limit"
		default:
			return "provider_request_failed"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "provider_timeout"
	}
	if strings.Contains(err.Error(), "security") {
		return "workspace_security_policy"
	}
	if strings.Contains(err.Error(), "spending") || strings.Contains(err.Error(), "budget") {
		return "workspace_usage_limit"
	}
	return "provider_request_failed"
}
func (h *Handler) workspaceModels(ctx context.Context, cid uuid.UUID, name string, refresh bool) WorkspaceProviderModels {
	out := WorkspaceProviderModels{Provider: name, Source: "catalog", Models: []WorkspaceModel{}}
	key, configured := h.configuredWorkspaceProvider(ctx, cid, name)
	out.Configured = configured
	if !configured {
		return out
	}
	fingerprint := credentialFingerprint(cid, name, key)
	out.fingerprint = fingerprint
	var raw []byte
	var discovered time.Time
	err := h.store.pool.QueryRow(ctx, `SELECT models,source,discovered_at,error_code FROM workspace_provider_models WHERE customer_id=$1 AND provider=$2 AND credential_fingerprint=$3`, cid, name, fingerprint).Scan(&raw, &out.Source, &discovered, &out.ErrorCode)
	ttl := time.Hour
	if out.ErrorCode != "" {
		ttl = 30 * time.Second
	}
	if refresh || err != nil || time.Since(discovered) > ttl {
		request, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		listed, source, discoveryErr := providers.DiscoverChatModels(request, h.registry, providers.Provider(name), key)
		out.Source = source
		out.ErrorCode = ""
		if discoveryErr != nil {
			out.ErrorCode = modelFailureCode(discoveryErr)
			listed = []providers.ListedModel{}
		}
		raw, _ = json.Marshal(listed)
		discovered = time.Now().UTC()
		// No credential or provider error bodies are retained, only a scoped hash
		// and safe model identifiers. Rotation invalidates checks immediately.
		_, err = h.store.pool.Exec(ctx, `INSERT INTO workspace_provider_models(customer_id,provider,credential_fingerprint,source,models,discovered_at,error_code) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(customer_id,provider) DO UPDATE SET credential_fingerprint=excluded.credential_fingerprint,source=excluded.source,models=excluded.models,discovered_at=excluded.discovered_at,error_code=excluded.error_code`, cid, name, fingerprint, source, raw, discovered, out.ErrorCode)
		if err != nil {
			out.ErrorCode = "model_status_unavailable"
		}
	}
	out.DiscoveredAt = &discovered
	var listed []providers.ListedModel
	if json.Unmarshal(raw, &listed) == nil {
		for _, model := range listed {
			out.Models = append(out.Models, WorkspaceModel{ID: model.ID, Name: model.Name, CheckStatus: "untested"})
		}
	}
	return out
}
func (h *Handler) listWorkspaceModels(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	manage := r.URL.Query().Get("manage") == "true"
	if manage && !RoleFromCtx(r.Context()).AtLeast(RoleAdmin) {
		writeError(w, 403, "administrator role required")
		return
	}
	allowed, err := h.store.EffectiveAllowedModels(r.Context(), cid, userIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, 503, "model policy unavailable")
		return
	}
	out := make([]WorkspaceProviderModels, len(workspaceProviders))
	var workers sync.WaitGroup
	slots := make(chan struct{}, 3)
	for i, name := range workspaceProviders {
		workers.Add(1)
		go func(i int, name string) {
			defer workers.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-r.Context().Done():
				return
			}
			out[i] = h.workspaceModels(r.Context(), cid, name, false)
		}(i, name)
	}
	workers.Wait()
	for i := range out {
		p := &out[i]
		if !p.Configured {
			continue
		}
		key, _ := h.configuredWorkspaceProvider(r.Context(), cid, p.Provider)
		fingerprint := credentialFingerprint(cid, p.Provider, key)
		if fingerprint != p.fingerprint {
			p.Models = []WorkspaceModel{}
			p.Source = "provider"
			p.ErrorCode = "credential_changed"
			p.DiscoveredAt = nil
			continue
		}
		// Explicitly approved custom IDs remain selectable even when an API does
		// not enumerate them; they are labelled untested until an actual reply.
		for _, entry := range allowed {
			if entry.Provider == p.Provider && !containsWorkspaceModel(p.Models, entry.Model) {
				p.Models = append(p.Models, WorkspaceModel{ID: entry.Model, CheckStatus: "untested"})
			}
		}
		rows, e := h.store.pool.Query(r.Context(), `SELECT model,checked_at,success,error_code FROM workspace_model_checks WHERE customer_id=$1 AND provider=$2 AND credential_fingerprint=$3`, cid, p.Provider, fingerprint)
		if e == nil {
			for rows.Next() {
				var model, code string
				var at time.Time
				var success bool
				if rows.Scan(&model, &at, &success, &code) == nil {
					if success && !containsWorkspaceModel(p.Models, model) {
						p.Models = append(p.Models, WorkspaceModel{ID: model})
					}
					for j := range p.Models {
						if p.Models[j].ID == model {
							p.Models[j].CheckedAt = &at
							p.Models[j].CheckStatus = "failed"
							if success {
								p.Models[j].CheckStatus = "passed"
							}
							p.Models[j].ErrorCode = code
						}
					}
				}
			}
			rows.Close()
		}
		if !manage && len(allowed) > 0 {
			filtered := []WorkspaceModel{}
			for _, model := range p.Models {
				for _, entry := range allowed {
					if entry.Provider == p.Provider && entry.Model == model.ID {
						filtered = append(filtered, model)
						break
					}
				}
			}
			p.Models = filtered
			// An empty provider group must not trigger a client catalogue fallback.
			if len(filtered) == 0 {
				p.Configured = false
			}
		}
	}
	writeJSON(w, 200, struct {
		Providers  []WorkspaceProviderModels `json:"providers"`
		Restricted bool                      `json:"restricted"`
	}{out, len(allowed) > 0 && !manage})
}
func containsWorkspaceModel(models []WorkspaceModel, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}
func (h *Handler) discoverWorkspaceModels(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var input struct {
		Provider string `json:"provider"`
	}
	if decodeMCP(r, &input) != nil || !knownWorkspaceProvider(input.Provider) {
		writeError(w, 400, "choose a supported provider")
		return
	}
	writeJSON(w, 200, h.workspaceModels(r.Context(), customerIDFromCtx(r.Context()), input.Provider, true))
}
func knownWorkspaceProvider(name string) bool {
	for _, p := range workspaceProviders {
		if p == name {
			return true
		}
	}
	return false
}
func (h *Handler) testWorkspaceModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var input struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if decodeMCP(r, &input) != nil || !knownWorkspaceProvider(input.Provider) {
		writeError(w, 400, "choose a supported provider and model")
		return
	}
	input.Model = strings.TrimSpace(input.Model)
	if input.Model == "" || len(input.Model) > 256 || strings.IndexFunc(input.Model, unicode.IsControl) >= 0 {
		writeError(w, 400, "choose a valid model")
		return
	}
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	key, configured := h.configuredWorkspaceProvider(r.Context(), cid, input.Provider)
	if !configured {
		writeStructuredError(w, 400, "provider_not_configured", "Ask a workspace admin to connect this provider.", nil)
		return
	}
	if _, err := h.store.EnsureSettings(r.Context(), cid); err != nil {
		writeError(w, 503, "workspace settings unavailable")
		return
	}
	var attempts int
	err := h.store.pool.QueryRow(r.Context(), `INSERT INTO workspace_model_test_limits(customer_id,user_id,attempts) VALUES($1,$2,1)
 ON CONFLICT(customer_id,user_id) DO UPDATE SET attempts=CASE WHEN workspace_model_test_limits.window_started_at<now()-interval '1 minute' THEN 1 ELSE workspace_model_test_limits.attempts+1 END,window_started_at=CASE WHEN workspace_model_test_limits.window_started_at<now()-interval '1 minute' THEN now() ELSE workspace_model_test_limits.window_started_at END RETURNING attempts`, cid, uid).Scan(&attempts)
	if err != nil {
		writeError(w, 503, "model test limit unavailable")
		return
	}
	if attempts > 5 {
		writeError(w, 429, "Please wait a minute before testing another model.")
		return
	}
	// Hidden test conversations retain usage accounting, but never message
	// bodies or titles. They do not appear in employee or admin chat lists.
	var id uuid.UUID
	err = h.store.pool.QueryRow(r.Context(), `INSERT INTO workspace_conversations(customer_id,user_id,title,content_stored,is_setup_test) VALUES($1,$2,'Workspace connection check',FALSE,TRUE) RETURNING id`, cid, uid).Scan(&id)
	if err != nil {
		writeError(w, 503, "model test unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(WithTemporaryChat(r.Context(), true), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, callOptionsKey{}, providerCallOptions{maxOutput: 512, reasoningEffort: providers.HelloReasoningEffort(input.Model), resolvedKey: key, keyResolved: true})
	route := chi.NewRouteContext()
	route.URLParams.Add("id", id.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, route)
	r = r.WithContext(ctx)
	const prompt = "Say hello in one short, cheerful sentence. Include a tiny robot celebrating a successful connection."
	_, err = h.store.AppendMessage(ctx, Message{CustomerID: cid, ConversationID: id, Role: "user", Content: prompt})
	if err != nil {
		writeError(w, 503, "model test unavailable")
		return
	}
	// runProvider applies the normal workspace scanner, member budgets,
	// spending reservation/settlement, provider wrapper, output scan and trace.
	message, err := h.runProvider(r, cid, providers.Provider(input.Provider), input.Model, []providers.Message{{Role: "user", Content: prompt}}, nil)
	success := err == nil && message != nil && message.Error == nil && strings.TrimSpace(message.Content) != ""
	code := ""
	if err != nil {
		code = modelFailureCode(err)
	} else if !success {
		code = "no_text_reply"
		if message != nil && message.Error != nil {
			code = "workspace_policy_or_limit"
		}
	}
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	_, saveErr := h.store.pool.Exec(saveCtx, `INSERT INTO workspace_model_checks(customer_id,provider,model,credential_fingerprint,success,error_code) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(customer_id,provider,model) DO UPDATE SET credential_fingerprint=excluded.credential_fingerprint,success=excluded.success,error_code=excluded.error_code,checked_at=now()`, cid, input.Provider, input.Model, credentialFingerprint(cid, input.Provider, key), success, code)
	if saveErr != nil {
		writeError(w, 503, "The request finished but its test status could not be saved. Refresh before retrying.")
		return
	}
	h.audit(r, "models.tested", AuditTarget{Type: "model", Label: input.Provider + "/" + input.Model}, map[string]any{"success": success, "error_code": code})
	reply := ""
	if success {
		reply = message.Content
	}
	writeJSON(w, 200, struct {
		Success   bool      `json:"success"`
		Reply     string    `json:"reply"`
		ErrorCode string    `json:"error_code,omitempty"`
		CheckedAt time.Time `json:"checked_at"`
	}{success, reply, code, time.Now().UTC()})
}
