-- Tenant Policy Overlays — additive customization on top of the core
-- security_profiles / security_patterns / security_access_rules tables.
--
-- The core tables remain the authoritative operator-level security
-- posture. An overlay is a tenant-owned, versioned, optional layer that
-- *adds* patterns, *adds* access rules, and *overrides* per-detector
-- thresholds and strategies. An overlay can never disable a core
-- detector — that capability is intentionally operator-only. Loosening
-- overrides (raising a threshold, weakening a strategy) are permitted
-- but logged at WARN by the merge layer so operators see them without
-- opening the UI.
--
-- Seeding: none. Zero customers get an overlay on deploy. Behavior is
-- identical to pre-migration until a tenant explicitly creates one.

-- Named overlay container, scoped to (customer, proxy). proxy_id NULL
-- means the overlay applies customer-wide — per-proxy overlays win
-- over customer-wide in the loader when both exist.
CREATE TABLE tenant_policy_overlays (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    proxy_id UUID REFERENCES proxies(id) ON DELETE RESTRICT,
    name TEXT NOT NULL DEFAULT 'default',
    active_version_id UUID,  -- FK added after versions table
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (customer_id, proxy_id, name)
);

CREATE INDEX idx_tenant_policy_overlays_customer
    ON tenant_policy_overlays(customer_id);

-- Immutable version snapshots. snapshot JSONB stores the delta only:
-- additional_patterns[], additional_access_rules[], detector_overrides{},
-- plugin_detectors[]. Never a copy of core config.
CREATE TABLE tenant_policy_overlay_versions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    overlay_id UUID NOT NULL REFERENCES tenant_policy_overlays(id) ON DELETE RESTRICT,
    customer_id UUID NOT NULL,
    version INT NOT NULL,
    state TEXT NOT NULL DEFAULT 'draft',
    snapshot JSONB NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    commit_message TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    shadow_started_at TIMESTAMPTZ,
    activated_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (overlay_id, version),
    CHECK (state IN ('draft', 'shadow', 'active', 'superseded'))
);

CREATE INDEX idx_overlay_versions_overlay
    ON tenant_policy_overlay_versions(overlay_id);
CREATE INDEX idx_overlay_versions_customer
    ON tenant_policy_overlay_versions(customer_id);
CREATE INDEX idx_overlay_versions_state
    ON tenant_policy_overlay_versions(overlay_id, state)
    WHERE state IN ('active', 'shadow');

-- DB-enforced: exactly one active version per overlay, at most one shadow.
-- Prevents any code path from producing two active versions via a race.
CREATE UNIQUE INDEX idx_overlay_versions_one_active
    ON tenant_policy_overlay_versions(overlay_id)
    WHERE state = 'active';
CREATE UNIQUE INDEX idx_overlay_versions_one_shadow
    ON tenant_policy_overlay_versions(overlay_id)
    WHERE state = 'shadow';

ALTER TABLE tenant_policy_overlays
    ADD CONSTRAINT fk_active_version
    FOREIGN KEY (active_version_id)
    REFERENCES tenant_policy_overlay_versions(id)
    ON DELETE SET NULL;

-- Append-only audit log. Every state change (create, shadow, activate,
-- rollback) inserts one row in the same transaction as the state change.
-- diff is a JSON Patch (RFC 6902) from the prior version; null on create.
CREATE TABLE tenant_policy_overlay_audit (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    overlay_id UUID NOT NULL REFERENCES tenant_policy_overlays(id) ON DELETE RESTRICT,
    customer_id UUID NOT NULL,
    version_id UUID REFERENCES tenant_policy_overlay_versions(id) ON DELETE RESTRICT,
    event TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    diff JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (event IN ('created', 'edited', 'shadowed', 'activated', 'rolled_back', 'deleted'))
);

CREATE INDEX idx_overlay_audit_overlay
    ON tenant_policy_overlay_audit(overlay_id, created_at DESC);
CREATE INDEX idx_overlay_audit_customer
    ON tenant_policy_overlay_audit(customer_id, created_at DESC);

-- Built-in starter templates. Global (no customer scope) — same snapshot
-- format as an overlay version. Populated in a later migration once we
-- settle the vertical-specific content.
CREATE TABLE tenant_policy_overlay_templates (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    snapshot JSONB NOT NULL,
    is_builtin BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Shadow mode event stream: divergences observed between the active
-- overlay and a shadow candidate. Populated asynchronously via River
-- (Phase 2) to keep the gateway request path clean.
CREATE TABLE tenant_policy_overlay_shadow_events (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL,
    overlay_id UUID NOT NULL REFERENCES tenant_policy_overlays(id) ON DELETE CASCADE,
    shadow_version_id UUID NOT NULL REFERENCES tenant_policy_overlay_versions(id) ON DELETE CASCADE,
    active_version_id UUID REFERENCES tenant_policy_overlay_versions(id) ON DELETE RESTRICT,
    trace_id UUID,
    divergence TEXT NOT NULL,
    active_action TEXT NOT NULL,
    shadow_action TEXT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_overlay_shadow_events_overlay
    ON tenant_policy_overlay_shadow_events(overlay_id, created_at DESC);
CREATE INDEX idx_overlay_shadow_events_customer
    ON tenant_policy_overlay_shadow_events(customer_id, created_at DESC);

-- updated_at triggers follow the style established in 001.
CREATE TRIGGER trg_tenant_policy_overlays_updated_at
    BEFORE UPDATE ON tenant_policy_overlays
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_tenant_policy_overlay_templates_updated_at
    BEFORE UPDATE ON tenant_policy_overlay_templates
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

COMMENT ON TABLE tenant_policy_overlays IS
    'Named, versioned tenant-owned overlay that adds rules and optional overrides on top of the core security configuration. Additive only — cannot disable core detectors.';
COMMENT ON COLUMN tenant_policy_overlay_versions.state IS
    'draft: editable, not enforced. shadow: runs in parallel, logs divergences. active: merged into effective profile per request. superseded: prior active, kept for rollback.';
COMMENT ON COLUMN tenant_policy_overlay_versions.snapshot IS
    'Delta-only JSON: additional_patterns[], additional_access_rules[], detector_overrides{}, plugin_detectors[]. Never a copy of core config. See internal/security/overlay/schema.go.';
