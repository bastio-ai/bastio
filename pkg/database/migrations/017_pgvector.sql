-- 017_pgvector.sql
-- Upgrade workspace knowledge retrieval from in-process REAL[] cosine
-- scoring to native pgvector. Sub-second similarity over a million
-- chunks; same code path on the customer side.
--
-- Rollout policy: this migration runs unconditionally. If the `vector`
-- extension is unavailable in the deployment's Postgres image, the
-- CREATE EXTENSION call fails and the migration aborts — operators
-- must either install pgvector (preferred: most managed-Postgres
-- providers ship it) OR remove this migration file and run the OSS
-- with the keyword-fallback retrieval path. Cloud's Postgres image
-- ships pgvector enabled by default.
--
-- Backwards compatibility: the existing `embedding REAL[]` column
-- stays. Phase 5 wrote into it; we mirror that data into the new
-- `embedding_vec vector(1536)` column on this migration and then
-- prefer the vector column at query time. Future migrations can drop
-- REAL[] once all clusters have been migrated and we're confident
-- the rollback path isn't needed.

CREATE EXTENSION IF NOT EXISTS vector;

-- 1536-dim matches text-embedding-3-small (the workspace embedding
-- client default). Customers using -3-large (3072) or -ada-002 (1536)
-- can override via Phase 8 multi-dim support; for now, model swaps
-- require a re-index.
ALTER TABLE workspace_knowledge_chunks
    ADD COLUMN IF NOT EXISTS embedding_vec vector(1536);

-- Backfill from REAL[]. Skipped rows (NULL or wrong dim) get NULL
-- embedding_vec; retrieval falls back through to keyword search for
-- those — same behavior as before the migration.
UPDATE workspace_knowledge_chunks
SET embedding_vec = embedding::vector(1536)
WHERE embedding IS NOT NULL
  AND array_length(embedding, 1) = 1536
  AND embedding_vec IS NULL;

-- HNSW index for cosine distance. Build params tuned for the typical
-- workspace KB scale (≤1M chunks per customer). m=16 is the pgvector
-- default; ef_construction=64 trades index build time for recall.
-- Cosine distance because embeddings are L2-normalized by OpenAI's
-- API — `<=>` then approximates 1 - cosine_similarity.
CREATE INDEX IF NOT EXISTS workspace_knowledge_chunks_embedding_hnsw
    ON workspace_knowledge_chunks
    USING hnsw (embedding_vec vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
