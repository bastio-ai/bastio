// Package tenant carries the resolved customer/tenant identity through
// a request context. OSS installs inject the single default tenant via
// middleware; cloud installs inject the customer derived from a session
// or token. Handlers and sqlc queries always read the tenant from context
// so a missing or wrong binding fails loudly rather than silently scoping
// to the wrong data.
package tenant

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// DefaultOSSID is the single-tenant customer seeded by the OSS install.
// Cloud code never uses this constant; cloud middleware must inject the
// real customer ID from session/token.
var DefaultOSSID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// ErrMissing is returned by FromContext when no tenant has been attached.
var ErrMissing = errors.New("tenant: missing customer id in context")

type ctxKey struct{}

// WithID attaches customer id to ctx. Returns a new context; caller must
// pass the returned ctx to downstream code.
func WithID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext reads the customer id from ctx. Returns ErrMissing if no
// tenant is bound — this indicates a wiring bug (missing middleware)
// rather than an auth failure.
func FromContext(ctx context.Context) (uuid.UUID, error) {
	v := ctx.Value(ctxKey{})
	id, ok := v.(uuid.UUID)
	if !ok {
		return uuid.Nil, ErrMissing
	}
	return id, nil
}

// MustFromContext reads the tenant or panics. Use only where a missing
// tenant is a programmer error and should crash the request.
func MustFromContext(ctx context.Context) uuid.UUID {
	id, err := FromContext(ctx)
	if err != nil {
		panic(err)
	}
	return id
}
