-- 018_auth.sql
-- Bastio Cloud authentication tables. Lives in the OSS migration runner
-- because that's the only migration story we have, but only cloud reads
-- + writes these — OSS deployments leave them empty.
--
-- Why no FK from customers.auth_user_id to auth_users.id: we want OSS
-- to be able to create customers freely without an auth user existing.
-- The link is one-way and validated at the application layer.
--
-- Lookup chain on a request: session_token (cookie) →
--   auth_sessions.token_hash → auth_sessions.user_id → customers row
--   where auth_user_id = user_id → request context customer_id.

CREATE TABLE IF NOT EXISTS auth_users (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    email           TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    password_hash   TEXT,                                       -- NULL when only OAuth-bound
    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at   TIMESTAMPTZ
);

-- Email is globally unique, case-insensitive. Lowercased on write.
CREATE UNIQUE INDEX IF NOT EXISTS auth_users_email_uq
    ON auth_users (lower(email));

-- Sessions: one row per active login. Token never stored — only its
-- SHA-256 hash. Sessions expire on a hard 30-day clock; refreshing
-- (every authenticated request) bumps last_seen_at but not expires_at.
-- Logout revokes by setting revoked_at; expired or revoked sessions
-- are kept for audit and pruned by a periodic sweep.
CREATE TABLE IF NOT EXISTS auth_sessions (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id         UUID NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,                       -- SHA-256 hex of cookie value
    user_agent      TEXT NOT NULL DEFAULT '',
    ip_hash         TEXT NOT NULL DEFAULT '',                   -- pseudonymized (SHA-256 of remote IP)
    expires_at      TIMESTAMPTZ NOT NULL,
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS auth_sessions_user_idx
    ON auth_sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS auth_sessions_active_idx
    ON auth_sessions (expires_at) WHERE revoked_at IS NULL;

-- OAuth account links. One user can have multiple linked providers
-- (we ship Google in MVP; Microsoft + GitHub are additions, not
-- migrations). provider_user_id is the upstream's stable subject id.
CREATE TABLE IF NOT EXISTS auth_oauth_accounts (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id           UUID NOT NULL,
    provider          TEXT NOT NULL CHECK (provider IN ('google','microsoft','github')),
    provider_user_id  TEXT NOT NULL,
    email             TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, provider_user_id)
);
CREATE INDEX IF NOT EXISTS auth_oauth_accounts_user_idx
    ON auth_oauth_accounts (user_id);

-- Link from a customer (workspace tenant) to its owning auth user.
-- Sparse-unique: most customers have exactly one owner; a customer
-- can exist without an auth_user_id (OSS single-tenant default).
ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS auth_user_id UUID;

CREATE UNIQUE INDEX IF NOT EXISTS customers_auth_user_uq
    ON customers (auth_user_id) WHERE auth_user_id IS NOT NULL;

-- updated_at trigger reuses the workspace helper if present, otherwise
-- declares its own — guards against migration order surprises.
CREATE OR REPLACE FUNCTION auth_set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS auth_users_set_updated_at ON auth_users;
CREATE TRIGGER auth_users_set_updated_at
    BEFORE UPDATE ON auth_users
    FOR EACH ROW EXECUTE FUNCTION auth_set_updated_at();
