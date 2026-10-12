package workspace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AccessGroup struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	UserIDs []string  `json:"user_ids"`
}
type ResourceSharing struct {
	Restricted bool        `json:"restricted"`
	GroupIDs   []uuid.UUID `json:"group_ids"`
	UserIDs    []string    `json:"user_ids"`
}

var errInvalidSharing = errors.New("invalid sharing recipients")

type ResourceAccess struct {
	Type       string      `json:"type"`
	ID         uuid.UUID   `json:"id"`
	GroupIDs   []uuid.UUID `json:"group_ids"`
	UserIDs    []string    `json:"user_ids"`
	Restricted *bool       `json:"restricted,omitempty"`
}
type AccessResponse struct {
	Groups    []AccessGroup    `json:"groups"`
	Resources []ResourceAccess `json:"resources"`
}

// Explicit restrictions survive an empty recipient list. Group and individual
// grants are additive. Owners/admins retain management access to shared resources.
func accessSQL(ctx context.Context, args *[]any, kind, idExpr string) string {
	if RoleFromCtx(ctx).AtLeast(RoleAdmin) {
		return ""
	}
	*args = append(*args, userIDFromCtx(ctx))
	n := intToStr(len(*args))
	resource := "customer_id=$1 AND resource_type='" + kind + "' AND resource_id=" + idExpr
	return ` AND ((NOT EXISTS (SELECT 1 FROM workspace_resource_sharing WHERE ` + resource + ` AND restricted)
 AND NOT EXISTS (SELECT 1 FROM workspace_resource_access WHERE ` + resource + `)
 AND NOT EXISTS (SELECT 1 FROM workspace_resource_users WHERE ` + resource + `))
 OR EXISTS (SELECT 1 FROM workspace_resource_access a JOIN workspace_access_group_members gm ON gm.customer_id=a.customer_id AND gm.group_id=a.group_id WHERE a.customer_id=$1 AND a.resource_type='` + kind + `' AND a.resource_id=` + idExpr + ` AND gm.user_id=$` + n + `)
 OR EXISTS (SELECT 1 FROM workspace_resource_users WHERE ` + resource + ` AND user_id=$` + n + `))`
}
func resourceTable(kind string) (string, bool) {
	switch kind {
	case "assistant":
		return "workspace_assistants", true
	case "knowledge":
		return "workspace_knowledge_sources", true
	case "mcp":
		return "workspace_mcp_connections", true
	}
	return "", false
}
func (s *Store) canAccess(ctx context.Context, cid, id uuid.UUID, kind string) (bool, error) {
	table, valid := resourceTable(kind)
	if !valid {
		return false, errInvalidSharing
	}
	args := []any{cid, id}
	active := "r.archived_at IS NULL"
	if kind == "mcp" {
		active = "r.revoked_at IS NULL AND r.scope='workspace'"
	}
	q := `SELECT EXISTS(SELECT 1 FROM ` + table + ` r WHERE r.customer_id=$1 AND r.id=$2 AND ` + active + accessSQL(ctx, &args, kind, "r.id") + `)`
	var ok bool
	err := s.pool.QueryRow(ctx, q, args...).Scan(&ok)
	return ok, err
}
func (s *Store) resourceSharing(ctx context.Context, cid, id uuid.UUID, kind string) (*ResourceSharing, error) {
	var out ResourceSharing
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT restricted FROM workspace_resource_sharing WHERE customer_id=$1 AND resource_type=$2 AND resource_id=$3),FALSE)
 OR EXISTS(SELECT 1 FROM workspace_resource_access WHERE customer_id=$1 AND resource_type=$2 AND resource_id=$3)
 OR EXISTS(SELECT 1 FROM workspace_resource_users WHERE customer_id=$1 AND resource_type=$2 AND resource_id=$3),
 ARRAY(SELECT group_id FROM workspace_resource_access WHERE customer_id=$1 AND resource_type=$2 AND resource_id=$3 ORDER BY group_id),
 ARRAY(SELECT user_id FROM workspace_resource_users WHERE customer_id=$1 AND resource_type=$2 AND resource_id=$3 ORDER BY user_id)`, cid, kind, id).Scan(&out.Restricted, &out.GroupIDs, &out.UserIDs)
	return &out, err
}

// setResourceSharing is always called in the same transaction as resource
// creation/update. Membership locks serialize grants with member removal.
func setResourceSharing(ctx context.Context, tx pgx.Tx, cid, id uuid.UUID, kind string, p ResourceSharing) error {
	if _, ok := resourceTable(kind); !ok || len(p.GroupIDs) > 100 || len(p.UserIDs) > 1000 || !p.Restricted && (len(p.GroupIDs) > 0 || len(p.UserIDs) > 0) {
		return errInvalidSharing
	}
	seenGroups := map[uuid.UUID]bool{}
	seenUsers := map[string]bool{}
	for _, gid := range p.GroupIDs {
		if gid == uuid.Nil || seenGroups[gid] {
			return errInvalidSharing
		}
		seenGroups[gid] = true
		var found uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM workspace_access_groups WHERE customer_id=$1 AND id=$2 FOR SHARE`, cid, gid).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: group unavailable", errInvalidSharing)
			}
			return fmt.Errorf("check sharing group: %w", err)
		}
	}
	for _, uid := range p.UserIDs {
		if uid == "" || seenUsers[uid] {
			return errInvalidSharing
		}
		seenUsers[uid] = true
		var found string
		if err := tx.QueryRow(ctx, `SELECT user_id FROM workspace_members WHERE customer_id=$1 AND user_id=$2 FOR SHARE`, cid, uid).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: member unavailable", errInvalidSharing)
			}
			return fmt.Errorf("check sharing member: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_resource_sharing(customer_id,resource_type,resource_id,restricted) VALUES($1,$2,$3,$4)
 ON CONFLICT(customer_id,resource_type,resource_id) DO UPDATE SET restricted=excluded.restricted WHERE workspace_resource_sharing.customer_id=$1`, cid, kind, id, p.Restricted); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_resource_access WHERE customer_id=$1 AND resource_type=$2 AND resource_id=$3`, cid, kind, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_resource_users WHERE customer_id=$1 AND resource_type=$2 AND resource_id=$3`, cid, kind, id); err != nil {
		return err
	}
	for _, gid := range p.GroupIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_resource_access(customer_id,resource_type,resource_id,group_id) VALUES($1,$2,$3,$4)`, cid, kind, id, gid); err != nil {
			return err
		}
	}
	for _, uid := range p.UserIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_resource_users(customer_id,resource_type,resource_id,user_id) VALUES($1,$2,$3,$4)`, cid, kind, id, uid); err != nil {
			return err
		}
	}
	return nil
}
func sharingError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, errInvalidSharing) {
		writeError(w, 400, "select valid workspace groups and members for restricted sharing")
		return true
	}
	return false
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
	rows, err = h.store.pool.Query(r.Context(), `SELECT p.resource_type,p.resource_id,p.restricted,
 ARRAY(SELECT group_id FROM workspace_resource_access a WHERE a.customer_id=$1 AND a.resource_type=p.resource_type AND a.resource_id=p.resource_id ORDER BY group_id),
 ARRAY(SELECT user_id FROM workspace_resource_users u WHERE u.customer_id=$1 AND u.resource_type=p.resource_type AND u.resource_id=p.resource_id ORDER BY user_id)
 FROM workspace_resource_sharing p WHERE p.customer_id=$1 ORDER BY p.resource_type,p.resource_id`, cid)
	if err != nil {
		writeError(w, 500, "permissions unavailable")
		return
	}
	for rows.Next() {
		var a ResourceAccess
		if err = rows.Scan(&a.Type, &a.ID, &a.Restricted, &a.GroupIDs, &a.UserIDs); err != nil {
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
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var b ResourceAccess
	if decodeMCP(r, &b) != nil || b.ID == uuid.Nil {
		writeError(w, 400, "invalid resource permissions")
		return
	}
	table, valid := resourceTable(b.Type)
	if !valid {
		writeError(w, 400, "invalid resource type")
		return
	}
	cid := customerIDFromCtx(r.Context())
	tx, err := h.store.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "permissions unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	active := "archived_at IS NULL"
	if b.Type == "mcp" {
		active = "revoked_at IS NULL AND scope='workspace'"
	}
	var lockedID uuid.UUID
	if err = tx.QueryRow(r.Context(), `SELECT id FROM `+table+` WHERE customer_id=$1 AND id=$2 AND `+active+` FOR UPDATE`, cid, b.ID).Scan(&lockedID); err != nil {
		writeError(w, 404, "resource not found")
		return
	}
	restricted := len(b.GroupIDs) > 0 || len(b.UserIDs) > 0
	if b.Restricted != nil {
		restricted = *b.Restricted
	}
	if err = setResourceSharing(r.Context(), tx, cid, b.ID, b.Type, ResourceSharing{restricted, b.GroupIDs, b.UserIDs}); err != nil {
		if !sharingError(w, err) {
			writeError(w, 500, "permissions unavailable")
		}
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "permissions unavailable")
		return
	}
	b.Restricted = &restricted
	if b.GroupIDs == nil {
		b.GroupIDs = []uuid.UUID{}
	}
	if b.UserIDs == nil {
		b.UserIDs = []string{}
	}
	h.audit(r, "access.resource_updated", AuditTarget{Type: b.Type, ID: b.ID.String()}, nil)
	writeJSON(w, 200, b)
}
