-- Detector toggles for the expanded security engine.
--
-- Adds per-detector enable flags for the four new detector classes
-- (secrets, indirect injection, output exfiltration, topic policy)
-- plus a preprocessing toggle (canonicalization). Each toggle follows
-- the existing pattern: `<detector>_enabled BOOLEAN`, optionally
-- paired with a threshold for score-based gating.
--
-- Defaults are conservative: every new detector is enabled so
-- existing customers get the uplift automatically. Topic policy is
-- off by default — it does nothing without per-customer
-- security_patterns rows and we don't want to waste a DB query per
-- request for users who haven't configured any.

ALTER TABLE security_profiles
    ADD COLUMN IF NOT EXISTS canonicalize_enabled       BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS secrets_enabled            BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS indirect_injection_enabled BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS output_exfil_enabled       BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS topic_policy_enabled       BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN security_profiles.canonicalize_enabled IS
    'Run unicode/homoglyph/base64/hex canonicalization before detectors. Typically on — single biggest defense against encoding-based bypasses.';
COMMENT ON COLUMN security_profiles.secrets_enabled IS
    'Detect API keys, cloud credentials, private keys in input. Distinct from PII — masks on match, never tokenizes.';
COMMENT ON COLUMN security_profiles.indirect_injection_enabled IS
    'Scan tool/retrieval/memory content (not user input) for embedded prompt injection. Stricter thresholds than the plain injection detector.';
COMMENT ON COLUMN security_profiles.output_exfil_enabled IS
    'Scan model responses for system prompt leaks, echoed secrets, and training-data regurgitation.';
COMMENT ON COLUMN security_profiles.topic_policy_enabled IS
    'Apply per-customer regex/keyword rules from security_patterns. Disabled by default until rules are configured.';
