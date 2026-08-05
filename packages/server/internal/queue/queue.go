// Package queue provides River-based job queue infrastructure.
// Two modes:
//   - Insert-only (API server): call InsertClient() — enqueues jobs but doesn't process them
//   - Full worker (cmd/worker): call StartWorker() — processes jobs with registered workers
//
// Both modes share the same Postgres database. River stores jobs in the river_job table.
package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Client wraps river.Client for job insertion. Used by the API server.
type Client struct {
	river *river.Client[pgx.Tx]
}

// Advisory lock keys for River migrations. The two-int form keeps us
// out of both River's own single-int 64-bit lock namespace and any
// accidental collision with golang-migrate's lock.
const (
	riverMigrationLockNamespace int32 = 1835884914 // "mmxr"
	riverMigrationLockID        int32 = 1
)

// RunMigrations runs River's internal schema migrations (creates river_job, river_leader, etc.).
// Safe to call from both server and worker — uses a Postgres advisory lock so only one
// process runs migrations even if both start simultaneously on a fresh database.
//
// Uses transaction-scoped `pg_advisory_xact_lock` rather than the
// session-scoped `pg_advisory_lock`. Rationale: if a release_command
// VM is killed mid-migration (Fly hard-deadline, OOM kill, runner
// network blip), a session-level lock lingers server-side until
// Postgres' TCP keepalive notices the dead session — which can take
// minutes on managed Postgres. Every subsequent deploy blocks on
// the ghost and fails its own 30s deadline, bricking CI. An xact
// lock is released the moment the tx ends, so once Postgres notices
// the session drop and aborts the tx the lock comes back. That
// shortens the cleanup scope from "session lock until session end"
// to "xact lock until tx abort" — not a cure for undetected-
// disconnect latency (a hard network partition can still leave the
// backend in `idle in transaction` holding the lock), but strictly
// better than the session-scoped predecessor.
//
// As a belt-and-suspenders, we sweep stale SESSION-LEVEL ghosts
// before attempting the xact lock. This is specifically scoped to
// `state = 'idle'` (no open transaction) because that's the only
// shape a session-lock ghost can take — the old pre-xact-lock code
// grabbed `pg_advisory_lock` outside any transaction. We MUST NOT
// sweep `idle in transaction` holders: that's the exact state our
// own lock conn sits in while `rivermigrate.Migrate` runs on other
// pool connections, and a concurrent migrator sweeping it would
// kill our legit lock holder and defeat the serialization guard.
// Ghosts of the new xact-lock path (rare: hard partition + no
// keepalive) are left for Postgres' own disconnect detection to
// reap; trying to distinguish them from a slow-but-alive migrator
// from `pg_stat_activity` alone isn't safe.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Defensive cleanup: if a prior migrator died mid-flight, its
	// session-level advisory lock may still be held server-side.
	// Kill any idle-for-too-long holders before we queue up for the
	// lock ourselves. Errors here are logged, not fatal — the xact
	// lock acquisition below is the authoritative gate.
	if err := terminateStaleLockHolders(ctx, pool, riverMigrationLockNamespace, riverMigrationLockID); err != nil {
		slog.Warn("River migrations: stale-lock sweep failed (continuing to lock attempt)", "error", err)
	}

	slog.Info("River migrations: acquiring advisory lock")
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn for advisory lock: %w", err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin lock tx: %w", err)
	}
	// Rollback on any exit path; a successful Commit below makes
	// this a no-op. Use Background so shutdown-cancelled ctx doesn't
	// skip the rollback.
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", riverMigrationLockNamespace, riverMigrationLockID); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	slog.Info("River migrations: advisory lock acquired")

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{
		Logger: slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("create river migrator: %w", err)
	}
	slog.Info("River migrations: running migrator")
	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}

	// Release the lock by committing. The migrator used pool
	// connections independent of our lock tx, so the Commit is
	// purely a lock-release signal.
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit lock tx: %w", err)
	}
	slog.Info("River migrations: migrator complete", "applied", len(res.Versions))
	for _, v := range res.Versions {
		slog.Info("river migration applied", "version", v.Version, "name", v.Name)
	}

	// Backfill app-owned river_job indexes that couldn't be
	// created by our migration 002 on a fresh DB (the river_job
	// table didn't exist yet when app migrations ran first). The
	// CREATE INDEX IF NOT EXISTS makes this idempotent on
	// already-migrated databases.
	if err := ensureRiverJobIndexes(ctx, pool); err != nil {
		return fmt.Errorf("ensure river_job indexes: %w", err)
	}
	return nil
}

// ensureRiverJobIndexes creates the app-owned btree indexes on
// river_job that the ops dashboard depends on. Idempotent — safe
// to call on every boot; Postgres short-circuits when the index
// already exists. Separate from migrations so the startup order
// (app migrations → River migrations → this) works.
func ensureRiverJobIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_river_job_created_desc
			ON public.river_job (created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_river_job_finalized_desc
			ON public.river_job (finalized_at DESC)
			WHERE finalized_at IS NOT NULL`,
		// Supports stuck-memory lookup which joins river_job by
		// args->>'memory_id'. Expression-btree, partial on
		// memory_process jobs only.
		`CREATE INDEX IF NOT EXISTS idx_river_job_memory_process_args_created
			ON public.river_job ((args->>'memory_id'), created_at DESC)
			WHERE kind = 'memory_process'`,
	}
	for _, sql := range stmts {
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}
	return nil
}

// terminateStaleLockHolders terminates Postgres backends that still
// hold our migration advisory lock from the old session-scoped
// code path. Specifically scoped to `state = 'idle'` (no open
// transaction): that's the only shape a session-lock ghost can
// take, because the predecessor `pg_advisory_lock` was held
// outside any transaction. Crucially we do NOT sweep `idle in
// transaction` — that's the exact state a legitimate current
// migrator's lock conn sits in while `rivermigrate.Migrate` runs
// on other pool connections, and sweeping it would terminate a
// live migrator's lock holder and defeat serialization.
//
// Threshold is 2 minutes. Successful migrations take seconds
// (Neon cold start + migrator ~15s p99), so any session-lock
// ghost idling 2+ min is safely evictable. Newer xact-lock
// ghosts (rare: hard network partition with undetected TCP
// drop) land in `idle in transaction` and are left for
// Postgres' own disconnect detection to reap — we can't safely
// distinguish them from a slow-but-alive migrator.
//
// We scope the query to our own role (`current_user`) so a
// shared database doesn't let us accidentally terminate another
// tenant's connection, and we skip pg_backend_pid() so this
// function can't accidentally terminate its own connection.
//
// Privileges: pg_terminate_backend requires the target's role
// or the `pg_signal_backend` role. The migrate binary connects
// with DATABASE_URL (the app role), which owns the connections
// it's terminating — so the self-targeted termination is always
// permitted.
func terminateStaleLockHolders(ctx context.Context, pool *pgxpool.Pool, namespace, id int32) error {
	const staleThreshold = 2 * time.Minute

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn for stale-lock sweep: %w", err)
	}
	defer conn.Release()

	// pg_locks advisory lock rows for the two-int form have
	// objsubid = 2; classid = namespace, objid = id. We match
	// only `state = 'idle'` — NOT `state LIKE 'idle%'` — so we
	// never hit an `idle in transaction` holder (the exact state
	// our own lock conn is in while migrations run).
	rows, err := conn.Query(ctx, `
		SELECT a.pid, a.state, a.state_change
		FROM pg_locks l
		JOIN pg_stat_activity a ON a.pid = l.pid
		WHERE l.locktype = 'advisory'
		  AND l.classid = $1
		  AND l.objid = $2
		  AND l.objsubid = 2
		  AND l.granted
		  AND a.state = 'idle'
		  AND a.state_change < now() - $3::interval
		  AND a.usename = current_user
		  AND a.pid <> pg_backend_pid()
	`, namespace, id, staleThreshold.String())
	if err != nil {
		return fmt.Errorf("query pg_locks: %w", err)
	}
	defer rows.Close()

	type holder struct {
		pid         int32
		state       string
		stateChange time.Time
	}
	var holders []holder
	for rows.Next() {
		var h holder
		if err := rows.Scan(&h.pid, &h.state, &h.stateChange); err != nil {
			return fmt.Errorf("scan pg_locks row: %w", err)
		}
		holders = append(holders, h)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pg_locks rows: %w", err)
	}

	for _, h := range holders {
		slog.Warn("River migrations: terminating stale advisory lock holder",
			"pid", h.pid, "state", h.state, "idle_since", h.stateChange)
		var terminated bool
		if err := conn.QueryRow(ctx, "SELECT pg_terminate_backend($1)", h.pid).Scan(&terminated); err != nil {
			slog.Warn("River migrations: pg_terminate_backend failed", "error", err, "pid", h.pid)
			continue
		}
		if !terminated {
			slog.Warn("River migrations: pg_terminate_backend returned false", "pid", h.pid)
		}
	}
	return nil
}

// InsertClient creates a River client that can only insert jobs (no processing).
// Used by the API server — jobs are processed by the separate worker process.
func InsertClient(pool *pgxpool.Pool) (*Client, error) {
	// Register all job types so River validates args on insert
	workers := river.NewWorkers()
	// We register empty/stub workers just so the client knows about the job types.
	// The real workers with dependencies run in cmd/worker.
	river.AddWorker(workers, &stubMemoryProcessWorker{})
	river.AddWorker(workers, &stubDreamCycleWorker{})
	river.AddWorker(workers, &stubNightlyDreamSweepWorker{})
	river.AddWorker(workers, &stubConfigExtractWorker{})
	river.AddWorker(workers, &stubSendEmailWorker{})
	river.AddWorker(workers, &stubSendRenderedEmailWorker{})
	river.AddWorker(workers, &stubSendInviteRemindersWorker{})
	river.AddWorker(workers, &stubExpireWaitlistInvitesWorker{})
	river.AddWorker(workers, &stubExpireNotificationsWorker{})
	river.AddWorker(workers, &stubSendBroadcastNotificationWorker{})
	river.AddWorker(workers, &stubCampaignSendWorker{})
	river.AddWorker(workers, &stubHubFreezeSweepWorker{})
	river.AddWorker(workers, &stubChatMessageRunWorker{})
	river.AddWorker(workers, &stubChatLeaseSweepWorker{})
	river.AddWorker(workers, &stubChatEventPurgeWorker{})
	river.AddWorker(workers, &stubChatApprovalSweepWorker{})
	river.AddWorker(workers, &stubCopySeedMemoriesWorker{})
	river.AddWorker(workers, &stubBoardSweepWorker{})
	river.AddWorker(workers, &stubBoardRefreshWorker{})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:  slog.Default(),
		Workers: workers,
		// No Queues — insert-only clients don't process
	})
	if err != nil {
		return nil, fmt.Errorf("create insert client: %w", err)
	}

	slog.Info("river insert-only client ready")
	return &Client{river: client}, nil
}

// Insert enqueues a job for processing by the worker.
func (c *Client) Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) error {
	_, err := c.river.Insert(ctx, args, opts)
	return err
}

// JobRetry re-schedules a non-running job for immediate execution.
// Only admin ops uses this — regular retries are driven by River's
// own attempt/backoff machinery. Returns ErrJobNotFound if the id
// is unknown. Safe to call on any state; River rejects invalid
// transitions (e.g. a currently-running job).
func (c *Client) JobRetry(ctx context.Context, id int64) error {
	_, err := c.river.JobRetry(ctx, id)
	return err
}

// JobCancel asks the worker to cancel a job. Running jobs receive
// a context-cancelled signal; non-running jobs are moved directly
// to state='cancelled'. Admin-ops uses this to abort runaway work.
func (c *Client) JobCancel(ctx context.Context, id int64) error {
	_, err := c.river.JobCancel(ctx, id)
	return err
}

// --- Stub workers for insert-only client (validation only, never called) ---

type stubMemoryProcessWorker struct {
	river.WorkerDefaults[MemoryProcessArgs]
}

func (w *stubMemoryProcessWorker) Work(_ context.Context, _ *river.Job[MemoryProcessArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubBoardSweepWorker struct {
	river.WorkerDefaults[BoardSweepArgs]
}

func (w *stubBoardSweepWorker) Work(_ context.Context, _ *river.Job[BoardSweepArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubBoardRefreshWorker struct {
	river.WorkerDefaults[BoardRefreshArgs]
}

func (w *stubBoardRefreshWorker) Work(_ context.Context, _ *river.Job[BoardRefreshArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubDreamCycleWorker struct {
	river.WorkerDefaults[DreamCycleArgs]
}

func (w *stubDreamCycleWorker) Work(_ context.Context, _ *river.Job[DreamCycleArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubNightlyDreamSweepWorker struct {
	river.WorkerDefaults[NightlyDreamSweepArgs]
}

func (w *stubNightlyDreamSweepWorker) Work(_ context.Context, _ *river.Job[NightlyDreamSweepArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubConfigExtractWorker struct {
	river.WorkerDefaults[ConfigExtractArgs]
}

func (w *stubConfigExtractWorker) Work(_ context.Context, _ *river.Job[ConfigExtractArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubSendEmailWorker struct {
	river.WorkerDefaults[SendEmailArgs]
}

func (w *stubSendEmailWorker) Work(_ context.Context, _ *river.Job[SendEmailArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubSendRenderedEmailWorker struct {
	river.WorkerDefaults[SendRenderedEmailArgs]
}

func (w *stubSendRenderedEmailWorker) Work(_ context.Context, _ *river.Job[SendRenderedEmailArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubSendInviteRemindersWorker struct {
	river.WorkerDefaults[SendInviteRemindersArgs]
}

func (w *stubSendInviteRemindersWorker) Work(_ context.Context, _ *river.Job[SendInviteRemindersArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubExpireWaitlistInvitesWorker struct {
	river.WorkerDefaults[ExpireWaitlistInvitesArgs]
}

func (w *stubExpireWaitlistInvitesWorker) Work(_ context.Context, _ *river.Job[ExpireWaitlistInvitesArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubExpireNotificationsWorker struct {
	river.WorkerDefaults[ExpireNotificationsArgs]
}

func (w *stubExpireNotificationsWorker) Work(_ context.Context, _ *river.Job[ExpireNotificationsArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubSendBroadcastNotificationWorker struct {
	river.WorkerDefaults[SendBroadcastNotificationArgs]
}

func (w *stubSendBroadcastNotificationWorker) Work(_ context.Context, _ *river.Job[SendBroadcastNotificationArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubCampaignSendWorker struct {
	river.WorkerDefaults[CampaignSendArgs]
}

func (w *stubCampaignSendWorker) Work(_ context.Context, _ *river.Job[CampaignSendArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubHubFreezeSweepWorker struct {
	river.WorkerDefaults[HubFreezeSweepArgs]
}

func (w *stubHubFreezeSweepWorker) Work(_ context.Context, _ *river.Job[HubFreezeSweepArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubChatMessageRunWorker struct {
	river.WorkerDefaults[ChatMessageRunArgs]
}

func (w *stubChatMessageRunWorker) Work(_ context.Context, _ *river.Job[ChatMessageRunArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubChatLeaseSweepWorker struct {
	river.WorkerDefaults[ChatLeaseSweepArgs]
}

func (w *stubChatLeaseSweepWorker) Work(_ context.Context, _ *river.Job[ChatLeaseSweepArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubChatEventPurgeWorker struct {
	river.WorkerDefaults[ChatEventPurgeArgs]
}

func (w *stubChatEventPurgeWorker) Work(_ context.Context, _ *river.Job[ChatEventPurgeArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubChatApprovalSweepWorker struct {
	river.WorkerDefaults[ChatApprovalSweepArgs]
}

func (w *stubChatApprovalSweepWorker) Work(_ context.Context, _ *river.Job[ChatApprovalSweepArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}

type stubCopySeedMemoriesWorker struct {
	river.WorkerDefaults[CopySeedMemoriesArgs]
}

func (w *stubCopySeedMemoriesWorker) Work(_ context.Context, _ *river.Job[CopySeedMemoriesArgs]) error {
	return fmt.Errorf("stub: should not be called in insert-only mode")
}
