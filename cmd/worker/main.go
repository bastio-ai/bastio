package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/bastio-ai/bastio/internal/workspace"
	"github.com/bastio-ai/bastio/pkg/cache"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
	"github.com/bastio-ai/bastio/pkg/config"
	"github.com/bastio-ai/bastio/pkg/database"
	"github.com/bastio-ai/bastio/pkg/server"
)

// Job arg types — workers register handlers for these.

// ThreatAlertArgs is enqueued when a high-severity threat is detected.
type ThreatAlertArgs struct {
	TraceID    string `json:"trace_id"`
	CustomerID string `json:"customer_id"`
	ThreatType string `json:"threat_type"`
	Severity   string `json:"severity"`
	Score      float64 `json:"score"`
}

func (ThreatAlertArgs) Kind() string { return "threat_alert" }

// ThreatAlertWorker processes threat alert jobs.
type ThreatAlertWorker struct {
	river.WorkerDefaults[ThreatAlertArgs]
}

func (w *ThreatAlertWorker) Work(ctx context.Context, job *river.Job[ThreatAlertArgs]) error {
	slog.Info("processing threat alert",
		"trace_id", job.Args.TraceID,
		"threat_type", job.Args.ThreatType,
		"severity", job.Args.Severity,
		"score", job.Args.Score,
	)
	// TODO: Send webhook, email, PagerDuty, Slack notification
	return nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	_ = cfg

	setupLogger()

	slog.Info("starting bastio worker")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer db.Close()

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

	// Register workers. Governance webhook delivery and the audit
	// ready-sweep used to register here too; both moved to bastio-cloud
	// during the OSS↔Cloud split. Cloud-server registers them in its
	// own River client (see bastio-cloud/cmd/cloud-server/main.go).
	workers := river.NewWorkers()
	river.AddWorker(workers, &ThreatAlertWorker{})
	// Workspace knowledge ingestion: reads uploaded blobs from BASTIO_DATA_DIR,
	// extracts text, chunks, marks the source 'ready'. Local blob store
	// in OSS; cloud will swap to S3 via the BlobStore interface. When
	// OPENAI_API_KEY is set, chunks are embedded for vector retrieval;
	// otherwise the chunks land with NULL embeddings and the handler
	// falls back to keyword search at query time.
	ingestWorker := workspace.NewIngestKnowledgeWorker(
		db.Pool,
		workspace.NewLocalBlobStore(cfg.DataDir),
	)
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		ingestWorker.SetEmbedder(workspace.NewOpenAIEmbeddingClient(key))
	}
	// Security gate — same engine the gateway runs on every chat
	// turn. Without this wiring the standalone worker mode would
	// fail-open (raw extracted text lands in chunks unscanned),
	// which matches the all-in-one path's pre-v2.0 behaviour but
	// is not the production posture we want. Redis is optional;
	// when the connection fails we proceed without the multi-turn
	// Crescendo detector — single-turn detection (PII, secrets,
	// jailbreak, etc.) still works fully.
	var redisCache *cache.Cache
	if cfg.RedisURL != "" {
		c, err := cache.New(ctx, cfg.RedisURL)
		if err != nil {
			slog.Warn("worker: redis unavailable, multi-turn jailbreak detection disabled",
				"error", err)
		} else {
			redisCache = c
			defer redisCache.Close()
		}
	}
	secEngine, secProfiles := server.BuildSecurityEngine(ctx, db.Pool, redisCache)
	ingestWorker.SetSecurityEngine(secEngine)
	ingestWorker.SetSecurityProfiles(secProfiles)
	slog.Info("worker: security gate wired for KB ingest",
		"redis", redisCache != nil)
	river.AddWorker(workers, ingestWorker)

	// Workspace daily archive sweep — moves PG messages older than each
	// customer's retention_days into ClickHouse, keeps PG hot. Runs as
	// a periodic River job at 24h intervals; SkippedJobs return nil
	// when CH isn't configured so OSS dev without CH still boots clean.
	var archiveCH *clickhouse.CH
	if cfg.ClickHouseURL != "" {
		ch, err := clickhouse.New(ctx, cfg.ClickHouseURL)
		if err != nil {
			slog.Warn("workspace archive: clickhouse unavailable, sweeps will no-op", "error", err)
		} else {
			archiveCH = ch
			defer ch.Close()
		}
	}
	river.AddWorker(workers, workspace.NewArchiveWorker(db.Pool, archiveCH))

	client, err := river.NewClient(riverpgxv5.New(db.Pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 100},
			"alerts":          {MaxWorkers: 20},
		},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			// Daily workspace archive sweep — runs once per worker
			// process at 24h intervals starting one minute after boot.
			// Multiple worker processes will all schedule, but River
			// dedupes via row-level locking on the queued args.
			river.NewPeriodicJob(
				river.PeriodicInterval(24*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) {
					return workspace.WorkspaceArchiveArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		},
	})
	if err != nil {
		slog.Error("failed to create river client", "error", err)
		os.Exit(1)
	}

	if err := client.Start(ctx); err != nil {
		slog.Error("failed to start worker", "error", err)
		os.Exit(1)
	}

	slog.Info("worker running", "queues", "default,alerts")

	// Wait for shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	<-done

	slog.Info("shutting down worker...")
	if err := client.Stop(ctx); err != nil {
		slog.Error("worker stop error", "error", err)
	}
	slog.Info("worker stopped")
}

func setupLogger() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
}

// Ensure pgx.Tx is used (River generic constraint)
var _ river.Client[pgx.Tx]
