// Package governance receives telemetry from the Bastio Governance browser
// extension, enforces HMAC-authenticated ingestion, and writes events to the
// shared ClickHouse analytics store. The PostgreSQL side stores per-org
// installations (installation_token, installation_secret) keyed to a tenant.
//
// The wire contract matches the extension at bastio-extension/src/lib/types.ts.
package governance

import (
	"time"

	"github.com/google/uuid"
)

// Severity matches the extension's tier model.
type Severity string

const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// Action describes the policy outcome for a single send-attempt event.
type Action string

const (
	ActionLogged     Action = "logged"
	ActionWarned     Action = "warned"
	ActionBlocked    Action = "blocked"
	ActionRedirected Action = "redirected"
	ActionOverridden Action = "overridden"
)

// EventPayload is the JSON body POSTed to /v1/governance/events.
// Extension must NEVER include prompt content here — only metadata.
// "Bastio sees that something happened, never what was said" is a
// load-bearing promise; do not regress this field set without review.
type EventPayload struct {
	EventID              string   `json:"event_id"`
	UserID               string   `json:"user_id"`
	OccurredAt           string   `json:"occurred_at"`
	SourceDomain         string   `json:"source_domain"`
	RuleIDs              []string `json:"rule_ids"`
	Severity             Severity `json:"severity"`
	Action               Action   `json:"action"`
	CharCountIntercepted int32    `json:"char_count_intercepted"`
	Browser              string   `json:"browser"`
	BrowserVersion       string   `json:"browser_version"`
	ExtensionVersion     string   `json:"extension_version"`
	RedirectTargetLabel  string   `json:"redirect_target_label,omitempty"`
	OverrideJustification string  `json:"override_justification,omitempty"`
}

// HeartbeatPayload is sent every five minutes from the extension while the
// browser is open. Drives the "Extension Deployment health" dashboard view.
type HeartbeatPayload struct {
	InstallID        string `json:"install_id"`
	ExtensionVersion string `json:"extension_version"`
	Browser          string `json:"browser"`
	BrowserVersion   string `json:"browser_version"`
}

// ClassifyRequest is the body of the async server-side classifier hook.
type ClassifyRequest struct {
	TextExcerpt  string   `json:"text_excerpt"`
	Layer3Hits   []string `json:"layer_3_hits"`
	SourceDomain string   `json:"source_domain"`
}

// ClassifyResponse is what the OSS server returns. The OSS server runs a
// regex+entropy fallback; Cloud overrides this with a trained model.
type ClassifyResponse struct {
	Severity   Severity `json:"severity"`
	Confidence float64  `json:"confidence"`
	Reasoning  string   `json:"reasoning"`
}

// PolicyConfig is what the extension polls every 30 minutes from /policy to
// refresh its severity-action mapping and customer-keyword list.
type PolicyConfig struct {
	Default        map[Severity]string `json:"default_policy"`
	CustomKeywords []string            `json:"custom_keywords"`
	OverrideEnabled bool               `json:"override_enabled"`
}

// DomainList is fetched every six hours from /domain-list. Bundled defaults
// live in the manifest's content-script matches; this list is for live updates.
type DomainList struct {
	Domains []string  `json:"domains"`
	Etag    string    `json:"etag"`
	Updated time.Time `json:"updated"`
}

// Installation is the per-org credential row in PostgreSQL. The
// installation_token authenticates inbound requests; the installation_secret
// is the HKDF root for per-install HMAC keys.
type Installation struct {
	ID                  uuid.UUID
	CustomerID          uuid.UUID
	OrgID               uuid.UUID // External org identifier surfaced to IT
	InstallationToken   string    // Bearer; stored hashed
	InstallationSecret  string    // HKDF root; encrypted at rest
	Label               string
	CreatedAt           time.Time
	RevokedAt           *time.Time
}
