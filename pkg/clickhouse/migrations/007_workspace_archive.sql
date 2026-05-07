-- 007_workspace_archive.sql
-- Bastio Workspace message archive. Conversations land in PG (hot, edited,
-- joined to assistants/sources). Older messages get swept here for
-- analytics + compliance retention; PG holds only what the dashboard
-- shows day-to-day.
--
-- Sweep is a River periodic job (workspace.archive_messages, see
-- internal/workspace/archive_worker.go) running every 24h.
--
-- Cross-cutting rule: customer_id-partitioned, monthly, ordered for
-- typical workspace queries (per-customer, per-conversation, time-series).

CREATE TABLE IF NOT EXISTS bastio.workspace_messages_archive (
    id                  UUID,
    conversation_id     UUID,
    customer_id         UUID,
    user_id             String,                                -- branded:<sess_uuid> or dashboard user id
    role                LowCardinality(String),                -- system | user | assistant | tool
    content             String,                                -- full text — keep for audit/replay
    provider            LowCardinality(String) DEFAULT '',
    model               String DEFAULT '',
    prompt_tokens       Int32,
    completion_tokens   Int32,
    cost_cents          Int32,
    finish_reason       LowCardinality(String) DEFAULT '',
    error               String DEFAULT '',
    created_at          DateTime64(3, 'UTC'),
    archived_at         DateTime64(3, 'UTC') DEFAULT now64(3, 'UTC')
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (customer_id, conversation_id, created_at, id)
TTL toDateTime(created_at) + INTERVAL 365 DAY DELETE
SETTINGS index_granularity = 8192;
