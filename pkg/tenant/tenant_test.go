package tenant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestFromContext_Missing(t *testing.T) {
	_, err := FromContext(context.Background())
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("want ErrMissing, got %v", err)
	}
}

func TestWithIDRoundTrip(t *testing.T) {
	id := uuid.New()
	ctx := WithID(context.Background(), id)
	got, err := FromContext(ctx)
	if err != nil {
		t.Fatalf("FromContext err: %v", err)
	}
	if got != id {
		t.Fatalf("id: want %v, got %v", id, got)
	}
}

func TestOSSMiddleware(t *testing.T) {
	var seen uuid.UUID
	var err error
	h := OSSMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen, err = FromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if err != nil {
		t.Fatalf("FromContext err: %v", err)
	}
	if seen != DefaultOSSID {
		t.Fatalf("tenant: want DefaultOSSID, got %v", seen)
	}
}

func TestMiddleware_RejectsZeroUUID(t *testing.T) {
	h := Middleware(func(_ *http.Request) (uuid.UUID, bool) {
		return uuid.Nil, true
	})(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next should not run when resolver returns zero UUID")
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", rr.Code)
	}
}
