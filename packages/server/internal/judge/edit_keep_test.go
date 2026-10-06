package judge_test

import (
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Rule 11 has a third way a Keep could beat the judge: Review's "edit, then
// keep" (POST :edit with keep: true). Changing one word of a proposal that
// touches a decision in force must not keep it unjudged. The edit is
// saved as the proposal's new version and judged; Keep on that version
// waits for the verdict, then keeps it or meets the conflict.
//
// Found while wiring Review's busy Keep (branch v2-review-judge): Edit
// with Keep never waited for the judge.
func TestEditThenKeepWaitsForTheJudge(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	railway := f.kept(zz, sp, decision("Deploy the v2 API to Railway for its preview environments.", "deploy target"))
	touching := f.propose(zz, sp, decision("Deploy the v2 API to Fly.io.", "deploy target"))

	// Keep alone waits (TestKeepWaitsForTheJudge).
	_, err := f.l.Apply(f.ctx, &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: touching.Ref})
	var pending *ledger.JudgePendingError
	if !errors.As(err, &pending) {
		t.Fatalf("keep before the judge = %v, want busy", err)
	}

	// Edit, then keep: saved, not kept, and the new words go to the judge.
	res := f.apply(&ledger.Edit{
		Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: touching.Ref, ExpectedVersion: touching.Version,
		Statement: "Deploy the v2 API to Fly.io in iad.", Keep: true,
	})
	if res.Memory.Lifecycle != lifecycle.Proposed || res.Outcome != ledger.OutcomeProposed || res.Policy.Code != policy.CodeJudgePending {
		t.Fatalf("edit then keep = %s %s %s; it kept words the judge hadn't seen (rule 11)",
			res.Memory.Lifecycle, res.Outcome, res.Policy.Code)
	}
	edited := res.Memory
	if job := f.judgeJob(edited, edited.Version); job.Version != edited.Version || job.Mode != ledger.JudgeProposal {
		t.Errorf("judge job = %+v", job)
	}
	// Keep on the new version waits for the verdict, as any Keep does.
	if _, err := f.l.Apply(f.ctx, &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: edited.Ref,
		ExpectedVersion: edited.Version}); !errors.As(err, &pending) {
		t.Fatalf("keep of the saved edit before the judge = %v, want busy", err)
	}
	// The judge flags the new words: Keep meets the conflict, not the seal.
	j := withModel(f, &fakeModel{answer: oracle(map[string]verdict{railway.Ref: {ledger.RelationContradicts, 0.9, false, ""}})}, tiers(false, false))
	f.run(j, edited)
	_, err = f.l.Apply(f.ctx, &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: edited.Ref, ExpectedVersion: edited.Version})
	var ic *ledger.InConflictError
	if !errors.As(err, &ic) || ic.With != railway.Ref {
		t.Errorf("keep of the flagged edit = %v", err)
	}
	if m := f.get(zz, edited.ID); m.Lifecycle != lifecycle.Proposed || m.State != lifecycle.MarkConflict {
		t.Errorf("after: %s %s", m.Lifecycle, m.State)
	}
}
