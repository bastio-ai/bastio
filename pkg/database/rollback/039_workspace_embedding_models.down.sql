-- REAL[] keeps all original vectors. Older servers fall back to keyword
-- retrieval for non-1536 vectors, so rollback does not erase indexed content.
DROP INDEX IF EXISTS workspace_knowledge_chunks_embedding_1536_hnsw;
DROP INDEX IF EXISTS workspace_knowledge_chunks_embedding_1024_hnsw;
DROP INDEX IF EXISTS workspace_knowledge_chunks_embedding_model_idx;
ALTER TABLE workspace_knowledge_chunks ALTER COLUMN embedding_vec TYPE vector(1536)
USING CASE WHEN vector_dims(embedding_vec)=1536 THEN embedding_vec::vector(1536) ELSE NULL END;
ALTER TABLE workspace_knowledge_chunks DROP COLUMN embedding_model;
CREATE INDEX workspace_knowledge_chunks_embedding_hnsw
ON workspace_knowledge_chunks USING hnsw (embedding_vec vector_cosine_ops);
