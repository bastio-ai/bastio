package audit

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a token doesn't match any pending audit.
var ErrNotFound = errors.New("audit: not found")

// Store wraps the pgxpool with the queries the audit service needs.
// Notably, audit creates rows in three tables (customers,
// pending_audits, governance_installations) inside a single
// transaction — the convenience method Provision encapsulates that.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ProvisionResult is what Provision returns: enough to render the
// activation + bundle URLs and emit the bundle blob downstream.
type ProvisionResult struct {
	AuditID         uuid.UUID
	CustomerID      uuid.UUID
	InstallationID  uuid.UUID
	OrgID           uuid.UUID
	ClaimToken      string // raw, never persisted
	BundleToken     string // raw, never persisted
	ExpiresAt       time.Time
	BundleExpiresAt time.Time
}

// Provision atomically creates a placeholder customer, a
// pending_audits row, and a governance_installations row. Returns the
// generated tokens so the caller can build the bundle and email.
//
// Slug shape: <company-slug>-<random6>. Falls back to "audit-<random>"
// when company is empty. Slug uniqueness is enforced by the customers
// table; collisions retry up to 3 times.
func (s *Store) Provision(ctx context.Context, req StartRequest, cfg Config, ingestKey string) (*ProvisionResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Slug — try a few candidates if collision.
	companyForSlug := strings.TrimSpace(req.CompanyName)
	if companyForSlug == "" {
		companyForSlug = "audit"
	}
	displayName := strings.TrimSpace(req.CompanyName)
	if displayName == "" {
		displayName = "Pending audit"
	}

	var customerID uuid.UUID
	const customerInsert = `INSERT INTO customers (name, slug)
VALUES ($1, $2)
RETURNING id`
	for attempt := range 3 {
		slug := suggestedSlug(companyForSlug)
		err := tx.QueryRow(ctx, customerInsert, displayName, slug).Scan(&customerID)
		if err == nil {
			break
		}
		if attempt == 2 {
			return nil, fmt.Errorf("create customer: %w", err)
		}
		// retry on slug collision
	}

	// Tokens.
	claimToken, err := generateRawToken(32)
	if err != nil {
		return nil, fmt.Errorf("claim token: %w", err)
	}
	bundleToken, err := generateRawToken(32)
	if err != nil {
		return nil, fmt.Errorf("bundle token: %w", err)
	}
	now := time.Now().UTC()
	claimExpires := now.Add(cfg.ClaimTokenTTL)

	mdmFormat := strings.TrimSpace(req.MDMFormat)
	if mdmFormat == "" {
		mdmFormat = "chrome"
	}

	var auditID uuid.UUID
	const auditInsert = `INSERT INTO pending_audits
(customer_id, claim_token_hash, bundle_token_hash, contact_email,
 contact_name, company_name, mdm_format, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id`
	if err := tx.QueryRow(ctx, auditInsert,
		customerID,
		hashToken(claimToken),
		hashToken(bundleToken),
		strings.ToLower(strings.TrimSpace(req.ContactEmail)),
		strings.TrimSpace(req.ContactName),
		strings.TrimSpace(req.CompanyName),
		mdmFormat,
		claimExpires,
	).Scan(&auditID); err != nil {
		return nil, fmt.Errorf("insert pending audit: %w", err)
	}

	// Link the customer to the audit so post-claim you can trace the
	// origin. Sparse-indexed.
	if _, err := tx.Exec(ctx,
		`UPDATE customers SET pending_audit_id = $2 WHERE id = $1`,
		customerID, auditID); err != nil {
		return nil, fmt.Errorf("link customer pending_audit: %w", err)
	}

	// governance_installations row — gives the bundle generator
	// somewhere to attach. Use a fresh org_id that the bundle config
	// will reference. Token hash is irrelevant for our purposes (the
	// extension uses HMAC at runtime, this row primarily exists so
	// the bundle generator has an installation to template against).
	orgID := uuid.New()
	const installInsert = `INSERT INTO governance_installations
(customer_id, org_id, installation_token_hash, installation_secret)
VALUES ($1, $2, $3, $4)
RETURNING id`
	var installationID uuid.UUID
	if err := tx.QueryRow(ctx, installInsert,
		customerID, orgID, hashToken(ingestKey), ingestKey,
	).Scan(&installationID); err != nil {
		return nil, fmt.Errorf("insert installation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &ProvisionResult{
		AuditID:         auditID,
		CustomerID:      customerID,
		InstallationID:  installationID,
		OrgID:           orgID,
		ClaimToken:      claimToken,
		BundleToken:     bundleToken,
		ExpiresAt:       claimExpires,
		BundleExpiresAt: now.Add(cfg.BundleTokenTTL),
	}, nil
}

// PendingAuditByBundleToken returns the audit whose bundle_token_hash
// matches. Used by the bundle download endpoint. Returns ErrNotFound
// for unknown tokens; expired or already-consumed audits are surfaced
// as the row + a status the caller checks.
func (s *Store) PendingAuditByBundleToken(ctx context.Context, rawToken string) (*PendingAudit, error) {
	const q = `SELECT id, customer_id, contact_email, contact_name,
company_name, mdm_format, bundle_used_at, expires_at, claimed_at,
claimed_by_user_id, created_at
FROM pending_audits WHERE bundle_token_hash = $1`
	var a PendingAudit
	err := s.pool.QueryRow(ctx, q, hashToken(rawToken)).Scan(
		&a.ID, &a.CustomerID, &a.ContactEmail, &a.ContactName,
		&a.CompanyName, &a.MDMFormat, &a.BundleUsedAt,
		&a.ExpiresAt, &a.ClaimedAt, &a.ClaimedByUserID, &a.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup pending audit: %w", err)
	}
	return &a, nil
}

// MarkBundleDownloaded is a one-shot guard. Returns true on the first
// call (race-safe via the WHERE bundle_used_at IS NULL clause); false
// or error on subsequent calls so the bundle handler can 410.
func (s *Store) MarkBundleDownloaded(ctx context.Context, auditID uuid.UUID) (bool, error) {
	const q = `UPDATE pending_audits SET bundle_used_at = NOW()
WHERE id = $1 AND bundle_used_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, auditID)
	if err != nil {
		return false, fmt.Errorf("mark bundle used: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// RotateResult is what RotateClaimToken* returns. The raw token is
// included exactly once and is the input the activation URL embeds —
// the only persisted form is the hash.
type RotateResult struct {
	AuditID     uuid.UUID
	ContactName string
	ClaimToken  string // raw, never persisted
}

// ResendCooldown is the minimum time between two /audit/resend rotations
// on the same audit. Prevents inbox-spam by replaying the resend
// endpoint. Sweep-driven rotations (RotateClaimTokenByID) bypass this.
const ResendCooldown = 60 * time.Second

// RotateClaimTokenByEmail finds the most recent unclaimed unexpired
// pending audit for `contactEmail`, generates a fresh claim token,
// updates the row's claim_token_hash + last_resend_at, and returns the
// raw token.
//
// Returns (nil, nil) — no error, no result — when no matching audit
// exists, or the matching audit was rotated within ResendCooldown.
// The handler treats both cases the same as "neutral 200" to prevent
// email-enumeration via timing differences.
func (s *Store) RotateClaimTokenByEmail(ctx context.Context, contactEmail string, ttl time.Duration) (*RotateResult, error) {
	contactEmail = strings.ToLower(strings.TrimSpace(contactEmail))
	if contactEmail == "" {
		return nil, nil
	}

	// Find the candidate audit. Most recent unclaimed unexpired row,
	// excluding rows whose last_resend_at is within the cooldown window.
	const lookupQ = `SELECT id, contact_name FROM pending_audits
WHERE lower(contact_email) = $1
  AND claimed_at IS NULL
  AND expires_at > NOW()
  AND (last_resend_at IS NULL OR last_resend_at < NOW() - $2::INTERVAL)
ORDER BY created_at DESC
LIMIT 1`

	var auditID uuid.UUID
	var contactName string
	err := s.pool.QueryRow(ctx, lookupQ, contactEmail, ResendCooldown.String()).
		Scan(&auditID, &contactName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // neutral path
	}
	if err != nil {
		return nil, fmt.Errorf("lookup pending audit by email: %w", err)
	}

	rawToken, err := generateRawToken(32)
	if err != nil {
		return nil, fmt.Errorf("rotate claim token: %w", err)
	}
	newExpires := time.Now().UTC().Add(ttl)

	// Conditional UPDATE — guards against TOCTOU where two concurrent
	// rotations race past the lookup. Whichever UPDATE lands first wins;
	// the loser sees zero rows affected and we treat it as "neutral".
	const updateQ = `UPDATE pending_audits
SET claim_token_hash = $2, expires_at = $3, last_resend_at = NOW()
WHERE id = $1
  AND claimed_at IS NULL
  AND expires_at > NOW()
  AND (last_resend_at IS NULL OR last_resend_at < NOW() - $4::INTERVAL)`
	tag, err := s.pool.Exec(ctx, updateQ, auditID, hashToken(rawToken), newExpires, ResendCooldown.String())
	if err != nil {
		return nil, fmt.Errorf("rotate claim token update: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, nil
	}

	return &RotateResult{
		AuditID:     auditID,
		ContactName: contactName,
		ClaimToken:  rawToken,
	}, nil
}

// RotateClaimTokenByID is the sweep-driven counterpart. The 14-day
// audit-ready worker calls this so the email it sends contains a
// working activation URL (the raw token from /audit/start was never
// persisted, only its hash). No cooldown — the worker fires once per
// audit per 24h and is itself the rate limit.
func (s *Store) RotateClaimTokenByID(ctx context.Context, auditID uuid.UUID, ttl time.Duration) (*RotateResult, error) {
	rawToken, err := generateRawToken(32)
	if err != nil {
		return nil, fmt.Errorf("rotate claim token: %w", err)
	}
	newExpires := time.Now().UTC().Add(ttl)

	const updateQ = `UPDATE pending_audits
SET claim_token_hash = $2, expires_at = $3, last_resend_at = NOW()
WHERE id = $1 AND claimed_at IS NULL
RETURNING contact_name`
	var contactName string
	err = s.pool.QueryRow(ctx, updateQ, auditID, hashToken(rawToken), newExpires).Scan(&contactName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("rotate claim token by id: %w", err)
	}
	return &RotateResult{
		AuditID:     auditID,
		ContactName: contactName,
		ClaimToken:  rawToken,
	}, nil
}

// =============================================================================
// helpers
// =============================================================================

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func generateRawToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func suggestedSlug(seed string) string {
	seed = strings.ToLower(seed)
	var b strings.Builder
	for _, r := range seed {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '.' || r == '_' || r == '-':
			b.WriteByte('-')
		}
	}
	base := b.String()
	if base == "" {
		base = "audit"
	}
	if len(base) > 24 {
		base = base[:24]
	}
	suffix, err := generateRawToken(4)
	if err != nil {
		return base + "-fallback"
	}
	suffix = strings.ToLower(suffix)
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	return base + "-" + suffix
}
