package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode"

	"github.com/google/uuid"
)

// MVP retrieval: chunk inline_text into ~1000-char windows on KB create,
// then on each user message pull the top-K chunks by Postgres trigram
// similarity (`pg_trgm`) restricted to the assistant's attached sources.
// Phase 2.5 swaps similarity for pgvector cosine once the dev image
// includes the extension.
//
// Why trigram and not full-text-search: `to_tsvector` lowers recall on
// short queries and acronym-heavy domains (the customer's own product
// names — exactly what they want their assistants to know about).
// Trigram similarity degrades gracefully without needing a dictionary.

const (
	chunkTargetChars = 1000
	chunkOverlap     = 100
	retrievalK       = 6
)

// chunkText splits a string into roughly chunkTargetChars-sized windows
// at sentence/paragraph boundaries. Overlap keeps continuity for
// retrieval — chunks that abut share their last/first chunkOverlap chars.
func chunkText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= chunkTargetChars {
		return []string{text}
	}

	var chunks []string
	for start := 0; start < len(text); {
		end := start + chunkTargetChars
		if end >= len(text) {
			chunks = append(chunks, text[start:])
			break
		}

		// Prefer a paragraph break, fall back to sentence end, then to space.
		split := bestSplit(text, start, end)
		chunks = append(chunks, strings.TrimSpace(text[start:split]))
		next := split - chunkOverlap
		if next <= start {
			next = split
		}
		start = next
	}
	return chunks
}

func bestSplit(text string, start, end int) int {
	// Look back from end for the nicest break inside the last 30% of the
	// window — keeps chunks close to target size without truncating
	// mid-sentence.
	floor := start + (end-start)*70/100
	for i := end; i > floor && i < len(text); i-- {
		if i+1 < len(text) && text[i] == '\n' && text[i+1] == '\n' {
			return i + 2
		}
	}
	for i := end; i > floor && i < len(text); i-- {
		if (text[i] == '.' || text[i] == '!' || text[i] == '?') &&
			(i+1 >= len(text) || unicode.IsSpace(rune(text[i+1]))) {
			return i + 1
		}
	}
	for i := end; i > floor && i < len(text); i-- {
		if unicode.IsSpace(rune(text[i])) {
			return i
		}
	}
	return end
}

// chunkAndEmbedWithScan is the security-aware chunk-and-write path.
// `sanitized` flags every produced chunk row as scanner-rewritten;
// `categories` records what the scanner detected. Both surface in
// the chunks table for retrieval-time filtering and forensic search.
//
// When `sanitized` is true the caller has already substituted the
// rewritten text into k.InlineText — this function does not re-scan
// or re-rewrite. Same atomicity guarantees as the legacy chunkAndEmbed.
func (s *Store) chunkAndEmbedWithScan(
	ctx context.Context,
	k *KnowledgeSource,
	embedder EmbeddingClient,
	sanitized bool,
	categories []string,
) error {
	if k.Type != "text" || k.InlineText == nil {
		return nil
	}
	parts := chunkText(*k.InlineText)
	if len(parts) == 0 {
		return nil
	}

	// Embed up-front (outside the txn) so a slow API call doesn't hold
	// a write lock on chunks. Failure is logged into metadata via the
	// caller but doesn't abort: NULL embeddings = keyword fallback.
	var vectors [][]float32
	if embedder != nil {
		v, err := embedder.Embed(ctx, parts)
		if err == nil && len(v) == len(parts) {
			vectors = v
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM workspace_knowledge_chunks WHERE knowledge_source_id = $1`, k.ID); err != nil {
		return fmt.Errorf("clear chunks: %w", err)
	}

	// Postgres pgx encodes nil-or-empty []string as `{}` (the schema
	// default) — we rely on that so the column always has a sane
	// non-NULL value for the GIN index.
	cats := categories
	if cats == nil {
		cats = []string{}
	}

	usePGVector := s.PGVectorReady(ctx)
	for i, p := range parts {
		var (
			emb    any
			embVec any
		)
		if i < len(vectors) {
			emb = vectors[i]
			if usePGVector {
				embVec = vectorLiteral(vectors[i])
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO workspace_knowledge_chunks
(knowledge_source_id, customer_id, ordinal, content, token_count, embedding, embedding_vec, sanitized, scan_categories)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			k.ID, k.CustomerID, i, p, len(p)/4, emb, embVec, sanitized, cats,
		); err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE workspace_knowledge_sources SET status = 'ready' WHERE id = $1`,
		k.ID,
	); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}

	return tx.Commit(ctx)
}

// VectorSearchPG runs an HNSW-indexed cosine-distance lookup on the
// pgvector column. Returns the top-K chunks ordered by similarity
// (closest first). Caller is responsible for checking `PGVectorReady`
// — this method assumes the index + column exist and will error
// otherwise. The score returned is the original cosine similarity
// (1 - distance), scaled to 0-100 for UI parity with the keyword path.
func (s *Store) VectorSearchPG(ctx context.Context, customerID, assistantID uuid.UUID, query []float32, k int) ([]ChunkHit, error) {
	if k <= 0 {
		k = retrievalK
	}
	args := []any{customerID, vectorLiteral(query), k}
	scopeSQL := ""
	if assistantID != uuid.Nil {
		args = append(args, assistantID)
		scopeSQL = `AND k.knowledge_source_id IN (
  SELECT knowledge_source_id FROM workspace_assistant_knowledge
  WHERE assistant_id = $4 AND customer_id = $1
)`
	}
	q := fmt.Sprintf(`SELECT k.id, k.knowledge_source_id, s.name, k.ordinal, k.content,
1 - (k.embedding_vec <=> $2::vector) AS similarity
FROM workspace_knowledge_chunks k
JOIN workspace_knowledge_sources s ON s.id = k.knowledge_source_id AND s.archived_at IS NULL
WHERE k.customer_id = $1
  AND k.embedding_vec IS NOT NULL %s
ORDER BY k.embedding_vec <=> $2::vector
LIMIT $3`, scopeSQL)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("pgvector search: %w", err)
	}
	defer rows.Close()
	out := []ChunkHit{}
	for rows.Next() {
		var h ChunkHit
		var sim float64
		if err := rows.Scan(&h.ChunkID, &h.SourceID, &h.SourceName,
			&h.Ordinal, &h.Content, &sim); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		h.Score = int(sim * 100)
		out = append(out, h)
	}
	return out, rows.Err()
}

// LoadChunksForAssistant returns every chunk attached to the assistant
// (or all customer chunks when assistantID is uuid.Nil). Used by the
// in-process cosine fallback when pgvector isn't available.
func (s *Store) LoadChunksForAssistant(ctx context.Context, customerID, assistantID uuid.UUID) ([]ChunkWithEmbedding, error) {
	args := []any{customerID}
	scopeSQL := ""
	if assistantID != uuid.Nil {
		args = append(args, assistantID)
		scopeSQL = `AND k.knowledge_source_id IN (
  SELECT knowledge_source_id FROM workspace_assistant_knowledge
  WHERE assistant_id = $2 AND customer_id = $1
)`
	}
	q := fmt.Sprintf(`SELECT k.id, k.knowledge_source_id, s.name, k.ordinal, k.content, k.embedding
FROM workspace_knowledge_chunks k
JOIN workspace_knowledge_sources s ON s.id = k.knowledge_source_id AND s.archived_at IS NULL
WHERE k.customer_id = $1 %s
  AND k.embedding IS NOT NULL
ORDER BY k.knowledge_source_id, k.ordinal`, scopeSQL)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("load chunks: %w", err)
	}
	defer rows.Close()
	out := []ChunkWithEmbedding{}
	for rows.Next() {
		var c ChunkWithEmbedding
		if err := rows.Scan(&c.ChunkID, &c.SourceID, &c.SourceName,
			&c.Ordinal, &c.Content, &c.Embedding); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChunkWithEmbedding is a chunk loaded for in-memory vector scoring.
type ChunkWithEmbedding struct {
	ChunkID    uuid.UUID
	SourceID   uuid.UUID
	SourceName string
	Ordinal    int
	Content    string
	Embedding  []float32
}

// SearchRelevantChunks returns up to retrievalK chunks most relevant to
// the query, scoped to the assistant's attached knowledge sources (or
// all of the customer's sources when the assistant has none attached).
func (s *Store) SearchRelevantChunks(ctx context.Context, customerID, assistantID uuid.UUID, query string) ([]ChunkHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	// Without pg_trgm available, fall back to ILIKE on the longest
	// non-stopword tokens. Cheap, no extension required, surprisingly
	// effective for small KBs. When pg_trgm is rolled out, swap the
	// scoring function to similarity().
	tokens := keywordTokens(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	var args []any
	args = append(args, customerID)

	// Optional assistant scope: chunks belonging to sources attached to
	// the assistant. NULL assistant means "all customer sources".
	scopeSQL := ""
	if assistantID != uuid.Nil {
		scopeSQL = `
AND k.knowledge_source_id IN (
  SELECT knowledge_source_id FROM workspace_assistant_knowledge
  WHERE assistant_id = $2 AND customer_id = $1
)`
		args = append(args, assistantID)
	}

	// Build (score, content) pair; score is the count of token matches.
	// Order by score desc, then chunk ordinal for stable retrieval.
	scoreParts := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		args = append(args, "%"+tok+"%")
		scoreParts = append(scoreParts,
			fmt.Sprintf("(CASE WHEN k.content ILIKE $%d THEN 1 ELSE 0 END)", len(args)))
	}
	scoreExpr := strings.Join(scoreParts, " + ")

	q := fmt.Sprintf(`
SELECT k.id, k.knowledge_source_id, s.name, k.ordinal, k.content, (%s) AS score
FROM workspace_knowledge_chunks k
JOIN workspace_knowledge_sources s ON s.id = k.knowledge_source_id AND s.archived_at IS NULL
WHERE k.customer_id = $1 %s
  AND (%s) > 0
ORDER BY score DESC, k.knowledge_source_id, k.ordinal
LIMIT %d`, scoreExpr, scopeSQL, scoreExpr, retrievalK)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("rag search: %w", err)
	}
	defer rows.Close()

	out := []ChunkHit{}
	for rows.Next() {
		var hit ChunkHit
		if err := rows.Scan(&hit.ChunkID, &hit.SourceID, &hit.SourceName,
			&hit.Ordinal, &hit.Content, &hit.Score); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}

// ChunkHit is one retrieval result.
type ChunkHit struct {
	ChunkID    uuid.UUID `json:"chunk_id"`
	SourceID   uuid.UUID `json:"source_id"`
	SourceName string    `json:"source_name"`
	Ordinal    int       `json:"ordinal"`
	Content    string    `json:"content"`
	Score      int       `json:"score"`
}

// keywordTokens normalizes a user query into searchable tokens:
// lowercase, drops punctuation, drops common stopwords, dedupes. Keeps
// the longest 5 tokens — past that, the ILIKE scan gets expensive
// without much retrieval benefit.
func keywordTokens(q string) []string {
	q = strings.ToLower(q)
	var current strings.Builder
	var raw []string
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			raw = append(raw, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		raw = append(raw, current.String())
	}

	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if len(t) < 3 || stopwords[t] {
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

// stopwords is intentionally short — covers the most common English
// connectors. Skipping a domain-specific stopword (e.g. "company") loses
// at most one ranking point, which the score-aggregation absorbs.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "had": true,
	"her": true, "was": true, "one": true, "our": true, "out": true,
	"day": true, "get": true, "has": true, "him": true, "his": true,
	"how": true, "man": true, "new": true, "now": true, "old": true,
	"see": true, "two": true, "way": true, "who": true, "boy": true,
	"did": true, "its": true, "let": true, "put": true, "say": true,
	"she": true, "too": true, "use": true, "what": true, "your": true,
	"this": true, "with": true, "from": true, "they": true, "have": true,
	"that": true, "will": true, "would": true, "their": true, "there": true,
	"about": true, "which": true, "when": true, "where": true, "while": true,
}

// augmentWithRAG returns the system prompt with retrieval context
// prepended when the active assistant has KB sources attached. Falls
// back to the plain prompt on retrieval failure — RAG is best-effort
// and must never block a chat turn.
//
// Two-tier retrieval:
//  1. If an embedding client is configured AND the assistant's KB has
//     embedded chunks, score by cosine similarity in-process.
//  2. Otherwise (or when the query embedding fails), fall back to the
//     keyword-overlap search. Both paths return the same shape so the
//     caller doesn't care which fired.
//
// Defense-in-depth security gate: every retrieved chunk is rescanned
// with the active customer's profile before it enters the LLM context.
// This catches:
//
//   - Chunks ingested before the v2.0 ingest gate landed (legacy
//     content that never saw a scanner).
//   - Profile changes after ingest — admin tightens the policy and
//     yesterday's "fine" chunk becomes today's "block".
//   - Any path that bypassed the ingest gate (future API changes,
//     manual SQL inserts, etc.).
//
// Block-action chunks are dropped from the prompt. Sanitize-action
// chunks have their content rewritten in-flight (the on-disk row is
// untouched — we never re-write storage from a retrieval-time
// decision; ingest is the canonical write path). Allow chunks pass
// through verbatim.
//
// Returns the augmented prompt PLUS the unique sources that were
// referenced — callers stash the citations on the assistant message
// so the chat UI can render "based on handbook.pdf" chips below the
// reply, without trying to parse the model's free-form text.
func (h *Handler) augmentWithRAG(ctx context.Context, customerID uuid.UUID, conv *Conversation, userMessage, systemPrompt string) (string, []Citation) {
	if conv == nil || conv.AssistantID == nil {
		return systemPrompt, nil
	}

	hits := h.findRelevantChunks(ctx, customerID, *conv.AssistantID, userMessage)
	if len(hits) == 0 {
		return systemPrompt, nil
	}

	// Retrieval-time security gate — see comment block above.
	hits = h.filterChunksBySecurity(ctx, customerID, hits)
	if len(hits) == 0 {
		// Every retrieved chunk was blocked. Don't leak the empty
		// "Use the following excerpts" preamble to the model — fall
		// back to the plain prompt and skip citations entirely.
		return systemPrompt, nil
	}

	var b strings.Builder
	if systemPrompt != "" {
		b.WriteString(systemPrompt)
		b.WriteString("\n\n")
	}
	b.WriteString("Use the following knowledge base excerpts to answer when relevant. Cite the source name when you do.\n\n")
	for i, hit := range hits {
		fmt.Fprintf(&b, "[%d] Source: %s (chunk %d)\n%s\n\n", i+1, hit.SourceName, hit.Ordinal, hit.Content)
	}
	return b.String(), dedupeCitations(hits)
}

// filterChunksBySecurity walks the retrieved chunks through the
// workspace's security engine and returns only the ones safe to
// include in the LLM context. Sanitized chunks have their `Content`
// rewritten in place (in the returned slice — the on-disk row is
// untouched).
//
// Runs the per-chunk scans in parallel via a sync.WaitGroup so the
// total latency is bounded by the slowest single scan, not the sum.
// At the typical retrieval-K of 5, this keeps the gate well under
// the 50ms budget that the rest of the security pipeline targets.
//
// Fails open per-chunk: a scanner error on one chunk lets that chunk
// through (logged at warn level). The alternative — failing closed
// per-chunk — would silently degrade RAG every time the engine
// hiccups. Logged failures are easier to diagnose than missing
// citations.
func (h *Handler) filterChunksBySecurity(
	ctx context.Context,
	customerID uuid.UUID,
	hits []ChunkHit,
) []ChunkHit {
	if h.secEngine == nil || h.secProfiles == nil || len(hits) == 0 {
		return hits
	}

	type result struct {
		keep    bool
		hit     ChunkHit
		blocked []string // categories, when blocked
	}

	results := make([]result, len(hits))
	var wg sync.WaitGroup
	wg.Add(len(hits))
	for i := range hits {
		go func(idx int) {
			defer wg.Done()
			hit := hits[idx]
			decision, err := scanForIngest(ctx, h.secEngine, h.secProfiles,
				customerID, hit.Content)
			if err != nil || decision == nil {
				// Fail-open per-chunk; log + keep.
				slog.Warn("rag retrieval scan: error, keeping chunk",
					"chunk_id", hit.ChunkID,
					"source_id", hit.SourceID,
					"err", err)
				results[idx] = result{keep: true, hit: hit}
				return
			}
			switch decision.Action {
			case "block":
				results[idx] = result{keep: false, blocked: decision.Categories}
			case "sanitize":
				hit.Content = decision.SanitizedContent
				results[idx] = result{keep: true, hit: hit}
			default:
				results[idx] = result{keep: true, hit: hit}
			}
		}(i)
	}
	wg.Wait()

	out := make([]ChunkHit, 0, len(hits))
	for _, r := range results {
		if !r.keep {
			slog.Warn("rag retrieval: chunk blocked by security gate",
				"customer_id", customerID,
				"categories", r.blocked)
			continue
		}
		out = append(out, r.hit)
	}
	return out
}

// Citation is the surfaced form of a KB chunk hit — just enough for
// the chat UI to render a "based on X" chip and link it to the
// source detail page. Stored on the assistant message's metadata
// (not as a separate column to avoid a migration for v1).
type Citation struct {
	SourceID   uuid.UUID `json:"source_id"`
	SourceName string    `json:"source_name"`
}

// dedupeCitations collapses the per-chunk hits to a per-source list.
// One source typically yields multiple chunks at retrieval time;
// the UI only needs to credit each distinct source once.
func dedupeCitations(hits []ChunkHit) []Citation {
	seen := make(map[uuid.UUID]struct{}, len(hits))
	out := make([]Citation, 0, len(hits))
	for _, h := range hits {
		if _, ok := seen[h.SourceID]; ok {
			continue
		}
		seen[h.SourceID] = struct{}{}
		out = append(out, Citation{SourceID: h.SourceID, SourceName: h.SourceName})
	}
	return out
}

// encodeCitationsMetadata produces the assistant message's
// `metadata` JSON blob. nil/empty input returns nil so the column
// stays NULL — only messages that actually used KB get a metadata
// row, and the chat UI's "any citations?" check is just a non-empty
// length check on a typed array.
//
// The shape is `{"citations": [{...}]}` so we can extend metadata
// later (e.g. a "tools_used" or "scan_result" field) without
// having to migrate the existing rows.
func encodeCitationsMetadata(citations []Citation) json.RawMessage {
	if len(citations) == 0 {
		return nil
	}
	b, err := json.Marshal(struct {
		Citations []Citation `json:"citations"`
	}{Citations: citations})
	if err != nil {
		return nil
	}
	return b
}

func (h *Handler) findRelevantChunks(ctx context.Context, customerID, assistantID uuid.UUID, query string) []ChunkHit {
	// Vector path — only when an embedder is configured. Failure on
	// either the embedder call or the chunk load drops us to keyword.
	if h.embedder != nil {
		if hits, ok := h.vectorSearch(ctx, customerID, assistantID, query); ok && len(hits) > 0 {
			return hits
		}
	}
	hits, err := h.store.SearchRelevantChunks(ctx, customerID, assistantID, query)
	if err != nil {
		return nil
	}
	return hits
}

// vectorSearch returns (hits, true) on success and (nil, false) when
// retrieval should fall back to keywords. The boolean keeps the caller
// from emitting an error on the RAG path — empty output is a legitimate
// "no relevant chunks" signal.
//
// Prefers pgvector when the extension is loaded (HNSW-indexed, scales
// to ~1M chunks per customer with sub-50ms p95). Falls back to
// in-process REAL[] cosine when not — fine for MVP-scale KBs.
func (h *Handler) vectorSearch(ctx context.Context, customerID, assistantID uuid.UUID, query string) ([]ChunkHit, bool) {
	queryVecs, err := h.embedder.Embed(ctx, []string{query})
	if err != nil || len(queryVecs) != 1 {
		return nil, false
	}
	q := queryVecs[0]

	if h.store.PGVectorReady(ctx) {
		hits, err := h.store.VectorSearchPG(ctx, customerID, assistantID, q, retrievalK)
		if err != nil || len(hits) == 0 {
			return nil, false
		}
		return hits, true
	}
	return h.vectorSearchInProcess(ctx, customerID, assistantID, q)
}

// vectorSearchInProcess scores cosine similarity in Go after loading
// every chunk attached to the assistant. Suitable up to ~10k chunks
// per customer; pgvector is the upgrade path for larger corpora.
func (h *Handler) vectorSearchInProcess(ctx context.Context, customerID, assistantID uuid.UUID, q []float32) ([]ChunkHit, bool) {
	chunks, err := h.store.LoadChunksForAssistant(ctx, customerID, assistantID)
	if err != nil || len(chunks) == 0 {
		return nil, false
	}

	type scored struct {
		ch    ChunkWithEmbedding
		score float32
	}
	scoredChunks := make([]scored, 0, len(chunks))
	for _, c := range chunks {
		s := CosineSimilarity(q, c.Embedding)
		if s <= 0 {
			continue
		}
		scoredChunks = append(scoredChunks, scored{ch: c, score: s})
	}
	if len(scoredChunks) == 0 {
		return nil, false
	}
	// Top-K by score (partial sort would be cheaper but MVP-size KBs
	// keep full sort comfortable).
	for i := 0; i < len(scoredChunks); i++ {
		for j := i + 1; j < len(scoredChunks); j++ {
			if scoredChunks[j].score > scoredChunks[i].score {
				scoredChunks[i], scoredChunks[j] = scoredChunks[j], scoredChunks[i]
			}
		}
	}
	limit := min(retrievalK, len(scoredChunks))
	out := make([]ChunkHit, 0, limit)
	for i := range limit {
		s := scoredChunks[i]
		out = append(out, ChunkHit{
			ChunkID:    s.ch.ChunkID,
			SourceID:   s.ch.SourceID,
			SourceName: s.ch.SourceName,
			Ordinal:    s.ch.Ordinal,
			Content:    s.ch.Content,
			Score:      int(s.score * 100),
		})
	}
	return out, true
}
