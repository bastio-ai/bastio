-- Prompt registry (OSS tier).
--
-- A "prompt" is a named, versioned thing the application fetches by name
-- and optional label. Versions are immutable — each edit creates a new
-- row. This is the OSS surface; Cloud layers deploy-by-label workflows,
-- approvals, and A/B experiments on top using the same tables.
--
-- The prompt_name + prompt_version columns on bastio.observations let
-- the dashboard back-link from a trace to the exact prompt version that
-- produced it without joining across Postgres and ClickHouse.

CREATE TABLE prompts (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (customer_id, name)
);

CREATE INDEX idx_prompts_customer_id ON prompts(customer_id);

CREATE TABLE prompt_versions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    prompt_id UUID NOT NULL REFERENCES prompts(id) ON DELETE RESTRICT,
    customer_id UUID NOT NULL,
    version INT NOT NULL,
    content TEXT NOT NULL,
    -- content_type = 'text' for a single-string template, 'chat' for a
    -- JSON-encoded array of OpenAI-shape messages.
    content_type TEXT NOT NULL DEFAULT 'text',
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    labels TEXT[] NOT NULL DEFAULT '{}',
    commit_message TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (prompt_id, version)
);

CREATE INDEX idx_prompt_versions_prompt_id ON prompt_versions(prompt_id);
CREATE INDEX idx_prompt_versions_customer_id ON prompt_versions(customer_id);
CREATE INDEX idx_prompt_versions_labels ON prompt_versions USING gin(labels);
