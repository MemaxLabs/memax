package v2api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ask"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
	"github.com/MemaxLabs/memax/packages/server/internal/v2plans"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// planEnv is an env whose ledger decides under the migrated plan rows
// (changed first by tweak, SQL on public.plans, so a test meets a limit in
// a few requests), enforced or not, as V2_PLAN_LIMITS says.
func planEnv(t *testing.T, enforce bool, tweak string, more func(*env) []v2api.Option) *env {
	t.Helper()
	db := testdb.Open(t, testdb.Options{})
	if tweak != "" {
		if _, err := db.Pool.Exec(context.Background(), tweak); err != nil {
			t.Fatal(err)
		}
	}
	r := plans.NewRegistry(db.Store)
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if more == nil {
		more = func(*env) []v2api.Option { return nil }
	}
	return newEnvOn(t, db, more, ledger.WithPlans(v2plans.New(r, enforce)))
}

// planRefusal is a refusal's policy decision with its plan limit.
type planRefusal struct {
	Code  string `json:"code"`
	Limit *struct {
		Plan    string `json:"plan"`
		Allowed int    `json:"allowed"`
		Used    int    `json:"used"`
		Upgrade string `json:"upgrade"`
	} `json:"limit"`
}

func refusalOf(t *testing.T, r *resp) planRefusal {
	t.Helper()
	r.fails(http.StatusForbidden, "refused")
	var body struct {
		Error struct {
			Details struct {
				Policy planRefusal `json:"policy"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	return body.Error.Details.Policy
}

// wantLimit checks a refusal's code and limit.
func wantLimit(t *testing.T, got planRefusal, code, plan string, allowed, used int, upgrade string) {
	t.Helper()
	if got.Code != code || got.Limit == nil || got.Limit.Plan != plan || got.Limit.Allowed != allowed ||
		got.Limit.Used != used || got.Limit.Upgrade != upgrade {
		t.Errorf("refusal %+v (limit %+v), want %s on %s %d/%d → %q", got, got.Limit, code, plan, used, allowed, upgrade)
	}
}

// TestPlanLimitsOverTheAPI: each plan limit /v2 can meet answers 403 with
// its code and limit when V2_PLAN_LIMITS enforces them, and the same
// request goes through when it doesn't (the alpha, and the default).
func TestPlanLimitsOverTheAPI(t *testing.T) {
	t.Parallel()
	for _, enforce := range []bool{true, false} {
		name := map[bool]string{true: "enforced", false: "not enforced"}[enforce]

		t.Run(name+": space_limit", func(t *testing.T) {
			t.Parallel()
			e := planEnv(t, enforce, "", nil)
			zz := e.user("zz")
			tok := e.session(zz)
			e.do(call{method: "POST", path: "/v2/spaces", token: tok, body: map[string]any{"name": "Acme web"}}).ok(http.StatusCreated, nil)
			r := e.do(call{method: "POST", path: "/v2/spaces", token: tok, body: map[string]any{"name": "Acme api"}})
			if !enforce {
				r.ok(http.StatusCreated, nil)
				return
			}
			wantLimit(t, refusalOf(t, r), policy.CodeSpaceLimit, "free", 1, 1, "pro")
			// On Pro, as many as they like.
			e.exec(`UPDATE users SET personal_plan_id = 'personal_pro' WHERE id = $1`, zz)
			e.do(call{method: "POST", path: "/v2/spaces", token: tok, body: map[string]any{"name": "Acme api"}}).ok(http.StatusCreated, nil)
		})

		t.Run(name+": agent_kind_limit", func(t *testing.T) {
			t.Parallel()
			// Free connects no kind of agent here, so the first one meets it.
			e := planEnv(t, enforce, `UPDATE plans SET agent_kind_limit = 0 WHERE id = 'personal_free'`, nil)
			zz := e.user("zz")
			sp := e.space(zz, policy.SpaceProject, "acme-web")
			r := e.do(call{method: "POST", path: "/v2/cloud-agents", token: e.session(zz),
				body: map[string]any{"agent": "copilot", "space": sp.slug}})
			if !enforce {
				r.ok(http.StatusCreated, nil)
				return
			}
			wantLimit(t, refusalOf(t, r), policy.CodeAgentKindLimit, "free", 0, 0, "pro")
		})

		t.Run(name+": seat_needed", func(t *testing.T) {
			t.Parallel()
			e := planEnv(t, enforce, "", nil)
			owner, member := e.user("zz"), e.user("jy")
			e.exec(`UPDATE users SET personal_plan_id = 'personal_pro' WHERE id = $1`, owner)
			sp := e.space(owner, policy.SpaceProject, "acme-web")
			e.join(sp, member, "contributor")
			// The member's own Remember waits for the owner.
			var res result
			e.do(call{method: "POST", path: memoriesPath(sp), token: e.session(member), body: remember("Releases are cut on Tuesdays.", "conventions")}).
				ok(http.StatusCreated, &res)
			if enforce && (res.Outcome != "proposed" || res.Policy.Code != policy.CodeSeatNeeded) {
				t.Errorf("remember: %s %s", res.Outcome, res.Policy.Code)
			}
			if !enforce && res.Outcome != "applied" {
				t.Errorf("remember during the alpha: %s %s", res.Outcome, res.Policy.Code)
			}
			// And they don't keep the owner's agent's proposal.
			key, _ := e.apiKey(owner, keyOpts{agent: "codex"})
			var p result
			e.do(call{method: "POST", path: memoriesPath(sp), token: key, body: remember("Deploys go through staging first.", "conventions")}).
				ok(http.StatusCreated, &p)
			r := e.do(call{method: "POST", path: memoryPath(p.Memory, ":keep"), token: e.session(member), header: map[string]string{"If-Match": `"1"`}})
			if !enforce {
				r.ok(http.StatusOK, nil)
				return
			}
			got := refusalOf(t, r)
			if got.Code != policy.CodeSeatNeeded || got.Limit == nil || got.Limit.Plan != "pro" || got.Limit.Upgrade != "team" {
				t.Errorf("keep: %+v %+v", got, got.Limit)
			}
			// The owner keeps it.
			e.do(call{method: "POST", path: memoryPath(p.Memory, ":keep"), token: e.session(owner), header: map[string]string{"If-Match": `"1"`}}).
				ok(http.StatusOK, nil)
		})

		t.Run(name+": ask_limit", func(t *testing.T) {
			t.Parallel()
			e := planEnv(t, enforce, `UPDATE plans SET v2_ask_limit = 1 WHERE id = 'personal_free'`, func(e *env) []v2api.Option {
				return []v2api.Option{v2api.WithAsk(ask.New(e.ledger, v2recall.New(e.ledger), newFake(citesAll),
					ask.Config{Model: "test/answer-tier", Log: quiet}))}
			})
			a := askEnv{env: e}
			zz := e.user("zz")
			sp := e.space(zz, policy.SpaceProject, "memax-v2")
			tok := e.session(zz)
			e.remember(tok, sp, "Background jobs run on River.")
			a.answer(tok, sp, "What runs background jobs?")
			if !enforce {
				a.answer(tok, sp, "Which queue runs background jobs?")
				return
			}
			wantLimit(t, refusalOf(t, a.ask(tok, sp, "Which queue runs background jobs?")), policy.CodeAskLimit, "free", 1, 1, "pro")
		})

		t.Run(name+": dream_cadence", func(t *testing.T) {
			t.Parallel()
			e := planEnv(t, enforce, "", func(e *env) []v2api.Option {
				jobs, err := queue.InsertClient(e.pool)
				if err != nil {
					t.Fatal(err)
				}
				return []v2api.Option{v2api.WithDream(v2dream.New(e.ledger, nil, v2dream.Config{Log: quiet}), jobs)}
			})
			zz := e.user("zz")
			sp := e.space(zz, policy.SpaceProject, "memax-v2")
			tok := e.session(zz)
			past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
			e.do(call{method: "POST", path: memoriesPath(sp), token: tok,
				body: map[string]any{"statement": "Ask answers with the Haiku tier.", "section": "conventions", "stale_after": past}}).
				ok(http.StatusCreated, nil)
			// Dream ran now once this week (the worker's run of a run-now).
			eng := v2dream.New(e.ledger, nil, v2dream.Config{Log: quiet})
			out, err := eng.Run(context.Background(), ledger.DreamSpaceArgs{SpaceID: sp.id, Slot: time.Now().Truncate(time.Second),
				Trigger: ledger.DreamManual, RequestedBy: zz})
			if err != nil || !out.Ran {
				t.Fatalf("run now: %+v %v", out, err)
			}
			r := e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/dream:run", token: tok})
			if !enforce {
				// DREAM_PLAN's cap (Pro's, three a day) stands as before.
				r.ok(http.StatusAccepted, nil)
				return
			}
			wantLimit(t, refusalOf(t, r), policy.CodeDreamCadence, "free", 1, 1, "pro")
		})

		t.Run(name+": Activity goes back as far as the plan", func(t *testing.T) {
			t.Parallel()
			e := planEnv(t, enforce, "", nil)
			zz := e.user("zz")
			sp := e.space(zz, policy.SpaceProject, "memax-v2")
			m := e.remember(e.session(zz), sp, "Background jobs run on River.").Memory
			var tenant uuid.UUID
			if err := e.pool.QueryRow(context.Background(), `SELECT tenant_id FROM hubs WHERE id = $1`, sp.id).Scan(&tenant); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			var events []ledger.ReadEvent
			for _, at := range []time.Time{now.AddDate(0, 0, -45), now.Add(-time.Hour)} {
				events = append(events, ledger.ReadEvent{SpaceID: sp.id, TenantID: tenant, Reader: ledger.ReaderAgent, ConnectionID: uuid.New(),
					PersonID: zz, Agent: "codex", Kind: ledger.ReadRecall, Via: policy.ViaMCP, Memories: []uuid.UUID{m.ID}, At: at})
			}
			if _, err := e.ledger.RecordReads(context.Background(), events); err != nil {
				t.Fatal(err)
			}
			var page struct {
				Items         []json.RawMessage `json:"items"`
				RetentionDays int               `json:"retention_days"`
			}
			e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/reads", token: e.session(zz)}).ok(http.StatusOK, &page)
			want, days := 2, 0
			if enforce {
				want, days = 1, 30
			}
			if len(page.Items) != want || page.RetentionDays != days {
				t.Errorf("%d reads over %d days, want %d over %d", len(page.Items), page.RetentionDays, want, days)
			}
		})
	}
}
