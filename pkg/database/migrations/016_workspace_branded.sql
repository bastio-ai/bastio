-- 016_workspace_branded.sql
-- Branded chat — public end-user chat surface for Workspace.
-- Two address forms:
--   1) workspace.bastio.com/<slug>           (hosted, slug from path)
--   2) <customer-domain>                     (CNAME → bastio; resolved from Host header)
--
-- Custom-domain support is a Favio feature-parity requirement (memory:
-- feedback_url_subdomain_architecture). DNS verification is via TXT
-- record; the customer adds `bastio-verify=<token>` to their domain
-- and the server confirms via net.LookupTXT before marking verified.

-- Slug for the platform-hosted address. UNIQUE across the cluster so
-- it doubles as a stable workspace identifier in URLs.
ALTER TABLE workspace_settings
    ADD COLUMN IF NOT EXISTS slug TEXT;

-- Partial unique index — slug NULL is fine (workspace not yet branded).
CREATE UNIQUE INDEX IF NOT EXISTS workspace_settings_slug_uq
    ON workspace_settings (slug) WHERE slug IS NOT NULL;

-- Custom domains. One row per (customer, domain) pair. Multiple domains
-- per customer is supported — agencies often run a primary plus several
-- staging/preview domains.
CREATE TABLE IF NOT EXISTS workspace_domains (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id         UUID NOT NULL,
    domain              TEXT NOT NULL,
    verification_token  TEXT NOT NULL,                          -- value the customer puts in TXT record
    verified_at         TIMESTAMPTZ,
    last_checked_at     TIMESTAMPTZ,
    last_check_error    TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Domain is globally unique — two customers cannot claim the same host.
-- Lower-cased on write so the unique index isn't fooled by AcMe.com
-- vs acme.com.
CREATE UNIQUE INDEX IF NOT EXISTS workspace_domains_domain_uq
    ON workspace_domains (lower(domain));

CREATE INDEX IF NOT EXISTS workspace_domains_customer_idx
    ON workspace_domains (customer_id);

-- Updated_at trigger using the existing helper.
DROP TRIGGER IF EXISTS workspace_domains_set_updated_at ON workspace_domains;
CREATE TRIGGER workspace_domains_set_updated_at
    BEFORE UPDATE ON workspace_domains
    FOR EACH ROW EXECUTE FUNCTION workspace_set_updated_at();

-- Anonymous end-user sessions for the branded chat. Tied to a session
-- cookie (random 32-byte token). One row per visitor; cleanup runs on
-- the retention sweep.
CREATE TABLE IF NOT EXISTS workspace_branded_sessions (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id     UUID NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,                       -- SHA-256 of the cookie value
    user_label      TEXT,                                       -- optional name the visitor typed
    user_agent      TEXT,
    ip_hash         TEXT,                                       -- pseudonymized client IP (FNV-1a or SHA-256)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS workspace_branded_sessions_customer_idx
    ON workspace_branded_sessions (customer_id, last_seen_at DESC);
