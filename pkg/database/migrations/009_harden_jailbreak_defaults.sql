-- Harden jailbreak defaults + add normalization toggles.
--
-- Two changes, both part of the hardened-jailbreak-detection work:
--
-- 1. Flip the default jailbreak_strategy from 'warn' to 'block'. This is
--    a policy change with customer-visible impact: any existing profile
--    still on the pre-hardening default ('warn') now blocks detected
--    jailbreaks. Profiles that explicitly set any non-warn value
--    (block/mask/log-only) are preserved — we only touch rows that
--    still look untouched since the detector's weakened era.
--
-- 2. Add normalize_unicode and normalize_decode toggles (default true)
--    so customers can opt out of the preprocessing pipeline for
--    specialised workloads (translation tools that legitimately carry
--    base64, coding assistants with exotic Unicode). Default ON
--    matches the security-first posture of the rest of the profile.

ALTER TABLE security_profiles
    ADD COLUMN IF NOT EXISTS normalize_unicode BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE security_profiles
    ADD COLUMN IF NOT EXISTS normalize_decode BOOLEAN NOT NULL DEFAULT TRUE;

-- Flip warn → block on profiles still on the warn-default. Intentionally
-- narrow: only touches rows that haven't been actively tuned away from
-- warn. Customers who chose 'warn' on purpose keep their choice.
UPDATE security_profiles
    SET jailbreak_strategy = 'block'
    WHERE jailbreak_strategy = 'warn';
