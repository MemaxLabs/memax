package v2api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Rule 11's two holes, closed, over /v2.

type receiptWithSource struct {
	ID        uuid.UUID `json:"id"`
	Action    string    `json:"action"`
	ActorKind string    `json:"actor_kind"`
	Reason    string    `json:"reason"`
	Source    struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	} `json:"source"`
}

// §5.6 downgrade (b): a Write-level agent's write the inline check let
// through is kept at once; when the judge finds it contradicts a decision
// in force, it is back in Review as a conflict, with a `returned` receipt
// naming the decision, and only settling it keeps it again (the return
// isn't undoable).
func TestJudgeReturnsAWriteToReview(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	owner := e.session(zz)
	var railway result
	e.do(call{method: "POST", path: memoriesPath(sp), token: owner,
		body: decisionBody("Deploy the v2 API to Railway for its preview environments.", "deploy target")}).ok(201, &railway)
	scope, err := e.ledger.UserScope(ctx, zz)
	if err != nil {
		t.Fatal(err)
	}
	writer := ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Name: "Codex", Agent: "codex", Autonomy: policy.AutonomyWrite}
	w, err := e.ledger.Apply(ctx, &ledger.Propose{Meta: ledger.Meta{Actor: writer, Scope: scope, Via: policy.ViaMCP, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: sp.id, Statement: "Preview builds run on Fly.io machines.", Section: ledger.SectionConventions}})
	if err != nil || w.Memory.Lifecycle != "kept" {
		t.Fatalf("the agent's write: %v %+v", err, w.Memory)
	}
	e.judgeAll(judge.New(e.ledger, contradicting{}, judge.Config{Primary: judge.Tier{Model: "fake"}, Log: quiet}))

	m := memory{ID: w.Memory.ID, Ref: w.Memory.Ref}
	var got struct {
		Memory judged `json:"memory"`
	}
	e.do(call{method: "GET", path: memoryPath(m, ""), token: owner}).ok(200, &got)
	if got.Memory.Lifecycle != "proposed" || got.Memory.State != "conflict" || len(got.Memory.Links) != 1 ||
		got.Memory.Links[0].Kind != "conflicts_with" || got.Memory.Links[0].Ref != railway.Memory.Ref {
		t.Fatalf("after the verdict: %+v %+v", got.Memory.memory, got.Memory.Links)
	}
	var review page[judged]
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/review", token: owner}).ok(200, &review)
	if len(review.Items) != 1 || review.Items[0].ID != m.ID || review.Items[0].State != "conflict" {
		t.Fatalf("review = %+v", review.Items)
	}
	var rcs page[receiptWithSource]
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/receipts?memory=" + m.ID.String(), token: owner}).ok(200, &rcs)
	ret := rcs.Items[0]
	if ret.Action != "returned" || ret.ActorKind != "memax" || ret.Source.Kind != "memory" || ret.Source.Ref != railway.Memory.Ref {
		t.Fatalf("receipts = %+v", rcs.Items)
	}
	// Not undoable: settle it instead.
	r := e.do(call{method: "POST", path: "/v2/receipts/" + ret.ID.String() + ":undo", token: owner})
	r.fails(409, "undo_refused")
	var refusal struct {
		Error struct {
			Details struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &refusal); err != nil || refusal.Error.Details.Reason != "not_undoable" {
		t.Errorf("undo the return = %s", r.body)
	}
	e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: owner}).fails(409, "in_conflict")
	var settled changes
	e.do(call{method: "POST", path: memoryPath(m, ":resolve-conflict"), token: owner,
		body: map[string]any{"choice": "keep_both"}}).ok(200, &settled)
	if settled.Outcome != "applied" || settled.Memory.Lifecycle != "kept" || settled.Memory.State != "kept" {
		t.Errorf("settled = %+v", settled.Memory.memory)
	}
}

// heldResult is a "keep both" that waits for the judge.
type heldResult struct {
	changes
	Policy struct {
		Effect string `json:"effect"`
		Code   string `json:"code"`
	} `json:"policy"`
}

// "Keep both": narrower words that touch another decision in force are
// saved and the conflict stays open (200, outcome proposed, policy
// judge_pending, no Retry-After); the same resolution then waits for the
// judge (503 judge_pending), and applies, or answers 409 in_conflict
// naming the decision the words contradict. A kept side's words wait as a
// draft, out of force until the resolution applies.
func TestKeepBothWaitsForTheJudgeOverV2(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	owner := e.session(zz)
	flagging := judge.New(e.ledger, contradicting{}, judge.Config{Primary: judge.Tier{Model: "fake"}, Log: quiet})
	clearing := judge.New(e.ledger, nil, judge.Config{Log: quiet})
	// conflict is Railway, a proposal the judge flagged against it, and
	// then a third decision, about previews, that narrowed words can touch.
	conflict := func(name string) (railway, fly, previews memory) {
		sp := e.space(zz, policy.SpaceProject, name)
		key, _ := e.apiKey(zz, keyOpts{agent: "codex"}) // connected to the spaces that exist
		var r result
		e.do(call{method: "POST", path: memoriesPath(sp), token: owner,
			body: decisionBody("Deploy the v2 API to Railway.", "deploy target")}).ok(201, &r)
		railway = r.Memory
		e.do(call{method: "POST", path: memoriesPath(sp), token: key,
			body: decisionBody("Deploy the v2 API to Fly.io in iad and ams.", "deploy target")}).ok(201, &r)
		fly = r.Memory
		e.judgeAll(flagging)
		e.do(call{method: "POST", path: memoriesPath(sp), token: owner,
			body: decisionBody("Preview environments run on Render.", "previews")}).ok(201, &r)
		return railway, fly, r.Memory
	}
	resolve := func(m memory, version int, body map[string]any) *resp {
		body["choice"] = "keep_both"
		return e.do(call{method: "POST", path: memoryPath(m, ":resolve-conflict"), token: owner,
			header: map[string]string{"If-Match": fmt.Sprintf(`"%d"`, version)}, body: body})
	}
	held := func(r *resp, want ...string) heldResult {
		t.Helper()
		var res heldResult
		r.ok(200, &res)
		if res.Outcome != "proposed" || res.Policy.Code != "judge_pending" || res.Policy.Effect != "propose" || r.header.Get("Retry-After") != "" {
			t.Fatalf("held = %s %s %s (Retry-After %q)", res.Outcome, res.Policy.Effect, res.Policy.Code, r.header.Get("Retry-After"))
		}
		got := []string{}
		for _, rc := range res.Receipts {
			got = append(got, rc.Action)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("held receipts = %v, want %v", got, want)
		}
		return res
	}
	waits := func(r *resp, ref string) {
		t.Helper()
		if r.header.Get("Retry-After") == "" {
			t.Errorf("no Retry-After: %v", r.header)
		}
		if got := r.fails(503, "judge_pending"); got.Details.Ref != ref {
			t.Errorf("judge_pending names %q, want %s", got.Details.Ref, ref)
		}
	}
	const narrowFly = "Previews deploy to Fly.io; production stays on Railway."
	const narrowRailway = "Production deploys to Railway; previews run elsewhere."

	t.Run("the proposal's words contradict the third decision", func(t *testing.T) {
		railway, fly, previews := conflict("a")
		res := held(resolve(fly, 1, map[string]any{"other": railway.Ref, "statement": narrowFly}), "edited")
		if res.Memory.ID != fly.ID || res.Memory.Version != 2 || res.Memory.Statement != narrowFly || res.Memory.State != "conflict" {
			t.Fatalf("saved = %+v", res.Memory.memory)
		}
		waits(resolve(fly, 2, map[string]any{"other": railway.Ref, "statement": narrowFly}), fly.Ref)
		// The judge (settling, beside Railway) finds the previews decision.
		e.judgeAll(flagging)
		if got := resolve(fly, 2, map[string]any{"other": railway.Ref, "statement": narrowFly}).fails(409, "in_conflict"); got.Details.Ref != previews.Ref {
			t.Errorf("in_conflict names %q, want %s", got.Details.Ref, previews.Ref)
		}
		// The conflict with Railway is still open.
		var cf struct {
			FlaggedRef  string `json:"flagged_ref"`
			DecisionRef string `json:"decision_ref"`
		}
		e.do(call{method: "GET", path: "/v2/memories/" + fly.ID.String() + "/conflict?with=" + railway.Ref, token: owner}).ok(200, &cf)
		if cf.FlaggedRef != fly.Ref || cf.DecisionRef != railway.Ref {
			t.Errorf("conflict = %+v", cf)
		}
	})
	t.Run("the kept side's words wait as a draft, then apply", func(t *testing.T) {
		railway, fly, _ := conflict("b")
		res := held(resolve(fly, 1, map[string]any{"other": railway.Ref, "other_statement": narrowRailway}), "drafted")
		if len(res.Memories) != 2 || res.Memories[1].ID != railway.ID || res.Memories[1].Version != 1 ||
			res.Memories[1].Statement != railway.Statement {
			t.Fatalf("memories = %+v", res.Memories)
		}
		waits(resolve(fly, 1, map[string]any{"other": railway.Ref, "other_statement": narrowRailway}), railway.Ref)
		e.judgeAll(clearing)
		var done changes
		resolve(fly, 1, map[string]any{"other": railway.Ref, "other_statement": narrowRailway}).ok(200, &done)
		if done.Outcome != "applied" || done.Memory.Lifecycle != "kept" {
			t.Fatalf("applied = %+v", done)
		}
		for _, m := range done.Memories {
			if m.ID == railway.ID && (m.Version != 2 || m.Statement != narrowRailway) {
				t.Errorf("railway = v%d %q", m.Version, m.Statement)
			}
		}
	})
	t.Run("words that touch no other decision apply at once", func(t *testing.T) {
		railway, fly, _ := conflict("c")
		var done changes
		resolve(fly, 1, map[string]any{"other": railway.Ref, "statement": "Production runs on Fly.io in iad and ams.",
			"other_statement": "Staging runs on Railway."}).ok(200, &done)
		if done.Outcome != "applied" || done.Memory.Lifecycle != "kept" || len(done.Receipts) != 4 {
			t.Errorf("applied = %+v", done)
		}
	})
}
