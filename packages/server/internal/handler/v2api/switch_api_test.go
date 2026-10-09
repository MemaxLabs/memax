package v2api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

type switchOut struct {
	Space      spaceOut `json:"space"`
	State      string   `json:"state"`
	Step       string   `json:"step"`
	Background bool     `json:"background"`
	ImportID   *string  `json:"import_id"`
	Preview    struct {
		Kinds []string `json:"kinds"`
		Notes struct {
			Total, Candidates, Fold, Kept, Seeds int
		} `json:"notes"`
		Members []struct {
			V1Role    string `json:"v1_role"`
			Role      string `json:"role"`
			CanForget bool   `json:"can_forget"`
		} `json:"members"`
		Agents []struct {
			Autonomy  string `json:"autonomy"`
			Connected bool   `json:"connected"`
		} `json:"agents"`
		DreamRuns int    `json:"dream_runs"`
		Plan      string `json:"plan"`
		Empty     bool   `json:"empty"`
	} `json:"preview"`
	Progress struct {
		Notes     int `json:"notes"`
		Proposed  int `json:"proposed"`
		Connected int `json:"connected"`
		Notified  int `json:"notified"`
	} `json:"progress"`
}

type noteOut struct {
	ID          uuid.UUID `json:"id"`
	Ref         string    `json:"ref"`
	Disposition string    `json:"disposition"`
	Excerpt     string    `json:"excerpt"`
}

// v1Note writes a V1 memory with its chunk, as V1's push and ingest do.
func (e *env) v1Note(hub space, owner uuid.UUID, content string, set ...string) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	e.exec(`INSERT INTO memories (id, hub_id, owner_id, title, content, content_type, state) VALUES ($1, $2, $3, $4, $4, 'text', 'active')`,
		id, hub.id, owner, content)
	e.exec(`INSERT INTO chunks (memory_id, content, chunk_index, search_text, language, search_config) VALUES ($1, $2, 0, $2, 'en', 'simple')`,
		id, content)
	for _, s := range set {
		e.exec(`UPDATE memories SET `+s+` WHERE id = $1`, id)
	}
	return id
}

// A V1 team hub switches to V2 over the API, as a project space: the
// preview, the refusals, the background run, the V1 import, notes, Forget
// of a note, V1's Dream runs, the agents' notices and switching back.
func TestSwitchToV2OverTheAPI(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	zz, co, vi := e.user("zz"), e.user("co"), e.user("vi")
	team := e.space(zz, policy.SpaceTeam, "acme-web")
	e.join(team, co, "contributor")
	e.join(team, vi, "viewer")
	e.exec(`UPDATE hubs SET plan = 'pro' WHERE id = $1`, team.id)
	mine := e.v1Note(team, zz, "We use pnpm workspaces for every package.")
	e.v1Note(team, co, "The staging database lives on Neon.")
	e.v1Note(team, zz, "Claude saw cache misses on lockfile changes.", "created_by_type = 'agent'", "created_by_slug = 'claude-code'")
	e.v1Note(team, zz, "Welcome to Memax.", "source_kind = 'onboarding-seed'")
	e.exec(`INSERT INTO dream_runs (owner_id, hub_id, status, duplicates_merged) VALUES ($1, $2, 'completed', 3)`, zz, team.id)
	key, _ := e.apiKey(zz, keyOpts{agent: "claude-code", unconnected: true})
	tok := e.session(zz)
	base := "/v2/spaces/" + team.slug

	// The dry run.
	var st switchOut
	e.do(call{method: "GET", path: base + "/switch", token: tok}).ok(http.StatusOK, &st)
	n := st.Preview.Notes
	if st.State != "v1" || n.Total != 3 || n.Candidates != 2 || n.Fold != 1 || n.Seeds != 1 || st.Preview.Empty {
		t.Errorf("preview = %+v", st)
	}
	if strings.Join(st.Preview.Kinds, ",") != "team,project" || st.Preview.Plan != "pro" || st.Preview.DreamRuns != 1 {
		t.Errorf("preview kinds %v plan %q runs %d", st.Preview.Kinds, st.Preview.Plan, st.Preview.DreamRuns)
	}
	if len(st.Preview.Agents) != 1 || st.Preview.Agents[0].Autonomy != "propose" {
		t.Errorf("agents = %+v", st.Preview.Agents)
	}
	// Any member reads it; only the owner switches; never a key.
	e.do(call{method: "GET", path: base + "/switch", token: e.session(vi)}).ok(http.StatusOK, nil)
	if err := e.do(call{method: "POST", path: base + ":switch", token: e.session(co)}).fails(http.StatusForbidden, "refused"); err.Details.Policy.Code != policy.CodeSwitchByOwner {
		t.Errorf("a contributor switches: %+v", err.Details.Policy)
	}
	if err := e.do(call{method: "POST", path: base + ":switch", token: key}).fails(http.StatusForbidden, "refused"); err.Details.Policy.Code != policy.CodeSpaceByPerson {
		t.Errorf("a key switches: %+v", err.Details.Policy)
	}
	e.do(call{method: "POST", path: base + ":switch", token: tok, body: map[string]any{"to": "v3"}, invalid: true}).
		fails(http.StatusBadRequest, "invalid_request")

	// It has candidates: it switches in the background (202), as the
	// space_switch job; here the job is run by hand.
	e.do(call{method: "POST", path: base + ":switch", token: tok,
		body: map[string]any{"kind": "project", "repository": "acme/web"}}).ok(http.StatusAccepted, &st)
	if !st.Background || st.State != "running" || st.Space.Kind != "project" || st.Space.Repository != "acme/web" {
		t.Fatalf("started = %+v", st)
	}
	if n := e.count(`SELECT count(*) FROM river_job WHERE kind = 'space_switch'`); n != 1 {
		t.Fatalf("%d space_switch jobs", n)
	}
	// Asking again while it runs changes nothing.
	e.do(call{method: "POST", path: base + ":switch", token: tok}).ok(http.StatusAccepted, &st)
	if _, err := e.ledger.RunSwitch(ctx, team.id); err != nil {
		t.Fatal(err)
	}
	e.do(call{method: "GET", path: base + "/switch", token: tok}).ok(http.StatusOK, &st)
	if st.State != "switched" || st.Space.V2EnabledAt == nil || st.ImportID == nil || st.Progress.Proposed != 2 ||
		st.Progress.Connected != 1 || st.Progress.Notified != 1 {
		t.Fatalf("switched = %+v", st)
	}
	// The V1 import, for Review's "From V1".
	var imp struct {
		Import struct {
			Origin string `json:"origin"`
		} `json:"import"`
		Memories []struct {
			Bulk bool `json:"bulk"`
		} `json:"memories"`
	}
	e.do(call{method: "GET", path: base + "/imports/" + *st.ImportID, token: tok}).ok(http.StatusOK, &imp)
	if imp.Import.Origin != "v1" || len(imp.Memories) != 2 {
		t.Errorf("import = %+v", imp)
	}
	// The agent is told, once, that the space moved and what it may do.
	var notices struct {
		Notices []struct {
			Kind     string `json:"kind"`
			Autonomy string `json:"autonomy"`
		} `json:"notices"`
	}
	e.do(call{method: "GET", path: "/v2/notices", token: key}).ok(http.StatusOK, &notices)
	if len(notices.Notices) != 1 || notices.Notices[0].Kind != "switched" || notices.Notices[0].Autonomy != "propose" {
		t.Errorf("notices = %+v", notices)
	}

	// Notes: the owner searches and reads them.
	var notes page[noteOut]
	e.do(call{method: "GET", path: base + "/notes?q=pnpm", token: tok}).ok(http.StatusOK, &notes)
	if len(notes.Items) != 1 || notes.Items[0].ID != mine || !strings.HasPrefix(notes.Items[0].Ref, "N-") {
		t.Fatalf("notes = %+v", notes.Items)
	}
	ref := notes.Items[0].Ref
	var got noteOut
	e.do(call{method: "GET", path: base + "/notes/" + ref, token: tok}).ok(http.StatusOK, &got)
	if got.Disposition != "candidate" {
		t.Errorf("note = %+v", got)
	}
	// A contributor sees their own notes only.
	e.do(call{method: "GET", path: base + "/notes?q=pnpm", token: e.session(co)}).ok(http.StatusOK, &notes)
	if len(notes.Items) != 0 {
		t.Errorf("a contributor sees the owner's notes: %+v", notes.Items)
	}
	e.do(call{method: "GET", path: base + "/notes/" + ref, token: e.session(co)}).fails(http.StatusNotFound, "not_found")

	// Forget a note: what goes with it first, then the note.
	var pv struct {
		Carries []struct {
			Ref string `json:"ref"`
		} `json:"carries"`
		Allowed bool `json:"allowed"`
	}
	e.do(call{method: "GET", path: base + "/notes/" + ref + "/forget-preview", token: tok}).ok(http.StatusOK, &pv)
	if !pv.Allowed || len(pv.Carries) != 1 {
		t.Fatalf("preview = %+v", pv)
	}
	e.do(call{method: "POST", path: base + "/notes/" + ref + ":forget", token: tok}).fails(http.StatusConflict, "forget_carries")
	var forgot struct {
		Tombstone struct {
			Kind string `json:"kind"`
			Ref  string `json:"ref"`
		} `json:"tombstone"`
		Memories []struct {
			Lifecycle string `json:"lifecycle"`
		} `json:"memories"`
	}
	e.do(call{method: "POST", path: base + "/notes/" + ref + ":forget", token: tok,
		body: map[string]any{"carries": []string{pv.Carries[0].Ref}}}).ok(http.StatusOK, &forgot)
	if forgot.Tombstone.Kind != "note" || forgot.Tombstone.Ref != ref || len(forgot.Memories) != 1 || forgot.Memories[0].Lifecycle != "forgotten" {
		t.Errorf("forgot = %+v", forgot)
	}
	if e.count(`SELECT count(*) FROM memories WHERE id = $1`, mine) != 0 {
		t.Error("the note's V1 row is still there")
	}
	e.do(call{method: "GET", path: base + "/notes/" + ref, token: tok}).fails(http.StatusNotFound, "not_found")

	// V1's Dream runs, read-only.
	var runs page[struct {
		Merged int `json:"merged"`
	}]
	e.do(call{method: "GET", path: base + "/v1-dream-runs", token: tok}).ok(http.StatusOK, &runs)
	if len(runs.Items) != 1 || runs.Items[0].Merged != 3 {
		t.Errorf("runs = %+v", runs.Items)
	}

	// Back to V1.
	var back switchOut
	e.do(call{method: "POST", path: base + ":switch", token: tok, body: map[string]any{"to": "v1"}}).ok(http.StatusOK, &back)
	if back.State != "off" || back.Space.V2EnabledAt != nil {
		t.Errorf("back = %+v", back)
	}
	// A V1 team hub with a V2 record can't become a project space any
	// more (this one already is one); a personal space can't either.
	personal := e.space(zz, policy.SpacePersonal, "zz-personal")
	e.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + ":switch", token: tok,
		body: map[string]any{"kind": "project"}}).fails(http.StatusConflict, "space_kind")
}

// V1 named every personal space after its owner: the slug is the owner's
// user id, a uuid that isn't the space's id. /v2 finds the space by that
// slug, as the web's links name it, and such a key never reaches a space
// outside the caller's own.
func TestAPersonalSpaceNamedForItsOwner(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	zz, ot := e.user("zz"), e.user("ot")
	mine := e.space(zz, policy.SpacePersonal, "memory-hub")
	e.exec(`UPDATE hubs SET slug = $1 WHERE id = $2`, zz.String(), mine.id)
	theirs := e.space(ot, policy.SpacePersonal, "memory-hub")
	e.exec(`UPDATE hubs SET slug = $1 WHERE id = $2`, ot.String(), theirs.id)
	e.v1Note(mine, zz, "We use pnpm workspaces for every package.")
	tok := e.session(zz)

	var st switchOut
	for _, key := range []string{zz.String(), mine.id.String()} {
		e.do(call{method: "GET", path: "/v2/spaces/" + key + "/switch", token: tok}).ok(http.StatusOK, &st)
		if st.State != "v1" || st.Space.ID != mine.id || st.Space.Slug != zz.String() {
			t.Errorf("by %s: %+v", key, st)
		}
	}
	// Another person's personal space isn't found by its slug or its id.
	for _, key := range []string{ot.String(), theirs.id.String()} {
		e.do(call{method: "GET", path: "/v2/spaces/" + key + "/switch", token: tok}).fails(http.StatusNotFound, "not_found")
	}

	// It switches by that slug, and its record reads by it.
	base := "/v2/spaces/" + zz.String()
	e.do(call{method: "POST", path: base + ":switch", token: tok}).ok(http.StatusAccepted, &st)
	if _, err := e.ledger.RunSwitch(ctx, mine.id); err != nil {
		t.Fatal(err)
	}
	e.do(call{method: "GET", path: base + "/switch", token: tok}).ok(http.StatusOK, &st)
	if st.State != "switched" || st.Progress.Proposed != 1 {
		t.Fatalf("switched = %+v", st)
	}
	e.do(call{method: "GET", path: base + "/review", token: tok}).ok(http.StatusOK, nil)
	e.do(call{method: "GET", path: base + "/memories", token: tok}).ok(http.StatusOK, nil)
}
