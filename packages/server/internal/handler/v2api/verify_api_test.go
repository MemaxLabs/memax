package v2api_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/stalesweep"
)

// staleReport is GET /v2/memories/{ref}/stale as the tests read it.
type staleReport struct {
	Ref        string    `json:"ref"`
	MemoryID   uuid.UUID `json:"memory_id"`
	Version    int       `json:"version"`
	Statement  string    `json:"statement"`
	State      string    `json:"state"`
	Stale      bool      `json:"stale"`
	StaleAfter *string   `json:"stale_after"`
	Flagged    *receipt  `json:"flagged"`
	Kept       *receipt  `json:"kept"`
	ReadsSince int       `json:"reads_since"`
	Causes     []struct {
		Kind       string `json:"kind"`
		StaleAfter string `json:"stale_after"`
	} `json:"causes"`
}

type verifyOut struct {
	result
	Tombstone *struct {
		Ref string `json:"ref"`
	} `json:"tombstone"`
}

// verifyEnv is a project space whose memories passed their dates to check
// again, flagged by Memax's hourly sweep, with AGENTS.md to recompile.
type verifyEnv struct {
	*dreamEnv
	stale []memory
}

func newVerifyEnv(t *testing.T, statements ...string) *verifyEnv {
	t.Helper()
	e := newEnv(t)
	d := &dreamEnv{env: e, owner: e.user("zz")}
	d.token = e.session(d.owner)
	d.sp = e.space(d.owner, policy.SpaceProject, "memax-v2")
	e.do(call{method: "POST", path: briefPath(d.sp), token: d.token, body: briefBody("memax-v2")}).ok(http.StatusCreated, nil)
	e.do(call{method: "POST", path: targetsPath(d.sp), token: d.token, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, nil)
	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	v := &verifyEnv{dreamEnv: d}
	for _, s := range statements {
		var res result
		e.do(call{method: "POST", path: memoriesPath(d.sp), token: d.token,
			body: map[string]any{"statement": s, "section": "conventions", "stale_after": past}}).ok(http.StatusCreated, &res)
		v.stale = append(v.stale, res.Memory)
	}
	if _, err := stalesweep.FlagSpace(context.Background(), e.ledger, ledger.StaleSpaceArgs{SpaceID: d.sp.id, Slot: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return v
}

func (v *verifyEnv) verify(m memory, version int, body map[string]any) *resp {
	return v.do(call{method: "POST", path: memoryPath(m, ":verify"), token: v.token,
		header: map[string]string{"If-Match": strconv.Quote(strconv.Itoa(version))}, body: body})
}

// VerifyStale over the API: why it is stale, then each answer, its
// receipts and the flag, the date and the compile that follow.
func TestVerifyOverTheAPI(t *testing.T) {
	t.Parallel()
	v := newVerifyEnv(t, "Ask memax answers with the Haiku tier.", "Releases are cut on Fridays.", "The CLI prints receipts in web order.")
	haiku, fridays, receipts := v.stale[0], v.stale[1], v.stale[2]

	// Why: Memax flagged it, its date passed, kept by the person.
	var rep staleReport
	r := v.do(call{method: "GET", path: "/v2/memories/" + haiku.Ref + "/stale?space=" + v.sp.slug, token: v.token}).ok(http.StatusOK, &rep)
	if r.header.Get("ETag") != `"1"` {
		t.Errorf("ETag %q", r.header.Get("ETag"))
	}
	if !rep.Stale || rep.State != "stale" || rep.Version != 1 || rep.Statement != haiku.Statement || rep.Flagged == nil ||
		rep.Flagged.ActorKind != "memax" || rep.Flagged.Action != "flagged" || rep.Kept == nil || rep.Kept.Action != "kept" ||
		len(rep.Causes) != 1 || rep.Causes[0].Kind != "date" {
		t.Fatalf("report %+v", rep)
	}

	// Still true: verified by the person on the web, citing the date.
	var out verifyOut
	r = v.verify(haiku, 1, map[string]any{"answer": "still_true"}).ok(http.StatusOK, &out)
	if r.header.Get("ETag") != `"1"` || out.Outcome != "applied" || out.Memory.State != "kept" || len(out.Receipts) != 1 {
		t.Fatalf("still true: %s %+v", r.header.Get("ETag"), out)
	}
	if rc := out.Receipts[0]; rc.Action != "verified" || rc.ActorKind != "person" || rc.Source == nil || rc.Source.Kind != "date" {
		t.Errorf("receipt %+v", rc)
	}
	rep = staleReport{}
	v.do(call{method: "GET", path: "/v2/memories/" + haiku.ID.String() + "/stale", token: v.token}).ok(http.StatusOK, &rep)
	if rep.Stale || len(rep.Causes) != 0 || rep.StaleAfter != nil || rep.Kept == nil || rep.Kept.Action != "verified" {
		t.Errorf("after still true: %+v", rep)
	}
	// It isn't stale any more.
	v.verify(haiku, 1, map[string]any{"answer": "still_true"}).fails(http.StatusConflict, "invalid_transition")

	// Update: edited, then verified; Undo puts it back as it was.
	out = verifyOut{}
	r = v.verify(fridays, 1, map[string]any{"answer": "update", "statement": "Releases are cut on Thursdays.",
		"reason": "The release train moved."}).ok(http.StatusOK, &out)
	if r.header.Get("ETag") != `"2"` || out.Memory.Version != 2 || out.Memory.Statement != "Releases are cut on Thursdays." ||
		len(out.Receipts) != 2 || out.Receipts[0].Action != "edited" || out.Receipts[1].Action != "verified" ||
		out.Receipts[1].Reason != "The release train moved." {
		t.Fatalf("update: %+v", out)
	}
	var undone memoriesOut
	v.do(call{method: "POST", path: "/v2/receipts/" + out.Receipts[1].ID.String() + ":undo", token: v.token}).ok(http.StatusOK, &undone)
	if undone.Memory.State != "stale" || undone.Memory.Version != 1 {
		t.Errorf("after undo: %+v", undone.Memory)
	}

	// Forget it: Forget itself, with its tombstone.
	out = verifyOut{}
	r = v.verify(receipts, 1, map[string]any{"answer": "forget", "note": "No longer how the CLI works."}).ok(http.StatusOK, &out)
	if out.Memory.State != "forgotten" || out.Tombstone == nil || out.Tombstone.Ref != receipts.Ref || len(out.Receipts) == 0 ||
		out.Receipts[0].Action != "forgot" {
		t.Errorf("forget: %+v", out)
	}

	// What's refused before anything is written.
	v.verify(fridays, 2, map[string]any{"answer": "still_true"}).fails(http.StatusPreconditionFailed, "edit_clash")
	v.verify(fridays, 1, map[string]any{"answer": "forget", "statement": "words"}).fails(http.StatusBadRequest, "invalid_request")
	v.verify(fridays, 1, map[string]any{"answer": "still_true", "note": "a note"}).fails(http.StatusBadRequest, "invalid_request")
	v.verify(fridays, 1, map[string]any{"answer": "update", "statement": fridays.Statement}).fails(http.StatusBadRequest, "invalid_request")
	v.verify(fridays, 1, map[string]any{"answer": "still_true",
		"stale_after": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}).fails(http.StatusBadRequest, "invalid_request")
	v.do(call{method: "POST", path: memoryPath(fridays, ":verify"), token: v.token, invalid: true,
		header: map[string]string{"If-Match": ""}, body: map[string]any{"answer": "still_true"}}).
		fails(http.StatusPreconditionRequired, "precondition_required")
	key, _ := v.apiKey(v.owner, keyOpts{agent: "codex"})
	if got := v.do(call{method: "POST", path: memoryPath(fridays, ":verify"), token: key,
		header: map[string]string{"If-Match": `"1"`}, body: map[string]any{"answer": "still_true"}}).fails(http.StatusForbidden, "refused"); got.Details.Policy.Code != "verify_by_person" {
		t.Errorf("an API key: %+v", got.Details.Policy)
	}
	// A date answer.
	next := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	v.verify(fridays, 1, map[string]any{"answer": "still_true", "stale_after": next.Format(time.RFC3339)}).ok(http.StatusOK, &out)
	rep = staleReport{}
	v.do(call{method: "GET", path: "/v2/memories/" + fridays.Ref + "/stale?space=" + v.sp.slug, token: v.token}).ok(http.StatusOK, &rep)
	if rep.Stale || rep.StaleAfter == nil {
		t.Errorf("after a date answer: %+v", rep)
	}
	// Someone else's space isn't there.
	other := v.session(v.user("jy"))
	v.do(call{method: "GET", path: "/v2/memories/" + fridays.ID.String() + "/stale", token: other}).fails(http.StatusNotFound, "not_found")
}

type memoriesOut struct {
	Memory   memory    `json:"memory"`
	Memories []memory  `json:"memories"`
	Receipts []receipt `json:"receipts"`
}
