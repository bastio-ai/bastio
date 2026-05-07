package workspace

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

// TestOpenAPISpecMatchesWorkspaceRoutes walks the workspace chi router and
// asserts every (method, path) appears in cmd/server/openapi.yaml. Mirrors
// the governance sync test — catches drift the moment a handler is added
// or renamed without updating the spec, before the dashboard's typed client
// silently goes stale.
func TestOpenAPISpecMatchesWorkspaceRoutes(t *testing.T) {
	specBytes, err := os.ReadFile(workspaceOpenAPISpecPath(t))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	spec := string(specBytes)

	// Zero-value Handler is safe — Routes() only references methods by
	// value and never invokes them during registration.
	h := &Handler{}
	router := h.Routes()

	missing := []string{}
	err = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		full := "/v1/workspace" + route
		full = strings.TrimSuffix(full, "/")
		if !workspaceSpecContains(spec, full, method) {
			missing = append(missing, fmt.Sprintf("%s %s", method, full))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(missing) > 0 {
		t.Fatalf("openapi.yaml missing route definitions:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

func workspaceSpecContains(spec, path, method string) bool {
	method = strings.ToLower(method)
	header := "  " + path + ":"
	idx := strings.Index(spec, header+"\n")
	if idx < 0 {
		return false
	}
	rest := spec[idx+len(header)+1:]

	// Walk forward until we hit either the requested method or the next
	// path-level header (two-space indent + leading slash).
	for line := range strings.SplitSeq(rest, "\n") {
		// Next path block — done with this one.
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":") {
			return false
		}
		// End of paths section.
		if strings.HasPrefix(line, "components:") || strings.HasPrefix(line, "tags:") {
			return false
		}
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, method+":") {
			return true
		}
	}
	return false
}

// workspaceOpenAPISpecPath finds cmd/server/openapi.yaml relative to this
// test file's location — works regardless of where `go test` runs from.
func workspaceOpenAPISpecPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/workspace/ → repo root → cmd/server/openapi.yaml
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "cmd", "server", "openapi.yaml")
}
