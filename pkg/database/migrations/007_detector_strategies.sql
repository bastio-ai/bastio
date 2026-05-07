-- Per-detector strategy + explicit threshold storage.
--
-- Today the engine hardcodes what happens when each detector fires:
-- injection → block, jailbreak → warn, secrets → mask, etc. That works
-- for the opinionated default, but customers with different risk
-- appetites have no way to override. A consumer chatbot wants
-- jailbreak → warn (UX over strictness). A regulated enterprise wants
-- jailbreak → block (compliance over UX). Same engine, different policy.
--
-- This migration adds a `<detector>_strategy` column per toggleable
-- detector with a CHECK constraint limiting each to the strategy subset
-- that actually makes sense for that detector:
--
--   * Text-threat detectors (injection, jailbreak, indirect_injection,
--     output_exfil) — block | warn | log_only. No mask, because there's
--     nothing to sanitize: the threat is a request or response whose
--     meaning is the threat.
--
--   * Secrets — block | mask | warn | log_only. No tokenize, because
--     tokenizing a secret would make it recoverable in the response
--     restore path, which defeats the point.
--
--   * PII — already has pii_action; left alone.
--   * Topic policy — strategy lives per-rule in security_patterns.action.
--
-- Defaults match today's hardcoded behavior so rolling the migration
-- has zero behavior change until someone edits a profile.

ALTER TABLE security_profiles
    ADD COLUMN IF NOT EXISTS injection_strategy          TEXT NOT NULL DEFAULT 'block',
    ADD COLUMN IF NOT EXISTS jailbreak_strategy          TEXT NOT NULL DEFAULT 'warn',
    ADD COLUMN IF NOT EXISTS secrets_strategy            TEXT NOT NULL DEFAULT 'mask',
    ADD COLUMN IF NOT EXISTS indirect_injection_strategy TEXT NOT NULL DEFAULT 'block',
    ADD COLUMN IF NOT EXISTS output_exfil_strategy       TEXT NOT NULL DEFAULT 'block';

ALTER TABLE security_profiles
    ADD CONSTRAINT security_profiles_injection_strategy_valid
        CHECK (injection_strategy IN ('block', 'warn', 'log_only'));

ALTER TABLE security_profiles
    ADD CONSTRAINT security_profiles_jailbreak_strategy_valid
        CHECK (jailbreak_strategy IN ('block', 'warn', 'log_only'));

ALTER TABLE security_profiles
    ADD CONSTRAINT security_profiles_secrets_strategy_valid
        CHECK (secrets_strategy IN ('block', 'mask', 'warn', 'log_only'));

ALTER TABLE security_profiles
    ADD CONSTRAINT security_profiles_indirect_injection_strategy_valid
        CHECK (indirect_injection_strategy IN ('block', 'warn', 'log_only'));

ALTER TABLE security_profiles
    ADD CONSTRAINT security_profiles_output_exfil_strategy_valid
        CHECK (output_exfil_strategy IN ('block', 'warn', 'log_only'));

COMMENT ON COLUMN security_profiles.injection_strategy IS
    'What happens when the injection step fires: block | warn | log_only. Default block.';
COMMENT ON COLUMN security_profiles.jailbreak_strategy IS
    'What happens when the jailbreak step fires: block | warn | log_only. Default warn (jailbreak signals are broader and more prone to false positives).';
COMMENT ON COLUMN security_profiles.secrets_strategy IS
    'What happens when the secrets step fires: block | mask | warn | log_only. Default mask — safer than block for developer ergonomics, never tokenize (tokens must not round-trip).';
COMMENT ON COLUMN security_profiles.indirect_injection_strategy IS
    'What happens when the indirect_injection step fires on tool/retrieval/memory content. Default block — this is a trust-boundary attack and warrants strict handling.';
COMMENT ON COLUMN security_profiles.output_exfil_strategy IS
    'What happens when the response-side exfil step fires. Default block — leaked system prompts and echoed secrets must not reach the end user.';
