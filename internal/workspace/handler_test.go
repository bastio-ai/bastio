package workspace

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestRoutesSmoke ensures the chi router is constructable with nil deps.
// A real DB-backed integration test would need a postgres fixture; this
// smoke test catches obvious wiring breakage in the handler signature
// without requiring infrastructure.
func TestRoutesSmoke(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil, nil, nil)
	r := h.Routes()
	if r == nil {
		t.Fatal("Routes() returned nil router")
	}
}

// TestCustomerIDFromCtxFallback covers the OSS single-tenant fallback.
// When no middleware injects a customer ID into the request context, the
// handler must use the default OSS customer so workspace remains usable
// out-of-the-box on a fresh OSS install.
func TestCustomerIDFromCtxFallback(t *testing.T) {
	t.Parallel()

	got := customerIDFromCtx(context.Background())
	want := uuid.MustParse(defaultCustomerIDStr)
	if got != want {
		t.Fatalf("default customer mismatch: got %s, want %s", got, want)
	}
}

// TestCustomerIDFromCtxOverride verifies cloud's auth middleware path:
// when it stuffs a uuid into the well-known context key, the handler
// scopes its queries to the authenticated customer.
func TestCustomerIDFromCtxOverride(t *testing.T) {
	t.Parallel()

	want := uuid.New()
	ctx := context.WithValue(context.Background(), ctxCustomerID, want)
	if got := customerIDFromCtx(ctx); got != want {
		t.Fatalf("override customer mismatch: got %s, want %s", got, want)
	}
}

func TestEstimateCostCents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		model string
		in    int
		out   int
		want  int
	}{
		{"gpt-4o-mini", 1_000_000, 1_000_000, 75},     // 15 + 60
		{"gpt-4o", 1_000_000, 1_000_000, 1250},        // 250 + 1000
		{"claude-3-5-sonnet", 1_000_000, 1_000_000, 1800},
		// Unknown models return 0 cents — gateway behavior. We don't
		// fabricate prices for models we don't recognize; the cost
		// rate table lives in internal/llmpipeline and is shared
		// with gateway, so adding a model registers it in both
		// surfaces simultaneously.
		{"unknown-model", 1_000_000, 1_000_000, 0},
		{"gpt-4o-mini", 0, 0, 0},
	}
	for _, c := range cases {
		got := estimateCostCents("", c.model, c.in, c.out)
		if got != c.want {
			t.Errorf("estimateCostCents(%q, %d, %d) = %d, want %d",
				c.model, c.in, c.out, got, c.want)
		}
	}
}
