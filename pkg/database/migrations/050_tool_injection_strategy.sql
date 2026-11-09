-- Gateway strategy for indirect injection in agent context.
--
-- The gateway scans tool results (OpenAI tool/function messages, Claude
-- tool_result blocks, Responses function_call_output, Gemini
-- functionResponse) and assistant turns replayed as history with the
-- indirect_injection detector. Those parts come from agents: a false
-- block there breaks the customer's agent mid-run, while a false flag
-- costs nothing. The detector has known false positives on honest
-- content (articles quoting "ignore previous instructions", files of
-- prompt templates read by a coding agent), so the default is warn:
-- the finding is recorded and the request proceeds. Tenants opt in to
-- blocking per profile.
--
-- This is a separate column from indirect_injection_strategy (007) on
-- purpose. That column also governs Workspace knowledge-base ingest and
-- workspace tool gating, which keep their strict block default.
-- Changing it would have weakened those paths for every tenant.
--
-- Additive: existing rows pick up the default, so the gateway flags
-- rather than blocks for every tenant until someone opts in.

ALTER TABLE security_profiles
    ADD COLUMN IF NOT EXISTS tool_injection_strategy TEXT NOT NULL DEFAULT 'warn';

ALTER TABLE security_profiles
    DROP CONSTRAINT IF EXISTS security_profiles_tool_injection_strategy_valid;

ALTER TABLE security_profiles
    ADD CONSTRAINT security_profiles_tool_injection_strategy_valid
        CHECK (tool_injection_strategy IN ('block', 'warn', 'log_only'));

COMMENT ON COLUMN security_profiles.tool_injection_strategy IS
    'What happens when indirect_injection fires on tool results or replayed assistant turns in a gateway request: block | warn | log_only. Default warn (flag and forward). Knowledge-base ingest uses indirect_injection_strategy instead.';
