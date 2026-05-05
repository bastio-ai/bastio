-- Playground run history.
--
-- Persists prompts executed from the dashboard Playground so developers
-- can scroll back through their recent experiments and click to replay.
-- Kept deliberately separate from traces: the Traces view tracks real
-- production agent calls, and mixing synthetic test prompts into it
-- would skew threat counts, latency percentiles, and cost dashboards.
--
-- Access is always scoped by customer_id (multi-tenancy rule). Cloud
-- layers user attribution on top via created_by when available.

CREATE TABLE playground_runs (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,

    -- Context — which profile/proxy the run targeted. Both nullable
    -- because the playground supports ad-hoc step lists and the
    -- "Any (customer default)" proxy selection.
    profile_name TEXT NOT NULL,
    proxy_id UUID REFERENCES proxies(id) ON DELETE SET NULL,
    direction TEXT NOT NULL CHECK (direction IN ('input', 'output')),

    -- Input + engine output. prompt is plain text so replays work
    -- without further joins. sanitized_content is the value the model
    -- would have seen — same as the playground "After" pane.
    prompt TEXT NOT NULL,
    sanitized_content TEXT NOT NULL,

    -- Aggregate decision. Mirrors DetectResponse.action / should_block.
    action TEXT NOT NULL,
    should_block BOOLEAN NOT NULL,

    -- Which detectors actually fired. Denormalized for cheap filtering
    -- in the list view ("show me runs where injection fired").
    fired_detectors TEXT[] NOT NULL DEFAULT '{}',

    -- Full per-step result, matching DetectStepResult. JSONB so the
    -- replay UI can reconstruct the trace without re-running detection.
    steps JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- Wall-clock run latency. Duration column in nanoseconds to match
    -- the engine's native unit; format on display.
    duration_ns BIGINT NOT NULL DEFAULT 0,

    -- Optional user attribution (set by cloud). OSS leaves this null.
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot path: list recent runs for a customer. DESC created_at matches
-- the UI's "most recent first" order.
CREATE INDEX idx_playground_runs_customer_created
    ON playground_runs (customer_id, created_at DESC);

-- Filter by fired detector (e.g. "show runs where jailbreak fired").
CREATE INDEX idx_playground_runs_fired_detectors
    ON playground_runs USING GIN (fired_detectors);

COMMENT ON TABLE playground_runs IS
    'History of prompts executed from the dashboard Playground. Deliberately isolated from traces so synthetic test activity does not pollute production analytics.';
