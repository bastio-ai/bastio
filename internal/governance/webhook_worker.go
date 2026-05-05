package governance

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// WebhookDeliveryArgs is the River job payload for a single webhook delivery.
// One job per (webhook, event) pair. River handles durable retry across
// server restarts; the worker just attempts the HTTP POST and returns an
// error on failure to trigger River's exponential backoff.
type WebhookDeliveryArgs struct {
	WebhookID uuid.UUID
	Event     EventPayload
}

// Kind names the job type for River's registry.
func (WebhookDeliveryArgs) Kind() string { return "governance.webhook_delivery" }

// InsertOpts caps River retries at 6 attempts on the "alerts" queue.
// Backoff is River's default exponential.
func (WebhookDeliveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 6,
		Queue:       "alerts",
	}
}

// WebhookDeliveryWorker executes a queued webhook delivery. Looks up the
// webhook config, formats the payload, posts. Failures bubble up so River
// retries; the worker also persists last_error to the webhook row for
// dashboard visibility.
type WebhookDeliveryWorker struct {
	river.WorkerDefaults[WebhookDeliveryArgs]

	pool *pgxpool.Pool
}

// NewWebhookDeliveryWorker constructs the worker. cmd/worker registers it
// alongside the other River workers when the bastio worker process starts.
func NewWebhookDeliveryWorker(pool *pgxpool.Pool) *WebhookDeliveryWorker {
	return &WebhookDeliveryWorker{pool: pool}
}

// Work executes one delivery attempt. Returns nil on 2xx, an error on
// network failure or non-2xx status — River uses the error to schedule a
// retry with exponential backoff.
func (w *WebhookDeliveryWorker) Work(ctx context.Context, job *river.Job[WebhookDeliveryArgs]) error {
	store := NewWebhookStore(w.pool)
	hook, err := store.LookupByID(ctx, job.Args.WebhookID)
	if err != nil {
		// Webhook deleted between enqueue and execution: drop the job
		// (return nil; not an error) so River doesn't keep retrying.
		return nil
	}

	body, err := formatPayload(hook.Format, job.Args.Event)
	if err != nil {
		store.recordFire(ctx, hook.ID, "format error: "+err.Error())
		return nil // not retriable
	}

	deliverer := NewWebhookDeliverer(store)
	if err := deliverer.deliverOnce(ctx, *hook, body); err != nil {
		store.recordFire(ctx, hook.ID, err.Error())
		return fmt.Errorf("delivery failed: %w", err)
	}
	store.recordFire(ctx, hook.ID, "")
	return nil
}

// EnqueueWebhook routes a single webhook delivery through River when a
// client is available, falling back to the goroutine deliverer otherwise.
// Caller decides which path is correct for their deployment topology.
func EnqueueWebhook(ctx context.Context, client *river.Client[pgx.Tx], deliverer *WebhookDeliverer, hook Webhook, ev EventPayload) {
	if client != nil {
		_, err := client.Insert(ctx, WebhookDeliveryArgs{
			WebhookID: hook.ID,
			Event:     ev,
		}, nil)
		if err == nil {
			return
		}
		// fall through to goroutine on enqueue failure
	}
	go deliverer.deliverWithRetry(hook, ev)
}
