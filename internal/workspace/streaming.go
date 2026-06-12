package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/providers"
)

// streamSendMessage handles POST /conversations/{id}/messages/stream.
// Server-sent events protocol:
//
//	event: token   data: {"delta": "<text>"}
//	event: done    data: {"message": {full Message}}
//	event: error   data: {"error": "<msg>"}
//
// One user message + one assistant message persist regardless of stream
// success — the user's text is durable even if the provider call fails.
func (h *Handler) streamSendMessage(w http.ResponseWriter, r *http.Request) {
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

	// Resolve assistant config + RAG context (RAG via knowledge.go).
	systemPrompt, prov, model := h.resolveAssistantConfig(r.Context(), cid, conv, body)

	// Defense-in-depth model whitelist enforcement (same check the
	// non-streaming send path runs). Reject before the response goes
	// SSE — once we flush the headers, JSON errors no longer travel
	// through the standard error path.
	if err := h.assertModelAllowed(r.Context(), cid, userIDFromCtx(r.Context()), string(prov), model); err != nil {
		writeStructuredError(w, http.StatusForbidden,
			"model_not_allowed",
			err.Error(),
			map[string]any{"provider": string(prov), "model": model})
		return
	}

	// Pre-flight security scan on the user's message. Same engine the
	// gateway uses; nil-safe when the workspace handler isn't wired
	// with one. We scan BEFORE writing SSE headers so a block returns
	// a normal HTTP error (consistent with model_not_allowed). After
	// the SSE headers go out, error responses can only travel as
	// SSE events — keep the early reject path simple.
	startedAt := time.Now().UTC()
	traceID := uuid.New()
	userID := userIDFromCtx(r.Context())
	ipHash, userAgent := requestSignals(r)
	// Extract image data URLs BEFORE the security scan. The base64
	// payload looks like a high-entropy secret to the secrets
	// detector, which would redact every Nth chunk of it and leave
	// `[REDACTED]` markers — breaking image-extract regex on the
	// persisted message and stripping the image from the LLM call.
	// Scan the text portion only; re-embed images afterwards.
	scanText, scanImages := extractImagesForProvider(body.Content)
	scan, _, scanErr := h.scanUserMessage(r.Context(), cid, id, userID, ipHash, userAgent, scanText)
	if scanErr != nil {
		// Only set in fail-closed mode (BASTIO_SCAN_FAIL_MODE=closed).
		// We're still pre-SSE here, so a plain HTTP error works —
		// consistent with the model_not_allowed reject above.
		writeStructuredError(w, http.StatusServiceUnavailable,
			"security_scan_unavailable",
			"security scan unavailable, message not sent",
			nil)
		return
	}

	if scan != nil && scan.ShouldBlock {
		// Persist user message + a blocked assistant message so the
		// chat history reflects what happened. No provider call.
		_, _ = h.store.AppendMessage(r.Context(), Message{
			ConversationID: id, CustomerID: cid,
			Role: "user", Content: body.Content,
		})
		blocked := "Bastio policy blocked this prompt before it left your tenant. " +
			"Detected: " + joinThreatTypes(scan.ThreatTypes) + ". " +
			"Edit and re-send, or contact your workspace admin."
		finish := "blocked"
		errStr := "blocked by security policy"
		_, _ = h.store.AppendMessage(r.Context(), Message{
			ConversationID: id, CustomerID: cid,
			Role: "assistant", Content: blocked,
			Provider: strPtr(string(prov)), Model: strPtr(model),
			FinishReason: &finish, Error: &errStr,
		})
		h.recordChatTrace(chatTraceInput{
			customerID:     cid,
			conversationID: id,
			traceID:        traceID,
			userID:         userID,
			ipHash:         ipHash,
			userAgent:      userAgent,
			provider:       string(prov),
			model:          model,
			startedAt:      startedAt,
			completedAt:    time.Now().UTC(),
			finishReason:   "blocked",
			requestBody:    body.Content,
			responseBody:   blocked,
			scanResult:     scan,
		})
		writeStructuredError(w, http.StatusForbidden,
			"policy_blocked",
			blocked,
			map[string]any{"threat_types": scan.ThreatTypes, "threat_score": scan.ThreatScore})
		return
	}

	// Per-member budget enforcement. Mirrors the non-streaming path —
	// any block here returns 403 with a structured budget error
	// before SSE headers go out.
	if budgetErr := h.enforceMemberBudget(r.Context(), cid, userID); budgetErr != nil {
		var be *budgetExceededError
		userFacing := budgetErr.Error()
		var meta map[string]any
		if errors.As(budgetErr, &be) {
			userFacing = be.userMessage()
			meta = map[string]any{"kind": be.Kind, "used": be.Used, "limit": be.Limit}
		}
		_, _ = h.store.AppendMessage(r.Context(), Message{
			ConversationID: id, CustomerID: cid,
			Role: "user", Content: body.Content,
		})
		finish := "budget_exceeded"
		errStr := budgetErr.Error()
		_, _ = h.store.AppendMessage(r.Context(), Message{
			ConversationID: id, CustomerID: cid,
			Role: "assistant", Content: userFacing,
			Provider: strPtr(string(prov)), Model: strPtr(model),
			FinishReason: &finish, Error: &errStr,
		})
		h.recordChatTrace(chatTraceInput{
			customerID:     cid,
			conversationID: id,
			traceID:        traceID,
			userID:         userID,
			ipHash:         ipHash,
			userAgent:      userAgent,
			provider:       string(prov),
			model:          model,
			startedAt:      startedAt,
			completedAt:    time.Now().UTC(),
			finishReason:   "budget_exceeded",
			requestBody:    body.Content,
			responseBody:   userFacing,
		})
		writeStructuredError(w, http.StatusForbidden, "budget_exceeded", userFacing, meta)
		return
	}

	// Sanitization: if the engine masked PII / etc. in the text
	// portion, use the rewritten text. Images are re-attached as-is
	// — they survived the scan because we didn't show them to it.
	textPart := scanText
	if scan != nil && scan.SanitizedContent != "" {
		textPart = scan.SanitizedContent
	}
	userContent := recomposeWithImages(textPart, scanImages)

	systemPrompt, citations := h.augmentWithRAG(r.Context(), cid, conv, userContent, systemPrompt)

	// Persist user message first.
	if _, err := h.store.AppendMessage(r.Context(), Message{
		ConversationID: id,
		CustomerID:     cid,
		Role:           "user",
		Content:        userContent,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Set up SSE response. Flush headers immediately so the browser sees
	// the connection open before we have content.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering.
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	// Build LLM message history.
	priorMsgs, err := h.store.ListMessages(r.Context(), cid, id)
	if err != nil {
		sseEmit(w, flusher, "error", map[string]string{"error": err.Error()})
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
		// Mirror conversations.go: peel inline data:image base64
		// markdown out of the content and pass as multimodal images
		// to providers that support them.
		text, imgs := extractImagesForProvider(m.Content)
		if len(imgs) > 0 && text == "" {
			text = "[image attached]"
		}
		// See conversations.go's matching block for rationale —
		// tool-result content is the indirect-injection vector,
		// scan + sanitize before re-entering the model.
		if m.Role == "tool" {
			text = h.gateToolResult(r.Context(), cid, text)
		}
		llmMsgs = append(llmMsgs, providers.Message{Role: m.Role, Content: text, Images: imgs})
	}

	// Stream provider response. ChatStream returns provider-specific SSE
	// data; deltaFromChunk normalizes it to plain text deltas for the
	// dashboard. Unknown providers fall through to non-streaming below.
	final, ferr := h.streamProvider(r.Context(), w, flusher, cid, prov, model, llmMsgs)
	if ferr != nil {
		// Persist an error placeholder so the conversation history reflects
		// the failed turn — otherwise the user types into a void.
		errStr := ferr.Error()
		if errMsg, perr := h.store.AppendMessage(r.Context(), Message{
			ConversationID: id, CustomerID: cid,
			Role: "assistant", Content: "(provider call failed)",
			Provider: strPtr(string(prov)), Model: strPtr(model), Error: &errStr,
		}); perr == nil {
			sseEmit(w, flusher, "done", map[string]any{"message": errMsg})
		}
		sseEmit(w, flusher, "error", map[string]string{"error": errStr})
		return
	}

	// Persist final assistant message + record the trace so workspace
	// chat shows up in /traces alongside gateway traffic.
	finishReason := final.finishReason
	costCents := estimateCostCentsFloat(string(prov), model, final.promptTokens, final.completionTokens)
	h.recordChatTrace(chatTraceInput{
		customerID:     cid,
		conversationID: id,
		traceID:        traceID,
		userID:         userID,
		ipHash:         ipHash,
		userAgent:      userAgent,
		provider:       string(prov),
		model:          model,
		startedAt:      startedAt,
		completedAt:    time.Now().UTC(),
		inputTokens:    final.promptTokens,
		outputTokens:   final.completionTokens,
		costCents:      costCents,
		finishReason:   finishReason,
		requestBody:    userContent,
		responseBody:   final.content,
		scanResult:     scan,
	})

	// =========================================================
	// POST-FLIGHT OUTPUT SCAN — the user has already seen the
	// streamed chunks (we can't unsend SSE), but we still
	// protect storage integrity + audit visibility:
	//
	//   block    → persisted message says "the model's response
	//              was blocked"; original is dropped from
	//              storage. Future replays of this thread don't
	//              echo the bad content.
	//   sanitize → persisted message holds the rewritten text.
	//              The user saw the original via SSE, but the
	//              audit log + future re-renders use the clean
	//              version.
	//   allow    → persisted as-is.
	//
	// Yes, the user saw the raw stream — that's the price of
	// streaming UX without an in-flight rewriter. v2.1 will
	// add a chunk-level rewriter so the user sees sanitized
	// content as it arrives. For now, the storage + audit
	// layers are the source of truth.
	// =========================================================
	finalContent := final.content
	persistFinish := finishReason
	var persistError *string
	outDecision, _ := scanForIngest(r.Context(), h.secEngine, h.secProfiles, cid, final.content)
	if outDecision != nil {
		switch outDecision.Action {
		case "block":
			finalContent = "Bastio blocked the model's response — it contained " +
				joinCategories(outDecision.Categories) +
				". This conversation has been flagged in your workspace's audit log."
			fr := "blocked_output"
			persistFinish = fr
			es := "blocked by output security policy"
			persistError = &es
		case "sanitize":
			finalContent = outDecision.SanitizedContent
		}
	}

	asstMsg, err := h.store.AppendMessage(r.Context(), Message{
		ConversationID:   id,
		CustomerID:       cid,
		Role:             "assistant",
		Content:          finalContent,
		Provider:         strPtr(string(prov)),
		Model:            strPtr(model),
		PromptTokens:     final.promptTokens,
		CompletionTokens: final.completionTokens,
		CostCents:        estimateCostCents(string(prov), model, final.promptTokens, final.completionTokens),
		FinishReason:     &persistFinish,
		Error:            persistError,
		Metadata:         encodeCitationsMetadata(citations),
	})
	if err != nil {
		sseEmit(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}
	sseEmit(w, flusher, "done", map[string]any{"message": asstMsg})
}

// streamResult is what streamProvider accumulates as it consumes the
// provider stream — passed back to the caller for persistence.
type streamResult struct {
	content          string
	promptTokens     int
	completionTokens int
	finishReason     string
}

// streamProvider opens the provider stream and pumps deltas to the
// client as SSE. Falls back to a non-streaming Chat call when the
// provider isn't a known SSE format we can decode.
func (h *Handler) streamProvider(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	customerID uuid.UUID,
	prov providers.Provider,
	model string,
	msgs []providers.Message,
) (streamResult, error) {
	if h.registry == nil {
		return streamResult{}, fmt.Errorf("provider registry not configured")
	}
	client, err := h.registry.Get(prov)
	if err != nil {
		return streamResult{}, err
	}
	apiKey := h.resolveAPIKeyCtx(ctx, customerID, prov)

	if !supportsStreaming(prov) {
		// Non-streaming fallback: full response → emit as one delta.
		resp, err := client.Chat(ctx, &providers.ChatRequest{
			Provider: prov, Model: model, Messages: msgs,
		}, apiKey)
		if err != nil {
			return streamResult{}, err
		}
		sseEmit(w, flusher, "token", map[string]string{"delta": resp.Content})
		return streamResult{
			content:          resp.Content,
			promptTokens:     resp.InputTokens,
			completionTokens: resp.OutputTokens,
			finishReason:     resp.FinishReason,
		}, nil
	}

	chunks, err := client.ChatStream(ctx, &providers.ChatRequest{
		Provider: prov, Model: model, Messages: msgs, Stream: true,
	}, apiKey)
	if err != nil {
		return streamResult{}, err
	}

	var out streamResult
	for chunk := range chunks {
		if chunk.Error != nil {
			return out, chunk.Error
		}
		if chunk.Done {
			break
		}
		delta, finish, usage := decodeChunk(prov, chunk.Data)
		if delta != "" {
			out.content += delta
			sseEmit(w, flusher, "token", map[string]string{"delta": delta})
		}
		if finish != "" {
			out.finishReason = finish
		}
		if usage.PromptTokens > 0 {
			out.promptTokens = usage.PromptTokens
		}
		if usage.CompletionTokens > 0 {
			out.completionTokens = usage.CompletionTokens
		}
	}

	// Some providers (Anthropic) report usage via a final message_stop
	// event, others (OpenAI) only when stream_options.include_usage is
	// set. Token-count fallbacks: estimate from content if we got nothing.
	if out.completionTokens == 0 && out.content != "" {
		out.completionTokens = len(out.content) / 4 // rough heuristic
	}
	return out, nil
}

// supportsStreaming reports whether we can decode this provider's SSE
// format. Add providers here as their decoders land in decodeChunk.
func supportsStreaming(p providers.Provider) bool {
	switch p {
	case providers.ProviderOpenAI, providers.ProviderAnthropic:
		return true
	}
	return false
}

// chunkUsage carries provider-reported token usage when the stream
// surfaces it. Zero values mean "not in this chunk".
type chunkUsage struct {
	PromptTokens     int
	CompletionTokens int
}

// decodeChunk parses a single provider SSE data line and extracts the
// content delta + finish reason + usage. Unknown shapes return zero
// values — the caller falls through to length heuristics.
func decodeChunk(prov providers.Provider, data []byte) (delta string, finish string, usage chunkUsage) {
	if len(data) == 0 {
		return
	}
	switch prov {
	case providers.ProviderOpenAI:
		return decodeOpenAIChunk(data)
	case providers.ProviderAnthropic:
		return decodeAnthropicChunk(data)
	}
	return
}

// OpenAI format:
//
//	{"id":"...","choices":[{"delta":{"content":"Hello"},"finish_reason":null}],"usage":...}
func decodeOpenAIChunk(data []byte) (string, string, chunkUsage) {
	var v struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", "", chunkUsage{}
	}
	var delta, finish string
	if len(v.Choices) > 0 {
		delta = v.Choices[0].Delta.Content
		if v.Choices[0].FinishReason != nil {
			finish = *v.Choices[0].FinishReason
		}
	}
	var u chunkUsage
	if v.Usage != nil {
		u.PromptTokens = v.Usage.PromptTokens
		u.CompletionTokens = v.Usage.CompletionTokens
	}
	return delta, finish, u
}

// Anthropic format (events):
//
//	{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}
//	{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{...}}
func decodeAnthropicChunk(data []byte) (string, string, chunkUsage) {
	var v struct {
		Type  string `json:"type"`
		Delta struct {
			Text       string `json:"text"`
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", "", chunkUsage{}
	}
	var u chunkUsage
	if v.Usage != nil {
		u.PromptTokens = v.Usage.InputTokens
		u.CompletionTokens = v.Usage.OutputTokens
	}
	switch v.Type {
	case "content_block_delta":
		return v.Delta.Text, "", u
	case "message_delta":
		return "", v.Delta.StopReason, u
	}
	return "", "", u
}

// resolveAssistantConfig pulls the assistant's system prompt + provider
// + model defaults, with body overrides winning. Prepends a
// workspace-level persona instruction (when configured) and a
// per-assistant language instruction (auto-detect or forced) so every
// call site that uses the system prompt — non-streaming and streaming
// — gets identical behavior.
func (h *Handler) resolveAssistantConfig(
	ctx context.Context,
	customerID uuid.UUID,
	conv *Conversation,
	body SendMessageRequest,
) (string, providers.Provider, string) {
	systemPrompt := ""
	provName := body.Provider
	model := body.Model
	var assistantLang *string
	if conv.AssistantID != nil {
		a, err := h.store.GetAssistant(ctx, customerID, *conv.AssistantID)
		if err == nil && a != nil {
			systemPrompt = a.SystemPrompt
			assistantLang = a.Language
			if provName == "" {
				provName = a.DefaultProvider
			}
			if model == "" {
				model = a.DefaultModel
			}
		}
	}
	if provName == "" {
		provName = "openai"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	// Persona instruction goes first — it's the highest-level
	// context ("you are Bob, the company assistant"). Language
	// instruction follows so the model knows how to respond in
	// Bob's voice. Per-assistant system_prompt is appended last so
	// task-specific instructions override the workspace defaults.
	prefix := ""
	if settings, err := h.store.GetSettings(ctx, customerID); err == nil && settings != nil {
		if p := personaInstruction(settings); p != "" {
			prefix += p + "\n\n"
		}
	}
	if l := languageInstruction(assistantLang); l != "" {
		prefix += l + "\n\n"
	}
	return prefix + systemPrompt, providers.Provider(provName), model
}

// personaInstruction renders the workspace's AI persona as a system
// instruction. Returns "" when no persona fields are set — the
// assistant's raw system_prompt is used as-is.
//
// The shape mirrors Favio's getPersonaPrompt() helper so existing
// users see the same behavior after migration.
func personaInstruction(s *Settings) string {
	if s == nil {
		return ""
	}
	name := strDeref(s.AIPersonaName)
	personality := strDeref(s.AIPersonaPersonality)
	tone := strDeref(s.AIPersonaTone)
	if name == "" && personality == "" && tone == "" {
		return ""
	}
	out := "You are "
	if name != "" {
		out += name + ", "
	}
	if personality != "" {
		out += "the company assistant. " + personality
	} else {
		out = strings.TrimSuffix(out, ", ")
		out += ", the company assistant."
	}
	if tone != "" {
		out += " Tone: " + tone + "."
	}
	return out
}

// languageInstruction returns the per-assistant language directive.
// nil/empty = auto-detect from user input, ISO code = forced.
func languageInstruction(lang *string) string {
	if lang == nil || strings.TrimSpace(*lang) == "" {
		return "Detect the language of the user's message and reply in the same language."
	}
	return "Always respond in " + *lang + " regardless of the user's input language."
}

func strDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// assertModelAllowed enforces the workspace's per-user effective
// allowed_models list. Returns nil when the user can use the
// requested (provider, model) pair, an error otherwise.
//
// Empty effective list = "open" — every (provider, model) is allowed.
// Non-empty = strict whitelist; the pair must match exactly.
//
// Lookup errors fail open (return nil) — the rest of the chat path
// has its own error handling, and a transient settings-fetch failure
// shouldn't lock the user out of their own workspace.
func (h *Handler) assertModelAllowed(ctx context.Context, customerID uuid.UUID, userID, provider, model string) error {
	allowed, err := h.store.EffectiveAllowedModels(ctx, customerID, userID)
	if err != nil {
		// Fail open — log the lookup failure if we ever wire structured
		// errors here, but don't let a Postgres blip lock chat.
		return nil
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, a := range allowed {
		if a.Provider == provider && a.Model == model {
			return nil
		}
	}
	return fmt.Errorf("model %s/%s is not in your workspace's allowed list — pick one of the models in the picker", provider, model)
}

// sseEmit writes a typed SSE event + flushes. Errors writing to a
// disconnected client are silently dropped — there's no recovery and
// the caller has no way to do anything meaningful.
func sseEmit(w http.ResponseWriter, flusher http.Flusher, event string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
	if flusher != nil {
		flusher.Flush()
	}
}
