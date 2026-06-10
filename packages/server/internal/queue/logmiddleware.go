package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/observability"
)

// LoggerMiddleware wraps every job's Work() with a logger-enriching
// context so any slog.InfoContext / slog.ErrorContext call inside the
// job carries job_id, job_kind, job_queue, and job_attempt.
//
// The enriched ctx is handled by observability.NewCtxAttrsHandler
// (installed on the worker's slog default at boot); individual
// workers don't need to pull a logger out of ctx — they just need to
// use slog.*Context variants instead of plain slog.*.
//
// Also emits structured job lifecycle logs (started/completed/failed)
// with the same enrichment so a single LogQL filter surfaces both the
// lifecycle markers AND the worker's own logs. Operators looking at a
// specific job id in Grafana see the full story without needing
// separate queries.
type LoggerMiddleware struct {
	river.WorkerMiddlewareDefaults
}

func NewLoggerMiddleware() *LoggerMiddleware { return &LoggerMiddleware{} }

// Work runs before and after the job handler. Enrichment happens
// once; nested code gets the fields automatically via ctxAttrsHandler.
func (m *LoggerMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(ctx context.Context) error) error {
	ctx = observability.WithAttrs(ctx,
		slog.Int64("job_id", job.ID),
		slog.String("job_kind", job.Kind),
		slog.String("job_queue", job.Queue),
		slog.Int("job_attempt", job.Attempt),
	)

	started := time.Now()
	slog.InfoContext(ctx, "job started")

	err := doInner(ctx)

	elapsed := time.Since(started)
	if err != nil {
		slog.ErrorContext(ctx, "job failed",
			slog.String("error", err.Error()),
			slog.Duration("elapsed", elapsed),
		)
		return err
	}
	slog.InfoContext(ctx, "job completed",
		slog.Duration("elapsed", elapsed),
	)
	return nil
}
