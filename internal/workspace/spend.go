package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/google/uuid"
)

const workspaceMaxOutputTokens = 8192

type SpendStatus struct {
	UsedCents  float64 `json:"used_cents"`
	LimitCents *int    `json:"limit_cents"`
	NearLimit  bool    `json:"near_limit"`
}

func (s *Store) SpendStatus(ctx context.Context, cid uuid.UUID) (SpendStatus, error) {
	var out SpendStatus
	err := s.pool.QueryRow(ctx, `SELECT s.spend_cap_cents,COALESCE((SELECT SUM(amount_cents) FROM workspace_spend WHERE customer_id=$1 AND created_at>=date_trunc('month',NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),0) FROM workspace_settings s WHERE s.customer_id=$1`, cid).Scan(&out.LimitCents, &out.UsedCents)
	out.NearLimit = out.LimitCents != nil && *out.LimitCents > 0 && out.UsedCents >= float64(*out.LimitCents)*0.8
	return out, err
}
func (h *Handler) spendStatus(w http.ResponseWriter, r *http.Request) {
	out, err := h.store.SpendStatus(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, 500, "spending status unavailable")
		return
	}
	writeJSON(w, 200, out)
}

func (h *Handler) reserveSpend(ctx context.Context, cid uuid.UUID, model string, msgs []providers.Message, provider ...providers.Provider) (uuid.UUID, error) {
	if h.store == nil {
		return uuid.Nil, nil
	}
	// Byte count intentionally overestimates input tokens, with room for framing.
	input := 1024
	for _, m := range msgs {
		input += len(m.Content) + len(m.ReasoningContent) + 32
		for _, state := range m.ProviderState {
			input += len(state)
		}
		for _, image := range m.Images {
			input += len(image.Data)
		}
	}
	if tools := callOptions(ctx).tools; len(tools) > 0 {
		raw, _ := json.Marshal(tools)
		input += len(raw)
	}
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			input += len(c.Arguments) + len(c.Name) + 32
		}
	}
	var prov providers.Provider
	if len(provider) > 0 {
		prov = provider[0]
	}
	estimate := estimateCostCentsFloat(string(prov), model, input, callMaxOutput(ctx))
	tx, err := h.store.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("spending limit unavailable")
	}
	defer tx.Rollback(ctx)
	var cap *int
	if err = tx.QueryRow(ctx, `SELECT spend_cap_cents FROM workspace_settings WHERE customer_id=$1 FOR UPDATE`, cid).Scan(&cap); err != nil {
		return uuid.Nil, fmt.Errorf("spending limit unavailable")
	}
	if cap != nil && *cap > 0 {
		if estimate == 0 && prov != providers.ProviderOllama {
			return uuid.Nil, fmt.Errorf("model pricing unavailable; choose a priced model or ask your admin to remove the spending limit")
		}
		var used float64
		if err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(amount_cents),0) FROM workspace_spend WHERE customer_id=$1 AND created_at>=date_trunc('month',NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`, cid).Scan(&used); err != nil {
			return uuid.Nil, fmt.Errorf("spending limit unavailable")
		}
		if used+estimate > float64(*cap) {
			return uuid.Nil, fmt.Errorf("workspace spending limit reached or insufficient allowance for this request; ask an admin to raise the limit")
		}
	}
	id := uuid.New()
	if _, err = tx.Exec(ctx, `INSERT INTO workspace_spend(id,customer_id,user_id,amount_cents,state) VALUES($1,$2,$3,$4,'reserved')`, id, cid, userIDFromCtx(ctx), estimate); err != nil {
		return uuid.Nil, fmt.Errorf("reserve spending: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("reserve spending: %w", err)
	}
	return id, nil
}

// When a provider omits usage, retain the conservative allowance instead of
// undercounting hidden reasoning or image input from visible response text.
func accountMissingUsage(msgs []providers.Message, input, output *int, maximum ...int) {
	if *input == 0 {
		*input = 1024
		for _, m := range msgs {
			*input += len(m.Content) + len(m.ReasoningContent) + 32
			for _, state := range m.ProviderState {
				*input += len(state)
			}
			for _, image := range m.Images {
				*input += len(image.Data)
			}
		}
	}
	if *output == 0 {
		*output = workspaceMaxOutputTokens
		if len(maximum) > 0 && maximum[0] > 0 && maximum[0] <= workspaceMaxOutputTokens {
			*output = maximum[0]
		}
	}
}
func (h *Handler) settleSpend(cid, id uuid.UUID, cost float64) error {
	if id == uuid.Nil {
		return nil
	}
	// Use an independent bounded context so disconnecting cannot lose accounting.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
		return fmt.Errorf("invalid model cost")
	}
	_, err := h.store.pool.Exec(ctx, `UPDATE workspace_spend SET amount_cents=$3,state='settled' WHERE customer_id=$1 AND id=$2`, cid, id, cost)
	return err
}
