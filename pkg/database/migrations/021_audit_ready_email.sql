-- 021_audit_ready_email.sql
-- The audit-ready sweep worker (audit.ready_sweep, see
-- internal/audit/ready_sweep.go) needs a way to know which pending
-- audits have already had their 14-day report email sent — otherwise
-- a daily cron would re-mail every active audit every day.
--
-- Sparse-indexed for the "outstanding" predicate the sweep query uses
-- (WHERE audit_ready_emailed_at IS NULL AND claimed_at IS NULL AND
-- created_at <= NOW() - INTERVAL '14 days'). Most rows in steady state
-- carry a non-null timestamp, so the partial index keeps the active
-- working set small.

ALTER TABLE pending_audits
    ADD COLUMN IF NOT EXISTS audit_ready_emailed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS pending_audits_outstanding_idx
    ON pending_audits (created_at)
    WHERE audit_ready_emailed_at IS NULL AND claimed_at IS NULL;
