package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/mcp"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/google/uuid"
)

func TestMCPOAuthStateIssuerTenantAndSerializedRefresh(t *testing.T) {
	store := workspaceToolsTestStore(t)
	cid := uuid.New()
	admin := testIdentity(cid, "manager", RoleAdmin)
	if _, err := store.pool.Exec(admin, `INSERT INTO customers(id,name,slug) VALUES($1,'OAuth test',$2)`, cid, cid.String()); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(store, nil, nil)
	cipher, _ := encryption.New("")
	h.SetMCPCredentialEncryption(cipher)
	h.SetMCPOAuthOrigins("https://cloud.example.com", "https://workspace.example.com")
	var tokens, refresh atomic.Int64
	var expectedChallenge string
	h.mcpHTTP = &http.Client{Transport: workspaceMCPTransport(func(r *http.Request) (*http.Response, error) {
		body := `{}`
		code := 200
		headers := http.Header{"Content-Type": {"application/json"}}
		switch r.URL.String() {
		case "https://tools.example.com/mcp":
			code = 401
			headers.Set("WWW-Authenticate", `Bearer resource_metadata="https://tools.example.com/resource",scope="search:read"`)
		case "https://tools.example.com/resource":
			body = `{"resource":"https://tools.example.com","authorization_servers":["https://auth.example.com"]}`
		case "https://auth.example.com/.well-known/oauth-authorization-server":
			body = `{"issuer":"https://auth.example.com","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","response_types_supported":["code"],"code_challenge_methods_supported":["S256"],"authorization_response_iss_parameter_supported":true,"client_id_metadata_document_supported":true}`
		case "https://auth.example.com/token":
			tokens.Add(1)
			raw, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(raw))
			if form.Get("resource") != "https://tools.example.com" || r.Header.Get("Cookie") != "" {
				t.Error("resource binding missing or cookies forwarded")
			}
			if form.Get("grant_type") == "refresh_token" {
				refresh.Add(1)
				if form.Get("refresh_token") != "fixture-refresh" {
					t.Error("refresh token missing")
				}
			} else {
				digest := sha256.Sum256([]byte(form.Get("code_verifier")))
				if base64.RawURLEncoding.EncodeToString(digest[:]) != expectedChallenge {
					t.Error("PKCE verifier mismatch")
				}
			}
			body = `{"access_token":"fixture-access-secret","refresh_token":"fixture-refresh","token_type":"Bearer","expires_in":3600}`
		default:
			t.Errorf("unexpected OAuth request %s", r.URL)
		}
		return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	routes := h.Routes()
	request := func(method, path string, body any, ctx context.Context) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "platform-session=do-not-forward")
		out := httptest.NewRecorder()
		routes.ServeHTTP(out, req)
		return out
	}
	saved := request("POST", "/mcp/connections", map[string]any{"name": "OAuth search", "endpoint": "https://tools.example.com/mcp", "scope": "workspace", "auth_type": "oauth"}, admin)
	if saved.Code != 201 {
		t.Fatalf("save %d %s", saved.Code, saved.Body.String())
	}
	var c MCPConnection
	json.Unmarshal(saved.Body.Bytes(), &c)
	discover := request("POST", "/mcp/connections/"+c.ID.String()+"/oauth/discover", map[string]any{}, admin)
	if discover.Code != 200 {
		t.Fatalf("discovery %s", discover.Body.String())
	}
	var discovered struct {
		Stamp string `json:"discovery_stamp"`
	}
	json.Unmarshal(discover.Body.Bytes(), &discovered)
	start := func() url.Values {
		out := request("POST", "/mcp/connections/"+c.ID.String()+"/oauth/start", map[string]any{"discovery_stamp": discovered.Stamp, "destination": "employee"}, admin)
		if out.Code != 200 {
			t.Fatalf("start %d %s", out.Code, out.Body.String())
		}
		var reply struct {
			URL string `json:"authorization_url"`
		}
		json.Unmarshal(out.Body.Bytes(), &reply)
		u, _ := url.Parse(reply.URL)
		q := u.Query()
		expectedChallenge = q.Get("code_challenge")
		if q.Get("code_challenge_method") != "S256" || q.Get("resource") != "https://tools.example.com" || q.Get("scope") != "search:read" {
			t.Fatal("invalid authorize request")
		}
		return q
	}
	callback := func(q url.Values, issuer string, ctx context.Context) *httptest.ResponseRecorder {
		return request("GET", "/mcp/oauth/callback?"+url.Values{"state": {q.Get("state")}, "code": {"fixture-code"}, "iss": {issuer}}.Encode(), nil, ctx)
	}
	if out := request("POST", "/mcp/connections/"+c.ID.String()+"/oauth/start", map[string]any{"discovery_stamp": discovered.Stamp, "destination": "employee"}, testIdentity(cid, "employee", RoleMember)); out.Code != 403 {
		t.Fatal("employee can authorize shared account")
	}
	q := start()
	if out := callback(q, "https://other.example.com", admin); out.Code != 303 || !strings.Contains(out.Header().Get("Location"), "mcp_oauth=failed") || tokens.Load() != 0 {
		t.Fatal("issuer mix-up sent a code to token endpoint")
	}
	if callback(q, "https://auth.example.com", admin).Code != 403 {
		t.Fatal("OAuth state replayed")
	}
	q = start()
	if callback(q, "https://auth.example.com", testIdentity(uuid.New(), "manager", RoleOwner)).Code != 403 {
		t.Fatal("foreign tenant used OAuth state")
	}
	if callback(q, "https://auth.example.com", testIdentity(cid, "different-manager", RoleAdmin)).Code != 403 {
		t.Fatal("another session user used OAuth state")
	}
	success := callback(q, "https://auth.example.com", admin)
	if success.Code != 303 || success.Header().Get("Location") != "https://workspace.example.com/?mcp_oauth=connected" || tokens.Load() != 1 {
		t.Fatalf("callback failed %d %s", success.Code, success.Header().Get("Location"))
	}
	stored, err := h.loadMCPConnection(admin, cid, c.ID, "manager")
	if err != nil {
		t.Fatal(err)
	}
	if !stored.OAuthConnected || strings.Contains(string(stored.oauth), "fixture-access-secret") {
		t.Fatal("tokens not encrypted")
	}
	listed := request("GET", "/mcp", nil, admin)
	if strings.Contains(listed.Body.String(), "fixture-") {
		t.Fatal("OAuth secrets exposed to browser")
	}
	bundle, err := h.unsealOAuth(cid, c.ID, stored.oauth)
	if err != nil {
		t.Fatal(err)
	}
	bundle.ExpiresAt = time.Now().Add(-time.Minute)
	raw, _ := h.sealOAuth(cid, c.ID, bundle)
	_, _ = store.pool.Exec(admin, `UPDATE workspace_mcp_connections SET encrypted_oauth=$3 WHERE customer_id=$1 AND id=$2`, cid, c.ID, raw)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, e := h.oauthAccessToken(admin, cid, stored)
			if e != nil || token != "fixture-access-secret" {
				t.Errorf("refresh failed %v", e)
			}
		}()
	}
	wg.Wait()
	if refresh.Load() != 1 {
		t.Fatal("concurrent requests reused a rotating refresh token")
	}
	if out := request("POST", "/mcp/connections/"+c.ID.String()+"/oauth/disconnect", map[string]any{}, admin); out.Code != 200 {
		t.Fatal("disconnect failed")
	}
	stored, _ = h.loadMCPConnection(admin, cid, c.ID, "manager")
	if stored.OAuthConnected {
		t.Fatal("OAuth account remained connected")
	}
	bundle, _ = h.unsealOAuth(cid, c.ID, stored.oauth)
	if bundle.Token.AccessToken != "" || bundle.Token.RefreshToken != "" {
		t.Fatal("disconnect retained tokens")
	}
}
func TestMCPOAuthIssuerValidationExactAndMetadataFixed(t *testing.T) {
	d := mcp.OAuthDiscovery{Issuer: "https://auth.example.com", IssParameterSupported: true}
	for _, issuer := range []string{"", "https://auth.example.com/", "https://AUTH.example.com", "https://evil.example.com"} {
		if validOAuthIssuer(d, url.Values{"iss": {issuer}}) {
			t.Fatal("issuer was normalized or omitted")
		}
	}
	if !validOAuthIssuer(d, url.Values{"iss": {d.Issuer}}) {
		t.Fatal("valid issuer denied")
	}
	d.IssParameterSupported = false
	if !validOAuthIssuer(d, url.Values{}) {
		t.Fatal("legacy issuer-less server rejected")
	}
	h := &Handler{}
	h.SetMCPOAuthOrigins("https://cloud.example.com", "https://workspace.example.com")
	out := httptest.NewRecorder()
	h.MCPOAuthClientMetadata(out, httptest.NewRequest("GET", "https://attacker.example.com/v1/mcp/oauth/client-metadata", nil))
	if out.Code != 200 || strings.Contains(out.Body.String(), "attacker") || !strings.Contains(out.Body.String(), "https://cloud.example.com/v1/workspace/mcp/oauth/callback") {
		t.Fatal("untrusted Host influenced callback")
	}
}
