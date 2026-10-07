package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/MemaxLabs/memax/packages/server/internal/workerapp"
)

// embeddedWorkerEnabled reports whether this process also runs the River
// worker (MEMAX_EMBEDDED_WORKER). Staging sets it so one machine that
// stops when idle serves both, which lets Neon's staging compute scale to
// zero; production keeps a separate worker app.
func embeddedWorkerEnabled() bool {
	on, err := strconv.ParseBool(os.Getenv("MEMAX_EMBEDDED_WORKER"))
	return err == nil && on
}

// startEmbeddedWorker builds and starts the worker exactly as cmd/worker
// does, reading the same environment. The caller stops it with Shutdown
// after the HTTP server has drained, so a job a request just enqueued is
// still picked up.
func startEmbeddedWorker(ctx context.Context) (*workerapp.App, error) {
	app, err := workerapp.New(ctx)
	if err != nil {
		return nil, err
	}
	if err := app.Start(ctx); err != nil {
		app.Shutdown(context.Background())
		return nil, err
	}
	slog.Info("embedded worker started")
	return app, nil
}
