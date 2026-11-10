package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeKeyCache struct {
	mu      sync.Mutex
	entries map[string]*APIKeyInfo
	getErr  error
	sets    int
}

func (f *fakeKeyCache) getKey(ctx context.Context, keyHash string) (*APIKeyInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("get key apikey:%s: %w", keyHash, err)
	}
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.entries[keyHash]
	return info, ok, nil
}

func (f *fakeKeyCache) setKey(_ context.Context, keyHash string, info *APIKeyInfo) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.entries == nil {
		f.entries = map[string]*APIKeyInfo{}
	}
	f.entries[keyHash] = info
	f.sets++
	return nil
}

func (f *fakeKeyCache) setCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sets
}

type fakeKeyStore struct {
	keys      map[string]*APIKeyInfo
	lookupErr error
	lookups   int
}

func (f *fakeKeyStore) lookupKey(ctx context.Context, keyHash string) (*APIKeyInfo, error) {
	f.lookups++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	info, ok := f.keys[keyHash]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	cp := *info
	return &cp, nil
}

func (f *fakeKeyStore) touchKey(context.Context, uuid.UUID) {}

const testKey = "sk-bastio-0123456789abcdef"

func newTestService(c *fakeKeyCache, s *fakeKeyStore) *Service {
	return &Service{keys: c, store: s}
}

func knownStore() *fakeKeyStore {
	return &fakeKeyStore{keys: map[string]*APIKeyInfo{
		HashAPIKey(testKey): {ID: uuid.New(), CustomerID: uuid.New(), KeyPrefix: testKey[:12]},
	}}
}

func serve(ctx context.Context, t *testing.T, svc *Service) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testKey)
	rec := httptest.NewRecorder()
	svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	return rec
}

func assertUnavailable(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != RetryAfterSeconds {
		t.Fatalf("Retry-After = %q, want %q", got, RetryAfterSeconds)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"type":"service_unavailable"`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestMiddleware_DefiniteMissIs401(t *testing.T) {
	c := &fakeKeyCache{}
	rec := serve(context.Background(), t, newTestService(c, &fakeKeyStore{}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("401 must not carry Retry-After")
	}
	if c.setCount() != 0 {
		t.Fatal("a miss must not be cached")
	}
}

func TestMiddleware_CancelledContextIs503(t *testing.T) {
	c := &fakeKeyCache{}
	s := knownStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := serve(ctx, t, newTestService(c, s))
	assertUnavailable(t, rec)
	if s.lookups != 0 {
		t.Fatalf("db queried %d times with a dead context", s.lookups)
	}
	if c.setCount() != 0 {
		t.Fatal("an unavailable lookup must not be cached")
	}
}

func TestMiddleware_RedisDownDBOK(t *testing.T) {
	c := &fakeKeyCache{getErr: errors.New("dial tcp redis:6379: connection refused")}
	rec := serve(context.Background(), t, newTestService(c, knownStore()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
}

func TestMiddleware_RedisDownDBDownIs503(t *testing.T) {
	c := &fakeKeyCache{getErr: errors.New("dial tcp redis:6379: connection refused")}
	s := knownStore()
	s.lookupErr = errors.New("closed pool")
	rec := serve(context.Background(), t, newTestService(c, s))
	assertUnavailable(t, rec)
	if c.setCount() != 0 {
		t.Fatal("an unavailable lookup must not be cached")
	}
}

func TestAuthenticate_ErrorClassification(t *testing.T) {
	ctx := context.Background()
	_, err := newTestService(&fakeKeyCache{}, &fakeKeyStore{}).Authenticate(ctx, testKey)
	if !errors.Is(err, ErrKeyNotFound) || errors.Is(err, ErrLookupUnavailable) {
		t.Fatalf("miss: err = %v", err)
	}

	s := knownStore()
	s.lookupErr = context.DeadlineExceeded
	_, err = newTestService(&fakeKeyCache{}, s).Authenticate(ctx, testKey)
	if !errors.Is(err, ErrLookupUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("db timeout: err = %v", err)
	}

	c := &fakeKeyCache{}
	info, err := newTestService(c, knownStore()).Authenticate(ctx, testKey)
	if err != nil || info == nil {
		t.Fatalf("hit: info=%v err=%v", info, err)
	}
	if c.setCount() != 1 {
		t.Fatalf("positive result cached %d times, want 1", c.setCount())
	}
}
