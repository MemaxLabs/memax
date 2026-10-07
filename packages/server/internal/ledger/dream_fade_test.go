package ledger_test

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Fading never takes a fact that sits in a compiled file whose loads Memax
// can't observe (plan 25 §5.10), a decision in force, or one the Brief
// places; it does take a fact no file carries, after 60 days unread.
func TestFadeCandidatesSpareWhatFilesCarry(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	inFile := f.remember(zz, space, "Use tabs, not spaces, in generated Go files.")
	loose := f.remember(zz, space, "The staging database is reset every Sunday night.")
	placed := f.remember(zz, space, "Background jobs run on River, not Temporal.")
	nm := ledger.NewMemory{SpaceID: space, Statement: "Use pnpm workspaces only.", Section: ledger.SectionDecisions,
		Kind: ledger.KindDecision, Decision: &ledger.DecisionFields{Area: "package manager"}}
	decision := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), NewMemory: nm}).Memory
	f.brief(zz, space, 0, demoSections(placed.Ref))
	target := f.target(zz, space, ledger.TargetAgentsMD)
	f.compiled(target, "# Brief\n", inFile.Ref, placed.Ref)

	later := f.clockAt(time.Now().Add(61 * 24 * time.Hour))
	cands, err := later.FadeCandidates(ctx, f.scope(zz), space, 60*24*time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, c := range cands {
		refs = append(refs, c.Ref)
	}
	if len(refs) != 1 || refs[0] != loose.Ref {
		t.Errorf("fade candidates %v, want only %s (%s is in a file nobody reports loading, %s in the Brief, %s a decision in force)",
			refs, loose.Ref, inFile.Ref, placed.Ref, decision.Ref)
	}
	// Nothing is 60 days old yet.
	if cands, err := f.l.FadeCandidates(ctx, f.scope(zz), space, 60*24*time.Hour, 10); err != nil || len(cands) != 0 {
		t.Errorf("fresh memories fade: %v %v", cands, err)
	}
}
