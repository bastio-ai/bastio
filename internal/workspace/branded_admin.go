package workspace

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// =============================================================================
// Slug + domain admin endpoints (dashboard-facing, behind the same auth
// the rest of /v1/workspace uses).
// =============================================================================

// SetSlugRequest is the body for PUT /v1/workspace/settings/slug.
type SetSlugRequest struct {
	Slug string `json:"slug"`
}

func (h *Handler) setSlug(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	if _, err := h.store.EnsureSettings(r.Context(), cid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var body SetSlugRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := h.store.SetSlug(r.Context(), cid, body.Slug); err != nil {
		if errors.Is(err, ErrSlugTaken) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := h.store.GetSettings(r.Context(), cid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit(r, "slug.set",
		AuditTarget{Type: "slug", ID: body.Slug, Label: body.Slug}, nil)
	writeJSON(w, http.StatusOK, st)
}

func (h *Handler) listDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListDomains(r.Context(), customerIDFromCtx(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": rows})
}

type CreateDomainRequest struct {
	Domain string `json:"domain"`
}

func (h *Handler) createDomain(w http.ResponseWriter, r *http.Request) {
	var body CreateDomainRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	d, err := h.store.CreateDomain(r.Context(), customerIDFromCtx(r.Context()), body.Domain)
	if err != nil {
		if errors.Is(err, ErrDomainTaken) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r, "domain.created",
		AuditTarget{Type: "domain", ID: d.ID.String(), Label: d.Domain}, nil)
	writeJSON(w, http.StatusCreated, d)
}

func (h *Handler) verifyDomain(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	d, err := h.store.VerifyDomain(r.Context(), customerIDFromCtx(r.Context()), id, h.txtResolver)
	if err != nil {
		// VerifyDomain has already persisted last_check_error; surface
		// the human-readable message but return the latest row state so
		// the dashboard can refresh without a separate GET.
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		// 422 is the right shape for "everything is correct except DNS
		// hasn't propagated yet" — the request itself is well-formed.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.audit(r, "domain.verified",
		AuditTarget{Type: "domain", ID: d.ID.String(), Label: d.Domain}, nil)
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) deleteDomain(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteDomain(r.Context(), customerIDFromCtx(r.Context()), id); err != nil {
		notFoundOr500(w, err)
		return
	}
	h.audit(r, "domain.deleted",
		AuditTarget{Type: "domain", ID: id.String(), Label: ""}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Host intercept middleware — pulls custom-domain requests off the
// standard routing so the branded chat is served on the customer's
// own hostname.
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
