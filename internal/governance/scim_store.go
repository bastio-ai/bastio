package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SCIMStore handles persistence for SCIM-pushed users + groups. Bearer-token
// auth lookup also lives here so the handler middleware has a single place
// to ask "is this token valid for which customer?"
type SCIMStore struct {
	pool *pgxpool.Pool
}

func NewSCIMStore(pool *pgxpool.Pool) *SCIMStore {
	return &SCIMStore{pool: pool}
}

var ErrSCIMNotFound = errors.New("scim resource not found")

// ============================================================
// Bearer tokens
// ============================================================

// CreateToken issues a fresh SCIM bearer for a customer. Returns the
// plaintext token to the caller exactly once; only the hash is stored.
// One token per customer for v1 (DELETE conflict rotates).
func (s *SCIMStore) CreateToken(ctx context.Context, customerID uuid.UUID, label string, plaintext string) (uuid.UUID, error) {
	hash := sha256Hex(plaintext)
	const q = `
INSERT INTO governance_scim_tokens (customer_id, token_hash, label)
VALUES ($1, $2, $3)
ON CONFLICT (customer_id) DO UPDATE SET
    token_hash = EXCLUDED.token_hash,
    label = EXCLUDED.label,
    revoked_at = NULL,
    last_used_at = NULL
RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, customerID, hash, label).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("create scim token: %w", err)
	}
	return id, nil
}

// LookupCustomerByToken validates a presented bearer and returns the
// customer it belongs to. Constant-time compare via the hash.
func (s *SCIMStore) LookupCustomerByToken(ctx context.Context, plaintext string) (uuid.UUID, error) {
	hash := sha256Hex(plaintext)
	const q = `
SELECT customer_id FROM governance_scim_tokens
WHERE token_hash = $1 AND revoked_at IS NULL
LIMIT 1`
	var customerID uuid.UUID
	err := s.pool.QueryRow(ctx, q, hash).Scan(&customerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrSCIMNotFound
		}
		return uuid.Nil, fmt.Errorf("lookup scim token: %w", err)
	}
	// Touch last_used_at; best-effort.
	_, _ = s.pool.Exec(ctx, `UPDATE governance_scim_tokens SET last_used_at = NOW() WHERE customer_id = $1`, customerID)
	return customerID, nil
}

func (s *SCIMStore) RevokeToken(ctx context.Context, customerID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE governance_scim_tokens SET revoked_at = NOW() WHERE customer_id = $1 AND revoked_at IS NULL`, customerID)
	if err != nil {
		return fmt.Errorf("revoke scim token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSCIMNotFound
	}
	return nil
}

// ============================================================
// Users
// ============================================================

// CreateUser inserts a new user and returns it. userName must be unique
// per customer (active rows only).
func (s *SCIMStore) CreateUser(ctx context.Context, u *GovernanceUser) error {
	if u.UserName == "" {
		return errors.New("userName required")
	}
	const q = `
INSERT INTO governance_users (
    customer_id, external_id, user_name, email, display_name, given_name, family_name, active
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, created_at, updated_at`
	if err := s.pool.QueryRow(ctx, q,
		u.CustomerID, nullable(u.ExternalID), u.UserName, nullable(u.Email),
		u.DisplayName, u.GivenName, u.FamilyName, u.Active,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// GetUser returns a single active user by id within the customer's scope.
func (s *SCIMStore) GetUser(ctx context.Context, customerID, id uuid.UUID) (*GovernanceUser, error) {
	const q = `
SELECT id, customer_id, COALESCE(external_id,''), user_name, COALESCE(email,''),
       display_name, given_name, family_name, active, created_at, updated_at, deleted_at
FROM governance_users
WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL`
	row := s.pool.QueryRow(ctx, q, id, customerID)
	return scanUser(row)
}

// FindUserByUserName implements the SCIM `userName eq "..."` filter.
func (s *SCIMStore) FindUserByUserName(ctx context.Context, customerID uuid.UUID, userName string) (*GovernanceUser, error) {
	const q = `
SELECT id, customer_id, COALESCE(external_id,''), user_name, COALESCE(email,''),
       display_name, given_name, family_name, active, created_at, updated_at, deleted_at
FROM governance_users
WHERE customer_id = $1 AND user_name = $2 AND deleted_at IS NULL
LIMIT 1`
	row := s.pool.QueryRow(ctx, q, customerID, userName)
	u, err := scanUser(row)
	if errors.Is(err, ErrSCIMNotFound) {
		return nil, nil
	}
	return u, err
}

// ListUsers returns all active users for a customer (paged).
func (s *SCIMStore) ListUsers(ctx context.Context, customerID uuid.UUID, startIndex, count int) ([]GovernanceUser, int, error) {
	if count <= 0 || count > 200 {
		count = 100
	}
	if startIndex < 1 {
		startIndex = 1
	}
	const totalQ = `SELECT count(*) FROM governance_users WHERE customer_id = $1 AND deleted_at IS NULL`
	var total int
	if err := s.pool.QueryRow(ctx, totalQ, customerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	const listQ = `
SELECT id, customer_id, COALESCE(external_id,''), user_name, COALESCE(email,''),
       display_name, given_name, family_name, active, created_at, updated_at, deleted_at
FROM governance_users
WHERE customer_id = $1 AND deleted_at IS NULL
ORDER BY user_name
OFFSET $2 LIMIT $3`
	rows, err := s.pool.Query(ctx, listQ, customerID, startIndex-1, count)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	out := make([]GovernanceUser, 0, count)
	for rows.Next() {
		u, scanErr := scanUserFromRows(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		out = append(out, *u)
	}
	return out, total, nil
}

// PatchUser applies a SCIM PATCH op set to a user row. Supports the
// operations IdPs actually push: replace active, replace name, replace
// email, replace displayName, replace externalId.
func (s *SCIMStore) PatchUser(ctx context.Context, customerID, id uuid.UUID, ops []SCIMPatchEntry) (*GovernanceUser, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const lockQ = `
SELECT id, customer_id, COALESCE(external_id,''), user_name, COALESCE(email,''),
       display_name, given_name, family_name, active, created_at, updated_at, deleted_at
FROM governance_users
WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL
FOR UPDATE`
	row := tx.QueryRow(ctx, lockQ, id, customerID)
	u, err := scanUser(row)
	if err != nil {
		return nil, err
	}

	for _, op := range ops {
		op.Op = strings.ToLower(op.Op)
		path := strings.ToLower(op.Path)
		if op.Op != "replace" && op.Op != "add" {
			continue
		}
		switch path {
		case "active":
			if v, ok := op.Value.(bool); ok {
				u.Active = v
			}
		case "displayname":
			if v, ok := op.Value.(string); ok {
				u.DisplayName = v
			}
		case "externalid":
			if v, ok := op.Value.(string); ok {
				u.ExternalID = v
			}
		case "name.givenname":
			if v, ok := op.Value.(string); ok {
				u.GivenName = v
			}
		case "name.familyname":
			if v, ok := op.Value.(string); ok {
				u.FamilyName = v
			}
		case "username":
			if v, ok := op.Value.(string); ok {
				u.UserName = v
			}
		case "":
			// path-less replace: value is a partial user object
			if m, ok := op.Value.(map[string]any); ok {
				applyMapToUser(m, u)
			}
		}
	}

	const updateQ = `
UPDATE governance_users SET
  external_id = $3, user_name = $4, email = $5, display_name = $6,
  given_name = $7, family_name = $8, active = $9, updated_at = NOW()
WHERE id = $1 AND customer_id = $2
RETURNING updated_at`
	if err := tx.QueryRow(ctx, updateQ,
		u.ID, u.CustomerID, nullable(u.ExternalID), u.UserName, nullable(u.Email),
		u.DisplayName, u.GivenName, u.FamilyName, u.Active,
	).Scan(&u.UpdatedAt); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return u, nil
}

func applyMapToUser(m map[string]any, u *GovernanceUser) {
	if v, ok := m["userName"].(string); ok {
		u.UserName = v
	}
	if v, ok := m["displayName"].(string); ok {
		u.DisplayName = v
	}
	if v, ok := m["externalId"].(string); ok {
		u.ExternalID = v
	}
	if v, ok := m["active"].(bool); ok {
		u.Active = v
	}
	if name, ok := m["name"].(map[string]any); ok {
		if v, ok := name["givenName"].(string); ok {
			u.GivenName = v
		}
		if v, ok := name["familyName"].(string); ok {
			u.FamilyName = v
		}
	}
	if emails, ok := m["emails"].([]any); ok && len(emails) > 0 {
		if e, ok := emails[0].(map[string]any); ok {
			if v, ok := e["value"].(string); ok {
				u.Email = v
			}
		}
	}
}

// SoftDeleteUser sets deleted_at; preserves the row for audit (event
// history continues to show the original user_name) and frees the
// (customer, user_name) unique index for re-provisioning.
func (s *SCIMStore) SoftDeleteUser(ctx context.Context, customerID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE governance_users SET deleted_at = NOW(), active = FALSE WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL`,
		id, customerID)
	if err != nil {
		return fmt.Errorf("soft-delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSCIMNotFound
	}
	// Cascade memberships
	_, _ = s.pool.Exec(ctx,
		`DELETE FROM governance_user_group_memberships WHERE customer_id = $1 AND user_id = $2`,
		customerID, id)
	return nil
}

// ============================================================
// Groups
// ============================================================

func (s *SCIMStore) CreateGroup(ctx context.Context, g *GovernanceGroup, memberIDs []uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx,
		`INSERT INTO governance_groups (customer_id, external_id, display_name) VALUES ($1, $2, $3) RETURNING id, created_at, updated_at`,
		g.CustomerID, nullable(g.ExternalID), g.DisplayName,
	).Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return fmt.Errorf("create group: %w", err)
	}
	for _, uid := range memberIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO governance_user_group_memberships (customer_id, user_id, group_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			g.CustomerID, uid, g.ID,
		); err != nil {
			return fmt.Errorf("add membership: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *SCIMStore) GetGroup(ctx context.Context, customerID, id uuid.UUID) (*GovernanceGroup, []uuid.UUID, error) {
	const q = `
SELECT id, customer_id, COALESCE(external_id,''), display_name, created_at, updated_at, deleted_at
FROM governance_groups
WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL`
	row := s.pool.QueryRow(ctx, q, id, customerID)
	g, err := scanGroup(row)
	if err != nil {
		return nil, nil, err
	}

	memberRows, err := s.pool.Query(ctx,
		`SELECT user_id FROM governance_user_group_memberships WHERE customer_id = $1 AND group_id = $2`,
		customerID, id)
	if err != nil {
		return nil, nil, fmt.Errorf("group members: %w", err)
	}
	defer memberRows.Close()
	members := []uuid.UUID{}
	for memberRows.Next() {
		var m uuid.UUID
		if err := memberRows.Scan(&m); err != nil {
			return nil, nil, err
		}
		members = append(members, m)
	}
	return g, members, nil
}

func (s *SCIMStore) ListGroups(ctx context.Context, customerID uuid.UUID, startIndex, count int) ([]GovernanceGroup, int, error) {
	if count <= 0 || count > 200 {
		count = 100
	}
	if startIndex < 1 {
		startIndex = 1
	}
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM governance_groups WHERE customer_id = $1 AND deleted_at IS NULL`,
		customerID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, customer_id, COALESCE(external_id,''), display_name, created_at, updated_at, deleted_at
FROM governance_groups
WHERE customer_id = $1 AND deleted_at IS NULL
ORDER BY display_name
OFFSET $2 LIMIT $3`, customerID, startIndex-1, count)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]GovernanceGroup, 0, count)
	for rows.Next() {
		g, scanErr := scanGroupFromRows(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		out = append(out, *g)
	}
	return out, total, nil
}

// PatchGroup applies SCIM PATCH ops. Critical operations:
//   - replace displayName
//   - add members [{value: user_id}, ...]
//   - remove members (with filter `members[value eq "user_id"]`)
func (s *SCIMStore) PatchGroup(ctx context.Context, customerID, id uuid.UUID, ops []SCIMPatchEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const lockQ = `SELECT id FROM governance_groups WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL FOR UPDATE`
	var checkID uuid.UUID
	if err := tx.QueryRow(ctx, lockQ, id, customerID).Scan(&checkID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSCIMNotFound
		}
		return err
	}

	for _, op := range ops {
		op.Op = strings.ToLower(op.Op)
		path := strings.ToLower(op.Path)

		if (op.Op == "replace" || op.Op == "add") && (path == "displayname" || path == "") {
			if v, ok := op.Value.(string); ok && path == "displayname" {
				if _, err := tx.Exec(ctx, `UPDATE governance_groups SET display_name = $1, updated_at = NOW() WHERE id = $2`, v, id); err != nil {
					return err
				}
			}
			if m, ok := op.Value.(map[string]any); ok {
				if v, ok := m["displayName"].(string); ok {
					if _, err := tx.Exec(ctx, `UPDATE governance_groups SET display_name = $1, updated_at = NOW() WHERE id = $2`, v, id); err != nil {
						return err
					}
				}
			}
		}

		if op.Op == "add" && (path == "members" || strings.HasPrefix(path, "members")) {
			memberIDs := extractMemberIDs(op.Value)
			for _, uid := range memberIDs {
				if _, err := tx.Exec(ctx,
					`INSERT INTO governance_user_group_memberships (customer_id, user_id, group_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
					customerID, uid, id,
				); err != nil {
					return err
				}
			}
		}

		if op.Op == "remove" && (path == "members" || strings.HasPrefix(path, "members")) {
			// SCIM filtered remove: path looks like `members[value eq "uuid"]`.
			// Extract from path or from value.
			memberIDs := extractMemberIDs(op.Value)
			if len(memberIDs) == 0 {
				memberIDs = extractMemberIDFromFilterPath(op.Path)
			}
			for _, uid := range memberIDs {
				if _, err := tx.Exec(ctx,
					`DELETE FROM governance_user_group_memberships WHERE customer_id = $1 AND user_id = $2 AND group_id = $3`,
					customerID, uid, id,
				); err != nil {
					return err
				}
			}
		}
	}

	_, _ = tx.Exec(ctx, `UPDATE governance_groups SET updated_at = NOW() WHERE id = $1`, id)
	return tx.Commit(ctx)
}

func extractMemberIDs(v any) []uuid.UUID {
	out := []uuid.UUID{}
	arr, ok := v.([]any)
	if !ok {
		return out
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := m["value"].(string); ok {
			if uid, err := uuid.Parse(id); err == nil {
				out = append(out, uid)
			}
		}
	}
	return out
}

// extractMemberIDFromFilterPath parses `members[value eq "uuid"]` style paths
// that Okta uses for individual member removal.
func extractMemberIDFromFilterPath(path string) []uuid.UUID {
	out := []uuid.UUID{}
	start := strings.Index(path, "\"")
	end := strings.LastIndex(path, "\"")
	if start < 0 || end <= start {
		return out
	}
	id := path[start+1 : end]
	if uid, err := uuid.Parse(id); err == nil {
		out = append(out, uid)
	}
	return out
}

func (s *SCIMStore) SoftDeleteGroup(ctx context.Context, customerID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE governance_groups SET deleted_at = NOW() WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL`,
		id, customerID)
	if err != nil {
		return fmt.Errorf("soft-delete group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSCIMNotFound
	}
	_, _ = s.pool.Exec(ctx,
		`DELETE FROM governance_user_group_memberships WHERE customer_id = $1 AND group_id = $2`,
		customerID, id)
	return nil
}

// ============================================================
// Helpers
// ============================================================

// UserGroupBreakdown returns a map of user_id → display_name for users in a
// customer, joined with their primary group. Used by pilot-report
// enrichment to produce a "by department" rollup.
func (s *SCIMStore) UserGroupBreakdown(ctx context.Context, customerID uuid.UUID) (map[string]string, error) {
	const q = `
SELECT u.user_name, COALESCE(g.display_name, '')
FROM governance_users u
LEFT JOIN governance_user_group_memberships m
    ON m.customer_id = u.customer_id AND m.user_id = u.id
LEFT JOIN governance_groups g
    ON g.id = m.group_id AND g.deleted_at IS NULL
WHERE u.customer_id = $1 AND u.deleted_at IS NULL`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var userName, groupName string
		if err := rows.Scan(&userName, &groupName); err != nil {
			return nil, err
		}
		// Last-wins on multi-group users — fine for v1, IdP-driven pilots
		// usually have one primary department per user. v1.1 can switch
		// to "list of groups" if needed.
		if groupName != "" {
			out[userName] = groupName
		}
	}
	return out, nil
}

func sha256Hex(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanUser(row pgx.Row) (*GovernanceUser, error) {
	u := &GovernanceUser{}
	if err := row.Scan(
		&u.ID, &u.CustomerID, &u.ExternalID, &u.UserName, &u.Email,
		&u.DisplayName, &u.GivenName, &u.FamilyName, &u.Active,
		&u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSCIMNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

func scanUserFromRows(rows pgx.Rows) (*GovernanceUser, error) {
	u := &GovernanceUser{}
	if err := rows.Scan(
		&u.ID, &u.CustomerID, &u.ExternalID, &u.UserName, &u.Email,
		&u.DisplayName, &u.GivenName, &u.FamilyName, &u.Active,
		&u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	); err != nil {
		return nil, err
	}
	return u, nil
}

func scanGroup(row pgx.Row) (*GovernanceGroup, error) {
	g := &GovernanceGroup{}
	if err := row.Scan(
		&g.ID, &g.CustomerID, &g.ExternalID, &g.DisplayName,
		&g.CreatedAt, &g.UpdatedAt, &g.DeletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSCIMNotFound
		}
		return nil, fmt.Errorf("scan group: %w", err)
	}
	return g, nil
}

func scanGroupFromRows(rows pgx.Rows) (*GovernanceGroup, error) {
	g := &GovernanceGroup{}
	if err := rows.Scan(
		&g.ID, &g.CustomerID, &g.ExternalID, &g.DisplayName,
		&g.CreatedAt, &g.UpdatedAt, &g.DeletedAt,
	); err != nil {
		return nil, err
	}
	return g, nil
}

// LookupUserByExternalIDOrUsername is used by the event ingest path to
// enrich incoming events with internal user_id when the extension's
// external_user_id matches an SCIM-pushed user.
func (s *SCIMStore) LookupUserByExternalIDOrUsername(ctx context.Context, customerID uuid.UUID, hint string) (*GovernanceUser, error) {
	const q = `
SELECT id, customer_id, COALESCE(external_id,''), user_name, COALESCE(email,''),
       display_name, given_name, family_name, active, created_at, updated_at, deleted_at
FROM governance_users
WHERE customer_id = $1
  AND deleted_at IS NULL
  AND (external_id = $2 OR user_name = $2 OR email = $2)
LIMIT 1`
	row := s.pool.QueryRow(ctx, q, customerID, hint)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, ErrSCIMNotFound) {
			return nil, nil
		}
		return nil, err
	}
	_ = time.Now() // keep import alive
	return u, nil
}
