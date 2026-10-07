package ledger_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// TestDreamScopeComesFirst runs everything Dream does to the record on the
// wire audit (netsim.Audit): the sweep and the email fan-out as
// DreamSweeperRole, a run's reads, publishing an edition, undoing and
// restoring, the editions' reads and the settings. No statement on a v2
// table may run outside a transaction, before its scope, or as a role
// other than memax_v2 and the sweeper.
func TestDreamScopeComesFirst(t *testing.T) {
	t.Parallel()
	audit := netsim.NewAudit(ledger.DBRole, ledger.DreamSweeperRole)
	db := testdb.Open(t, testdb.Options{Watch: audit.Observe})
	client, err := river.NewClient(riverpgxv5.New(db.Pool), &river.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	w := newDreamWorldOn(t, newFixtureOn(t, db, ledger.WithJobs(client)))
	w.f.exec(`UPDATE hubs SET v2_enabled_at = now() WHERE id = $1`, w.space)
	ctx := context.Background()
	scope := w.f.scope(w.owner)
	audit.Arm()

	// The sweep: every space gets a schedule and its first run is queued,
	// in the same few round trips however many spaces come due.
	for _, name := range []string{"api", "web", "docs"} {
		other := w.f.space(w.owner, policy.SpaceProject, name)
		w.f.exec(`UPDATE hubs SET v2_enabled_at = now() WHERE id = $1`, other)
	}
	now := time.Now().UTC().Truncate(time.Second)
	sweepCtx, trips := netsim.Track(ctx)
	queued, err := w.f.l.DreamSweep(sweepCtx, now, 10, func(s ledger.SweepSpace) ledger.SweepDecision {
		slot := now.Add(-time.Hour)
		return ledger.SweepDecision{Cadence: "nightly", TimeZone: s.TimeZone, DueAt: now.Add(23 * time.Hour), Slot: &slot}
	}, client)
	if err != nil || len(queued) != 4 {
		t.Fatalf("sweep: %v %v", queued, err)
	}
	t.Logf("a sweep of 4 spaces: %d round trips\n%s", trips.RoundTrips(), trips)
	// The candidates, their ranks, River's insert and COMMIT, whatever the
	// number of spaces.
	if got, budget := trips.RoundTrips(), 5; got > budget {
		t.Errorf("a sweep of 4 spaces took %d round trips, budget %d\n%s", got, budget, trips)
	}

	// A run's reads.
	snap, err := w.f.l.DreamSnapshot(ctx, w.dreamScope(), w.space, ledger.DreamSnapshotOptions{})
	if err != nil || !snap.HasInput() || snap.Brief == nil || len(snap.Proposals) != 2 {
		t.Fatalf("snapshot: %+v %v", snap, err)
	}
	if _, err := w.f.l.FadeCandidates(ctx, w.dreamScope(), w.space, time.Nanosecond, 10); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.f.l.RecentEditions(ctx, w.dreamScope(), w.space, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// An edition, its reads, an undo and a restore.
	unread := time.Now()
	res := w.publish(now.Add(-time.Hour),
		ledger.PlannedAction{Kind: ledger.DreamFold, Memory: w.k.ID, Version: 1, Notes: w.notes[:2]},
		ledger.PlannedAction{Kind: ledger.DreamConflict, Memory: w.x.ID, Version: 1, Related: w.y.ID, RelatedVersion: 1},
		ledger.PlannedAction{Kind: ledger.DreamFade, Memory: w.fd.ID, Version: 1, Unread: &unread})
	if _, err := w.f.l.ListEditions(ctx, scope, ledger.EditionQuery{SpaceID: w.space}); err != nil {
		t.Fatal(err)
	}
	e, err := w.f.l.GetEdition(ctx, scope, w.space, "latest")
	if err != nil || len(e.Actions) != 3 {
		t.Fatalf("edition: %+v %v", e, err)
	}
	if _, err := w.f.l.ListDreamActions(ctx, scope, ledger.DreamActionQuery{SpaceID: w.space, Edition: res.Edition.Ref}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.f.l.GetDreamAction(ctx, scope, e.Actions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.undo(e.Actions[1].ID); err != nil {
		t.Fatal(err)
	}
	w.f.apply(&ledger.Restore{Meta: meta(person(w.owner), scope, policy.ViaWeb), Memory: w.fd.Ref})

	// Run now, the settings and the morning email.
	if _, _, err := w.f.l.QueueDreamRun(ctx, scope, person(w.owner), policy.ViaWeb, w.space, client); err != nil {
		t.Fatal(err)
	}
	zone := "America/Vancouver"
	if err := w.f.l.ObserveTimeZone(ctx, scope, "Europe/Paris"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.f.l.UpdateDreamSettings(ctx, scope, &zone, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.f.l.GetDreamSettings(ctx, scope); err != nil {
		t.Fatal(err)
	}
	_, recipients, err := w.f.l.DreamRecipients(ctx, w.space, res.Edition.ID)
	if err != nil || len(recipients) != 1 {
		t.Fatalf("recipients: %v %v", recipients, err)
	}
	if err := w.f.l.MarkDreamEmailSent(ctx, w.space, res.Edition.ID, w.owner, "msg-1"); err != nil {
		t.Fatal(err)
	}
	if err := w.f.l.UnsubscribeDreamEmail(ctx, recipients[0].Token); err != nil {
		t.Fatal(err)
	}

	// The sweep again: the owner's zone changed, so the schedule moves to
	// it (with nothing queued); then nothing is due.
	moved := 0
	if queued, err := w.f.l.DreamSweep(ctx, now, 10, func(s ledger.SweepSpace) ledger.SweepDecision {
		moved++
		if s.TimeZone != zone || s.ScheduleTimeZone != "UTC" {
			t.Errorf("the sweep saw zones %q (owner) and %q (schedule)", s.TimeZone, s.ScheduleTimeZone)
		}
		return ledger.SweepDecision{Cadence: "nightly", TimeZone: s.TimeZone, DueAt: now.Add(22 * time.Hour)}
	}, client); err != nil || len(queued) != 0 || moved != 4 {
		t.Fatalf("second sweep: %v %v (decided %d)", queued, err, moved)
	}
	if queued, err := w.f.l.DreamSweep(ctx, now, 10, func(ledger.SweepSpace) ledger.SweepDecision {
		t.Error("the sweep decided a space that isn't due")
		return ledger.SweepDecision{Cadence: "nightly"}
	}, client); err != nil || len(queued) != 0 {
		t.Fatalf("third sweep: %v %v", queued, err)
	}
	audit.Require(t)
}
