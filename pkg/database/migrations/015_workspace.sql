-- 015_workspace.sql
-- Bastio Workspace — multi-model chat product. Conversations, assistants,
-- knowledge bases, branding. The wedge product Governance reveals leaks;
-- Workspace is the redirect destination.
--
-- Tenant key: customer_id. NOT foreign-keyed to customers(id) — matches the
-- governance subsystem convention so downstream customer models can diverge
-- without breaking these migrations. Application layer is responsible for
-- ensuring referential integrity at write time.

-- =============================================================================
-- Settings (one row per customer; lazy-created on first workspace access)
-- =============================================================================
CREATE TABLE IF NOT EXISTS workspace_settings (
    customer_id          UUID PRIMARY KEY,
    branding             JSONB NOT NULL DEFAULT '{}'::jsonb,        -- logo_url, primary_color, welcome_message, etc.
    default_assistant_id UUID,                                       -- NULL until first assistant created
    seat_limit           INTEGER NOT NULL DEFAULT 5,                  -- enforced on invite
    retention_days       INTEGER NOT NULL DEFAULT 90,                 -- conversation auto-archive
    spend_cap_cents      INTEGER,                                     -- NULL = no cap
    billing_mode         TEXT NOT NULL DEFAULT 'platform_keys'        -- platform_keys | byo_keys
                         CHECK (billing_mode IN ('platform_keys','byo_keys')),
    onboarding_completed_at TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- Assistants (named system-prompt presets, optional KB attach)
-- =============================================================================
CREATE TABLE IF NOT EXISTS workspace_assistants (
    id                 UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id        UUID NOT NULL,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    system_prompt      TEXT NOT NULL DEFAULT '',
    default_provider   TEXT NOT NULL DEFAULT 'openai'
                       CHECK (default_provider IN ('openai','anthropic','bedrock','ollama','google')),
    default_model      TEXT NOT NULL DEFAULT 'gpt-4o-mini',
    language           TEXT NOT NULL DEFAULT 'en',
    suggested_prompts  JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_default         BOOLEAN NOT NULL DEFAULT FALSE,
    archived_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS workspace_assistants_customer_idx
    ON workspace_assistants (customer_id) WHERE archived_at IS NULL;

-- One default assistant per customer (enforced via partial unique index).
CREATE UNIQUE INDEX IF NOT EXISTS workspace_assistants_default_uq
    ON workspace_assistants (customer_id) WHERE is_default = TRUE AND archived_at IS NULL;

-- =============================================================================
-- Knowledge sources (uploaded docs / URLs / text snippets)
-- =============================================================================
CREATE TABLE IF NOT EXISTS workspace_knowledge_sources (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id  UUID NOT NULL,
    name         TEXT NOT NULL,
    type         TEXT NOT NULL CHECK (type IN ('file','url','text')),
    source_ref   TEXT NOT NULL DEFAULT '',                  -- s3://... or https://... or '' for inline text
    inline_text  TEXT,                                      -- non-NULL for type='text'
    mime_type    TEXT,
    size_bytes   BIGINT NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','processing','ready','failed')),
    error        TEXT,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    archived_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS workspace_knowledge_sources_customer_idx
    ON workspace_knowledge_sources (customer_id) WHERE archived_at IS NULL;

-- Chunks. Stored without pgvector for MVP (deferred until pgvector is in
-- the dev/prod Postgres image). Retrieval uses naive substring/keyword
-- match in MVP, swapped for vector cosine in Phase 2. The embedding
-- column is REAL[] to give us a clean upgrade path: same column shape
-- pgvector accepts.
CREATE TABLE IF NOT EXISTS workspace_knowledge_chunks (
    id                   UUID PRIMARY KEY DEFAULT uuidv7(),
    knowledge_source_id  UUID NOT NULL REFERENCES workspace_knowledge_sources(id) ON DELETE RESTRICT,
    customer_id          UUID NOT NULL,
    ordinal              INTEGER NOT NULL,
    content              TEXT NOT NULL,
    token_count          INTEGER NOT NULL DEFAULT 0,
    embedding            REAL[],                              -- NULL until embedded
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS workspace_knowledge_chunks_source_idx
    ON workspace_knowledge_chunks (knowledge_source_id, ordinal);
CREATE INDEX IF NOT EXISTS workspace_knowledge_chunks_customer_idx
    ON workspace_knowledge_chunks (customer_id);

-- Assistant ↔ Knowledge source pivot.
CREATE TABLE IF NOT EXISTS workspace_assistant_knowledge (
    assistant_id          UUID NOT NULL REFERENCES workspace_assistants(id) ON DELETE RESTRICT,
    knowledge_source_id   UUID NOT NULL REFERENCES workspace_knowledge_sources(id) ON DELETE RESTRICT,
    customer_id           UUID NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (assistant_id, knowledge_source_id)
);
CREATE INDEX IF NOT EXISTS workspace_assistant_knowledge_customer_idx
    ON workspace_assistant_knowledge (customer_id);

-- =============================================================================
-- Conversations + messages
-- =============================================================================
CREATE TABLE IF NOT EXISTS workspace_conversations (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id   UUID NOT NULL,
    user_id       TEXT NOT NULL,                              -- external user id (Better Auth subject)
    assistant_id  UUID REFERENCES workspace_assistants(id) ON DELETE SET NULL,
    title         TEXT NOT NULL DEFAULT 'New chat',
    pinned        BOOLEAN NOT NULL DEFAULT FALSE,
    archived_at   TIMESTAMPTZ,
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS workspace_conversations_customer_user_idx
    ON workspace_conversations (customer_id, user_id, last_message_at DESC)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS workspace_messages (
    id                 UUID PRIMARY KEY DEFAULT uuidv7(),
    conversation_id    UUID NOT NULL REFERENCES workspace_conversations(id) ON DELETE RESTRICT,
    customer_id        UUID NOT NULL,
    role               TEXT NOT NULL CHECK (role IN ('system','user','assistant','tool')),
    content            TEXT NOT NULL,
    provider           TEXT,
    model              TEXT,
    prompt_tokens      INTEGER NOT NULL DEFAULT 0,
    completion_tokens  INTEGER NOT NULL DEFAULT 0,
    cost_cents         INTEGER NOT NULL DEFAULT 0,
    finish_reason      TEXT,
    error              TEXT,
    metadata           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS workspace_messages_conversation_idx
    ON workspace_messages (conversation_id, created_at);
CREATE INDEX IF NOT EXISTS workspace_messages_customer_idx
    ON workspace_messages (customer_id, created_at DESC);

CREATE TABLE IF NOT EXISTS workspace_message_attachments (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    message_id   UUID NOT NULL REFERENCES workspace_messages(id) ON DELETE RESTRICT,
    customer_id  UUID NOT NULL,
    name         TEXT NOT NULL,
    mime_type    TEXT NOT NULL,
    size_bytes   BIGINT NOT NULL DEFAULT 0,
    blob_ref     TEXT NOT NULL,                              -- s3://bucket/key or local://...
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS workspace_message_attachments_customer_idx
    ON workspace_message_attachments (customer_id);

-- =============================================================================
-- Members + invitations (workspace-scoped; user_id is opaque text so any
-- upstream auth system can be plugged in)
-- =============================================================================
CREATE TABLE IF NOT EXISTS workspace_members (
    customer_id  UUID NOT NULL,
    user_id      TEXT NOT NULL,
    email        TEXT NOT NULL,
    role         TEXT NOT NULL DEFAULT 'member'
                 CHECK (role IN ('owner','admin','member','viewer')),
    invited_by   TEXT,
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ,
    PRIMARY KEY (customer_id, user_id)
);
CREATE INDEX IF NOT EXISTS workspace_members_email_idx
    ON workspace_members (lower(email));

CREATE TABLE IF NOT EXISTS workspace_invitations (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id  UUID NOT NULL,
    email        TEXT NOT NULL,
    role         TEXT NOT NULL DEFAULT 'member'
                 CHECK (role IN ('owner','admin','member','viewer')),
    token_hash   TEXT NOT NULL UNIQUE,                       -- SHA-256 of bearer token
    invited_by   TEXT,
    expires_at   TIMESTAMPTZ NOT NULL,
    accepted_at  TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS workspace_invitations_customer_idx
    ON workspace_invitations (customer_id) WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- =============================================================================
-- updated_at triggers (mirror existing convention)
-- =============================================================================
CREATE OR REPLACE FUNCTION workspace_set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE t TEXT;
BEGIN
    FOR t IN
        SELECT unnest(ARRAY[
            'workspace_settings',
            'workspace_assistants',
            'workspace_knowledge_sources',
            'workspace_conversations'
        ])
    LOOP
        EXECUTE format(
            'DROP TRIGGER IF EXISTS %I_set_updated_at ON %I;
             CREATE TRIGGER %I_set_updated_at
             BEFORE UPDATE ON %I
             FOR EACH ROW EXECUTE FUNCTION workspace_set_updated_at();',
            t, t, t, t
        );
    END LOOP;
END $$;
