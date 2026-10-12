-- Preserve existing 1536-dimensional vectors while adding self-hosted BGE-M3.
-- Model identity is required as equal dimensions do not imply equal spaces.
ALTER TABLE workspace_knowledge_chunks ADD COLUMN embedding_model TEXT NOT NULL DEFAULT 'openai:text-embedding-3-small';
DROP INDEX IF EXISTS workspace_knowledge_chunks_embedding_hnsw;
ALTER TABLE workspace_knowledge_chunks ALTER COLUMN embedding_vec TYPE vector;
CREATE INDEX workspace_knowledge_chunks_embedding_1536_hnsw
ON workspace_knowledge_chunks USING hnsw ((embedding_vec::vector(1536)) vector_cosine_ops)
WHERE vector_dims(embedding_vec) = 1536;
CREATE INDEX workspace_knowledge_chunks_embedding_1024_hnsw
ON workspace_knowledge_chunks USING hnsw ((embedding_vec::vector(1024)) vector_cosine_ops)
WHERE vector_dims(embedding_vec) = 1024;
CREATE INDEX workspace_knowledge_chunks_embedding_model_idx
ON workspace_knowledge_chunks(customer_id, embedding_model);
