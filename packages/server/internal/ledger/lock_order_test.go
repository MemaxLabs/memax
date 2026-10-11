package ledger_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The lock order (locks.go), proven on the real database: each test
// pauses one command while it holds the locks it took first, lets a second
// command take its own and wait on the first's, then lets the first go on
// to ask for the second's. Two commands that lock in opposite orders
// deadlock there every time (40P01 within deadlock_timeout); in the one
// order, the second waits for the first and both finish. Nothing depends
// on timing: the pauses are row locks the test holds, and the test moves
// on only once Postgres says the commands wait.

// barrier is a row lock the test holds in its own transaction, so that a
// command waits there holding what it locked before.
type barrier struct {
	f  *fixture
	tx pgx.Tx
}

// holdKey holds the idempotency key a command will claim: the command
// waits at its claim (ON CONFLICT waits for the transaction that inserted
// the key) until release.
func (f *fixture) holdKey(space uuid.UUID, m ledger.Meta) *barrier {
	f.t.Helper()
	var actor *uuid.UUID
	if m.Actor.ID != uuid.Nil {
		id := m.Actor.ID
		actor = &id
	}
	return f.hold(`INSERT INTO v2.command_keys (space_id, actor_kind, actor_id, idempotency_key, command, request_hash)
		VALUES ($1, $2, $3, $4, 'barrier', '\x00')`, space, string(m.Actor.Kind), actor, m.IdempotencyKey)
}

// hold runs sql in a transaction it keeps open until release.
func (f *fixture) hold(sql string, args ...any) *barrier {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		_ = tx.Rollback(ctx)
		f.t.Fatalf("barrier %q: %v", firstLine(sql), err)
	}
	b := &barrier{f: f, tx: tx}
	f.t.Cleanup(b.release)
	return b
}

func (b *barrier) release() { _ = b.tx.Rollback(context.Background()) }

// awaitWaiting waits until n of this database's backends wait on a lock.
func (f *fixture) awaitWaiting(n int) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := f.count(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`)
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("%d backends wait on a lock after 10 s, want %d", got, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// applyAsync applies a command on its own goroutine.
func (f *fixture) applyAsync(l *ledger.Ledger, cmd ledger.Command) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := l.Apply(context.Background(), cmd)
		done <- err
	}()
	return done
}

// result waits for a command applyAsync started.
func (f *fixture) result(name string, done <-chan error) error {
	f.t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		f.t.Fatalf("%s didn't finish within 30 s", name)
		return nil
	}
}

// interleave applies first until it waits at barrier (holding what it
// locked before it), then second until it waits on a lock, then releases
// the barrier and returns both commands' errors. One attempt each: the
// retry on a deadlock (Apply's backstop) would hide the cycle these tests
// look for.
func (f *fixture) interleave(first ledger.Command, b *barrier, second ledger.Command) (error, error) {
	f.t.Helper()
	l := f.oneTry()
	f.awaitWaiting(0)
	d1 := f.applyAsync(l, first)
	f.awaitWaiting(1)
	d2 := f.applyAsync(l, second)
	f.awaitWaiting(2)
	b.release()
	return f.result(string(first.Name()), d1), f.result(string(second.Name()), d2)
}

// oneTry is the fixture's ledger with the deadlock retry off.
func (f *fixture) oneTry() *ledger.Ledger {
	opts := []ledger.Option{ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), ledger.WithCommandAttempts(1)}
	if f.inserter != nil {
		opts = append(opts, ledger.WithJobs(f.inserter))
	}
	return ledger.New(f.pool, opts...)
}

func isDeadlock(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && (pg.Code == "40P01" || pg.Code == "40001")
}

// noDeadlock fails on a deadlock (or a serialization failure), and on any
// error but the domain ones allowed.
func noDeadlock(t *testing.T, name string, err error, allowed ...error) {
	t.Helper()
	switch {
	case err == nil:
	case isDeadlock(err):
		t.Errorf("%s: %v", name, err)
	default:
		for _, a := range allowed {
			if errors.Is(err, a) {
				return
			}
		}
		t.Errorf("%s: %v", name, err)
	}
}

// The cycle CI logged on Oct 11, 2026 ("ledger: mark targets dirty:
// deadlock detected"): the compile job's RecordCompile locks its target,
// then its compile run's foreign key on the Brief takes FOR KEY SHARE on
// the Brief; Forget locks the Brief to take the words out of it, then
// bumps every target. FOR UPDATE on the Brief blocked the foreign key's
// FOR KEY SHARE: target, Brief against Brief, target.
func TestRecordCompileBesideForget(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "The staging vault rotates on Thursdays.")
	f.brief(zz, space, 0, demoSections(m.Ref))
	tg := f.target(zz, space, ledger.TargetAgentsMD)

	rc := f.recordCompileCmd(tg, "compiled", m.Ref)
	b := f.holdKey(space, rc.Meta)
	forget := forgetCmd(person(zz), f.scope(zz), policy.ViaWeb, m.Ref, 1)
	errCompile, errForget := f.interleave(rc, b, forget)
	noDeadlock(t, "record compile", errCompile, ledger.ErrBehind)
	noDeadlock(t, "forget", errForget)
	if got := f.mem(zz, m.ID); got.Lifecycle != "forgotten" {
		t.Errorf("after: %s", got.Lifecycle)
	}
	_ = ctx
}

// recordCompileCmd is the RecordCompile the compile job would apply for
// the target's current generation.
func (f *fixture) recordCompileCmd(t *ledger.Target, content string, refs ...string) *ledger.RecordCompile {
	f.t.Helper()
	ctx := context.Background()
	scope, err := f.l.SpaceScope(ctx, t.SpaceID)
	if err != nil {
		f.t.Fatal(err)
	}
	cur, err := f.l.GetTarget(ctx, scope, t.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	ref, err := f.l.ReserveCompileRef(ctx, scope, t.SpaceID)
	if err != nil {
		f.t.Fatal(err)
	}
	brief, err := f.briefOf(scope, t.SpaceID)
	if err != nil {
		f.t.Fatal(err)
	}
	out := ledger.CompiledOutput{Path: t.Path, SHA256: sha(content), DriftSHA256: sha(content), Bytes: len(content),
		Lines: strings.Count(content, "\n"), Refs: append([]string{}, refs...), Cites: []string{}, DroppedForBudget: []string{}}
	outSHA, drift := ledger.OutputManifests([]ledger.CompiledOutput{out})
	now := time.Now()
	return &ledger.RecordCompile{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Target: t.ID, Ref: ref, Generation: cur.DirtyGen, BriefID: brief.ID, BriefVersion: brief.Version,
		InputSHA256: sha("input " + content), OutputSHA256: outSHA, DriftSHA256: drift,
		ArtifactKey: "test/" + ref, Bytes: out.Bytes, Lines: out.Lines, Files: []ledger.CompiledOutput{out}, Refs: refs,
		EnqueuedAt: cur.DirtyAt, StartedAt: now, CompiledAt: now,
	}
}

// The judge's verdict on a proposal that cites a kept memory, beside that
// memory's Forget, which carries the proposal: the judge locked the
// proposal and then (for its verdict's target) the memory; Forget locked
// the memory and then what goes with it.
func TestVerdictBesideForget(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "Background jobs run on River.")
	nm := fact(space, "River retries a failed job three times.")
	nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceMemory, Ref: m.Ref}}
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP), NewMemory: nm}).Memory

	forget := forgetCmd(person(zz), f.scope(zz), policy.ViaWeb, m.Ref, 1, p.Ref)
	b := f.holdKey(space, forget.Meta)
	verdict := f.verdictCmd(space, p, ledger.OutcomeLinked, m)
	errForget, errVerdict := f.interleave(forget, b, verdict)
	noDeadlock(t, "forget", errForget)
	noDeadlock(t, "verdict", errVerdict)
	if got := f.mem(zz, p.ID); got.Lifecycle != "forgotten" {
		t.Errorf("the proposal after: %s", got.Lifecycle)
	}
}

// verdictCmd is judgeAs's RecordVerdict, unapplied.
func (f *fixture) verdictCmd(space uuid.UUID, m *ledger.Memory, outcome ledger.VerdictOutcome, target *ledger.Memory) *ledger.RecordVerdict {
	f.t.Helper()
	scope, err := f.l.SpaceScope(context.Background(), space)
	if err != nil {
		f.t.Fatal(err)
	}
	rel := map[ledger.VerdictOutcome]ledger.Relation{ledger.OutcomeFlagged: ledger.RelationContradicts,
		ledger.OutcomeLinked: ledger.RelationUpdates, ledger.OutcomeFolded: ledger.RelationDuplicate}[outcome]
	return &ledger.RecordVerdict{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Memory: m.ID, Version: m.Version, Mode: ledger.JudgeProposal, Outcome: outcome, Target: target.ID,
		Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: rel, Related: target.ID},
	}
}

// Keeping an agent's update of a memory supersedes the memory; forgetting
// the memory carries the update. Keep locked the update and then the
// memory it updates; Forget, the memory and then the update.
func TestKeepBesideForget(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "Background jobs run on River.")
	upd := f.apply(&ledger.Edit{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP), Memory: m.Ref,
		ExpectedVersion: 1, Statement: "Background jobs run on River, never on Temporal."}).Memory
	if upd.ID == m.ID {
		t.Fatal("the agent's edit applied in place")
	}

	keep := &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: upd.Ref}
	b := f.holdKey(space, keep.Meta)
	forget := forgetCmd(person(zz), f.scope(zz), policy.ViaWeb, m.Ref, 1, upd.Ref)
	errKeep, errForget := f.interleave(keep, b, forget)
	noDeadlock(t, "keep", errKeep)
	// Forget comes second: the memory it names was kept over meanwhile
	// (superseded is a fact's no-op) and the update is no longer a
	// proposal, so what goes with it changed.
	noDeadlock(t, "forget", errForget, ledger.ErrForgetCarries)
}

// Two people settle one conflict at once, from either side: each locked
// the memory it named, then the other side.
func TestResolveConflictFromBothSides(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	p, d := f.conflictPair(zz, space)

	fromP := &ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: p.Ref, Other: d.Ref, Choice: ledger.ChooseOther}
	b := f.holdKey(space, fromP.Meta)
	fromD := &ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: d.Ref, Other: p.Ref, Choice: ledger.ChooseThis}
	errP, errD := f.interleave(fromP, b, fromD)
	noDeadlock(t, "settle from the proposal", errP)
	// The second finds it settled.
	noDeadlock(t, "settle from the decision", errD, ledger.ErrInvalidTransition, ledger.ErrNotFound, ledger.ErrEditClash)
}

// Forget takes a forgotten memory out of a hand edit's changeset, under
// its target's lock, and then bumps every target; a Keep bumps every
// target in id order. Forget held the later target and asked for the
// earlier one: targets are locked once, in id order.
func TestForgetTargetsBesideKeep(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "pnpm workspaces only.")
	f.brief(zz, space, 0, demoSections(m.Ref))
	first := f.target(zz, space, ledger.TargetClaudeMD)
	later := f.target(zz, space, ledger.TargetAgentsMD)
	if strings.Compare(first.ID.String(), later.ID.String()) >= 0 {
		t.Fatalf("targets out of order: %s, %s", first.ID, later.ID)
	}
	run := f.compiled(f.get(zz, later.ID), "compiled v1", m.Ref)
	f.apply(&ledger.RecordDelivery{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Target: later.ID, Compile: run.Ref, SHA256: run.DriftSHA256})
	content := "hand edited"
	obs := f.apply(&ledger.RecordObservation{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Target: later.ID, Path: "AGENTS.md",
		ObservedSHA256: sha(content), ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop", ArtifactKey: "obs/" + sha(content),
		Bytes: len(content), Changes: ledger.ChangeSet{Changes: []ledger.DriftChange{
			{Kind: ledger.ChangeEdit, Ref: m.Ref, Refs: []string{m.Ref}, OldText: m.Statement, NewText: "pnpm only; never npm.", OldLine: 4, NewLine: 4},
		}}}).Observations[0]
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP),
		NewMemory: fact(space, "Prefer named exports.")}).Memory

	// Forget waits at the observation it rewrites, holding what it locked
	// before it.
	b := f.hold(`SELECT 1 FROM v2.target_observations WHERE id = $1 FOR UPDATE`, obs.ID)
	forget := forgetCmd(person(zz), f.scope(zz), policy.ViaWeb, m.Ref, 1)
	keep := &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: p.Ref}
	errForget, errKeep := f.interleave(forget, b, keep)
	noDeadlock(t, "forget", errForget)
	noDeadlock(t, "keep", errKeep)
}

// The backstop: a command Postgres aborts for a deadlock runs again, once
// the other transaction is out of the way, and says so in the log (the
// command and the code, no words). The cycle here is the test's own: it
// holds the verdict's proposal, the verdict locks the decision and waits
// for the proposal, then the test asks for the decision. The verdict
// waited first, so its deadlock check runs first and it is the one aborted.
func TestApplyRunsADeadlockedCommandAgain(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	d := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
		NewMemory: decisionIn(space, "Deploy the v2 API to Railway.", "deploy target")}).Memory
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP),
		NewMemory: decisionIn(space, "Deploy the v2 API to Railway, in two regions.", "deploy target")}).Memory
	if strings.Compare(d.ID.String(), p.ID.String()) >= 0 {
		t.Fatalf("memories out of order: %s, %s", d.ID, p.ID)
	}
	held := f.hold(`SELECT 1 FROM v2.memories WHERE id = $1 FOR UPDATE`, p.ID)
	done := f.applyAsync(f.l, f.verdictCmd(space, p, ledger.OutcomeLinked, d))
	f.awaitWaiting(1)
	taken := make(chan error, 1)
	go func() {
		_, err := held.tx.Exec(ctx, `SELECT 1 FROM v2.memories WHERE id = $1 FOR UPDATE`, d.ID)
		held.release()
		taken <- err
	}()
	if err := <-taken; err != nil {
		t.Fatalf("the test's transaction was the one aborted: %v", err)
	}
	if err := f.result("record verdict", done); err != nil {
		t.Fatalf("record verdict: %v", err)
	}
	if got := f.mem(zz, p.ID); !hasLink(got, ledger.LinkSupersedes, ledger.LinkOut, d.ID) {
		t.Errorf("the verdict didn't apply: %+v", got.Links)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, `msg="ledger: retrying a command" command=record_verdict code=40P01 attempt=2`) {
		t.Errorf("no retry logged:\n%s", logs)
	}
	if strings.Contains(logs, p.Statement) || strings.Contains(logs, d.Statement) {
		t.Errorf("the log holds memory text:\n%s", logs)
	}
}
