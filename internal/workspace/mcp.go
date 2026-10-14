package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/internal/mcp"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/google/uuid"
)

type MCPConnection struct {
	AuthType       string `json:"auth_type"`
	OAuthConnected bool   `json:"oauth_connected"`
	oauth          []byte
	Sharing        *ResourceSharing `json:"sharing,omitempty"`
	ID             uuid.UUID        `json:"id"`
	Name           string           `json:"name"`
	Endpoint       string           `json:"endpoint"`
	Scope          string           `json:"scope"`
	OwnerUserID    string           `json:"owner_user_id"`
	AllowedTools   []string         `json:"allowed_tools"`
	HasCredential  bool             `json:"has_credential"`
	UpdatedAt      time.Time        `json:"updated_at"`
	CreatedAt      time.Time        `json:"created_at"`
	credential     []byte
}
type mcpConnectionInput struct {
	AuthType          string           `json:"auth_type,omitempty"`
	OAuthClientID     *string          `json:"oauth_client_id,omitempty"`
	OAuthClientSecret *string          `json:"oauth_client_secret,omitempty"`
	Sharing           *ResourceSharing `json:"sharing,omitempty"`
	Name              string           `json:"name"`
	Endpoint          string           `json:"endpoint"`
	Scope             string           `json:"scope"`
	AllowedTools      []string         `json:"allowed_tools"`
	Credential        *string          `json:"credential,omitempty"`
}
type mcpPolicyResponse struct {
	Connections   []MCPConnection `json:"connections"`
	AllowPersonal bool            `json:"allow_personal"`
	CanManage     bool            `json:"can_manage"`
}

func (h *Handler) SetMCPCredentialEncryption(cipher *encryption.Service) { h.mcpCipher = cipher }
func (h *Handler) mcpPersonalAllowed(ctx context.Context, cid uuid.UUID) (bool, error) {
	var allowed bool
	err := h.store.pool.QueryRow(ctx, `SELECT COALESCE((SELECT allow_personal FROM workspace_mcp_policy WHERE customer_id=$1),TRUE)`, cid).Scan(&allowed)
	return allowed, err
}
func (h *Handler) listMCPConnections(w http.ResponseWriter, r *http.Request) {
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	allowed, err := h.mcpPersonalAllowed(r.Context(), cid)
	if err != nil {
		writeError(w, 500, "integration policy unavailable")
		return
	}
	args := []any{cid, uid}
	filter := accessSQL(r.Context(), &args, "mcp", "c.id")
	rows, err := h.store.pool.Query(r.Context(), `SELECT id,name,endpoint,scope,owner_user_id,allowed_tools,encrypted_credential IS NOT NULL,created_at,updated_at,auth_type,oauth_connected
FROM workspace_mcp_connections c WHERE customer_id=$1 AND revoked_at IS NULL AND ((scope='workspace'`+filter+`) OR (scope='personal' AND owner_user_id=$2)) ORDER BY scope,name`, args...)
	if err != nil {
		writeError(w, 500, "integrations unavailable")
		return
	}
	defer rows.Close()
	items := []MCPConnection{}
	for rows.Next() {
		var c MCPConnection
		if rows.Scan(&c.ID, &c.Name, &c.Endpoint, &c.Scope, &c.OwnerUserID, &c.AllowedTools, &c.HasCredential, &c.CreatedAt, &c.UpdatedAt, &c.AuthType, &c.OAuthConnected) != nil {
			writeError(w, 500, "integrations unavailable")
			return
		}
		items = append(items, c)
	}
	if rows.Err() != nil {
		writeError(w, 500, "integrations unavailable")
		return
	}
	rows.Close()
	if RoleFromCtx(r.Context()).AtLeast(RoleAdmin) {
		for i := range items {
			if items[i].Scope == "workspace" {
				items[i].Sharing, err = h.store.resourceSharing(r.Context(), cid, items[i].ID, "mcp")
				if err != nil {
					writeError(w, 500, "permissions unavailable")
					return
				}
			}
		}
	}
	writeJSON(w, 200, mcpPolicyResponse{items, allowed, RoleFromCtx(r.Context()).AtLeast(RoleAdmin)})
}
func (h *Handler) putMCPPolicy(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var b struct {
		AllowPersonal *bool `json:"allow_personal"`
	}
	if decodeMCP(r, &b) != nil || b.AllowPersonal == nil {
		writeError(w, 400, "allow_personal is required")
		return
	}
	cid := customerIDFromCtx(r.Context())
	_, err := h.store.pool.Exec(r.Context(), `INSERT INTO workspace_mcp_policy(customer_id,allow_personal) VALUES($1,$2)
ON CONFLICT(customer_id) DO UPDATE SET allow_personal=excluded.allow_personal WHERE workspace_mcp_policy.customer_id=$1`, cid, *b.AllowPersonal)
	if err != nil {
		writeError(w, 500, "integration policy update failed")
		return
	}
	h.audit(r, "mcp.policy_updated", AuditTarget{Type: "mcp_policy", ID: cid.String()}, nil)
	writeJSON(w, 200, struct {
		AllowPersonal bool `json:"allow_personal"`
	}{*b.AllowPersonal})
}
func (h *Handler) loadMCPConnection(ctx context.Context, cid, id uuid.UUID, uid string) (MCPConnection, error) {
	var c MCPConnection
	args := []any{cid, id, uid}
	filter := accessSQL(ctx, &args, "mcp", "c.id")
	err := h.store.pool.QueryRow(ctx, `SELECT id,name,endpoint,scope,owner_user_id,allowed_tools,encrypted_credential,encrypted_credential IS NOT NULL,created_at,updated_at,auth_type,oauth_connected,encrypted_oauth
FROM workspace_mcp_connections c WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL AND ((scope='workspace'`+filter+`) OR (scope='personal' AND owner_user_id=$3))`, args...).Scan(&c.ID, &c.Name, &c.Endpoint, &c.Scope, &c.OwnerUserID, &c.AllowedTools, &c.credential, &c.HasCredential, &c.CreatedAt, &c.UpdatedAt, &c.AuthType, &c.OAuthConnected, &c.oauth)
	if err == nil && c.Scope == "workspace" && RoleFromCtx(ctx).AtLeast(RoleAdmin) {
		c.Sharing, err = h.store.resourceSharing(ctx, cid, id, "mcp")
	}
	return c, err
}
func (h *Handler) saveMCPConnection(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var b mcpConnectionInput
	if decodeMCP(r, &b) != nil {
		writeError(w, 400, "invalid integration configuration")
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.AuthType != "" && b.AuthType != "none" && b.AuthType != "bearer" && b.AuthType != "oauth" {
		writeError(w, 400, "invalid authentication method")
		return
	}
	if b.Scope != "personal" && b.Scope != "workspace" || len(b.Name) < 1 || len(b.Name) > 100 || mcp.ValidateRemoteURL(b.Endpoint) != nil || len(b.AllowedTools) > 100 {
		writeError(w, 400, "provide a name, HTTPS MCP endpoint and valid scope")
		return
	}
	for _, name := range b.AllowedTools {
		if !mcp.ValidToolName(name) {
			writeError(w, 400, "invalid allowed tool name")
			return
		}
	}
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	if b.Scope == "workspace" && !RoleFromCtx(r.Context()).AtLeast(RoleAdmin) {
		writeError(w, 403, "only workspace administrators can share integrations")
		return
	}
	allowed, err := h.mcpPersonalAllowed(r.Context(), cid)
	if err != nil || b.Scope == "personal" && !allowed {
		writeError(w, 403, "personal integrations are disabled by your workspace")
		return
	}
	if b.Scope == "personal" && b.Sharing != nil {
		writeError(w, 400, "personal connections cannot have shared recipients")
		return
	}
	tx, err := h.store.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "integration save failed")
		return
	}
	defer tx.Rollback(r.Context())
	id := uuid.New()
	var cipher, oauth []byte
	connected := false
	if r.Method == http.MethodPut {
		id, err = uuidParam(r, "id")
		if err != nil {
			writeError(w, 400, "invalid integration ID")
			return
		}
		var locked uuid.UUID
		if e := tx.QueryRow(r.Context(), `SELECT id FROM workspace_mcp_connections WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL AND (scope='workspace' OR owner_user_id=$3) FOR UPDATE`, cid, id, uid).Scan(&locked); e != nil {
			writeError(w, 404, "integration not found")
			return
		}
		var previous MCPConnection
		if e := tx.QueryRow(r.Context(), `SELECT scope,endpoint,encrypted_credential,auth_type,encrypted_oauth,oauth_connected FROM workspace_mcp_connections WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`, cid, id).Scan(&previous.Scope, &previous.Endpoint, &previous.credential, &previous.AuthType, &previous.oauth, &previous.OAuthConnected); e != nil {
			writeError(w, 404, "integration not found")
			return
		}
		if previous.Scope == "workspace" && !RoleFromCtx(r.Context()).AtLeast(RoleAdmin) {
			writeError(w, 403, "only administrators can edit shared integrations")
			return
		}
		cipher = previous.credential
		oauth = previous.oauth
		connected = previous.OAuthConnected
		if b.AuthType == "" {
			b.AuthType = previous.AuthType
		}
		if previous.Endpoint != b.Endpoint || previous.AuthType != b.AuthType {
			oauth = nil
			connected = false
			cipher = nil
		}
		// Credentials must be explicitly supplied again when changing host,
		// so editing an endpoint cannot redirect an existing token elsewhere.
		if previous.Endpoint != b.Endpoint && b.Credential == nil {
			cipher = nil
		}
	}
	if b.AuthType == "" {
		if b.Credential != nil && *b.Credential != "" {
			b.AuthType = "bearer"
		} else {
			b.AuthType = "none"
		}
	}
	if b.AuthType != "bearer" {
		cipher = nil
	}
	if b.AuthType != "oauth" {
		oauth = nil
		connected = false
	}
	if b.AuthType == "oauth" && (b.OAuthClientID != nil || b.OAuthClientSecret != nil) {
		bundle := oauthBundle{}
		if b.OAuthClientID != nil {
			bundle.Client.ClientID = strings.TrimSpace(*b.OAuthClientID)
		}
		if b.OAuthClientSecret != nil {
			bundle.Client.ClientSecret = *b.OAuthClientSecret
		}
		if bundle.Client.ClientSecret != "" && bundle.Client.ClientID == "" {
			writeError(w, 400, "a client secret requires its pre-registered client ID")
			return
		}
		if len(bundle.Client.ClientID) > 4096 || len(bundle.Client.ClientSecret) > 8192 || strings.ContainsAny(bundle.Client.ClientSecret, "\r\n") {
			writeError(w, 400, "invalid OAuth client configuration")
			return
		}
		bundle.Client.AuthMethod = "none"
		if bundle.Client.ClientSecret != "" {
			bundle.Client.AuthMethod = "client_secret_basic"
		}
		oauth, err = h.sealOAuth(cid, id, bundle)
		if err != nil {
			writeError(w, 503, "OAuth encryption unavailable")
			return
		}
		connected = false
	}
	if b.Credential != nil && b.AuthType == "bearer" {
		if len(*b.Credential) > 8192 || strings.ContainsAny(*b.Credential, "\r\n") {
			writeError(w, 400, "invalid credential")
			return
		}
		cipher = nil
		if *b.Credential != "" {
			if h.mcpCipher == nil {
				writeError(w, 503, "credential encryption is not configured")
				return
			}
			sealed, e := h.mcpCipher.Encrypt(*b.Credential, cid.String(), "workspace-mcp:"+id.String())
			if e != nil {
				writeError(w, 500, "credential encryption failed")
				return
			}
			cipher, _ = json.Marshal(sealed)
		}
	}
	if b.AllowedTools == nil {
		b.AllowedTools = []string{}
	}
	if r.Method == http.MethodPost {
		_, err = tx.Exec(r.Context(), `INSERT INTO workspace_mcp_connections(id,customer_id,owner_user_id,name,endpoint,scope,allowed_tools,encrypted_credential,auth_type,encrypted_oauth,oauth_connected) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, cid, uid, b.Name, b.Endpoint, b.Scope, b.AllowedTools, cipher, b.AuthType, oauth, connected)
	} else {
		_, err = tx.Exec(r.Context(), `UPDATE workspace_mcp_connections SET name=$4,endpoint=$5,scope=$6,allowed_tools=$7,encrypted_credential=$8,auth_type=$9,encrypted_oauth=$10,oauth_connected=$11,owner_user_id=$3,updated_at=now() WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL AND (scope='workspace' OR owner_user_id=$3)`, cid, id, uid, b.Name, b.Endpoint, b.Scope, b.AllowedTools, cipher, b.AuthType, oauth, connected)
	}
	if err != nil {
		writeError(w, 500, "integration save failed")
		return
	}
	if b.Scope == "workspace" && b.Sharing != nil {
		err = setResourceSharing(r.Context(), tx, cid, id, "mcp", *b.Sharing)
	}
	if b.Scope == "personal" {
		err = setResourceSharing(r.Context(), tx, cid, id, "mcp", ResourceSharing{})
	}
	if err != nil {
		if !sharingError(w, err) {
			writeError(w, 500, "integration save failed")
		}
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "integration save failed")
		return
	}

	h.audit(r, "mcp.connection_saved", AuditTarget{Type: "mcp_connection", ID: id.String()}, nil)
	c, err := h.loadMCPConnection(r.Context(), cid, id, uid)
	if err != nil {
		writeError(w, 500, "integration unavailable")
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	writeJSON(w, status, c)
}
func (h *Handler) revokeMCPConnection(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, 400, "invalid integration ID")
		return
	}
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	c, err := h.loadMCPConnection(r.Context(), cid, id, uid)
	if err != nil {
		writeError(w, 404, "integration not found")
		return
	}
	if c.Scope == "workspace" && !RoleFromCtx(r.Context()).AtLeast(RoleAdmin) {
		writeError(w, 403, "only administrators can remove shared integrations")
		return
	}
	_, err = h.store.pool.Exec(r.Context(), `UPDATE workspace_mcp_connections SET revoked_at=now(),encrypted_credential=NULL,encrypted_oauth=NULL,oauth_connected=FALSE WHERE customer_id=$1 AND id=$2 AND revoked_at IS NULL`, cid, id)
	if err != nil {
		writeError(w, 500, "integration removal failed")
		return
	}
	h.audit(r, "mcp.connection_revoked", AuditTarget{Type: "mcp_connection", ID: id.String()}, nil)
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) mcpClient(r *http.Request) (MCPConnection, *mcp.RemoteClient, error) {
	id, err := uuidParam(r, "id")
	if err != nil {
		return MCPConnection{}, nil, err
	}
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	c, err := h.loadMCPConnection(r.Context(), cid, id, uid)
	if err != nil {
		return c, nil, errors.New("integration unavailable")
	}
	return h.mcpClientFor(r.Context(), cid, c)
}
func (h *Handler) mcpClientFor(ctx context.Context, cid uuid.UUID, c MCPConnection) (MCPConnection, *mcp.RemoteClient, error) {
	var err error
	id := c.ID
	if c.Scope == "personal" {
		allowed, e := h.mcpPersonalAllowed(ctx, cid)
		if e != nil || !allowed {
			return c, nil, errors.New("personal integrations are disabled")
		}
	}
	token := ""
	if c.AuthType == "oauth" {
		token, err = h.oauthAccessToken(ctx, cid, c)
		if err != nil {
			return c, nil, err
		}
	} else if c.HasCredential {
		var sealed encryption.Encrypted
		if h.mcpCipher == nil || json.Unmarshal(c.credential, &sealed) != nil {
			return c, nil, errors.New("integration credential unavailable")
		}
		token, err = h.mcpCipher.Decrypt(&sealed, cid.String(), "workspace-mcp:"+id.String())
		if err != nil {
			return c, nil, errors.New("integration credential unavailable")
		}
	}
	client, err := mcp.NewRemoteClient(c.Endpoint, token, h.mcpHTTP)
	return c, client, err
}
func closeMCP(client *mcp.RemoteClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client.Close(ctx)
}
func (h *Handler) protectedMCPTools(ctx context.Context, cid uuid.UUID, c MCPConnection, client *mcp.RemoteClient) ([]mcp.RemoteTool, error) {
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	visible := []mcp.RemoteTool{}
	for _, tool := range tools {
		if len(c.AllowedTools) > 0 && !slices.Contains(c.AllowedTools, tool.Name) {
			continue
		}
		raw, _ := json.Marshal(tool)
		processed, _, e := h.inspectMCPJSON(ctx, cid, raw, security.RoleRetrieval, 0)
		if e != nil {
			continue
		}
		if json.Unmarshal(processed, &tool) == nil {
			visible = append(visible, tool)
		}
	}
	return visible, nil
}
func (h *Handler) listMCPTools(w http.ResponseWriter, r *http.Request) {
	c, client, err := h.mcpClient(r)
	if err != nil {
		writeError(w, 404, "integration unavailable to your account")
		return
	}
	defer closeMCP(client)
	tools, err := h.protectedMCPTools(r.Context(), customerIDFromCtx(r.Context()), c, client)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, struct {
		Tools []mcp.RemoteTool `json:"tools"`
	}{tools})
}
func (h *Handler) callMCPTool(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var b struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if decodeMCP(r, &b) != nil || !mcp.ValidToolName(b.Name) || len(b.Arguments) == 0 || bytes.TrimSpace(b.Arguments)[0] != '{' || !json.Valid(b.Arguments) {
		writeError(w, 400, "provide a tool name and JSON object arguments")
		return
	}
	c, client, err := h.mcpClient(r)
	if err != nil {
		writeError(w, 404, "integration unavailable to your account")
		return
	}
	defer closeMCP(client)
	r, trace := h.beginMCPTrace(w, r, c, b.Name, "manual", uuid.Nil)
	defer trace.complete()
	trace.startStage("guardrail", "mcp.arguments")
	cid := customerIDFromCtx(r.Context())
	// Re-check both access and current allowlist on every invocation.
	decision := mcp.Inspect(r.Context(), nil, mcp.InspectRequest{ToolName: b.Name, Arguments: b.Arguments, Policy: mcp.Policy{AllowedTools: c.AllowedTools}})
	if decision.Action == "blocked" {
		trace.outcome("blocked", "tool_denied", 403, "Tool call blocked by the integration allowlist or argument policy.")
		h.auditMCP(r, c, b.Name, "blocked", decision.Categories)
		writeError(w, 403, "tool call blocked by security policy")
		return
	}
	arguments, categories, err := h.inspectMCPJSON(r.Context(), cid, b.Arguments, security.RoleUser, 0)
	if err != nil {
		trace.inspectionFailure(err, categories, "arguments", 403)
		h.auditMCPInspection(r, c, b.Name, "blocked", categories, err, "arguments")
		writeJSON(w, 403, mcpSecurityFailure(err, categories, "arguments"))
		return
	}
	trace.inspected(categories)
	trace.startStage("span", "mcp.discovery")
	tools, err := h.protectedMCPTools(r.Context(), cid, c, client)
	if err != nil {
		trace.outcome("error", "discovery_failed", 502, "MCP tool discovery failed.")
		writeError(w, 502, err.Error())
		return
	}
	if !slices.ContainsFunc(tools, func(t mcp.RemoteTool) bool { return t.Name == b.Name }) {
		trace.outcome("error", "tool_unavailable", 403, "Tool is unavailable or not allowed.")
		writeError(w, 403, "tool is unavailable or not allowed")
		return
	}
	// Resolve again immediately before the side effect, so revoked sharing
	// or disabled personal access cannot reuse an earlier discovery response.
	current, _, err := h.mcpClient(r)
	if err != nil || current.Endpoint != c.Endpoint || !bytes.Equal(current.credential, c.credential) || len(current.AllowedTools) > 0 && !slices.Contains(current.AllowedTools, b.Name) {
		trace.outcome("error", "access_changed", 403, "Integration access changed before execution.")
		writeError(w, 403, "integration access changed; refresh before running")
		return
	}
	trace.startStage("tool", "mcp."+b.Name)
	trace.trace.Tags["remote_action"] = "unknown"
	raw, err := client.CallTool(r.Context(), b.Name, arguments)
	if err != nil {
		trace.outcome("error", "remote_error", 502, "MCP request failed; the remote action may have completed.")
		h.auditMCP(r, c, b.Name, "error", categories)
		writeError(w, 502, "MCP tool request failed. The remote action may already have completed; check that service before retrying.")
		return
	}
	trace.trace.Tags["remote_action"] = "completed"
	trace.startStage("guardrail", "mcp.result")
	processed, resultCategories, err := h.inspectMCPJSON(r.Context(), cid, raw, security.RoleRetrieval, 0)
	categories = append(categories, resultCategories...)
	if err != nil {
		trace.inspectionFailure(err, resultCategories, "result", 403)
		h.auditMCPInspection(r, c, b.Name, "result_blocked", resultCategories, err, "result")
		writeJSON(w, 403, mcpSecurityFailure(err, resultCategories, "result"))
		return
	}
	trace.inspected(resultCategories)
	// Return text/structured content only. Text inspection cannot protect
	// image pixels or embedded binary resources, so don't expose those.
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent,omitempty"`
		IsError    bool            `json:"isError"`
	}
	if json.Unmarshal(processed, &result) != nil {
		trace.outcome("error", "unsupported_result", 502, "The tool returned an unsupported result.")
		writeError(w, 502, "unsupported MCP result")
		return
	}
	var text []string
	for _, part := range result.Content {
		if part.Type == "text" {
			text = append(text, part.Text)
		}
	}
	if len(result.Structured) > 0 {
		text = append(text, string(result.Structured))
	}
	if len(text) == 0 {
		trace.outcome("error", "unsupported_result", 422, "The tool returned no supported text content.")
		writeError(w, 422, "this tool returned no supported text content")
		return
	}
	if categories == nil {
		categories = []string{}
	}
	trace.outcome("ok", "completed", 200, "")
	if result.IsError {
		trace.outcome("error", "tool_error", 200, "The remote tool returned isError=true.")
	}
	h.auditMCP(r, c, b.Name, "completed", categories)
	writeJSON(w, 200, struct {
		Text       string   `json:"text"`
		IsError    bool     `json:"is_error"`
		Categories []string `json:"categories"`
	}{strings.Join(text, "\n"), result.IsError, categories})
}
func (h *Handler) auditMCP(r *http.Request, c MCPConnection, tool, action string, categories []string) {
	// No arguments, outputs, endpoint tokens or content excerpts are retained.
	body, _ := json.Marshal(map[string]any{"tool": tool, "categories": categories, "trace_id": mcpAuditTraceID(r)})
	h.auditJSON(r, "mcp.tool_"+action, AuditTarget{Type: "mcp_connection", ID: c.ID.String()}, body)
}
func mustJSONString(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
func mustJSONStrings(s []string) json.RawMessage {
	if s == nil {
		s = []string{}
	}
	b, _ := json.Marshal(s)
	return b
}

type mcpProfileSnapshot struct {
	customerID uuid.UUID
	profile    *security.Profile
}

func (p mcpProfileSnapshot) GetDefault(_ context.Context, cid uuid.UUID) (*security.Profile, error) {
	if cid != p.customerID {
		return nil, errors.New("profile tenant mismatch")
	}
	return p.profile, nil
}
func (h *Handler) inspectMCPJSON(ctx context.Context, cid uuid.UUID, raw json.RawMessage, role string, depth int) (json.RawMessage, []string, error) {
	if h.secEngine == nil || h.secProfiles == nil {
		return nil, nil, errors.New("mcp security unavailable")
	}
	profile, err := h.secProfiles.GetDefault(ctx, cid)
	if err != nil || profile == nil {
		return nil, nil, errors.New("mcp security unavailable")
	}
	return inspectMCPPayload(ctx, h.secEngine, mcpProfileSnapshot{cid, profile}, cid, raw, role, depth)
}
func inspectMCPPayload(ctx context.Context, engine *security.Engine, profiles security.ProfileLookup, cid uuid.UUID, raw json.RawMessage, role string, depth int) (json.RawMessage, []string, error) {
	if depth > 24 || len(raw) > 2<<20 {
		return nil, nil, errors.New("mcp content exceeds inspection limits")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil, errors.New("invalid MCP content")
	}
	var categories []string
	switch raw[0] {
	case '"':
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return nil, nil, errors.New("invalid MCP text")
		}
		decision, err := scanWorkspaceContent(ctx, engine, profiles, cid, text, role, true)
		if err != nil || decision == nil {
			return nil, nil, errors.New("mcp security unavailable")
		}
		if trace := mcpTraceFromContext(ctx); trace != nil {
			trace.captureScan(cid, decision.Result)
		}
		if decision.Action == "block" {
			return nil, decision.Categories, mcpBlockedInspection(decision, "policy_block", "The workspace security policy blocks this content.")
		}
		if decision.Action == "sanitize" {
			text = decision.SanitizedContent
		}
		out, _ := json.Marshal(text)
		return out, decision.Categories, nil
	case '{':
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return nil, nil, errors.New("invalid MCP object")
		}
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			value := fields[key]
			protectedKey, cats, err := inspectMCPPayload(ctx, engine, profiles, cid, mustJSONString(key), role, depth+1)
			categories = append(categories, cats...)
			if err != nil {
				return nil, categories, err
			}
			if !bytes.Equal(protectedKey, mustJSONString(key)) {
				return nil, categories, errors.New("mcp field name blocked")
			}
			processed, cats, err := inspectMCPPayload(ctx, engine, profiles, cid, value, role, depth+1)
			categories = append(categories, cats...)
			if err != nil {
				return nil, categories, err
			}
			fields[key] = processed
		}
		out, _ := json.Marshal(fields)
		return out, categories, nil
	case '[':
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return nil, nil, errors.New("invalid MCP array")
		}
		for i, value := range values {
			processed, cats, err := inspectMCPPayload(ctx, engine, profiles, cid, value, role, depth+1)
			categories = append(categories, cats...)
			if err != nil {
				return nil, categories, err
			}
			values[i] = processed
		}
		out, _ := json.Marshal(values)
		return out, categories, nil
	default:
		if !json.Valid(raw) {
			return nil, nil, fmt.Errorf("invalid MCP value")
		}
		if bytes.Equal(raw, []byte("true")) || bytes.Equal(raw, []byte("false")) || bytes.Equal(raw, []byte("null")) {
			return raw, categories, nil
		}
		// Personal identifiers can be JSON numbers too. Rewriting a number
		// into a string changes the tool schema, so withhold it on sanitize.
		decision, err := scanWorkspaceContent(ctx, engine, profiles, cid, string(raw), role, true)
		if err != nil || decision == nil {
			return nil, nil, errors.New("mcp security unavailable")
		}
		if trace := mcpTraceFromContext(ctx); trace != nil {
			trace.captureScan(cid, decision.Result)
		}
		if decision.Action == "block" {
			return nil, decision.Categories, mcpBlockedInspection(decision, "policy_block", "The workspace security policy blocks this content.")
		}
		if decision.Action == "sanitize" {
			return nil, decision.Categories, mcpBlockedInspection(decision, "numeric_rewrite", "A detected value is a JSON number. Masking or tokenizing it would change its data type, so it was withheld.")
		}
		return raw, decision.Categories, nil
	}
}

func decodeMCP[T any](r *http.Request, body *T) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(body); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("only one JSON body is permitted")
	}
	return nil
}
