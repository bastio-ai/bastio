package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// SCIMHandler exposes /scim/v2/* per RFC 7644. Mounted on the OSS server
// outside the dashboard middleware group — bearer auth is the SCIM spec's
// way and lives entirely in this handler's middleware.
type SCIMHandler struct {
	store *SCIMStore
}

func NewSCIMHandler(store *SCIMStore) *SCIMHandler {
	return &SCIMHandler{store: store}
}

// scimContextKey is the context key for the resolved customer id.
type scimContextKey struct{}

func customerFromCtx(ctx context.Context) (uuid.UUID, bool) {
	v := ctx.Value(scimContextKey{})
	if v == nil {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}

// Routes returns the SCIM 2.0 router. Bearer auth runs as middleware on
// every authenticated route; the discovery endpoints stay open per spec.
func (h *SCIMHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/ServiceProviderConfig", h.serviceProviderConfig)
	r.Get("/ResourceTypes", h.resourceTypes)

	r.Group(func(r chi.Router) {
		r.Use(h.bearerAuth)
		r.Get("/Users", h.listUsers)
		r.Post("/Users", h.createUser)
		r.Get("/Users/{id}", h.getUser)
		r.Patch("/Users/{id}", h.patchUser)
		r.Put("/Users/{id}", h.patchUser) // PUT treated as full replace via PATCH semantics
		r.Delete("/Users/{id}", h.deleteUser)

		r.Get("/Groups", h.listGroups)
		r.Post("/Groups", h.createGroup)
		r.Get("/Groups/{id}", h.getGroup)
		r.Patch("/Groups/{id}", h.patchGroup)
		r.Put("/Groups/{id}", h.patchGroup)
		r.Delete("/Groups/{id}", h.deleteGroup)
	})
	return r
}

// =====================================================================
// Auth middleware
// =====================================================================

func (h *SCIMHandler) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(hdr, "Bearer ") {
			scimError(w, http.StatusUnauthorized, "Bearer token required")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
		if token == "" {
			scimError(w, http.StatusUnauthorized, "empty bearer")
			return
		}
		customerID, err := h.store.LookupCustomerByToken(r.Context(), token)
		if err != nil {
			scimError(w, http.StatusUnauthorized, "invalid bearer")
			return
		}
		ctx := context.WithValue(r.Context(), scimContextKey{}, customerID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// =====================================================================
// Discovery
// =====================================================================

func (h *SCIMHandler) serviceProviderConfig(w http.ResponseWriter, _ *http.Request) {
	scimJSON(w, http.StatusOK, map[string]any{
		"schemas":               []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri":      "https://bastio.com/docs/scim",
		"patch":                 map[string]bool{"supported": true},
		"bulk":                  map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":                map[string]any{"supported": true, "maxResults": 200},
		"changePassword":        map[string]bool{"supported": false},
		"sort":                  map[string]bool{"supported": false},
		"etag":                  map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]string{{
			"name":        "OAuth Bearer Token",
			"description": "Per-customer bearer issued from the bastio dashboard",
			"specUri":     "https://datatracker.ietf.org/doc/html/rfc6750",
			"type":        "oauthbearertoken",
		}},
	})
}

func (h *SCIMHandler) resourceTypes(w http.ResponseWriter, _ *http.Request) {
	scimJSON(w, http.StatusOK, map[string]any{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": 2,
		"Resources": []map[string]any{
			{
				"schemas":  []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
				"id":       "User",
				"name":     "User",
				"endpoint": "/Users",
				"schema":   scimUserSchema,
			},
			{
				"schemas":  []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
				"id":       "Group",
				"name":     "Group",
				"endpoint": "/Groups",
				"schema":   scimGroupSchema,
			},
		},
	})
}

// =====================================================================
// Users
// =====================================================================

func (h *SCIMHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	startIndex, count := paginationParams(r)

	// SCIM filter: `userName eq "..."` is the one IdPs actually use to
	// check existence before POST. Parse it; ignore others.
	if filter := r.URL.Query().Get("filter"); filter != "" {
		if userName := parseUserNameEqFilter(filter); userName != "" {
			u, err := h.store.FindUserByUserName(r.Context(), customerID, userName)
			if err != nil {
				scimError(w, http.StatusInternalServerError, err.Error())
				return
			}
			resources := []any{}
			if u != nil {
				resources = append(resources, u.toSCIM(scimBaseURL(r)))
			}
			scimJSON(w, http.StatusOK, SCIMListResponse{
				Schemas:      []string{scimListSchema},
				TotalResults: len(resources),
				StartIndex:   1,
				ItemsPerPage: len(resources),
				Resources:    resources,
			})
			return
		}
	}

	users, total, err := h.store.ListUsers(r.Context(), customerID, startIndex, count)
	if err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resources := make([]any, 0, len(users))
	base := scimBaseURL(r)
	for i := range users {
		resources = append(resources, users[i].toSCIM(base))
	}
	scimJSON(w, http.StatusOK, SCIMListResponse{
		Schemas:      []string{scimListSchema},
		TotalResults: total,
		StartIndex:   startIndex,
		ItemsPerPage: len(resources),
		Resources:    resources,
	})
}

func (h *SCIMHandler) createUser(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	var body SCIMUser
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		scimError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.UserName == "" {
		scimError(w, http.StatusBadRequest, "userName required")
		return
	}

	primaryEmail := ""
	for _, e := range body.Emails {
		if e.Primary || primaryEmail == "" {
			primaryEmail = e.Value
		}
		if e.Primary {
			break
		}
	}
	given, family := "", ""
	if body.Name != nil {
		given = body.Name.GivenName
		family = body.Name.FamilyName
	}
	u := &GovernanceUser{
		CustomerID:  customerID,
		ExternalID:  body.ExternalID,
		UserName:    body.UserName,
		Email:       primaryEmail,
		DisplayName: body.DisplayName,
		GivenName:   given,
		FamilyName:  family,
		Active:      body.Active,
	}
	// SCIM allows "active" to be omitted; default to true.
	if !u.Active {
		u.Active = true
	}

	if err := h.store.CreateUser(r.Context(), u); err != nil {
		// duplicate userName → 409 per SCIM
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			scimError(w, http.StatusConflict, "userName already exists")
			return
		}
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	scimJSON(w, http.StatusCreated, u.toSCIM(scimBaseURL(r)))
}

func (h *SCIMHandler) getUser(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusBadRequest, "invalid id")
		return
	}
	u, err := h.store.GetUser(r.Context(), customerID, id)
	if err != nil {
		if errors.Is(err, ErrSCIMNotFound) {
			scimError(w, http.StatusNotFound, "user not found")
			return
		}
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	scimJSON(w, http.StatusOK, u.toSCIM(scimBaseURL(r)))
}

func (h *SCIMHandler) patchUser(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ops, err := decodeOps(r)
	if err != nil {
		scimError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := h.store.PatchUser(r.Context(), customerID, id, ops)
	if err != nil {
		if errors.Is(err, ErrSCIMNotFound) {
			scimError(w, http.StatusNotFound, "user not found")
			return
		}
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	scimJSON(w, http.StatusOK, u.toSCIM(scimBaseURL(r)))
}

func (h *SCIMHandler) deleteUser(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.SoftDeleteUser(r.Context(), customerID, id); err != nil {
		if errors.Is(err, ErrSCIMNotFound) {
			scimError(w, http.StatusNotFound, "user not found")
			return
		}
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// =====================================================================
// Groups
// =====================================================================

func (h *SCIMHandler) listGroups(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	startIndex, count := paginationParams(r)
	groups, total, err := h.store.ListGroups(r.Context(), customerID, startIndex, count)
	if err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resources := make([]any, 0, len(groups))
	base := scimBaseURL(r)
	for i := range groups {
		resources = append(resources, groups[i].toSCIM(base, nil))
	}
	scimJSON(w, http.StatusOK, SCIMListResponse{
		Schemas:      []string{scimListSchema},
		TotalResults: total,
		StartIndex:   startIndex,
		ItemsPerPage: len(resources),
		Resources:    resources,
	})
}

func (h *SCIMHandler) createGroup(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	var body SCIMGroup
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		scimError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.DisplayName == "" {
		scimError(w, http.StatusBadRequest, "displayName required")
		return
	}
	g := &GovernanceGroup{
		CustomerID:  customerID,
		ExternalID:  body.ExternalID,
		DisplayName: body.DisplayName,
	}
	memberIDs := []uuid.UUID{}
	for _, m := range body.Members {
		if uid, err := uuid.Parse(m.Value); err == nil {
			memberIDs = append(memberIDs, uid)
		}
	}
	if err := h.store.CreateGroup(r.Context(), g, memberIDs); err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members := make([]SCIMMemberRef, 0, len(memberIDs))
	for _, uid := range memberIDs {
		members = append(members, SCIMMemberRef{Value: uid.String(), Type: "User"})
	}
	scimJSON(w, http.StatusCreated, g.toSCIM(scimBaseURL(r), members))
}

func (h *SCIMHandler) getGroup(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusBadRequest, "invalid id")
		return
	}
	g, members, err := h.store.GetGroup(r.Context(), customerID, id)
	if err != nil {
		if errors.Is(err, ErrSCIMNotFound) {
			scimError(w, http.StatusNotFound, "group not found")
			return
		}
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	memberRefs := make([]SCIMMemberRef, 0, len(members))
	for _, uid := range members {
		memberRefs = append(memberRefs, SCIMMemberRef{Value: uid.String(), Type: "User"})
	}
	scimJSON(w, http.StatusOK, g.toSCIM(scimBaseURL(r), memberRefs))
}

func (h *SCIMHandler) patchGroup(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ops, err := decodeOps(r)
	if err != nil {
		scimError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.PatchGroup(r.Context(), customerID, id, ops); err != nil {
		if errors.Is(err, ErrSCIMNotFound) {
			scimError(w, http.StatusNotFound, "group not found")
			return
		}
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	g, members, err := h.store.GetGroup(r.Context(), customerID, id)
	if err != nil {
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	memberRefs := make([]SCIMMemberRef, 0, len(members))
	for _, uid := range members {
		memberRefs = append(memberRefs, SCIMMemberRef{Value: uid.String(), Type: "User"})
	}
	scimJSON(w, http.StatusOK, g.toSCIM(scimBaseURL(r), memberRefs))
}

func (h *SCIMHandler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerFromCtx(r.Context())
	if !ok {
		scimError(w, http.StatusUnauthorized, "no tenant")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.SoftDeleteGroup(r.Context(), customerID, id); err != nil {
		if errors.Is(err, ErrSCIMNotFound) {
			scimError(w, http.StatusNotFound, "group not found")
			return
		}
		scimError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// =====================================================================
// helpers
// =====================================================================

func decodeOps(r *http.Request) ([]SCIMPatchEntry, error) {
	var body SCIMPatchOp
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid patch json: %w", err)
	}
	return body.Operations, nil
}

func paginationParams(r *http.Request) (int, int) {
	startIndex, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if startIndex < 1 {
		startIndex = 1
	}
	if count <= 0 {
		count = 100
	}
	return startIndex, count
}

// parseUserNameEqFilter is a minimal SCIM filter parser. It handles the
// ONE form IdPs use in the wild: `userName eq "value"`. Everything else
// is ignored; callers fall through to a list query.
func parseUserNameEqFilter(filter string) string {
	f := strings.TrimSpace(filter)
	lower := strings.ToLower(f)
	if !strings.HasPrefix(lower, "username eq ") {
		return ""
	}
	rest := strings.TrimSpace(f[len("userName eq "):])
	rest = strings.Trim(rest, `"'`)
	// URL-decode just in case
	if decoded, err := url.QueryUnescape(rest); err == nil {
		return decoded
	}
	return rest
}

func scimBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host + "/scim/v2"
}

func scimJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func scimError(w http.ResponseWriter, status int, detail string) {
	scimJSON(w, status, SCIMError{
		Schemas: []string{scimErrorSchema},
		Detail:  detail,
		Status:  strconv.Itoa(status),
	})
}
