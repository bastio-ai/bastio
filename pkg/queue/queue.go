package queue

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Queue wraps a River job queue backed by PostgreSQL.
type Queue struct {
	Client *river.Client[pgx.Tx]
	pool   *pgxpool.Pool
}

// New creates a new River queue client.
// Workers should be registered before calling Start.
func New(ctx context.Context, pool *pgxpool.Pool, workers *river.Workers) (*Queue, error) {
	// Run River's internal migrations
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("create river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("run river migrations: %w", err)
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 100},
			"ingest":          {MaxWorkers: 50},
			"alerts":          {MaxWorkers: 20},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create river client: %w", err)
	}

	slog.Info("river queue initialized")
	return &Queue{Client: client, pool: pool}, nil
}

// Start begins processing jobs. Call this after all workers are registered.
func (q *Queue) Start(ctx context.Context) error {
	return q.Client.Start(ctx)
}

// Stop gracefully stops the queue.
func (q *Queue) Stop(ctx context.Context) error {
	return q.Client.Stop(ctx)
}
