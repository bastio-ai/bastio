package iplist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// listedManager returns a refreshed manager with 192.0.2.0/24 on
// firehol_level1.
func listedManager(t *testing.T) *Manager {
	t.Helper()
	fh := &fakeProvider{name: "firehol_level1"}
	fh.set(mustPrefixes(t, "192.0.2.0/24"), nil)
	m := NewManager([]Provider{fh})
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return m
}

// runMiddleware sends one request through the middleware and reports
// whether next ran, what verdict it saw, and the recorder.
func runMiddleware(t *testing.T, m *Manager, cfg MiddlewareConfig, remoteAddr, path string) (nextRan bool, verdict Verdict, verdictOK bool, rec *httptest.ResponseRecorder) {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextRan = true
		verdict, verdictOK = VerdictFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := Middleware(m, cfg)(next)
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.RemoteAddr = remoteAddr
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return nextRan, verdict, verdictOK, rec
}

func TestMiddlewareAnnotateListedIP(t *testing.T) {
	m := listedManager(t)
	nextRan, verdict, ok, rec := runMiddleware(t, m, MiddlewareConfig{}, "192.0.2.10:51234", "/v1/chat/completions")
	if !nextRan {
		t.Fatal("next not called in annotate mode")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !ok || !verdict.Listed || len(verdict.Lists) != 1 || verdict.Lists[0] != "firehol_level1" {
		t.Fatalf("verdict = %+v ok=%v, want listed firehol_level1", verdict, ok)
	}
}

func TestMiddlewareAnnotateUnlistedIP(t *testing.T) {
	m := listedManager(t)
	nextRan, _, ok, rec := runMiddleware(t, m, MiddlewareConfig{}, "8.8.8.8:443", "/v1/chat/completions")
	if !nextRan || rec.Code != http.StatusOK {
		t.Fatalf("nextRan=%v status=%d", nextRan, rec.Code)
	}
	if ok {
		t.Fatal("unlisted ip must not attach a verdict")
	}
}

func TestMiddlewareBlockListedIP(t *testing.T) {
	m := listedManager(t)
	nextRan, _, _, rec := runMiddleware(t, m, MiddlewareConfig{Block: true}, "192.0.2.10:51234", "/v1/chat/completions")
	if nextRan {
		t.Fatal("next called despite block mode")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body struct {
		Error       string   `json:"error"`
		ThreatTypes []string `json:"threat_types"`
		Lists       []string `json:"lists"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (%s)", err, rec.Body.String())
	}
	if body.Error == "" || len(body.ThreatTypes) != 1 || body.ThreatTypes[0] != "ip_reputation" {
		t.Fatalf("body = %+v", body)
	}
	if len(body.Lists) != 1 || body.Lists[0] != "firehol_level1" {
		t.Fatalf("lists = %v", body.Lists)
	}
}

func TestMiddlewareBlockUnlistedIPPasses(t *testing.T) {
	m := listedManager(t)
	nextRan, _, _, rec := runMiddleware(t, m, MiddlewareConfig{Block: true}, "8.8.8.8:443", "/v1/chat/completions")
	if !nextRan || rec.Code != http.StatusOK {
		t.Fatalf("nextRan=%v status=%d", nextRan, rec.Code)
	}
}

func TestMiddlewareNeverBlocksHealthEndpoints(t *testing.T) {
	m := listedManager(t)
	for _, path := range []string{"/health", "/healthz", "/ready", "/readyz", "/metrics"} {
		nextRan, _, _, rec := runMiddleware(t, m, MiddlewareConfig{Block: true}, "192.0.2.10:51234", path)
		if !nextRan || rec.Code != http.StatusOK {
			t.Errorf("%s: nextRan=%v status=%d, want pass-through", path, nextRan, rec.Code)
		}
	}
}

func TestMiddlewareRemoteAddrForms(t *testing.T) {
	m := listedManager(t)
	cases := []struct {
		remoteAddr string
		blocked    bool
	}{
		{"192.0.2.10:51234", true}, // host:port (direct conn)
		{"192.0.2.10", true},       // bare IP (RealIP rewrite)
		{"[2001:db8::1]:443", false},
		{"::ffff:192.0.2.10", true}, // 4-mapped v6 unmaps to listed v4
		{"not-an-ip", false},        // unparseable: fail open
		{"", false},
	}
	for _, tc := range cases {
		_, _, _, rec := runMiddleware(t, m, MiddlewareConfig{Block: true}, tc.remoteAddr, "/v1/chat/completions")
		got := rec.Code == http.StatusForbidden
		if got != tc.blocked {
			t.Errorf("RemoteAddr %q: blocked=%v, want %v", tc.remoteAddr, got, tc.blocked)
		}
	}
}

func TestMiddlewareNilManagerPassesThrough(t *testing.T) {
	nextRan := false
	h := Middleware(nil, MiddlewareConfig{Block: true})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextRan = true
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.RemoteAddr = "192.0.2.10:1"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !nextRan {
		t.Fatal("nil manager must pass through")
	}
}
