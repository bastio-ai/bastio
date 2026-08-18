-- Per-profile false-positive skips.
--
-- Operators allow a noisy detector pattern from a threat row. The engine
-- drops matching findings before strategy evaluation so the next request
-- is not flagged. Distinct from overlay additional_patterns (which add
-- rules) and from security_patterns (topic-policy keywords).

CREATE TABLE security_suppressions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    detector TEXT NOT NULL,
    pattern TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, profile_id, detector, pattern)
);

CREATE INDEX security_suppressions_profile_idx
    ON security_suppressions (customer_id, profile_id);

COMMENT ON TABLE security_suppressions IS
    'Tenant-owned detector pattern skips. Findings whose detector and matched_pattern (or subcategory / matched_content) match a row are dropped before the profile strategy runs.';
