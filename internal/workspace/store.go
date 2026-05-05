package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a row lookup yields zero results.
var ErrNotFound = errors.New("workspace: not found")

// Store wraps the Postgres pool with all workspace queries. Every query
// is customer_id-scoped per the cross-repo multi-tenancy rule.
type Store struct {
	pool *pgxpool.Pool

	// pgvectorReady caches the result of a one-time probe against
	// pg_extension. Loaded ⇒ writes go to both REAL[] and vector(1536),
	// retrieval uses pgvector's `<=>` operator with the HNSW index.
	// Unloaded ⇒ REAL[]-only writes, in-process cosine fallback.
	pgvectorOnce  sync.Once
	pgvectorReady bool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// PGVectorReady returns true when the `vector` extension is loaded in
// the database the pool talks to. Detected once on first call, cached
// for the process lifetime — operators install pgvector before boot
// and don't toggle it at runtime.
func (s *Store) PGVectorReady(ctx context.Context) bool {
	s.pgvectorOnce.Do(func() {
		var n int
		err := s.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM pg_extension WHERE extname = 'vector'`).Scan(&n)
		s.pgvectorReady = err == nil && n > 0
	})
	return s.pgvectorReady
}

// vectorLiteral formats a float32 slice as the textual `[a,b,c]` form
// pgvector accepts. pgx doesn't natively codec vectors so we lean on
// the textual representation; it's not the fastest but a 1536-dim
// vector serializes in <50µs which is dwarfed by the network hop.
func vectorLiteral(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(v) * 10)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// =============================================================================
// Settings
// =============================================================================

// EnsureSettings reads or lazily-creates the settings row for a customer.
func (s *Store) EnsureSettings(ctx context.Context, customerID uuid.UUID) (*Settings, error) {
	const insert = `INSERT INTO workspace_settings (customer_id) VALUES ($1)
ON CONFLICT (customer_id) DO NOTHING`
	if _, err := s.pool.Exec(ctx, insert, customerID); err != nil {
		return nil, fmt.Errorf("ensure settings: %w", err)
	}
	return s.GetSettings(ctx, customerID)
}

func (s *Store) GetSettings(ctx context.Context, customerID uuid.UUID) (*Settings, error) {
	const q = `SELECT customer_id, branding, default_assistant_id, seat_limit,
retention_days, spend_cap_cents, billing_mode, allowed_models,
ai_persona_name, ai_persona_personality, ai_persona_tone,
onboarding_completed_at, disable_image_attachments,
created_at, updated_at
FROM workspace_settings WHERE customer_id = $1`
	row := s.pool.QueryRow(ctx, q, customerID)
	var st Settings
	var allowedRaw []byte
	err := row.Scan(&st.CustomerID, &st.Branding, &st.DefaultAssistantID,
		&st.SeatLimit, &st.RetentionDays, &st.SpendCapCents,
		&st.BillingMode, &allowedRaw,
		&st.AIPersonaName, &st.AIPersonaPersonality, &st.AIPersonaTone,
		&st.OnboardingCompletedAt, &st.DisableImageAttachments,
		&st.CreatedAt, &st.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}
	if len(st.Branding) == 0 {
		st.Branding = json.RawMessage(`{}`)
	}
	// Decode the JSONB allowed_models column into the typed slice.
	// Treat empty/null as "use defaults" (empty slice).
	st.AllowedModels = []AllowedModel{}
	if len(allowedRaw) > 0 {
		if err := json.Unmarshal(allowedRaw, &st.AllowedModels); err != nil {
			return nil, fmt.Errorf("decode allowed_models: %w", err)
		}
	}
	return &st, nil
}

// UpdateSettings patches branding + limits in one shot.
func (s *Store) UpdateSettings(ctx context.Context, customerID uuid.UUID, p SettingsPatch) (*Settings, error) {
	// allowed_models is a slice — encode to JSONB only when the patch
	// supplies one. Nil pointer = leave existing value alone.
	var allowedJSON []byte
	if p.AllowedModels != nil {
		j, err := json.Marshal(p.AllowedModels)
		if err != nil {
			return nil, fmt.Errorf("marshal allowed_models: %w", err)
		}
		allowedJSON = j
	}
	const q = `UPDATE workspace_settings SET
branding               = COALESCE($2, branding),
seat_limit             = COALESCE($3, seat_limit),
retention_days         = COALESCE($4, retention_days),
spend_cap_cents        = COALESCE($5, spend_cap_cents),
billing_mode           = COALESCE($6, billing_mode),
allowed_models         = COALESCE($7::jsonb, allowed_models),
ai_persona_name        = COALESCE($8, ai_persona_name),
ai_persona_personality = COALESCE($9, ai_persona_personality),
ai_persona_tone        = COALESCE($10, ai_persona_tone),
disable_image_attachments = COALESCE($11, disable_image_attachments)
WHERE customer_id = $1`
	if _, err := s.pool.Exec(ctx, q, customerID, p.Branding, p.SeatLimit,
		p.RetentionDays, p.SpendCapCents, p.BillingMode, allowedJSON,
		p.AIPersonaName, p.AIPersonaPersonality, p.AIPersonaTone,
		p.DisableImageAttachments); err != nil {
		return nil, fmt.Errorf("update settings: %w", err)
	}
	return s.GetSettings(ctx, customerID)
}

// SettingsPatch is the partial-update body for settings. AllowedModels
// is a *slice — nil means "leave alone", empty slice means "clear".
//
// AI persona fields are *string — nil = leave alone, empty string =
// clear. The UPDATE uses COALESCE so passing nil keeps the existing
// value; passing "" overwrites with NULL via Postgres's empty-string
// vs NULL semantics (note: an empty string here writes the empty
// string, NOT NULL — to clear, callers should send a JSON null).
type SettingsPatch struct {
	Branding             json.RawMessage `json:"branding"`
	SeatLimit            *int            `json:"seat_limit"`
	RetentionDays        *int            `json:"retention_days"`
	SpendCapCents        *int            `json:"spend_cap_cents"`
	BillingMode          *string         `json:"billing_mode"`
	AllowedModels        *[]AllowedModel `json:"allowed_models"`
	AIPersonaName        *string         `json:"ai_persona_name"`
	AIPersonaPersonality *string         `json:"ai_persona_personality"`
	AIPersonaTone        *string         `json:"ai_persona_tone"`
	// DisableImageAttachments — admin toggle to hard-block image
	// uploads in the chat surface. nil = leave alone; pointer to
	// true/false replaces. See migration 030 for the rationale.
	DisableImageAttachments *bool `json:"disable_image_attachments"`
}

// EffectiveAllowedModels returns the merged allowed-models list a
// specific user sees, in precedence order:
//
//	1. workspace_members.allowed_models (per-user override) — when non-NULL
//	2. workspace_settings.allowed_models (customer-wide)    — when set
//	3. nil (caller treats as "open — full catalog visible")
//
// userID may be empty / "default-user" — that path skips the
// member-override step and falls straight to customer-wide. The
// chat send handler validates against this list before invoking
// the LLM provider.
func (s *Store) EffectiveAllowedModels(ctx context.Context, customerID uuid.UUID, userID string) ([]AllowedModel, error) {
	// 1. Member override — only attempt when we have a real user.
	if userID != "" && userID != "default-user" {
		var memberRaw []byte
		err := s.pool.QueryRow(ctx,
			`SELECT allowed_models FROM workspace_members
WHERE customer_id = $1 AND user_id = $2`,
			customerID, userID).Scan(&memberRaw)
		if err == nil && len(memberRaw) > 0 && string(memberRaw) != "null" {
			var out []AllowedModel
			if err := json.Unmarshal(memberRaw, &out); err == nil {
				return out, nil
			}
			// Malformed JSON in the column — log via the caller's
			// path; fall through to customer-wide rather than 500.
		}
		// errors.Is(err, pgx.ErrNoRows) means the user isn't a
		// workspace_member — they could still be the customer's
		// home auth_user (signup-time owner). Fall through.
	}

	// 2. Customer-wide.
	st, err := s.GetSettings(ctx, customerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return st.AllowedModels, nil
}

// =============================================================================
// Assistants
// =============================================================================

func (s *Store) ListAssistants(ctx context.Context, customerID uuid.UUID) ([]Assistant, error) {
	const q = `SELECT id, customer_id, name, description, system_prompt,
default_provider, default_model, language, suggested_prompts, is_default,
created_at, updated_at
FROM workspace_assistants
WHERE customer_id = $1 AND archived_at IS NULL
ORDER BY is_default DESC, created_at ASC`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("list assistants: %w", err)
	}
	defer rows.Close()
	out := []Assistant{}
	for rows.Next() {
		var a Assistant
		if err := rows.Scan(&a.ID, &a.CustomerID, &a.Name, &a.Description,
			&a.SystemPrompt, &a.DefaultProvider, &a.DefaultModel, &a.Language,
			&a.SuggestedPrompts, &a.IsDefault, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan assistant: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAssistant(ctx context.Context, customerID, id uuid.UUID) (*Assistant, error) {
	const q = `SELECT id, customer_id, name, description, system_prompt,
default_provider, default_model, language, suggested_prompts, is_default,
created_at, updated_at
FROM workspace_assistants
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	var a Assistant
	err := s.pool.QueryRow(ctx, q, customerID, id).Scan(&a.ID, &a.CustomerID,
		&a.Name, &a.Description, &a.SystemPrompt, &a.DefaultProvider,
		&a.DefaultModel, &a.Language, &a.SuggestedPrompts, &a.IsDefault,
		&a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get assistant: %w", err)
	}
	a.KnowledgeSourceIDs, err = s.listAssistantKnowledgeIDs(ctx, customerID, a.ID)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) listAssistantKnowledgeIDs(ctx context.Context, customerID, assistantID uuid.UUID) ([]uuid.UUID, error) {
	const q = `SELECT knowledge_source_id FROM workspace_assistant_knowledge
WHERE customer_id = $1 AND assistant_id = $2`
	rows, err := s.pool.Query(ctx, q, customerID, assistantID)
	if err != nil {
		return nil, fmt.Errorf("list assistant knowledge: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CreateAssistant inserts and returns the row. Setting IsDefault=true clears
// the previous default in the same transaction.
func (s *Store) CreateAssistant(ctx context.Context, a Assistant) (*Assistant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if a.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE workspace_assistants SET is_default = FALSE
WHERE customer_id = $1 AND is_default = TRUE`, a.CustomerID); err != nil {
			return nil, fmt.Errorf("clear default: %w", err)
		}
	}
	if len(a.SuggestedPrompts) == 0 {
		a.SuggestedPrompts = json.RawMessage(`[]`)
	}

	const q = `INSERT INTO workspace_assistants
(customer_id, name, description, system_prompt, default_provider,
default_model, language, suggested_prompts, is_default)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING id, created_at, updated_at`
	if err := tx.QueryRow(ctx, q,
		a.CustomerID, a.Name, a.Description, a.SystemPrompt,
		a.DefaultProvider, a.DefaultModel, a.Language,
		a.SuggestedPrompts, a.IsDefault,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert assistant: %w", err)
	}

	if a.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE workspace_settings SET default_assistant_id = $2
WHERE customer_id = $1`, a.CustomerID, a.ID); err != nil {
			return nil, fmt.Errorf("set settings default: %w", err)
		}
	}

	// Knowledge sources: insert join rows for each attached source.
	// Done in the same tx so a partial failure rolls back the whole
	// assistant — admins never end up with an assistant that's
	// half-attached to KB sources.
	if len(a.KnowledgeSourceIDs) > 0 {
		if err := setAssistantKnowledge(ctx, tx, a.CustomerID, a.ID, a.KnowledgeSourceIDs); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &a, nil
}

func (s *Store) UpdateAssistant(ctx context.Context, customerID, id uuid.UUID, p AssistantPatch) (*Assistant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Promotion to default: same logic as CreateAssistant — clear
	// any existing default in the same tx so we never end up with
	// two assistants flagged default at the same time. Then
	// workspace_settings.default_assistant_id is updated below.
	if p.IsDefault != nil && *p.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE workspace_assistants SET is_default = FALSE
WHERE customer_id = $1 AND is_default = TRUE AND id <> $2`, customerID, id); err != nil {
			return nil, fmt.Errorf("clear default: %w", err)
		}
	}

	const q = `UPDATE workspace_assistants SET
name              = COALESCE($3, name),
description       = COALESCE($4, description),
system_prompt     = COALESCE($5, system_prompt),
default_provider  = COALESCE($6, default_provider),
default_model     = COALESCE($7, default_model),
language          = COALESCE($8, language),
suggested_prompts = COALESCE($9, suggested_prompts),
is_default        = COALESCE($10, is_default)
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	tag, err := tx.Exec(ctx, q, customerID, id,
		p.Name, p.Description, p.SystemPrompt, p.DefaultProvider,
		p.DefaultModel, p.Language, p.SuggestedPrompts, p.IsDefault)
	if err != nil {
		return nil, fmt.Errorf("update assistant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	// Knowledge sources: nil pointer → leave attachments untouched
	// (partial PATCH semantics). Non-nil → replace the full set
	// (including empty slice meaning "detach all").
	if p.KnowledgeSourceIDs != nil {
		if err := setAssistantKnowledge(ctx, tx, customerID, id, *p.KnowledgeSourceIDs); err != nil {
			return nil, err
		}
	}
	// Mirror to workspace_settings so the chat surface's
	// "no-assistant default" lookup picks up the new pointer.
	if p.IsDefault != nil && *p.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE workspace_settings SET default_assistant_id = $2
WHERE customer_id = $1`, customerID, id); err != nil {
			return nil, fmt.Errorf("set settings default: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.GetAssistant(ctx, customerID, id)
}

// setAssistantKnowledge replaces the assistant's KB attachments
// atomically inside the caller's transaction. Pass an empty slice
// to detach everything. The DELETE + INSERT pattern is fine at
// expected scale (single-digit KB sources per assistant in
// practice).
func setAssistantKnowledge(ctx context.Context, tx pgx.Tx, customerID, assistantID uuid.UUID, ids []uuid.UUID) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM workspace_assistant_knowledge
WHERE customer_id = $1 AND assistant_id = $2`,
		customerID, assistantID,
	); err != nil {
		return fmt.Errorf("clear assistant knowledge: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	for _, sid := range ids {
		if _, err := tx.Exec(ctx,
			`INSERT INTO workspace_assistant_knowledge
(customer_id, assistant_id, knowledge_source_id) VALUES ($1, $2, $3)
ON CONFLICT (assistant_id, knowledge_source_id) DO NOTHING`,
			customerID, assistantID, sid,
		); err != nil {
			return fmt.Errorf("attach knowledge %s: %w", sid, err)
		}
	}
	return nil
}

// AssistantPatch is the partial-update body for assistants.
//
// Pointer fields use partial-update semantics: nil = leave alone,
// non-nil = replace. KnowledgeSourceIDs is *[]uuid.UUID rather than
// []uuid.UUID specifically so the caller can distinguish "don't
// touch attachments" (nil) from "detach everything" (empty slice).
//
// IsDefault, when true, atomically clears any other "default"
// assistant in the workspace (mirroring CreateAssistant). When
// false, no row is touched — admins can't currently *un-default*
// without picking a new default; that's the correct safety
// behavior since there should always be exactly one default.
type AssistantPatch struct {
	Name               *string         `json:"name"`
	Description        *string         `json:"description"`
	SystemPrompt       *string         `json:"system_prompt"`
	DefaultProvider    *string         `json:"default_provider"`
	DefaultModel       *string         `json:"default_model"`
	Language           *string         `json:"language"`
	SuggestedPrompts   json.RawMessage `json:"suggested_prompts"`
	IsDefault          *bool           `json:"is_default,omitempty"`
	KnowledgeSourceIDs *[]uuid.UUID    `json:"knowledge_source_ids,omitempty"`
}

func (s *Store) ArchiveAssistant(ctx context.Context, customerID, id uuid.UUID) error {
	const q = `UPDATE workspace_assistants SET archived_at = NOW()
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, customerID, id)
	if err != nil {
		return fmt.Errorf("archive assistant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// =============================================================================
// Knowledge sources
// =============================================================================

func (s *Store) ListKnowledgeSources(ctx context.Context, customerID uuid.UUID) ([]KnowledgeSource, error) {
	const q = `SELECT id, customer_id, name, type, source_ref, inline_text,
mime_type, size_bytes, character_count, last_synced_at,
status, error, content_hash, scan_result, metadata, created_at, updated_at
FROM workspace_knowledge_sources
WHERE customer_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("list knowledge sources: %w", err)
	}
	defer rows.Close()
	out := []KnowledgeSource{}
	for rows.Next() {
		var k KnowledgeSource
		if err := rows.Scan(&k.ID, &k.CustomerID, &k.Name, &k.Type,
			&k.SourceRef, &k.InlineText, &k.MimeType, &k.SizeBytes,
			&k.CharacterCount, &k.LastSyncedAt,
			&k.Status, &k.Error, &k.ContentHash, &k.ScanResult,
			&k.Metadata, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan kb: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) GetKnowledgeSource(ctx context.Context, customerID, id uuid.UUID) (*KnowledgeSource, error) {
	const q = `SELECT id, customer_id, name, type, source_ref, inline_text,
mime_type, size_bytes, character_count, last_synced_at,
status, error, content_hash, scan_result, metadata, created_at, updated_at
FROM workspace_knowledge_sources
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	var k KnowledgeSource
	err := s.pool.QueryRow(ctx, q, customerID, id).Scan(&k.ID, &k.CustomerID,
		&k.Name, &k.Type, &k.SourceRef, &k.InlineText, &k.MimeType,
		&k.SizeBytes, &k.CharacterCount, &k.LastSyncedAt,
		&k.Status, &k.Error, &k.ContentHash, &k.ScanResult,
		&k.Metadata, &k.CreatedAt, &k.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get kb: %w", err)
	}
	return &k, nil
}

func (s *Store) CreateKnowledgeSource(ctx context.Context, k KnowledgeSource) (*KnowledgeSource, error) {
	if len(k.Metadata) == 0 {
		k.Metadata = json.RawMessage(`{}`)
	}
	const q = `INSERT INTO workspace_knowledge_sources
(customer_id, name, type, source_ref, inline_text, mime_type, size_bytes,
status, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING id, created_at, updated_at`
	if err := s.pool.QueryRow(ctx, q,
		k.CustomerID, k.Name, k.Type, k.SourceRef, k.InlineText,
		k.MimeType, k.SizeBytes, k.Status, k.Metadata,
	).Scan(&k.ID, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert kb: %w", err)
	}
	return &k, nil
}

func (s *Store) ArchiveKnowledgeSource(ctx context.Context, customerID, id uuid.UUID) error {
	const q = `UPDATE workspace_knowledge_sources SET archived_at = NOW()
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, customerID, id)
	if err != nil {
		return fmt.Errorf("archive kb: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// =============================================================================
// KB security pipeline — quarantine, release, hash recording.
// See migration 029 for the schema rationale.
// =============================================================================

// SetKnowledgeSourceHash records the SHA-256 of the raw uploaded
// bytes on the source row. Called once at upload time, before the
// blob is even saved to disk. Idempotent — re-uploading the same
// hash for the same source is a no-op (typically only happens on
// retry).
func (s *Store) SetKnowledgeSourceHash(ctx context.Context, customerID, id uuid.UUID, hash string) error {
	const q = `UPDATE workspace_knowledge_sources SET content_hash = $3
WHERE customer_id = $1 AND id = $2`
	_, err := s.pool.Exec(ctx, q, customerID, id, hash)
	if err != nil {
		return fmt.Errorf("set kb hash: %w", err)
	}
	return nil
}

// QuarantineKnowledgeSource transitions the source to status =
// 'quarantined' with the scan result snapshot + threat categories
// surfaced via the existing `error` column (so the dashboard's
// status-renderer doesn't need to learn JSONB).
//
// The chunk table is NOT touched — the worker should not have
// written any chunks for a blocked source. If somehow it has
// (race condition between scan and chunk write, defensive
// coding), they're left in place; the chunks-list UI surfaces
// them under the quarantined source's row and the admin can
// archive to clear.
func (s *Store) QuarantineKnowledgeSource(
	ctx context.Context,
	customerID, id uuid.UUID,
	categories []string,
	scanResult json.RawMessage,
) error {
	if scanResult == nil {
		scanResult = json.RawMessage(`{}`)
	}
	reason := "blocked by security scan"
	if len(categories) > 0 {
		reason = "blocked by security scan: " + strings.Join(categories, ", ")
	}
	const q = `UPDATE workspace_knowledge_sources
SET status = 'quarantined',
    error = $4,
    scan_result = $3
WHERE customer_id = $1 AND id = $2`
	tag, err := s.pool.Exec(ctx, q, customerID, id, scanResult, reason)
	if err != nil {
		return fmt.Errorf("quarantine kb: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReleaseKnowledgeSource is the admin-override "I reviewed the
// quarantined content and it's actually fine" action. Flips the
// status back to 'pending' so the next worker run picks it up
// for chunking. Only allowed when the source is currently
// quarantined — releasing a 'ready' source is a no-op (returns
// ErrNotFound).
//
// The scan_result snapshot is left in place so the audit trail
// shows what was caught + that an admin chose to release. The
// release action itself is also audited at the handler level.
func (s *Store) ReleaseKnowledgeSource(ctx context.Context, customerID, id uuid.UUID) error {
	const q = `UPDATE workspace_knowledge_sources
SET status = 'pending', error = NULL
WHERE customer_id = $1 AND id = $2 AND status = 'quarantined'`
	tag, err := s.pool.Exec(ctx, q, customerID, id)
	if err != nil {
		return fmt.Errorf("release kb: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// =============================================================================
// Conversations + messages
// =============================================================================

// ConversationListItem is a lightweight conversation row for sidebar lists.
type ConversationListItem struct {
	Conversation
	MessageCount     int    `json:"message_count"`
	LastMessageRole  string `json:"last_message_role,omitempty"`
	LastMessagePeek  string `json:"last_message_peek,omitempty"`
}

// ListConversations returns conversations for the given customer.
// userID == "" means "all conversations in the workspace" — used
// by admin/viewer surfaces for governance. Non-empty userID
// restricts to that user's own conversations — the privacy default
// for members. The handler decides which mode to call based on the
// caller's role.
func (s *Store) ListConversations(ctx context.Context, customerID uuid.UUID, userID string, limit int) ([]ConversationListItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT
  c.id, c.customer_id, c.user_id, c.assistant_id, c.title, c.pinned,
  c.last_message_at, c.created_at, c.updated_at,
  COALESCE(m.cnt, 0) AS msg_count,
  COALESCE(lm.role, '') AS last_role,
  COALESCE(SUBSTRING(lm.content FROM 1 FOR 200), '') AS last_peek
FROM workspace_conversations c
LEFT JOIN LATERAL (
  SELECT COUNT(*) AS cnt FROM workspace_messages
  WHERE conversation_id = c.id
) m ON TRUE
LEFT JOIN LATERAL (
  SELECT role, content FROM workspace_messages
  WHERE conversation_id = c.id
  ORDER BY created_at DESC LIMIT 1
) lm ON TRUE
WHERE c.customer_id = $1 AND c.archived_at IS NULL`
	args := []any{customerID}
	if userID != "" {
		q += ` AND c.user_id = $2`
		args = append(args, userID)
	}
	q += ` ORDER BY c.pinned DESC, c.last_message_at DESC LIMIT $` +
		intToStr(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	out := []ConversationListItem{}
	for rows.Next() {
		var item ConversationListItem
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.UserID,
			&item.AssistantID, &item.Title, &item.Pinned,
			&item.LastMessageAt, &item.CreatedAt, &item.UpdatedAt,
			&item.MessageCount, &item.LastMessageRole, &item.LastMessagePeek); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetConversation(ctx context.Context, customerID, id uuid.UUID) (*Conversation, error) {
	const q = `SELECT id, customer_id, user_id, assistant_id, title, pinned,
last_message_at, created_at, updated_at
FROM workspace_conversations
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	var c Conversation
	err := s.pool.QueryRow(ctx, q, customerID, id).Scan(&c.ID, &c.CustomerID,
		&c.UserID, &c.AssistantID, &c.Title, &c.Pinned, &c.LastMessageAt,
		&c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return &c, nil
}

func (s *Store) CreateConversation(ctx context.Context, c Conversation) (*Conversation, error) {
	const q = `INSERT INTO workspace_conversations
(customer_id, user_id, assistant_id, title)
VALUES ($1,$2,$3,$4)
RETURNING id, last_message_at, created_at, updated_at`
	if err := s.pool.QueryRow(ctx, q,
		c.CustomerID, c.UserID, c.AssistantID, c.Title,
	).Scan(&c.ID, &c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert conversation: %w", err)
	}
	return &c, nil
}

func (s *Store) RenameConversation(ctx context.Context, customerID, id uuid.UUID, title string) error {
	const q = `UPDATE workspace_conversations SET title = $3
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, customerID, id, title)
	if err != nil {
		return fmt.Errorf("rename conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateConversation patches title, pinned, and archived state in a
// single round-trip. Each field is independently optional. Archived
// flips archived_at to NOW() (true) or NULL (false). Allows operating
// on already-archived rows so the unarchive flow works.
func (s *Store) UpdateConversation(ctx context.Context, customerID, id uuid.UUID, p UpdateConversationRequest) error {
	const q = `UPDATE workspace_conversations SET
title       = COALESCE($3, title),
pinned      = COALESCE($4, pinned),
archived_at = CASE
    WHEN $5::bool IS NULL THEN archived_at
    WHEN $5::bool         THEN NOW()
    ELSE NULL
END
WHERE customer_id = $1 AND id = $2`
	tag, err := s.pool.Exec(ctx, q, customerID, id, p.Title, p.Pinned, p.Archived)
	if err != nil {
		return fmt.Errorf("update conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ConversationOwner returns the user_id who owns the given
// conversation, scoped to the customer. Returns ErrNotFound when
// the conversation doesn't exist or has been archived (treat
// archived as "gone" for access checks).
//
// Used by the handler's canAccessConversation to gate per-user
// visibility — members see own only, admins+viewers see all,
// writes are owner-only regardless of role.
func (s *Store) ConversationOwner(ctx context.Context, customerID, conversationID uuid.UUID) (string, error) {
	const q = `SELECT user_id FROM workspace_conversations
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	var owner string
	if err := s.pool.QueryRow(ctx, q, customerID, conversationID).Scan(&owner); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("conversation owner: %w", err)
	}
	return owner, nil
}

// DeleteFromMessage removes the target message and every message
// in the same conversation with a created_at greater than or equal
// to the target's. Powers chat regenerate (delete from last user
// onward, replay) and edit (replace user content, delete subsequent,
// replay). Returns ErrNotFound when the target doesn't exist.
//
// Idempotent within a conversation: if the target is already deleted
// the second call no-ops with ErrNotFound. Messages cascade-deleted
// here are gone — there is no soft-delete column on workspace_messages
// because the conversation row is the durable audit unit, not each
// individual message.
func (s *Store) DeleteFromMessage(ctx context.Context, customerID, conversationID, messageID uuid.UUID) error {
	const q = `WITH target AS (
    SELECT created_at FROM workspace_messages
    WHERE customer_id = $1 AND conversation_id = $2 AND id = $3
)
DELETE FROM workspace_messages
WHERE customer_id = $1
  AND conversation_id = $2
  AND created_at >= (SELECT created_at FROM target)`
	tag, err := s.pool.Exec(ctx, q, customerID, conversationID, messageID)
	if err != nil {
		return fmt.Errorf("delete from message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ArchiveConversation(ctx context.Context, customerID, id uuid.UUID) error {
	const q = `UPDATE workspace_conversations SET archived_at = NOW()
WHERE customer_id = $1 AND id = $2 AND archived_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, customerID, id)
	if err != nil {
		return fmt.Errorf("archive conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListMessages(ctx context.Context, customerID, conversationID uuid.UUID) ([]Message, error) {
	const q = `SELECT id, conversation_id, customer_id, role, content,
provider, model, prompt_tokens, completion_tokens, cost_cents,
finish_reason, error, metadata, created_at
FROM workspace_messages
WHERE customer_id = $1 AND conversation_id = $2
ORDER BY created_at ASC, id ASC`
	rows, err := s.pool.Query(ctx, q, customerID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.CustomerID,
			&m.Role, &m.Content, &m.Provider, &m.Model,
			&m.PromptTokens, &m.CompletionTokens, &m.CostCents,
			&m.FinishReason, &m.Error, &m.Metadata, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendMessage writes one message and bumps the conversation's
// last_message_at in a single transaction.
func (s *Store) AppendMessage(ctx context.Context, m Message) (*Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if len(m.Metadata) == 0 {
		m.Metadata = json.RawMessage(`{}`)
	}

	const ins = `INSERT INTO workspace_messages
(conversation_id, customer_id, role, content, provider, model,
prompt_tokens, completion_tokens, cost_cents, finish_reason, error, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING id, created_at`
	if err := tx.QueryRow(ctx, ins,
		m.ConversationID, m.CustomerID, m.Role, m.Content,
		m.Provider, m.Model, m.PromptTokens, m.CompletionTokens,
		m.CostCents, m.FinishReason, m.Error, m.Metadata,
	).Scan(&m.ID, &m.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE workspace_conversations SET last_message_at = NOW()
WHERE customer_id = $1 AND id = $2`, m.CustomerID, m.ConversationID); err != nil {
		return nil, fmt.Errorf("bump conversation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &m, nil
}

// =============================================================================
// Members + invitations
// =============================================================================

func (s *Store) ListMembers(ctx context.Context, customerID uuid.UUID) ([]Member, error) {
	const q = `SELECT customer_id, user_id, email, role, invited_by,
joined_at, last_seen_at, monthly_token_limit, daily_rate_limit
FROM workspace_members
WHERE customer_id = $1
ORDER BY joined_at ASC`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.CustomerID, &m.UserID, &m.Email, &m.Role,
			&m.InvitedBy, &m.JoinedAt, &m.LastSeenAt,
			&m.MonthlyTokenLimit, &m.DailyRateLimit); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// WriteAudit appends one row to workspace_audit_log. Snapshot fields
// (actor_email, actor_role, target_label) are captured by the caller
// at write time — see migration 028 for why we don't join to the
// live tables on read.
func (s *Store) WriteAudit(ctx context.Context, entry AuditEntry) error {
	const q = `INSERT INTO workspace_audit_log
(customer_id, actor_user_id, actor_email, actor_role, action,
target_type, target_id, target_label, metadata, ip_address, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11)`
	if _, err := s.pool.Exec(ctx, q,
		entry.CustomerID, entry.ActorUserID, entry.ActorEmail, entry.ActorRole,
		entry.Action, entry.TargetType, entry.TargetID, entry.TargetLabel,
		string(entry.Metadata), entry.IPAddress, entry.UserAgent,
	); err != nil {
		return fmt.Errorf("write audit: %w", err)
	}
	return nil
}

// ListAudit returns audit rows for the given customer in
// reverse-chronological order, capped at `limit` (max 200).
// `actionFilter` is empty = all actions. `before` is an audit ID;
// when non-zero, only rows older than that ID's created_at are
// returned (cursor pagination — stable across new writes).
func (s *Store) ListAudit(
	ctx context.Context,
	customerID uuid.UUID,
	actionFilter string,
	before uuid.UUID,
	limit int,
) ([]AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// Cursor: when `before` is non-zero, fetch its created_at and
	// use it as the upper bound. Done as a sub-select so the
	// caller doesn't have to round-trip first.
	q := `SELECT id, customer_id, actor_user_id, actor_email, actor_role,
action, target_type, target_id, target_label, metadata,
ip_address, user_agent, created_at
FROM workspace_audit_log
WHERE customer_id = $1`
	args := []any{customerID}
	if actionFilter != "" {
		q += ` AND action = $` + intToStr(len(args)+1)
		args = append(args, actionFilter)
	}
	if before != uuid.Nil {
		q += ` AND created_at < (SELECT created_at FROM workspace_audit_log WHERE id = $` +
			intToStr(len(args)+1) + `)`
		args = append(args, before)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT $` + intToStr(len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var meta []byte
		if err := rows.Scan(
			&e.ID, &e.CustomerID, &e.ActorUserID, &e.ActorEmail, &e.ActorRole,
			&e.Action, &e.TargetType, &e.TargetID, &e.TargetLabel, &meta,
			&e.IPAddress, &e.UserAgent, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		if len(meta) == 0 {
			meta = []byte(`{}`)
		}
		e.Metadata = meta
		out = append(out, e)
	}
	return out, rows.Err()
}

// intToStr is a tiny stringifier for SQL placeholder building. Avoids
// a strconv import bloat for one call site.
func intToStr(n int) string {
	if n < 10 {
		return string('0' + byte(n))
	}
	return strconvItoa(n)
}

func strconvItoa(n int) string {
	// Inline to dodge the strconv import (already imported elsewhere
	// in the package, but this file uses it sparingly).
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// GetMemberRole returns the role of one workspace member.
// Returns ErrNotFound when the user isn't a member of this customer.
// Used by the role-change handler before mutating: it must reject
// attempts to demote the owner without going through the transfer
// flow.
func (s *Store) GetMemberRole(ctx context.Context, customerID uuid.UUID, userID string) (string, error) {
	const q = `SELECT role FROM workspace_members
WHERE customer_id = $1 AND user_id = $2`
	var role string
	if err := s.pool.QueryRow(ctx, q, customerID, userID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get member role: %w", err)
	}
	return role, nil
}

// SetMemberRole writes a member's role. Caller is responsible for
// validating the input role string + applying business rules
// (e.g. don't demote the owner here — the handler enforces that
// before calling).
func (s *Store) SetMemberRole(ctx context.Context, customerID uuid.UUID, userID, role string) error {
	const q = `UPDATE workspace_members SET role = $3
WHERE customer_id = $1 AND user_id = $2`
	tag, err := s.pool.Exec(ctx, q, customerID, userID, role)
	if err != nil {
		return fmt.Errorf("set member role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TransferOwnership atomically demotes the current owner to admin
// and promotes the target to owner. Single transaction so the
// workspace is never ownerless and never has two owners. Returns
// ErrNotFound when the target user isn't a workspace_member.
//
// Pre-conditions checked in SQL (not Go): both the current owner
// row and the target row must exist. If either is missing, the
// COMMIT rolls back and the caller sees ErrNotFound.
func (s *Store) TransferOwnership(ctx context.Context, customerID uuid.UUID, currentOwnerUserID, newOwnerUserID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transfer: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Verify the target exists in this workspace before touching
	// roles. Otherwise we'd demote the existing owner and end up
	// with no owner if the target user_id was a typo.
	const exists = `SELECT EXISTS (
SELECT 1 FROM workspace_members
WHERE customer_id = $1 AND user_id = $2
)`
	var ok bool
	if err := tx.QueryRow(ctx, exists, customerID, newOwnerUserID).Scan(&ok); err != nil {
		return fmt.Errorf("check target membership: %w", err)
	}
	if !ok {
		return ErrNotFound
	}

	// Demote current owner. Use the user_id we trust from the
	// session, not a body-supplied value, so a malicious caller
	// can't ask us to demote someone else.
	if _, err := tx.Exec(ctx,
		`UPDATE workspace_members SET role = 'admin'
WHERE customer_id = $1 AND user_id = $2 AND role = 'owner'`,
		customerID, currentOwnerUserID,
	); err != nil {
		return fmt.Errorf("demote owner: %w", err)
	}

	// Promote target.
	if _, err := tx.Exec(ctx,
		`UPDATE workspace_members SET role = 'owner'
WHERE customer_id = $1 AND user_id = $2`,
		customerID, newOwnerUserID,
	); err != nil {
		return fmt.Errorf("promote new owner: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transfer: %w", err)
	}
	return nil
}

// GetMemberBudgets returns the monthly token limit and daily rate
// limit for a single user. NULL columns surface as nil pointers.
// Returns nil, nil for customers/users that have no workspace_members
// row (e.g. the customer's owner who signed up directly without
// being invited) — the caller treats this as "no budget enforcement
// for this user".
func (s *Store) GetMemberBudgets(ctx context.Context, customerID uuid.UUID, userID string) (monthlyTokenLimit *int, dailyRateLimit *int, err error) {
	const q = `SELECT monthly_token_limit, daily_rate_limit
FROM workspace_members
WHERE customer_id = $1 AND user_id = $2`
	if err := s.pool.QueryRow(ctx, q, customerID, userID).Scan(&monthlyTokenLimit, &dailyRateLimit); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("get member budgets: %w", err)
	}
	return monthlyTokenLimit, dailyRateLimit, nil
}

// SetMemberBudgets updates a member's budget caps. Pass nil to clear
// (no limit). Errors with ErrNotFound when the member doesn't exist.
func (s *Store) SetMemberBudgets(ctx context.Context, customerID uuid.UUID, userID string, monthlyTokenLimit, dailyRateLimit *int) error {
	const q = `UPDATE workspace_members
SET monthly_token_limit = $3, daily_rate_limit = $4
WHERE customer_id = $1 AND user_id = $2`
	tag, err := s.pool.Exec(ctx, q, customerID, userID, monthlyTokenLimit, dailyRateLimit)
	if err != nil {
		return fmt.Errorf("set member budgets: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MemberUsage counts a user's tokens spent in two windows:
//
//   monthlyTokens — sum of prompt_tokens + completion_tokens in the
//   current calendar month (UTC). Resets on the 1st.
//
//   last24hMessages — count of assistant-role messages in the
//   rolling last 24 hours. Used as the daily rate-limit signal.
//
// Both queries hit workspace_messages with the customer + user
// scope, so per-user usage is real even when budgets are off.
func (s *Store) MemberUsage(ctx context.Context, customerID uuid.UUID, userID string) (monthlyTokens int, last24hMessages int, err error) {
	const q = `WITH user_msgs AS (
    SELECT m.prompt_tokens, m.completion_tokens, m.role, m.created_at
    FROM workspace_messages m
    JOIN workspace_conversations c ON c.id = m.conversation_id
    WHERE m.customer_id = $1
      AND c.customer_id = $1
      AND c.user_id     = $2
)
SELECT
    COALESCE(SUM(prompt_tokens + completion_tokens) FILTER (
        WHERE created_at >= date_trunc('month', NOW())
    ), 0)::INT,
    COUNT(*) FILTER (
        WHERE role = 'assistant' AND created_at >= NOW() - INTERVAL '24 hours'
    )::INT
FROM user_msgs`
	if err := s.pool.QueryRow(ctx, q, customerID, userID).Scan(&monthlyTokens, &last24hMessages); err != nil {
		return 0, 0, fmt.Errorf("member usage: %w", err)
	}
	return monthlyTokens, last24hMessages, nil
}

func (s *Store) ListInvitations(ctx context.Context, customerID uuid.UUID) ([]Invitation, error) {
	const q = `SELECT id, customer_id, email, role, invited_by, expires_at,
accepted_at, revoked_at, created_at
FROM workspace_invitations
WHERE customer_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL
ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var inv Invitation
		if err := rows.Scan(&inv.ID, &inv.CustomerID, &inv.Email, &inv.Role,
			&inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt, &inv.RevokedAt,
			&inv.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// CountConsumedSeats returns the number of seats the customer is
// currently using and the workspace_settings.seat_limit they're
// allowed. "Consumed" is active members + pending (unaccepted +
// unrevoked + unexpired) invitations — pending invites count to
// prevent the inviter from issuing N invitations beyond the cap and
// having them all accepted later.
//
// When workspace_settings has no row for this customer, the limit
// falls back to 5 (matches the schema default and the analytics
// summary's COALESCE). The cloud billing webhook upserts a row
// reflecting the Stripe seat_count after every subscription event.
func (s *Store) CountConsumedSeats(ctx context.Context, customerID uuid.UUID) (consumed, limit int, err error) {
	const q = `WITH s AS (
  SELECT seat_limit FROM workspace_settings WHERE customer_id = $1
)
SELECT
  (SELECT COUNT(*) FROM workspace_members WHERE customer_id = $1)
  + (SELECT COUNT(*) FROM workspace_invitations
     WHERE customer_id = $1
       AND accepted_at IS NULL
       AND revoked_at IS NULL
       AND expires_at > NOW()) AS consumed,
  COALESCE((SELECT seat_limit FROM s), 5) AS seat_limit`
	if err := s.pool.QueryRow(ctx, q, customerID).Scan(&consumed, &limit); err != nil {
		return 0, 0, fmt.Errorf("count consumed seats: %w", err)
	}
	return consumed, limit, nil
}

// CreateInvitation issues a token (returned only here, never read back),
// stores its SHA-256 hash, and returns the invitation row.
func (s *Store) CreateInvitation(ctx context.Context, customerID uuid.UUID, email, role string, invitedBy *string, ttl time.Duration, rawToken string) (*Invitation, error) {
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])
	expires := time.Now().Add(ttl).UTC()
	const q = `INSERT INTO workspace_invitations
(customer_id, email, role, token_hash, invited_by, expires_at)
VALUES ($1,$2,$3,$4,$5,$6)
RETURNING id, created_at`
	var inv Invitation
	inv.CustomerID = customerID
	inv.Email = email
	inv.Role = role
	inv.InvitedBy = invitedBy
	inv.ExpiresAt = expires
	if err := s.pool.QueryRow(ctx, q, customerID, email, role, tokenHash,
		invitedBy, expires).Scan(&inv.ID, &inv.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert invitation: %w", err)
	}
	return &inv, nil
}

// Sentinel errors surfaced by AcceptInvitation. The handler maps each
// to a stable structured-error code so the dashboard can render
// precise user-facing copy.
var (
	ErrInvitationExpired   = errors.New("workspace: invitation expired")
	ErrInvitationRevoked   = errors.New("workspace: invitation revoked")
	ErrInvitationConsumed  = errors.New("workspace: invitation already accepted")
	ErrInvitationEmailMismatch = errors.New("workspace: invitation issued to a different email")
	ErrSeatLimitReached    = errors.New("workspace: seat limit reached")
)

// AcceptResult is what AcceptInvitation returns on success — enough
// for the handler to redirect the user into the right workspace and
// for the dashboard to confirm the role.
type AcceptResult struct {
	CustomerID uuid.UUID `json:"customer_id"`
	Role       string    `json:"role"`
	Email      string    `json:"email"`
}

// AcceptInvitation atomically binds a signed-in user to the inviter's
// workspace_members. Single transaction:
//
//  1. Lock the matching workspace_invitations row by token_hash.
//  2. Validate state — not expired, not revoked, not already
//     consumed, and the invitation's email matches the signed-in
//     user's email (case-insensitive). Email mismatch is the only
//     security check beyond the bearer token; without it, anyone who
//     intercepted the email could redeem the invite under their own
//     account.
//  3. Re-check seat limit. The issue-time check in createInvitation
//     could be stale by acceptance time (downgrades, other invites
//     accepted in parallel). The re-check shares the same predicate
//     so seat-limited acceptance fails the same way invite-creation
//     would.
//  4. INSERT workspace_members ON CONFLICT DO NOTHING — handles the
//     double-click case where the user's browser fires accept twice.
//  5. UPDATE the invitation's accepted_at.
//
// Sentinel errors surface the exact failure mode; handler maps them
// to structured-error codes.
func (s *Store) AcceptInvitation(ctx context.Context, rawToken, userID, userEmail string) (*AcceptResult, error) {
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const lookupQ = `SELECT id, customer_id, email, role, invited_by,
expires_at, accepted_at, revoked_at
FROM workspace_invitations
WHERE token_hash = $1
FOR UPDATE`
	var inv Invitation
	if err := tx.QueryRow(ctx, lookupQ, tokenHash).Scan(
		&inv.ID, &inv.CustomerID, &inv.Email, &inv.Role, &inv.InvitedBy,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.RevokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lookup invitation: %w", err)
	}

	switch {
	case inv.RevokedAt != nil:
		return nil, ErrInvitationRevoked
	case inv.AcceptedAt != nil:
		return nil, ErrInvitationConsumed
	case time.Now().After(inv.ExpiresAt):
		return nil, ErrInvitationExpired
	}

	normalizedInv := strings.ToLower(strings.TrimSpace(inv.Email))
	normalizedUser := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedInv != normalizedUser {
		return nil, ErrInvitationEmailMismatch
	}

	// Re-check the seat predicate against the inviter's customer.
	// Same shape as CountConsumedSeats but inside the transaction.
	const seatQ = `WITH s AS (
  SELECT seat_limit FROM workspace_settings WHERE customer_id = $1
)
SELECT
  (SELECT COUNT(*) FROM workspace_members WHERE customer_id = $1)
  + (SELECT COUNT(*) FROM workspace_invitations
     WHERE customer_id = $1
       AND accepted_at IS NULL
       AND revoked_at IS NULL
       AND expires_at > NOW()
       AND id <> $2) AS consumed,
  COALESCE((SELECT seat_limit FROM s), 5) AS seat_limit`
	var consumed, limit int
	if err := tx.QueryRow(ctx, seatQ, inv.CustomerID, inv.ID).Scan(&consumed, &limit); err != nil {
		return nil, fmt.Errorf("recheck seats: %w", err)
	}
	if consumed >= limit {
		return nil, ErrSeatLimitReached
	}

	const insertQ = `INSERT INTO workspace_members
(customer_id, user_id, email, role, invited_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (customer_id, user_id) DO NOTHING`
	if _, err := tx.Exec(ctx, insertQ,
		inv.CustomerID, userID, normalizedUser, inv.Role, inv.InvitedBy,
	); err != nil {
		return nil, fmt.Errorf("insert member: %w", err)
	}

	const markQ = `UPDATE workspace_invitations SET accepted_at = NOW()
WHERE id = $1 AND accepted_at IS NULL`
	if _, err := tx.Exec(ctx, markQ, inv.ID); err != nil {
		return nil, fmt.Errorf("mark accepted: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &AcceptResult{
		CustomerID: inv.CustomerID,
		Role:       inv.Role,
		Email:      normalizedUser,
	}, nil
}

func (s *Store) RevokeInvitation(ctx context.Context, customerID, id uuid.UUID) error {
	const q = `UPDATE workspace_invitations SET revoked_at = NOW()
WHERE customer_id = $1 AND id = $2 AND accepted_at IS NULL AND revoked_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, customerID, id)
	if err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RemoveMember deletes a member row outright. Owner role cannot be removed.
func (s *Store) RemoveMember(ctx context.Context, customerID uuid.UUID, userID string) error {
	const q = `DELETE FROM workspace_members
WHERE customer_id = $1 AND user_id = $2 AND role <> 'owner'`
	tag, err := s.pool.Exec(ctx, q, customerID, userID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// =============================================================================
// Analytics
// =============================================================================

// AnalyticsSummary is the dashboard KPI block.
type AnalyticsSummary struct {
	MessagesThisMonth int    `json:"messages_this_month"`
	TokensThisMonth   int    `json:"tokens_this_month"`
	CostCentsThisMonth int   `json:"cost_cents_this_month"`
	ActiveUsers       int    `json:"active_users"`
	SeatLimit         int    `json:"seat_limit"`
}

// ByModelCount is one row in the "Usage by model" chart.
type ByModelCount struct {
	Model string `json:"model"`
	Count int    `json:"count"`
}

// DailyUsagePoint is one row in the 14-day usage chart.
type DailyUsagePoint struct {
	Day      time.Time `json:"day"`
	Messages int       `json:"messages"`
	Tokens   int       `json:"tokens"`
}

func (s *Store) AnalyticsSummary(ctx context.Context, customerID uuid.UUID) (*AnalyticsSummary, error) {
	const q = `WITH this_month AS (
  SELECT
    COUNT(*) AS msgs,
    COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS tokens,
    COALESCE(SUM(cost_cents), 0) AS cost_cents
  FROM workspace_messages
  WHERE customer_id = $1
    AND role = 'assistant'
    AND created_at >= date_trunc('month', NOW())
), seats AS (
  SELECT seat_limit FROM workspace_settings WHERE customer_id = $1
), active AS (
  SELECT COUNT(DISTINCT user_id) AS n
  FROM workspace_conversations
  WHERE customer_id = $1
    AND last_message_at >= NOW() - INTERVAL '30 days'
)
SELECT
  COALESCE((SELECT msgs FROM this_month), 0),
  COALESCE((SELECT tokens FROM this_month), 0),
  COALESCE((SELECT cost_cents FROM this_month), 0),
  COALESCE((SELECT n FROM active), 0),
  COALESCE((SELECT seat_limit FROM seats), 5)`
	var sum AnalyticsSummary
	if err := s.pool.QueryRow(ctx, q, customerID).Scan(
		&sum.MessagesThisMonth, &sum.TokensThisMonth,
		&sum.CostCentsThisMonth, &sum.ActiveUsers, &sum.SeatLimit,
	); err != nil {
		return nil, fmt.Errorf("analytics summary: %w", err)
	}
	return &sum, nil
}

func (s *Store) AnalyticsDaily(ctx context.Context, customerID uuid.UUID, days int) ([]DailyUsagePoint, error) {
	if days <= 0 || days > 90 {
		days = 14
	}
	const q = `SELECT
  date_trunc('day', created_at) AS day,
  COUNT(*) FILTER (WHERE role = 'assistant') AS msgs,
  COALESCE(SUM(prompt_tokens + completion_tokens) FILTER (WHERE role = 'assistant'), 0) AS tokens
FROM workspace_messages
WHERE customer_id = $1 AND created_at >= NOW() - ($2 || ' days')::INTERVAL
GROUP BY 1
ORDER BY 1 ASC`
	rows, err := s.pool.Query(ctx, q, customerID, days)
	if err != nil {
		return nil, fmt.Errorf("analytics daily: %w", err)
	}
	defer rows.Close()
	out := []DailyUsagePoint{}
	for rows.Next() {
		var p DailyUsagePoint
		if err := rows.Scan(&p.Day, &p.Messages, &p.Tokens); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) AnalyticsByModel(ctx context.Context, customerID uuid.UUID) ([]ByModelCount, error) {
	const q = `SELECT COALESCE(model, 'unknown') AS m, COUNT(*) AS n
FROM workspace_messages
WHERE customer_id = $1
  AND role = 'assistant'
  AND created_at >= date_trunc('month', NOW())
GROUP BY 1
ORDER BY n DESC
LIMIT 10`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("analytics by model: %w", err)
	}
	defer rows.Close()
	out := []ByModelCount{}
	for rows.Next() {
		var r ByModelCount
		if err := rows.Scan(&r.Model, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// =============================================================================
// Per-user drill-down — top-N, forecast, week-over-week, user detail.
// All computed on the fly from workspace_messages joined to
// workspace_conversations. No rollup table for v1; if these queries
// get hot, add a workspace_usage_daily materialized view.
// =============================================================================

// TopUserUsage is one row in the top-N users table.
type TopUserUsage struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email,omitempty"`
	Messages  int    `json:"messages"`
	Tokens    int    `json:"tokens"`
	CostCents int    `json:"cost_cents"`
}

// AnalyticsTopUsers returns the top N users by cost in the current
// calendar month. Email is joined from workspace_members so the
// dashboard can show a name alongside the metric. Users without a
// member row (e.g. early customer-owners) still appear with an
// empty email — drill-down should still work via user_id.
func (s *Store) AnalyticsTopUsers(ctx context.Context, customerID uuid.UUID, limit int) ([]TopUserUsage, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	const q = `SELECT
  c.user_id,
  COALESCE(m.email, '') AS email,
  COUNT(*) FILTER (WHERE msg.role = 'assistant') AS messages,
  COALESCE(SUM(msg.prompt_tokens + msg.completion_tokens) FILTER (WHERE msg.role = 'assistant'), 0)::INT AS tokens,
  COALESCE(SUM(msg.cost_cents) FILTER (WHERE msg.role = 'assistant'), 0)::INT AS cost_cents
FROM workspace_messages msg
JOIN workspace_conversations c ON c.id = msg.conversation_id AND c.customer_id = msg.customer_id
LEFT JOIN workspace_members m ON m.customer_id = c.customer_id AND m.user_id = c.user_id
WHERE msg.customer_id = $1
  AND msg.created_at >= date_trunc('month', NOW())
GROUP BY c.user_id, m.email
HAVING COUNT(*) FILTER (WHERE msg.role = 'assistant') > 0
ORDER BY cost_cents DESC, tokens DESC
LIMIT $2`
	rows, err := s.pool.Query(ctx, q, customerID, limit)
	if err != nil {
		return nil, fmt.Errorf("analytics top users: %w", err)
	}
	defer rows.Close()
	out := []TopUserUsage{}
	for rows.Next() {
		var u TopUserUsage
		if err := rows.Scan(&u.UserID, &u.Email, &u.Messages, &u.Tokens, &u.CostCents); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ForecastResult is the "current → daily-avg → projected" answer.
// All cost numbers are in cents.
type ForecastResult struct {
	CurrentCents       int     `json:"current_cents"`
	DaysElapsed        int     `json:"days_elapsed"`
	DaysInMonth        int     `json:"days_in_month"`
	DailyAverageCents  float64 `json:"daily_average_cents"`
	ProjectedCents     int     `json:"projected_cents"`
	LastMonthCents     int     `json:"last_month_cents"`
	// DeltaPctVsLastMonth is positive when projected > last-month total.
	// Useful for "are we on pace?" callouts.
	DeltaPctVsLastMonth float64 `json:"delta_pct_vs_last_month"`
}

// AnalyticsForecast returns the spend forecast for the current calendar
// month plus a delta vs the prior month total. Self-contained math —
// no external pricing service involved.
func (s *Store) AnalyticsForecast(ctx context.Context, customerID uuid.UUID) (*ForecastResult, error) {
	const q = `WITH cur AS (
    SELECT COALESCE(SUM(cost_cents) FILTER (WHERE role = 'assistant'), 0)::INT AS spend
    FROM workspace_messages
    WHERE customer_id = $1 AND created_at >= date_trunc('month', NOW())
), prev AS (
    SELECT COALESCE(SUM(cost_cents) FILTER (WHERE role = 'assistant'), 0)::INT AS spend
    FROM workspace_messages
    WHERE customer_id = $1
      AND created_at >= date_trunc('month', NOW()) - INTERVAL '1 month'
      AND created_at <  date_trunc('month', NOW())
)
SELECT
    (SELECT spend FROM cur),
    EXTRACT(DAY FROM NOW())::INT,
    EXTRACT(DAY FROM (date_trunc('month', NOW()) + INTERVAL '1 month - 1 day'))::INT,
    (SELECT spend FROM prev)`
	var f ForecastResult
	var prevCents int
	if err := s.pool.QueryRow(ctx, q, customerID).Scan(
		&f.CurrentCents, &f.DaysElapsed, &f.DaysInMonth, &prevCents,
	); err != nil {
		return nil, fmt.Errorf("analytics forecast: %w", err)
	}
	if f.DaysElapsed > 0 {
		f.DailyAverageCents = float64(f.CurrentCents) / float64(f.DaysElapsed)
	}
	f.ProjectedCents = int(f.DailyAverageCents * float64(f.DaysInMonth))
	f.LastMonthCents = prevCents
	if prevCents > 0 {
		f.DeltaPctVsLastMonth = (float64(f.ProjectedCents) - float64(prevCents)) / float64(prevCents) * 100
	}
	return &f, nil
}

// PeriodStats is one half of a week-over-week comparison.
type PeriodStats struct {
	Messages      int `json:"messages"`
	Tokens        int `json:"tokens"`
	CostCents     int `json:"cost_cents"`
	ActiveUsers   int `json:"active_users"`
	Conversations int `json:"conversations"`
}

// CompareResult is the WoW response: this_week + last_week side by
// side. The dashboard renders deltas client-side.
type CompareResult struct {
	ThisWeek PeriodStats `json:"this_week"`
	LastWeek PeriodStats `json:"last_week"`
}

// AnalyticsCompare returns this-week-vs-last-week stats. "Week" =
// rolling 7 days from NOW (this) and the 7 days before that (last).
// Calendar-week boundaries are deliberately avoided so the answer is
// stable regardless of when the user opens the page.
func (s *Store) AnalyticsCompare(ctx context.Context, customerID uuid.UUID) (*CompareResult, error) {
	const q = `WITH params AS (
    SELECT
        NOW() - INTERVAL '7 days'  AS this_start,
        NOW()                      AS this_end,
        NOW() - INTERVAL '14 days' AS last_start,
        NOW() - INTERVAL '7 days'  AS last_end
), this_week AS (
    SELECT
        COUNT(*) FILTER (WHERE msg.role = 'assistant')                                   AS messages,
        COALESCE(SUM(msg.prompt_tokens + msg.completion_tokens) FILTER (WHERE msg.role = 'assistant'), 0)::INT AS tokens,
        COALESCE(SUM(msg.cost_cents) FILTER (WHERE msg.role = 'assistant'), 0)::INT     AS cost_cents,
        COUNT(DISTINCT c.user_id)                                                        AS active_users,
        COUNT(DISTINCT c.id)                                                             AS conversations
    FROM workspace_messages msg
    JOIN workspace_conversations c ON c.id = msg.conversation_id
    WHERE msg.customer_id = $1
      AND msg.created_at >= (SELECT this_start FROM params)
      AND msg.created_at <  (SELECT this_end   FROM params)
), last_week AS (
    SELECT
        COUNT(*) FILTER (WHERE msg.role = 'assistant')                                   AS messages,
        COALESCE(SUM(msg.prompt_tokens + msg.completion_tokens) FILTER (WHERE msg.role = 'assistant'), 0)::INT AS tokens,
        COALESCE(SUM(msg.cost_cents) FILTER (WHERE msg.role = 'assistant'), 0)::INT     AS cost_cents,
        COUNT(DISTINCT c.user_id)                                                        AS active_users,
        COUNT(DISTINCT c.id)                                                             AS conversations
    FROM workspace_messages msg
    JOIN workspace_conversations c ON c.id = msg.conversation_id
    WHERE msg.customer_id = $1
      AND msg.created_at >= (SELECT last_start FROM params)
      AND msg.created_at <  (SELECT last_end   FROM params)
)
SELECT
    COALESCE((SELECT messages      FROM this_week), 0),
    COALESCE((SELECT tokens        FROM this_week), 0),
    COALESCE((SELECT cost_cents    FROM this_week), 0),
    COALESCE((SELECT active_users  FROM this_week), 0),
    COALESCE((SELECT conversations FROM this_week), 0),
    COALESCE((SELECT messages      FROM last_week), 0),
    COALESCE((SELECT tokens        FROM last_week), 0),
    COALESCE((SELECT cost_cents    FROM last_week), 0),
    COALESCE((SELECT active_users  FROM last_week), 0),
    COALESCE((SELECT conversations FROM last_week), 0)`
	var c CompareResult
	if err := s.pool.QueryRow(ctx, q, customerID).Scan(
		&c.ThisWeek.Messages, &c.ThisWeek.Tokens, &c.ThisWeek.CostCents,
		&c.ThisWeek.ActiveUsers, &c.ThisWeek.Conversations,
		&c.LastWeek.Messages, &c.LastWeek.Tokens, &c.LastWeek.CostCents,
		&c.LastWeek.ActiveUsers, &c.LastWeek.Conversations,
	); err != nil {
		return nil, fmt.Errorf("analytics compare: %w", err)
	}
	return &c, nil
}

// UserAssistantUsage is one row in a user's "top assistants" list.
type UserAssistantUsage struct {
	AssistantID   uuid.UUID `json:"assistant_id"`
	AssistantName string    `json:"assistant_name"`
	Messages      int       `json:"messages"`
	Tokens        int       `json:"tokens"`
}

// UserAnalyticsDetail is the per-user drill-down payload.
type UserAnalyticsDetail struct {
	UserID         string               `json:"user_id"`
	Email          string               `json:"email,omitempty"`
	TotalMessages  int                  `json:"total_messages"`
	TotalTokens    int                  `json:"total_tokens"`
	TotalCostCents int                  `json:"total_cost_cents"`
	Daily          []DailyUsagePoint    `json:"daily"`
	TopAssistants  []UserAssistantUsage `json:"top_assistants"`
}

// AnalyticsUserDetail returns one user's last-30-day usage:
// totals + daily series + top 3 assistants used. The daily series
// shares DailyUsagePoint with the workspace-wide chart so the
// dashboard reuses the same chart component.
func (s *Store) AnalyticsUserDetail(ctx context.Context, customerID uuid.UUID, userID string) (*UserAnalyticsDetail, error) {
	d := &UserAnalyticsDetail{UserID: userID, Daily: []DailyUsagePoint{}, TopAssistants: []UserAssistantUsage{}}
	const totalsQ = `SELECT
    COALESCE(m.email, ''),
    COUNT(*) FILTER (WHERE msg.role = 'assistant'),
    COALESCE(SUM(msg.prompt_tokens + msg.completion_tokens) FILTER (WHERE msg.role = 'assistant'), 0)::INT,
    COALESCE(SUM(msg.cost_cents) FILTER (WHERE msg.role = 'assistant'), 0)::INT
FROM workspace_conversations c
LEFT JOIN workspace_messages msg ON msg.conversation_id = c.id
LEFT JOIN workspace_members m ON m.customer_id = c.customer_id AND m.user_id = c.user_id
WHERE c.customer_id = $1 AND c.user_id = $2
  AND (msg.created_at IS NULL OR msg.created_at >= NOW() - INTERVAL '30 days')
GROUP BY m.email`
	if err := s.pool.QueryRow(ctx, totalsQ, customerID, userID).Scan(
		&d.Email, &d.TotalMessages, &d.TotalTokens, &d.TotalCostCents,
	); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("analytics user totals: %w", err)
	}

	const dailyQ = `SELECT
  date_trunc('day', msg.created_at) AS day,
  COUNT(*) FILTER (WHERE msg.role = 'assistant') AS msgs,
  COALESCE(SUM(msg.prompt_tokens + msg.completion_tokens) FILTER (WHERE msg.role = 'assistant'), 0)::INT AS tokens
FROM workspace_messages msg
JOIN workspace_conversations c ON c.id = msg.conversation_id
WHERE msg.customer_id = $1 AND c.user_id = $2
  AND msg.created_at >= NOW() - INTERVAL '30 days'
GROUP BY 1
ORDER BY 1 ASC`
	rows, err := s.pool.Query(ctx, dailyQ, customerID, userID)
	if err != nil {
		return nil, fmt.Errorf("analytics user daily: %w", err)
	}
	for rows.Next() {
		var p DailyUsagePoint
		if err := rows.Scan(&p.Day, &p.Messages, &p.Tokens); err != nil {
			rows.Close()
			return nil, err
		}
		d.Daily = append(d.Daily, p)
	}
	rows.Close()

	const topQ = `SELECT
  c.assistant_id,
  COALESCE(a.name, '(deleted)') AS name,
  COUNT(*) FILTER (WHERE msg.role = 'assistant') AS msgs,
  COALESCE(SUM(msg.prompt_tokens + msg.completion_tokens) FILTER (WHERE msg.role = 'assistant'), 0)::INT AS tokens
FROM workspace_conversations c
JOIN workspace_messages msg ON msg.conversation_id = c.id
LEFT JOIN workspace_assistants a ON a.id = c.assistant_id
WHERE c.customer_id = $1 AND c.user_id = $2
  AND c.assistant_id IS NOT NULL
  AND msg.created_at >= NOW() - INTERVAL '30 days'
GROUP BY c.assistant_id, a.name
ORDER BY msgs DESC
LIMIT 3`
	rows, err = s.pool.Query(ctx, topQ, customerID, userID)
	if err != nil {
		return nil, fmt.Errorf("analytics user top assistants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var u UserAssistantUsage
		var aid *uuid.UUID
		if err := rows.Scan(&aid, &u.AssistantName, &u.Messages, &u.Tokens); err != nil {
			return nil, err
		}
		if aid != nil {
			u.AssistantID = *aid
		}
		d.TopAssistants = append(d.TopAssistants, u)
	}
	return d, rows.Err()
}
