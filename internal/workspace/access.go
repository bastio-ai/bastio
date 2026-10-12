package workspace

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type AccessGroup struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	UserIDs []string  `json:"user_ids"`
}
type ResourceAccess struct {
	Type     string      `json:"type"`
	ID       uuid.UUID   `json:"id"`
	GroupIDs []uuid.UUID `json:"group_ids"`
}
type AccessResponse struct {
	Groups    []AccessGroup    `json:"groups"`
	Resources []ResourceAccess `json:"resources"`
}

// An unassigned resource is shared. Any assignment restricts it to group
// members. Owners/admins manage every resource; viewers don't bypass groups.
func accessSQL(ctx context.Context, args *[]any, kind, idExpr string) string {
	if RoleFromCtx(ctx).AtLeast(RoleAdmin) {
		return ""
	}
	*args = append(*args, userIDFromCtx(ctx))
	n := intToStr(len(*args))
	return ` AND (NOT EXISTS (SELECT 1 FROM workspace_resource_access a WHERE a.customer_id=$1 AND a.resource_type='` + kind + `' AND a.resource_id=` + idExpr + `) OR EXISTS (SELECT 1 FROM workspace_resource_access a JOIN workspace_access_group_members gm ON gm.customer_id=a.customer_id AND gm.group_id=a.group_id WHERE a.customer_id=$1 AND a.resource_type='` + kind + `' AND a.resource_id=` + idExpr + ` AND gm.user_id=$` + n + `))`
}
func (s *Store) canAccess(ctx context.Context, cid, id uuid.UUID, kind string) (bool, error) {
	args := []any{cid, id}
	q := `SELECT EXISTS(SELECT 1 FROM `
	if kind == "assistant" {
		q += `workspace_assistants`
	} else {
		q += `workspace_knowledge_sources`
	}
	q += ` r WHERE r.customer_id=$1 AND r.id=$2 AND r.archived_at IS NULL` + accessSQL(ctx, &args, kind, "r.id") + `)`
	var ok bool
	err := s.pool.QueryRow(ctx, q, args...).Scan(&ok)
	return ok, err
}
func (h *Handler) getAccess(w http.ResponseWriter, r *http.Request) {
	cid := customerIDFromCtx(r.Context())
	out := AccessResponse{Groups: []AccessGroup{}, Resources: []ResourceAccess{}}
	rows, err := h.store.pool.Query(r.Context(), `SELECT g.id,g.name,ARRAY(SELECT user_id FROM workspace_access_group_members WHERE customer_id=$1 AND group_id=g.id ORDER BY user_id) FROM workspace_access_groups g WHERE g.customer_id=$1 ORDER BY g.name`, cid)
	if err != nil {
		writeError(w, 500, "groups unavailable")
		return
	}
	for rows.Next() {
		var g AccessGroup
		if err = rows.Scan(&g.ID, &g.Name, &g.UserIDs); err != nil {
			break
		}
		out.Groups = append(out.Groups, g)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		writeError(w, 500, "groups unavailable")
		return
	}
	rows, err = h.store.pool.Query(r.Context(), `SELECT resource_type,resource_id,array_agg(group_id ORDER BY group_id) FROM workspace_resource_access WHERE customer_id=$1 GROUP BY resource_type,resource_id`, cid)
	if err != nil {
		writeError(w, 500, "permissions unavailable")
		return
	}
	for rows.Next() {
		var a ResourceAccess
		if err = rows.Scan(&a.Type, &a.ID, &a.GroupIDs); err != nil {
			break
		}
		out.Resources = append(out.Resources, a)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		writeError(w, 500, "permissions unavailable")
		return
	}
	writeJSON(w, 200, out)
}
func (h *Handler) putAccessGroup(w http.ResponseWriter, r *http.Request) {
	var b AccessGroup
	if decodeJSON(r, &b) != nil || strings.TrimSpace(b.Name) == "" || len(b.Name) > 80 || len(b.UserIDs) > 1000 {
		writeError(w, 400, "group name and up to 1000 members required")
		return
	}
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	cid := customerIDFromCtx(r.Context())
	tx, err := h.store.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "groups unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `INSERT INTO workspace_access_groups(id,customer_id,name) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name WHERE workspace_access_groups.customer_id=$2`, b.ID, cid, strings.TrimSpace(b.Name))
	if err != nil {
		writeError(w, 400, "group name must be unique")
		return
	}
	var owned bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM workspace_access_groups WHERE customer_id=$1 AND id=$2)`, cid, b.ID).Scan(&owned); err != nil || !owned {
		writeError(w, 404, "group not found")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM workspace_access_group_members WHERE customer_id=$1 AND group_id=$2`, cid, b.ID); err != nil {
		writeError(w, 500, "groups unavailable")
		return
	}
	for _, uid := range b.UserIDs {
		tag, e := tx.Exec(r.Context(), `INSERT INTO workspace_access_group_members(customer_id,group_id,user_id) SELECT $1,$2,user_id FROM workspace_members WHERE customer_id=$1 AND user_id=$3 ON CONFLICT DO NOTHING`, cid, b.ID, uid)
		if e != nil || tag.RowsAffected() == 0 {
			writeError(w, 400, "every selected user must be a workspace member, with no duplicates")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "groups unavailable")
		return
	}
	h.audit(r, "access.group_updated", AuditTarget{Type: "group", ID: b.ID.String()}, nil)
	writeJSON(w, 200, b)
}
func (h *Handler) putResourceAccess(w http.ResponseWriter, r *http.Request) {
	var b ResourceAccess
	if decodeJSON(r, &b) != nil || (b.Type != "knowledge" && b.Type != "assistant") || b.ID == uuid.Nil || len(b.GroupIDs) > 100 {
		writeError(w, 400, "invalid resource permissions")
		return
	}
	cid := customerIDFromCtx(r.Context())
	ok, err := h.store.canAccess(r.Context(), cid, b.ID, b.Type)
	if err != nil || !ok {
		writeError(w, 404, "resource not found")
		return
	}
	tx, err := h.store.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "permissions unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize replacement of bindings on the resource, including the first
	// assignment when there are no binding rows to lock yet.
	table := "workspace_knowledge_sources"
	if b.Type == "assistant" {
		table = "workspace_assistants"
	}
	var lockedID uuid.UUID
	if err = tx.QueryRow(r.Context(), `SELECT id FROM `+table+` WHERE customer_id=$1 AND id=$2 AND archived_at IS NULL FOR UPDATE`, cid, b.ID).Scan(&lockedID); err != nil {
		writeError(w, 404, "resource not found")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM workspace_resource_access WHERE customer_id=$1 AND resource_type=$2 AND resource_id=$3`, cid, b.Type, b.ID); err != nil {
		writeError(w, 500, "permissions unavailable")
		return
	}
	for _, gid := range b.GroupIDs {
		tag, e := tx.Exec(r.Context(), `INSERT INTO workspace_resource_access(customer_id,resource_type,resource_id,group_id) SELECT $1,$2,$3,id FROM workspace_access_groups WHERE customer_id=$1 AND id=$4 ON CONFLICT DO NOTHING`, cid, b.Type, b.ID, gid)
		if e != nil || tag.RowsAffected() == 0 {
			writeError(w, 400, "group not found or duplicated")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "permissions unavailable")
		return
	}
	h.audit(r, "access.resource_updated", AuditTarget{Type: b.Type, ID: b.ID.String()}, nil)
	writeJSON(w, 200, b)
}
