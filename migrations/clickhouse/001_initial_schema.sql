-- Bastio v2 ClickHouse Initial Schema
-- Analytics and observability tables optimized for time-series queries

CREATE DATABASE IF NOT EXISTS bastio;

-- Traces: top-level request traces
CREATE TABLE IF NOT EXISTS bastio.traces (
    id UUID,
    customer_id UUID,
    proxy_id UUID,
    api_key_id UUID,

    -- Request metadata
    method String,
    path String,
    provider String,
    model String,

    -- Timing
    started_at DateTime64(3, 'UTC'),
    completed_at DateTime64(3, 'UTC'),
    duration_ms UInt32,

    -- Tokens & cost
    input_tokens UInt32 DEFAULT 0,
    output_tokens UInt32 DEFAULT 0,
    total_tokens UInt32 DEFAULT 0,
    cost_cents Float64 DEFAULT 0,

    -- Status
    status String DEFAULT 'ok', -- ok, error, blocked, rate_limited
    error_message String DEFAULT '',
    http_status UInt16 DEFAULT 200,

    -- Security
    threat_detected Bool DEFAULT false,
    threat_types Array(String) DEFAULT [],
    threat_score Float32 DEFAULT 0,
    security_action String DEFAULT 'pass', -- pass, block, warn, redact

    -- User context (end-user of the AI app, not Bastio user)
    end_user_id String DEFAULT '',
    session_id String DEFAULT '',

    -- Request/response content (for trace detail view)
    request_body String DEFAULT '',
    response_body String DEFAULT '',

    -- Metadata
    tags Map(String, String) DEFAULT map(),

    INDEX idx_threat_detected threat_detected TYPE minmax GRANULARITY 1,
    INDEX idx_status status TYPE set(0) GRANULARITY 1,
    INDEX idx_provider provider TYPE set(0) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(started_at)
ORDER BY (customer_id, started_at, id)
TTL toDateTime(started_at) + INTERVAL 90 DAY;

-- Observations: spans within a trace (generation, tool call, retrieval, etc.)
CREATE TABLE IF NOT EXISTS bastio.observations (
    id UUID,
    trace_id UUID,
    parent_id Nullable(UUID),
    customer_id UUID,

    -- Span info
    type String, -- generation, span, event, tool, retrieval, embedding, guardrail, agent
    name String DEFAULT '',
    depth UInt8 DEFAULT 0,

    -- Timing
    started_at DateTime64(3, 'UTC'),
    completed_at DateTime64(3, 'UTC'),
    duration_ms UInt32,

    -- Tokens (for generation type)
    input_tokens UInt32 DEFAULT 0,
    output_tokens UInt32 DEFAULT 0,
    model String DEFAULT '',

    -- Content
    input String DEFAULT '',
    output String DEFAULT '',
    metadata Map(String, String) DEFAULT map(),

    -- Status
    status String DEFAULT 'ok',
    error_message String DEFAULT '',

    INDEX idx_type type TYPE set(0) GRANULARITY 1,
    INDEX idx_trace_id trace_id TYPE bloom_filter() GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(started_at)
ORDER BY (customer_id, trace_id, started_at, id)
TTL toDateTime(started_at) + INTERVAL 90 DAY;

-- Security threat logs: detailed threat detection events
CREATE TABLE IF NOT EXISTS bastio.security_threat_logs (
    id UUID,
    trace_id UUID,
    customer_id UUID,
    proxy_id UUID,

    -- Threat info
    threat_type String, -- injection, pii, jailbreak, bot, rate_anomaly, custom_pattern
    severity String, -- low, medium, high, critical
    score Float32,
    action_taken String, -- pass, block, warn, redact

    -- Detection details
    detector_name String,
    matched_pattern String DEFAULT '',
    matched_content String DEFAULT '',
    confidence Float32 DEFAULT 0,

    -- Context
    end_user_id String DEFAULT '',
    ip_address String DEFAULT '',
    user_agent String DEFAULT '',

    -- Metadata
    details Map(String, String) DEFAULT map(),

    detected_at DateTime64(3, 'UTC'),

    INDEX idx_threat_type threat_type TYPE set(0) GRANULARITY 1,
    INDEX idx_severity severity TYPE set(0) GRANULARITY 1,
    INDEX idx_trace_id trace_id TYPE bloom_filter() GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(detected_at)
ORDER BY (customer_id, detected_at, id)
TTL toDateTime(detected_at) + INTERVAL 180 DAY;

-- Analytics request logs: lightweight aggregation-friendly request log
CREATE TABLE IF NOT EXISTS bastio.analytics_request_logs (
    customer_id UUID,
    proxy_id UUID,

    timestamp DateTime64(3, 'UTC'),

    provider String,
    model String,

    input_tokens UInt32 DEFAULT 0,
    output_tokens UInt32 DEFAULT 0,
    cost_cents Float64 DEFAULT 0,
    duration_ms UInt32,

    status String DEFAULT 'ok',
    threat_detected Bool DEFAULT false,

    end_user_id String DEFAULT '',

    INDEX idx_provider provider TYPE set(0) GRANULARITY 1,
    INDEX idx_status status TYPE set(0) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (customer_id, proxy_id, timestamp)
TTL toDateTime(timestamp) + INTERVAL 365 DAY;

-- Bot detections: fingerprinting and behavioral signals
CREATE TABLE IF NOT EXISTS bastio.bot_detections (
    id UUID,
    trace_id UUID,
    customer_id UUID,

    -- Detection
    detected_at DateTime64(3, 'UTC'),
    bot_type String DEFAULT '', -- automated, scripted, replay, unknown
    confidence Float32,

    -- Fingerprint
    ip_address String DEFAULT '',
    user_agent String DEFAULT '',
    fingerprint_hash String DEFAULT '',

    -- Behavioral signals
    request_rate_1m Float32 DEFAULT 0,
    pattern_regularity Float32 DEFAULT 0,
    signals Map(String, String) DEFAULT map(),

    INDEX idx_bot_type bot_type TYPE set(0) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(detected_at)
ORDER BY (customer_id, detected_at, id)
TTL toDateTime(detected_at) + INTERVAL 90 DAY;

-- Materialized view: hourly request aggregates for dashboard
CREATE TABLE IF NOT EXISTS bastio.request_stats_hourly (
    customer_id UUID,
    proxy_id UUID,
    hour DateTime,

    request_count UInt64,
    error_count UInt64,
    threat_count UInt64,
    blocked_count UInt64,

    total_input_tokens UInt64,
    total_output_tokens UInt64,
    total_cost_cents Float64,

    avg_duration_ms Float64,
    p50_duration_ms Float64,
    p95_duration_ms Float64,
    p99_duration_ms Float64
)
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (customer_id, proxy_id, hour);

CREATE MATERIALIZED VIEW IF NOT EXISTS bastio.mv_request_stats_hourly
TO bastio.request_stats_hourly AS
SELECT
    customer_id,
    proxy_id,
    toStartOfHour(timestamp) AS hour,
    count() AS request_count,
    countIf(status != 'ok') AS error_count,
    countIf(threat_detected = true) AS threat_count,
    countIf(status = 'blocked') AS blocked_count,
    sum(input_tokens) AS total_input_tokens,
    sum(output_tokens) AS total_output_tokens,
    sum(cost_cents) AS total_cost_cents,
    avg(duration_ms) AS avg_duration_ms,
    quantile(0.5)(duration_ms) AS p50_duration_ms,
    quantile(0.95)(duration_ms) AS p95_duration_ms,
    quantile(0.99)(duration_ms) AS p99_duration_ms
FROM bastio.analytics_request_logs
GROUP BY customer_id, proxy_id, hour;
