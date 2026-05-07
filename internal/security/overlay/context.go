package overlay

import "context"

// Active is the shape of overlay state threaded through a request
// context. The security engine, detectors, and plugin runner read
// this when they need to honour overlay additions at runtime.
//
// Only one overlay is attached per context — the active one. Shadow
// runs build a second Active from a different snapshot and never share
// context with the primary path.
type Active struct {
	Snapshot *OverlaySnapshot
	Identity Identity
}

type ctxKey struct{}

// WithActive returns a new context with the overlay snapshot attached.
// A nil snapshot returns the context unchanged — callers don't need a
// guard before calling this.
func WithActive(ctx context.Context, snap *OverlaySnapshot, ident Identity) context.Context {
	if snap == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, &Active{Snapshot: snap, Identity: ident})
}

// FromContext returns the overlay state stashed by WithActive, if any.
// The second return value is false when no overlay is attached — the
// typical case for tenants without a configured overlay.
func FromContext(ctx context.Context) (*Active, bool) {
	v, ok := ctx.Value(ctxKey{}).(*Active)
	if !ok || v == nil || v.Snapshot == nil {
		return nil, false
	}
	return v, true
}
