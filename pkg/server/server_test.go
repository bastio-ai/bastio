package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestOptions_Mount(t *testing.T) {
	var o options
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	WithMount("/auth", h)(&o)
	WithMount("/billing", h)(&o)

	if got := len(o.mounts); got != 2 {
		t.Fatalf("mounts: want 2, got %d", got)
	}
	if o.mounts[0].Prefix != "/auth" || o.mounts[1].Prefix != "/billing" {
		t.Fatalf("unexpected mount order: %+v", o.mounts)
	}
}

func TestOptions_Middleware(t *testing.T) {
	var o options
	mw := func(next http.Handler) http.Handler { return next }
	WithMiddleware(mw, mw)(&o)
	WithMiddleware(mw)(&o)
	if got := len(o.rootMiddleware); got != 3 {
		t.Fatalf("rootMiddleware: want 3, got %d", got)
	}
}

func TestOptions_DashboardMiddleware(t *testing.T) {
	var o options
	mw := func(next http.Handler) http.Handler { return next }
	WithDashboardMiddleware(mw)(&o)
	if got := len(o.dashboardMiddleware); got != 1 {
		t.Fatalf("dashboardMiddleware: want 1, got %d", got)
	}
}

func TestOptions_APIExtension(t *testing.T) {
	var o options
	WithAPIExtension(func(r chi.Router) {})(&o)
	WithAPIExtension(func(r chi.Router) {})(&o)
	if got := len(o.apiExtenders); got != 2 {
		t.Fatalf("apiExtenders: want 2, got %d", got)
	}
}

func TestOptions_Timeouts(t *testing.T) {
	var o options
	WithTimeouts(1*time.Second, 2*time.Second, 3*time.Second)(&o)
	if o.readTimeout != time.Second || o.writeTimeout != 2*time.Second || o.idleTimeout != 3*time.Second {
		t.Fatalf("timeouts not applied: %+v", o)
	}
	WithShutdownTimeout(5 * time.Second)(&o)
	if o.shutdownTimeout != 5*time.Second {
		t.Fatalf("shutdown timeout not applied: %v", o.shutdownTimeout)
	}
}

func TestOptions_DashboardAndOpenAPI(t *testing.T) {
	var o options
	WithOpenAPISpec([]byte("spec"))(&o)
	if string(o.openapiSpec) != "spec" {
		t.Fatalf("openapiSpec not set")
	}
	if o.dashboardFS != nil {
		t.Fatalf("dashboardFS should start nil")
	}
}

func TestRegisterInfraRoutes_Docs(t *testing.T) {
	docsFS := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>docs</title>")},
		"scalar.standalone.js": {Data: []byte("console.log('scalar')")},
	}
	s := &Server{opts: options{docsFS: docsFS}}
	r := chi.NewRouter()
	s.registerInfraRoutes(r)

	srv := httptest.NewServer(r)
	defer srv.Close()

	cases := []struct {
		path        string
		wantCTPart  string
		wantBodyHas string
	}{
		{"/docs", "text/html", "<!doctype html>"},
		{"/docs/scalar.standalone.js", "javascript", "scalar"},
	}
	for _, tc := range cases {
		resp, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: status %d, body %q", tc.path, resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, tc.wantCTPart) {
			t.Fatalf("GET %s: content-type %q does not contain %q", tc.path, ct, tc.wantCTPart)
		}
		if !strings.Contains(string(body), tc.wantBodyHas) {
			t.Fatalf("GET %s: body missing %q: %q", tc.path, tc.wantBodyHas, body)
		}
	}
}

func TestRegisterInfraRoutes_NoDocs(t *testing.T) {
	s := &Server{opts: options{}}
	r := chi.NewRouter()
	s.registerInfraRoutes(r)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/docs")
	if err != nil {
		t.Fatalf("GET /docs: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("GET /docs without WithDocs: want 404, got %d", resp.StatusCode)
	}
}
