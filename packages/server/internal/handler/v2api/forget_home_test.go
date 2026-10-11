package v2api_test

import (
	"net/http"
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
