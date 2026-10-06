package reads

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The worker's daily upkeep of reads: next month's partitions, retention,
// and the north star (plan 25 §5.18) for the week that just ended.

// MaintainInterval is how often reads_maintain runs.
const MaintainInterval = 24 * time.Hour

// MaintainArgs is the periodic reads_maintain job.
type MaintainArgs struct{}

// Kind implements river.JobArgs.
func (MaintainArgs) Kind() string { return "reads_maintain" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (MaintainArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Hour}}
}

// MaintainWorker runs reads_maintain.
type MaintainWorker struct {
	river.WorkerDefaults[MaintainArgs]
	Ledger *ledger.Ledger
	Log    *slog.Logger
	// Now replaces time.Now (tests).
	Now func() time.Time
}

// Work ensures the coming months' partitions, prunes what's past
// retention, and reports the north star.
func (w *MaintainWorker) Work(ctx context.Context, _ *river.Job[MaintainArgs]) error {
	if w.Ledger == nil {
		return river.JobCancel(errors.New("reads: not configured (the worker has no database)"))
	}
	log := w.Log
	if log == nil {
		log = slog.Default()
	}
	now := time.Now()
	if w.Now != nil {
		now = w.Now()
	}
	if err := w.Ledger.EnsureReadPartitions(ctx, now, 2); err != nil {
		return err
	}
	dropped, err := w.Ledger.PruneReads(ctx, now.AddDate(0, -ledger.ReadRetentionMonths, 0))
	if err != nil {
		return err
	}
	// The week that ended yesterday (UTC): a whole week of data.
	m, err := w.Ledger.GetReadMetrics(ctx, now.UTC().AddDate(0, 0, -1))
	if err != nil {
		return err
	}
	reportNorthStar(ctx, m)
	log.InfoContext(ctx, "reads: north star", "metric", "north_star", "week_ending", m.Day.Format(time.DateOnly),
		"spaces_two_agents", m.SpacesTwoAgents, "spaces_read", m.SpacesRead,
		"spaces_two_connections", m.SpacesTwoConnections, "spaces_hook_loads", m.SpacesHookLoads,
		"connections_reading", m.ConnectionsReading, "connections_seen", m.ConnectionsSeen,
		"coverage", m.Coverage(), "partitions_dropped", dropped)
	return nil
}

// reportNorthStar records the week's numbers as gauges.
func reportNorthStar(ctx context.Context, m ledger.ReadMetrics) {
	meter := otel.Meter("memax.reads")
	gauge := func(name, desc string, v int64) {
		if g, err := meter.Int64Gauge(name, metric.WithDescription(desc)); err == nil {
			g.Record(ctx, v)
		}
	}
	gauge("memax.north_star.spaces", "Spaces read by 2+ agent kinds in the week (the north star)", m.SpacesTwoAgents)
	gauge("memax.north_star.spaces_read", "Spaces with any recorded read in the week", m.SpacesRead)
	gauge("memax.north_star.connections_reading", "Agent connections with a recorded read in the week", m.ConnectionsReading)
	gauge("memax.north_star.connections_seen", "Agent connections used in the week (coverage denominator)", m.ConnectionsSeen)
}

// PeriodicJobs is reads_maintain, daily, and once at start so a fresh
// worker ensures next month's partition.
func PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{river.NewPeriodicJob(
		river.PeriodicInterval(MaintainInterval),
		func() (river.JobArgs, *river.InsertOpts) { return MaintainArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)}
}

// AddWorkers registers the reads worker.
func AddWorkers(workers *river.Workers, l *ledger.Ledger) {
	river.AddWorker(workers, &MaintainWorker{Ledger: l})
}
