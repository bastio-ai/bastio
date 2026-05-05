package workspace

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// enforceMemberBudget checks whether the user is allowed to start
// another LLM call right now. Returns nil when allowed, a typed
// error when blocked.
//
// Two limits, both per-member, both nullable in PG:
//
//   monthly_token_limit — total prompt+completion tokens this
//   calendar month (UTC). Compared against the running sum so a
//   user with 95k of a 100k cap can still send a small reply but
//   gets blocked once they cross the line.
//
//   daily_rate_limit — assistant-role messages in the last 24h.
//   This is a rate counter, not a token counter — it's the
//   "stop me from clicking send 500 times in a panic" guard.
//
// Lookup errors fail OPEN. A transient PG hiccup shouldn't lock the
// user out of their own workspace; the security/observability
// pipeline will still record what happened.
//
// Called from runProvider + streamProvider just after the security
// scan returns and before the provider call goes out.
func (h *Handler) enforceMemberBudget(ctx context.Context, customerID uuid.UUID, userID string) error {
	if h.store == nil || userID == "" {
		return nil
	}
	monthlyLimit, dailyLimit, err := h.store.GetMemberBudgets(ctx, customerID, userID)
	if err != nil {
		return nil // fail-open per docstring
	}
	if monthlyLimit == nil && dailyLimit == nil {
		return nil // no caps set for this user
	}
	monthlyTokens, last24hMsgs, err := h.store.MemberUsage(ctx, customerID, userID)
	if err != nil {
		return nil // fail-open
	}
	if monthlyLimit != nil && monthlyTokens >= *monthlyLimit {
		return &budgetExceededError{
			Kind:  "monthly_tokens",
			Used:  monthlyTokens,
			Limit: *monthlyLimit,
		}
	}
	if dailyLimit != nil && last24hMsgs >= *dailyLimit {
		return &budgetExceededError{
			Kind:  "daily_messages",
			Used:  last24hMsgs,
			Limit: *dailyLimit,
		}
	}
	return nil
}

// budgetExceededError is returned by enforceMemberBudget when a user
// is at or over a configured cap. The Kind names the cap so the
// caller can render a precise message ("you've used 1.2M of your
// 1M monthly tokens" vs "you've sent 50 messages today").
type budgetExceededError struct {
	Kind  string // "monthly_tokens" | "daily_messages"
	Used  int
	Limit int
}

func (e *budgetExceededError) Error() string {
	return fmt.Sprintf("workspace budget exceeded: %s (%d/%d)", e.Kind, e.Used, e.Limit)
}

// userMessage returns a short, user-facing string describing the
// breach. Surfaced as the assistant-bubble content when a chat send
// is blocked.
func (e *budgetExceededError) userMessage() string {
	switch e.Kind {
	case "monthly_tokens":
		return fmt.Sprintf("Monthly token limit reached (%d / %d). Resets at the start of next month, or ask an admin to raise the cap.", e.Used, e.Limit)
	case "daily_messages":
		return fmt.Sprintf("Daily message limit reached (%d / %d). Resets in 24 hours.", e.Used, e.Limit)
	default:
		return "Usage limit reached. Contact an admin to raise the cap."
	}
}
