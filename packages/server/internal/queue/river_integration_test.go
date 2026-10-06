package queue

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// These tests run the insert-only client and River's own migrations
// against a real Postgres (testdb). They pin behaviour that a River
// upgrade could change without breaking the build: which kinds the
// API can enqueue, where they land, how unique jobs dedupe, and that
// River's schema migrates forward from the version production runs.

// latestRiverVersion is the newest River schema migration shipped by
// the River version in go.mod (v0.49: 007 drops river_client, 008 is
// a Postgres no-op). Bump it when a River upgrade adds a migration.
const latestRiverVersion = 8

// riverV032SchemaVersion is the last River schema migration that
// River v0.32 shipped. Migrations 001–006 are byte-identical in v0.49,
// so migrating to this target reproduces the production schema from
// before the upgrade.
const riverV032SchemaVersion = 6

func newTestInsertClient(t *testing.T) (*Client, *pgxpool.Pool) {
	t.Helper()
	_, pool := testdb.Acquire(t)
	c, err := InsertClient(pool)
	if err != nil {
		t.Fatalf("InsertClient: %v", err)
	}
	return c, pool
}

// Every kind the API server enqueues must be registered on the
// insert-only client and land on its queue with its max attempts.
// UsageSyncArgs is left out on purpose: only the worker's periodic
// enqueuer inserts it, so the insert-only client has no stub for it.
func TestInsertClient_InsertsEveryKindOnItsQueue(t *testing.T) {
	t.Parallel()
	c, pool := newTestInsertClient(t)
	ctx := context.Background()

	cases := []struct {
		args        river.JobArgs
		queue       string
		maxAttempts int
	}{
		{MemoryProcessArgs{MemoryID: "m1"}, "default", 3},
		{DreamCycleArgs{HubID: "h1"}, "dreams", 2},
		{NightlyDreamSweepArgs{}, "dreams", 1},
		{ConfigExtractArgs{ConfigID: "c1", OwnerID: "u1"}, "default", 2},
		{CopySeedMemoriesArgs{UserID: "u1"}, "default", 3},
		{SendEmailArgs{Template: "t", To: "a@example.com"}, "default", 3},
		{SendRenderedEmailArgs{To: "a@example.com", Subject: "s"}, "default", 3},
		{SendInviteRemindersArgs{}, "default", 1},
		{ExpireWaitlistInvitesArgs{}, "default", 1},
		{ExpireNotificationsArgs{}, "default", 1},
		{SendBroadcastNotificationArgs{NotifKind: "k", Payload: json.RawMessage(`{}`), BatchID: "b1"}, "default", 3},
		{CampaignSendArgs{CampaignID: "c1"}, "default", 1},
		{HubFreezeSweepArgs{}, "default", 1},
		{ChatMessageRunArgs{AssistantMessageID: "m1"}, "chat", 1},
		{ChatLeaseSweepArgs{}, "default", 2},
		{ChatApprovalSweepArgs{}, "default", 2},
		{ChatEventPurgeArgs{}, "default", 2},
		{BoardSweepArgs{}, "dreams", 1},
		{BoardRefreshArgs{HubID: "h1"}, "dreams", 2},
		{ledger.CompileTargetArgs{TargetID: uuid.New(), SpaceID: uuid.New()}, ledger.QueueCompile, 5},
		{compile.SweepArgs{}, ledger.QueueCompile, 1},
		{ledger.JudgeArgs{MemoryID: uuid.New(), SpaceID: uuid.New(), Version: 1, Mode: ledger.JudgeProposal}, ledger.QueueJudge, 3},
	}

	expected := make([]rivertest.ExpectedJob, 0, len(cases))
	for _, tc := range cases {
		if err := c.Insert(ctx, tc.args, nil); err != nil {
			t.Fatalf("Insert %s: %v", tc.args.Kind(), err)
		}
		expected = append(expected, rivertest.ExpectedJob{
			Args: tc.args,
			Opts: &rivertest.RequireInsertedOpts{
				Queue:       tc.queue,
				MaxAttempts: tc.maxAttempts,
				State:       rivertype.JobStateAvailable,
			},
		})
	}
	rivertest.RequireManyInserted(ctx, t, riverpgxv5.New(pool), expected)
}

type unregisteredArgs struct{}

func (unregisteredArgs) Kind() string { return "memax_test_unregistered" }

// The insert-only client registers stub workers precisely so River
// rejects a job kind nobody can work. Guard that the check is still on.
func TestInsertClient_RejectsUnregisteredKind(t *testing.T) {
	t.Parallel()
	c, _ := newTestInsertClient(t)

	err := c.Insert(context.Background(), unregisteredArgs{}, nil)
	var unknown *river.UnknownJobKindError
	if !errors.As(err, &unknown) {
		t.Fatalf("Insert of unregistered kind: got %v, want *river.UnknownJobKindError", err)
	}
}

// Scheduled inserts keep working (admin campaign scheduling).
func TestInsertClient_ScheduledInsert(t *testing.T) {
	t.Parallel()
	c, pool := newTestInsertClient(t)
	ctx := context.Background()

	at := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if err := c.Insert(ctx, CampaignSendArgs{CampaignID: "c1"}, &river.InsertOpts{ScheduledAt: at}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	job := rivertest.RequireInserted(ctx, t, riverpgxv5.New(pool), CampaignSendArgs{}, &rivertest.RequireInsertedOpts{
		State: rivertype.JobStateScheduled,
	})
	if !job.ScheduledAt.Equal(at) {
		t.Errorf("scheduled_at = %v, want %v", job.ScheduledAt, at)
	}
}

// Admin ops retry and cancel jobs through the insert-only client
// (River v0.48 reworked both queries).
func TestInsertClient_AdminRetryAndCancel(t *testing.T) {
	t.Parallel()
	c, pool := newTestInsertClient(t)
	ctx := context.Background()
	state := func(id int64) string {
		var s string
		if err := pool.QueryRow(ctx, `SELECT state::text FROM river_job WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatalf("read state: %v", err)
		}
		return s
	}

	toCancel, err := c.river.Insert(ctx, CampaignSendArgs{CampaignID: "c1"}, nil)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := c.JobCancel(ctx, toCancel.Job.ID); err != nil {
		t.Fatalf("JobCancel: %v", err)
	}
	if got := state(toCancel.Job.ID); got != "cancelled" {
		t.Errorf("state after JobCancel = %s, want cancelled", got)
	}

	toRetry, err := c.river.Insert(ctx, CampaignSendArgs{CampaignID: "c2"}, nil)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE river_job SET state = 'discarded', attempt = 1, attempted_at = now(), finalized_at = now() WHERE id = $1`,
		toRetry.Job.ID); err != nil {
		t.Fatalf("discard job: %v", err)
	}
	if err := c.JobRetry(ctx, toRetry.Job.ID); err != nil {
		t.Fatalf("JobRetry: %v", err)
	}
	if got := state(toRetry.Job.ID); got != "available" {
		t.Errorf("state after JobRetry = %s, want available", got)
	}

	if err := c.JobRetry(ctx, 999_999); !errors.Is(err, rivertype.ErrNotFound) {
		t.Errorf("JobRetry unknown id: got %v, want rivertype.ErrNotFound", err)
	}
}

// insertTwiceInOnePeriod inserts first then second and returns the two
// results. ByPeriod keys use the wall clock, so a pair that straddles a
// period boundary would legitimately produce two jobs; when that
// happens the rows are cleared and the pair is retried.
func insertTwiceInOnePeriod(t *testing.T, c *Client, pool *pgxpool.Pool, period time.Duration, first, second river.JobArgs) (*rivertype.JobInsertResult, *rivertype.JobInsertResult) {
	t.Helper()
	ctx := context.Background()
	for range 3 {
		before := time.Now()
		r1, err := c.river.Insert(ctx, first, nil)
		if err != nil {
			t.Fatalf("first insert: %v", err)
		}
		r2, err := c.river.Insert(ctx, second, nil)
		if err != nil {
			t.Fatalf("second insert: %v", err)
		}
		if period == 0 || before.Truncate(period).Equal(time.Now().Truncate(period)) {
			return r1, r2
		}
		if _, err := pool.Exec(ctx, `DELETE FROM river_job`); err != nil {
			t.Fatalf("reset river_job: %v", err)
		}
	}
	t.Fatalf("could not insert both jobs inside one %s period", period)
	return nil, nil
}

func countJobs(t *testing.T, pool *pgxpool.Pool, kind string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = $1`, kind).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", kind, err)
	}
	return n
}

// Unique-job dedupe as each job's InsertOpts declares it. River v0.48
// changed how ByPeriod buckets and ByArgs keys are derived; these cases
// pin that our jobs still collapse (or don't) exactly as before.
func TestInsertClient_UniqueJobDedupe(t *testing.T) {
	t.Parallel()
	email := SendEmailArgs{Template: "waitlist_invite", To: "a@example.com", Variables: map[string]string{"name": "A", "link": "https://memax.app/x?y=1"}}
	broadcast := SendBroadcastNotificationArgs{NotifKind: "release", Payload: json.RawMessage(`{"v":1}`), BatchID: "b1", AdminID: "a1"}

	cases := []struct {
		name          string
		period        time.Duration
		first, second river.JobArgs
		wantDuplicate bool
	}{
		// river:"unique" on HubID narrows the key to the hub, so a
		// manual trigger collapses into the nightly sweep's job.
		{"dream cycle same hub, different trigger", time.Hour, DreamCycleArgs{HubID: "h1"}, DreamCycleArgs{HubID: "h1", TriggeredBy: "u1"}, true},
		{"dream cycle different hubs", time.Hour, DreamCycleArgs{HubID: "h1"}, DreamCycleArgs{HubID: "h2"}, false},
		{"board refresh same hub", 10 * time.Minute, BoardRefreshArgs{HubID: "h1"}, BoardRefreshArgs{HubID: "h1"}, true},
		{"copy seed memories same user", 24 * time.Hour, CopySeedMemoriesArgs{UserID: "u1"}, CopySeedMemoriesArgs{UserID: "u1"}, true},
		{"config extract same args", time.Hour, ConfigExtractArgs{ConfigID: "c1", OwnerID: "u1"}, ConfigExtractArgs{ConfigID: "c1", OwnerID: "u1"}, true},
		{"config extract different config", time.Hour, ConfigExtractArgs{ConfigID: "c1", OwnerID: "u1"}, ConfigExtractArgs{ConfigID: "c2", OwnerID: "u1"}, false},
		{"send email same args", 5 * time.Minute, email, email, true},
		{"send email different recipient", 5 * time.Minute, email, SendEmailArgs{Template: email.Template, To: "b@example.com", Variables: email.Variables}, false},
		{"send rendered email same args", 5 * time.Minute, SendRenderedEmailArgs{To: "a@example.com", Subject: "s", HTML: "<p>x</p>"}, SendRenderedEmailArgs{To: "a@example.com", Subject: "s", HTML: "<p>x</p>"}, true},
		{"broadcast same batch", time.Hour, broadcast, broadcast, true},
		// ByPeriod-only sweeps: one per period regardless of args.
		{"chat lease sweep", 30 * time.Second, ChatLeaseSweepArgs{}, ChatLeaseSweepArgs{}, true},
		{"chat approval sweep", 30 * time.Second, ChatApprovalSweepArgs{}, ChatApprovalSweepArgs{}, true},
		{"chat event purge", 30 * time.Minute, ChatEventPurgeArgs{}, ChatEventPurgeArgs{}, true},
		{"board sweep", 6 * time.Hour, BoardSweepArgs{}, BoardSweepArgs{}, true},
		// No UniqueOpts: every insert is a new job.
		{"memory process is not unique", 0, MemoryProcessArgs{MemoryID: "m1"}, MemoryProcessArgs{MemoryID: "m1"}, false},
		{"campaign send is not unique", 0, CampaignSendArgs{CampaignID: "c1"}, CampaignSendArgs{CampaignID: "c1"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, pool := newTestInsertClient(t)
			r1, r2 := insertTwiceInOnePeriod(t, c, pool, tc.period, tc.first, tc.second)

			if r1.UniqueSkippedAsDuplicate {
				t.Fatal("first insert reported as duplicate")
			}
			if r2.UniqueSkippedAsDuplicate != tc.wantDuplicate {
				t.Errorf("second insert UniqueSkippedAsDuplicate = %v, want %v", r2.UniqueSkippedAsDuplicate, tc.wantDuplicate)
			}
			wantRows := 2
			if tc.wantDuplicate {
				wantRows = 1
				if r2.Job.ID != r1.Job.ID {
					t.Errorf("duplicate should return the existing job %d, got %d", r1.Job.ID, r2.Job.ID)
				}
			}
			if got := countJobs(t, pool, tc.first.Kind()); got != wantRows {
				t.Errorf("%s rows = %d, want %d", tc.first.Kind(), got, wantRows)
			}
		})
	}
}

// BoardRefreshArgs narrows ByState to unfinished states so a refresh
// chained after a dream still runs when an earlier refresh for the
// same hub already completed in the same period. DreamCycleArgs keeps
// River's default states (which include completed), so a second dream
// for the hub in the same hour stays deduped.
func TestInsertClient_UniqueByStateAfterCompletion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		period        time.Duration
		args          river.JobArgs
		wantDuplicate bool
	}{
		{"board refresh reruns after completion", 10 * time.Minute, BoardRefreshArgs{HubID: "h1"}, false},
		{"dream cycle stays deduped after completion", time.Hour, DreamCycleArgs{HubID: "h1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, pool := newTestInsertClient(t)
			ctx := context.Background()
			for range 3 {
				before := time.Now()
				r1, err := c.river.Insert(ctx, tc.args, nil)
				if err != nil {
					t.Fatalf("first insert: %v", err)
				}
				if _, err := pool.Exec(ctx,
					`UPDATE river_job SET state = 'completed', attempt = 1, attempted_at = now(), finalized_at = now() WHERE id = $1`,
					r1.Job.ID); err != nil {
					t.Fatalf("complete job: %v", err)
				}
				r2, err := c.river.Insert(ctx, tc.args, nil)
				if err != nil {
					t.Fatalf("second insert: %v", err)
				}
				if !before.Truncate(tc.period).Equal(time.Now().Truncate(tc.period)) {
					if _, err := pool.Exec(ctx, `DELETE FROM river_job`); err != nil {
						t.Fatalf("reset river_job: %v", err)
					}
					continue
				}
				if r2.UniqueSkippedAsDuplicate != tc.wantDuplicate {
					t.Errorf("UniqueSkippedAsDuplicate = %v, want %v", r2.UniqueSkippedAsDuplicate, tc.wantDuplicate)
				}
				return
			}
			t.Fatalf("could not insert both jobs inside one %s period", tc.period)
		})
	}
}

func riverVersions(t *testing.T, pool *pgxpool.Pool) []int {
	t.Helper()
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatalf("rivermigrate.New: %v", err)
	}
	existing, err := m.ExistingVersions(context.Background())
	if err != nil {
		t.Fatalf("ExistingVersions: %v", err)
	}
	versions := make([]int, 0, len(existing))
	for _, v := range existing {
		versions = append(versions, v.Version)
	}
	return versions
}

func relationExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatalf("to_regclass %s: %v", name, err)
	}
	return exists
}

// resetRiverSchema removes River's schema from a testdb clone (which
// testdb migrates to the latest version) and, when target > 0,
// migrates it back up to target with the current migrator.
func resetRiverSchema(t *testing.T, pool *pgxpool.Pool, target int) {
	t.Helper()
	ctx := context.Background()
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatalf("rivermigrate.New: %v", err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionDown, &rivermigrate.MigrateOpts{TargetVersion: -1}); err != nil {
		t.Fatalf("migrate River down: %v", err)
	}
	if relationExists(t, pool, "river_job") {
		t.Fatal("river_job still exists after migrating River down")
	}
	if target > 0 {
		if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{TargetVersion: target}); err != nil {
			t.Fatalf("migrate River up to %d: %v", target, err)
		}
	}
}

func wantVersions(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// Fresh database: RunMigrations (the release_command path) creates
// River's whole schema plus our river_job indexes, and is a no-op
// the second time.
func TestRunMigrations_FreshDatabase(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	resetRiverSchema(t, pool, 0)

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	if got := riverVersions(t, pool); !slices.Equal(got, wantVersions(latestRiverVersion)) {
		t.Fatalf("River versions = %v, want 1..%d", got, latestRiverVersion)
	}
	for _, table := range []string{"river_job", "river_leader", "river_queue", "river_notification"} {
		if !relationExists(t, pool, table) {
			t.Errorf("%s missing after fresh migrate", table)
		}
	}
	for _, idx := range []string{"idx_river_job_created_desc", "idx_river_job_finalized_desc", "idx_river_job_memory_process_args_created"} {
		if !relationExists(t, pool, idx) {
			t.Errorf("app-owned index %s missing", idx)
		}
	}

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}
	if got := riverVersions(t, pool); !slices.Equal(got, wantVersions(latestRiverVersion)) {
		t.Fatalf("River versions after rerun = %v", got)
	}
}

// Existing database: production runs River schema version 6 (River
// v0.32). RunMigrations must roll it forward in place, keeping jobs
// and unique keys, and dropping river_client (River migration 007).
func TestRunMigrations_UpgradesFromV032Schema(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	resetRiverSchema(t, pool, riverV032SchemaVersion)
	if err := ensureRiverJobIndexes(ctx, pool); err != nil {
		t.Fatalf("ensureRiverJobIndexes: %v", err)
	}
	if !relationExists(t, pool, "river_client") {
		t.Fatal("river_client should exist at River schema version 6")
	}

	// State an old deployment leaves behind: our old heartbeat row and
	// a pending unique job inserted before the upgrade.
	if _, err := pool.Exec(ctx,
		`INSERT INTO river_client (id, metadata, updated_at) VALUES ('old-worker', '{}', now())`); err != nil {
		t.Fatalf("seed river_client: %v", err)
	}
	c, err := InsertClient(pool)
	if err != nil {
		t.Fatalf("InsertClient: %v", err)
	}
	before, err := c.river.Insert(ctx, DreamCycleArgs{HubID: "h1"}, nil)
	if err != nil {
		t.Fatalf("insert before upgrade: %v", err)
	}

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations from v6: %v", err)
	}
	if got := riverVersions(t, pool); !slices.Equal(got, wantVersions(latestRiverVersion)) {
		t.Fatalf("River versions = %v, want 1..%d", got, latestRiverVersion)
	}
	if relationExists(t, pool, "river_client") || relationExists(t, pool, "river_client_queue") {
		t.Error("river_client tables should be dropped by River migration 007")
	}
	if !relationExists(t, pool, "river_notification") {
		t.Error("river_notification missing after upgrade")
	}
	var maxAttemptsDefault string
	if err := pool.QueryRow(ctx, `
		SELECT column_default FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'river_job' AND column_name = 'max_attempts'
	`).Scan(&maxAttemptsDefault); err != nil {
		t.Fatalf("read max_attempts default: %v", err)
	}
	if maxAttemptsDefault != "25" {
		t.Errorf("river_job.max_attempts default = %q, want 25", maxAttemptsDefault)
	}

	// The pre-upgrade job survives and still holds its unique key.
	after, err := c.river.Insert(ctx, DreamCycleArgs{HubID: "h1", TriggeredBy: "u1"}, nil)
	if err != nil {
		t.Fatalf("insert after upgrade: %v", err)
	}
	if !after.UniqueSkippedAsDuplicate || after.Job.ID != before.Job.ID {
		t.Errorf("post-upgrade insert should dedupe against job %d, got duplicate=%v id=%d",
			before.Job.ID, after.UniqueSkippedAsDuplicate, after.Job.ID)
	}
}
