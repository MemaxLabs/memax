package v2api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream/dreamtest"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

type dreamEdition struct {
	ID        uuid.UUID      `json:"id"`
	Ref       string         `json:"ref"`
	N         int            `json:"n"`
	NotesRead int            `json:"notes_read"`
	NoteRefs  []string       `json:"note_refs"`
	FactRefs  []string       `json:"fact_refs"`
	Counts    map[string]int `json:"counts"`
	NeedsYou  int            `json:"needs_you"`
	Undone    int            `json:"undone"`
	Actions   []dreamAction  `json:"actions"`
}

type dreamAction struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"`
	Memory   *memory   `json:"memory"`
	NoteRefs []string  `json:"note_refs"`
	Undoable bool      `json:"undoable"`
	Undone   *struct {
		ReceiptID uuid.UUID `json:"receipt_id"`
	} `json:"undone"`
}

// dreamEnv is the API with run-now on, and a space Dream has run on once.
type dreamEnv struct {
	*env
	owner      uuid.UUID
	token      string
	sp         space
	first      dreamEdition
	notedFacts []string
}

func newDreamEnv(t *testing.T) *dreamEnv {
	t.Helper()
	e := newEnvWith(t, func(e *env) []v2api.Option {
		// The API's own insert-only client (its workers bundle), as
		// serverapp wires it: a kind it doesn't register fails run now.
		jobs, err := queue.InsertClient(e.pool)
		if err != nil {
			t.Fatal(err)
		}
		return []v2api.Option{v2api.WithDream(v2dream.New(e.ledger, nil, v2dream.Config{Log: quiet}), jobs)}
	})
	d := &dreamEnv{env: e, owner: e.user("zz")}
	d.token = e.session(d.owner)
	d.sp = e.space(d.owner, policy.SpaceProject, "memax-v2")
	e.exec(`UPDATE hubs SET v2_enabled_at = now() WHERE id = $1`, d.sp.id)
	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	d.remember(d.token, d.sp, "Background jobs run on River, not Temporal.")
	d.remember(d.token, d.sp, "Deploys go to Railway.")
	d.remember(d.token, d.sp, "Deploys go to Fly.io in iad and ams.")
	var stale result
	e.do(call{method: "POST", path: "/v2/spaces/" + d.sp.slug + "/memories", token: d.token,
		body: map[string]any{"statement": "Ask memax answers with the Haiku tier.", "section": "conventions", "stale_after": past}}).
		ok(http.StatusCreated, &stale)
	for _, body := range []string{"Again today: background jobs run on River.", "Decided: the CLI prints receipts in the same order as the web app."} {
		e.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content, created_by_type, created_by_slug)
		        VALUES ($1, $2, $3, '', $4, 'agent', 'codex')`, uuid.New(), d.owner, d.sp.id, body)
	}
	d.dream(time.Now(), &dreamtest.Oracle{
		Folds: []dreamtest.FoldRule{{Note: "background jobs run on River", Memory: "Background jobs run on River"}},
		Facts: []dreamtest.FactRule{{Notes: []string{"prints receipts"}, Statement: "The CLI prints receipts in the same order as the web app.", Section: "conventions"}},
		Pairs: []dreamtest.PairRule{{A: "Fly.io in iad", B: "Deploys go to Railway", Relation: "updates", Confidence: 0.92}},
	})
	d.do(call{method: "GET", path: "/v2/spaces/" + d.sp.slug + "/dream/editions/latest", token: d.token}).ok(http.StatusOK, &d.first)
	return d
}

// dream runs Dream on the space as the worker would, with clock now.
func (d *dreamEnv) dream(now time.Time, model v2dream.Model) {
	d.t.Helper()
	l := ledger.New(d.pool, ledger.WithLogger(quiet), ledger.WithClock(func() time.Time { return now }))
	cfg := v2dream.Config{Log: quiet}
	if model != nil {
		cfg.Primary.Model, cfg.Strong.Model = "fake/primary", "fake/strong"
	}
	eng := v2dream.New(l, model, cfg, v2dream.WithSearcher(v2recall.New(l)), v2dream.WithClock(func() time.Time { return now }))
	out, err := eng.Run(context.Background(), ledger.DreamSpaceArgs{SpaceID: d.sp.id, Slot: now.Truncate(time.Second), Trigger: ledger.DreamScheduled})
	if err != nil || !out.Ran {
		d.t.Fatalf("dream: %+v %v", out, err)
	}
}

func (d *dreamEnv) editions() string { return "/v2/spaces/" + d.sp.slug + "/dream/editions" }

func TestDreamEditionsOverTheAPI(t *testing.T) {
	t.Parallel()
	d := newDreamEnv(t)
	e := d.first
	if e.Counts["fold"] != 1 || e.Counts["propose"] != 1 || e.Counts["conflict"] != 1 || e.Counts["stale"] != 1 || e.NotesRead != 2 {
		t.Fatalf("the first edition: %+v", e)
	}
	// The list, with the person's zone recorded from their app.
	var list struct {
		Items []dreamEdition `json:"items"`
	}
	d.do(call{method: "GET", path: d.editions(), token: d.token, header: map[string]string{"X-Timezone": "America/Vancouver"}}).
		ok(http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].Ref != e.Ref || list.Items[0].Actions != nil {
		t.Errorf("list %+v", list)
	}
	if n := d.count(`SELECT count(*) FROM v2.dream_settings WHERE person_id = $1 AND time_zone = 'America/Vancouver' AND time_zone_source = 'observed'`, d.owner); n != 1 {
		t.Error("the person's zone wasn't recorded")
	}
	// By ref, by number, by id.
	for _, key := range []string{e.Ref, fmt.Sprint(e.N), e.ID.String()} {
		var got dreamEdition
		d.do(call{method: "GET", path: d.editions() + "/" + key, token: d.token}).ok(http.StatusOK, &got)
		if got.ID != e.ID || len(got.Actions) != 4 {
			t.Errorf("%s: %+v", key, got)
		}
	}
	var actions struct {
		Items []dreamAction `json:"items"`
	}
	d.do(call{method: "GET", path: d.editions() + "/" + e.Ref + "/actions?kind=stale", token: d.token}).ok(http.StatusOK, &actions)
	if len(actions.Items) != 1 || actions.Items[0].Kind != "stale" || !actions.Items[0].Undoable {
		t.Fatalf("stale actions %+v", actions)
	}
	// Undo one: the flag goes, with an undid receipt citing the action.
	var undone struct {
		Action   dreamAction `json:"action"`
		Memories []memory    `json:"memories"`
		Receipts []receipt   `json:"receipts"`
	}
	d.do(call{method: "POST", path: "/v2/dream/actions/" + actions.Items[0].ID.String() + ":undo", token: d.token}).ok(http.StatusOK, &undone)
	if undone.Action.Undone == nil || len(undone.Memories) != 1 || undone.Memories[0].State != "kept" ||
		undone.Receipts[0].Action != "undid" || undone.Receipts[0].ActorKind != "person" {
		t.Errorf("undo %+v", undone)
	}
	d.do(call{method: "POST", path: "/v2/dream/actions/" + actions.Items[0].ID.String() + ":undo", token: d.token}).fails(http.StatusConflict, "undo_refused")
	// Undo every action of a kind.
	var bulk struct {
		Undone  []dreamAction `json:"undone"`
		Refused []struct {
			Reason string `json:"reason"`
		} `json:"refused"`
	}
	d.do(call{method: "POST", path: d.editions() + "/" + e.Ref + ":undo", token: d.token, body: map[string]any{"kind": "conflict"}}).
		ok(http.StatusOK, &bulk)
	if len(bulk.Undone) != 1 || len(bulk.Refused) != 0 {
		t.Errorf("bulk %+v", bulk)
	}
	// Another person's spaces see nothing of it.
	jy := d.user("jy")
	d.do(call{method: "GET", path: d.editions() + "/" + e.Ref, token: d.session(jy)}).fails(http.StatusNotFound, "not_found")
	d.do(call{method: "POST", path: "/v2/dream/actions/" + e.Actions[0].ID.String() + ":undo", token: d.session(jy)}).
		fails(http.StatusNotFound, "not_found")
	// A viewer reads it and can't undo.
	viewer := d.user("sam")
	d.join(d.sp, viewer, "viewer")
	d.do(call{method: "GET", path: d.editions() + "/latest", token: d.session(viewer)}).ok(http.StatusOK, nil)
	d.do(call{method: "POST", path: "/v2/dream/actions/" + e.Actions[0].ID.String() + ":undo", token: d.session(viewer)}).
		fails(http.StatusForbidden, "refused")
}

// Sixty days later, Dream fades what nobody read; a person restores one.
func TestDreamFadeAndRestoreOverTheAPI(t *testing.T) {
	t.Parallel()
	d := newDreamEnv(t)
	// Fading needs a run, and a run needs something new: a note will do.
	d.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content) VALUES ($1, $2, $3, '', 'Back from leave.')`,
		uuid.New(), d.owner, d.sp.id)
	d.dream(time.Now().Add(61*24*time.Hour), nil)
	var ed dreamEdition
	d.do(call{method: "GET", path: d.editions() + "/latest", token: d.token}).ok(http.StatusOK, &ed)
	if ed.Counts["fade"] == 0 {
		t.Fatalf("nothing faded: %+v", ed.Counts)
	}
	var faded []dreamAction
	for _, a := range ed.Actions {
		if a.Kind == "fade" {
			faded = append(faded, a)
		}
	}
	var res result
	d.do(call{method: "POST", path: "/v2/memories/" + faded[0].Memory.Ref + ":restore?space=" + d.sp.slug, token: d.token}).
		ok(http.StatusOK, &res)
	if res.Memory.State != "kept" || res.Receipts[0].Action != "restored" {
		t.Errorf("restore %+v", res)
	}
	d.do(call{method: "POST", path: "/v2/memories/" + faded[0].Memory.Ref + ":restore?space=" + d.sp.slug, token: d.token}).
		fails(http.StatusConflict, "invalid_transition")
	var bulk struct {
		Undone  []dreamAction `json:"undone"`
		Refused []struct {
			Reason string `json:"reason"`
		} `json:"refused"`
	}
	d.do(call{method: "POST", path: d.editions() + "/" + ed.Ref + ":undo", token: d.token, body: map[string]any{"kind": "fade"}}).
		ok(http.StatusOK, &bulk)
	if len(bulk.Undone) != len(faded)-1 || len(bulk.Refused) != 1 || bulk.Refused[0].Reason != ledger.UndoLaterChanges {
		t.Errorf("restore all: %d undone, refused %+v", len(bulk.Undone), bulk.Refused)
	}
}

func TestDreamRunNowAndSettings(t *testing.T) {
	t.Parallel()
	d := newDreamEnv(t)
	var run struct {
		Queued  bool   `json:"queued"`
		Trigger string `json:"trigger"`
	}
	d.do(call{method: "POST", path: "/v2/spaces/" + d.sp.slug + "/dream:run", token: d.token}).ok(http.StatusAccepted, &run)
	if !run.Queued || run.Trigger != "manual" {
		t.Errorf("run %+v", run)
	}
	if n := d.count(`SELECT count(*) FROM river_job WHERE kind = 'dream_space' AND args->>'trigger' = 'manual'`); n != 1 {
		t.Errorf("%d runs queued", n)
	}
	d.do(call{method: "POST", path: "/v2/spaces/" + d.sp.slug + "/dream:run", token: d.token}).fails(http.StatusTooManyRequests, "rate_limited")
	member := d.user("jy")
	d.join(d.sp, member, "contributor")
	err := d.do(call{method: "POST", path: "/v2/spaces/" + d.sp.slug + "/dream:run", token: d.session(member)}).fails(http.StatusForbidden, "refused")
	if err.Details.Policy.Code != policy.CodeDreamRunByOwner {
		t.Errorf("a member ran Dream: %+v", err)
	}

	var s struct {
		TimeZone     string `json:"time_zone"`
		Source       string `json:"time_zone_source"`
		MorningEmail bool   `json:"morning_email"`
	}
	d.do(call{method: "GET", path: "/v2/dream/settings", token: d.token}).ok(http.StatusOK, &s)
	if s.TimeZone != "UTC" || s.Source != "default" || !s.MorningEmail {
		t.Errorf("defaults %+v", s)
	}
	d.do(call{method: "PATCH", path: "/v2/dream/settings", token: d.token, body: map[string]any{"time_zone": "Asia/Shanghai", "morning_email": false}}).
		ok(http.StatusOK, &s)
	if s.TimeZone != "Asia/Shanghai" || s.Source != "set" || s.MorningEmail {
		t.Errorf("set %+v", s)
	}
	d.do(call{method: "PATCH", path: "/v2/dream/settings", token: d.token, body: map[string]any{"time_zone": "Pacific"}}).
		fails(http.StatusBadRequest, "invalid_request")
	// A zone the person set isn't overwritten by their app's clock.
	d.do(call{method: "GET", path: d.editions(), token: d.token, header: map[string]string{"X-Timezone": "Europe/Berlin"}}).ok(http.StatusOK, nil)
	d.do(call{method: "GET", path: "/v2/dream/settings", token: d.token}).ok(http.StatusOK, &s)
	if s.TimeZone != "Asia/Shanghai" {
		t.Errorf("the app's clock overwrote a set zone: %+v", s)
	}

	// One-click unsubscribe needs no sign-in, and says nothing about the token.
	d.do(call{method: "PATCH", path: "/v2/dream/settings", token: d.token, body: map[string]any{"morning_email": true}}).ok(http.StatusOK, nil)
	var token string
	if err := d.pool.QueryRow(context.Background(), `SELECT unsubscribe_token FROM v2.dream_settings WHERE person_id = $1`, d.owner).Scan(&token); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Unsubscribed bool `json:"unsubscribed"`
	}
	d.do(call{method: "POST", path: "/v2/dream/email:unsubscribe?token=" + token}).ok(http.StatusOK, &out)
	d.do(call{method: "POST", path: "/v2/dream/email:unsubscribe?token=" + token}).ok(http.StatusOK, &out)
	d.do(call{method: "POST", path: "/v2/dream/email:unsubscribe?token=nonsense"}).ok(http.StatusOK, &out)
	if n := d.count(`SELECT count(*) FROM v2.dream_settings WHERE person_id = $1 AND NOT morning_email`, d.owner); n != 1 {
		t.Error("the morning email is still on")
	}
}
