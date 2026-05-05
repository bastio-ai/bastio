package governance

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BuildAnonymousMDMBundle generates an MDM bundle for a pending-audit
// customer who hasn't signed up yet. Wraps the same BuildMDMBundle
// generator the dashboard's authenticated endpoint uses; the
// difference is the lookup path — we pull org_id +
// installation_secret directly from governance_installations using
// customer_id (anonymous audits create exactly one installation per
// customer at /audit/start time, so there's no ambiguity).
//
// Returns the zip bytes + Content-Type. backendURL is where the
// extension's events POST to; defaults to "https://api.bastio.com"
// when empty so embedded MDM templates resolve correctly even if the
// caller forgot to pass one.
func (h *Handler) BuildAnonymousMDMBundle(ctx context.Context, customerID uuid.UUID, mdmFormat, companyName, backendURL string) ([]byte, string, error) {
	if mdmFormat == "" {
		mdmFormat = "chrome"
	}
	if backendURL == "" {
		// Self-hosted operators that don't set BASTIO_PUBLIC_URL hit
		// this default — production API host. Anonymous audit flow
		// can't extract the host from a request because the bundle
		// is downloaded by the prospect's IT, not the original POST
		// submitter (different IP, different network).
		backendURL = "https://api.bastio.com"
	}

	// Anonymous-audit customers have exactly one installation row,
	// created in the same transaction as the pending_audits row by
	// audit/store.go's Provision. No need to disambiguate.
	const q = `SELECT org_id, installation_secret
FROM governance_installations
WHERE customer_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC
LIMIT 1`
	var (
		orgID              uuid.UUID
		installationSecret string
	)
	err := h.installs.pool.QueryRow(ctx, q, customerID).Scan(&orgID, &installationSecret)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", fmt.Errorf("no installation found for customer %s", customerID)
	}
	if err != nil {
		return nil, "", fmt.Errorf("look up installation: %w", err)
	}

	zipBytes, err := BuildMDMBundle(orgID, backendURL,
		"<token-supplied-at-install>", installationSecret)
	if err != nil {
		return nil, "", fmt.Errorf("build mdm bundle: %w", err)
	}
	_ = companyName // reserved for future per-company README copy in the bundle
	return zipBytes, "application/zip", nil
}
