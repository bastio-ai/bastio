package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bastio-ai/bastio/internal/security"
)

// WorkspaceIngestKnowledgeArgs is the River job payload for one async
// knowledge-source ingestion. The handler enqueues this immediately
// after the upload endpoint persists the blob; the worker reads the
// blob, extracts text, chunks, and flips the source row to 'ready'
// (or 'failed' with an error message). The dashboard polls the source
// list and re-renders status on each tick.
type WorkspaceIngestKnowledgeArgs struct {
	SourceID   uuid.UUID `json:"source_id"`
	CustomerID uuid.UUID `json:"customer_id"`
}

func (WorkspaceIngestKnowledgeArgs) Kind() string {
	return "workspace.ingest_knowledge"
}

// InsertOpts caps retries — a malformed file is unlikely to succeed
// on retry and we'd rather show 'failed' fast than burn worker time.
func (WorkspaceIngestKnowledgeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Queue:       river.QueueDefault,
	}
}

// IngestKnowledgeWorker pulls one source from PG, fetches its blob from
// the configured BlobStore, extracts plain text via ExtractText, scans
// the extracted text via the workspace security engine, and writes
// chunks via Store.chunkAndEmbedWithScan. Status transitions:
//
//	pending → processing → ready    (success)
//	pending → processing → failed   (extract or chunk error; row.error populated)
type IngestKnowledgeWorker struct {
	river.WorkerDefaults[WorkspaceIngestKnowledgeArgs]

	pool     *pgxpool.Pool
	store    *Store
	blobs    BlobStore
	embedder EmbeddingClient // nil → chunks land with NULL embeddings (keyword RAG)
	// secEngine + secProfiles are the same engine the gateway runs on
	// every chat turn. Wired here so the worker scans extracted KB text
	// before it reaches the chunk store. Both nil = fail-open (no scan
	// performed). See ingest_security.go for the decision policy.
	secEngine   *security.Engine
	secProfiles security.ProfileLookup
}

// NewIngestKnowledgeWorker constructs the worker. cmd/worker/main.go
// registers it with the rest of the workers when bastio's worker
// process starts. Both pgxpool and BlobStore are required — nil checks
// fail loudly during Work() rather than at registration so a missing
// dep is observable in the queue's failed_jobs. The embedder is
// optional: when nil the chunks have NULL embeddings and retrieval
// falls back to keyword search.
func NewIngestKnowledgeWorker(pool *pgxpool.Pool, blobs BlobStore) *IngestKnowledgeWorker {
	return &IngestKnowledgeWorker{
		pool:  pool,
		store: NewStore(pool),
		blobs: blobs,
	}
}

// SetEmbedder wires the embedding client. cmd/worker constructs an
// OpenAI client when OPENAI_API_KEY is set; cloud will swap to a
// per-customer key resolver via the same setter.
func (w *IngestKnowledgeWorker) SetEmbedder(e EmbeddingClient) { w.embedder = e }

// SetSecurityEngine wires the same security engine the gateway uses.
// Without it, KB ingest fails open (no scanning, raw content lands).
// Production deployments must set both engine and profiles.
func (w *IngestKnowledgeWorker) SetSecurityEngine(e *security.Engine) { w.secEngine = e }

// SetSecurityProfiles wires the per-customer profile lookup. Without
// it the worker falls back to fail-open ingest. Pair with
// SetSecurityEngine.
func (w *IngestKnowledgeWorker) SetSecurityProfiles(p security.ProfileLookup) {
	w.secProfiles = p
}

// Work executes one ingestion attempt. River handles the retry loop;
// fatal errors (not-found row, bad ref) return nil so the job stops
// retrying — only transient errors (read I/O, transaction conflicts)
// bubble up to retry.
func (w *IngestKnowledgeWorker) Work(ctx context.Context, job *river.Job[WorkspaceIngestKnowledgeArgs]) error {
	if w.blobs == nil {
		w.markFailed(ctx, job.Args.CustomerID, job.Args.SourceID,
			"blob store not configured (set BASTIO_DATA_DIR and run worker with workspace deps)")
		return nil
	}
	if err := w.markStatus(ctx, job.Args.CustomerID, job.Args.SourceID, "processing", ""); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	src, err := w.store.GetKnowledgeSource(ctx, job.Args.CustomerID, job.Args.SourceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			slog.Info("workspace ingest: source archived before processing",
				"source_id", job.Args.SourceID)
			return nil
		}
		return fmt.Errorf("load source: %w", err)
	}
	if src.Type != "file" && src.Type != "url" {
		// Inline text was already chunked inline in the createKnowledge
		// path — nothing to do here. Mark ready defensively.
		_ = w.markStatus(ctx, job.Args.CustomerID, job.Args.SourceID, "ready", "")
		return nil
	}

	if src.SourceRef == "" {
		w.markFailed(ctx, job.Args.CustomerID, job.Args.SourceID, "source has no blob ref")
		return nil
	}

	body, err := w.blobs.Get(ctx, src.SourceRef)
	if err != nil {
		w.markFailed(ctx, job.Args.CustomerID, job.Args.SourceID,
			fmt.Sprintf("read blob: %v", err))
		return nil
	}
	defer body.Close()

	mimeType := ""
	if src.MimeType != nil {
		mimeType = *src.MimeType
	}
	text, err := ExtractText(body, mimeType, src.Name)
	if err != nil {
		w.markFailed(ctx, job.Args.CustomerID, job.Args.SourceID,
			fmt.Sprintf("extract: %v", err))
		return nil
	}

	// =========================================================
	// SECURITY GATE — extracted text gets the same treatment a
	// chat prompt does. The result drives one of three branches:
	//
	//   block    → mark source quarantined, audit, never chunk.
	//              Admin sees it in the dashboard with the
	//              detected categories + a release / archive
	//              affordance.
	//   sanitize → use the rewritten text as the chunk source.
	//              The original is GC'd; only the redacted form
	//              ever reaches the embedding API or chunk row.
	//   allow    → continue with the original text. Categories
	//              (if any — `warn` / `log_only` actions) get
	//              tagged onto every chunk for forensic search.
	//
	// Fails open when the engine isn't wired (OSS-standalone) —
	// scanForIngest returns Action="allow" and we proceed as
	// though no scan happened.
	// =========================================================
	decision, derr := scanForIngest(ctx, w.secEngine, w.secProfiles,
		job.Args.CustomerID, text)
	if derr != nil {
		w.markFailed(ctx, job.Args.CustomerID, job.Args.SourceID,
			fmt.Sprintf("security scan: %v", derr))
		return nil
	}
	switch decision.Action {
	case "block":
		if err := w.store.QuarantineKnowledgeSource(ctx,
			job.Args.CustomerID, job.Args.SourceID,
			decision.Categories, encodeScanResultForStorage(decision)); err != nil {
			return fmt.Errorf("mark quarantined: %w", err)
		}
		slog.Warn("workspace ingest: quarantined by security scan",
			"source_id", job.Args.SourceID,
			"customer_id", job.Args.CustomerID,
			"categories", decision.Categories)
		return nil
	case "sanitize":
		// Use the sanitized text everywhere downstream. The
		// original never reaches storage or the embedding API.
		text = decision.SanitizedContent
		slog.Info("workspace ingest: sanitized by security scan",
			"source_id", job.Args.SourceID,
			"customer_id", job.Args.CustomerID,
			"categories", decision.Categories)
	}

	// Promote extracted text into inline_text so retrieval treats this
	// source like any other text source. Then run the existing chunker
	// with the scan tags so each chunk row records what was found.
	src.InlineText = &text
	src.Type = "text" // chunkAndEmbed is keyed off type='text'
	sanitized := decision.Action == "sanitize"
	if err := w.store.chunkAndEmbedWithScan(ctx, src, w.embedder,
		sanitized, decision.Categories); err != nil {
		w.markFailed(ctx, job.Args.CustomerID, job.Args.SourceID,
			fmt.Sprintf("chunk: %v", err))
		return nil
	}

	// Persist the inline_text + ready status so reloads see the result.
	// character_count + last_synced_at populated here so the dashboard
	// can show "1.2M chars, synced 5 minutes ago" without re-counting.
	const finalize = `UPDATE workspace_knowledge_sources
SET inline_text = $3, status = 'ready', error = NULL,
    character_count = $4, last_synced_at = NOW()
WHERE customer_id = $1 AND id = $2`
	if _, err := w.pool.Exec(ctx, finalize,
		job.Args.CustomerID, job.Args.SourceID, text, len(text)); err != nil {
		return fmt.Errorf("finalize source: %w", err)
	}
	slog.Info("workspace ingest: ready",
		"source_id", job.Args.SourceID,
		"customer_id", job.Args.CustomerID,
		"chars", len(text))
	return nil
}

func (w *IngestKnowledgeWorker) markStatus(ctx context.Context, customerID, sourceID uuid.UUID, status, errStr string) error {
	const q = `UPDATE workspace_knowledge_sources
SET status = $3, error = NULLIF($4, '')
WHERE customer_id = $1 AND id = $2`
	_, err := w.pool.Exec(ctx, q, customerID, sourceID, status, errStr)
	return err
}

func (w *IngestKnowledgeWorker) markFailed(ctx context.Context, customerID, sourceID uuid.UUID, msg string) {
	if err := w.markStatus(ctx, customerID, sourceID, "failed", msg); err != nil {
		slog.Error("workspace ingest: mark failed",
			"source_id", sourceID, "error", err)
	}
}

// EnqueueIngest dispatches the ingest job. Today it always runs the
// worker inline in a background goroutine — kicks off immediately,
// returns to the caller fast, and the upload's status row flips
// pending → processing → ready as the worker progresses.
//
// Why not River for this job: the durable-retry guarantees River
// gives don't pay off here. If the server crashes mid-ingest, the
// row stays at 'processing' until manually retried — but a re-
// upload is a single click. The trade-off vs the "what worker
// process is registered?" plumbing isn't worth it. River stays in
// place for governance webhook delivery, where retry actually
// matters (the customer's webhook URL might be transiently down).
//
// Multi-server deployments: an upload lands on the server that
// took the request, the worker runs there. No coordination needed.
//
// `client` is accepted for API compatibility but ignored. Removing
// it from the signature would touch every caller; keeping it
// quietly is cheaper.
//
// secEngine + secProfiles are threaded through from the Handler so
// the inline worker runs the same security gate the standalone
// worker mode would. Both nil = fail-open (no scanning, raw content
// lands in chunks). Production deployments must wire both via
// pkg/server's existing setters.
func EnqueueIngest(
	ctx context.Context,
	_ *river.Client[pgx.Tx],
	pool *pgxpool.Pool,
	blobs BlobStore,
	embedder EmbeddingClient,
	secEngine *security.Engine,
	secProfiles security.ProfileLookup,
	args WorkspaceIngestKnowledgeArgs,
) error {
	w := NewIngestKnowledgeWorker(pool, blobs)
	w.SetEmbedder(embedder)
	w.SetSecurityEngine(secEngine)
	w.SetSecurityProfiles(secProfiles)
	go func() {
		// Detached context: the request that triggered the upload may
		// finish well before extraction completes. Use Background so
		// the goroutine isn't cancelled when the request returns.
		bg := context.Background()
		job := &river.Job[WorkspaceIngestKnowledgeArgs]{Args: args}
		if err := w.Work(bg, job); err != nil {
			// markFailed already runs inside Work() on error — nothing
			// else to do here. Logged at the worker level.
			_ = err
		}
	}()
	return nil
}
