package gatewaypolicy

import (
	"context"
	"sync/atomic"
)

// CacheObservation is stamped by a host's response-cache extension. Caller
// headers cannot set it. Original provider response usage remains intact on
// the wire; recorder integrations use this receipt to avoid billing replay
// tokens as a new provider invocation.
type CacheObservation struct {
	Status            string
	Operation         string
	SavedInputTokens  int
	SavedOutputTokens int
	SavedCostUSD      float64
}

type cacheReceipt struct {
	value atomic.Pointer[CacheObservation]
}
type cacheKey struct{}

func WithCacheReceipt(ctx context.Context) context.Context {
	return context.WithValue(ctx, cacheKey{}, &cacheReceipt{})
}

func SetCacheObservation(ctx context.Context, observation CacheObservation) {
	if receipt, ok := ctx.Value(cacheKey{}).(*cacheReceipt); ok {
		receipt.value.Store(&observation)
	}
}

func CacheFrom(ctx context.Context) CacheObservation {
	if receipt, ok := ctx.Value(cacheKey{}).(*cacheReceipt); ok {
		if value := receipt.value.Load(); value != nil {
			return *value
		}
	}
	return CacheObservation{}
}
