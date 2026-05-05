package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"os"

	"github.com/bastio-ai/bastio/pkg/config"
	"github.com/bastio-ai/bastio/pkg/server"
)

//go:embed all:dashboard_dist
var dashboardFS embed.FS

//go:embed openapi.yaml
var openapiSpec []byte

//go:embed all:docs
var docsFS embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	server.SetupLogger(cfg.LogLevel)
	slog.Info("starting bastio", "mode", cfg.Mode, "port", cfg.Port)

	ctx := context.Background()

	dashDist, subErr := fs.Sub(dashboardFS, "dashboard_dist")
	if subErr != nil {
		slog.Error("failed to load embedded dashboard", "error", subErr)
		os.Exit(1)
	}

	docsDist, subErr := fs.Sub(docsFS, "docs")
	if subErr != nil {
		slog.Error("failed to load embedded docs", "error", subErr)
		os.Exit(1)
	}

	srv, err := server.New(ctx, cfg,
		server.WithDashboard(dashDist),
		server.WithOpenAPISpec(openapiSpec),
		server.WithDocs(docsDist),
	)
	if err != nil {
		slog.Error("failed to initialize server", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			slog.Error("close error", "error", err)
		}
	}()

	if err := srv.Start(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
