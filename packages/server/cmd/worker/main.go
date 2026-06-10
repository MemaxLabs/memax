// cmd/worker is the background job processor for Memax.
// It connects to the same Postgres database as cmd/server and processes
// River jobs: memory processing (chunking, embedding, classification) and
// Memory Dreams (consolidation, contradiction detection, archival).
//
// Run separately from the API server so crashes don't affect API availability
// and each can scale independently.
//
// Exposes a health endpoint on HEALTH_PORT (default 9090) for Fly.io checks.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/MemaxLabs/memax/packages/server/internal/observability"
	"github.com/MemaxLabs/memax/packages/server/internal/workerapp"
)

func main() {
	logger, logShutdown, err := observability.NewLogger("memax-worker")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := logShutdown(shutdownCtx); err != nil {
			slog.Warn("log exporter shutdown failed", "error", err)
		}
	}()

	otelResult, err := observability.Setup(context.Background(), "memax-worker")
	if err != nil {
		slog.Error("failed to initialize observability", "error", err)
		os.Exit(1)
	}
	if otelResult.Enabled {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := otelResult.Shutdown(shutdownCtx); err != nil {
				slog.Warn("otel shutdown failed", "error", err)
			}
		}()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	app, err := workerapp.New(ctx)
	if err != nil {
		slog.Error("failed to initialize worker", "error", err)
		os.Exit(1)
	}
	if err := app.Start(ctx); err != nil {
		slog.Error("failed to start worker", "error", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	<-ctx.Done()
	slog.Info("shutting down worker...")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	app.Shutdown(shutdownCtx)

	slog.Info("worker stopped")
}
