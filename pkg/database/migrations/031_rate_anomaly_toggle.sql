-- Rate-anomaly session detector toggle.
--
-- Adds the enable flag for the rate_anomaly session-aware detector
-- (request-rate burst detection within a session). Follows the
-- existing `<detector>_enabled BOOLEAN` pattern from 005.
--
-- Default is OFF: the detector needs session-scoped traffic
-- (X-Bastio-Session-Id) to be meaningful, and existing deployments
-- must not start emitting anomaly warns after an upgrade. Operators
-- opt in per profile from the dashboard.

ALTER TABLE security_profiles
    ADD COLUMN IF NOT EXISTS rate_anomaly_enabled BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN security_profiles.rate_anomaly_enabled IS
    'Flag session request-rate bursts that exceed the session''s own trailing baseline. Needs X-Bastio-Session-Id; off by default.';
