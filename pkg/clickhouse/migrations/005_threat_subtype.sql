-- Add threat_subtype + source columns to security_threat_logs.
-- threat_subtype carries the dotted taxonomy slot ("persona.dan",
-- "extraction.meta", "encoding.base64") that the hardened jailbreak
-- detector emits. Older rows have empty strings — detectors that
-- haven't adopted the taxonomy yet leave the field blank and the
-- dashboard filter ignores empty values.
--
-- source is a provenance note for the pattern (paper, advisory,
-- disclosure), surfaced in the detail view. Never used for matching.
--
-- Both columns are LowCardinality-friendly (tiny value space), so we
-- index threat_subtype with a bloom filter to keep filter queries
-- fast without a partition rewrite.
ALTER TABLE bastio.security_threat_logs
    ADD COLUMN IF NOT EXISTS threat_subtype String DEFAULT '';

ALTER TABLE bastio.security_threat_logs
    ADD COLUMN IF NOT EXISTS source String DEFAULT '';

ALTER TABLE bastio.security_threat_logs
    ADD INDEX IF NOT EXISTS idx_threat_subtype threat_subtype TYPE bloom_filter GRANULARITY 1;
