package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InstallStore reads and writes the per-org governance_installations rows.
// The installation_secret column is the HKDF root used by the extension and
// server to derive per-install HMAC keys. Stored in plaintext at the
// PostgreSQL row level today; cloud overlays envelope encryption (KMS).
type InstallStore struct {
	pool *pgxpool.Pool
}

func NewInstallStore(pool *pgxpool.Pool) *InstallStore {
	return &InstallStore{pool: pool}
}

// ErrInstallationNotFound is returned when no row matches the lookup.
var ErrInstallationNotFound = errors.New("governance installation not found")

// LookupByOrg resolves an Installation by external org_id. Used by HMAC
// verification on every inbound request.
func (s *InstallStore) LookupByOrg(ctx context.Context, orgID string) (*Installation, error) {
	parsed, err := uuid.Parse(orgID)
	if err != nil {
		return nil, fmt.Errorf("parse org_id: %w", err)
	}

	const q = `
SELECT id, customer_id, org_id, installation_token_hash, installation_secret, label, created_at, revoked_at
FROM governance_installations
WHERE org_id = $1 AND revoked_at IS NULL
LIMIT 1`
	row := s.pool.QueryRow(ctx, q, parsed)
	inst := &Installation{}
	var tokenHash string
	if err := row.Scan(
		&inst.ID, &inst.CustomerID, &inst.OrgID, &tokenHash,
		&inst.InstallationSecret, &inst.Label, &inst.CreatedAt, &inst.RevokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInstallationNotFound
		}
		return nil, fmt.Errorf("query installation: %w", err)
	}
	inst.InstallationToken = tokenHash // hash, not plaintext
	return inst, nil
}

// Create inserts a new installation row and returns the freshly generated
// installation_token (plaintext, returned to caller exactly once) and
// installation_secret. The token is stored hashed.
//
// Used by the dashboard's "Generate MDM bundle" wizard.
func (s *InstallStore) Create(ctx context.Context, customerID uuid.UUID, label string, plaintextToken, plaintextSecret string) (*Installation, error) {
	hashHex := hashToken(plaintextToken)
	const q = `
INSERT INTO governance_installations (customer_id, org_id, installation_token_hash, installation_secret, label)
VALUES ($1, uuidv7(), $2, $3, $4)
RETURNING id, customer_id, org_id, installation_token_hash, installation_secret, label, created_at, revoked_at`
	row := s.pool.QueryRow(ctx, q, customerID, hashHex, plaintextSecret, label)
	inst := &Installation{}
	var tokenHashOut string
	if err := row.Scan(
		&inst.ID, &inst.CustomerID, &inst.OrgID, &tokenHashOut,
		&inst.InstallationSecret, &inst.Label, &inst.CreatedAt, &inst.RevokedAt,
	); err != nil {
		return nil, fmt.Errorf("create installation: %w", err)
	}
	inst.InstallationToken = tokenHashOut
	return inst, nil
}

// Revoke flips revoked_at and stops the installation from authenticating.
func (s *InstallStore) Revoke(ctx context.Context, customerID, installationID uuid.UUID) error {
	const q = `
UPDATE governance_installations
SET revoked_at = NOW()
WHERE id = $1 AND customer_id = $2 AND revoked_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, installationID, customerID)
	if err != nil {
		return fmt.Errorf("revoke installation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInstallationNotFound
	}
	return nil
}

// VerifyToken constant-time compares the bearer token against the stored
// hash. Currently unused (HMAC is the primary auth) but exposed for an
// optional dual-auth tier — extension can present both bearer + HMAC.
func (i *Installation) VerifyToken(plaintext string) bool {
	want, err := hex.DecodeString(i.InstallationToken)
	if err != nil {
		return false
	}
	got := sha256.Sum256([]byte(plaintext))
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] != got[i] {
			return false
		}
	}
	return true
}

func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
