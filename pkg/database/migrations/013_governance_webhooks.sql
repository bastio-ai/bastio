-- 013_governance_webhooks.sql
-- Customer-defined webhook endpoints fired on high-severity governance events.
-- One row per (customer, name); soft-deleted via deleted_at so audit history
-- survives a remove-and-recreate.

CREATE TABLE IF NOT EXISTS governance_webhooks (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id    UUID NOT NULL,
    name           TEXT NOT NULL,
    url            TEXT NOT NULL,
    format         TEXT NOT NULL CHECK (format IN ('slack','teams','raw_json')),
    trigger        TEXT NOT NULL CHECK (trigger IN ('severity:high','action:overridden','any')),
    last_fired_at  TIMESTAMPTZ,
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS governance_webhooks_customer_idx
    ON governance_webhooks (customer_id)
    WHERE deleted_at IS NULL;

-- Per-customer domain overrides for the public AI domain list.
-- Server merges these with the bundled default list at /domain-list response time.
CREATE TABLE IF NOT EXISTS governance_domain_overrides (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id    UUID NOT NULL,
    domain         TEXT NOT NULL,
    label          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS governance_domain_overrides_customer_domain_idx
    ON governance_domain_overrides (customer_id, domain);
