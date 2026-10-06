package judge_test

import (
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Rule 11 has a third way a Keep can beat the judge: Review's "edit, then
// keep" (POST :edit with keep: true). A person who changes one word of a
// proposal that touches a decision in force, before the judge has run,
// keeps it at once: Edit never calls awaitJudge, so neither the old
// version (not judged yet) nor the new one is checked before it's kept.
//
// Found while wiring Review's busy Keep (branch v2-review-judge). This
// test fails on purpose until Edit with Keep waits for the judge like
// Keep does (503 busy for at most JudgeGrace).
func TestEditThenKeepWaitsForTheJudge(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	f.kept(zz, sp, decision("Deploy the v2 API to Railway for its preview environments.", "deploy target"))
	touching := f.propose(zz, sp, decision("Deploy the v2 API to Fly.io.", "deploy target"))

	// Keep alone waits (TestKeepWaitsForTheJudge).
	_, err := f.l.Apply(f.ctx, &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: touching.Ref})
	var pending *ledger.JudgePendingError
	if !errors.As(err, &pending) {
		t.Fatalf("keep before the judge = %v, want busy", err)
	}

	// Edit, then keep, must wait too.
	res, err := f.l.Apply(f.ctx, &ledger.Edit{
		Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: touching.Ref, ExpectedVersion: touching.Version,
		Statement: "Deploy the v2 API to Fly.io in iad.", Keep: true,
	})
	if errors.As(err, &pending) {
		return
	}
	if err != nil {
		t.Fatalf("edit then keep = %v", err)
	}
	if res.Memory != nil && res.Memory.Lifecycle == lifecycle.Kept {
		t.Errorf("edit then keep kept %s before the judge looked at it (rule 11); want 503 busy like Keep", res.Memory.Ref)
	}
}
