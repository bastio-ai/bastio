-- 012_governance_installations.sql
-- Per-org credential rows for the Bastio Governance browser extension.
-- One row per IT-generated MDM bundle. The installation_token (hash stored)
-- identifies the org; installation_secret is the HKDF root for per-install
-- HMAC keys. Rotated independently of the token.

CREATE TABLE IF NOT EXISTS governance_installations (
    id                       UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id              UUID NOT NULL,
    org_id                   UUID NOT NULL UNIQUE,                  -- external identifier exposed in MDM bundle
    installation_token_hash  TEXT NOT NULL,                          -- SHA-256 hex of bearer token
    installation_secret      TEXT NOT NULL,                          -- HKDF root, base64url; encrypt at rest in cloud
    label                    TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at               TIMESTAMPTZ,
    rotation_grace_until     TIMESTAMPTZ,                            -- 24h grace after secret rotation
    previous_secret          TEXT                                    -- only valid until rotation_grace_until
);

CREATE INDEX IF NOT EXISTS governance_installations_customer_idx
    ON governance_installations (customer_id)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS governance_installations_active_idx
    ON governance_installations (org_id)
    WHERE revoked_at IS NULL;

-- Per-org policy overrides. Defaults are baked into the handler; rows here
-- override per customer. Keeps the v1.1 path clear without forcing a schema
-- change later.
CREATE TABLE IF NOT EXISTS governance_policies (
    customer_id      UUID PRIMARY KEY,
    severity_low     TEXT NOT NULL DEFAULT 'log'             CHECK (severity_low IN ('log','warn','block_redirect')),
    severity_medium  TEXT NOT NULL DEFAULT 'warn'            CHECK (severity_medium IN ('log','warn','block_redirect')),
    severity_high    TEXT NOT NULL DEFAULT 'block_redirect'  CHECK (severity_high IN ('log','warn','block_redirect')),
    custom_keywords  JSONB NOT NULL DEFAULT '[]'::jsonb,
    custom_regex_packs JSONB NOT NULL DEFAULT '[]'::jsonb,
    redirect_target  JSONB,
    override_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    pseudonymize_pii BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Override audit trail. governance_events in CH carries action='overridden'
-- + justification, but compliance teams want the cross-product audit table
-- to also reflect this. PG row is authoritative for IT operator UI; CH row
-- powers analytics.
CREATE TABLE IF NOT EXISTS governance_overrides (
    id                     UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id            UUID NOT NULL,
    org_id                 UUID NOT NULL,
    user_id                TEXT NOT NULL,
    event_id               TEXT NOT NULL,
    severity               TEXT NOT NULL CHECK (severity IN ('low','medium','high')),
    rule_ids               JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_domain          TEXT NOT NULL,
    justification          TEXT NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS governance_overrides_customer_idx
    ON governance_overrides (customer_id, created_at DESC);
