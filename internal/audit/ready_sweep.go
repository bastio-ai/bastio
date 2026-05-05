package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bastio-ai/bastio/pkg/clickhouse"
	"github.com/bastio-ai/bastio/pkg/email"
)

// ReadySweepArgs is the River job payload for the daily 14-day audit
// sweep. Parameterless — the worker walks every outstanding pending
// audit and emails the contacts whose 14-day window has closed.
//
// Closes the wedge loop: prospect submits the form → bundle in inbox
// → IT deploys → 14 days later this cron emails the report-ready
// notice with the activation URL → prospect signs up. Without it,
// prospects would have to remember the activation URL on their own.
type ReadySweepArgs struct{}

func (ReadySweepArgs) Kind() string { return "audit.ready_sweep" }

// InsertOpts caps retry attempts. CH outages outlast 3 attempts get
// re-discovered on the next 24h tick — same pattern as the workspace
// archive worker.
func (ReadySweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Queue:       river.QueueDefault,
	}
}

// ReadySweepWorker is the worker implementation. Constructed once at
// cmd/worker startup and registered with the River client.
type ReadySweepWorker struct {
	river.WorkerDefaults[ReadySweepArgs]

	pool    *pgxpool.Pool
	store   *Store
	ch      *clickhouse.CH
	emailer email.Client
	cfg     Config
}

// NewReadySweepWorker constructs the worker. `ch` is optional; nil
// makes the totals in the email body 0 (the activation URL still
// works and the dashboard shows real numbers).
func NewReadySweepWorker(pool *pgxpool.Pool, ch *clickhouse.CH, emailer email.Client, cfg Config) *ReadySweepWorker {
	return &ReadySweepWorker{
		pool:    pool,
		store:   NewStore(pool),
		ch:      ch,
		emailer: emailer,
		cfg:     cfg.WithDefaults(),
	}
}

const sweepWindow = 14 * 24 * time.Hour

// Work executes one daily sweep. Walks every outstanding audit and
// emails the contact. Per-audit failures are logged and the sweep
// continues — one customer's CH outage shouldn't freeze everyone
// else's notifications. Returns nil on success (drops job from queue);
// errors trigger River's retry with exponential backoff.
func (w *ReadySweepWorker) Work(ctx context.Context, _ *river.Job[ReadySweepArgs]) error {
	if w.emailer == nil {
		slog.Info("audit ready sweep: no email client configured, skipping")
		return nil
	}

	due, err := w.listDueAudits(ctx)
	if err != nil {
		return fmt.Errorf("list due audits: %w", err)
	}

	sent := 0
	for _, a := range due {
		if err := w.processOne(ctx, a); err != nil {
			slog.Error("audit ready sweep: per-audit failed",
				"audit_id", a.ID, "contact_email", a.ContactEmail, "error", err)
			continue
		}
		sent++
	}

	slog.Info("audit ready sweep: complete", "due", len(due), "sent", sent)
	return nil
}

// readyRow is the subset of pending_audits we need for the sweep.
type readyRow struct {
	ID           uuid.UUID
	CustomerID   uuid.UUID
	ContactEmail string
	ContactName  string
	CompanyName  string
	ExpiresAt    time.Time
}

// Note on raw tokens: only claim_token_hash is stored, not the raw
// token. To put a working activation URL in the ready email, the sweep
// rotates the token via Store.RotateClaimTokenByID — same mechanism
// /audit/resend uses, just without the cooldown check. The 24h
// periodicity of the sweep is its own rate limit.

func (w *ReadySweepWorker) listDueAudits(ctx context.Context) ([]readyRow, error) {
	const q = `SELECT id, customer_id, contact_email, contact_name, company_name, expires_at
FROM pending_audits
WHERE audit_ready_emailed_at IS NULL
  AND claimed_at IS NULL
  AND created_at <= NOW() - $1::INTERVAL
ORDER BY created_at ASC
LIMIT 500`
	rows, err := w.pool.Query(ctx, q, sweepWindow.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []readyRow{}
	for rows.Next() {
		var r readyRow
		if err := rows.Scan(&r.ID, &r.CustomerID, &r.ContactEmail,
			&r.ContactName, &r.CompanyName, &r.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// processOne emails the contact + marks audit_ready_emailed_at. The
// mark uses a conditional UPDATE so two concurrent sweeps (or a retry
// after partial failure) don't double-send.
func (w *ReadySweepWorker) processOne(ctx context.Context, a readyRow) error {
	totals, err := w.totalsFor(ctx, a.CustomerID)
	if err != nil {
		// Email goes out anyway with zeros — losing the totals is
		// less bad than missing the notification entirely. Logged
		// for operator visibility.
		slog.Warn("audit ready sweep: totals lookup failed",
			"audit_id", a.ID, "error", err)
	}

	// Rotate the claim token so the URL we email actually works.
	// generateRawToken at /audit/start was never persisted, only its
	// hash. RotateClaimTokenByID writes a fresh hash + last_resend_at
	// and returns the raw token we embed below.
	rot, err := w.store.RotateClaimTokenByID(ctx, a.ID, w.cfg.ClaimTokenTTL)
	if err != nil {
		return fmt.Errorf("rotate claim token: %w", err)
	}
	activationURL := fmt.Sprintf("%s/activate?token=%s",
		w.cfg.PublicBaseURL, rot.ClaimToken)

	msg := email.AuditReady(a.ContactName, activationURL,
		totals.totalEvents, totals.uniqueUsers)
	msg.To = a.ContactEmail

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := w.emailer.Send(sendCtx, msg); err != nil {
		return fmt.Errorf("send audit ready: %w", err)
	}

	const markQ = `UPDATE pending_audits SET audit_ready_emailed_at = NOW()
WHERE id = $1 AND audit_ready_emailed_at IS NULL`
	if _, err := w.pool.Exec(ctx, markQ, a.ID); err != nil {
		return fmt.Errorf("mark emailed: %w", err)
	}
	slog.Info("audit ready sweep: emailed",
		"audit_id", a.ID, "to", a.ContactEmail)
	return nil
}

// auditTotals carries the headline numbers shown in the email body.
type auditTotals struct {
	totalEvents int
	uniqueUsers int
}

// totalsFor pulls the count summary from ClickHouse. Returns zeros
// when CH isn't configured or returns an error — never blocks the
// email send (the activation URL is the actual call-to-action; the
// numbers are flavor).
func (w *ReadySweepWorker) totalsFor(ctx context.Context, customerID uuid.UUID) (auditTotals, error) {
	if w.ch == nil {
		return auditTotals{}, nil
	}
	const q = `SELECT count() AS total, uniqExact(user_id) AS users
FROM bastio.governance_events
WHERE customer_id = ?`
	var t auditTotals
	// uint64 and convert to int — overflows aren't realistic for a
	// 14-day single-tenant audit window.
	var total, users uint64
	if err := w.ch.Conn.QueryRow(ctx, q, customerID).Scan(&total, &users); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return t, nil
		}
		return t, err
	}
	t.totalEvents = int(total)
	t.uniqueUsers = int(users)
	return t, nil
}
