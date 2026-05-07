-- Force-recalibration of jailbreak_threshold.
--
-- Migration 006 used an equality comparison against a float literal
-- (`WHERE jailbreak_threshold = 0.8`). In practice REAL columns can
-- store values whose bit representation drifts slightly from the
-- literal's REAL cast — depending on how the row was inserted (seed
-- vs. dashboard edit vs. manual SQL), the equality can miss rows that
-- were *logically* at 0.8. When that happens, migration 006 reports
-- success but doesn't change any rows, and the gateway keeps using the
-- old high threshold — the exact state the Playground was reproducing.
--
-- This migration brute-forces any row currently > 0.65 down to 0.6.
-- That safely catches:
--
--   * The original 0.8 DB default (both exact and bit-drifted).
--   * 0.7, 0.75 — common manual re-calibrations that still miss real
--     attacks against the v2 pattern library.
--
-- Threshold choices at or below 0.65 are treated as intentional
-- operator preference (tight enough that a human clearly wanted it)
-- and left alone.
--
-- Follow-up: future migrations touching threshold columns should use
-- BETWEEN windows rather than exact equality to avoid repeating the
-- float-match issue.

UPDATE security_profiles
    SET jailbreak_threshold = 0.6
    WHERE jailbreak_threshold > 0.65;
