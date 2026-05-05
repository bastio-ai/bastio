package security

import "context"

type tokenMapCtxKey struct{}

// WithTokenMap stores a request-scoped TokenMap on the context so the
// gateway response path can restore originals after the provider call.
// Callers must never pass this context to third-party clients — the map
// values are PII.
func WithTokenMap(ctx context.Context, tm *TokenMap) context.Context {
	if tm == nil {
		return ctx
	}
	return context.WithValue(ctx, tokenMapCtxKey{}, tm)
}

// TokenMapFromContext returns the TokenMap previously stored on ctx.
// Returns (nil, false) when no map was attached.
func TokenMapFromContext(ctx context.Context) (*TokenMap, bool) {
	tm, ok := ctx.Value(tokenMapCtxKey{}).(*TokenMap)
	if !ok || tm == nil {
		return nil, false
	}
	return tm, true
}
