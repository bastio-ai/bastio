// Package workspace implements the Bastio Workspace product — a multi-model
// chat application with conversations, assistants, knowledge bases, and
// branding. Tables defined in migration 015_workspace.sql.
//
// Tenant scoping: every query filters on customer_id. OSS runs in
// single-tenant mode and uses a default customer; Bastio Cloud injects
// the authenticated user's customer through DashboardMiddleware.
//
// Wiring: see bastio/pkg/server/server.go where workspace.NewHandler is
// constructed and mounted under /v1/workspace.
package workspace

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Settings is one-row-per-customer global state for the workspace.
type Settings struct {
	CustomerID            uuid.UUID       `json:"customer_id"`
	Branding              json.RawMessage `json:"branding"`
	DefaultAssistantID    *uuid.UUID      `json:"default_assistant_id,omitempty"`
	SeatLimit             int             `json:"seat_limit"`
	RetentionDays         int             `json:"retention_days"`
	SpendCapCents         *int            `json:"spend_cap_cents,omitempty"`
	BillingMode           string          `json:"billing_mode"`
	// AllowedModels is the strict whitelist of provider+model pairs
	// surfaced in the employee chat's model picker. Empty = all
	// curated defaults available; non-empty = strict whitelist.
	// Stored as JSON array of {provider, model} objects in
	// workspace_settings.allowed_models (migration 023).
	AllowedModels []AllowedModel `json:"allowed_models"`
	// AI persona — workspace-level. When any of these are set, the
	// employee chat surface treats the assistant as "Bob" (or whatever
	// name the customer chose) by injecting a persona instruction at
	// the top of every assistant's system prompt. NULL = no persona,
	// raw assistant prompts only.
	AIPersonaName        *string `json:"ai_persona_name,omitempty"`
	AIPersonaPersonality *string `json:"ai_persona_personality,omitempty"`
	AIPersonaTone        *string `json:"ai_persona_tone,omitempty"`
	OnboardingCompletedAt *time.Time `json:"onboarding_completed_at,omitempty"`
	// DisableImageAttachments hard-blocks image uploads in the
	// chat surface for this workspace. Admin toggle. When TRUE,
	// the dashboard's chat-tab hides the attach button and the
	// /chat-attachments endpoint refuses multipart uploads. The
	// motivation: image pixels bypass the security engine (we
	// strip base64 before scanning text and recompose after), so
	// regulated workspaces that can't accept that exposure can
	// disable the feature entirely. Default FALSE.
	DisableImageAttachments bool `json:"disable_image_attachments"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// AllowedModel is one entry in workspace_settings.allowed_models. The
// provider name matches the OSS provider enum (openai, anthropic,
// bedrock, gemini, mistral, cohere, ollama). The model string is the
// provider SDK's identifier (e.g. "gpt-4o-mini", "claude-haiku-4-5").
type AllowedModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Assistant is a named system-prompt preset with an optional KB attach.
type Assistant struct {
	ID                uuid.UUID       `json:"id"`
	CustomerID        uuid.UUID       `json:"customer_id"`
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	SystemPrompt      string          `json:"system_prompt"`
	DefaultProvider   string          `json:"default_provider"`
	DefaultModel      string          `json:"default_model"`
	// Language: nil = auto-detect from user input, non-nil = always
	// respond in this ISO code. The instruction is injected into the
	// system prompt at message time (resolveAssistantConfig).
	Language          *string         `json:"language,omitempty"`
	SuggestedPrompts  json.RawMessage `json:"suggested_prompts"`
	IsDefault         bool            `json:"is_default"`
	KnowledgeSourceIDs []uuid.UUID    `json:"knowledge_source_ids,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// KnowledgeSource is one document/URL/text snippet attached to one or more
// assistants. Chunks are stored in workspace_knowledge_chunks.
type KnowledgeSource struct {
	ID             uuid.UUID       `json:"id"`
	CustomerID     uuid.UUID       `json:"customer_id"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`     // file | url | text
	SourceRef      string          `json:"source_ref"`
	InlineText     *string         `json:"inline_text,omitempty"`
	MimeType       *string         `json:"mime_type,omitempty"`
	SizeBytes      int64           `json:"size_bytes"`
	// CharacterCount is the extracted text length post-ingest. 0 until
	// the worker flips status to 'ready'. Useful for "this PDF
	// produced N characters of indexed text" — reveals empty/scanned
	// PDFs that look uploaded but have no usable content.
	CharacterCount int             `json:"character_count"`
	// LastSyncedAt ticks every time the ingest worker successfully
	// finalizes the row. NULL until first ingest. Re-ingest (e.g.
	// re-crawl a URL) updates it. The dashboard renders "synced 3h
	// ago" so users know how fresh the indexed copy is.
	LastSyncedAt   *time.Time      `json:"last_synced_at,omitempty"`
	// Status: pending → processing → ready (success); pending → failed
	// (extract / chunk error); pending → quarantined (security scan
	// blocked the content). 'quarantined' was added in v2.0 to flag
	// uploads where the security engine refused the content; the row
	// stays visible but no chunks are produced. Admins release via
	// POST /knowledge/{id}/release.
	Status string `json:"status"`
	Error  *string `json:"error,omitempty"`
	// ContentHash is the SHA-256 of the raw uploaded bytes (hex).
	// Populated at upload time, before extraction. Empty for inline-
	// text sources (no raw bytes to hash) and for sources from before
	// migration 029. Drives dedup queries + post-incident forensics.
	ContentHash *string `json:"content_hash,omitempty"`
	// ScanResult is the snapshot of the ingest-time security scan that
	// either sanitized or quarantined this source. NULL when the
	// content was clean (no rewriting needed) or when the source
	// predates the scan gate. Shape:
	//   { "action": "block"|"sanitize"|"allow",
	//     "categories": ["pii_email", "secrets"],
	//     "threat_score": 0.92 }
	ScanResult json.RawMessage `json:"scan_result,omitempty"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// Conversation groups messages by user + assistant.
type Conversation struct {
	ID            uuid.UUID  `json:"id"`
	CustomerID    uuid.UUID  `json:"customer_id"`
	UserID        string     `json:"user_id"`
	AssistantID   *uuid.UUID `json:"assistant_id,omitempty"`
	Title         string     `json:"title"`
	Pinned        bool       `json:"pinned"`
	LastMessageAt time.Time  `json:"last_message_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Message is one turn in a conversation.
type Message struct {
	ID                uuid.UUID       `json:"id"`
	ConversationID    uuid.UUID       `json:"conversation_id"`
	CustomerID        uuid.UUID       `json:"customer_id"`
	Role              string          `json:"role"` // system | user | assistant | tool
	Content           string          `json:"content"`
	Provider          *string         `json:"provider,omitempty"`
	Model             *string         `json:"model,omitempty"`
	PromptTokens      int             `json:"prompt_tokens"`
	CompletionTokens  int             `json:"completion_tokens"`
	CostCents         int             `json:"cost_cents"`
	FinishReason      *string         `json:"finish_reason,omitempty"`
	Error             *string         `json:"error,omitempty"`
	Metadata          json.RawMessage `json:"metadata"`
	CreatedAt         time.Time       `json:"created_at"`
}

// Member is a user with workspace access at the customer level.
type Member struct {
	CustomerID uuid.UUID  `json:"customer_id"`
	UserID     string     `json:"user_id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"` // owner | admin | member | viewer
	InvitedBy  *string    `json:"invited_by,omitempty"`
	JoinedAt   time.Time  `json:"joined_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	// MonthlyTokenLimit and DailyRateLimit cap a single user's
	// generation. Both are NULL = no limit. Enforced before the LLM
	// call in runProvider/streamProvider via budget.go. Reset
	// implicitly via time-window queries (calendar month for
	// monthly, rolling 24h for daily — see enforceBudget).
	MonthlyTokenLimit *int `json:"monthly_token_limit,omitempty"`
	DailyRateLimit    *int `json:"daily_rate_limit,omitempty"`
}

// Invitation is a pending workspace invite addressed by email.
type Invitation struct {
	ID         uuid.UUID  `json:"id"`
	CustomerID uuid.UUID  `json:"customer_id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	InvitedBy  *string    `json:"invited_by,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}
