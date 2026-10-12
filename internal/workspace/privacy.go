package workspace

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

type privacyKey struct{}
type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func privateChat(ctx context.Context) bool { v, _ := ctx.Value(privacyKey{}).(bool); return v }

// Temporary chats accept bounded browser-held history. System/tool roles cannot
// be supplied by a client, and every replayed turn passes the security engine.
func (h *Handler) prepareChat(r *http.Request, c *Conversation, b SendMessageRequest) (*http.Request, error) {
	if c.AssistantID != nil {
		ok, err := h.store.canAccess(r.Context(), c.CustomerID, *c.AssistantID, "assistant")
		if err != nil || !ok {
			return r, fmt.Errorf("assistant unavailable to your account")
		}
	}
	st, err := h.store.GetSettings(r.Context(), c.CustomerID)
	if err != nil {
		return r, fmt.Errorf("storage policy unavailable")
	}
	temporary := !st.StoreConversationContent || !c.ContentStored
	if len(b.History) > 100 {
		return r, fmt.Errorf("history is limited to 100 messages")
	}
	total := len(b.Content)
	for _, m := range b.History {
		if m.Role != "user" && m.Role != "assistant" {
			return r, fmt.Errorf("history permits only user and assistant roles")
		}
		total += len(m.Content)
	}
	if total > 32*1024*1024 {
		return r, fmt.Errorf("chat context exceeds 32 MB")
	}
	return r.WithContext(context.WithValue(r.Context(), privacyKey{}, temporary)), nil
}
func (h *Handler) chatHistory(ctx context.Context, cid, id uuid.UUID, b SendMessageRequest) ([]Message, error) {
	if !privateChat(ctx) {
		return h.store.ListMessages(ctx, cid, id)
	}
	out := make([]Message, 0, len(b.History)+1)
	for _, m := range b.History {
		text, images := extractImagesForProvider(m.Content)
		scan, _, err := h.scanUserMessage(ctx, cid, id, userIDFromCtx(ctx), "", "", text)
		if err != nil {
			return nil, err
		}
		if scan != nil && scan.ShouldBlock {
			return nil, fmt.Errorf("history blocked by security policy; start a new chat")
		}
		if scan != nil && scan.SanitizedContent != "" {
			text = scan.SanitizedContent
		}
		out = append(out, Message{Role: m.Role, Content: recomposeWithImages(text, images)})
	}
	return append(out, Message{Role: "user", Content: b.Content}), nil
}
