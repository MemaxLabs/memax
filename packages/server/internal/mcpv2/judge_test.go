package mcpv2_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// supersede keeps an explicit change of decision d, linked by the judge
// (as Memax), so d is superseded: it stays kept, out of force.
func (e *env) supersede(user uuid.UUID, sp space, d *ledger.Memory, statement string) *ledger.Memory {
	e.t.Helper()
	ctx := context.Background()
	scope, err := e.ledger.UserScope(ctx, user)
	if err != nil {
		e.t.Fatal(err)
	}
	me := func() ledger.Meta {
		return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user}, Scope: scope.Narrow(sp.id),
			Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()}
	}
	res, err := e.ledger.Apply(ctx, &ledger.Propose{Meta: me(), NewMemory: ledger.NewMemory{SpaceID: sp.id,
		Statement: statement, Section: ledger.SectionDecisions, Kind: ledger.KindDecision}})
	if err != nil {
		e.t.Fatal(err)
	}
	p := res.Memory
	system, err := e.ledger.SpaceScope(ctx, sp.id)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.ledger.Apply(ctx, &ledger.RecordVerdict{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: system, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Memory: p.ID, Version: 1, Mode: ledger.JudgeProposal, Outcome: ledger.OutcomeSuperseding, Target: d.ID,
		Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: ledger.RelationUpdates, Related: d.ID},
	}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.ledger.Apply(ctx, &ledger.Keep{Meta: me(), Memory: p.ID.String()}); err != nil {
		e.t.Fatal(err)
	}
	old, err := e.ledger.GetMemory(ctx, scope, d.ID.String())
	if err != nil || old.Decision == nil || old.Decision.Status != ledger.DecisionSuperseded {
		e.t.Fatalf("not superseded: %v %+v", err, old)
	}
	return p
}

// A superseded decision stays kept with its history, but recall, search
// and the digest serve only the decision that replaced it.
func TestSupersededDecisionsLeaveRecall(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	railway := e.keep(f.user, f.sp, "Deploy the v2 API to Railway.", ledger.SectionDecisions)
	fly := e.supersede(f.user, f.sp, railway, "We moved the v2 API from Railway to Fly.io.")
	cs := e.connectClient(f.token, "/mcp", legacy, nil)

	for _, tool := range []string{"memax_recall", "memax_search"} {
		res := call(t, cs, tool, map[string]any{"query": "v2 API Railway", "hub_id": f.sp.id.String()})
		out := structured[handler.MCPRecallOutput](t, res)
		var refs []string
		for _, r := range out.Results {
			refs = append(refs, r.Ref)
		}
		if strings.Contains(strings.Join(refs, ","), railway.Ref) || !strings.Contains(strings.Join(refs, ","), fly.Ref) {
			t.Errorf("%s: %v; want %s and not the superseded %s", tool, refs, fly.Ref, railway.Ref)
		}
	}
	res := call(t, cs, "memax_recall", map[string]any{"hub_id": f.sp.id.String()})
	if got := text(res); strings.Contains(got, "Deploy the v2 API to Railway.") || !strings.Contains(got, "from Railway to Fly.io") {
		t.Errorf("digest:\n%s", got)
	}
	// It is still readable by reference, with its status.
	if res := call(t, cs, "memax_get", map[string]any{"id": railway.ID.String()}); res.IsError {
		t.Errorf("get: %s", text(res))
	}
}

// A Write agent's push that touches a decision in force goes to Review,
// and the result says so plainly.
func TestPushTouchingADecisionGoesToReview(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyWrite)
	e.keep(f.user, f.sp, "Deploy the v2 API to Railway for its preview environments.", ledger.SectionDecisions)
	cs := e.connectClient(f.token, "/mcp", older, nil)
	res := call(t, cs, "memax_push", push("Deploy the v2 API to Fly.io in iad and ams.", f.sp, map[string]any{"section": "decisions"}))
	validates(t, "agent", "memax_push", res)
	out := structured[handler.MCPPushOutput](t, res)
	if res.IsError || out.Status != handler.MCPPushProposed {
		t.Fatalf("push: %s", text(res))
	}
	mustContain(t, text(res), "touches a decision in force", "went to Review", out.ID, "https://memax.test/")
	if lifecycle, _, _ := e.memory(f.sp, out.ID); lifecycle != "proposed" {
		t.Errorf("lifecycle = %s", lifecycle)
	}
	// A push that touches nothing is still kept at once.
	res = call(t, cs, "memax_push", push("Background jobs retry five times.", f.sp))
	if out := structured[handler.MCPPushOutput](t, res); out.Status != handler.MCPPushKept {
		t.Errorf("unrelated push: %s", text(res))
	}
}
