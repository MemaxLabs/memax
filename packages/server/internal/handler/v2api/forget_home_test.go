package v2api_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Rule 7 for the personal layer (design §5.4): a Personal memory compiled
// into every home file on two machines is forgotten, and within the minute
// each of the six files is compiled again without it, one propagation step
// each, and the words are in no table, object or key. (That the daemons
// rewrite the files on disk is the CLI's two-machine test.)
func TestForgetTakesTheWordsOutOfEveryMachinesFiles(t *testing.T) {
	t.Parallel()
	s := newForgetStack(t)
	zz := s.user("zz")
	personal := s.space(zz, policy.SpacePersonal, "Personal")
	tok := s.session(zz)
	token := word("zh")
	m := s.remember(tok, personal, "Write tests the way "+token+" would.").Memory
	s.remember(tok, personal, "Prefer small commits.")
	for _, name := range []string{"work-mbp", "home-linux"} {
		s.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
			body: linkBody(uuid.New(), name, "claude_user", "codex_user", "agents_md")}).ok(http.StatusCreated, nil)
	}
	targets := s.awaitTargets(tok, personal.slug, 30*time.Second)
	if len(targets) != 6 {
		t.Fatalf("%d targets, want 3 on each of 2 machines", len(targets))
	}
	if hits := s.grepObjects(token); len(hits) < 6 {
		t.Fatalf("before the Forget, the words are in %d stored compiles, want every home file's: %v", len(hits), hits)
	}

	var res forgetResult
	s.do(call{method: "POST", path: "/v2/memories/" + m.Ref + ":forget?space=" + personal.slug, token: tok,
		header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, &res)
	forgotAt := time.Now()
	var ts tombstone
	for {
		s.do(call{method: "GET", path: "/v2/memories/" + m.Ref + "/tombstone?space=" + personal.slug, token: tok}).ok(http.StatusOK, &ts)
		if ts.Status == ledger.TombstoneDone {
			break
		}
		if time.Since(forgotAt) > time.Minute {
			t.Fatalf("propagation didn't finish within the minute: %+v", ts)
		}
		time.Sleep(25 * time.Millisecond)
	}
	steps := 0
	for _, st := range ts.Steps {
		if st.Kind == "target" {
			steps++
		}
	}
	if steps != 6 {
		t.Errorf("%d target steps, want one per file per machine: %+v", steps, ts.Steps)
	}
	for _, tg := range s.awaitTargets(tok, personal.slug, 30*time.Second) {
		if tg.CompiledGen < tg.DirtyGen {
			t.Errorf("%s isn't compiled past the Forget", tg.Label)
		}
	}
	if hits := s.grepDB(token); len(hits) != 0 {
		t.Errorf("after the Forget, the words are still in: %v", hits)
	}
	if hits := s.grepObjects(token); len(hits) != 0 {
		t.Errorf("after the Forget, object storage still holds the words in: %v", hits)
	}
	if hits := s.grepRedis(token); len(hits) != 0 {
		t.Errorf("after the Forget, Redis still holds the words in: %v", hits)
	}
}

// homeTombstone is what the per-machine assertions read of a tombstone.
type homeTombstone struct {
	Status string `json:"status"`
	Steps  []struct {
		Kind   string `json:"kind"`
		Status string `json:"status"`
		Reason string `json:"reason"`
		Target *struct {
			ID      uuid.UUID `json:"id"`
			Label   string    `json:"label"`
			Machine *struct {
				Label      string     `json:"label"`
				Name       string     `json:"name"`
				Linked     *bool      `json:"linked"`
				LastSeenAt *time.Time `json:"last_seen_at"`
			} `json:"machine"`
		} `json:"target"`
	} `json:"steps"`
	Unreachable []struct {
		Kind     string   `json:"kind"`
		Files    []string `json:"files"`
		Machines []string `json:"machines"`
		Count    int      `json:"count"`
	} `json:"unreachable"`
}

// The tombstone per machine (personal-layer design §5.4): a machine online
// waits for its daemon; one whose daemon hasn't been seen is named offline
// with its last-seen time, and is rewritten when it reconnects; a hand
// edit on a machine is held there and named; no home file is said to be in
// git history; and the person's own files Memax imported it from are
// counted, never named.
func TestForgetsTombstoneNamesEachMachine(t *testing.T) {
	t.Parallel()
	s := newForgetStack(t)
	zz := s.user("zz")
	personal := s.space(zz, policy.SpacePersonal, "Personal")
	tok := s.session(zz)
	machines := map[string]uuid.UUID{}
	for _, name := range []string{"work-mbp", "home-linux"} {
		var linked wireMachineResult
		s.do(call{method: "POST", path: "/v2/spaces/" + personal.slug + "/machines", token: tok,
			body: linkBody(uuid.New(), name, "claude_user", "codex_user")}).ok(http.StatusCreated, &linked)
		machines[name] = linked.Machine.ID
	}
	// Brought in from the person's own ~/.claude/CLAUDE.md, as init sends it.
	var kept result
	s.do(call{method: "POST", path: memoriesPath(personal), token: tok, body: map[string]any{
		"statement": "Answer in British English.", "section": "preferences",
		"sources": []map[string]any{{"kind": "file", "ref": "~/.claude/CLAUDE.md:3", "locator": map[string]any{"path": "~/.claude/CLAUDE.md"}}},
	}}).ok(http.StatusCreated, &kept)
	targets := s.awaitTargets(tok, personal.slug, 30*time.Second)
	if len(targets) != 4 {
		t.Fatalf("%d targets, want 2 on each of 2 machines", len(targets))
	}
	byLabel := map[string]uuid.UUID{}
	var listed struct {
		Items []wireTarget `json:"items"`
	}
	for name, id := range machines {
		// work-mbp's daemon polls (and is seen); home-linux's was last seen two days ago.
		s.do(call{method: "GET", path: "/v2/spaces/" + personal.slug + "/targets?machine=" + id.String(), token: tok}).
			ok(http.StatusOK, &listed)
		for _, tg := range listed.Items {
			byLabel[name+" "+tg.Label] = tg.ID
		}
	}
	twoDaysAgo := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	s.exec(`UPDATE v2.machines SET last_seen_at = $2 WHERE id = $1`, machines["home-linux"], twoDaysAgo)
	// A hand edit on home-linux's Codex file, before the Forget.
	s.do(call{method: "POST", path: targetPath(byLabel["home-linux ~/.codex/AGENTS.md"], "/observations"), token: tok,
		body: map[string]any{"path": ".codex/AGENTS.md", "content": "# Mine\n<!-- memax:start -->\n- edited by hand\n<!-- memax:end -->\n",
			"device_id": "home-linux"}}).ok(http.StatusCreated, nil)

	s.do(call{method: "POST", path: "/v2/memories/" + kept.Memory.Ref + ":forget?space=" + personal.slug, token: tok,
		header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, nil)
	read := func() homeTombstone {
		t.Helper()
		var ts homeTombstone
		deadline := time.Now().Add(time.Minute)
		for {
			s.do(call{method: "GET", path: "/v2/memories/" + kept.Memory.Ref + "/tombstone?space=" + personal.slug, token: tok}).
				ok(http.StatusOK, &ts)
			if ts.Status == ledger.TombstoneDone || time.Now().After(deadline) {
				return ts
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	ts := read()
	step := func(ts homeTombstone, machine, label string) (status, reason string, seen *time.Time, linked bool) {
		t.Helper()
		for _, st := range ts.Steps {
			if st.Target != nil && st.Target.Machine != nil && st.Target.Machine.Name == machine && st.Target.Label == label {
				return st.Status, st.Reason, st.Target.Machine.LastSeenAt, st.Target.Machine.Linked != nil && *st.Target.Machine.Linked
			}
		}
		for _, st := range ts.Steps {
			if st.Target != nil {
				t.Logf("step %s %s target %q machine %+v", st.Status, st.Reason, st.Target.Label, st.Target.Machine)
			}
		}
		t.Fatalf("no step for %s on %s: %+v", label, machine, ts.Steps)
		return
	}
	if status, reason, seen, linked := step(ts, "work-mbp", "~/.claude/rules/memax-personal.md"); status != "waiting" ||
		reason != "delivery" || seen == nil || !linked {
		t.Errorf("work-mbp, online: %s %s %v %v", status, reason, seen, linked)
	}
	if status, reason, seen, _ := step(ts, "home-linux", "~/.claude/rules/memax-personal.md"); status != "waiting" ||
		reason != "offline" || seen == nil || !seen.Equal(twoDaysAgo) {
		t.Errorf("home-linux, offline: %s %s %v", status, reason, seen)
	}
	if status, reason, _, _ := step(ts, "home-linux", "~/.codex/AGENTS.md"); status != "held" || reason != "hand_edit" {
		t.Errorf("home-linux's hand edit: %s %s", status, reason)
	}
	kinds := map[string]int{}
	for _, u := range ts.Unreachable {
		kinds[u.Kind]++
		switch u.Kind {
		case "hand_edits":
			if !slices.Equal(u.Files, []string{"~/.codex/AGENTS.md"}) || !slices.Equal(u.Machines, []string{"home-linux"}) {
				t.Errorf("hand edits: %+v", u)
			}
		case "source_files":
			if u.Count != 1 {
				t.Errorf("source files: %+v", u)
			}
		}
	}
	if kinds["git_history"] != 0 || kinds["hand_edits"] != 1 || kinds["source_files"] != 1 {
		t.Errorf("unreachable: %+v", ts.Unreachable)
	}

	// home-linux connects: its daemon writes the compile without it.
	id := byLabel["home-linux ~/.claude/rules/memax-personal.md"]
	var pv preview
	s.do(call{method: "GET", path: targetPath(id, "/preview"), token: tok}).ok(http.StatusOK, &pv)
	s.do(call{method: "POST", path: targetPath(id, "/deliveries"), token: tok, header: map[string]string{"X-Memax-Via": "cli"},
		body: map[string]any{"compile": pv.Compile.Ref, "sha256": pv.Compile.DriftSHA256}}).ok(http.StatusOK, nil)
	if status, reason, _, _ := step(read(), "home-linux", "~/.claude/rules/memax-personal.md"); status != "done" || reason != "" {
		t.Errorf("home-linux after it connects: %s %s", status, reason)
	}
}
