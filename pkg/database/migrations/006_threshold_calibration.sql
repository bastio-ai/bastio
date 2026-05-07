-- Threshold calibration for the jailbreak detector.
--
-- The original jailbreak_threshold of 0.8 predates the expanded
-- v2 pattern library. Real attacks now cluster in the 0.6–0.75
-- band (single-pattern high-confidence hits land around 0.68,
-- multi-pattern combinations with the combo-bonus peak around 0.74).
-- At 0.8 those weren't clearing the bar, so the Playground's "Fiction
-- framing" sample showed score 0.74 but never fired the warn step —
-- indistinguishable from "detector missed" to the operator.
--
-- Scope is intentionally narrow: we only lower jailbreak_threshold.
-- injection patterns are mostly critical-severity with scores > 0.8;
-- lowering that threshold would shift semantics without closing any
-- real gap. Leave it alone.
--
-- Only rows sitting at exactly the old default (0.8) are migrated —
-- operators who explicitly chose a threshold keep their value.

ALTER TABLE security_profiles
    ALTER COLUMN jailbreak_threshold SET DEFAULT 0.6;

UPDATE security_profiles
    SET jailbreak_threshold = 0.6
    WHERE jailbreak_threshold = 0.8;

COMMENT ON COLUMN security_profiles.jailbreak_threshold IS
    'Weighted (score × confidence) threshold above which the jailbreak step fires warn. Default 0.6 — calibrated against the v2 pattern library.';
