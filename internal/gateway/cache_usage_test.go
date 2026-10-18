package gateway

import (
	"context"
	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
	"net/http"
	"testing"
)

func TestResponseCacheUsageReceiptDoesNotDoubleCountProviderWork(t *testing.T) {
	for _, status := range []string{"hit", "coalesced", "miss", "bypass", ""} {
		t.Run(status, func(t *testing.T) {
			ctx := gatewaypolicy.WithCacheReceipt(context.Background())
			gatewaypolicy.SetCacheObservation(ctx, gatewaypolicy.CacheObservation{Status: status, Operation: "classify", SavedInputTokens: 100, SavedOutputTokens: 20, SavedCostUSD: 0.01})
			input := recordInput{InputTokens: 100, OutputTokens: 20, RespBody: []byte(`{"usage":{"prompt_tokens":100}}`), Headers: http.Header{"X-Bastio-Cache": {"hit"}}}
			tags := map[string]string{}
			cost := 1.0
			applyCacheTraceUsage(ctx, &input, tags, &cost)
			if status == "hit" || status == "coalesced" {
				if input.InputTokens != 0 || input.OutputTokens != 0 || cost != 0 || tags["cache.saved_input_tokens"] != "100" || tags["cache.status"] != status {
					t.Fatalf("replay billed as upstream usage: %+v %v %v", input, tags, cost)
				}
			} else if input.InputTokens != 100 || input.OutputTokens != 20 || cost != 1 {
				t.Fatal("real provider usage removed")
			}
			if string(input.RespBody) != `{"usage":{"prompt_tokens":100}}` {
				t.Fatal("provider response body was changed")
			}
		})
	}
	// Untrusted client headers cannot fabricate a receipt.
	input := recordInput{InputTokens: 100, Headers: http.Header{"X-Bastio-Cache": {"hit"}}}
	cost := 1.0
	applyCacheTraceUsage(context.Background(), &input, map[string]string{}, &cost)
	if input.InputTokens != 100 || cost != 1 {
		t.Fatal("untrusted header suppressed usage")
	}
}
