package overlay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a lookup by ID yields no row. Callers
// typically map this to a 404 at the HTTP boundary.
var ErrNotFound = errors.New("overlay: not found")

// ErrInvalidState is returned when a state transition is rejected
// (e.g. activating a version that is not in draft or shadow).
var ErrInvalidState = errors.New("overlay: invalid state transition")

// VersionState is the lifecycle of a single overlay version. Mirrors
// the DB CHECK constraint. State transitions are guarded by the store.
type VersionState string

const (
	StateDraft      VersionState = "draft"
	StateShadow     VersionState = "shadow"
	StateActive     VersionState = "active"
	StateSuperseded VersionState = "superseded"
)

// Overlay is the DB-level representation of a tenant_policy_overlays row.
type Overlay struct {
	ID              uuid.UUID  `json:"id"`
	CustomerID      uuid.UUID  `json:"customer_id"`
	ProxyID         *uuid.UUID `json:"proxy_id,omitempty"`
	Name            string     `json:"name"`
	ActiveVersionID *uuid.UUID `json:"active_version_id,omitempty"`
	Description     string     `json:"description"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Version is the DB-level representation of a
// tenant_policy_overlay_versions row with the snapshot decoded.
type Version struct {
	ID               uuid.UUID    `json:"id"`
	OverlayID        uuid.UUID    `json:"overlay_id"`
	CustomerID       uuid.UUID    `json:"customer_id"`
	Version          int          `json:"version"`
	State            VersionState `json:"state"`
	Snapshot         OverlaySnapshot `json:"snapshot"`
	Source           string       `json:"source"`
	CommitMessage    string       `json:"commit_message"`
	CreatedBy        string       `json:"created_by"`
	ShadowStartedAt  *time.Time   `json:"shadow_started_at,omitempty"`
	ActivatedAt      *time.Time   `json:"activated_at,omitempty"`
	SupersededAt     *time.Time   `json:"superseded_at,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
}

// ShadowEvent is one row from tenant_policy_overlay_shadow_events —
// one divergence observed between the active effective profile and
// the shadow-candidate effective profile. Written asynchronously so
// the gateway request path is unaffected.
type ShadowEvent struct {
	ID              uuid.UUID       `json:"id"`
	CustomerID      uuid.UUID       `json:"customer_id"`
	OverlayID       uuid.UUID       `json:"overlay_id"`
	ShadowVersionID uuid.UUID       `json:"shadow_version_id"`
	ActiveVersionID *uuid.UUID      `json:"active_version_id,omitempty"`
	TraceID         *uuid.UUID      `json:"trace_id,omitempty"`
	Divergence      string          `json:"divergence"`
	ActiveAction    string          `json:"active_action"`
	ShadowAction    string          `json:"shadow_action"`
	Detail          json.RawMessage `json:"detail,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// Allowed divergence values. Extend carefully — the UI groups on these.
const (
	DivergenceWouldBlock       = "would_block"
	DivergenceWouldAllow       = "would_allow"
	DivergenceThresholdDiff    = "threshold_diff"
	DivergencePatternMatchDiff = "pattern_match_diff"
	DivergenceNewDetectorFired = "new_detector_fired"
)

// AuditEntry is one row from tenant_policy_overlay_audit.
type AuditEntry struct {
	ID         uuid.UUID       `json:"id"`
	OverlayID  uuid.UUID       `json:"overlay_id"`
	CustomerID uuid.UUID       `json:"customer_id"`
	VersionID  *uuid.UUID      `json:"version_id,omitempty"`
	Event      string          `json:"event"`
	Actor      string          `json:"actor"`
	Reason     string          `json:"reason"`
	Diff       json.RawMessage `json:"diff,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// Store encapsulates the DB operations for overlays. The HTTP handler
// and the loader both go through it so query details live in one file.
type Store struct {
	db *pgxpool.Pool
}

// NewStore constructs a Store over the shared pool.
func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// ListOverlays returns all overlays for a customer.
func (s *Store) ListOverlays(ctx context.Context, customerID uuid.UUID) ([]Overlay, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, customer_id, proxy_id, name, active_version_id, description, created_at, updated_at
		FROM tenant_policy_overlays
		WHERE customer_id = $1
		ORDER BY created_at DESC
	`, customerID)
	if err != nil {
		return nil, fmt.Errorf("list overlays: %w", err)
	}
	defer rows.Close()

	var out []Overlay
	for rows.Next() {
		var o Overlay
		if err := rows.Scan(&o.ID, &o.CustomerID, &o.ProxyID, &o.Name, &o.ActiveVersionID, &o.Description, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan overlay: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetOverlay fetches one overlay by ID, scoped to the customer.
func (s *Store) GetOverlay(ctx context.Context, customerID, id uuid.UUID) (*Overlay, error) {
	var o Overlay
	err := s.db.QueryRow(ctx, `
		SELECT id, customer_id, proxy_id, name, active_version_id, description, created_at, updated_at
		FROM tenant_policy_overlays
		WHERE id = $1 AND customer_id = $2
	`, id, customerID).Scan(&o.ID, &o.CustomerID, &o.ProxyID, &o.Name, &o.ActiveVersionID, &o.Description, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get overlay: %w", err)
	}
	return &o, nil
}

// CreateOverlay inserts a new overlay container plus a draft version 1
// containing the provided snapshot. Returns the overlay and the version.
func (s *Store) CreateOverlay(
	ctx context.Context,
	customerID uuid.UUID,
	proxyID *uuid.UUID,
	name, description, createdBy, commitMessage, source string,
	snap *OverlaySnapshot,
) (*Overlay, *Version, error) {
	if snap == nil {
		return nil, nil, fmt.Errorf("snapshot is required")
	}
	if err := snap.Validate(); err != nil {
		return nil, nil, err
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal snapshot: %w", err)
	}
	if len(snapJSON) > MaxSnapshotBytes {
		return nil, nil, fmt.Errorf("snapshot exceeds %d bytes", MaxSnapshotBytes)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var o Overlay
	err = tx.QueryRow(ctx, `
		INSERT INTO tenant_policy_overlays (customer_id, proxy_id, name, description)
		VALUES ($1, $2, $3, $4)
		RETURNING id, customer_id, proxy_id, name, active_version_id, description, created_at, updated_at
	`, customerID, proxyID, name, description).Scan(
		&o.ID, &o.CustomerID, &o.ProxyID, &o.Name, &o.ActiveVersionID, &o.Description, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("insert overlay: %w", err)
	}

	var v Version
	v.Snapshot = *snap
	err = tx.QueryRow(ctx, `
		INSERT INTO tenant_policy_overlay_versions (
			overlay_id, customer_id, version, state, snapshot,
			source, commit_message, created_by
		)
		VALUES ($1, $2, 1, 'draft', $3, $4, $5, $6)
		RETURNING id, overlay_id, customer_id, version, state::text,
			source, commit_message, created_by, created_at
	`, o.ID, customerID, snapJSON, source, commitMessage, createdBy).Scan(
		&v.ID, &v.OverlayID, &v.CustomerID, &v.Version, &v.State,
		&v.Source, &v.CommitMessage, &v.CreatedBy, &v.CreatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("insert version: %w", err)
	}

	if err := writeAudit(ctx, tx, o.ID, customerID, &v.ID, "created", createdBy, commitMessage, nil); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return &o, &v, nil
}

// DeleteOverlay removes an overlay and all its related rows
// (versions, shadow events, audit) in one transaction. Works even
// when the overlay is active — deletion is the user's intent and
// they've confirmed at the UI. The cache invalidation step in the
// handler ensures subsequent requests stop seeing this overlay.
//
// FK order matters: the schema uses RESTRICT rather than CASCADE on
// most relationships (to prevent accidental bulk deletes elsewhere),
// so everything referencing the overlay or its versions must be
// removed before the overlay row itself. Order:
//  1. shadow events — reference overlay (CASCADE) AND versions (RESTRICT on active_version_id).
//  2. audit rows — reference overlay and versions with RESTRICT.
//  3. clear overlay.active_version_id — overlay → version FK.
//  4. versions — safe now that nothing references them.
//  5. overlay.
//
// The "deleted" event audit row that previous revisions of this
// function tried to write is intentionally omitted: the audit table
// is FK-bound to the overlay, so any row describing the deletion
// would be wiped by step 2 anyway. Forensic tracking of deletions
// belongs in a process-wide audit log, not the per-overlay table.
// The actor parameter is preserved on the signature for when that
// system-wide log lands.
func (s *Store) DeleteOverlay(ctx context.Context, customerID, id uuid.UUID, actor string) error {
	_ = actor // reserved for the future process-wide deletion-audit log
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var owned bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tenant_policy_overlays WHERE id = $1 AND customer_id = $2)
	`, id, customerID).Scan(&owned); err != nil {
		return fmt.Errorf("lookup overlay: %w", err)
	}
	if !owned {
		return ErrNotFound
	}

	steps := []struct {
		label string
		sql   string
	}{
		{"shadow events", `DELETE FROM tenant_policy_overlay_shadow_events WHERE overlay_id = $1`},
		{"audit", `DELETE FROM tenant_policy_overlay_audit WHERE overlay_id = $1`},
		{"clear active ptr", `UPDATE tenant_policy_overlays SET active_version_id = NULL WHERE id = $1`},
		{"versions", `DELETE FROM tenant_policy_overlay_versions WHERE overlay_id = $1`},
	}
	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.sql, id); err != nil {
			return fmt.Errorf("delete %s: %w", step.label, err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tenant_policy_overlays WHERE id = $1 AND customer_id = $2`, id, customerID); err != nil {
		return fmt.Errorf("delete overlay: %w", err)
	}
	return tx.Commit(ctx)
}

// ListVersions returns versions for an overlay, newest first.
func (s *Store) ListVersions(ctx context.Context, customerID, overlayID uuid.UUID) ([]Version, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, overlay_id, customer_id, version, state::text, snapshot,
			source, commit_message, created_by,
			shadow_started_at, activated_at, superseded_at, created_at
		FROM tenant_policy_overlay_versions
		WHERE customer_id = $1 AND overlay_id = $2
		ORDER BY version DESC
	`, customerID, overlayID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()

	var out []Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetVersion fetches one version by number, scoped to (customer, overlay).
func (s *Store) GetVersion(ctx context.Context, customerID, overlayID uuid.UUID, version int) (*Version, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, overlay_id, customer_id, version, state::text, snapshot,
			source, commit_message, created_by,
			shadow_started_at, activated_at, superseded_at, created_at
		FROM tenant_policy_overlay_versions
		WHERE customer_id = $1 AND overlay_id = $2 AND version = $3
	`, customerID, overlayID, version)
	v, err := scanVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// GetActiveVersion returns the currently-active version for an overlay,
// or ErrNotFound if none exists.
func (s *Store) GetActiveVersion(ctx context.Context, customerID, overlayID uuid.UUID) (*Version, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, overlay_id, customer_id, version, state::text, snapshot,
			source, commit_message, created_by,
			shadow_started_at, activated_at, superseded_at, created_at
		FROM tenant_policy_overlay_versions
		WHERE customer_id = $1 AND overlay_id = $2 AND state = 'active'
		LIMIT 1
	`, customerID, overlayID)
	v, err := scanVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// GetShadowVersion returns the current shadow version, or ErrNotFound.
func (s *Store) GetShadowVersion(ctx context.Context, customerID, overlayID uuid.UUID) (*Version, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, overlay_id, customer_id, version, state::text, snapshot,
			source, commit_message, created_by,
			shadow_started_at, activated_at, superseded_at, created_at
		FROM tenant_policy_overlay_versions
		WHERE customer_id = $1 AND overlay_id = $2 AND state = 'shadow'
		LIMIT 1
	`, customerID, overlayID)
	v, err := scanVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// CreateVersion appends a new draft version to an existing overlay.
// version number is the next contiguous integer (MAX(version)+1).
func (s *Store) CreateVersion(
	ctx context.Context,
	customerID, overlayID uuid.UUID,
	snap *OverlaySnapshot,
	source, commitMessage, createdBy string,
) (*Version, error) {
	if snap == nil {
		return nil, fmt.Errorf("snapshot is required")
	}
	if err := snap.Validate(); err != nil {
		return nil, err
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}
	if len(snapJSON) > MaxSnapshotBytes {
		return nil, fmt.Errorf("snapshot exceeds %d bytes", MaxSnapshotBytes)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Confirm the overlay belongs to the customer. This is the
	// multi-tenancy guard — the subsequent queries key off overlay_id
	// but the ownership check lives here.
	var owned bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tenant_policy_overlays WHERE id = $1 AND customer_id = $2)
	`, overlayID, customerID).Scan(&owned); err != nil {
		return nil, fmt.Errorf("owner check: %w", err)
	}
	if !owned {
		return nil, ErrNotFound
	}

	var nextVersion int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM tenant_policy_overlay_versions
		WHERE overlay_id = $1
	`, overlayID).Scan(&nextVersion); err != nil {
		return nil, fmt.Errorf("next version: %w", err)
	}

	var v Version
	v.Snapshot = *snap
	err = tx.QueryRow(ctx, `
		INSERT INTO tenant_policy_overlay_versions (
			overlay_id, customer_id, version, state, snapshot,
			source, commit_message, created_by
		)
		VALUES ($1, $2, $3, 'draft', $4, $5, $6, $7)
		RETURNING id, overlay_id, customer_id, version, state::text,
			source, commit_message, created_by, created_at
	`, overlayID, customerID, nextVersion, snapJSON, source, commitMessage, createdBy).Scan(
		&v.ID, &v.OverlayID, &v.CustomerID, &v.Version, &v.State,
		&v.Source, &v.CommitMessage, &v.CreatedBy, &v.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert version: %w", err)
	}

	if err := writeAudit(ctx, tx, overlayID, customerID, &v.ID, "created", createdBy, commitMessage, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &v, nil
}

// PromoteToShadow moves a draft version into shadow state. If another
// shadow exists, it's demoted back to draft in the same transaction —
// a one-shadow-per-overlay invariant (also enforced by partial index).
func (s *Store) PromoteToShadow(ctx context.Context, customerID, overlayID uuid.UUID, version int, actor, reason string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Ensure the target version is in draft.
	var state string
	var versionID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id, state::text FROM tenant_policy_overlay_versions
		WHERE customer_id = $1 AND overlay_id = $2 AND version = $3
	`, customerID, overlayID, version).Scan(&versionID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup version: %w", err)
	}
	if state != string(StateDraft) {
		return fmt.Errorf("%w: version is %s, want draft", ErrInvalidState, state)
	}

	// Demote any existing shadow back to draft.
	if _, err := tx.Exec(ctx, `
		UPDATE tenant_policy_overlay_versions
		SET state = 'draft', shadow_started_at = NULL
		WHERE overlay_id = $1 AND state = 'shadow'
	`, overlayID); err != nil {
		return fmt.Errorf("demote prior shadow: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE tenant_policy_overlay_versions
		SET state = 'shadow', shadow_started_at = NOW()
		WHERE id = $1
	`, versionID); err != nil {
		return fmt.Errorf("promote to shadow: %w", err)
	}

	if err := writeAudit(ctx, tx, overlayID, customerID, &versionID, "shadowed", actor, reason, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Activate promotes a version to active. Any current active is
// superseded. The overlay's active_version_id is updated atomically.
// Accepts either a draft or shadow source state.
func (s *Store) Activate(ctx context.Context, customerID, overlayID uuid.UUID, version int, actor, reason string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var state string
	var versionID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id, state::text FROM tenant_policy_overlay_versions
		WHERE customer_id = $1 AND overlay_id = $2 AND version = $3
	`, customerID, overlayID, version).Scan(&versionID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup version: %w", err)
	}
	if state != string(StateDraft) && state != string(StateShadow) {
		return fmt.Errorf("%w: version is %s, want draft or shadow", ErrInvalidState, state)
	}

	// Supersede the current active (if any).
	if _, err := tx.Exec(ctx, `
		UPDATE tenant_policy_overlay_versions
		SET state = 'superseded', superseded_at = NOW()
		WHERE overlay_id = $1 AND state = 'active'
	`, overlayID); err != nil {
		return fmt.Errorf("supersede prior active: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE tenant_policy_overlay_versions
		SET state = 'active', activated_at = NOW(), shadow_started_at = NULL
		WHERE id = $1
	`, versionID); err != nil {
		return fmt.Errorf("activate: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE tenant_policy_overlays
		SET active_version_id = $1
		WHERE id = $2 AND customer_id = $3
	`, versionID, overlayID, customerID); err != nil {
		return fmt.Errorf("update overlay active ptr: %w", err)
	}

	if err := writeAudit(ctx, tx, overlayID, customerID, &versionID, "activated", actor, reason, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Rollback activates the most recent superseded version. Useful when a
// freshly activated overlay is misbehaving.
func (s *Store) Rollback(ctx context.Context, customerID, overlayID uuid.UUID, actor, reason string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var versionID uuid.UUID
	var versionNum int
	err = tx.QueryRow(ctx, `
		SELECT id, version FROM tenant_policy_overlay_versions
		WHERE customer_id = $1 AND overlay_id = $2 AND state = 'superseded'
		ORDER BY superseded_at DESC NULLS LAST, version DESC
		LIMIT 1
	`, customerID, overlayID).Scan(&versionID, &versionNum)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: no superseded version to roll back to", ErrInvalidState)
	}
	if err != nil {
		return fmt.Errorf("find rollback target: %w", err)
	}

	// Supersede the current active.
	if _, err := tx.Exec(ctx, `
		UPDATE tenant_policy_overlay_versions
		SET state = 'superseded', superseded_at = NOW()
		WHERE overlay_id = $1 AND state = 'active'
	`, overlayID); err != nil {
		return fmt.Errorf("supersede current: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE tenant_policy_overlay_versions
		SET state = 'active', activated_at = NOW(), superseded_at = NULL
		WHERE id = $1
	`, versionID); err != nil {
		return fmt.Errorf("reactivate: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE tenant_policy_overlays
		SET active_version_id = $1
		WHERE id = $2 AND customer_id = $3
	`, versionID, overlayID, customerID); err != nil {
		return fmt.Errorf("update overlay active ptr: %w", err)
	}

	if err := writeAudit(ctx, tx, overlayID, customerID, &versionID, "rolled_back", actor, reason, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListAudit returns audit entries for an overlay, newest first.
func (s *Store) ListAudit(ctx context.Context, customerID, overlayID uuid.UUID, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, overlay_id, customer_id, version_id, event, actor, reason, diff, created_at
		FROM tenant_policy_overlay_audit
		WHERE customer_id = $1 AND overlay_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, customerID, overlayID, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.OverlayID, &a.CustomerID, &a.VersionID, &a.Event, &a.Actor, &a.Reason, &a.Diff, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// scanVersion accepts either pgx.Row or pgx.Rows. Both expose Scan with
// the same column order used throughout this file.
type scanner interface {
	Scan(dest ...any) error
}

func scanVersion(s scanner) (Version, error) {
	var v Version
	var snapJSON []byte
	if err := s.Scan(
		&v.ID, &v.OverlayID, &v.CustomerID, &v.Version, &v.State, &snapJSON,
		&v.Source, &v.CommitMessage, &v.CreatedBy,
		&v.ShadowStartedAt, &v.ActivatedAt, &v.SupersededAt, &v.CreatedAt,
	); err != nil {
		return v, err
	}
	if err := json.Unmarshal(snapJSON, &v.Snapshot); err != nil {
		return v, fmt.Errorf("decode snapshot for version %s: %w", v.ID, err)
	}
	return v, nil
}

// Template is one row from tenant_policy_overlay_templates — a
// pre-built snapshot for a vertical that tenants can clone via
// POST /overlays/from-template. Built-in templates are seeded by
// migration 011; is_builtin distinguishes them from any future
// customer-uploaded templates.
type Template struct {
	ID          uuid.UUID       `json:"id"`
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Snapshot    OverlaySnapshot `json:"snapshot"`
	IsBuiltin   bool            `json:"is_builtin"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// ListTemplates returns all templates. Global — no customer scope.
// Built-ins come first, then any non-builtins.
func (s *Store) ListTemplates(ctx context.Context) ([]Template, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, slug, name, description, snapshot, is_builtin, created_at, updated_at
		FROM tenant_policy_overlay_templates
		ORDER BY is_builtin DESC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var out []Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTemplateBySlug fetches one template by its stable slug.
func (s *Store) GetTemplateBySlug(ctx context.Context, slug string) (*Template, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, slug, name, description, snapshot, is_builtin, created_at, updated_at
		FROM tenant_policy_overlay_templates
		WHERE slug = $1
	`, slug)
	t, err := scanTemplate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanTemplate(s scanner) (Template, error) {
	var t Template
	var snapJSON []byte
	if err := s.Scan(&t.ID, &t.Slug, &t.Name, &t.Description, &snapJSON, &t.IsBuiltin, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return t, err
	}
	if err := json.Unmarshal(snapJSON, &t.Snapshot); err != nil {
		return t, fmt.Errorf("decode template snapshot %s: %w", t.Slug, err)
	}
	return t, nil
}

// InsertShadowEvent records one divergence. Called from the async
// shadow goroutine in DetectHandler. Never mutates user-facing state —
// a DB failure here is logged by the caller and then swallowed.
func (s *Store) InsertShadowEvent(ctx context.Context, e *ShadowEvent) error {
	if e == nil {
		return fmt.Errorf("event is nil")
	}
	detail := e.Detail
	if len(detail) == 0 {
		detail = json.RawMessage(`{}`)
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO tenant_policy_overlay_shadow_events (
			customer_id, overlay_id, shadow_version_id, active_version_id,
			trace_id, divergence, active_action, shadow_action, detail
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, e.CustomerID, e.OverlayID, e.ShadowVersionID, e.ActiveVersionID,
		e.TraceID, e.Divergence, e.ActiveAction, e.ShadowAction, detail)
	if err != nil {
		return fmt.Errorf("insert shadow event: %w", err)
	}
	return nil
}

// ListShadowEvents returns shadow events for an overlay, newest first.
// limit is clamped to [1,500]; zero uses the default 100.
func (s *Store) ListShadowEvents(ctx context.Context, customerID, overlayID uuid.UUID, limit int) ([]ShadowEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, customer_id, overlay_id, shadow_version_id, active_version_id,
			trace_id, divergence, active_action, shadow_action, detail, created_at
		FROM tenant_policy_overlay_shadow_events
		WHERE customer_id = $1 AND overlay_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, customerID, overlayID, limit)
	if err != nil {
		return nil, fmt.Errorf("list shadow events: %w", err)
	}
	defer rows.Close()

	var out []ShadowEvent
	for rows.Next() {
		var e ShadowEvent
		if err := rows.Scan(
			&e.ID, &e.CustomerID, &e.OverlayID, &e.ShadowVersionID, &e.ActiveVersionID,
			&e.TraceID, &e.Divergence, &e.ActiveAction, &e.ShadowAction, &e.Detail, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan shadow event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// writeAudit inserts one audit row inside the caller's transaction.
func writeAudit(
	ctx context.Context,
	tx pgx.Tx,
	overlayID, customerID uuid.UUID,
	versionID *uuid.UUID,
	event, actor, reason string,
	diff json.RawMessage,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tenant_policy_overlay_audit (
			overlay_id, customer_id, version_id, event, actor, reason, diff
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, overlayID, customerID, versionID, event, actor, reason, diff)
	if err != nil {
		return fmt.Errorf("write audit: %w", err)
	}
	return nil
}
