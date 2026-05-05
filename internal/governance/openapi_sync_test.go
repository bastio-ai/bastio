package governance

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestOpenAPISpecMatchesGovernanceRoutes walks the live governance chi router
// and asserts every (method, path) combination appears in cmd/server/openapi.yaml.
// Catches drift when a handler is added/renamed without updating the spec —
// otherwise the typed client at dashboard/src/api/schema.ts silently goes stale.
//
// This intentionally lives in the governance package so it can construct a
// zero-value *Handler without panicking on nil store deps (route registration
// only refers to handler methods by value, not by call).
func TestOpenAPISpecMatchesGovernanceRoutes(t *testing.T) {
	specPath := openAPISpecPath(t)
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	spec := string(specBytes)

	h := &Handler{}

	// Each Routes() function returns a sub-router mounted on a path prefix.
	// Walk both and prefix the discovered routes with the mount point used
	// in pkg/server/server.go (`r.Mount("/governance", h.Routes())` etc.).
	cases := []struct {
		mount  string
		router chi.Router
	}{
		{"/v1/governance", h.Routes()},
		{"/v1/governance/dashboard", h.DashboardRoutes()},
	}

	missing := []string{}
	for _, c := range cases {
		err := chi.Walk(c.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			full := c.mount + route
			// Chi normalizes trailing slash; OpenAPI does not. Strip "/" suffixes
			// except for the root path itself.
			full = strings.TrimSuffix(full, "/")
			if !specContains(spec, full, method) {
				missing = append(missing, fmt.Sprintf("%s %s", method, full))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", c.mount, err)
		}
	}

	if len(missing) > 0 {
		t.Fatalf("openapi.yaml missing route definitions:\n  %s", strings.Join(missing, "\n  "))
	}
}

// specContains is true when the spec has both a path block matching `path`
// and the appropriate HTTP method declared inside that block.
//
// OpenAPI path-block headers are 2-space indented (`  /v1/governance/events:`),
// methods are 4-space indented (`    post:`).
func specContains(spec, path, method string) bool {
	method = strings.ToLower(method)
	header := "  " + path + ":"
	idx := strings.Index(spec, header+"\n")
	if idx < 0 {
		return false
	}

	// Slice from after the header to the next path-level header (or EOF).
	rest := spec[idx+len(header)+1:]
	end := nextPathHeader(rest)
	block := rest
	if end >= 0 {
		block = rest[:end]
	}

	return strings.Contains(block, "    "+method+":\n") ||
		strings.Contains(block, "    "+method+":\r\n")
}

// nextPathHeader returns the offset of the next 2-space-indented YAML key
// (the next path block header) so we know where the current block ends.
// Returns -1 if not found.
func nextPathHeader(s string) int {
	lines := strings.Split(s, "\n")
	offset := 0
	for i, ln := range lines {
		if i == 0 {
			offset += len(ln) + 1
			continue
		}
		// 2-space indent + `/` start = next path; or `components:` at column 0.
		if strings.HasPrefix(ln, "  /") || strings.HasPrefix(ln, "components:") {
			return offset
		}
		offset += len(ln) + 1
	}
	return -1
}

// openAPISpecPath returns the absolute path to cmd/server/openapi.yaml,
// resolved relative to this test file's location. Avoids depending on the
// working directory at test time.
func openAPISpecPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file = .../bastio/internal/governance/openapi_sync_test.go
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(repoRoot, "cmd", "server", "openapi.yaml")
}
