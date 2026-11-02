package mcp

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Policy is enforced by both the hosted remote gateway and the local stdio proxy.
type Policy struct {
	AllowedTools          []string `json:"allowed_tools"`
	BlockedTools          []string `json:"blocked_tools"`
	ApprovalRequiredTools []string `json:"approval_required_tools"`
	WebhookURL            string   `json:"webhook_url,omitempty"`
	WebhookFormat         string   `json:"webhook_format,omitempty"`
}

type Server struct {
	ID               uuid.UUID  `json:"id"`
	CustomerID       uuid.UUID  `json:"-"`
	Name             string     `json:"name"`
	Client           string     `json:"client"`
	Mode             string     `json:"mode"`
	UpstreamURL      string     `json:"upstream_url,omitempty"`
	AuthType         string     `json:"auth_type"`
	HasCredential    bool       `json:"has_credential"`
	Policy           Policy     `json:"policy"`
	CreatedAt        time.Time  `json:"created_at"`
	LastSeenAt       *time.Time `json:"last_seen_at"`
	RevokedAt        *time.Time `json:"revoked_at"`
	DroppedEvents    int64      `json:"dropped_events"`
	Calls            int64      `json:"calls"`
	Blocked          int64      `json:"blocked"`
	PendingApprovals int64      `json:"pending_approvals"`
	credential       []byte
}

// Event deliberately has no arguments, results, prompts, credentials, or RPC IDs.
type Event struct {
	ID         uuid.UUID `json:"id"`
	ServerID   uuid.UUID `json:"server_id"`
	ServerName string    `json:"server_name,omitempty"`
	Client     string    `json:"client,omitempty"`
	ToolName   string    `json:"tool_name"`
	Direction  string    `json:"direction"`
	Action     string    `json:"action"`
	Categories []string  `json:"categories"`
	DurationMS float64   `json:"duration_ms"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Approval tracks a human-in-the-loop gate for high-risk tool calls.
// It stores only top-level argument keys and threat categories, never argument values.
type Approval struct {
	ID             uuid.UUID  `json:"id"`
	CustomerID     uuid.UUID  `json:"-"`
	ServerID       uuid.UUID  `json:"server_id"`
	ServerName     string     `json:"server_name,omitempty"`
	Client         string     `json:"client,omitempty"`
	ToolName       string     `json:"tool_name"`
	ArgumentKeys   []string   `json:"argument_keys"`
	Categories     []string   `json:"categories"`
	Status         string     `json:"status"`
	DecidedBy      string     `json:"decided_by,omitempty"`
	DecisionReason string     `json:"decision_reason,omitempty"`
	RequestedAt    time.Time  `json:"requested_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
}

type RuntimeBatch struct {
	Events        []Event `json:"events"`
	DroppedEvents int64   `json:"dropped_events"`
}

type InspectRequest struct {
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments"`
	Policy    Policy          `json:"policy"`
}

type Decision struct {
	Action     string          `json:"action"`
	Categories []string        `json:"categories"`
	Arguments  json.RawMessage `json:"arguments"`
	DurationMS float64         `json:"duration_ms"`
}
