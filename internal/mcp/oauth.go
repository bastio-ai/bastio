package mcp

// OAuth requests use the same pinned, public-HTTPS-only transport as MCP.
// Credentials are scoped to one authorization server and one MCP resource.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

type OAuthDiscovery struct {
	Resource                string   `json:"resource"`
	Issuer                  string   `json:"issuer"`
	AuthorizationEndpoint   string   `json:"authorization_endpoint"`
	TokenEndpoint           string   `json:"token_endpoint"`
	RegistrationEndpoint    string   `json:"registration_endpoint,omitempty"`
	Scopes                  []string `json:"scopes"`
	MetadataClientSupported bool     `json:"client_id_metadata_document_supported"`
	IssParameterSupported   bool     `json:"authorization_response_iss_parameter_supported"`
	TokenAuthMethods        []string `json:"token_endpoint_auth_methods_supported"`
}
type OAuthClientRegistration struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	AuthMethod   string `json:"token_endpoint_auth_method"`
}
type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// A protected resource can represent the server root or a parent path (Notion
// uses https://mcp.notion.com for its /mcp endpoint). Bind tokens to that declared
// resource, but only accept the configured endpoint's HTTPS origin and a path
// ancestor on a segment boundary. Credentials still go solely to the saved MCP
// endpoint; this never enables forwarding to another host or sibling path.
func OAuthResourceCoversEndpoint(resource, endpoint string) bool {
	if ValidateRemoteURL(resource) != nil || ValidateRemoteURL(endpoint) != nil {
		return false
	}
	r, _ := url.Parse(resource)
	e, _ := url.Parse(endpoint)
	for _, u := range []*url.URL{r, e} {
		for _, part := range strings.Split(u.Path, "/") {
			if part == "." || part == ".." {
				return false
			}
		}
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		return "443"
	}
	if !strings.EqualFold(r.Hostname(), e.Hostname()) || port(r) != port(e) {
		return false
	}
	path := strings.TrimSuffix(r.EscapedPath(), "/")
	return path == "" || e.EscapedPath() == path || strings.HasPrefix(e.EscapedPath(), path+"/")
}

func validateOAuthURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid OAuth URL")
	}
	clean := *u
	clean.RawQuery = ""
	return ValidateRemoteURL(clean.String())
}
func OAuthHTTP(client *http.Client) *http.Client {
	if client == nil {
		return secureMCPHTTP()
	}
	// An injected transport is operator-owned (used by tests); redirects remain forbidden.
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &copy
}
func oauthJSON(ctx context.Context, client *http.Client, method, endpoint string, body []byte, headers http.Header, out any) error {
	if validateOAuthURL(endpoint) != nil {
		return errors.New("OAuth endpoint must use public HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("OAuth request invalid")
	}
	for k, values := range headers {
		req.Header[k] = values
	}
	resp, err := OAuthHTTP(client).Do(req)
	if err != nil {
		return errors.New("OAuth server unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("OAuth server rejected the request")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return errors.New("OAuth response exceeds limit")
	}
	if json.Unmarshal(raw, out) != nil {
		return errors.New("OAuth server returned invalid metadata")
	}
	return nil
}

var authParameterRE = regexp.MustCompile(`(?i)([a-z_]+)\s*=\s*(?:"([^"\r\n]*)"|([^,\s]+))`)

func challengeParameters(header string) map[string]string {
	result := map[string]string{}
	if !strings.Contains(strings.ToLower(header), "bearer ") {
		return result
	}
	for _, match := range authParameterRE.FindAllStringSubmatch(header, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		result[strings.ToLower(match[1])] = value
	}
	return result
}
func DiscoverOAuth(ctx context.Context, endpoint string, client *http.Client) (OAuthDiscovery, error) {
	var out OAuthDiscovery
	if ValidateRemoteURL(endpoint) != nil {
		return out, errors.New("invalid MCP endpoint")
	}
	probe, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":"oauth-discovery","method":"tools/list","params":{}}`))
	if err != nil {
		return out, err
	}
	probe.Header.Set("Content-Type", "application/json")
	probe.Header.Set("Accept", "application/json, text/event-stream")
	probe.Header.Set("MCP-Protocol-Version", "2026-07-28")
	resp, err := OAuthHTTP(client).Do(probe)
	if err != nil {
		return out, errors.New("MCP server unreachable")
	}
	params := challengeParameters(strings.Join(resp.Header.Values("WWW-Authenticate"), ","))
	resp.Body.Close()
	u, _ := url.Parse(endpoint)
	origin := u.Scheme + "://" + u.Host
	candidates := []string{}
	if params["resource_metadata"] != "" {
		candidates = append(candidates, params["resource_metadata"])
	} else {
		if u.Path != "" && u.Path != "/" {
			candidates = append(candidates, origin+"/.well-known/oauth-protected-resource"+u.Path)
		}
		candidates = append(candidates, origin+"/.well-known/oauth-protected-resource")
	}
	var resource struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		Scopes               []string `json:"scopes_supported"`
	}
	discovered := false
	for _, candidate := range candidates {
		if oauthJSON(ctx, client, "GET", candidate, nil, nil, &resource) == nil {
			discovered = true
			break
		}
	}
	if !discovered || len(resource.AuthorizationServers) == 0 || len(resource.AuthorizationServers) > 8 || !OAuthResourceCoversEndpoint(resource.Resource, endpoint) {
		return out, errors.New("MCP OAuth resource metadata missing or resource mismatch")
	}
	issuer := resource.AuthorizationServers[0]
	if ValidateRemoteURL(issuer) != nil {
		return out, errors.New("invalid authorization server issuer")
	}
	as, _ := url.Parse(issuer)
	asOrigin := as.Scheme + "://" + as.Host
	path := strings.TrimSuffix(as.Path, "/")
	urls := []string{asOrigin + "/.well-known/oauth-authorization-server" + path, asOrigin + "/.well-known/openid-configuration" + path}
	if path != "" {
		urls = append(urls, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration")
	}
	var metadata struct {
		OAuthDiscovery
		ChallengeMethods []string `json:"code_challenge_methods_supported"`
		ResponseTypes    []string `json:"response_types_supported"`
	}
	discovered = false
	for _, candidate := range urls {
		metadata = struct {
			OAuthDiscovery
			ChallengeMethods []string `json:"code_challenge_methods_supported"`
			ResponseTypes    []string `json:"response_types_supported"`
		}{}
		if oauthJSON(ctx, client, "GET", candidate, nil, nil, &metadata) == nil {
			if metadata.Issuer != issuer {
				return out, errors.New("authorization server issuer mismatch")
			}
			discovered = true
			break
		}
	}
	if !discovered || !slices.Contains(metadata.ChallengeMethods, "S256") || !slices.Contains(metadata.ResponseTypes, "code") {
		return out, errors.New("authorization server must support authorization code and PKCE S256")
	}
	out = metadata.OAuthDiscovery
	out.Resource = resource.Resource
	if validateOAuthURL(out.AuthorizationEndpoint) != nil || validateOAuthURL(out.TokenEndpoint) != nil || out.RegistrationEndpoint != "" && validateOAuthURL(out.RegistrationEndpoint) != nil {
		return OAuthDiscovery{}, errors.New("invalid OAuth server endpoints")
	}
	out.Scopes = resource.Scopes
	if scope := params["scope"]; scope != "" {
		out.Scopes = strings.Fields(scope)
	}
	if len(strings.Join(out.Scopes, " ")) > 2048 || len(out.Scopes) > 64 {
		return OAuthDiscovery{}, errors.New("too many OAuth scopes")
	}
	return out, nil
}
func RegisterOAuthClient(ctx context.Context, client *http.Client, discovery OAuthDiscovery, metadataURL, redirectURL string) (OAuthClientRegistration, error) {
	if discovery.MetadataClientSupported {
		return OAuthClientRegistration{ClientID: metadataURL, AuthMethod: "none"}, nil
	}
	if discovery.RegistrationEndpoint == "" {
		return OAuthClientRegistration{}, errors.New("this service requires a pre-registered OAuth client ID")
	}
	body, _ := json.Marshal(map[string]any{"client_name": "Bastio Workspace", "redirect_uris": []string{redirectURL}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"})
	var result OAuthClientRegistration
	err := oauthJSON(ctx, client, "POST", discovery.RegistrationEndpoint, body, http.Header{"Content-Type": []string{"application/json"}}, &result)
	if err != nil || result.ClientID == "" || len(result.ClientID) > 4096 {
		return OAuthClientRegistration{}, errors.New("OAuth client registration failed; supply a pre-registered client ID")
	}
	if result.AuthMethod == "" {
		result.AuthMethod = "none"
	}
	if result.AuthMethod != "none" && result.AuthMethod != "client_secret_basic" && result.AuthMethod != "client_secret_post" {
		return OAuthClientRegistration{}, errors.New("unsupported OAuth client authentication")
	}
	return result, nil
}
func ExchangeOAuthToken(ctx context.Context, client *http.Client, d OAuthDiscovery, registration OAuthClientRegistration, values url.Values) (OAuthToken, error) {
	values.Set("client_id", registration.ClientID)
	values.Set("resource", d.Resource)
	headers := http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}}
	if registration.AuthMethod == "client_secret_post" {
		values.Set("client_secret", registration.ClientSecret)
	}
	if registration.AuthMethod == "client_secret_basic" {
		req, _ := http.NewRequest("POST", "https://example.com", nil)
		req.SetBasicAuth(url.QueryEscape(registration.ClientID), url.QueryEscape(registration.ClientSecret))
		headers.Set("Authorization", req.Header.Get("Authorization"))
		values.Del("client_id")
	}
	var token OAuthToken
	err := oauthJSON(ctx, client, "POST", d.TokenEndpoint, []byte(values.Encode()), headers, &token)
	if err != nil || token.AccessToken == "" || len(token.AccessToken) > 32<<10 || len(token.RefreshToken) > 32<<10 || token.ExpiresIn < 0 || token.ExpiresIn > 366*24*60*60 || !strings.EqualFold(token.TokenType, "Bearer") || strings.ContainsAny(token.AccessToken, "\r\n") {
		return OAuthToken{}, errors.New("OAuth token exchange failed; reconnect the account")
	}
	return token, nil
}
