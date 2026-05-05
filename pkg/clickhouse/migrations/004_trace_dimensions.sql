-- Trace dimensions for Langfuse-parity slicing.
--
-- environment: logical stage the request came from (e.g. production, staging,
--   preview-pr-42). Lets users keep noisy dev traffic out of prod dashboards.
-- release: application version / git SHA, so you can pin a regression to
--   "traces since v1.4.0 deploy".
-- trace_name: semantic label for the request (e.g. "guard_chat_completions",
--   "rag_qa"). Already carried at the span level via observations.name; this
--   gives the trace itself a human-friendly identifier independent of path.
--
-- tags Map(String, String) already exists on the traces table, we just
-- surface it through the API.

ALTER TABLE bastio.traces
    ADD COLUMN IF NOT EXISTS environment String DEFAULT '';

ALTER TABLE bastio.traces
    ADD COLUMN IF NOT EXISTS release String DEFAULT '';

ALTER TABLE bastio.traces
    ADD COLUMN IF NOT EXISTS trace_name String DEFAULT '';

ALTER TABLE bastio.traces
    ADD INDEX IF NOT EXISTS idx_environment environment TYPE set(0) GRANULARITY 1;

-- Mirror on observations so span-level filtering keeps the same shape.
ALTER TABLE bastio.observations
    ADD COLUMN IF NOT EXISTS environment String DEFAULT '';

-- Mirror on analytics_request_logs so hourly aggregates can slice by env.
ALTER TABLE bastio.analytics_request_logs
    ADD COLUMN IF NOT EXISTS environment String DEFAULT '';
