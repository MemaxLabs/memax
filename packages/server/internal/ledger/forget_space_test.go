package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// sealAndVerify seals everything in the space and verifies its chain from
// genesis.
func (f *fixture) sealAndVerify(space uuid.UUID) *ledger.Verification {
	f.t.Helper()
	ctx := context.Background()
	// The watermark is the cluster's oldest transaction, and parallel
	// tests hold some: seal until every receipt is below it.
	var v *ledger.Verification
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := f.l.SealSpace(ctx, space, nil, 1000); err != nil {
			f.t.Fatalf("seal: %v", err)
		}
		var err error
		if v, err = f.l.VerifySpace(ctx, space, nil); err != nil {
			f.t.Fatalf("verify: %v", err)
		}
		if v.Unsealed == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !v.OK() {
		f.t.Errorf("the chain doesn't verify: %+v", v.Problems)
	}
	if v.Unsealed != 0 {
		f.t.Errorf("%d receipts unsealed", v.Unsealed)
	}
	return v
}

// seedSpace fills a space with what a Forget of it must reach: memories
// (one with a source quote and a reason), a decision a gate became, a gate
// still waiting, a Brief, a compiled target and a read.
func (f *fixture) seedSpace(owner, space uuid.UUID) []*ledger.Memory {
	f.t.Helper()
	scope := f.scope(owner).Narrow(space)
	nm := fact(space, "Jiahao is away from Oct 12 to Oct 26.")
	nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceSession, Ref: "Session 3e1a", Quote: "away Oct 12-26"}}
	rm := meta(person(owner), scope, policy.ViaWeb)
	rm.Reason = "said in standup"
	a := f.apply(&ledger.Remember{Meta: rm, NewMemory: nm}).Memory
	b := f.remember(owner, space, "Route reviews to Ziyang.")
	conn := f.connect(owner, ledger.CredentialAPIKey, f.apiKey(owner, "codex", nil), ledger.AgentCodex, at(space, policy.AutonomyPropose))
	actor, ascope := f.agentActor(owner, conn, policy.AutonomyPropose)
	g := f.asked(actor, ascope.Narrow(space), space)
	d := f.apply(answer(person(owner), scope, policy.ViaWeb, g.Ref, 1)).Memory
	f.asked(actor, ascope.Narrow(space), space) // a second gate, left waiting
	f.brief(owner, space, 0, []ledger.BriefSection{{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{
		{Ref: a.Ref}, {Text: "Ziyang covers for Jiahao.", Cites: []string{a.Ref, b.Ref}}}}})
	tg := f.target(owner, space, ledger.TargetAgentsMD)
	f.compiled(tg, "- Jiahao is away [M]\n", a.Ref, b.Ref)
	f.record(readOf(a, conn.ID, owner, "codex", ledger.ReadRecall, time.Now(), a.ID))
	return []*ledger.Memory{a, b, d}
}

// Forgetting everything in a space reaches every memory, gate, Brief
// version and reason in it, keeps the space usable, and its chain still
// verifies.
func TestForgetSpace(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	ms := f.seedSpace(zz, space)
	f.sealAndVerify(space)
	scope := f.scope(zz).Narrow(space)

	// Only the owner may.
	jy := f.user("jy")
	f.join(space, jy, "admin")
	refusedWith(t, f.apply(&ledger.ForgetSpace{Meta: meta(person(jy), f.scope(jy).Narrow(space), policy.ViaWeb), SpaceID: space}),
		policy.CodeForgetNotAllowed)

	res := f.apply(&ledger.ForgetSpace{Meta: meta(person(zz), scope, policy.ViaAPI), SpaceID: space})
	if res.Outcome != ledger.OutcomeApplied || res.Tombstone == nil || res.Tombstone.Kind != ledger.ObjectSpace {
		t.Fatalf("forget space: %+v", res)
	}
	if res.Tombstone.Gone.Memories != 3 {
		t.Errorf("gone = %+v", res.Tombstone.Gone)
	}
	for _, m := range ms {
		if got := f.mem(zz, m.ID); got.Lifecycle != lifecycle.Forgotten {
			t.Errorf("%s: %s", m.Ref, got.Lifecycle)
		}
	}
	for q, what := range map[string]string{
		`SELECT count(*) FROM v2.memory_versions WHERE space_id = $1 AND statement IS NOT NULL`:                        "versions with words",
		`SELECT count(*) FROM v2.sources WHERE space_id = $1 AND quote IS NOT NULL`:                                    "quotes",
		`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND reason IS NOT NULL`:                                  "reasons",
		`SELECT count(*) FROM v2.decision_gates WHERE space_id = $1 AND (question IS NOT NULL OR options IS NOT NULL)`: "gates with words",
		`SELECT count(*) FROM v2.brief_versions v, jsonb_array_elements(v.structure->'sections') s, jsonb_array_elements(s->'items') i
		  WHERE v.space_id = $1 AND i ? 'text'`: "Brief prose",
		`SELECT count(*) FROM v2.brief_versions WHERE space_id = $1 AND (title <> 'Brief' OR summary IS NOT NULL)`: "Brief titles",
	} {
		if n := f.count(q, space); n != 0 {
			t.Errorf("%d %s left", n, what)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.tombstones WHERE op_id = $1`, res.Tombstone.ID); n != 4 {
		t.Errorf("%d tombstones in the op, want the space's and 3 memories'", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.agent_notices WHERE op_id = $1 AND kind = 'space_forgotten'`, res.Tombstone.ID); n != 1 {
		t.Errorf("%d notices", n)
	}
	// The space stays usable, and its chain verifies with every reason gone.
	f.remember(zz, space, "A fresh start.")
	f.sealAndVerify(space)
	if _, err := f.l.GetTombstone(ctx, scope, ms[0].Ref); err != nil {
		t.Errorf("a memory's tombstone: %v", err)
	}
}

// Deleting a space retires its V2 rows: only receipts (reasons redacted),
// seals, tombstones and notices stay, V1 can then delete the hub, and the
// chain still seals and verifies afterwards.
func TestRetireSpaceLetsTheHubGo(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.seedSpace(zz, space)
	f.sealAndVerify(space)
	// Before, V1 can't delete the hub.
	if _, err := f.pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1`, space); err == nil {
		t.Fatal("V1 deleted a hub that holds V2 records")
	}
	ok, err := f.l.ForgetSpaceForV1(ctx, zz, space, true)
	if err != nil || !ok {
		t.Fatalf("ForgetSpaceForV1: %v %v", ok, err)
	}
	for _, table := range []string{"memories", "memory_versions", "sources", "memory_sources", "memory_links", "judge_verdicts",
		"briefs", "brief_versions", "targets", "compile_runs", "target_observations", "decision_gates", "undo_entries",
		"command_keys", "agent_connection_spaces", "reads", "read_rollups", "memory_embeddings", "forget_requests"} {
		if n := f.count(`SELECT count(*) FROM v2.`+table+` WHERE space_id = $1`, space); n != 0 {
			t.Errorf("v2.%s keeps %d rows", table, n)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1`, space); n == 0 {
		t.Error("the receipts went")
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND reason IS NOT NULL`, space); n != 0 {
		t.Errorf("%d reasons left", n)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1`, space); err != nil {
		t.Fatalf("V1 delete of the hub after retiring: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM v2.space_ledgers WHERE space_id = $1 AND retired_at IS NOT NULL`, space); n != 1 {
		t.Error("the space's ledger row isn't kept as retired")
	}
	// The forgot receipts are sealed after the hub is gone, and the chain
	// verifies from genesis.
	v := f.sealAndVerify(space)
	if v.Receipts == 0 {
		t.Error("nothing verified")
	}
	// Nothing reaches a retired space any more.
	if _, err := f.l.SpaceScope(ctx, space); err != nil {
		t.Errorf("SpaceScope of a retired space (for its seals): %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), NewMemory: fact(space, "Back?")}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("remember into a deleted space: %v", err)
	}
}

// ForgetAccount forgets the person's personal and project spaces (which
// stay, empty), leaves team spaces with their members, and disconnects
// every agent of theirs.
func TestForgetAccount(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	team := f.space(zz, policy.SpaceTeam, "memax-team")
	f.join(team, jy, "contributor")
	p := f.remember(zz, personal, "I prefer tabs.")
	q := f.remember(zz, project, "Pin Node 24.")
	tm := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz).Narrow(team), policy.ViaWeb), NewMemory: fact(team, "The team ships on Fridays.")}).Memory
	conn := f.connect(zz, ledger.CredentialAPIKey, f.apiKey(zz, "codex", nil), ledger.AgentCodex, at(project, policy.AutonomyPropose))

	out, err := f.l.ForgetAccount(ctx, zz, policy.ViaAPI)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Spaces) != 2 || len(out.Kept) != 1 || len(out.Disconnected) != 1 || out.Disconnected[0] != conn.ID {
		t.Errorf("forget account: %+v", out)
	}
	if f.mem(zz, p.ID).Lifecycle != lifecycle.Forgotten || f.mem(zz, q.ID).Lifecycle != lifecycle.Forgotten {
		t.Error("the personal or project memory isn't forgotten")
	}
	if got, err := f.l.GetMemory(ctx, f.scope(jy).Narrow(team), tm.Ref); err != nil || got.Lifecycle != lifecycle.Kept {
		t.Errorf("the team's memory: %+v, %v", got, err)
	}
	if n := f.count(`SELECT count(*) FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL`, zz); n != 0 {
		t.Errorf("%d keys still work", n)
	}
	for _, sp := range []uuid.UUID{personal, project} {
		f.sealAndVerify(sp)
	}
	// Repeating it finds nothing more to do.
	again, err := f.l.ForgetAccount(ctx, zz, policy.ViaAPI)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Disconnected) != 0 {
		t.Errorf("again: %+v", again)
	}
}
