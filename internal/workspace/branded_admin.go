package workspace

import (
	"context"
	"net/http"
	"strings"
)

// =============================================================================
// Host intercept middleware — pulls custom-domain requests off the
// standard routing so the branded chat is served on the customer's
// own hostname.
//
// The slug + custom-domain ADMIN endpoints (PUT /settings/slug,
// /domains CRUD with DNS verification) live in
// bastio-cloud/internal/workspace — branded multi-tenant routing is a
// cloud-only concept (OSS is single-tenant, single-host). This file
// keeps just the host-routing middleware in OSS so server.go's call
// site at the platform router is unaffected; on a fresh OSS install
// where workspace_domains doesn't exist, CustomerByDomain errors and
// the middleware falls through cleanly (line 27-29 below).
// =============================================================================

// HostInterceptMiddleware wraps the OSS server's root router. When the
// request's Host header is *not* one of the platform hosts AND it
// matches a verified workspace_domains row, the middleware dispatches
// to the branded HostRoutes handler. All other requests pass through
// unchanged — performance-wise this is a single string compare on
// every request and a single indexed PG lookup only on miss.
//
// platformHosts is the list of canonical hosts the OSS server knows
// itself by (e.g. `bastio.com`, `workspace.bastio.com`, `localhost`).
// Empty list disables the intercept entirely — useful for tests.
func (h *Handler) HostInterceptMiddleware(platformHosts []string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(platformHosts))
	for _, p := range platformHosts {
		allow[normalizeDomain(p)] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := normalizeDomain(r.Host)
			if _, ok := allow[host]; ok {
				next.ServeHTTP(w, r)
				return
			}
			// Only short-paths get the branded treatment — anything
			// looking like a real API call falls through. Keeps the
			// dashboard SPA reachable from any host during local dev.
			if !brandedShortPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			cid, err := h.store.CustomerByDomain(r.Context(), r.Host)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), CustomerIDKey, cid)
			h.HostRoutes().ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// brandedShortPath returns true for paths the branded chat owns on a
// custom domain — the chat root, label PUT, and the message endpoints.
// Any other path falls through to standard routing so a customer's
// custom-domain'd chat can co-exist with the rest of the OSS server.
func brandedShortPath(p string) bool {
	switch p {
	case "/", "/messages", "/messages/stream", "/session/label":
		return true
	}
	// Allow trailing-slash variants too.
	if strings.HasPrefix(p, "/messages") || strings.HasPrefix(p, "/session/") {
		return true
	}
	return false
}
