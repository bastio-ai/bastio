-- 014_governance_scim.sql
-- SCIM 2.0 identity sync for Bastio Governance.
--
-- Customer's IdP (Okta, Microsoft Entra ID, Google Workspace, etc.) pushes
-- users + groups via SCIM. Extension events stamp user identifiers; the
-- governance pipeline enriches events with group/department membership
-- when present, enabling "by department" breakdowns in the pilot report.

-- Per-customer SCIM bearer tokens. One IdP integration per customer is
-- enough for v1; future versions can support multiple if needed.
CREATE TABLE IF NOT EXISTS governance_scim_tokens (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id     UUID NOT NULL UNIQUE,
    token_hash      TEXT NOT NULL,                          -- SHA-256 hex of bearer token
    label           TEXT NOT NULL DEFAULT '',
    last_used_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS governance_scim_tokens_active_idx
    ON governance_scim_tokens (token_hash)
    WHERE revoked_at IS NULL;

-- SCIM users. Mirrors Okta/Entra ID's standard core user attributes plus
-- the bits we actually use downstream (active flag, email, displayName).
CREATE TABLE IF NOT EXISTS governance_users (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id     UUID NOT NULL,
    external_id     TEXT,                                   -- IdP-supplied externalId (often the IdP's user UUID)
    user_name       TEXT NOT NULL,                          -- typically email or sAMAccountName
    email           TEXT,
    display_name    TEXT NOT NULL DEFAULT '',
    given_name      TEXT NOT NULL DEFAULT '',
    family_name     TEXT NOT NULL DEFAULT '',
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS governance_users_customer_username_idx
    ON governance_users (customer_id, user_name)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS governance_users_customer_email_idx
    ON governance_users (customer_id, email)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS governance_users_external_id_idx
    ON governance_users (customer_id, external_id)
    WHERE deleted_at IS NULL;

-- SCIM groups. Customers map their IdP groups to "departments" for the
-- pilot report breakdown.
CREATE TABLE IF NOT EXISTS governance_groups (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id     UUID NOT NULL,
    external_id     TEXT,
    display_name    TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS governance_groups_customer_displayname_idx
    ON governance_groups (customer_id, display_name)
    WHERE deleted_at IS NULL;

-- Membership join. SCIM PATCH/Add and PATCH/Remove flow through this table.
CREATE TABLE IF NOT EXISTS governance_user_group_memberships (
    customer_id     UUID NOT NULL,
    user_id         UUID NOT NULL,
    group_id        UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (customer_id, user_id, group_id)
);

CREATE INDEX IF NOT EXISTS governance_memberships_user_idx
    ON governance_user_group_memberships (customer_id, user_id);

CREATE INDEX IF NOT EXISTS governance_memberships_group_idx
    ON governance_user_group_memberships (customer_id, group_id);
