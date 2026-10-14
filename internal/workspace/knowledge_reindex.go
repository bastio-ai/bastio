package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// RunKnowledgeMaintenance gradually replaces missing/outdated vectors. It
// starts with the API, is bounded per workspace and stops with the API context.
// Original uploaded files remain intact; only security-checked text is embedded.
func (h *Handler) RunKnowledgeMaintenance(ctx context.Context) {
	if h.store == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		_, _ = h.store.pool.Exec(ctx, `DELETE FROM workspace_mcp_oauth_flows WHERE expires_at<now()`)
		_, _ = h.store.pool.Exec(ctx, `DELETE FROM workspace_tool_approvals WHERE expires_at<now()-interval '30 days'`)
		if h.embedder != nil {
			h.reindexPass(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (h *Handler) reindexPass(ctx context.Context) {
	// This privileged scheduler enumerates tenant IDs, then all content work
	// below is explicitly scoped to a single customer. It never logs content.
	rows, err := h.store.pool.Query(ctx, `SELECT customer_id FROM workspace_settings WHERE customer_id IS NOT NULL ORDER BY customer_id`)
	if err != nil {
		return
	}
	var customers []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			customers = append(customers, id)
		}
	}
	rows.Close()
	for _, cid := range customers {
		if ctx.Err() != nil {
			return
		}
		ids, err := h.reindexCandidates(ctx, cid)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return
			}
			work, cancel := context.WithTimeout(ctx, 45*time.Second)
			err = h.reindexKnowledge(work, cid, id)
			cancel()
			if err != nil {
				slog.Warn("knowledge indexing incomplete", "customer_id", cid, "source_id", id)
			}
		}
	}
}
func (h *Handler) reindexCandidates(ctx context.Context, cid uuid.UUID) ([]uuid.UUID, error) {
	rows, err := h.store.pool.Query(ctx, `SELECT s.id FROM workspace_knowledge_sources s
WHERE s.customer_id=$1 AND s.archived_at IS NULL AND s.status='ready'
AND s.inline_text IS NOT NULL AND btrim(s.inline_text)<>''
AND (NOT EXISTS (SELECT 1 FROM workspace_knowledge_chunks k WHERE k.customer_id=$1 AND k.knowledge_source_id=s.id)
OR EXISTS (SELECT 1 FROM workspace_knowledge_chunks k WHERE k.customer_id=$1 AND k.knowledge_source_id=s.id
AND (k.embedding IS NULL OR k.embedding_vec IS NULL OR vector_dims(k.embedding_vec)<>$3 OR k.embedding_model<>$2))) ORDER BY s.updated_at, s.id LIMIT 4`, cid, embeddingModel(h.embedder), h.embedder.Dimension())
	if err != nil {
		return nil, fmt.Errorf("index candidates: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (h *Handler) reindexKnowledge(ctx context.Context, cid, id uuid.UUID) error {
	// Serialize competing API instances without holding a write transaction
	// during the GPU call. The source lock is released on every return path.
	conn, err := h.store.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, cid.String()+":"+id.String()).Scan(&locked); err != nil || !locked {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, cid.String()+":"+id.String()); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	source, err := h.store.GetKnowledgeSource(ctx, cid, id)
	if err != nil {
		return err
	}
	if source.Status != "ready" || source.InlineText == nil {
		return nil
	}
	decision, err := h.scanForIngest(ctx, cid, *source.InlineText)
	if err != nil || decision == nil {
		return fmt.Errorf("knowledge security unavailable")
	}
	if decision.Action == "block" {
		_, err = h.store.pool.Exec(ctx, `UPDATE workspace_knowledge_sources SET status='quarantined',scan_result=$3 WHERE customer_id=$1 AND id=$2 AND archived_at IS NULL`, cid, id, encodeScanResultForStorage(decision))
		return err
	}
	processed := *source.InlineText
	if decision.Action == "sanitize" {
		processed = decision.SanitizedContent
		_, err = h.store.pool.Exec(ctx, `UPDATE workspace_knowledge_sources SET inline_text=$3,scan_result=$4 WHERE customer_id=$1 AND id=$2 AND archived_at IS NULL`, cid, id, processed, encodeScanResultForStorage(decision))
		if err != nil {
			return err
		}
	}
	var count int
	if err = h.store.pool.QueryRow(ctx, `SELECT count(*) FROM workspace_knowledge_chunks WHERE customer_id=$1 AND knowledge_source_id=$2`, cid, id).Scan(&count); err != nil {
		return err
	}
	if count == 0 || processed != *source.InlineText {
		source.Type = "text"
		source.InlineText = &processed
		// Commit protected text/keyword chunks before incremental GPU work.
		if err = h.store.chunkAndEmbedWithScan(ctx, source, nil, decision.Action == "sanitize", decision.Categories); err != nil {
			return err
		}
	}
	for batch := 0; batch < 8; batch++ {
		rows, e := h.store.pool.Query(ctx, `SELECT id,content FROM workspace_knowledge_chunks WHERE customer_id=$1 AND knowledge_source_id=$2
 AND (embedding IS NULL OR embedding_vec IS NULL OR vector_dims(embedding_vec)<>$4 OR embedding_model<>$3) ORDER BY ordinal LIMIT 16`, cid, id, embeddingModel(h.embedder), h.embedder.Dimension())
		if e != nil {
			return e
		}
		var chunks []struct {
			id         uuid.UUID
			text       string
			sanitized  bool
			categories []string
		}
		for rows.Next() {
			var chunk struct {
				id         uuid.UUID
				text       string
				sanitized  bool
				categories []string
			}
			if e = rows.Scan(&chunk.id, &chunk.text); e != nil {
				rows.Close()
				return e
			}
			chunks = append(chunks, chunk)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(chunks) == 0 {
			break
		}
		inputs := make([]string, len(chunks))
		for i := range chunks {
			scan, e := h.scanForIngest(ctx, cid, chunks[i].text)
			if e != nil || scan == nil {
				return fmt.Errorf("knowledge security unavailable")
			}
			if scan.Action == "block" {
				_, e = h.store.pool.Exec(ctx, `UPDATE workspace_knowledge_sources SET status='quarantined',scan_result=$3 WHERE customer_id=$1 AND id=$2 AND archived_at IS NULL`, cid, id, encodeScanResultForStorage(scan))
				return e
			}
			chunks[i].sanitized = scan.Action == "sanitize"
			chunks[i].categories = scan.Categories
			if chunks[i].sanitized {
				chunks[i].text = scan.SanitizedContent
			}
			inputs[i] = chunks[i].text
		}
		vectors, e := h.embedder.Embed(ctx, inputs)
		if e != nil || len(vectors) != len(chunks) || !allValidEmbeddings(vectors, h.embedder.Dimension()) {
			return fmt.Errorf("knowledge embedding unavailable")
		}
		tx, e := h.store.pool.Begin(ctx)
		if e != nil {
			return e
		}
		for i, chunk := range chunks {
			if chunk.categories == nil {
				chunk.categories = []string{}
			}
			_, e = tx.Exec(ctx, `UPDATE workspace_knowledge_chunks SET content=$4,embedding=$5,embedding_vec=$6::vector,embedding_model=$7,
 sanitized=sanitized OR $8,scan_categories=scan_categories || $9::text[] WHERE customer_id=$1 AND knowledge_source_id=$2 AND id=$3`, cid, id, chunk.id, chunk.text, vectors[i], vectorLiteral(vectors[i]), embeddingModel(h.embedder), chunk.sanitized, chunk.categories)
			if e != nil {
				break
			}
		}
		if e == nil {
			_, e = tx.Exec(ctx, `UPDATE workspace_knowledge_sources SET updated_at=now() WHERE customer_id=$1 AND id=$2 AND archived_at IS NULL`, cid, id)
		}
		if e != nil {
			_ = tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	return nil
}
