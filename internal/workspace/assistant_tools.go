package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/internal/mcp"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type plannedTool struct {
	Alias          string         `json:"alias"`
	ConnectionID   uuid.UUID      `json:"connection_id"`
	ConnectionName string         `json:"connection_name"`
	UpdatedAt      time.Time      `json:"updated_at"`
	Tool           mcp.RemoteTool `json:"tool"`
}
type assistantToolPlan struct {
	Provider           providers.Provider `json:"provider"`
	Model              string             `json:"model"`
	Tools              []plannedTool      `json:"tools"`
	ConnectionIDs      []uuid.UUID        `json:"connection_ids"`
	AssistantID        *uuid.UUID         `json:"assistant_id,omitempty"`
	AssistantUpdatedAt time.Time          `json:"assistant_updated_at"`
	Depth              int                `json:"depth"`
}

func (p *assistantToolPlan) definitions() []providers.ToolDefinition {
	out := []providers.ToolDefinition{}
	for _, t := range p.Tools {
		out = append(out, providers.ToolDefinition{Name: t.Alias, Description: t.Tool.Description, Parameters: t.Tool.InputSchema})
	}
	return out
}

type toolProposal struct {
	ID             uuid.UUID             `json:"id"`
	Token          *encryption.Encrypted `json:"token"`
	ConnectionName string                `json:"connection_name"`
	ToolName       string                `json:"tool_name"`
	Arguments      json.RawMessage       `json:"arguments"`
	Categories     []string              `json:"categories"`
	ExpiresAt      time.Time             `json:"expires_at"`
}
type toolContinuation struct {
	ProviderState  []json.RawMessage   `json:"provider_state,omitempty"`
	UserID         string              `json:"user_id"`
	ConversationID uuid.UUID           `json:"conversation_id"`
	Plan           assistantToolPlan   `json:"plan"`
	Messages       []providers.Message `json:"messages"`
	Call           providers.ToolCall  `json:"call"`
	Target         plannedTool         `json:"target"`
	ExpiresAt      time.Time           `json:"expires_at"`
}

func (h *Handler) prepareAssistantTools(r *http.Request, conv *Conversation, b SendMessageRequest, provider providers.Provider, model string) (*assistantToolPlan, error) {
	if !providers.SupportsTools(provider) {
		return nil, fmt.Errorf("this provider does not support assistant tools; choose OpenAI, Claude, Gemini or an OpenAI-compatible provider")
	}
	if h.mcpCipher == nil {
		return nil, fmt.Errorf("tool approval encryption unavailable")
	}
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	p := &assistantToolPlan{Provider: provider, Model: model, AssistantID: conv.AssistantID, ConnectionIDs: append([]uuid.UUID{}, b.MCPConnectionIDs...)}
	if conv.AssistantID != nil {
		a, err := h.store.GetAssistant(r.Context(), cid, *conv.AssistantID)
		if err != nil {
			return nil, err
		}
		p.AssistantUpdatedAt = a.UpdatedAt
		p.ConnectionIDs = append(p.ConnectionIDs, a.MCPConnectionIDs...)
	}
	slices.SortFunc(p.ConnectionIDs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	p.ConnectionIDs = slices.Compact(p.ConnectionIDs)
	if len(p.ConnectionIDs) == 0 || len(p.ConnectionIDs) > 10 {
		return nil, fmt.Errorf("select between 1 and 10 integrations for this chat")
	}
	accessibleIDs := []uuid.UUID{}
	for _, id := range p.ConnectionIDs {
		c, err := h.loadMCPConnection(r.Context(), cid, id, uid)
		// An assistant default that is unavailable to this employee is withheld.
		if err != nil {
			if slices.Contains(b.MCPConnectionIDs, id) {
				return nil, fmt.Errorf("selected integration unavailable to your account")
			}
			continue
		}
		_, client, err := h.mcpClientFor(r.Context(), cid, c)
		if err != nil {
			return nil, fmt.Errorf("connect %s before using assistant tools", c.Name)
		}
		tools, err := h.protectedMCPTools(r.Context(), cid, c, client)
		closeMCP(client)
		if err != nil {
			return nil, fmt.Errorf("tools unavailable from %s; check its connection", c.Name)
		}
		labelJSON, _ := json.Marshal(map[string]string{"integration_name": c.Name})
		labelJSON, _, err = h.inspectMCPJSON(r.Context(), cid, labelJSON, security.RoleRetrieval, 0)
		if err != nil {
			return nil, fmt.Errorf("integration description blocked by security policy")
		}
		var label map[string]string
		if json.Unmarshal(labelJSON, &label) != nil {
			return nil, fmt.Errorf("integration description unavailable")
		}
		accessibleIDs = append(accessibleIDs, id)
		for _, tool := range tools {
			tool.Description = label["integration_name"] + ": " + tool.Description
			if len(p.Tools) >= 64 {
				return nil, fmt.Errorf("too many tools; narrow the integration's allowed tools")
			}
			sum := sha256.Sum256([]byte(id.String() + ":" + tool.Name))
			p.Tools = append(p.Tools, plannedTool{Alias: "bastio_" + hex.EncodeToString(sum[:12]), ConnectionID: id, ConnectionName: c.Name, UpdatedAt: c.UpdatedAt, Tool: tool})
		}
	}
	p.ConnectionIDs = accessibleIDs
	if len(p.Tools) == 0 {
		return nil, fmt.Errorf("no permitted tools available; check integration sharing and allowed tools")
	}
	return p, nil
}

// Schemas may reference their own definitions, never arbitrary network URLs.
// JSON-schema validation uses RE2, so untrusted patterns cannot backtrack.
type offlineSchemaLoader struct{}

func (offlineSchemaLoader) Load(string) (any, error) {
	return nil, errors.New("external schema references are not supported")
}
func validateToolArguments(schema, arguments json.RawMessage) error {
	if len(arguments) == 0 || len(arguments) > 64<<10 || bytes.TrimSpace(arguments)[0] != '{' {
		return errors.New("tool arguments must be a JSON object under 64 KB")
	}
	var doc, value any
	if json.Unmarshal(schema, &doc) != nil || json.Unmarshal(arguments, &value) != nil {
		return errors.New("invalid tool JSON")
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(offlineSchemaLoader{})
	if err := c.AddResource("urn:bastio:tool", doc); err != nil {
		return errors.New("unsupported tool schema")
	}
	compiled, err := c.Compile("urn:bastio:tool")
	if err != nil {
		return errors.New("unsupported tool schema")
	}
	if err = compiled.Validate(value); err != nil {
		return errors.New("tool arguments do not match its input schema")
	}
	return nil
}
func (h *Handler) protectProposedArguments(ctx context.Context, cid uuid.UUID, target plannedTool, call providers.ToolCall) (json.RawMessage, []string, error) {
	if err := validateToolArguments(target.Tool.InputSchema, call.Arguments); err != nil {
		return nil, nil, err
	}
	decision := mcp.Inspect(ctx, nil, mcp.InspectRequest{ToolName: target.Tool.Name, Arguments: call.Arguments})
	if decision.Action == "blocked" {
		return nil, decision.Categories, errors.New("proposed tool request blocked by security policy")
	}
	raw, categories, err := h.inspectMCPJSON(ctx, cid, call.Arguments, security.RoleUser, 0)
	if err != nil {
		return nil, categories, errors.New("tool arguments blocked or security evaluation unavailable")
	}
	if err = validateToolArguments(target.Tool.InputSchema, raw); err != nil {
		return nil, categories, errors.New("protected arguments no longer match this tool's schema; rephrase the request")
	}
	if categories == nil {
		categories = []string{}
	}
	return raw, categories, nil
}
func (h *Handler) proposeAssistantTool(r *http.Request, convID uuid.UUID, p *assistantToolPlan, msgs []providers.Message, resp *providers.ChatResponse, metadata json.RawMessage) (json.RawMessage, error) {
	if len(resp.ToolCalls) != 1 {
		return nil, errors.New("the model proposed multiple tools; ask it to do one action at a time")
	}
	if p.Depth >= 4 {
		return nil, errors.New("tool sequence limit reached; send a new message to continue")
	}
	call := resp.ToolCalls[0]
	index := slices.IndexFunc(p.Tools, func(t plannedTool) bool { return t.Alias == call.Name })
	if index < 0 || call.ID == "" || len(call.ID) > 256 {
		return nil, errors.New("model proposed an unknown tool")
	}
	target := p.Tools[index]
	cid := customerIDFromCtx(r.Context())
	arguments, categories, err := h.protectProposedArguments(r.Context(), cid, target, call)
	if err != nil {
		return nil, err
	}
	call.Arguments = arguments
	id := uuid.New()
	expiry := time.Now().UTC().Add(10 * time.Minute)
	// Raw provider tool arguments never enter storage or a review card.
	continuation := toolContinuation{ProviderState: resp.ProviderState, UserID: userIDFromCtx(r.Context()), ConversationID: convID, Plan: *p, Messages: msgs, Call: call, Target: target, ExpiresAt: expiry}
	raw, err := json.Marshal(continuation)
	if err != nil || len(raw) > 256<<10 {
		return nil, errors.New("tool context exceeds 256 KB; start a shorter conversation")
	}
	sealed, err := h.mcpCipher.Encrypt(string(raw), cid.String(), "workspace-tool-approval:"+id.String())
	if err != nil {
		return nil, errors.New("tool approval encryption failed")
	}
	_, err = h.store.pool.Exec(r.Context(), `INSERT INTO workspace_tool_approvals(id,customer_id,user_id,conversation_id,status,expires_at) VALUES($1,$2,$3,$4,'pending',$5)`, id, cid, continuation.UserID, convID, expiry)
	if err != nil {
		return nil, errors.New("tool approval unavailable")
	}
	var meta map[string]any
	_ = json.Unmarshal(metadata, &meta)
	if meta == nil {
		meta = map[string]any{}
	}
	meta["tool_proposal"] = toolProposal{id, sealed, target.ConnectionName, target.Tool.Name, arguments, categories, expiry}
	return json.Marshal(meta)
}

func (h *Handler) toolApprovalStatus(w http.ResponseWriter, r *http.Request) {
	convID, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, 400, "invalid conversation")
		return
	}
	if h.requireConversationWrite(w, r, convID) == nil {
		return
	}
	approvalID, err := uuidParam(r, "approvalID")
	if err != nil {
		writeError(w, 400, "invalid approval")
		return
	}
	var status string
	var expiry time.Time
	err = h.store.pool.QueryRow(r.Context(), `SELECT status,expires_at FROM workspace_tool_approvals WHERE id=$1 AND customer_id=$2 AND user_id=$3 AND conversation_id=$4`, approvalID, customerIDFromCtx(r.Context()), userIDFromCtx(r.Context()), convID).Scan(&status, &expiry)
	if err != nil {
		writeError(w, 404, "approval unavailable")
		return
	}
	if status == "pending" && time.Now().After(expiry) {
		status = "expired"
	}
	if status == "running" && time.Now().After(expiry) {
		status = "unknown"
	}
	writeJSON(w, 200, map[string]any{"status": status})
}
func (h *Handler) approveAssistantTool(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	var body struct {
		Action string               `json:"action"`
		Token  encryption.Encrypted `json:"token"`
	}
	if decodeMCP(r, &body) != nil || body.Action != "approve" && body.Action != "decline" {
		writeError(w, 400, "choose approve or decline")
		return
	}
	convID, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, 400, "invalid conversation")
		return
	}
	if h.requireConversationWrite(w, r, convID) == nil {
		return
	}
	conv, err := h.store.GetConversation(r.Context(), customerIDFromCtx(r.Context()), convID)
	if err != nil {
		writeError(w, 404, "conversation unavailable")
		return
	}
	approvalID, err := uuidParam(r, "approvalID")
	if err != nil {
		writeError(w, 400, "invalid approval")
		return
	}
	cid, uid := customerIDFromCtx(r.Context()), userIDFromCtx(r.Context())
	if body.Action == "decline" {
		tag, e := h.store.pool.Exec(r.Context(), `UPDATE workspace_tool_approvals SET status='declined' WHERE id=$1 AND customer_id=$2 AND user_id=$3 AND conversation_id=$4 AND status='pending'`, approvalID, cid, uid, convID)
		if e != nil || tag.RowsAffected() != 1 {
			writeError(w, 409, "approval already handled or unavailable")
			return
		}
		h.audit(r, "mcp.tool_declined", AuditTarget{Type: "tool_approval", ID: approvalID.String()}, nil)
		writeJSON(w, 200, map[string]any{"status": "declined"})
		return
	}
	if h.mcpCipher == nil {
		writeError(w, 503, "tool approval unavailable")
		return
	}
	plain, err := h.mcpCipher.Decrypt(&body.Token, cid.String(), "workspace-tool-approval:"+approvalID.String())
	var cont toolContinuation
	if err != nil || len(plain) > 256<<10 || json.Unmarshal([]byte(plain), &cont) != nil || cont.UserID != uid || cont.ConversationID != convID || time.Now().After(cont.ExpiresAt) {
		writeError(w, 403, "approval invalid or expired")
		return
	}
	r, err = h.prepareChat(r, conv, SendMessageRequest{})
	if err != nil {
		writeError(w, 403, "conversation permissions changed")
		return
	}
	if err = h.assertModelAllowed(r.Context(), cid, uid, string(cont.Plan.Provider), cont.Plan.Model); err != nil {
		writeError(w, 403, "model permissions changed")
		return
	}
	if conv.AssistantID == nil && cont.Plan.AssistantID != nil || conv.AssistantID != nil && (cont.Plan.AssistantID == nil || *conv.AssistantID != *cont.Plan.AssistantID) {
		writeError(w, 409, "assistant changed; send a new request")
		return
	}
	if conv.AssistantID != nil {
		a, e := h.store.GetAssistant(r.Context(), cid, *conv.AssistantID)
		if e != nil || !a.UpdatedAt.Equal(cont.Plan.AssistantUpdatedAt) {
			writeError(w, 409, "assistant changed; send a new request")
			return
		}
	}
	c, err := h.loadMCPConnection(r.Context(), cid, cont.Target.ConnectionID, uid)
	if err != nil || !c.UpdatedAt.Equal(cont.Target.UpdatedAt) {
		writeError(w, 403, "integration access or configuration changed; send a new request")
		return
	}
	_, client, err := h.mcpClientFor(r.Context(), cid, c)
	if err != nil {
		writeError(w, 403, "integration needs reconnection")
		return
	}
	defer closeMCP(client)
	tools, err := h.protectedMCPTools(r.Context(), cid, c, client)
	index := slices.IndexFunc(tools, func(t mcp.RemoteTool) bool { return t.Name == cont.Target.Tool.Name })
	if err != nil || index < 0 || !bytes.Equal(tools[index].InputSchema, cont.Target.Tool.InputSchema) {
		writeError(w, 403, "tool permissions or schema changed; send a new request")
		return
	}
	arguments, categories, err := h.protectProposedArguments(r.Context(), cid, cont.Target, cont.Call)
	if err != nil {
		writeError(w, 403, "request blocked by current security policy")
		return
	}
	if !equalJSON(arguments, cont.Call.Arguments) {
		writeError(w, 409, "privacy protection changed these arguments; send a new request to review them")
		return
	}
	if err = h.enforceMemberBudget(r.Context(), cid, uid); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	// Reserve the bounded continuation before executing an external action.
	options := callOptions(r.Context())
	options.tools = cont.Plan.definitions()
	budgetCtx := context.WithValue(r.Context(), callOptionsKey{}, options)
	estimateMsgs := append(slices.Clone(cont.Messages), providers.Message{Role: "tool", Content: strings.Repeat(" ", 32<<10)})
	reservation, err := h.reserveSpend(budgetCtx, cid, cont.Plan.Model, estimateMsgs, cont.Plan.Provider)
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	continued := false
	defer func() {
		if !continued {
			_ = h.settleSpend(cid, reservation, 0)
		}
	}()
	// Atomic one-use claim. Newer user turns invalidate the old proposal.
	tag, err := h.store.pool.Exec(r.Context(), `UPDATE workspace_tool_approvals a SET status='running' WHERE id=$1 AND customer_id=$2 AND user_id=$3 AND conversation_id=$4 AND status='pending' AND expires_at>now() AND message_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM workspace_messages m WHERE m.customer_id=a.customer_id AND m.conversation_id=a.conversation_id AND m.id<>a.message_id AND m.created_at>a.created_at)`, approvalID, cid, uid, convID)
	if err != nil || tag.RowsAffected() != 1 {
		writeError(w, 409, "approval expired, already handled, or conversation moved on")
		return
	}
	h.audit(r, "mcp.tool_approved", AuditTarget{Type: "tool_approval", ID: approvalID.String()}, map[string]any{"connection_id": c.ID.String(), "tool": cont.Target.Tool.Name})
	status := "failed"
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = h.store.pool.Exec(ctx, `UPDATE workspace_tool_approvals SET status=$2 WHERE id=$1 AND status='running'`, approvalID, status)
	}()
	// Access was checked immediately before claim; check once more before side effect.
	current, err := h.loadMCPConnection(r.Context(), cid, c.ID, uid)
	if err != nil || !current.UpdatedAt.Equal(c.UpdatedAt) {
		writeError(w, 403, "integration permissions changed")
		return
	}
	raw, err := client.CallTool(r.Context(), cont.Target.Tool.Name, arguments)
	if err != nil {
		h.auditMCP(r, c, cont.Target.Tool.Name, "error", categories)
		writeJSON(w, 200, map[string]any{"status": "failed", "error": "The remote action may already have completed. Check the service before making a new request."})
		return
	}
	status = "completed"
	processed, resultCategories, err := h.inspectMCPJSON(r.Context(), cid, raw, security.RoleRetrieval, 0)
	categories = append(categories, resultCategories...)
	if err != nil {
		h.auditMCP(r, c, cont.Target.Tool.Name, "result_blocked", categories)
		writeJSON(w, 200, map[string]any{"status": status, "error": "The tool ran, but its result was withheld by security policy. The remote action was not undone."})
		return
	}
	text, err := supportedToolText(processed)
	if err != nil {
		writeJSON(w, 200, map[string]any{"status": status, "error": "The tool ran, but its text result was empty, unsupported or larger than 32 KB. Narrow the request before trying again."})
		return
	}
	h.auditMCP(r, c, cont.Target.Tool.Name, "completed", categories)
	// Rebuild current RAG rather than replaying potentially revoked documents.
	system, _, _ := h.resolveAssistantConfig(r.Context(), cid, conv, SendMessageRequest{Provider: string(cont.Plan.Provider), Model: cont.Plan.Model})
	system, citations := h.augmentWithRAG(r.Context(), cid, conv, lastUserContent(cont.Messages), system)
	if len(cont.Messages) > 0 && cont.Messages[0].Role == "system" {
		cont.Messages[0].Content = system
	} else if system != "" {
		cont.Messages = append([]providers.Message{{Role: "system", Content: system}}, cont.Messages...)
	}
	cont.Messages = append(cont.Messages, providers.Message{Role: "assistant", ProviderState: cont.ProviderState, ToolCalls: []providers.ToolCall{cont.Call}}, providers.Message{Role: "tool", Content: text, ToolCallID: cont.Call.ID, ToolName: cont.Call.Name})
	cont.Plan.Depth++
	// Refresh accessible definitions for the next proposal; every next call asks again.
	p, e := h.prepareAssistantTools(r, conv, SendMessageRequest{MCPConnectionIDs: cont.Plan.ConnectionIDs}, cont.Plan.Provider, cont.Plan.Model)
	if e != nil {
		writeJSON(w, 200, map[string]any{"status": status, "result": text, "error": "Tool completed. Integration permissions changed before the assistant could continue."})
		return
	}
	p.Depth = cont.Plan.Depth
	options = callOptions(r.Context())
	options.reservation = reservation
	options.hasReservation = true
	options.toolPlan = p
	options.tools = p.definitions()
	r = r.WithContext(context.WithValue(r.Context(), callOptionsKey{}, options))
	continued = true
	message, err := h.runProvider(r, cid, p.Provider, p.Model, cont.Messages, citations)
	if err != nil {
		writeJSON(w, 200, map[string]any{"status": status, "result": text, "error": "Tool completed, but the assistant reply failed. The tool will not run again."})
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "message": message})
}
func equalJSON(a, b []byte) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	aa, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return bytes.Equal(aa, bb)
}
func supportedToolText(raw []byte) (string, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", errors.New("unsupported result")
	}
	parts := []string{}
	for _, c := range result.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	if len(result.Structured) > 0 && string(result.Structured) != "null" {
		parts = append(parts, string(result.Structured))
	}
	text := strings.Join(parts, "\n")
	if result.IsError {
		text = "The tool reported an error: " + text
	}
	if len(text) > 32<<10 {
		return "", errors.New("tool result exceeds 32 KB; narrow the request")
	}
	if text == "" {
		return "", errors.New("no text result")
	}
	return text, nil
}
