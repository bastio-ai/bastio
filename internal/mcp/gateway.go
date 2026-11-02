package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func closeRemote(client *RemoteClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client.Close(ctx)
}

func (h *Handler) waitDuration(r *http.Request) time.Duration {
	wait := h.approvalWait
	if wait <= 0 {
		wait = 60 * time.Second
	}
	if raw := r.URL.Query().Get("timeout_ms"); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms >= 100 && ms <= 120000 {
			wait = time.Duration(ms) * time.Millisecond
		}
	}
	return wait
}

func (h *Handler) notifyWaiters(approval Approval) {
	h.waitMu.Lock()
	chans := h.waiters[approval.ID]
	delete(h.waiters, approval.ID)
	h.waitMu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- approval:
		default:
		}
	}
}

func (h *Handler) waitForApproval(ctx context.Context, cid uuid.UUID, initial Approval, maxWait time.Duration) Approval {
	if initial.Status != "pending" || maxWait <= 0 {
		return initial
	}
	ch := make(chan Approval, 1)
	h.waitMu.Lock()
	h.waiters[initial.ID] = append(h.waiters[initial.ID], ch)
	h.waitMu.Unlock()
	defer func() {
		h.waitMu.Lock()
		list := h.waiters[initial.ID]
		for i, item := range list {
			if item == ch {
				h.waiters[initial.ID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(h.waiters[initial.ID]) == 0 {
			delete(h.waiters, initial.ID)
		}
		h.waitMu.Unlock()
	}()

	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case resolved := <-ch:
			return resolved
		case <-ticker.C:
			current, err := h.store.GetApproval(ctx, cid, initial.ID)
			if err == nil && current.Status != "pending" {
				return current
			}
		case <-timer.C:
			current, err := h.store.GetApproval(ctx, cid, initial.ID)
			if err == nil {
				return current
			}
			return initial
		case <-ctx.Done():
			return initial
		}
	}
}

func (h *Handler) fireApprovalNotification(ctx context.Context, srv Server, approval Approval) {
	if h.notifier != nil {
		go h.notifier(context.WithoutCancel(ctx), srv.CustomerID, approval)
	}
	if srv.Policy.WebhookURL != "" && ValidateRemoteURL(srv.Policy.WebhookURL) == nil {
		go h.deliverPolicyWebhook(srv.Policy.WebhookURL, srv.Policy.WebhookFormat, approval)
	}
}

func (h *Handler) deliverPolicyWebhook(webhookURL, format string, approval Approval) {
	payload := formatApprovalWebhook(format, approval)
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Bastio-MCP-Firewall/1.0")
	client := h.httpClient
	if client == nil {
		client = secureMCPHTTP()
	}
	res, err := client.Do(req)
	if err != nil {
		slog.Warn("MCP approval webhook delivery failed", "approval_id", approval.ID, "error", err)
		return
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
}

func formatApprovalWebhook(format string, approval Approval) any {
	keys := "none"
	if len(approval.ArgumentKeys) > 0 {
		keys = strings.Join(approval.ArgumentKeys, ", ")
	}
	cats := "human_approval"
	if len(approval.Categories) > 0 {
		cats = strings.Join(approval.Categories, ", ")
	}
	switch format {
	case "teams":
		return map[string]any{
			"@type":      "MessageCard",
			"@context":   "https://schema.org/extensions",
			"themeColor": "FFAA00",
			"summary":    "Bastio MCP Approval Required",
			"title":      fmt.Sprintf("Bastio MCP Approval Required — %s", approval.ToolName),
			"sections": []map[string]any{{
				"facts": []map[string]string{
					{"name": "Server", "value": approval.ServerName},
					{"name": "Client", "value": approval.Client},
					{"name": "Tool", "value": approval.ToolName},
					{"name": "Argument Keys", "value": keys},
					{"name": "Categories", "value": cats},
					{"name": "Approval ID", "value": approval.ID.String()},
				},
			}},
		}
	case "raw_json":
		return map[string]any{
			"event":    "mcp.approval_required",
			"approval": approval,
		}
	default:
		return map[string]any{
			"text": fmt.Sprintf(":lock: *Bastio MCP Approval Required*\nServer: `%s` (%s)\nTool: `%s`\nArgument Keys: `%s`\nCategories: `%s`\nApproval ID: `%s`",
				approval.ServerName, approval.Client, approval.ToolName, keys, cats, approval.ID.String()),
		}
	}
}

// RuntimeCreateApproval allows local stdio proxies to request human-in-the-loop approval before running a tool.
func (h *Handler) RuntimeCreateApproval(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerEnrollmentToken(w, r)
	if !ok {
		return
	}
	srv, err := h.store.LookupByTokenHash(r.Context(), tokenHash(token))
	if errors.Is(err, ErrEnrollment) {
		fail(w, 401, "MCP enrollment is invalid or revoked")
		return
	}
	if err != nil {
		h.storeError(w, err)
		return
	}
	var b struct {
		ToolName     string   `json:"tool_name"`
		ArgumentKeys []string `json:"argument_keys"`
		Categories   []string `json:"categories"`
	}
	if !decode(w, r, &b) {
		return
	}
	if !toolNameRE.MatchString(b.ToolName) || len(b.ArgumentKeys) > 32 || len(b.Categories) > 16 {
		fail(w, 400, "invalid approval request")
		return
	}
	for _, k := range b.ArgumentKeys {
		if !toolNameRE.MatchString(k) {
			fail(w, 400, "invalid argument key")
			return
		}
	}
	for _, c := range b.Categories {
		if !toolNameRE.MatchString(c) {
			fail(w, 400, "invalid category")
			return
		}
	}
	if !slices.Contains(b.Categories, "human_approval") {
		b.Categories = append(b.Categories, "human_approval")
	}
	approval, err := h.store.CreateApproval(r.Context(), srv.CustomerID, srv.ID, b.ToolName, b.ArgumentKeys, b.Categories, 5*time.Minute)
	if err != nil {
		h.storeError(w, err)
		return
	}
	_ = h.store.RecordServerEvents(r.Context(), srv.CustomerID, srv.ID, []Event{{
		ID:         uuid.New(),
		ServerID:   srv.ID,
		ToolName:   b.ToolName,
		Direction:  "request",
		Action:     "approval_required",
		Categories: b.Categories,
		DurationMS: 0,
		OccurredAt: time.Now().UTC(),
	}})
	h.fireApprovalNotification(r.Context(), srv, approval)
	if r.URL.Query().Get("wait") == "true" || r.Header.Get("X-Bastio-Approval-Wait") == "true" {
		approval = h.waitForApproval(r.Context(), srv.CustomerID, approval, h.waitDuration(r))
	}
	status := 201
	if approval.Status != "pending" {
		status = 200
	}
	respond(w, status, struct {
		Approval Approval `json:"approval"`
	}{approval})
}

// RuntimeGetApproval allows local stdio proxies to poll or long-poll a pending approval.
func (h *Handler) RuntimeGetApproval(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerEnrollmentToken(w, r)
	if !ok {
		return
	}
	srv, err := h.store.LookupByTokenHash(r.Context(), tokenHash(token))
	if errors.Is(err, ErrEnrollment) {
		fail(w, 401, "MCP enrollment is invalid or revoked")
		return
	}
	if err != nil {
		h.storeError(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		fail(w, 400, "invalid approval ID")
		return
	}
	approval, err := h.store.GetApproval(r.Context(), srv.CustomerID, id)
	if err != nil || approval.ServerID != srv.ID {
		fail(w, 404, "approval not found")
		return
	}
	if approval.Status == "pending" && r.URL.Query().Get("wait") == "true" {
		approval = h.waitForApproval(r.Context(), srv.CustomerID, approval, min(h.waitDuration(r), 25*time.Second))
	}
	respond(w, 200, struct {
		Approval Approval `json:"approval"`
	}{approval})
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string, data any) {
	var rawData json.RawMessage
	if data != nil {
		rawData, _ = json.Marshal(data)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonrpcError{
			Code:    code,
			Message: message,
			Data:    rawData,
		},
	})
}

// GatewayDelete terminates a Streamable HTTP MCP session cleanly.
func (h *Handler) GatewayDelete(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerEnrollmentToken(w, r)
	if !ok {
		return
	}
	if _, err := h.store.LookupByTokenHash(r.Context(), tokenHash(token)); err != nil {
		fail(w, 401, "MCP enrollment is invalid or revoked")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Gateway is the Remote Hosted Streamable HTTP MCP Gateway.
// It authenticates the client via its Bastio enrollment token, vaults & injects the
// upstream Bearer credential on egress, enforces tool allow/block/approval policies,
// and scans tool descriptions, arguments, and responses.
func (h *Handler) Gateway(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerEnrollmentToken(w, r)
	if !ok {
		return
	}
	srv, err := h.store.LookupByTokenHash(r.Context(), tokenHash(token))
	if errors.Is(err, ErrEnrollment) {
		fail(w, 401, "MCP enrollment is invalid or revoked")
		return
	}
	if err != nil {
		h.storeError(w, err)
		return
	}
	if srv.Mode != "remote" || srv.UpstreamURL == "" {
		fail(w, 400, "MCP server enrollment is configured for local stdio mode, not remote gateway")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var msg jsonrpcMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil || msg.JSONRPC != "2.0" {
		fail(w, 400, "invalid JSON-RPC 2.0 request")
		return
	}
	if len(msg.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch msg.Method {
	case "initialize":
		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &initParams)
		version := "2025-11-25"
		if slices.Contains([]string{"2025-03-26", "2025-06-18", "2025-11-25", "2026-07-28"}, initParams.ProtocolVersion) {
			version = initParams.ProtocolVersion
		}
		writeRPCResult(w, msg.ID, map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "Bastio MCP Gateway (" + srv.Name + ")",
				"version": "1.0.0",
			},
		})
	case "ping":
		writeRPCResult(w, msg.ID, map[string]any{})
	case "tools/list":
		h.gatewayToolsList(w, r, srv, msg)
	case "tools/call":
		h.gatewayToolsCall(w, r, srv, msg)
	default:
		writeRPCError(w, msg.ID, -32601, "Unsupported MCP method on Bastio Gateway", nil)
	}
}

func (h *Handler) gatewayToolsList(w http.ResponseWriter, r *http.Request, srv Server, msg jsonrpcMessage) {
	bearer, err := h.openCredential(srv.CustomerID, srv)
	if err != nil {
		writeRPCError(w, msg.ID, -32603, "Upstream MCP credential unavailable", nil)
		return
	}
	client, err := NewRemoteClient(srv.UpstreamURL, bearer, h.httpClient)
	if err != nil {
		writeRPCError(w, msg.ID, -32603, "Invalid upstream MCP endpoint", nil)
		return
	}
	defer closeRemote(client)

	tools, err := client.ListTools(r.Context())
	if err != nil {
		writeRPCError(w, msg.ID, -32603, "Upstream MCP tool discovery failed", nil)
		return
	}
	filtered := make([]RemoteTool, 0, len(tools))
	var events []Event
	for _, tool := range tools {
		if slices.Contains(srv.Policy.BlockedTools, tool.Name) {
			continue
		}
		if len(srv.Policy.AllowedTools) > 0 && !slices.Contains(srv.Policy.AllowedTools, tool.Name) {
			continue
		}
		if tool.Description != "" && h.engine != nil {
			scanRes := h.engine.Scan(r.Context(), &security.ScanRequest{Content: tool.Description})
			if scanRes != nil && (scanRes.ShouldBlock || len(scanRes.Findings) > 0) {
				tool.Description = "[BLOCKED BY BASTIO: Malicious tool description detected]"
				events = append(events, Event{
					ID:         uuid.New(),
					ServerID:   srv.ID,
					ToolName:   tool.Name,
					Direction:  "tool_description",
					Action:     "sanitized",
					Categories: categoriesFromScan(scanRes),
					DurationMS: float64(scanRes.ScanDuration.Microseconds()) / 1000,
					OccurredAt: time.Now().UTC(),
				})
			}
		}
		filtered = append(filtered, tool)
	}
	_ = h.store.RecordServerEvents(r.Context(), srv.CustomerID, srv.ID, events)
	writeRPCResult(w, msg.ID, map[string]any{"tools": filtered})
}

func (h *Handler) gatewayToolsCall(w http.ResponseWriter, r *http.Request, srv Server, msg jsonrpcMessage) {
	var params toolCallParams
	err := json.Unmarshal(msg.Params, &params)
	if len(params.Arguments) == 0 {
		params.Arguments = json.RawMessage(`{}`)
	}
	params.Arguments = bytes.TrimSpace(params.Arguments)
	if err != nil || !toolNameRE.MatchString(params.Name) || !json.Valid(params.Arguments) || params.Arguments[0] != '{' {
		writeRPCError(w, msg.ID, -32602, "Invalid MCP tool call parameters", nil)
		return
	}

	decision := Inspect(r.Context(), h.engine, InspectRequest{
		ToolName:  params.Name,
		Arguments: params.Arguments,
		Policy:    srv.Policy,
	})
	if decision.Action == "blocked" {
		_ = h.store.RecordServerEvents(r.Context(), srv.CustomerID, srv.ID, []Event{{
			ID:         uuid.New(),
			ServerID:   srv.ID,
			ToolName:   params.Name,
			Direction:  "request",
			Action:     "blocked",
			Categories: decision.Categories,
			DurationMS: decision.DurationMS,
			OccurredAt: time.Now().UTC(),
		}})
		writeRPCError(w, msg.ID, -32600, "Blocked by Bastio Security: tool policy or unsafe action", map[string]any{
			"action":     "blocked",
			"categories": decision.Categories,
		})
		return
	}

	if decision.Action == "approval_required" {
		argKeys := ExtractArgumentKeys(params.Arguments)
		approval, err := h.store.CreateApproval(r.Context(), srv.CustomerID, srv.ID, params.Name, argKeys, decision.Categories, 5*time.Minute)
		if err != nil {
			writeRPCError(w, msg.ID, -32603, "Unable to create MCP approval request", nil)
			return
		}
		_ = h.store.RecordServerEvents(r.Context(), srv.CustomerID, srv.ID, []Event{{
			ID:         uuid.New(),
			ServerID:   srv.ID,
			ToolName:   params.Name,
			Direction:  "request",
			Action:     "approval_required",
			Categories: decision.Categories,
			DurationMS: decision.DurationMS,
			OccurredAt: time.Now().UTC(),
		}})
		h.fireApprovalNotification(r.Context(), srv, approval)
		if strings.EqualFold(r.Header.Get("X-Bastio-Approval-Wait"), "false") {
			writeRPCError(w, msg.ID, -32001, "Human approval required by Bastio Security", map[string]any{
				"action":      "approval_required",
				"approval_id": approval.ID,
				"categories":  decision.Categories,
			})
			return
		}
		resolved := h.waitForApproval(r.Context(), srv.CustomerID, approval, h.waitDuration(r))
		if resolved.Status != "approved" {
			writeRPCError(w, msg.ID, -32001, "Blocked by Bastio Security: human approval "+resolved.Status, map[string]any{
				"action":      resolved.Status,
				"approval_id": resolved.ID,
				"categories":  decision.Categories,
			})
			return
		}
	} else {
		_ = h.store.RecordServerEvents(r.Context(), srv.CustomerID, srv.ID, []Event{{
			ID:         uuid.New(),
			ServerID:   srv.ID,
			ToolName:   params.Name,
			Direction:  "request",
			Action:     decision.Action,
			Categories: decision.Categories,
			DurationMS: decision.DurationMS,
			OccurredAt: time.Now().UTC(),
		}})
	}

	bearer, err := h.openCredential(srv.CustomerID, srv)
	if err != nil {
		writeRPCError(w, msg.ID, -32603, "Upstream MCP credential unavailable", nil)
		return
	}
	client, err := NewRemoteClient(srv.UpstreamURL, bearer, h.httpClient)
	if err != nil {
		writeRPCError(w, msg.ID, -32603, "Invalid upstream MCP endpoint", nil)
		return
	}
	defer closeRemote(client)

	callStart := time.Now()
	rawResult, err := client.CallTool(r.Context(), params.Name, decision.Arguments)
	callMS := float64(time.Since(callStart).Microseconds()) / 1000
	if err != nil {
		writeRPCError(w, msg.ID, -32603, "Upstream MCP tool call failed", nil)
		return
	}

	inspectedResult, respAction, respCategories := inspectToolResult(r.Context(), h.engine, rawResult)
	_ = h.store.RecordServerEvents(r.Context(), srv.CustomerID, srv.ID, []Event{{
		ID:         uuid.New(),
		ServerID:   srv.ID,
		ToolName:   params.Name,
		Direction:  "response",
		Action:     respAction,
		Categories: respCategories,
		DurationMS: callMS,
		OccurredAt: time.Now().UTC(),
	}})
	writeRPCResult(w, msg.ID, inspectedResult)
}

func inspectToolResult(ctx context.Context, engine *security.Engine, raw json.RawMessage) (json.RawMessage, string, []string) {
	action := "allowed"
	categories := []string{}
	if len(raw) == 0 || engine == nil {
		return raw, action, categories
	}
	var callRes toolCallResult
	if err := json.Unmarshal(raw, &callRes); err != nil {
		return raw, action, categories
	}
	modified := false
	for i := range callRes.Content {
		if callRes.Content[i].Type == "text" && callRes.Content[i].Text != "" {
			text := callRes.Content[i].Text
			scanRes := engine.Scan(ctx, &security.ScanRequest{
				Content:   text,
				PIIAction: security.ActionMask,
			})
			if scanRes != nil {
				categories = append(categories, categoriesFromScan(scanRes)...)
				if scanRes.ShouldBlock {
					callRes.Content[i].Text = "[BLOCKED BY BASTIO: Unsafe tool result]"
					modified = true
					action = "blocked"
					continue
				}
				if scanRes.SanitizedContent != "" && scanRes.SanitizedContent != text {
					callRes.Content[i].Text = scanRes.SanitizedContent
					if action != "blocked" {
						action = "sanitized"
					}
					modified = true
				}
			}
		}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && len(fields["structuredContent"]) > 0 {
		structured := fields["structuredContent"]
		scan := engine.Scan(ctx, &security.ScanRequest{Content: string(structured), PIIAction: security.ActionMask})
		if scan != nil {
			categories = append(categories, categoriesFromScan(scan)...)
			if scan.ShouldBlock {
				delete(fields, "structuredContent")
				modified = true
				action = "blocked"
			} else if scan.SanitizedContent != "" && scan.SanitizedContent != string(structured) {
				if json.Valid([]byte(scan.SanitizedContent)) {
					fields["structuredContent"] = json.RawMessage(scan.SanitizedContent)
				} else {
					delete(fields, "structuredContent")
				}
				modified = true
				if action != "blocked" {
					action = "sanitized"
				}
			}
		}
		raw, _ = json.Marshal(fields)
	}
	if modified {
		if rewritten, err := rewriteToolContent(raw, callRes.Content); err == nil {
			raw = rewritten
		}
	}
	unique := []string{}
	for _, cat := range categories {
		if toolNameRE.MatchString(cat) && !slices.Contains(unique, cat) && len(unique) < 16 {
			unique = append(unique, cat)
		}
	}
	return raw, action, unique
}
