-- 024_workspace_member_allowed_models.sql
-- Per-member override of the customer-wide LLM model whitelist.
--
--   NULL          → inherit workspace_settings.allowed_models
--   '[]'::jsonb   → explicit empty (this user has no models — locked out)
--   non-empty     → strict subset that overrides the customer-wide list
--
-- Mirrors Favio's OrganizationUser.allowed_models semantics: the merge
-- precedence is "member-override else customer-wide else everything"
-- (everything = the workspace-app's hardcoded MODEL_CATALOG).
--
-- Idempotent — safe to re-run.

ALTER TABLE workspace_members
    ADD COLUMN IF NOT EXISTS allowed_models JSONB;
