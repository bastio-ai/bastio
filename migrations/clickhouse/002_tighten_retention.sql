-- Bastio v2 ClickHouse — Tighten retention TTLs to 90 days
--
-- Aligns ClickHouse storage TTLs with the BAS-29 tier catalog's maximum
-- retention promise (90 days, Cloud Pro). Two tables in 001_initial_schema.sql
-- exceed this:
--
--   * bastio.security_threat_logs   : 180d -> 90d
--   * bastio.analytics_request_logs : 365d -> 90d
--
-- GDPR rationale: holding customer telemetry for up to a year while we only
-- promise 90 days creates an unbounded data-deletion liability. Enterprise
-- customers with custom retention will be handled out-of-band (per-table
-- overrides), not by leaving the default high for everyone.
--
-- ============================================================================
-- PRODUCTION-DATA RISK — READ BEFORE DEPLOYING
-- ============================================================================
-- ClickHouse's ALTER TABLE ... MODIFY TTL does NOT delete rows that are
-- already past the new TTL synchronously. The next background merge on each
-- affected partition will evict rows older than 90 days. In practice:
--
--   * On a live cluster, expect a wave of merges shortly after this migration
--     applies. Rows older than 90 days in the two tables above will be
--     dropped over the following minutes-to-hours, depending on table size
--     and merge scheduler load.
--   * These rows are already older than what any current tier promises to
--     surface in the dashboard, so customer-visible impact should be zero.
--   * No customer can claim the data was promised: the tier catalog (BAS-29)
--     advertises 90 days as the maximum.
--
-- RECOMMENDATION: ACCEPT THE LOSS. Do NOT back up >90d data. Reasoning:
--   1. No tier promises >90d retention, so no contract is broken.
--   2. Keeping a backup re-creates the GDPR liability we are removing.
--   3. Customers who want long-term retention should export via API
--      (analytics endpoints, SIEM/Datadog/etc. integrations) — that is the
--      supported escape hatch, not our ClickHouse cluster.
--
-- HUMAN FOLLOW-UPS BEFORE DEPLOY:
--   * Announce a 7-day window via in-app banner + email so any customer with
--     compliance/audit needs can export their >90d data.
--   * Update the privacy policy / DPA to state "max 90 days" explicitly for
--     these data classes (already promised by tier catalog, but make it
--     explicit at the data-class level).
--   * If anyone is on an Enterprise contract with bespoke retention, override
--     their tables individually AFTER this migration (separate ALTER per
--     customer-scoped data, or revisit when we land per-tenant TTLs).
-- ============================================================================

ALTER TABLE bastio.security_threat_logs
    MODIFY TTL toDateTime(detected_at) + INTERVAL 90 DAY;

ALTER TABLE bastio.analytics_request_logs
    MODIFY TTL toDateTime(timestamp) + INTERVAL 90 DAY;
