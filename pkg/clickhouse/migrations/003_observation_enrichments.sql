-- Observation enrichments: ported from v1 (.ref/tracing/observations.go).
-- Adds model parameters, prompt versioning, tool call I/O, status messages,
-- and per-span cost to the observations table so the dashboard can show a
-- Langfuse-class span drill-down (model config used, which prompt version
-- ran, tool arguments and results, per-span cost).
--
-- All columns are additive with defaults, so existing rows remain valid and
-- the ingest pipeline can fill them in incrementally.

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS model_parameters String DEFAULT '';

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS prompt_id String DEFAULT '';

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS prompt_name String DEFAULT '';

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS prompt_version UInt32 DEFAULT 0;

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS tool_name String DEFAULT '';

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS tool_input String DEFAULT '';

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS tool_output String DEFAULT '';

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS status_message String DEFAULT '';

ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS cost_cents Float64 DEFAULT 0;
