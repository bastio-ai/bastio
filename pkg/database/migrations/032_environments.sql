-- Managed deployment environments used by the dashboard header and filters.
-- Observed trace environment values remain valid even when they have not yet
-- been adopted into this registry.

CREATE TABLE environments (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'custom'
        CHECK (kind IN ('production', 'staging', 'development', 'custom')),
    description TEXT NOT NULL DEFAULT '',
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX environments_customer_name_key
    ON environments (customer_id, lower(name));

CREATE INDEX environments_customer_created_idx
    ON environments (customer_id, created_at);
