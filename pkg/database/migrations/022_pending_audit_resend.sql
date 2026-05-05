-- 022_pending_audit_resend.sql
-- Phase 10.6: /v1/audit/resend lets a prospect who lost the activation
-- email request a fresh one. The endpoint rotates claim_token_hash on
-- the existing pending_audits row and re-sends the activation email.
--
-- Two reasons we need last_resend_at:
--   1. Rate limiting. /audit/resend is anonymous; without a per-row
--      cooldown an attacker could flood the prospect's inbox by
--      replaying the email N times. 60-second cooldown per audit.
--   2. Operator visibility. When a prospect emails support saying
--      "the link doesn't work", the admin can see whether they've been
--      bouncing through resend repeatedly.
--
-- The audit-ready sweep worker also rotates the token when sending the
-- 14-day email (so the ready email contains a working URL — the
-- raw claim token isn't stored, only its hash, so we can't re-emit
-- the original token after creation). It writes last_resend_at too;
-- the cooldown doesn't apply to sweep writes since the worker is
-- self-rate-limited at 24h periodicity.

ALTER TABLE pending_audits
    ADD COLUMN IF NOT EXISTS last_resend_at TIMESTAMPTZ;
