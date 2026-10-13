package v2api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// GET /v2/search (epic 3.3): the person's search across their spaces, on
// the same hybrid search as memax_search.

type searchHitWire struct {
	Memory   memory    `json:"memory"`
	Receipts []receipt `json:"receipts"`
}

type searchPageWire struct {
	Items      []searchHitWire `json:"items"`
	HasMore    bool            `json:"has_more"`
	NextCursor string          `json:"next_cursor"`
	Notes      []noteOut       `json:"notes"`
	Lexical    bool            `json:"lexical_only"`
}

// newSearchEnv serves /v2/search on the lexical searcher (or on vectors,
// when given), and records agents' reads at once.
func newSearchEnv(t *testing.T, vectors *mockembed.Words) (*env, *syncReads) {
	t.Helper()
	rec := &syncReads{t: t}
	e := newEnvWith(t, func(e *env) []v2api.Option {
		search := v2recall.New(e.ledger)
		if vectors != nil {
			search.WithVectors(v2recall.NewVectors(e.ledger, vectors, vectors, v2recall.VectorConfig{Model: embedModel, Log: quiet}))
		}
		return []v2api.Option{v2api.WithSearch(search), v2api.WithReads(rec)}
	})
	rec.l = e.ledger
	return e, rec
}

// search runs GET /v2/search with the query's parameters.
func (e *env) search(token string, params url.Values) searchPageWire {
	e.t.Helper()
	var out searchPageWire
	e.do(call{method: "GET", path: "/v2/search?" + params.Encode(), token: token}).ok(http.StatusOK, &out)
	return out
}

func searchRefs(p searchPageWire) []string {
	refs := make([]string, len(p.Items))
	for i, it := range p.Items {
		refs[i] = it.Memory.SpaceID.String()[:8] + "/" + it.Memory.Ref
	}
	slices.Sort(refs)
	return refs
}

func refAt(sp space, m memory) string { return sp.id.String()[:8] + "/" + m.Ref }

func sortedRefs(refs ...string) []string { slices.Sort(refs); return refs }

// A person finds what's kept and what waits in Review across every space
// of theirs on the V2 record, with who wrote each and who kept it; never a
// space of anyone else's (rule 13), never a space on V1, and never what
// was rejected, forgotten or superseded.
func TestSearchAcrossSpaces(t *testing.T) {
	t.Parallel()
	e, _ := newSearchEnv(t, nil)
	zz, jy := e.user("zz"), e.user("jy")
	personal := e.space(zz, policy.SpacePersonal, "zz")
	proj := e.space(zz, policy.SpaceProject, "memax-v2")
	team := e.space(jy, policy.SpaceTeam, "memax-team")
	e.join(team, zz, "contributor")
	theirs := e.space(jy, policy.SpaceProject, "jiahao")
	onV1 := e.space(zz, policy.SpaceProject, "acme-web")
	tok, jyTok := e.session(zz), e.session(jy)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})

	kept := e.remember(tok, proj, "Workers deploy from the main branch only.").Memory
	mine := e.remember(tok, personal, "I deploy on Tuesdays, never late in the day.").Memory
	inTeam := e.remember(jyTok, team, "No Friday deploys after 16:00.").Memory
	e.remember(jyTok, theirs, "Deploy secrets live in Doppler.")
	e.remember(tok, onV1, "Deploy acme-web to Vercel.")
	e.exec(`UPDATE hubs SET v2_enabled_at = NULL WHERE id = $1`, onV1.id) // switched back to V1
	proposal := e.remember(key, proj, "Deploy the v2 API to Fly.io in iad and ams.").Memory
	reviewed := e.remember(key, proj, "Deploy previews to Railway.").Memory
	e.do(call{method: "POST", path: memoryPath(reviewed, ":keep"), token: tok, header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, nil)
	rejected := e.remember(key, proj, "Deploy on Fridays whenever.").Memory
	e.do(call{method: "POST", path: memoryPath(rejected, ":reject"), token: tok, header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, nil)
	forgotten := e.remember(tok, proj, "Deploy keys rotate every month.").Memory
	e.do(call{method: "POST", path: memoryPath(forgotten, ":forget"), token: tok, header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, nil)
	rememberWith(e, tok, proj, map[string]any{"statement": "The deploy target is Heroku.", "section": "decisions",
		"kind": "decision", "decision": map[string]any{"status": "superseded"}})
	decision := rememberWith(e, tok, proj, map[string]any{"statement": "The deploy target is Fly.io.", "section": "decisions",
		"kind": "decision", "decision": map[string]any{"status": "in_force"}}).Memory

	q := url.Values{"q": {"deploy"}}
	got := e.search(tok, q)
	want := sortedRefs(refAt(proj, kept), refAt(personal, mine), refAt(team, inTeam), refAt(proj, proposal), refAt(proj, reviewed), refAt(proj, decision))
	if !slices.Equal(searchRefs(got), want) {
		t.Fatalf("found %v, want %v", searchRefs(got), want)
	}
	if !got.Lexical || got.HasMore || got.Notes != nil {
		t.Errorf("lexical_only %v has_more %v notes %v", got.Lexical, got.HasMore, got.Notes)
	}
	for _, it := range got.Items {
		switch it.Memory.ID {
		case reviewed.ID:
			// Codex wrote it and Ziyang kept it: both receipts, oldest first.
			if len(it.Receipts) != 2 || it.Receipts[0].Action != "proposed" || it.Receipts[0].Agent != "codex" ||
				it.Receipts[1].Action != "kept" || it.Receipts[1].ActorKind != "person" || it.Memory.State != "kept" {
				t.Errorf("the kept proposal: %+v %+v", it.Memory, it.Receipts)
			}
		case proposal.ID:
			if len(it.Receipts) != 1 || it.Receipts[0].Action != "proposed" || it.Memory.State != "proposed" {
				t.Errorf("the proposal: %+v %+v", it.Memory, it.Receipts)
			}
		default:
			if len(it.Receipts) != 1 || it.Receipts[0].ObjectID != it.Memory.ID {
				t.Errorf("%s: receipts %+v", it.Memory.Ref, it.Receipts)
			}
		}
		if it.Memory.Statement == "" || len(it.Memory.Sources) != 0 {
			t.Errorf("%s: %+v", it.Memory.Ref, it.Memory)
		}
	}

	// One space, or two, by slug or id; kind narrows to decisions.
	if got := e.search(tok, url.Values{"q": {"deploy"}, "space": {proj.slug}, "kind": {"decision"}}); !slices.Equal(searchRefs(got), []string{refAt(proj, decision)}) {
		t.Errorf("decisions in memax-v2: %v", searchRefs(got))
	}
	if got := e.search(tok, url.Values{"q": {"deploy"}, "space": {personal.id.String(), team.slug}}); !slices.Equal(searchRefs(got), sortedRefs(refAt(personal, mine), refAt(team, inTeam))) {
		t.Errorf("personal and team: %v", searchRefs(got))
	}
	// A space on V1 has nothing here, even named.
	if got := e.search(tok, url.Values{"q": {"deploy"}, "space": {onV1.slug}}); len(got.Items) != 0 {
		t.Errorf("a space on V1: %v", searchRefs(got))
	}
	// Another person's space is not found, as anywhere.
	e.do(call{method: "GET", path: "/v2/search?" + url.Values{"q": {"deploy"}, "space": {theirs.slug}}.Encode(), token: tok}).
		fails(http.StatusNotFound, "not_found")
	// Jiahao finds his own and the team's, never Ziyang's.
	jyGot := e.search(jyTok, q)
	for _, it := range jyGot.Items {
		if it.Memory.SpaceID == proj.id || it.Memory.SpaceID == personal.id {
			t.Errorf("Jiahao found %s in Ziyang's space", it.Memory.Ref)
		}
	}
	if len(jyGot.Items) != 2 {
		t.Errorf("Jiahao found %v", searchRefs(jyGot))
	}
	// Words nobody kept find nothing.
	if got := e.search(tok, url.Values{"q": {"kubernetes"}}); len(got.Items) != 0 {
		t.Errorf("kubernetes: %v", searchRefs(got))
	}
}

// An agent finds only what people kept, where its connection is connected,
// and its search is a read of what it found; a person's is no read.
func TestSearchByAnAgent(t *testing.T) {
	t.Parallel()
	e, _ := newSearchEnv(t, nil)
	zz := e.user("zz")
	proj := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	key, keyID := e.apiKey(zz, keyOpts{agent: "codex"})
	later := e.space(zz, policy.SpaceProject, "later") // the key isn't connected here
	kept := e.remember(tok, proj, "Workers deploy from the main branch only.").Memory
	e.remember(tok, later, "Deploy later from the release branch.")
	e.remember(key, proj, "Deploy the v2 API to Fly.io.") // a proposal

	reads := func() int { return e.count(`SELECT count(*) FROM v2.reads WHERE kind = 'search'`) }
	e.search(tok, url.Values{"q": {"deploy"}})
	if n := reads(); n != 0 {
		t.Fatalf("a person's search recorded %d reads", n)
	}
	got := e.search(key, url.Values{"q": {"deploy"}})
	if !slices.Equal(searchRefs(got), []string{refAt(proj, kept)}) {
		t.Fatalf("the agent found %v", searchRefs(got))
	}
	e.h.Wait()
	conn := e.connection(keyID)
	if n := e.count(`SELECT count(*) FROM v2.reads WHERE kind = 'search' AND connection_id = $1 AND space_id = $2`, conn, proj.id); n != 1 {
		t.Errorf("%d search reads by the agent", n)
	}
}

// Pages run the search again from where the last one ended; a cursor
// belongs to its search.
func TestSearchPages(t *testing.T) {
	t.Parallel()
	e, _ := newSearchEnv(t, nil)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	for _, s := range []string{"Deploy one.", "Deploy two.", "Deploy three.", "Deploy four.", "Deploy five."} {
		e.remember(tok, sp, s)
	}
	seen := map[uuid.UUID]bool{}
	params := url.Values{"q": {"deploy"}, "limit": {"2"}}
	pages := 0
	for {
		got := e.search(tok, params)
		pages++
		for _, it := range got.Items {
			if seen[it.Memory.ID] {
				t.Fatalf("page %d repeats %s", pages, it.Memory.Ref)
			}
			seen[it.Memory.ID] = true
		}
		if !got.HasMore {
			if got.NextCursor != "" {
				t.Errorf("a cursor on the last page")
			}
			break
		}
		if len(got.Items) != 2 || got.NextCursor == "" || pages > 3 {
			t.Fatalf("page %d: %d items, cursor %q", pages, len(got.Items), got.NextCursor)
		}
		params.Set("cursor", got.NextCursor)
		cursor := got.NextCursor
		// Another search's cursor is refused.
		e.do(call{method: "GET", path: "/v2/search?" + url.Values{"q": {"deploys"}, "cursor": {cursor}}.Encode(), token: tok}).
			fails(http.StatusBadRequest, "invalid_request")
	}
	if pages != 3 || len(seen) != 5 {
		t.Errorf("%d pages, %d memories", pages, len(seen))
	}
}

// What a search request must say.
func TestSearchRequests(t *testing.T) {
	t.Parallel()
	e, _ := newSearchEnv(t, nil)
	zz := e.user("zz")
	tok := e.session(zz)
	for _, c := range []struct {
		name, query, field string
		invalid            bool
	}{
		{"no words", "", "q", true},
		{"only spaces", "q=%20%20", "q", false},
		{"too long", "q=" + strings.Repeat("a", 501), "q", true},
		{"a bad kind", "q=x&kind=note", "kind", true},
		{"a page too large", "q=x&limit=51", "limit", true},
		{"a bad flag", "q=x&include_notes=maybe", "include_notes", true},
		{"a made-up cursor", "q=x&cursor=abc", "cursor", false},
	} {
		got := e.do(call{method: "GET", path: "/v2/search?" + c.query, token: tok, invalid: c.invalid}).fails(http.StatusBadRequest, "invalid_request")
		if got.Details.Field != c.field {
			t.Errorf("%s: field %q, want %q", c.name, got.Details.Field, c.field)
		}
	}
	// Without a searcher, the server says so.
	plain := newEnv(t)
	plain.do(call{method: "GET", path: "/v2/search?q=deploy", token: plain.session(plain.user("zz"))}).fails(http.StatusServiceUnavailable, "unavailable")
}

// With embeddings, a search finds by meaning as memax_search does.
func TestSearchByMeaning(t *testing.T) {
	t.Parallel()
	words := mockembed.NewWords([]string{"river", "queue"}, []string{"job", "task"}, []string{"background", "async"})
	e, _ := newSearchEnv(t, words)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	m := e.remember(tok, sp, "Background jobs run on River.").Memory
	e.remember(tok, sp, "The web app is a Next.js project.")
	e.indexAll(words, sp)
	got := e.search(tok, url.Values{"q": {"Which queue handles async tasks?"}})
	if len(got.Items) != 1 || got.Items[0].Memory.ID != m.ID || got.Lexical {
		t.Fatalf("found %v (lexical_only %v)", searchRefs(got), got.Lexical)
	}
}

// With include_notes, the first page carries the person's notes that
// match: a V1 space's memories, numbered as notes when it switched.
func TestSearchIncludesNotes(t *testing.T) {
	t.Parallel()
	e, _ := newSearchEnv(t, nil)
	zz, jy := e.user("zz"), e.user("jy")
	hub := e.v1Space(zz, policy.SpaceProject, "acme-web")
	e.v1Note(hub, zz, "Claude saw deploys fail when the lockfile changed.", "created_by_type = 'agent'", "created_by_slug = 'claude-code'")
	tok := e.session(zz)
	e.do(call{method: "POST", path: "/v2/spaces/" + hub.slug + ":switch", token: tok, body: map[string]any{"kind": "project"}}).
		ok(http.StatusOK, nil)
	e.remember(tok, hub, "Deploys go out through the release workflow.")

	got := e.search(tok, url.Values{"q": {"deploys"}, "include_notes": {"true"}, "limit": {"1"}})
	if len(got.Items) != 1 || len(got.Notes) != 1 || !strings.Contains(got.Notes[0].Excerpt, "lockfile") {
		t.Fatalf("items %v notes %+v", searchRefs(got), got.Notes)
	}
	// Without the flag, and after the first page, no notes.
	if got := e.search(tok, url.Values{"q": {"deploys"}}); got.Notes != nil {
		t.Errorf("notes without include_notes: %+v", got.Notes)
	}
	// Notes are their owner's: a member who joins later finds none of them.
	e.join(hub, jy, "contributor")
	if got := e.search(e.session(jy), url.Values{"q": {"deploys"}, "include_notes": {"true"}}); len(got.Notes) != 0 || len(got.Items) != 1 {
		t.Errorf("a member's search: items %v notes %+v", searchRefs(got), got.Notes)
	}
}

// The spaces list says what the switcher shows about each space on V2.
func TestSpacesCarryTheirCounts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	personal := e.space(zz, policy.SpacePersonal, "zz")
	proj := e.space(zz, policy.SpaceProject, "memax-v2")
	onV1 := e.v1Space(zz, policy.SpaceProject, "acme-web")
	tok := e.session(zz)
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	for _, s := range []string{"One.", "Two.", "Three."} {
		e.remember(tok, proj, s)
	}
	e.remember(key, proj, "A proposal.")
	stale := e.remember(tok, proj, "A stale fact.").Memory
	e.flag(stale, "stale")

	var list struct {
		Items []struct {
			ID        uuid.UUID  `json:"id"`
			CreatedAt *time.Time `json:"created_at"`
			Counts    *struct {
				Kept    int `json:"kept"`
				Waiting int `json:"waiting"`
				Agents  int `json:"agents"`
			} `json:"counts"`
		} `json:"items"`
	}
	e.do(call{method: "GET", path: "/v2/spaces", token: tok}).ok(http.StatusOK, &list)
	got := map[uuid.UUID]string{}
	for _, it := range list.Items {
		if it.CreatedAt == nil || time.Since(*it.CreatedAt) > time.Hour {
			t.Errorf("%s created %v", it.ID, it.CreatedAt)
		}
		if it.Counts == nil {
			got[it.ID] = "none"
			continue
		}
		got[it.ID] = fmt.Sprintf("%d/%d/%d", it.Counts.Kept, it.Counts.Waiting, it.Counts.Agents)
	}
	// The key connected to every space zz had: personal, memax-v2 and the
	// V1 one, whose record is V1's (no counts).
	if got[proj.id] != "4/2/1" || got[personal.id] != "0/0/1" || got[onV1.id] != "none" {
		t.Errorf("counts = %v", got)
	}
}
