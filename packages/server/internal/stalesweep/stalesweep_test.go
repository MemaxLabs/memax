package stalesweep_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/stalesweep"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// The sweep as the worker runs it: on start, the space with a memory past
// its date gets one stale_space job, which flags it as Memax, via system,
// citing the date. Sweeping again in the same hour queues nothing new.
func TestTheSweepFlagsThroughRiver(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, pool := testdb.Acquire(t)
	l := ledger.New(pool, ledger.WithLogger(quiet))
	owner, space := uuid.New(), uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`, []any{owner, owner.String() + "@stale.test"}},
		{`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at) VALUES ($1, 'memax-v2', $2, 'team', $3, 'project', now())`,
			[]any{space, space.String(), owner}},
		{`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{space, owner}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	scope, err := l.UserScope(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-26 * time.Hour)
	res, err := l.Apply(ctx, &ledger.Remember{
		Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: owner}, Scope: scope, Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: space, Statement: "Ask answers with the Haiku tier.", Section: ledger.SectionConventions,
			StaleAfter: &past}})
	if err != nil || res.Memory == nil {
		t.Fatalf("remember: %v %+v", err, res.Policy)
	}
	mem := res.Memory

	workers := river.NewWorkers()
	stalesweep.AddWorkers(workers, l)
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: quiet, Workers: workers, PeriodicJobs: stalesweep.PeriodicJobs(),
		Queues:            map[string]river.QueueConfig{ledger.QueueStale: {MaxWorkers: stalesweep.MaxWorkers}},
		FetchPollInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	deadline := time.Now().Add(30 * time.Second)
	for {
		m, err := l.GetMemory(ctx, scope, mem.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if m.Flags.Has(lifecycle.Stale) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the memory past its date wasn't flagged within 30 s")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var actor, via, kind, ref string
	if err := pool.QueryRow(ctx, `SELECT actor_kind, via, source->>'kind', source->>'ref' FROM v2.receipts
	                               WHERE stream_id = $1 AND action = 'flagged'`, mem.ID).Scan(&actor, &via, &kind, &ref); err != nil {
		t.Fatal(err)
	}
	if actor != "memax" || via != "system" || kind != "date" || ref != past.UTC().Format(time.DateOnly) {
		t.Errorf("flagged by %s via %s, source %s %s", actor, via, kind, ref)
	}

	// The slot's job again (a second machine's sweep in the same hour):
	// River keeps the one it has.
	var slot time.Time
	if err := pool.QueryRow(ctx, `SELECT (args->>'slot')::timestamptz FROM river_job WHERE kind = 'stale_space'`).Scan(&slot); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Insert(ctx, ledger.StaleSpaceArgs{SpaceID: space, Slot: slot}, nil); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'stale_space'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d stale_space jobs, want 1", n)
	}
}
