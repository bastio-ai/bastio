-- Bastio v2 ClickHouse Governance Schema (Migration 002)
-- Adds tables for the Bastio Governance browser extension's telemetry pipeline.
--
-- Cross-cutting rule: every row carries customer_id and is partitioned by month.
-- Partitioning + ordering matches the existing traces / threats pattern.

-- ============================================================
-- governance_events: every Block / Redirect / Override / Logged event
-- ============================================================
CREATE TABLE IF NOT EXISTS bastio.governance_events (
    event_id              String,
    customer_id           UUID,
    user_id               String,                                -- IdP subject, install UUID, or stub
    occurred_at           DateTime64(3, 'UTC'),

    -- Source + classification
    source_domain         String,
    rule_ids              Array(String),
    severity              LowCardinality(String),                -- low | medium | high
    action                LowCardinality(String),                -- logged | warned | blocked | redirected | overridden

    -- Metadata only — NEVER prompt content (brand promise)
    char_count_intercepted Int32,
    browser               LowCardinality(String),                -- chrome | edge | unknown
    browser_version       String,
    extension_version     String,
    redirect_target_label String DEFAULT '',
    override_justification String DEFAULT ''
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (customer_id, occurred_at, event_id)
TTL toDateTime(occurred_at) + INTERVAL 90 DAY DELETE
SETTINGS index_granularity = 8192;

-- ============================================================
-- governance_heartbeats: per-install liveness pings (every 5 min)
-- Powers "Extension Deployment health" dashboard view.
-- ============================================================
CREATE TABLE IF NOT EXISTS bastio.governance_heartbeats (
    customer_id        UUID,
    org_id             UUID,
    install_id         String,
    last_seen_at       DateTime64(3, 'UTC'),
    browser            LowCardinality(String),
    browser_version    String,
    extension_version  String
)
ENGINE = ReplacingMergeTree(last_seen_at)
PARTITION BY toYYYYMM(last_seen_at)
ORDER BY (customer_id, org_id, install_id)
TTL toDateTime(last_seen_at) + INTERVAL 30 DAY DELETE
SETTINGS index_granularity = 8192;

-- ============================================================
-- governance_audit: cross-product audit log surface (overrides, config changes)
-- Separate from events table so compliance queries don't wade through every
-- detection event when looking for accountability records.
-- ============================================================
CREATE TABLE IF NOT EXISTS bastio.governance_audit (
    audit_id           UUID DEFAULT generateUUIDv4(),
    customer_id        UUID,
    org_id             UUID,
    actor_user_id      String,
    action             LowCardinality(String),                   -- override.send | policy.changed | install.created | install.revoked
    resource_type      LowCardinality(String),
    resource_id        String,
    metadata           String DEFAULT '{}',                       -- JSON blob
    created_at         DateTime64(3, 'UTC') DEFAULT now64(3, 'UTC')
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (customer_id, created_at, audit_id)
TTL toDateTime(created_at) + INTERVAL 365 DAY DELETE
SETTINGS index_granularity = 8192;

-- ============================================================
-- governance_overview_daily (materialized view): pre-aggregated daily
-- counts by customer + severity + action. Powers the Governance Overview
-- dashboard tile without scanning the raw events table.
-- ============================================================
CREATE MATERIALIZED VIEW IF NOT EXISTS bastio.governance_overview_daily
ENGINE = SummingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (customer_id, day, severity, action)
AS SELECT
    customer_id,
    toDate(occurred_at) AS day,
    severity,
    action,
    count() AS event_count,
    uniqState(user_id) AS unique_users_state,
    uniqState(source_domain) AS unique_domains_state
FROM bastio.governance_events
GROUP BY customer_id, day, severity, action;
