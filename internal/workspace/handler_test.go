package workspace

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// TestBillingGate_HookFires verifies the cloud-side billing gate is
// invoked on every request when SetBillingGate has been called. Cloud
// installs the actual subscription-checking middleware in production;
// this test just proves the OSS extension point fires reliably.
func TestBillingGate_HookFires(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil, nil, nil)
	var observedMethods []string
	h.SetBillingGate(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			observedMethods = append(observedMethods, r.Method)
			// Return a sentinel so we don't depend on downstream handlers
			// (the workspace handler 500s on nil store; we don't care).
			w.Header().Set("X-Billing-Gate-Hit", "yes")
			w.WriteHeader(http.StatusTeapot)
		})
	})

	r := h.Routes()
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		req := httptest.NewRequest(method, "/whoami", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Billing-Gate-Hit"); got != "yes" {
			t.Errorf("%s /whoami: gate did not fire (header: %q)", method, got)
		}
	}
	if len(observedMethods) != 4 {
		t.Fatalf("expected 4 observed methods, got %d (%v)", len(observedMethods), observedMethods)
	}
}

// TestBillingGate_NotInstalled confirms OSS deployments stay free of
// the gate when SetBillingGate has not been called — no surprise hop.
func TestBillingGate_NotInstalled(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil, nil, nil)
	r := h.Routes()
	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Billing-Gate-Hit"); got != "" {
		t.Errorf("OSS path leaked a billing-gate header: %q", got)
	}
}
