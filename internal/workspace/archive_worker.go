package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bastio-ai/bastio/pkg/clickhouse"
)

// WorkspaceArchiveArgs is the River job payload for the daily message
// archive sweep. The job is parameterless; it walks every workspace
// settings row and applies the customer's `retention_days`. Cloud will
// extend this with per-tier overrides (e.g. enterprise gets 365 days
// regardless of the customer's column value).
type WorkspaceArchiveArgs struct{}

func (WorkspaceArchiveArgs) Kind() string { return "workspace.archive_messages" }

// InsertOpts caps retries — a transient CH outage that lasts longer
// than 3 attempts means tomorrow's sweep picks up the same backlog;
// no point hammering the queue.
func (WorkspaceArchiveArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Queue:       river.QueueDefault,
	}
}

// ArchiveWorker copies PG messages older than each customer's
// `retention_days` into CH, then deletes them from PG. Three guarantees:
//
//  1. Archive is durable before delete — a CH insert failure aborts
//     the sweep for that customer, leaving PG untouched.
//  2. The PG delete is bounded by the same `created_at` cutoff used
//     for the CH insert, so a row created during the sweep can't be
//     accidentally deleted before being archived.
//  3. Per-customer batching: a customer with 1M old messages doesn't
//     hold a transaction longer than `archiveBatchSize` rows.
type ArchiveWorker struct {
	river.WorkerDefaults[WorkspaceArchiveArgs]

	pool *pgxpool.Pool
	ch   *clickhouse.CH
}

func NewArchiveWorker(pool *pgxpool.Pool, ch *clickhouse.CH) *ArchiveWorker {
	return &ArchiveWorker{pool: pool, ch: ch}
}

const archiveBatchSize = 1000

// Work runs one sweep across every customer with workspace settings.
// Returning nil drops the job from the queue; returning an error
// triggers River retry with exponential backoff. Per-customer errors
// are logged and the sweep continues — one customer's CH outage doesn't
// freeze everyone else's retention.
func (w *ArchiveWorker) Work(ctx context.Context, _ *river.Job[WorkspaceArchiveArgs]) error {
	if w.ch == nil {
		slog.Info("workspace archive: clickhouse not configured, skipping")
		return nil
	}

	customers, err := w.listCustomersWithSettings(ctx)
	if err != nil {
		return fmt.Errorf("list customers: %w", err)
	}

	totalArchived := 0
	for _, row := range customers {
		archived, err := w.archiveCustomer(ctx, row.customerID, row.retentionDays)
		if err != nil {
			slog.Error("workspace archive: customer failed",
				"customer_id", row.customerID,
				"error", err)
			continue
		}
		totalArchived += archived
	}
	slog.Info("workspace archive: sweep complete",
		"customers", len(customers),
		"archived_rows", totalArchived)
	return nil
}

type customerRetention struct {
	customerID    uuid.UUID
	retentionDays int
}

func (w *ArchiveWorker) listCustomersWithSettings(ctx context.Context) ([]customerRetention, error) {
	const q = `SELECT customer_id, retention_days FROM workspace_settings`
	rows, err := w.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []customerRetention{}
	for rows.Next() {
		var c customerRetention
		if err := rows.Scan(&c.customerID, &c.retentionDays); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// archiveCustomer sweeps one customer in batches of archiveBatchSize.
// Returns the total number of rows archived for the customer.
func (w *ArchiveWorker) archiveCustomer(ctx context.Context, customerID uuid.UUID, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil // 0 = retain forever; nothing to sweep.
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	total := 0
	for {
		batch, err := w.fetchBatch(ctx, customerID, cutoff)
		if err != nil {
			return total, fmt.Errorf("fetch batch: %w", err)
		}
		if len(batch) == 0 {
			return total, nil
		}
		if err := w.writeToCH(ctx, batch); err != nil {
			return total, fmt.Errorf("write CH: %w", err)
		}
		ids := make([]uuid.UUID, len(batch))
		for i, r := range batch {
			ids[i] = r.ID
		}
		if err := w.deleteBatch(ctx, customerID, ids); err != nil {
			return total, fmt.Errorf("delete pg: %w", err)
		}
		total += len(batch)
		if len(batch) < archiveBatchSize {
			return total, nil
		}
	}
}

// archivedRow mirrors the CH archive table.
type archivedRow struct {
	ID               uuid.UUID
	ConversationID   uuid.UUID
	CustomerID       uuid.UUID
	UserID           string
	Role             string
	Content          string
	Provider         string
	Model            string
	PromptTokens     int32
	CompletionTokens int32
	CostCents        int32
	FinishReason     string
	ErrorText        string
	CreatedAt        time.Time
}

func (w *ArchiveWorker) fetchBatch(ctx context.Context, customerID uuid.UUID, cutoff time.Time) ([]archivedRow, error) {
	const q = `SELECT m.id, m.conversation_id, m.customer_id,
COALESCE(c.user_id, ''),
m.role, m.content,
COALESCE(m.provider, ''),
COALESCE(m.model, ''),
m.prompt_tokens, m.completion_tokens, m.cost_cents,
COALESCE(m.finish_reason, ''),
COALESCE(m.error, ''),
m.created_at
FROM workspace_messages m
LEFT JOIN workspace_conversations c ON c.id = m.conversation_id
WHERE m.customer_id = $1 AND m.created_at < $2
ORDER BY m.created_at ASC
LIMIT $3`
	rows, err := w.pool.Query(ctx, q, customerID, cutoff, archiveBatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []archivedRow{}
	for rows.Next() {
		var r archivedRow
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.CustomerID,
			&r.UserID, &r.Role, &r.Content, &r.Provider, &r.Model,
			&r.PromptTokens, &r.CompletionTokens, &r.CostCents,
			&r.FinishReason, &r.ErrorText, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (w *ArchiveWorker) writeToCH(ctx context.Context, batch []archivedRow) error {
	const stmt = `INSERT INTO bastio.workspace_messages_archive (
id, conversation_id, customer_id, user_id, role, content,
provider, model, prompt_tokens, completion_tokens, cost_cents,
finish_reason, error, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, r := range batch {
		if err := w.ch.Exec(ctx, stmt,
			r.ID, r.ConversationID, r.CustomerID, r.UserID,
			r.Role, r.Content, r.Provider, r.Model,
			r.PromptTokens, r.CompletionTokens, r.CostCents,
			r.FinishReason, r.ErrorText, r.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (w *ArchiveWorker) deleteBatch(ctx context.Context, customerID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	const q = `DELETE FROM workspace_messages
WHERE customer_id = $1 AND id = ANY($2::uuid[])`
	_, err := w.pool.Exec(ctx, q, customerID, ids)
	return err
}
