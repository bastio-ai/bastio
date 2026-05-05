-- PII handling modes.
--
-- Extends security_profiles with the knobs needed for reversible PII
-- redaction ("tokenize" mode) and response-side scanning.
--
-- The original pii_action allowed 'block' | 'redact' | 'warn' | 'log'.
-- 'redact' was a lossy one-way mask. It is renamed to 'mask' and a new
-- 'tokenize' mode is added that uses reversible placeholders so the
-- gateway can restore originals in the LLM response when the caller
-- owns the data (e.g. the end user looking up their own record).

-- Migrate existing 'redact' rows to the new canonical name first so the
-- CHECK constraint below accepts them.
UPDATE security_profiles SET pii_action = 'mask' WHERE pii_action = 'redact';
UPDATE security_profiles SET pii_action = 'log_only' WHERE pii_action = 'log';

ALTER TABLE security_profiles
    ADD COLUMN IF NOT EXISTS pii_scan_response    BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS pii_restore_response BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS pii_token_style      TEXT    NOT NULL DEFAULT 'angle';

ALTER TABLE security_profiles
    ALTER COLUMN pii_action SET DEFAULT 'mask';

ALTER TABLE security_profiles
    ADD CONSTRAINT security_profiles_pii_action_valid
        CHECK (pii_action IN ('mask', 'tokenize', 'block', 'warn', 'log_only'));

ALTER TABLE security_profiles
    ADD CONSTRAINT security_profiles_pii_token_style_valid
        CHECK (pii_token_style IN ('angle', 'curly'));

COMMENT ON COLUMN security_profiles.pii_action IS
    'How to handle detected PII: mask (lossy) | tokenize (reversible) | block | warn | log_only';
COMMENT ON COLUMN security_profiles.pii_scan_response IS
    'When true, also scan the LLM response for PII that was not in the request (hallucination / RAG leak).';
COMMENT ON COLUMN security_profiles.pii_restore_response IS
    'When true and pii_action=tokenize, swap placeholders back to originals before returning the response to the caller.';
COMMENT ON COLUMN security_profiles.pii_token_style IS
    'Placeholder shape when pii_action=tokenize: angle (<PII_SSN_1>) or curly ({{PII_SSN_1}}).';
