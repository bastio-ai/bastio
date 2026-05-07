-- trace_scores: post-hoc scoring and labeling of traces.
-- Supports both numeric scores ("accuracy"=0.82) and categorical labels
-- ("sentiment"="positive"). One row per score per trace. Multiple
-- evaluators may score the same trace; the (trace_id, name, evaluator)
-- triple is the natural key.
CREATE TABLE IF NOT EXISTS bastio.trace_scores (
    id UUID,
    trace_id UUID,
    customer_id UUID,
    name String,                    -- "accuracy", "helpfulness", "sentiment"
    value_type String,              -- "numeric" | "categorical" | "boolean"
    numeric_value Nullable(Float64),
    string_value String DEFAULT '', -- categorical or boolean ("true"/"false")
    comment String DEFAULT '',
    evaluator String DEFAULT '',    -- "human:daniel@acme.com", "llm:gpt-4o", "rule:..."
    created_at DateTime64(3, 'UTC'),

    INDEX idx_trace_id trace_id TYPE bloom_filter() GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(created_at)
ORDER BY (customer_id, trace_id, name, created_at)
TTL toDateTime(created_at) + INTERVAL 365 DAY;
