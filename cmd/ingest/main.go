package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/bastio-ai/bastio/pkg/config"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
	"github.com/bastio-ai/bastio/pkg/database"
)

// IngestTraceArgs is enqueued by the gateway for each request.
type IngestTraceArgs struct {
	TraceID      string  `json:"trace_id"`
	CustomerID   string  `json:"customer_id"`
	ProxyID      string  `json:"proxy_id"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	DurationMs   uint32  `json:"duration_ms"`
	InputTokens  uint32  `json:"input_tokens"`
	OutputTokens uint32  `json:"output_tokens"`
	CostCents    float64 `json:"cost_cents"`
	Status       string  `json:"status"`
	ThreatDetected bool  `json:"threat_detected"`
}

func (IngestTraceArgs) Kind() string { return "ingest_trace" }

// IngestWorker batch-inserts traces into ClickHouse.
type IngestWorker struct {
	river.WorkerDefaults[IngestTraceArgs]
	ch *clickhouse.CH
}

func (w *IngestWorker) Work(ctx context.Context, job *river.Job[IngestTraceArgs]) error {
	args := job.Args
	return w.ch.Exec(ctx, `
		INSERT INTO bastio.analytics_request_logs (
			customer_id, proxy_id, timestamp, provider, model,
			input_tokens, output_tokens, cost_cents, duration_ms,
			status, threat_detected, end_user_id
		) VALUES (?, ?, now(), ?, ?, ?, ?, ?, ?, ?, ?, '')
	`,
		args.CustomerID, args.ProxyID, args.Provider, args.Model,
		args.InputTokens, args.OutputTokens, args.CostCents, args.DurationMs,
		args.Status, args.ThreatDetected,
	)
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	setupLogger()

	slog.Info("starting bastio ingest")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ch, err := clickhouse.New(ctx, cfg.ClickHouseURL)
	if err != nil {
		slog.Error("failed to connect to clickhouse", "error", err)
		os.Exit(1)
	}
	defer ch.Close()

	// Run CH migrations
	if err := ch.Migrate(ctx); err != nil {
		slog.Error("failed to run clickhouse migrations", "error", err)
		os.Exit(1)
	}

	// Run River migrations
	migrator, err := rivermigrate.New(riverpgxv5.New(db.Pool), nil)
	if err != nil {
		slog.Error("failed to create river migrator", "error", err)
		os.Exit(1)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		slog.Error("failed to run river migrations", "error", err)
		os.Exit(1)
	}

	// Register ingest worker
	workers := river.NewWorkers()
	river.AddWorker(workers, &IngestWorker{ch: ch})

	client, err := river.NewClient(riverpgxv5.New(db.Pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			"ingest": {MaxWorkers: 50},
		},
		Workers: workers,
	})
	if err != nil {
		slog.Error("failed to create river client", "error", err)
		os.Exit(1)
	}

	if err := client.Start(ctx); err != nil {
		slog.Error("failed to start ingest", "error", err)
		os.Exit(1)
	}

	slog.Info("ingest running", "queue", "ingest")

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	<-done

	slog.Info("shutting down ingest...")
	if err := client.Stop(ctx); err != nil {
		slog.Error("ingest stop error", "error", err)
	}
	slog.Info("ingest stopped")
}

func setupLogger() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
}

// Ensure pgx.Tx is used (River generic constraint)
var _ river.Client[pgx.Tx]
