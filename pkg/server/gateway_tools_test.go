package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/proxy"
	"github.com/bastio-ai/bastio/internal/workspace"
	"github.com/bastio-ai/bastio/pkg/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type toolStore struct{ p *proxy.Proxy }

func (s toolStore) GetByID(_ context.Context, cid, id uuid.UUID) (*proxy.Proxy, error) {
	return s.p, nil
}

type toolKeys struct {
	calls    int
	cid, pid uuid.UUID
}

func (k *toolKeys) WithTemporaryGatewayKey(_ context.Context, cid, pid uuid.UUID, fn func(string)) error {
	k.calls++
	k.cid, k.pid = cid, pid
	fn("sk-bastio-test-only")
	return nil
}

func TestGatewayToolsUseScopedServerCredentialsAndNormalPipeline(t *testing.T) {
	for _, provider := range []providers.Provider{providers.ProviderOpenAI, providers.ProviderAnthropic, providers.ProviderGemini} {
		t.Run(string(provider), func(t *testing.T) {
			p := &proxy.Proxy{ID: uuid.New(), CustomerID: uuid.New(), TargetProvider: provider, IsActive: true}
			keys := &toolKeys{}
			called := false
			g := &gatewayTools{proxies: toolStore{p}, keys: keys, pipeline: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Header.Get("Authorization") != "Bearer sk-bastio-test-only" || r.Header.Get("Cookie") != "" || r.Header.Get("OpenAI-Project") != "" {
					t.Fatal("credentials or untrusted headers leaked")
				}
				if r.Header.Get("X-Bastio-Proxy-Id") != p.ID.String() || r.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("scope/cache metadata missing")
				}
				expected := "/v1/chat/completions"
				if provider == providers.ProviderOpenAI {
					expected = "/v1/guard/" + p.ID.String() + "/v1/chat/completions"
				}
				if provider == providers.ProviderAnthropic {
					expected = "/v1/guard/" + p.ID.String() + "/v1/messages"
				}
				if r.URL.Path != expected {
					t.Fatalf("route %s", r.URL.Path)
				}
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if body["model"] != "gpt-6-luna" || body["stream"] != false {
					t.Fatal("model or streaming changed")
				}
				messages := body["messages"].([]any)
				if len(messages) != 1 || messages[0].(map[string]any)["content"] != gatewayHelloPrompt {
					t.Fatal("arbitrary prompt")
				}
				tokenField := "max_tokens"
				if provider == providers.ProviderOpenAI {
					tokenField = "max_completion_tokens"
					if body["reasoning_effort"] != "none" {
						t.Fatal("bounded hello must avoid reasoning-only output")
					}
				}
				if body[tokenField] != float64(512) {
					t.Fatal("test not bounded")
				}
				// The normal gateway may reject for quota or security. Its verdict must
				// reach the dashboard, rather than calling a provider around middleware.
				w.WriteHeader(403)
				io.WriteString(w, `{"error":{"type":"security_blocked","message":"Policy denied"}}`)
			})}
			r := chi.NewRouter()
			r.Mount("/proxies", chi.NewRouter())
			r.With(workspace.RequireRole(workspace.RoleAdmin)).Post("/proxies/{id}/test", g.Test)
			req := httptest.NewRequest("POST", "https://cloud.bastio.com/proxies/"+p.ID.String()+"/test", strings.NewReader(`{"model":" gpt-6-luna "}`))
			req.Header.Set("Origin", "https://cloud.bastio.com")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Cookie", "bastio_session=private")
			req.Header.Set("OpenAI-Project", "attacker")
			req = req.WithContext(workspace.WithRole(tenant.WithID(req.Context(), p.CustomerID), workspace.RoleOwner))
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if !called || keys.calls != 1 || keys.cid != p.CustomerID || keys.pid != p.ID || out.Code != 403 || !strings.Contains(out.Body.String(), "security_blocked") {
				t.Fatalf("normal pipeline not preserved: %d %s", out.Code, out.Body.String())
			}
		})
	}
}

func TestGatewayToolsRejectUnsafeRequestsBeforeIssuingKey(t *testing.T) {
	p := &proxy.Proxy{ID: uuid.New(), CustomerID: uuid.New(), TargetProvider: providers.ProviderOpenAI, IsActive: true}
	for _, tc := range []struct {
		name, origin, body string
		role               workspace.Role
		cid                uuid.UUID
		status             int
	}{
		{"employee", "https://cloud.bastio.com", `{"model":"gpt-test"}`, workspace.RoleMember, p.CustomerID, 403},
		{"cross tenant", "https://cloud.bastio.com", `{"model":"gpt-test"}`, workspace.RoleAdmin, uuid.New(), 404},
		{"missing tenant", "https://cloud.bastio.com", `{"model":"gpt-test"}`, workspace.RoleAdmin, uuid.Nil, 401},
		{"cross origin", "https://evil.test", `{"model":"gpt-test"}`, workspace.RoleAdmin, p.CustomerID, 403},
		{"origin query", "https://cloud.bastio.com?evil=1", `{"model":"gpt-test"}`, workspace.RoleAdmin, p.CustomerID, 403},
		{"missing origin", "", `{"model":"gpt-test"}`, workspace.RoleAdmin, p.CustomerID, 403},
		{"arbitrary prompt", "https://cloud.bastio.com", `{"model":"gpt-test","messages":[]}`, workspace.RoleAdmin, p.CustomerID, 400},
		{"extra JSON", "https://cloud.bastio.com", `{"model":"gpt-test"}{}`, workspace.RoleAdmin, p.CustomerID, 400},
		{"empty model", "https://cloud.bastio.com", `{"model":" "}`, workspace.RoleAdmin, p.CustomerID, 400},
		{"oversized body", "https://cloud.bastio.com", `{"model":"` + strings.Repeat("a", 2100) + `"}`, workspace.RoleAdmin, p.CustomerID, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := &toolKeys{}
			g := &gatewayTools{proxies: toolStore{p}, keys: keys, pipeline: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unsafe request reached gateway") })}
			router := chi.NewRouter()
			router.With(workspace.RequireRole(workspace.RoleAdmin)).Post("/proxies/{id}/test", g.Test)
			req := httptest.NewRequest("POST", "https://cloud.bastio.com/proxies/"+p.ID.String()+"/test", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", tc.origin)
			req = req.WithContext(workspace.WithRole(tenant.WithID(req.Context(), tc.cid), tc.role))
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.status || keys.calls != 0 {
				t.Fatalf("got %d and %d issued keys", out.Code, keys.calls)
			}
		})
	}
}

func TestGatewayModelDiscoveryUsesSamePipelineAndFiltersNonChat(t *testing.T) {
	p := &proxy.Proxy{ID: uuid.New(), CustomerID: uuid.New(), TargetProvider: providers.ProviderOpenAI, IsActive: true}
	keys := &toolKeys{}
	g := &gatewayTools{proxies: toolStore{p}, keys: keys, native: func(providers.Provider) bool { return true }, pipeline: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/guard/"+p.ID.String()+"/v1/models" || r.Header.Get("Authorization") == "" {
			t.Fatal("wrong model discovery route")
		}
		io.WriteString(w, `{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o-mini"},{"id":"text-embedding-3-small"},{"id":"gpt-realtime"},{"id":"o3-mini"}]}`)
	})}
	router := chi.NewRouter()
	router.Get("/proxies/{id}/models", g.Models)
	req := httptest.NewRequest("GET", "/proxies/"+p.ID.String()+"/models", nil)
	req = req.WithContext(tenant.WithID(req.Context(), p.CustomerID))
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	var listing gatewayModelListing
	json.Unmarshal(out.Body.Bytes(), &listing)
	if out.Code != 200 || len(listing.Models) != 2 || listing.Source != "provider" || keys.calls != 1 || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("discovery: %d %s", out.Code, out.Body.String())
	}
	g.native = func(providers.Provider) bool { return false }
	out = httptest.NewRecorder()
	router.ServeHTTP(out, req)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"source":"catalog"`) || keys.calls != 1 {
		t.Fatal("unsupported discovery should be explicit and not issue keys")
	}
}

func TestGatewayHelloUsesSupportedReasoningControls(t *testing.T) {
	for model, want := range map[string]string{
		"gpt-5-nano": "minimal", "gpt-5-nano-2025-08-07": "minimal", "gpt-5-mini": "minimal", "gpt-5": "minimal",
		"gpt-6-luna": "none", "gpt-6-sol": "none", "gpt-6.1-sol": "low", "gpt-6-astra": "low",
		"gpt-4o-mini": "", "gpt-unknown": "", "gpt-5-nano-unrelated": "",
	} {
		if got := gatewayHelloReasoningEffort(model); got != want {
			t.Fatalf("%s effort %s want %s", model, got, want)
		}
	}
}
