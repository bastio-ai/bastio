package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Domain is one custom-domain attachment for a customer's branded chat.
type Domain struct {
	ID                 uuid.UUID  `json:"id"`
	CustomerID         uuid.UUID  `json:"customer_id"`
	Domain             string     `json:"domain"`
	VerificationToken  string     `json:"verification_token"`
	VerifiedAt         *time.Time `json:"verified_at,omitempty"`
	LastCheckedAt      *time.Time `json:"last_checked_at,omitempty"`
	LastCheckError     *string    `json:"last_check_error,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// BrandedSession is an anonymous end-user session on the branded chat.
type BrandedSession struct {
	ID         uuid.UUID `json:"id"`
	CustomerID uuid.UUID `json:"customer_id"`
	UserLabel  *string   `json:"user_label,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

const (
	verificationPrefix = "bastio-verify="
	sessionTTL         = 30 * 24 * time.Hour
)

// =============================================================================
// Slug
// =============================================================================

// SetSlug claims a slug for a customer. Returns ErrSlugTaken if another
// customer already holds it.
var ErrSlugTaken = errors.New("workspace: slug already taken")

func (s *Store) SetSlug(ctx context.Context, customerID uuid.UUID, slug string) error {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !validSlug(slug) {
		return fmt.Errorf("invalid slug: %q (lowercase letters, digits, hyphens; 3-40 chars)", slug)
	}
	const q = `UPDATE workspace_settings SET slug = $2 WHERE customer_id = $1`
	if _, err := s.pool.Exec(ctx, q, customerID, slug); err != nil {
		// Postgres error code 23505 = unique_violation.
		if strings.Contains(err.Error(), "workspace_settings_slug_uq") {
			return ErrSlugTaken
		}
		return fmt.Errorf("set slug: %w", err)
	}
	return nil
}

// CustomerBySlug looks up a customer by their workspace slug. Returns
// ErrNotFound when no settings row matches.
func (s *Store) CustomerBySlug(ctx context.Context, slug string) (uuid.UUID, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return uuid.Nil, ErrNotFound
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT customer_id FROM workspace_settings WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lookup slug: %w", err)
	}
	return id, nil
}

func validSlug(s string) bool {
	if len(s) < 3 || len(s) > 40 {
		return false
	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !ok {
			return false
		}
		if (i == 0 || i == len(s)-1) && r == '-' {
			return false
		}
	}
	return true
}

// =============================================================================
// Custom domains
// =============================================================================

func (s *Store) ListDomains(ctx context.Context, customerID uuid.UUID) ([]Domain, error) {
	const q = `SELECT id, customer_id, domain, verification_token, verified_at,
last_checked_at, last_check_error, created_at, updated_at
FROM workspace_domains
WHERE customer_id = $1
ORDER BY created_at ASC`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.CustomerID, &d.Domain, &d.VerificationToken,
			&d.VerifiedAt, &d.LastCheckedAt, &d.LastCheckError,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan domain: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CreateDomain registers a new pending domain. Returns ErrDomainTaken
// when the domain is already claimed (by any customer).
var ErrDomainTaken = errors.New("workspace: domain already registered")

func (s *Store) CreateDomain(ctx context.Context, customerID uuid.UUID, domain string) (*Domain, error) {
	domain = normalizeDomain(domain)
	if !validDomain(domain) {
		return nil, fmt.Errorf("invalid domain: %q", domain)
	}
	tok, err := generateBearerToken(24)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}

	const q = `INSERT INTO workspace_domains (customer_id, domain, verification_token)
VALUES ($1, $2, $3)
RETURNING id, created_at, updated_at`
	var d Domain
	d.CustomerID = customerID
	d.Domain = domain
	d.VerificationToken = tok
	err = s.pool.QueryRow(ctx, q, customerID, domain, tok).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "workspace_domains_domain_uq") {
			return nil, ErrDomainTaken
		}
		return nil, fmt.Errorf("insert domain: %w", err)
	}
	return &d, nil
}

func (s *Store) DeleteDomain(ctx context.Context, customerID, id uuid.UUID) error {
	const q = `DELETE FROM workspace_domains WHERE customer_id = $1 AND id = $2`
	tag, err := s.pool.Exec(ctx, q, customerID, id)
	if err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// VerifyDomain queries DNS for the customer-provided TXT token. On
// success, marks verified_at; on failure, records last_check_error and
// returns the error. Idempotent: re-verifying an already-verified
// domain is a no-op (still updates last_checked_at).
//
// resolver is exposed so tests can swap in a fake instead of hitting
// real DNS. Production wires `defaultTXTResolver`.
func (s *Store) VerifyDomain(ctx context.Context, customerID, id uuid.UUID, resolver TXTResolver) (*Domain, error) {
	if resolver == nil {
		resolver = defaultTXTResolver
	}
	d, err := s.getDomain(ctx, customerID, id)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	records, lookupErr := resolver(ctx, d.Domain)
	matched := false
	expected := verificationPrefix + d.VerificationToken
	for _, r := range records {
		if strings.TrimSpace(r) == expected {
			matched = true
			break
		}
	}

	if matched {
		const q = `UPDATE workspace_domains
SET verified_at = $3, last_checked_at = $3, last_check_error = NULL
WHERE customer_id = $1 AND id = $2`
		if _, err := s.pool.Exec(ctx, q, customerID, id, now); err != nil {
			return nil, fmt.Errorf("update verified: %w", err)
		}
	} else {
		errMsg := "TXT record not found; expected " + expected
		if lookupErr != nil {
			errMsg = "DNS lookup failed: " + lookupErr.Error()
		}
		const q = `UPDATE workspace_domains
SET last_checked_at = $3, last_check_error = $4
WHERE customer_id = $1 AND id = $2`
		if _, err := s.pool.Exec(ctx, q, customerID, id, now, errMsg); err != nil {
			return nil, fmt.Errorf("update unverified: %w", err)
		}
		return nil, errors.New(errMsg)
	}

	return s.getDomain(ctx, customerID, id)
}

func (s *Store) getDomain(ctx context.Context, customerID, id uuid.UUID) (*Domain, error) {
	const q = `SELECT id, customer_id, domain, verification_token, verified_at,
last_checked_at, last_check_error, created_at, updated_at
FROM workspace_domains
WHERE customer_id = $1 AND id = $2`
	var d Domain
	err := s.pool.QueryRow(ctx, q, customerID, id).Scan(&d.ID, &d.CustomerID, &d.Domain,
		&d.VerificationToken, &d.VerifiedAt, &d.LastCheckedAt,
		&d.LastCheckError, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get domain: %w", err)
	}
	return &d, nil
}

// CustomerByDomain resolves a Host header to a customer for the resolver
// middleware. Only verified domains return a hit — unverified rows are
// dormant.
func (s *Store) CustomerByDomain(ctx context.Context, host string) (uuid.UUID, error) {
	host = normalizeDomain(host)
	if host == "" {
		return uuid.Nil, ErrNotFound
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT customer_id FROM workspace_domains
WHERE lower(domain) = $1 AND verified_at IS NOT NULL`, host).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lookup domain: %w", err)
	}
	return id, nil
}

// TXTResolver fetches TXT records for a domain. Pluggable so tests can
// avoid real DNS.
type TXTResolver func(ctx context.Context, domain string) ([]string, error)

func defaultTXTResolver(ctx context.Context, domain string) ([]string, error) {
	r := &net.Resolver{}
	return r.LookupTXT(ctx, domain)
}

func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	if i := strings.Index(d, "/"); i > 0 {
		d = d[:i]
	}
	if i := strings.Index(d, ":"); i > 0 {
		d = d[:i]
	}
	return d
}

func validDomain(d string) bool {
	if len(d) < 4 || len(d) > 253 {
		return false
	}
	if !strings.Contains(d, ".") {
		return false
	}
	for _, r := range d {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

// =============================================================================
// Branded sessions
// =============================================================================

// EnsureBrandedSession reads-or-creates a session keyed by an opaque
// cookie token. Returns the session row + the token (always returned;
// the caller decides whether to set the cookie). When existing is true,
// the row was already in PG and last_seen_at was bumped.
func (s *Store) EnsureBrandedSession(ctx context.Context, customerID uuid.UUID, rawToken, userAgent, ipHash string) (*BrandedSession, string, bool, error) {
	if rawToken == "" {
		t, err := generateBearerToken(24)
		if err != nil {
			return nil, "", false, fmt.Errorf("token: %w", err)
		}
		rawToken = t
	}
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	// Try update first.
	const updateQ = `UPDATE workspace_branded_sessions
SET last_seen_at = NOW(), expires_at = NOW() + ($3::INTERVAL)
WHERE customer_id = $1 AND token_hash = $2 AND expires_at > NOW()
RETURNING id, customer_id, user_label, created_at, last_seen_at, expires_at`
	row := s.pool.QueryRow(ctx, updateQ, customerID, tokenHash, sessionTTL.String())
	var sess BrandedSession
	err := row.Scan(&sess.ID, &sess.CustomerID, &sess.UserLabel,
		&sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt)
	if err == nil {
		return &sess, rawToken, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, fmt.Errorf("update session: %w", err)
	}

	// Insert new row.
	const insertQ = `INSERT INTO workspace_branded_sessions
(customer_id, token_hash, user_agent, ip_hash, expires_at)
VALUES ($1, $2, $3, $4, NOW() + ($5::INTERVAL))
RETURNING id, customer_id, user_label, created_at, last_seen_at, expires_at`
	row = s.pool.QueryRow(ctx, insertQ, customerID, tokenHash, userAgent, ipHash, sessionTTL.String())
	if err := row.Scan(&sess.ID, &sess.CustomerID, &sess.UserLabel,
		&sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt); err != nil {
		return nil, "", false, fmt.Errorf("insert session: %w", err)
	}
	return &sess, rawToken, false, nil
}

// SetBrandedSessionLabel updates the visitor's chosen display name.
func (s *Store) SetBrandedSessionLabel(ctx context.Context, sessionID uuid.UUID, label string) error {
	const q = `UPDATE workspace_branded_sessions SET user_label = NULLIF($2, '')
WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, sessionID, label)
	if err != nil {
		return fmt.Errorf("set session label: %w", err)
	}
	return nil
}

// HashBrandedToken hashes a raw cookie token the same way the store
// stores it. Useful when a handler needs to look up a session before
// calling EnsureBrandedSession.
func HashBrandedToken(rawToken string) string {
	hash := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(hash[:])
}

// NewBrandedToken returns a fresh URL-safe random token suitable for a
// Set-Cookie value. Same length+encoding as the invitation tokens.
func NewBrandedToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
