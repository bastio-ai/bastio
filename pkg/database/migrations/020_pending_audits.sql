-- 020_pending_audits.sql
-- Anonymous Shadow AI Audit prospects. The wedge GTM motion needs to
-- run the 14-day audit BEFORE asking the prospect to sign up — that's
-- what makes the report a foregone conclusion that paying customers
-- have already de-risked. This migration adds the data model that
-- bridges "no auth user yet, but events are flowing" → "auth user
-- claimed the pending audit, customer row inherits the events".
--
-- Lifecycle:
--   1. POST /v1/audit/start creates a customer row (placeholder, no
--      auth_user_id), a governance_installations row, and a
--      pending_audits row tracking the token contracts.
--   2. The same handler generates an MDM bundle scoped to that
--      customer + installation, and emails the contact a link with a
--      one-shot bundle download token.
--   3. Events flow into governance_events keyed off the customer_id —
--      identical to a logged-in customer's audit.
--   4. After 14 days, an audit_ready email goes out with a claim_token
--      activation URL.
--   5. Prospect clicks → signs up → cloud's /auth/claim/{token} does a
--      transactional bind: sets auth_user_id on the existing customer
--      row + marks pending_audits.claimed_at. Events the customer
--      already has now show up in their authenticated dashboard.

CREATE TABLE IF NOT EXISTS pending_audits (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id         UUID NOT NULL UNIQUE,                    -- placeholder customer; one pending audit per customer
    claim_token_hash    TEXT NOT NULL UNIQUE,                    -- SHA-256 hex of the activation token
    bundle_token_hash   TEXT NOT NULL UNIQUE,                    -- SHA-256 hex of the one-shot bundle download token
    bundle_used_at      TIMESTAMPTZ,                             -- bundle is one-shot; second download attempt 410s
    contact_email       TEXT NOT NULL,
    contact_name        TEXT NOT NULL DEFAULT '',
    company_name        TEXT NOT NULL DEFAULT '',
    mdm_format          TEXT NOT NULL DEFAULT 'chrome'           -- chrome | intune | jamf
                        CHECK (mdm_format IN ('chrome','intune','jamf')),
    expires_at          TIMESTAMPTZ NOT NULL,                    -- claim token expires after 60 days
    claimed_at          TIMESTAMPTZ,                             -- non-null after /auth/claim succeeds
    claimed_by_user_id  UUID,                                    -- the auth_users.id that claimed this audit
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS pending_audits_email_idx
    ON pending_audits (lower(contact_email));

-- The reverse link from a customer to its originating audit is useful
-- for the dashboard's "your governance audit converted to this
-- workspace" celebratory copy. Sparse: not every customer comes from
-- an audit (Google OAuth signups skip this path entirely).
ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS pending_audit_id UUID;

CREATE INDEX IF NOT EXISTS customers_pending_audit_idx
    ON customers (pending_audit_id) WHERE pending_audit_id IS NOT NULL;
