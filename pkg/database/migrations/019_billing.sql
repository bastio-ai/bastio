-- 019_billing.sql
-- Bastio Cloud subscription state. Stripe is source of truth — this
-- table mirrors enough of it that the dashboard can answer "am I
-- paid?" without round-tripping to Stripe on every request.
--
-- The reconciliation flow:
--   1. Customer clicks "Activate Workspace" → server creates a
--      Stripe Checkout Session with quantity = seat count
--   2. Customer completes checkout → Stripe sends checkout.session.completed
--      webhook → we INSERT a row here with status='active'
--   3. Customer changes seats / cancels via the Stripe portal →
--      customer.subscription.updated webhook → we UPDATE the row
--   4. Period ends without renewal → customer.subscription.deleted →
--      we UPDATE status='canceled'
--
-- billing_event_log keeps every webhook by stripe_event_id for replay
-- safety and idempotency. Stripe sometimes redelivers events; matching
-- on event_id prevents double-processing.
--
-- Lives in OSS migrations because the migration runner is OSS-owned;
-- only cloud reads + writes these rows. OSS deployments leave them
-- empty.

CREATE TABLE IF NOT EXISTS subscriptions (
    id                       UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id              UUID NOT NULL UNIQUE,                  -- one active subscription per customer
    stripe_customer_id       TEXT NOT NULL,
    stripe_subscription_id   TEXT NOT NULL UNIQUE,
    status                   TEXT NOT NULL                          -- mirrors Stripe: active, trialing, past_due, canceled, unpaid, incomplete
                             CHECK (status IN ('active','trialing','past_due','canceled','unpaid','incomplete','incomplete_expired')),
    price_id                 TEXT NOT NULL,                          -- Stripe Price ID (which plan)
    seat_count               INTEGER NOT NULL DEFAULT 1,             -- subscription.items[0].quantity
    current_period_start     TIMESTAMPTZ NOT NULL,
    current_period_end       TIMESTAMPTZ NOT NULL,
    cancel_at_period_end     BOOLEAN NOT NULL DEFAULT FALSE,
    canceled_at              TIMESTAMPTZ,
    last_event_id            TEXT,                                   -- last applied Stripe event id (drift detection)
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS subscriptions_status_idx
    ON subscriptions (status, current_period_end);

-- Webhook event log. Every received Stripe event lands here once
-- (idempotent on stripe_event_id) so redelivered events don't
-- double-update the subscriptions row.
CREATE TABLE IF NOT EXISTS billing_event_log (
    stripe_event_id   TEXT PRIMARY KEY,
    event_type        TEXT NOT NULL,
    received_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload           JSONB NOT NULL                                  -- whole event for audit/replay
);

CREATE INDEX IF NOT EXISTS billing_event_log_received_idx
    ON billing_event_log (received_at DESC);

-- updated_at trigger reuses the existing helper if defined, else
-- declares its own — the auth helper from 018 is sufficient but the
-- workspace_set_updated_at and auth_set_updated_at functions are both
-- in scope, so use a local one to keep this migration self-contained.
CREATE OR REPLACE FUNCTION billing_set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS subscriptions_set_updated_at ON subscriptions;
CREATE TRIGGER subscriptions_set_updated_at
    BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION billing_set_updated_at();
