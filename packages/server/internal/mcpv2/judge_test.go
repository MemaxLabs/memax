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

// Rule 11, §5.6 downgrade (b), from the agent's side: a push kept at once
// that the judge then finds contradicting a decision in force is back in
// Review. Recall no longer serves it as kept: the session that pushed it
// sees it among its proposals, in conflict, every connection gets a
// notice once since it was last seen, and memax_get says why and that it
// isn't kept.
func TestAgentSeesItsWriteReturnedToReview(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyWrite)
	ctx := context.Background()
	railway := e.keep(f.user, f.sp, "Deploy the v2 API to Railway.", ledger.SectionDecisions)
	cs := e.connectClient(f.token, "/mcp", older, nil)
	res := call(t, cs, "memax_push", push("Preview builds run on Fly.io machines.", f.sp, map[string]any{"session_ref": "cc-7f3a"}))
	out := structured[handler.MCPPushOutput](t, res)
	if res.IsError || out.Status != handler.MCPPushKept {
		t.Fatalf("push: %s", text(res))
	}
	ref := out.ID
	// The connection was last seen before the verdict.
	e.exec(`UPDATE v2.agent_connections SET last_seen_at = now() - interval '1 hour'`)

	_, n, _ := ledger.ParseRef(ref)
	var id uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT id FROM v2.memories WHERE space_id = $1 AND seq = $2`, f.sp.id, n).Scan(&id); err != nil {
		t.Fatal(err)
	}
	system, err := e.ledger.SpaceScope(ctx, f.sp.id)
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.ledger.Apply(ctx, &ledger.RecordVerdict{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: system, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Memory: id, Version: 1, Mode: ledger.JudgeKept, Outcome: ledger.OutcomeFlagged, Target: railway.ID,
		Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: ledger.RelationContradicts, Related: railway.ID},
	})
	if err != nil || len(v.Receipts) != 1 || v.Receipts[0].Action != ledger.ActionReturned {
		t.Fatalf("verdict: %v %+v", err, v.Receipts)
	}

	res = call(t, cs, "memax_recall", map[string]any{"query": "preview builds Fly.io machines", "session_ref": "cc-7f3a"})
	validates(t, "agent", "memax_recall", res)
	rec := structured[handler.MCPRecallOutput](t, res)
	for _, r := range rec.Results {
		if r.Ref == ref {
			t.Errorf("recall serves %s as kept: %s", ref, text(res))
		}
	}
	found := false
	for _, p := range rec.Proposals {
		found = found || (p.Ref == ref && p.State == "conflict")
	}
	if !found {
		t.Errorf("the pushing session doesn't see %s in conflict: %+v", ref, rec.Proposals)
	}
	notice := ""
	for _, nt := range rec.Notices {
		if nt.Kind == "returned" {
			notice = nt.Message
		}
	}
	mustContain(t, notice, ref, "contradicts "+railway.Ref, "aren't kept now", "don't act on them")
	mustContain(t, text(res), "Back in Review in")

	res = call(t, cs, "memax_get", map[string]any{"id": ref, "space_id": f.sp.id.String()})
	if !res.IsError {
		t.Errorf("get serves the returned write: %s", text(res))
	}
	mustContain(t, text(res), ref+" is back in Review", "contradicts "+railway.Ref, "isn't kept now", "don't act on it")

	res = call(t, cs, "memax_list", map[string]any{"hub_id": f.sp.id.String()})
	if strings.Contains(text(res), ref) {
		t.Errorf("list serves the returned write: %s", text(res))
	}
}
