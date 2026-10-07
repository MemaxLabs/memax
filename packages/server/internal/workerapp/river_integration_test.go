package workerapp

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/forget"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/sealer"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
	"github.com/MemaxLabs/memax/packages/server/internal/v2switch"
)

// These tests run a real River client built from workerRiverConfig
// against testdb, so a River upgrade that changes queue, shutdown or
// periodic-job behaviour fails here rather than in production.

func TestWorkerRiverConfig_QueuesAndMiddleware(t *testing.T) {
	t.Parallel()
	cfg := workerRiverConfig(river.NewWorkers(), nil)

	want := map[string]int{river.QueueDefault: 20, "dreams": 3, "chat": 8, ledger.QueueCompile: compile.MaxWorkers,
		ledger.QueueJudge: judge.MaxWorkers, ledger.QueueIndex: v2index.MaxWorkers, ledger.QueueSeal: sealer.MaxWorkers,
		ledger.QueueForget: forget.MaxWorkers, ledger.QueueSwitch: v2switch.MaxWorkers, ledger.QueueDream: v2dream.MaxWorkers}
	if len(cfg.Queues) != len(want) {
		t.Errorf("queues = %v, want %v", cfg.Queues, want)
	}
	for name, maxWorkers := range want {
		if got := cfg.Queues[name].MaxWorkers; got != maxWorkers {
			t.Errorf("queue %q MaxWorkers = %d, want %d", name, got, maxWorkers)
		}
	}
	if len(cfg.Middleware) != 1 {
		t.Fatalf("middleware = %d entries, want 1 (LoggerMiddleware)", len(cfg.Middleware))
	}
	if _, ok := cfg.Middleware[0].(*queue.LoggerMiddleware); !ok {
		t.Errorf("middleware[0] = %T, want *queue.LoggerMiddleware", cfg.Middleware[0])
	}
	// See workerRiverConfig: a soft stop timeout would change what a
	// deploy does to running jobs.
	if cfg.SoftStopTimeout != 0 {
		t.Errorf("SoftStopTimeout = %v, want 0", cfg.SoftStopTimeout)
	}
}

type blockingArgs struct {
	N int `json:"n"`
}

func (blockingArgs) Kind() string { return "memax_test_blocking" }

// blockingWorker parks until its job ctx is cancelled, the way a
// long LLM call does when the worker receives SIGTERM.
type blockingWorker struct {
	river.WorkerDefaults[blockingArgs]
	started chan int64
}

func (w *blockingWorker) Work(ctx context.Context, job *river.Job[blockingArgs]) error {
	w.started <- job.ID
	<-ctx.Done()
	return ctx.Err()
}

type jobOutcome struct {
	state   string
	attempt int
	errors  int
}

func readJobOutcome(t *testing.T, pool *pgxpool.Pool, id int64) jobOutcome {
	t.Helper()
	var out jobOutcome
	if err := pool.QueryRow(context.Background(),
		`SELECT state::text, attempt, COALESCE(cardinality(errors), 0) FROM river_job WHERE id = $1`, id,
	).Scan(&out.state, &out.attempt, &out.errors); err != nil {
		t.Fatalf("read job %d: %v", id, err)
	}
	return out
}

// cmd/worker hands its signal.NotifyContext ctx to Client.Start, so
// SIGTERM cancels it with a "signal received" cause. River ≥ v0.44
// refunds the attempt of a job interrupted by a soft stop timeout or
// StopAndCancel; a cancelled Start ctx without SoftStopTimeout must
// stay a hard stop where the attempt counts, as it did on v0.32.
func TestWorkerRiverConfig_SignalStopCountsAttempt(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)

	worker := &blockingWorker{started: make(chan int64, 2)}
	workers := river.NewWorkers()
	river.AddWorker(workers, worker)
	cfg := workerRiverConfig(workers, nil)
	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	retryable, err := client.Insert(ctx, blockingArgs{N: 1}, &river.InsertOpts{MaxAttempts: 3})
	if err != nil {
		t.Fatalf("insert retryable: %v", err)
	}
	single, err := client.Insert(ctx, blockingArgs{N: 2}, &river.InsertOpts{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("insert single-attempt: %v", err)
	}

	startCtx, signal := context.WithCancelCause(ctx)
	if err := client.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for range 2 {
		select {
		case <-worker.started:
		case <-time.After(15 * time.Second):
			t.Fatal("jobs did not start")
		}
	}

	signal(errors.New("terminated signal received"))
	stopCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// The attempt and its error are recorded (an interrupted job would
	// show attempt 0 and no errors). The first retry backoff (~1s) is
	// under River's 5s scheduler interval, so River stores the retry
	// as `available` straight away instead of `retryable`.
	got := readJobOutcome(t, pool, retryable.Job.ID)
	if got.attempt != 1 || got.errors != 1 || (got.state != "available" && got.state != "retryable") {
		t.Errorf("MaxAttempts=3 job after signal stop = %+v, want the attempt and its error counted", got)
	}
	if got := readJobOutcome(t, pool, single.Job.ID); got != (jobOutcome{state: "discarded", attempt: 1, errors: 1}) {
		t.Errorf("MaxAttempts=1 job after signal stop = %+v, want discarded", got)
	}
}

type noopWorker[T river.JobArgs] struct {
	river.WorkerDefaults[T]
}

func (*noopWorker[T]) Work(context.Context, *river.Job[T]) error { return nil }

func TestConfigurePeriodicJobs_Count(t *testing.T) {
	t.Parallel()
	if got := len(configurePeriodicJobs(true)); got != 10 {
		t.Errorf("periodic jobs with dreams = %d, want 10", got)
	}
	if got := len(configurePeriodicJobs(false)); got != 9 {
		t.Errorf("periodic jobs without dreams = %d, want 9", got)
	}
}

// The elected leader enqueues the RunOnStart periodic jobs, and only
// those, as soon as it starts. Jobs are left unworked (the client only
// listens on an unused queue) so the enqueued set can be inspected.
func TestConfigurePeriodicJobs_LeaderEnqueuesRunOnStartJobs(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)

	workers := river.NewWorkers()
	river.AddWorker(workers, &noopWorker[queue.NightlyDreamSweepArgs]{})
	river.AddWorker(workers, &noopWorker[queue.BoardSweepArgs]{})
	river.AddWorker(workers, &noopWorker[queue.SendInviteRemindersArgs]{})
	river.AddWorker(workers, &noopWorker[queue.ExpireWaitlistInvitesArgs]{})
	river.AddWorker(workers, &noopWorker[queue.ExpireNotificationsArgs]{})
	river.AddWorker(workers, &noopWorker[queue.UsageSyncArgs]{})
	river.AddWorker(workers, &noopWorker[queue.HubFreezeSweepArgs]{})
	river.AddWorker(workers, &noopWorker[queue.ChatLeaseSweepArgs]{})
	river.AddWorker(workers, &noopWorker[queue.ChatEventPurgeArgs]{})
	river.AddWorker(workers, &noopWorker[queue.ChatApprovalSweepArgs]{})

	cfg := workerRiverConfig(workers, configurePeriodicJobs(true))
	cfg.Queues = map[string]river.QueueConfig{"memax_test_idle": {MaxWorkers: 1}}
	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			stopCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if err := client.Stop(stopCtx); err != nil {
				t.Errorf("Stop: %v", err)
			}
		})
	}
	t.Cleanup(stop)

	want := []string{"board_sweep", "chat_lease_sweep", "expire_notifications", "expire_waitlist_invites", "hub_freeze_sweep"}
	var got []string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := pool.Query(ctx, `SELECT kind FROM river_job ORDER BY kind`)
		if err != nil {
			t.Fatalf("list jobs: %v", err)
		}
		got, err = pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatalf("collect kinds: %v", err)
		}
		if len(got) >= len(want) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()

	// Re-read after stop so a late, unexpected insert is caught too.
	rows, err := pool.Query(ctx, `SELECT kind FROM river_job ORDER BY kind`)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	got, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect kinds: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("RunOnStart jobs enqueued = %v, want %v", got, want)
	}

	var periodic int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM river_job WHERE (metadata->>'periodic')::boolean`).Scan(&periodic); err != nil {
		t.Fatalf("count periodic metadata: %v", err)
	}
	if periodic != len(want) {
		t.Errorf("jobs marked periodic = %d, want %d", periodic, len(want))
	}
	var states []string
	rows, err = pool.Query(ctx, `SELECT DISTINCT state::text FROM river_job`)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	states, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect states: %v", err)
	}
	if !slices.Equal(states, []string{string(rivertype.JobStateAvailable)}) {
		t.Errorf("RunOnStart job states = %v, want only available", states)
	}
}

// The heartbeat writes to the app-owned worker_heartbeats table
// (River dropped river_client) and removes its row on shutdown.
func TestStartClientHeartbeat_WritesAndDeletesRow(t *testing.T) {
	t.Setenv("FLY_MACHINE_ID", "")
	t.Setenv("MEMAX_WORKER_CLIENT_ID", "memax-test-worker")
	t.Setenv("MEMAX_ENV", "test")
	_, pool := testdb.Acquire(t)
	ctx := context.Background()

	stop := startClientHeartbeat(ctx, pool)

	var service, env string
	var updatedAt time.Time
	if err := pool.QueryRow(ctx,
		`SELECT metadata->>'service', metadata->>'memax_env', updated_at FROM worker_heartbeats WHERE id = $1`,
		"memax-test-worker",
	).Scan(&service, &env, &updatedAt); err != nil {
		t.Fatalf("heartbeat row not written: %v", err)
	}
	if service != "memax-worker" || env != "test" {
		t.Errorf("metadata service=%q memax_env=%q", service, env)
	}
	if time.Since(updatedAt) > time.Minute {
		t.Errorf("updated_at %v is not fresh", updatedAt)
	}

	if err := stop(ctx); err != nil {
		t.Fatalf("heartbeat stop: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_heartbeats`).Scan(&n); err != nil {
		t.Fatalf("count heartbeats: %v", err)
	}
	if n != 0 {
		t.Errorf("heartbeat rows after stop = %d, want 0", n)
	}
}
