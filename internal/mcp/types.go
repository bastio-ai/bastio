package mcp

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Policy is enforced by the local proxy, including when reporting is delayed.
type Policy struct {
	AllowedTools []string `json:"allowed_tools"`
	BlockedTools []string `json:"blocked_tools"`
}

type Server struct {
	ID            uuid.UUID  `json:"id"`
	CustomerID    uuid.UUID  `json:"-"`
	Name          string     `json:"name"`
	Client        string     `json:"client"`
	Policy        Policy     `json:"policy"`
	CreatedAt     time.Time  `json:"created_at"`
	LastSeenAt    *time.Time `json:"last_seen_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	DroppedEvents int64      `json:"dropped_events"`
	Calls         int64      `json:"calls"`
	Blocked       int64      `json:"blocked"`
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
