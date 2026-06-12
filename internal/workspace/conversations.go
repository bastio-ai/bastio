package workspace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/llmpipeline"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
)

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	uid := userIDFromCtx(r.Context())
	role := RoleFromCtx(r.Context())
	limit := intQuery(r, "limit", 50)

	// Members see only their own conversations — privacy from peers.
	// Admins / viewers / owner see everything in the workspace —
	// governance + audit. Empty user_id sentinel means "all".
	scopeUserID := uid
	if role != RoleMember {
		scopeUserID = ""
	}

	rows, err := h.store.ListConversations(r.Context(), cid, scopeUserID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Privacy audit: when a privileged caller scans the all-users
	// view AND the result actually contains someone else's threads,
	// record one summary audit row. We don't write per-conversation
	// rows here — that would spam the log on every page load. The
	// per-conversation audits fire when they open one (getConversation).
	if scopeUserID == "" {
		crossOwners := map[string]struct{}{}
		for _, c := range rows {
			if c.UserID != "" && c.UserID != uid {
				crossOwners[c.UserID] = struct{}{}
			}
		}
		if len(crossOwners) > 0 {
			h.audit(r, "conversations.scanned_all_users", AuditTarget{
				Type: "workspace",
			}, map[string]any{
				"result_count":     len(rows),
				"distinct_owners":  len(crossOwners),
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"conversations": rows})
}

func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	// Per-user visibility: a member peeking at another member's
	// conversation gets ErrNotFound from resolveConversationAccess —
	// no existence leak. Admins/viewers see all.
	access, err := h.resolveConversationAccess(r.Context(), id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	c, err := h.store.GetConversation(r.Context(), customerIDFromCtx(r.Context()), id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	// Privacy audit: admin/viewer/owner reading someone else's thread.
	// No-op when caller is the owner.
	h.auditCrossUserRead(r, "conversation.viewed_cross_user", id, access.Owner)
	writeJSON(w, http.StatusOK, c)
}

// CreateConversationRequest is the POST body for /conversations.
type CreateConversationRequest struct {
	Title       string     `json:"title"`
	AssistantID *uuid.UUID `json:"assistant_id"`
}

func (h *Handler) createConversation(w http.ResponseWriter, r *http.Request) {
	var body CreateConversationRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Title == "" {
		body.Title = "New chat"
	}
	c := Conversation{
		CustomerID:  customerIDFromCtx(r.Context()),
		UserID:      userIDFromCtx(r.Context()),
		AssistantID: body.AssistantID,
		Title:       body.Title,
	}
	created, err := h.store.CreateConversation(r.Context(), c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// UpdateConversationRequest is the PATCH body for /conversations/{id}.
// Every field is optional — partial updates are the norm. Pinned and
// Archived can flip independently of Title; the right-click menu in
// the chat list sends just the field that changed.
type UpdateConversationRequest struct {
	Title    *string `json:"title,omitempty"`
	Pinned   *bool   `json:"pinned,omitempty"`
	// Archived: true → set archived_at = NOW(); false → clear it
	// (unarchive). Omitted = leave alone.
	Archived *bool `json:"archived,omitempty"`
}

// RenameConversationRequest is the PATCH body for /conversations/{id}.
// Deprecated: use UpdateConversationRequest. Kept for back-compat
// while clients update.
type RenameConversationRequest struct {
	Title string `json:"title"`
}

// deleteFromMessage handles DELETE /conversations/{id}/messages/{messageID}.
// Deletes the target message and every later message in the same
// conversation. Used by the chat surface's regenerate + edit flows
// — the client wipes from the point of divergence then re-sends to
// get a fresh assistant reply.
func (h *Handler) deleteFromMessage(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}
	messageID, err := uuidParam(r, "messageID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	if h.requireConversationWrite(w, r, conversationID) == nil {
		return
	}
	if err := h.store.DeleteFromMessage(
		r.Context(), customerIDFromCtx(r.Context()), conversationID, messageID,
	); err != nil {
		notFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateConversation handles partial updates to a conversation:
// title rename, pin/unpin, and archive/unarchive — in any combination.
// At least one field must be present.
func (h *Handler) updateConversation(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body UpdateConversationRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Title == nil && body.Pinned == nil && body.Archived == nil {
		writeError(w, http.StatusBadRequest, "at least one of title, pinned, archived is required")
		return
	}
	// Empty/whitespace title is invalid only when title is being set.
	if body.Title != nil && strings.TrimSpace(*body.Title) == "" {
		writeError(w, http.StatusBadRequest, "title cannot be empty")
		return
	}
	if h.requireConversationWrite(w, r, id) == nil {
		return
	}
	if err := h.store.UpdateConversation(r.Context(), customerIDFromCtx(r.Context()), id, body); err != nil {
		notFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) archiveConversation(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if h.requireConversationWrite(w, r, id) == nil {
		return
	}
	if err := h.store.ArchiveConversation(r.Context(), customerIDFromCtx(r.Context()), id); err != nil {
		notFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	// Read access via the rbac helper. ErrNotFound when the
	// caller is a member peeking at someone else's thread; admins
	// + viewers see all.
	access, err := h.resolveConversationAccess(r.Context(), id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	cid := customerIDFromCtx(r.Context())
	msgs, err := h.store.ListMessages(r.Context(), cid, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Privacy audit: privileged caller pulled message contents from
	// someone else's thread. Higher signal than viewing the
	// conversation header — this is the bytes the user typed.
	h.auditCrossUserRead(r, "messages.viewed_cross_user", id, access.Owner)
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

// SendMessageRequest is the POST body for /conversations/{id}/messages.
// MVP non-streaming. Streaming SSE variant is a Phase 2 follow-up.
type SendMessageRequest struct {
	Content  string `json:"content"`
	Provider string `json:"provider,omitempty"` // override assistant default
	Model    string `json:"model,omitempty"`
}

// SendMessageResponse returns both the user message and the assistant reply.
type SendMessageResponse struct {
	UserMessage      *Message `json:"user_message"`
	AssistantMessage *Message `json:"assistant_message"`
}

func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body SendMessageRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if h.requireConversationWrite(w, r, id) == nil {
		return
	}

	cid := customerIDFromCtx(r.Context())
	conv, err := h.store.GetConversation(r.Context(), cid, id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}

	// Resolve assistant config (system prompt + provider + model) and
	// augment the prompt with RAG context drawn from the assistant's
	// attached knowledge sources. Both ops are nil-safe.
	systemPromptBase, providerEnum, model := h.resolveAssistantConfig(r.Context(), cid, conv, body)
	provName := string(providerEnum)

	// Defense-in-depth model whitelist enforcement. The chat picker UI
	// already filters by the user's effective allowed_models, but a
	// malicious or stale client can POST any (provider, model) pair —
	// reject early before persisting the user's message or making a
	// provider call.
	if err := h.assertModelAllowed(r.Context(), cid, userIDFromCtx(r.Context()), provName, model); err != nil {
		writeStructuredError(w, http.StatusForbidden,
			"model_not_allowed",
			err.Error(),
			map[string]any{"provider": provName, "model": model})
		return
	}

	systemPrompt, citations := h.augmentWithRAG(r.Context(), cid, conv, body.Content, systemPromptBase)

	// Persist user message first so a provider failure still leaves the
	// user's text in the conversation history.
	userMsg, err := h.store.AppendMessage(r.Context(), Message{
		ConversationID: id,
		CustomerID:     cid,
		Role:           "user",
		Content:        body.Content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build the LLM request from the full conversation history.
	priorMsgs, err := h.store.ListMessages(r.Context(), cid, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	llmMsgs := make([]providers.Message, 0, len(priorMsgs)+1)
	if systemPrompt != "" {
		llmMsgs = append(llmMsgs, providers.Message{Role: "system", Content: systemPrompt})
	}
	for _, m := range priorMsgs {
		if m.Role == "system" {
			continue
		}
		// Peel out any inline data:image/...;base64 attachments and
		// pass them as proper multimodal images. Models that don't
		// support images (Ollama, others) get the plain text and
		// images are silently dropped — the user already saw a
		// "[image attached]" placeholder when they uploaded.
		text, imgs := extractImagesForProvider(m.Content)
		if len(imgs) > 0 && text == "" {
			text = "[image attached]"
		}
		// Tool-result scan — `tool`-role messages carry external
		// API output that an attacker could control (a tool that
		// returns scraped web content, a database row containing
		// adversarial text, etc). Without this gate, indirect-
		// injection through tool returns is the path of least
		// resistance into the model. Block / sanitize / allow
		// follows the same engine + mapping as KB ingest.
		if m.Role == "tool" {
			text = h.gateToolResult(r.Context(), cid, text)
		}
		llmMsgs = append(llmMsgs, providers.Message{Role: m.Role, Content: text, Images: imgs})
	}

	// Call the provider through the shared registry. Citations from
	// the RAG pass flow into the persisted assistant message so the
	// chat UI can render "based on X" chips below the reply.
	asstMsg, err := h.runProvider(r, cid, providers.Provider(provName), model, llmMsgs, citations)
	if err != nil {
		errStr := err.Error()
		errMsg, perr := h.store.AppendMessage(r.Context(), Message{
			ConversationID: id,
			CustomerID:     cid,
			Role:           "assistant",
			Content:        "(provider call failed)",
			Provider:       strPtr(provName),
			Model:          strPtr(model),
			Error:          &errStr,
		})
		if perr != nil {
			writeError(w, http.StatusInternalServerError, perr.Error())
			return
		}
		writeJSON(w, http.StatusOK, SendMessageResponse{UserMessage: userMsg, AssistantMessage: errMsg})
		return
	}
	writeJSON(w, http.StatusOK, SendMessageResponse{UserMessage: userMsg, AssistantMessage: asstMsg})
}

// runProvider executes the LLM call and persists the assistant
// message. Pre-flight: runs the workspace security pipeline against
// the user's latest message. If the scan blocks, returns a synthesized
// "blocked by policy" assistant message instead of calling the
// provider. Always records a TraceRecord (when ClickHouse is wired)
// so workspace traffic shows up in the same observability tabs as
// gateway traffic.
// citations is the deduped list of KB sources that the RAG pass
// retrieved for this turn. Empty when no KB attached, no chunks
// matched, or no assistant bound. The successful-reply branch
// stashes them on the assistant message's metadata so the UI can
// render source chips below the reply.
func (h *Handler) runProvider(r *http.Request, customerID uuid.UUID, prov providers.Provider, model string, msgs []providers.Message, citations []Citation) (*Message, error) {
	if h.registry == nil {
		return nil, fmt.Errorf("provider registry not configured")
	}
	conversationID, _ := uuidParam(r, "id")
	startedAt := time.Now().UTC()
	traceID := uuid.New()
	userID := userIDFromCtx(r.Context())
	ipHash, userAgent := requestSignals(r)

	// Pre-flight scan on the latest user message. Earlier messages
	// were scanned at their own send time; we only scan what's new.
	//
	// Image data URLs are extracted BEFORE the scan — the base64
	// payload looks like high-entropy secrets and the engine would
	// shred it with [REDACTED] markers, breaking image-extract on
	// the persisted message and stripping the image from the LLM
	// call. Scan the text only; recompose with images afterwards.
	latestUser := lastUserContent(msgs)
	latestUserText, latestUserImages := extractImagesForProvider(latestUser)
	scan, _, scanErr := h.scanUserMessage(r.Context(), customerID, conversationID, userID, ipHash, userAgent, latestUserText)
	if scanErr != nil {
		// Only set in fail-closed mode (BASTIO_SCAN_FAIL_MODE=closed):
		// the scan couldn't run, so the prompt must not reach the
		// provider. The caller's error branch persists an assistant
		// error message, so the chat UI shows why nothing came back.
		return nil, fmt.Errorf("security scan unavailable, message not sent: %w", scanErr)
	}
	if scan != nil && scan.ShouldBlock {
		// Persist a "blocked" assistant message so the chat UI shows
		// the policy hit. No provider call; cost stays zero.
		blockedMsg := "Bastio policy blocked this prompt before it left your tenant. " +
			"Detected: " + joinThreatTypes(scan.ThreatTypes) + ". " +
			"Edit and re-send, or contact your workspace admin."
		finish := "blocked"
		errStr := "blocked by security policy"
		asstMsg, err := h.store.AppendMessage(r.Context(), Message{
			ConversationID:   conversationID,
			CustomerID:       customerID,
			Role:             "assistant",
			Content:          blockedMsg,
			Provider:         strPtr(string(prov)),
			Model:            strPtr(model),
			FinishReason:     &finish,
			Error:            &errStr,
		})
		h.recordChatTrace(chatTraceInput{
			customerID:     customerID,
			conversationID: conversationID,
			traceID:        traceID,
			userID:         userID,
			ipHash:         ipHash,
			userAgent:      userAgent,
			provider:       string(prov),
			model:          model,
			startedAt:      startedAt,
			completedAt:    time.Now().UTC(),
			finishReason:   "blocked",
			requestBody:    latestUser,
			responseBody:   blockedMsg,
			scanResult:     scan,
		})
		if err != nil {
			return nil, err
		}
		return asstMsg, nil
	}

	// Per-member budget enforcement: monthly token cap + daily
	// message rate. Runs after the security scan so security
	// always wins. Blocked sends produce a visible assistant
	// bubble (same shape as the security-block path) — no
	// provider call, zero cost.
	if budgetErr := h.enforceMemberBudget(r.Context(), customerID, userID); budgetErr != nil {
		var be *budgetExceededError
		errStr := budgetErr.Error()
		userFacing := errStr
		if errors.As(budgetErr, &be) {
			userFacing = be.userMessage()
		}
		finish := "budget_exceeded"
		asstMsg, err := h.store.AppendMessage(r.Context(), Message{
			ConversationID: conversationID,
			CustomerID:     customerID,
			Role:           "assistant",
			Content:        userFacing,
			Provider:       strPtr(string(prov)),
			Model:          strPtr(model),
			FinishReason:   &finish,
			Error:          &errStr,
		})
		h.recordChatTrace(chatTraceInput{
			customerID:     customerID,
			conversationID: conversationID,
			traceID:        traceID,
			userID:         userID,
			ipHash:         ipHash,
			userAgent:      userAgent,
			provider:       string(prov),
			model:          model,
			startedAt:      startedAt,
			completedAt:    time.Now().UTC(),
			finishReason:   "budget_exceeded",
			requestBody:    latestUser,
			responseBody:   userFacing,
		})
		if err != nil {
			return nil, err
		}
		return asstMsg, nil
	}

	// If the scan sanitized the user's text, swap it into the
	// outbound messages slice so the provider sees the redacted form.
	// Re-attach the images we held aside before the scan so the
	// build-llmMsgs loop can re-extract them as multimodal parts.
	if len(msgs) > 0 {
		textPart := latestUserText
		if scan != nil && scan.SanitizedContent != "" {
			textPart = scan.SanitizedContent
		}
		if textPart != latestUser || len(latestUserImages) > 0 {
			recombined := recomposeWithImages(textPart, latestUserImages)
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == "user" {
					msgs[i].Content = recombined
					break
				}
			}
		}
	}

	client, err := h.registry.Get(prov)
	if err != nil {
		return nil, err
	}

	apiKey, err := h.resolveAPIKey(r, customerID, prov)
	if err != nil {
		return nil, err
	}

	resp, err := client.Chat(r.Context(), &providers.ChatRequest{
		Provider: prov,
		Model:    model,
		Messages: msgs,
	}, apiKey)
	if err != nil {
		// Record the failure-with-no-response so /traces sees it.
		h.recordChatTrace(chatTraceInput{
			customerID:     customerID,
			conversationID: conversationID,
			traceID:        traceID,
			userID:         userID,
			ipHash:         ipHash,
			userAgent:      userAgent,
			provider:       string(prov),
			model:          model,
			startedAt:      startedAt,
			completedAt:    time.Now().UTC(),
			finishReason:   "error",
			requestBody:    latestUser,
			scanResult:     scan,
		})
		return nil, err
	}

	// =========================================================
	// OUTPUT SCAN — same engine as the inbound scan, applied
	// to what the model returned. Closes the gap where a model
	// either regurgitates training-data PII or echoes an attack
	// injected through RAG / tool results back at the user.
	//
	// Action mapping:
	//   block    → synthesize a "policy blocked the response"
	//              assistant message (same shape as the inbound
	//              block path). The model's actual output is
	//              never persisted or shown to the user.
	//   sanitize → use the rewritten text everywhere downstream
	//              (persisted message, response payload). The
	//              raw text never reaches storage.
	//   allow    → continue with the original.
	// =========================================================
	outDecision, _ := scanForIngest(r.Context(), h.secEngine, h.secProfiles,
		customerID, resp.Content)
	if outDecision != nil && outDecision.Action == "block" {
		blockedMsg := "Bastio blocked the model's response — it contained " +
			joinCategories(outDecision.Categories) + ". Try rephrasing or contact your workspace admin."
		finishBlocked := "blocked"
		errStr := "blocked by output security policy"
		asstMsg, perr := h.store.AppendMessage(r.Context(), Message{
			ConversationID: conversationID,
			CustomerID:     customerID,
			Role:           "assistant",
			Content:        blockedMsg,
			Provider:       strPtr(string(prov)),
			Model:          strPtr(resp.Model),
			FinishReason:   &finishBlocked,
			Error:          &errStr,
		})
		h.recordChatTrace(chatTraceInput{
			customerID:     customerID,
			conversationID: conversationID,
			traceID:        traceID,
			userID:         userID,
			ipHash:         ipHash,
			userAgent:      userAgent,
			provider:       string(prov),
			model:          resp.Model,
			startedAt:      startedAt,
			completedAt:    time.Now().UTC(),
			finishReason:   "blocked_output",
			requestBody:    latestUser,
			responseBody:   blockedMsg,
			scanResult:     scan,
		})
		if perr != nil {
			return nil, perr
		}
		return asstMsg, nil
	}
	if outDecision != nil && outDecision.Action == "sanitize" {
		resp.Content = outDecision.SanitizedContent
	}

	finish := resp.FinishReason
	asstMsg, err := h.store.AppendMessage(r.Context(), Message{
		ConversationID:    conversationID,
		CustomerID:        customerID,
		Role:              "assistant",
		Content:           resp.Content,
		Provider:          strPtr(string(prov)),
		Model:             strPtr(resp.Model),
		PromptTokens:      resp.InputTokens,
		CompletionTokens:  resp.OutputTokens,
		CostCents:         estimateCostCents(string(prov), resp.Model, resp.InputTokens, resp.OutputTokens),
		FinishReason:      &finish,
		Metadata:          encodeCitationsMetadata(citations),
	})
	if err != nil {
		return nil, err
	}
	h.recordChatTrace(chatTraceInput{
		customerID:     customerID,
		conversationID: conversationID,
		traceID:        traceID,
		userID:         userID,
		ipHash:         ipHash,
		userAgent:      userAgent,
		provider:       string(prov),
		model:          resp.Model,
		startedAt:      startedAt,
		completedAt:    time.Now().UTC(),
		inputTokens:    resp.InputTokens,
		outputTokens:   resp.OutputTokens,
		costCents:      estimateCostCentsFloat(string(prov), resp.Model, resp.InputTokens, resp.OutputTokens),
		finishReason:   finish,
		requestBody:    latestUser,
		responseBody:   resp.Content,
		scanResult:     scan,
	})
	return asstMsg, nil
}

// lastUserContent returns the .Content of the most recent message
// with role="user" in the conversation snapshot we send to the
// provider. Empty string when none — the security scan then runs
// against an empty input and returns no findings.
func lastUserContent(msgs []providers.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

// joinThreatTypes flattens the security engine's typed threat list
// into a comma-separated string for user-facing copy.
func joinThreatTypes(ts []security.ThreatType) string {
	if len(ts) == 0 {
		return "policy violation"
	}
	out := ""
	for i, t := range ts {
		if i > 0 {
			out += ", "
		}
		out += string(t)
	}
	return out
}

// resolveAPIKey is a thin shim around resolveAPIKeyCtx kept for the
// non-streaming code path that already has the *http.Request handy.
func (h *Handler) resolveAPIKey(r *http.Request, customerID uuid.UUID, prov providers.Provider) (string, error) {
	return h.resolveAPIKeyCtx(r.Context(), customerID, prov), nil
}

// resolveAPIKeyCtx delegates to the cloud-injected resolver when set;
// falls back to environment variables for OSS dev. Empty string means
// "let the provider client surface a clear auth error to the user".
func (h *Handler) resolveAPIKeyCtx(ctx context.Context, customerID uuid.UUID, prov providers.Provider) string {
	if h.keys != nil {
		if key, err := h.keys.ResolveKey(ctx, customerID, string(prov)); err == nil && key != "" {
			return key
		}
	}
	switch prov {
	case providers.ProviderOpenAI:
		return os.Getenv("OPENAI_API_KEY")
	case providers.ProviderAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY")
	case providers.ProviderBedrock:
		return os.Getenv("AWS_ACCESS_KEY_ID")
	case providers.ProviderOllama:
		return "" // Ollama is keyless by default.
	}
	return ""
}

// estimateCostCents returns integer cents for the workspace_messages
// table (cost_cents is INTEGER). Sub-cent charges round down to 0,
// which is fine for the per-message audit row — the precise float
// number lives in the trace record (float64 column). Both call into
// the same llmpipeline rate table so the two surfaces never diverge.
func estimateCostCents(_ /*provider*/, model string, inputTokens, outputTokens int) int {
	return int(llmpipeline.EstimateCostCents(model, inputTokens, outputTokens))
}

// estimateCostCentsFloat returns fractional cents for trace rows so a
// $0.0001 charge isn't truncated to $0 in /traces.
func estimateCostCentsFloat(_ /*provider*/, model string, inputTokens, outputTokens int) float64 {
	return llmpipeline.EstimateCostCents(model, inputTokens, outputTokens)
}

func strPtr(s string) *string { return &s }
