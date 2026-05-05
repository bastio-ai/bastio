package audit

import (
	"errors"
	"os"
	"time"

	"github.com/google/uuid"
)

// LoadConfig reads audit configuration from environment. Operators
// override PublicBaseURL via BASTIO_PUBLIC_URL — bundle and activation
// URLs in emails / responses use this prefix.
func LoadConfig() Config {
	cfg := Config{
		PublicBaseURL: os.Getenv("BASTIO_PUBLIC_URL"),
	}
	return cfg.WithDefaults()
}

// PendingAudit mirrors a row in pending_audits. The token fields
// expose only their hashes — raw tokens live in the Set-Cookie /
// activation URL response and never appear at rest.
type PendingAudit struct {
	ID               uuid.UUID  `json:"id"`
	CustomerID       uuid.UUID  `json:"customer_id"`
	ContactEmail     string     `json:"contact_email"`
	ContactName      string     `json:"contact_name"`
	CompanyName      string     `json:"company_name"`
	MDMFormat        string     `json:"mdm_format"`
	BundleUsedAt     *time.Time `json:"bundle_used_at,omitempty"`
	ExpiresAt        time.Time  `json:"expires_at"`
	ClaimedAt        *time.Time `json:"claimed_at,omitempty"`
	ClaimedByUserID  *uuid.UUID `json:"claimed_by_user_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// =============================================================================
// Config
// =============================================================================

// Config carries audit-service knobs.
type Config struct {
	// PublicBaseURL is the URL prefix used when constructing
	// activation + bundle URLs in emails and responses. Default
	// "https://bastio.com" — operators override for staging.
	PublicBaseURL string

	// ClaimTokenTTL is how long the activation link works. Default
	// 60 days — enough for an enterprise procurement cycle.
	ClaimTokenTTL time.Duration

	// BundleTokenTTL is how long the one-shot bundle download URL
	// remains valid. Default 7 days — the recipient should download
	// it well before that.
	BundleTokenTTL time.Duration

	// RecommendedSeatsHintFallback is the seats hint used when the
	// audit hasn't accumulated enough events to detect active users.
	RecommendedSeatsHintFallback int
}

// WithDefaults fills in zero-valued config fields.
func (c Config) WithDefaults() Config {
	if c.PublicBaseURL == "" {
		c.PublicBaseURL = "https://bastio.com"
	}
	if c.ClaimTokenTTL == 0 {
		c.ClaimTokenTTL = 60 * 24 * time.Hour
	}
	if c.BundleTokenTTL == 0 {
		c.BundleTokenTTL = 7 * 24 * time.Hour
	}
	if c.RecommendedSeatsHintFallback == 0 {
		c.RecommendedSeatsHintFallback = 5
	}
	return c
}

// =============================================================================
// Request / response shapes
// =============================================================================

// StartRequest is the body of POST /v1/audit/start.
type StartRequest struct {
	ContactEmail string `json:"contact_email"`
	ContactName  string `json:"contact_name,omitempty"`
	CompanyName  string `json:"company_name,omitempty"`
	MDMFormat    string `json:"mdm_format,omitempty"` // chrome | intune | jamf; default chrome
}

// StartResponse is what /audit/start returns. ActivationURL is the
// link the prospect clicks after the 14-day audit completes (or
// before, if they're impatient — the link works any time within
// ClaimTokenTTL). BundleDownloadURL is the one-shot ZIP fetch.
type StartResponse struct {
	AuditID            uuid.UUID `json:"audit_id"`
	ActivationURL      string    `json:"activation_url"`
	BundleDownloadURL  string    `json:"bundle_download_url"`
	BundleSizeBytes    int64     `json:"bundle_size_bytes,omitempty"`
	ExpiresAt          time.Time `json:"expires_at"`
}

// ResendRequest is the body of POST /v1/audit/resend. The endpoint
// always returns 200 with a neutral message regardless of whether the
// email matches a known audit, to avoid leaking whether a given email
// has registered.
type ResendRequest struct {
	ContactEmail string `json:"contact_email"`
}

// ResendResponse is the neutral always-200 reply.
type ResendResponse struct {
	Status string `json:"status"` // always "ok"
}

// =============================================================================
// Sentinel errors
// =============================================================================

var (
	// ErrInvalidEmail surfaces from the start handler when the
	// contact email looks malformed. Caught early to avoid sending
	// SendGrid a junk address.
	ErrInvalidEmail = errors.New("audit: invalid contact email")

	// ErrBundleAlreadyDownloaded is returned by the bundle handler
	// when the one-shot token has already been consumed. Surfaces as
	// 410 Gone to the caller.
	ErrBundleAlreadyDownloaded = errors.New("audit: bundle already downloaded")

	// ErrTokenExpired is returned for both bundle + claim tokens past
	// expires_at. Surfaces as 410 Gone.
	ErrTokenExpired = errors.New("audit: token expired")

	// ErrTokenInvalid covers signature-mismatch or unknown tokens.
	// Surfaces as 401 to avoid leaking whether a given token was
	// ever issued.
	ErrTokenInvalid = errors.New("audit: token invalid")
)
