package ledger_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// dated keeps a memory whose date to check again is at.
func (f *fixture) dated(owner, space uuid.UUID, statement string, at time.Time) *ledger.Memory {
	f.t.Helper()
	nm := fact(space, statement)
	nm.StaleAfter = &at
	return f.apply(&ledger.Remember{Meta: meta(person(owner), f.scope(owner), policy.ViaWeb), NewMemory: nm}).Memory
}

// flagDue applies FlagDue as Memax, in the space's scope.
func flagDue(f *fixture, space uuid.UUID) ledger.Result {
	f.t.Helper()
	scope, err := f.l.SpaceScope(context.Background(), space)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.apply(&ledger.FlagDue{Meta: meta(ledger.Actor{Kind: policy.ActorMemax}, scope, policy.ViaSystem), SpaceID: space})
}

// The date sweep (migration 068): a kept memory whose date passed is
// flagged by Memax, its source the date and its words Memax's, once; a
// date to come, a proposal, a memory a person acted on since its date and
// a space back on V1 are left alone; every target recompiles.
func TestFlagDueFlagsWhatsPastItsDate(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	day := time.Now().UTC().Add(-48 * time.Hour).Truncate(24 * time.Hour).Add(9 * time.Hour)
	due := f.dated(zz, sp, "Ask answers with the Haiku tier.", day)
	later := f.dated(zz, sp, "The alpha ends in November.", time.Now().Add(72*time.Hour))
	looked := f.dated(zz, sp, "The judge's floor is 0.65.", day)
	// A person changed where it applies after its date: they looked at it.
	if res := f.apply(&ledger.ScopeMemory{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: looked.Ref,
		Repos: []string{"memaxlabs/memax"}}); res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("scope: %s %s", res.Outcome, res.Policy.Message)
	}
	pnm := fact(sp, "Dream runs at 03:00 in the owner's zone.")
	pnm.StaleAfter = &day
	proposal := f.apply(&ledger.Propose{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), NewMemory: pnm}).Memory
	// Another space with a memory due, back on V1.
	v1 := f.space(zz, policy.SpaceProject, "acme-web")
	f.dated(zz, v1, "Releases go out on Thursdays.", day)
	f.exec(`UPDATE hubs SET v2_enabled_at = NULL WHERE id = $1`, v1)
	f.brief(zz, sp, 0, []ledger.BriefSection{{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: due.Ref}}}})
	target := f.target(zz, sp, ledger.TargetAgentsMD)
	genBefore := f.get(zz, target.ID).DirtyGen

	spaces, err := f.l.StaleDueSpaces(ctx, time.Now(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(spaces) != 1 || spaces[0] != sp {
		t.Fatalf("spaces due = %v, want only %s", spaces, sp)
	}

	if res := f.apply(&ledger.FlagDue{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: sp}); res.Outcome != ledger.OutcomeRefused ||
		res.Policy.Code != policy.CodeFlagByMemax {
		t.Fatalf("a person flagging dates: %+v", res)
	}
	res := flagDue(f, sp)
	if res.Outcome != ledger.OutcomeApplied || len(res.Receipts) != 1 {
		t.Fatalf("flag due: %s %+v", res.Outcome, res.Receipts)
	}
	rc := res.Receipts[0]
	if rc.ObjectID != due.ID || rc.Action != ledger.ActionFlagged || rc.ActorKind != policy.ActorMemax || rc.Via != policy.ViaSystem ||
		rc.Source == nil || rc.Source.Kind != ledger.StaleSourceDate || rc.Source.Ref != day.Format(time.DateOnly) {
		t.Errorf("receipt %+v source %+v", rc, rc.Source)
	}
	if want := "Its date to check again, " + day.Format("Jan 2, 2006") + ", has passed. Verify it."; rc.Reason != want {
		t.Errorf("reason %q, want %q", rc.Reason, want)
	}
	if !f.mem(zz, due.ID).Flags.Has(lifecycle.Stale) {
		t.Error("the memory past its date isn't stale")
	}
	for _, m := range []*ledger.Memory{later, looked, proposal} {
		if f.mem(zz, m.ID).Flags.Has(lifecycle.Stale) {
			t.Errorf("%s is stale", m.Ref)
		}
	}
	if got := f.get(zz, target.ID).DirtyGen; got <= genBefore || f.jobs(target.ID) == 0 {
		t.Errorf("the target didn't recompile: dirty_gen %d (was %d), %d jobs", got, genBefore, f.jobs(target.ID))
	}

	// Once: nothing is due now, here or across spaces.
	if again := flagDue(f, sp); !again.Unchanged {
		t.Errorf("again: %+v", again)
	}
	if spaces, err := f.l.StaleDueSpaces(ctx, time.Now(), 100); err != nil || len(spaces) != 0 {
		t.Errorf("spaces due after = %v, %v", spaces, err)
	}

	// The stale report reads the date as the cause, and Verify answers it
	// citing the date.
	report, err := f.l.GetStaleReport(ctx, f.scope(zz), due.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Causes) != 1 || report.Causes[0].Kind != ledger.StaleCauseDate {
		t.Errorf("causes %+v", report.Causes)
	}
	v := f.apply(&ledger.Verify{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: due.Ref,
		ExpectedVersion: f.mem(zz, due.ID).Version, Answer: ledger.VerifyStillTrue})
	if v.Outcome != ledger.OutcomeApplied || len(v.Receipts) == 0 {
		t.Fatalf("verify: %s %s", v.Outcome, v.Policy.Message)
	}
	if src := v.Receipts[len(v.Receipts)-1].Source; src == nil || src.Kind != ledger.StaleSourceDate || src.Ref != day.Format(time.DateOnly) {
		t.Errorf("verified source %+v", src)
	}
	f.everyRowReceipted(t)
}

// Read again under the lock: a Verify that moved the date between the
// sweep's read and the lock leaves nothing to flag.
func TestFlagDueReadsTheDateUnderTheLock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.dated(zz, sp, "Ask answers with the Haiku tier.", time.Now().Add(-time.Hour))
	// Hold the memory as a Verify would, then move its date and let go
	// while FlagDue waits on the lock.
	b := f.hold(`SELECT 1 FROM v2.memories WHERE id = $1 FOR NO KEY UPDATE`, m.ID)
	scope, err := f.l.SpaceScope(context.Background(), sp)
	if err != nil {
		t.Fatal(err)
	}
	done := f.applyAsync(f.l, &ledger.FlagDue{Meta: meta(ledger.Actor{Kind: policy.ActorMemax}, scope, policy.ViaSystem), SpaceID: sp})
	f.awaitWaiting(1)
	// The receipt Verify writes beside it isn't what this tests.
	for _, sql := range []string{`SET LOCAL session_replication_role = replica`,
		`UPDATE v2.memories SET stale_after = now() + interval '30 days' WHERE id = $1`} {
		args := []any{m.ID}
		if !strings.Contains(sql, "$1") {
			args = nil
		}
		if _, err := b.tx.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.result("flag due", done); err != nil {
		t.Fatal(err)
	}
	if f.mem(zz, m.ID).Flags.Has(lifecycle.Stale) {
		t.Error("flagged a memory whose date moved under the lock")
	}
}

// memax_v2 setting app.sweep = 'stale' sees no other space's memories:
// the sweep's policies apply to memax_v2_stale_sweeper only.
func TestStaleSweepPoliciesAreTheSweepers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz, yy := f.user("zz"), f.user("yy")
	mine := f.space(zz, policy.SpaceProject, "memax-v2")
	theirs := f.space(yy, policy.SpaceProject, "acme-web")
	f.dated(yy, theirs, "Releases go out on Thursdays.", time.Now().Add(-time.Hour))
	var n int
	err := f.asV2([]uuid.UUID{mine}, []uuid.UUID{zz}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), `SELECT set_config('app.sweep', 'stale', true)`); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM v2.memories WHERE space_id = $1`, theirs).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("memax_v2 with app.sweep = 'stale' read %d memories of another space", n)
	}
	if spaces, err := f.l.StaleDueSpaces(context.Background(), time.Now(), 10); err != nil || len(spaces) != 1 || spaces[0] != theirs {
		t.Errorf("the sweeper: %v %v", spaces, err)
	}
}
