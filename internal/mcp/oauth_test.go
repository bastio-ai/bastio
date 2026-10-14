package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthDiscoveryIssuerPKCEAndResourceBinding(t *testing.T) {
	issuer := "https://auth.example.com/tenant"
	badIssuer := false
	pkce := true
	resource := "https://tools.example.com/mcp"
	tokenCalls := 0
	transport := remoteTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://tools.example.com/mcp":
			resp := remoteResponse(401, `{}`, "application/json")
			resp.Header.Set("WWW-Authenticate", `Bearer resource_metadata="https://tools.example.com/resource.json", scope="search:read documents:read"`)
			return resp, nil
		case "https://tools.example.com/resource.json":
			raw, _ := json.Marshal(map[string]any{"resource": resource, "authorization_servers": []string{issuer}, "scopes_supported": []string{"ignored"}})
			return remoteResponse(200, string(raw), "application/json"), nil
		case "https://auth.example.com/.well-known/oauth-authorization-server/tenant":
			name := issuer
			if badIssuer {
				name = "https://other.example.com"
			}
			methods := []string{"S256"}
			if !pkce {
				methods = []string{"plain"}
			}
			raw, _ := json.Marshal(map[string]any{"issuer": name, "authorization_endpoint": "https://auth.example.com/authorize", "token_endpoint": "https://auth.example.com/token", "response_types_supported": []string{"code"}, "code_challenge_methods_supported": methods, "client_id_metadata_document_supported": true, "authorization_response_iss_parameter_supported": true})
			return remoteResponse(200, string(raw), "application/json"), nil
		case "https://auth.example.com/token":
			tokenCalls++
			raw, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(raw))
			if values.Get("resource") != "https://tools.example.com/mcp" || values.Get("code_verifier") != "fixture-verifier" || values.Get("client_id") == "" {
				t.Error("token request lost resource, client or PKCE")
			}
			if r.Header.Get("Cookie") != "" {
				t.Error("platform cookie forwarded")
			}
			return remoteResponse(200, `{"access_token":"fixture-access","refresh_token":"fixture-refresh","token_type":"Bearer","expires_in":3600}`, "application/json"), nil
		}
		t.Errorf("unexpected request %s", r.URL)
		return remoteResponse(404, `{}`, "application/json"), nil
	})
	client := &http.Client{Transport: transport}
	discovery, err := DiscoverOAuth(context.Background(), "https://tools.example.com/mcp", client)
	if err != nil || discovery.Issuer != issuer || strings.Join(discovery.Scopes, " ") != "search:read documents:read" || !discovery.IssParameterSupported {
		t.Fatalf("discovery %v %+v", err, discovery)
	}
	registration, err := RegisterOAuthClient(context.Background(), client, discovery, "https://bastio.example.com/v1/mcp/oauth/client-metadata", "https://bastio.example.com/v1/workspace/mcp/oauth/callback")
	if err != nil || registration.AuthMethod != "none" {
		t.Fatal("metadata client registration failed")
	}
	token, err := ExchangeOAuthToken(context.Background(), client, discovery, registration, url.Values{"grant_type": {"authorization_code"}, "code": {"fixture-code"}, "code_verifier": {"fixture-verifier"}})
	if err != nil || token.AccessToken != "fixture-access" || tokenCalls != 1 {
		t.Fatal("token exchange failed")
	}
	badIssuer = true
	if _, err = DiscoverOAuth(context.Background(), "https://tools.example.com/mcp", client); err == nil {
		t.Fatal("accepted mixed-up issuer")
	}
	badIssuer = false
	pkce = false
	if _, err = DiscoverOAuth(context.Background(), "https://tools.example.com/mcp", client); err == nil {
		t.Fatal("accepted PKCE downgrade")
	}
	pkce = true
	resource = "https://other.example.com/mcp"
	if _, err = DiscoverOAuth(context.Background(), "https://tools.example.com/mcp", client); err == nil {
		t.Fatal("accepted resource mismatch")
	}
	discovery.TokenEndpoint = "https://169.254.169.254/token"
	if _, err = ExchangeOAuthToken(context.Background(), client, discovery, registration, url.Values{}); err == nil || tokenCalls != 1 {
		t.Fatal("private token endpoint accepted")
	}
}
func TestOAuthNoRedirectsAndNoSecretErrorBodies(t *testing.T) {
	client := &http.Client{Transport: remoteTransport(func(*http.Request) (*http.Response, error) {
		r := remoteResponse(302, `{"error":"fixture-secret"}`, "application/json")
		r.Header.Set("Location", "https://localhost/token")
		return r, nil
	})}
	var result any
	err := oauthJSON(context.Background(), client, "GET", "https://auth.example.com/metadata", nil, nil, &result)
	if err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatal("redirect followed or error body leaked")
	}
	for _, endpoint := range []string{"http://auth.example.com/token", "https://localhost/token", "https://auth.example.com/token#fragment", "https://user:secret@auth.example.com/token"} {
		if validateOAuthURL(endpoint) == nil {
			t.Fatal("unsafe OAuth URL")
		}
	}
}
