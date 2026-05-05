-- Migration 029 — workspace KB security pipeline.
--
-- Closes the gap that v2.0 launched with "we scan prompts but not the
-- documents you upload." Adds the schema affordances the new
-- ingest + retrieval scan gates need:
--
--  1. New 'quarantined' status — sources whose extracted text tripped
--     a block-action detector (secrets, jailbreak, injection). They
--     never get chunked; admins review them out-of-band and either
--     release (override + audit) or archive.
--
--  2. content_hash — SHA-256 of the raw uploaded bytes. Enables
--     dedup across uploads + post-incident forensics ("which
--     workspaces uploaded the same poisoned PDF?"). Captured at
--     upload time, before extraction.
--
--  3. scan_result — JSONB snapshot of the security scan that
--     decided this source's fate. Populated when the source was
--     either sanitized (PII rewritten) or quarantined. Empty for
--     clean sources to keep storage compact.
--
--  4. Per-chunk sanitized + scan_categories — at retrieval time we
--     need to know whether a chunk's `content` is the original or
--     the rewritten-by-scanner version, and which detector
--     categories fired. Lets the retrieval gate skip re-scanning
--     when the workspace's security profile hasn't changed since
--     ingest (an optimization we can layer on later).

-- =============================================================================
-- workspace_knowledge_sources — accept the new status + new metadata
-- =============================================================================

-- Drop the old CHECK and re-add with 'quarantined' included. Postgres
-- doesn't support ALTER CHECK in-place, so this is the standard pattern.
ALTER TABLE workspace_knowledge_sources
    DROP CONSTRAINT IF EXISTS workspace_knowledge_sources_status_check;

ALTER TABLE workspace_knowledge_sources
    ADD CONSTRAINT workspace_knowledge_sources_status_check
    CHECK (status IN ('pending', 'processing', 'ready', 'failed', 'quarantined'));

-- SHA-256 of the raw bytes (pre-extraction). Hex-encoded, 64 chars.
-- Indexed so we can answer "has this exact file been uploaded before?"
-- across the workspace + workspace audit trail across customers.
ALTER TABLE workspace_knowledge_sources
    ADD COLUMN IF NOT EXISTS content_hash TEXT;

CREATE INDEX IF NOT EXISTS workspace_knowledge_sources_hash_idx
    ON workspace_knowledge_sources (content_hash)
    WHERE content_hash IS NOT NULL AND archived_at IS NULL;

-- The scan result snapshot. Shape:
--   {"action": "block" | "sanitize" | "allow",
--    "categories": ["pii_email", "secrets"],
--    "risk_score": 92,
--    "engine_version": "v2.0",
--    "profile_version": "<uuid>"}
-- Quarantined sources always have this populated. Sanitized sources
-- have it populated. Clean sources leave it NULL.
ALTER TABLE workspace_knowledge_sources
    ADD COLUMN IF NOT EXISTS scan_result JSONB;

-- =============================================================================
-- workspace_knowledge_chunks — track sanitization + categories per chunk
-- =============================================================================

-- TRUE when the chunk's `content` is the scanner-rewritten version
-- (PII masked / secrets stripped). The retrieval path uses this to
-- decide whether to re-scan: a chunk that was scanned at ingest with
-- the current profile and stored as-is is trustworthy without re-scan,
-- a sanitized chunk is also trustworthy (already cleaned), but a
-- chunk from before the scan gate launched (sanitized = FALSE,
-- scan_categories = '{}') warrants re-scan because we can't tell
-- whether it was clean or never-scanned.
ALTER TABLE workspace_knowledge_chunks
    ADD COLUMN IF NOT EXISTS sanitized BOOLEAN NOT NULL DEFAULT FALSE;

-- Typed categories the scanner detected on this chunk's source. Empty
-- array = either clean OR never scanned. Combined with `sanitized` and
-- the source's `scan_result.engine_version` you can tell which is which.
-- Indexed for "show me all chunks that mentioned secrets, ever".
ALTER TABLE workspace_knowledge_chunks
    ADD COLUMN IF NOT EXISTS scan_categories TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS workspace_knowledge_chunks_categories_idx
    ON workspace_knowledge_chunks USING GIN (scan_categories)
    WHERE scan_categories <> '{}';
