-- 023_workspace_allowed_models.sql
-- Per-tenant whitelist of LLM providers + models surfaced to employees
-- in the Workspace chat's model picker.
--
-- Empty array (the default) = "all curated defaults available" — the
-- workspace-app's hardcoded ModelPicker list shows through.
-- Non-empty = strict whitelist, both at UI display time and as
-- defense-in-depth on the chat send path.
--
-- Shape: JSON array of {provider, model} objects. Example:
--
--   [
--     {"provider": "openai",    "model": "gpt-4o-mini"},
--     {"provider": "openai",    "model": "gpt-4o"},
--     {"provider": "anthropic", "model": "claude-haiku-4-5"}
--   ]
--
-- Provider names match the OSS provider enum (openai, anthropic,
-- bedrock, gemini, mistral, cohere, ollama). Model names match what
-- the providers' own SDKs accept.
--
-- Idempotent — safe to re-run.

ALTER TABLE workspace_settings
    ADD COLUMN IF NOT EXISTS allowed_models JSONB NOT NULL DEFAULT '[]'::jsonb;
