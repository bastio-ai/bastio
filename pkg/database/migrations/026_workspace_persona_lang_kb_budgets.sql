-- 026_workspace_persona_lang_kb_budgets.sql
-- Favio→Bastio launch-cut feature port. Groups schema changes for
-- four small features so they share one migration:
--
--   1. Workspace AI persona (name, personality, tone) — injected into
--      every assistant's system prompt at message time.
--   2. Per-assistant language auto-detect — column becomes NULL-able
--      so NULL = auto-detect from user input, ISO code = forced.
--   3. Knowledge source metadata — character_count + last_synced_at
--      (file_type and file_size already exist as mime_type/size_bytes).
--   4. Per-member budgets — monthly_token_limit and daily_rate_limit
--      enforced before the LLM call in runProvider/streamProvider.
--
-- All four are additive and independent. Idempotent via IF NOT EXISTS
-- on column adds (Postgres 9.6+).

-- =============================================================================
-- 1. AI Persona — three text fields on workspace_settings
-- =============================================================================
ALTER TABLE workspace_settings
    ADD COLUMN IF NOT EXISTS ai_persona_name        TEXT,
    ADD COLUMN IF NOT EXISTS ai_persona_personality TEXT,
    ADD COLUMN IF NOT EXISTS ai_persona_tone        TEXT;

-- =============================================================================
-- 2. Per-assistant language: NOT NULL → NULL allowed (NULL = auto-detect).
--    Existing rows keep their 'en' default; new rows can pass NULL.
-- =============================================================================
ALTER TABLE workspace_assistants
    ALTER COLUMN language DROP NOT NULL;

-- =============================================================================
-- 3. Knowledge source metadata
-- =============================================================================
ALTER TABLE workspace_knowledge_sources
    ADD COLUMN IF NOT EXISTS character_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_synced_at  TIMESTAMPTZ;

-- =============================================================================
-- 4. Per-member token + rate budgets: moved to bastio-cloud (workspace_members
-- is a cloud-only multi-user table). See bastio-cloud/pkg/migrations/
-- migrations/007_workspace_member_budgets.sql.
-- =============================================================================
