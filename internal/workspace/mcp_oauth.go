package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/internal/mcp"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/google/uuid"
)

type oauthBundle struct {
	Discovery mcp.OAuthDiscovery          `json:"discovery"`
	Client    mcp.OAuthClientRegistration `json:"client"`
	Token     mcp.OAuthToken              `json:"token"`
	ExpiresAt time.Time                   `json:"expires_at"`
}
type oauthFlow struct {
	ConnectionID uuid.UUID   `json:"connection_id"`
	UpdatedAt    time.Time   `json:"updated_at"`
	Verifier     string      `json:"verifier"`
	Bundle       oauthBundle `json:"bundle"`
	Employee     bool        `json:"employee"`
}

func (h *Handler) SetMCPOAuthOrigins(admin, employee string) {
	h.mcpOAuthBase = ""
	h.mcpOAuthEmployeeBase = ""
	for _, entry := range []struct {
		raw    string
		target *string
	}{{admin, &h.mcpOAuthBase}, {employee, &h.mcpOAuthEmployeeBase}} {
		u, err := url.Parse(strings.TrimRight(entry.raw, "/"))
		if err == nil && mcp.ValidateRemoteURL(u.String()) == nil && (u.Path == "" || u.Path == "/") {
			*entry.target = strings.TrimRight(u.String(), "/")
		}
	}
}
func (h *Handler) MCPOAuthClientMetadata(w http.ResponseWriter, r *http.Request) {
	if h.mcpOAuthBase == "" {
		writeError(w, 404, "MCP OAuth is not configured")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, 200, map[string]any{"client_id": h.mcpOAuthBase + "/v1/mcp/oauth/client-metadata", "client_name": "Bastio Workspace", "client_uri": h.mcpOAuthBase, "redirect_uris": []string{h.mcpOAuthBase + "/v1/workspace/mcp/oauth/callback"}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"})
}
func (h *Handler) sealOAuth(cid, id uuid.UUID, b oauthBundle) ([]byte, error) {
	if h.mcpCipher == nil {
		return nil, errors.New("OAuth encryption unavailable")
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	sealed, err := h.mcpCipher.Encrypt(string(raw), cid.String(), "workspace-mcp-oauth:"+id.String())
	if err != nil {
		return nil, err
	}
	return json.Marshal(sealed)
}
func (h *Handler) unsealOAuth(cid, id uuid.UUID, raw []byte) (oauthBundle, error) {
	var b oauthBundle
	var sealed encryption.Encrypted
	if h.mcpCipher == nil || json.Unmarshal(raw, &sealed) != nil {
		return b, errors.New("OAuth account is not connected")
	}
	plain, err := h.mcpCipher.Decrypt(&sealed, cid.String(), "workspace-mcp-oauth:"+id.String())
	if err != nil || json.Unmarshal([]byte(plain), &b) != nil {
		return b, errors.New("OAuth account unavailable; reconnect")
	}
	return b, nil
}
func (h *Handler) editableOAuthConnection(w http.ResponseWriter, r *http.Request) (MCPConnection, bool) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, 400, "invalid integration")
		return MCPConnection{}, false
	}
	c, err := h.loadMCPConnection(r.Context(), customerIDFromCtx(r.Context()), id, userIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, 404, "integration unavailable")
		return c, false
	}
	if c.Scope == "workspace" && !RoleFromCtx(r.Context()).AtLeast(RoleAdmin) {
		writeError(w, 403, "only administrators can connect a shared account")
		return c, false
	}
	if c.Scope == "personal" {
		allowed, e := h.mcpPersonalAllowed(r.Context(), customerIDFromCtx(r.Context()))
		if e != nil || !allowed {
			writeError(w, 403, "personal integrations are disabled")
			return c, false
		}
	}
	if c.AuthType != "oauth" {
		writeError(w, 400, "choose OAuth authentication for this integration")
		return c, false
	}
	if h.mcpOAuthBase == "" || h.mcpCipher == nil {
		writeError(w, 503, "OAuth callback origin is not configured")
		return c, false
	}
	return c, true
}
func oauthDiscoveryStamp(c MCPConnection, d mcp.OAuthDiscovery) string {
	raw, _ := json.Marshal(struct {
		ID        uuid.UUID
		UpdatedAt time.Time
		Discovery mcp.OAuthDiscovery
	}{c.ID, c.UpdatedAt, d})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (h *Handler) discoverMCPOAuth(w http.ResponseWriter, r *http.Request) {
	c, ok := h.editableOAuthConnection(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	d, err := mcp.DiscoverOAuth(ctx, c.Endpoint, h.mcpHTTP)
	if err != nil {
		writeError(w, 422, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"issuer": d.Issuer, "resource": d.Resource, "scopes": d.Scopes, "discovery_stamp": oauthDiscoveryStamp(c, d), "redirect_uri": h.mcpOAuthBase + "/v1/workspace/mcp/oauth/callback"})
}
func randomOAuthValue() string {
	raw := make([]byte, 32)
	_, err := rand.Read(raw)
	if err != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
func (h *Handler) startMCPOAuth(w http.ResponseWriter, r *http.Request) {
	c, ok := h.editableOAuthConnection(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var body struct {
		Stamp       string `json:"discovery_stamp"`
		Destination string `json:"destination"`
	}
	if decodeMCP(r, &body) != nil || body.Stamp == "" || body.Destination != "admin" && body.Destination != "employee" {
		writeError(w, 400, "review the authorization service first")
		return
	}
	if body.Destination == "employee" && h.mcpOAuthEmployeeBase == "" {
		writeError(w, 503, "employee callback destination unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	d, err := mcp.DiscoverOAuth(ctx, c.Endpoint, h.mcpHTTP)
	if err != nil {
		writeError(w, 422, err.Error())
		return
	}
	if oauthDiscoveryStamp(c, d) != body.Stamp {
		writeError(w, 409, "authorization service or scopes changed; review the connection again")
		return
	}
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	bundle := oauthBundle{}
	if len(c.oauth) > 0 {
		bundle, err = h.unsealOAuth(cid, c.ID, c.oauth)
		if err != nil {
			writeError(w, 503, err.Error())
			return
		}
	}
	// Never reuse client credentials registered with a different issuer.
	if bundle.Discovery.Issuer != "" && bundle.Discovery.Issuer != d.Issuer {
		writeError(w, 409, "authorization server changed; save the OAuth client configuration again")
		return
	}
	bundle.Discovery = d
	bundle.Token = mcp.OAuthToken{}
	bundle.ExpiresAt = time.Time{}
	redirect := h.mcpOAuthBase + "/v1/workspace/mcp/oauth/callback"
	if bundle.Client.ClientID == "" {
		bundle.Client, err = mcp.RegisterOAuthClient(ctx, h.mcpHTTP, d, h.mcpOAuthBase+"/v1/mcp/oauth/client-metadata", redirect)
		if err != nil {
			writeError(w, 422, err.Error())
			return
		}
	}
	if bundle.Client.ClientSecret != "" && len(d.TokenAuthMethods) > 0 && !slices.Contains(d.TokenAuthMethods, bundle.Client.AuthMethod) {
		if slices.Contains(d.TokenAuthMethods, "client_secret_post") {
			bundle.Client.AuthMethod = "client_secret_post"
		} else {
			writeError(w, 422, "OAuth service does not support this client authentication method")
			return
		}
	}
	state, verifier := randomOAuthValue(), randomOAuthValue()
	sum := sha256.Sum256([]byte(state))
	challenge := sha256.Sum256([]byte(verifier))
	flow := oauthFlow{ConnectionID: c.ID, UpdatedAt: c.UpdatedAt, Verifier: verifier, Bundle: bundle, Employee: body.Destination == "employee"}
	raw, _ := json.Marshal(flow)
	sealed, err := h.mcpCipher.Encrypt(string(raw), cid.String(), "workspace-mcp-oauth-flow:"+hex.EncodeToString(sum[:]))
	if err != nil {
		writeError(w, 503, "OAuth state encryption failed")
		return
	}
	raw, _ = json.Marshal(sealed)
	// Keep only a bounded number of live flows per user and connection.
	_, _ = h.store.pool.Exec(ctx, `DELETE FROM workspace_mcp_oauth_flows WHERE customer_id=$1 AND user_id=$2 AND connection_id=$3 OR expires_at<now()`, cid, uid, c.ID)
	_, err = h.store.pool.Exec(ctx, `INSERT INTO workspace_mcp_oauth_flows(state_hash,customer_id,user_id,connection_id,encrypted_flow,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '10 minutes')`, hex.EncodeToString(sum[:]), cid, uid, c.ID, raw)
	if err != nil {
		writeError(w, 503, "OAuth state unavailable")
		return
	}
	authorization, _ := url.Parse(d.AuthorizationEndpoint)
	q := authorization.Query()
	q.Set("response_type", "code")
	q.Set("client_id", bundle.Client.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("resource", d.Resource)
	if len(d.Scopes) > 0 {
		q.Set("scope", strings.Join(d.Scopes, " "))
	}
	authorization.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"authorization_url": authorization.String()})
}
func validOAuthIssuer(d mcp.OAuthDiscovery, values url.Values) bool {
	issuer := values.Get("iss")
	return !(d.IssParameterSupported && issuer == "") && (issuer == "" || issuer == d.Issuer) && len(values["iss"]) <= 1
}
func (h *Handler) mcpOAuthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	state := r.URL.Query().Get("state")
	if len(state) != 43 || len(r.URL.Query()["state"]) != 1 || h.mcpCipher == nil {
		writeError(w, 400, "invalid OAuth callback")
		return
	}
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	sum := sha256.Sum256([]byte(state))
	stamp := hex.EncodeToString(sum[:])
	var raw []byte
	// Consume once before exchanging a code; reconnect rather than retry a code.
	err := h.store.pool.QueryRow(r.Context(), `DELETE FROM workspace_mcp_oauth_flows WHERE state_hash=$1 AND customer_id=$2 AND user_id=$3 AND expires_at>now() RETURNING encrypted_flow`, stamp, cid, uid).Scan(&raw)
	if err != nil {
		writeError(w, 403, "OAuth state expired or belongs to another session")
		return
	}
	var sealed encryption.Encrypted
	var flow oauthFlow
	if json.Unmarshal(raw, &sealed) != nil {
		writeError(w, 403, "invalid OAuth state")
		return
	}
	plain, err := h.mcpCipher.Decrypt(&sealed, cid.String(), "workspace-mcp-oauth-flow:"+stamp)
	if err != nil || json.Unmarshal([]byte(plain), &flow) != nil {
		writeError(w, 403, "invalid OAuth state")
		return
	}
	destination := h.mcpOAuthBase + "/workspace?view=integrations"
	if flow.Employee {
		destination = h.mcpOAuthEmployeeBase + "/"
	}
	finish := func(success bool) {
		sep := "&"
		if !strings.Contains(destination, "?") {
			sep = "?"
		}
		value := "failed"
		if success {
			value = "connected"
		}
		http.Redirect(w, r, destination+sep+"mcp_oauth="+value, http.StatusSeeOther)
	}
	// Exact issuer validation applies even to error responses (RFC 9207).
	if !validOAuthIssuer(flow.Bundle.Discovery, r.URL.Query()) {
		finish(false)
		return
	}
	if r.URL.Query().Get("error") != "" {
		finish(false)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 8192 || len(r.URL.Query()["code"]) != 1 {
		finish(false)
		return
	}
	c, err := h.loadMCPConnection(r.Context(), cid, flow.ConnectionID, uid)
	if err != nil || c.AuthType != "oauth" || !c.UpdatedAt.Equal(flow.UpdatedAt) || c.Scope == "workspace" && !RoleFromCtx(r.Context()).AtLeast(RoleAdmin) {
		finish(false)
		return
	}
	if c.Scope == "personal" {
		allowed, e := h.mcpPersonalAllowed(r.Context(), cid)
		if e != nil || !allowed {
			finish(false)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	token, err := mcp.ExchangeOAuthToken(ctx, h.mcpHTTP, flow.Bundle.Discovery, flow.Bundle.Client, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {flow.Verifier}, "redirect_uri": {h.mcpOAuthBase + "/v1/workspace/mcp/oauth/callback"}})
	if err != nil {
		finish(false)
		return
	}
	flow.Bundle.Token = token
	if token.ExpiresIn > 0 {
		flow.Bundle.ExpiresAt = time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	raw, err = h.sealOAuth(cid, c.ID, flow.Bundle)
	if err != nil {
		finish(false)
		return
	}
	tag, err := h.store.pool.Exec(ctx, `UPDATE workspace_mcp_connections SET encrypted_oauth=$3,oauth_connected=TRUE,updated_at=now() WHERE customer_id=$1 AND id=$2 AND updated_at=$4 AND revoked_at IS NULL AND auth_type='oauth'`, cid, c.ID, raw, c.UpdatedAt)
	if err != nil || tag.RowsAffected() != 1 {
		finish(false)
		return
	}
	h.audit(r, "mcp.oauth_connected", AuditTarget{Type: "mcp_connection", ID: c.ID.String()}, nil)
	finish(true)
}
func (h *Handler) disconnectMCPOAuth(w http.ResponseWriter, r *http.Request) {
	c, ok := h.editableOAuthConnection(w, r)
	if !ok {
		return
	}
	cid := customerIDFromCtx(r.Context())
	bundle, err := h.unsealOAuth(cid, c.ID, c.oauth)
	if err != nil {
		bundle = oauthBundle{}
	}
	bundle.Token = mcp.OAuthToken{}
	bundle.ExpiresAt = time.Time{}
	raw, err := h.sealOAuth(cid, c.ID, bundle)
	if err != nil {
		writeError(w, 503, "OAuth encryption unavailable")
		return
	}
	_, err = h.store.pool.Exec(r.Context(), `UPDATE workspace_mcp_connections SET encrypted_oauth=$3,oauth_connected=FALSE,updated_at=now() WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`, cid, c.ID, raw)
	if err != nil {
		writeError(w, 503, "OAuth disconnect failed")
		return
	}
	_, _ = h.store.pool.Exec(r.Context(), `DELETE FROM workspace_mcp_oauth_flows WHERE customer_id=$1 AND connection_id=$2`, cid, c.ID)
	h.audit(r, "mcp.oauth_disconnected", AuditTarget{Type: "mcp_connection", ID: c.ID.String()}, nil)
	writeJSON(w, 200, map[string]any{"disconnected": true})
}
func (h *Handler) oauthAccessToken(ctx context.Context, cid uuid.UUID, c MCPConnection) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := h.store.pool.Begin(ctx)
	if err != nil {
		return "", errors.New("OAuth account unavailable")
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var endpoint string
	var connected bool
	var updated time.Time
	err = tx.QueryRow(ctx, `SELECT encrypted_oauth,endpoint,oauth_connected,updated_at FROM workspace_mcp_connections WHERE customer_id=$1 AND id=$2 AND auth_type='oauth' AND revoked_at IS NULL FOR UPDATE`, cid, c.ID).Scan(&raw, &endpoint, &connected, &updated)
	if err != nil || !connected || !updated.Equal(c.UpdatedAt) {
		return "", errors.New("OAuth account needs reconnection")
	}
	bundle, err := h.unsealOAuth(cid, c.ID, raw)
	if err != nil || bundle.Discovery.Resource != endpoint || bundle.Token.AccessToken == "" {
		return "", errors.New("OAuth account needs reconnection")
	}
	if bundle.ExpiresAt.IsZero() || time.Now().Add(30*time.Second).Before(bundle.ExpiresAt) {
		return bundle.Token.AccessToken, nil
	}
	if bundle.Token.RefreshToken == "" {
		_, _ = tx.Exec(ctx, `UPDATE workspace_mcp_connections SET oauth_connected=FALSE WHERE customer_id=$1 AND id=$2`, cid, c.ID)
		_ = tx.Commit(ctx)
		return "", errors.New("OAuth account expired; reconnect")
	}
	token, err := mcp.ExchangeOAuthToken(ctx, h.mcpHTTP, bundle.Discovery, bundle.Client, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {bundle.Token.RefreshToken}})
	if err != nil {
		_, _ = tx.Exec(ctx, `UPDATE workspace_mcp_connections SET oauth_connected=FALSE WHERE customer_id=$1 AND id=$2`, cid, c.ID)
		_ = tx.Commit(ctx)
		return "", errors.New("OAuth account expired; reconnect")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = bundle.Token.RefreshToken
	}
	bundle.Token = token
	bundle.ExpiresAt = time.Time{}
	if token.ExpiresIn > 0 {
		bundle.ExpiresAt = time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	raw, err = h.sealOAuth(cid, c.ID, bundle)
	if err != nil {
		return "", errors.New("OAuth token encryption unavailable")
	}
	// Refresh changes tokens only. Configuration versions remain stable.
	_, err = tx.Exec(ctx, `UPDATE workspace_mcp_connections SET encrypted_oauth=$3 WHERE customer_id=$1 AND id=$2`, cid, c.ID, raw)
	if err != nil || tx.Commit(ctx) != nil {
		return "", errors.New("OAuth refresh unavailable")
	}
	return token.AccessToken, nil
}
