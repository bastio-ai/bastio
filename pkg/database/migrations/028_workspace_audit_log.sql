-- 028_workspace_audit_log.sql
--
-- Durable audit trail for every privileged workspace action. RBAC
-- (migration 027 + handler middleware) enforces who CAN do things;
-- this migration records who DID things, when, from where, against
-- what target. Required for SOC2 / ISO27001 type compliance reviews
-- and for incident response after a "wait, who removed Sarah?".
--
-- Snapshots over joins: actor_email, actor_role, target_label are
-- captured at write time. Member rows get deleted, emails change,
-- roles get demoted — joining the live tables would surface the
-- CURRENT state, not the state when the action happened. The audit
-- log is a historical record; pin the values at write time and don't
-- look back.
--
-- metadata JSONB carries action-specific detail: old/new role for
-- member.role_changed, budget values for member.budgets_changed,
-- assistant fields for assistant.updated, etc. No schema for the
-- metadata — different actions carry different shapes.
--
-- Retention: not enforced here. A separate retention sweep can drop
-- rows older than the customer's retention_days from
-- workspace_settings — but compliance reviewers typically expect
-- multi-year retention, so default to "keep forever" until the
-- customer explicitly requests truncation.

CREATE TABLE IF NOT EXISTS workspace_audit_log (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id     UUID NOT NULL,
    actor_user_id   TEXT NOT NULL,                              -- whose session did this
    actor_email     TEXT NOT NULL,                              -- snapshot
    actor_role      TEXT NOT NULL,                              -- snapshot — see header
    action          TEXT NOT NULL,                              -- e.g. 'member.role_changed'
    target_type     TEXT NOT NULL DEFAULT '',                   -- 'member' | 'assistant' | ...
    target_id       TEXT NOT NULL DEFAULT '',                   -- target's user_id / uuid
    target_label    TEXT NOT NULL DEFAULT '',                   -- snapshot — email / name
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,         -- old/new fields, etc.
    ip_address      TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Customer-scoped reverse-chronological access — every list query
-- on the audit page hits this index. Composite (customer_id,
-- created_at DESC) so pagination by created_at works without a
-- second sort. Adding id makes the index a covering one for the
-- common SELECT shape.
CREATE INDEX IF NOT EXISTS workspace_audit_log_customer_time_idx
    ON workspace_audit_log (customer_id, created_at DESC, id);

-- Action filtering — useful for "show me every role change in the
-- last 30 days" type queries. Cheap to add up-front since the
-- column is highly selective.
CREATE INDEX IF NOT EXISTS workspace_audit_log_customer_action_idx
    ON workspace_audit_log (customer_id, action, created_at DESC);
