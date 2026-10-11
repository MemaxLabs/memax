package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// personalItem is a statement from a file on the person's machine, as init
// sends it to their Personal space; repos is its scope.repos, if any.
func personalItem(key, ref, statement string, repos ...string) ledger.ImportItem {
	it := ledger.ImportItem{
		Key: key, Ref: ref, Location: ledger.ImportHome,
		NewMemory: ledger.NewMemory{Statement: statement, Section: ledger.SectionPreferences,
			Sources: []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: ref, Trust: policy.TrustPerson}}},
	}
	if len(repos) > 0 {
		b, _ := json.Marshal(map[string][]string{"repos": repos})
		it.Applies = b
	}
	return it
}

// importRun uploads one init run's import into a space, keyed as init
// keys it ("<run>:<space>:0"), with its files.
func (f *fixture) importRun(owner, space uuid.UUID, run string, files []ledger.ImportFile, items ...ledger.ImportItem) *ledger.ImportResult {
	f.t.Helper()
	res, err := f.l.Import(context.Background(), ledger.ImportRequest{
		Meta:    ledger.Meta{Actor: person(owner), Scope: f.scope(owner), Via: policy.ViaCLI, IdempotencyKey: run + ":" + space.String() + ":0"},
		SpaceID: space, Client: "memax-cli test", Files: files, Items: items,
	})
	if err != nil {
		f.t.Fatalf("Import: %v", err)
	}
	return res
}

func repos(t *testing.T, m *ledger.Memory) []string {
	t.Helper()
	return ledger.ScopeRepos(m.Applies)
}

var personalFiles = []ledger.ImportFile{
	{Path: "CLAUDE.local.md", Kind: "claude_local", Agent: "claude-code", Location: ledger.ImportHome, Trust: policy.TrustPerson},
	{Path: "~/.claude/projects/…/memory/notes.md", Kind: "claude_memory", Agent: "claude-code", Location: ledger.ImportHome, Trust: policy.TrustAgentOwnWork},
	{Path: "~/.claude/CLAUDE.md", Kind: "claude_user", Agent: "claude-code", Location: ledger.ImportHome, Trust: policy.TrustPerson},
}

// scope.repos goes in with an import (any spelling, stored as owner/name,
// sorted, once), stays through Keep, and is in the export; a repeat folded
// from a file that applies everywhere applies everywhere.
func TestRepoScopeThroughImportKeepAndExport(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	res := f.importRun(zz, personal, "init-0000aaaa", personalFiles,
		personalItem("a", "CLAUDE.local.md:3", "Use the staging database when working on acme.", "https://github.com/Acme/Web.git", "acme/web", "acme/api"),
		personalItem("b", "CLAUDE.local.md:5", "Prefer small commits.", "acme/web"),
		personalItem("c", "~/.claude/CLAUDE.md:2", "Prefer small commits"),
	)
	if got := outcomes(res); !slices.Equal(got, []ledger.ImportOutcome{ledger.ImportProposed, ledger.ImportProposed, ledger.ImportFolded}) {
		t.Fatalf("outcomes %v", got)
	}
	scope := f.scope(zz)
	tied, err := f.l.GetMemory(context.Background(), scope, res.Items[0].Memory.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if got := repos(t, tied); !slices.Equal(got, []string{"acme/api", "acme/web"}) {
		t.Errorf("scope.repos = %v, want [acme/api acme/web]", got)
	}
	folded, err := f.l.GetMemory(context.Background(), scope, res.Items[1].Memory.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if got := repos(t, folded); got != nil || string(folded.Applies) != "{}" {
		t.Errorf("a repeat from ~/.claude/CLAUDE.md applies everywhere; scope = %s", folded.Applies)
	}

	kept := f.apply(&ledger.Keep{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: tied.Ref})
	if kept.Outcome != ledger.OutcomeApplied || !slices.Equal(repos(t, kept.Memory), []string{"acme/api", "acme/web"}) {
		t.Fatalf("keep: %s, scope %s", kept.Outcome, kept.Memory.Applies)
	}

	var c collector
	if err := f.l.ReadExport(context.Background(), scope.Narrow(personal), personal, &c); err != nil {
		t.Fatal(err)
	}
	var exported *ledger.Memory
	for i := range c.memories {
		if c.memories[i].Memory.ID == tied.ID {
			exported = &c.memories[i].Memory
		}
	}
	if exported == nil || !slices.Equal(ledger.ScopeRepos(exported.Applies), []string{"acme/api", "acme/web"}) {
		t.Errorf("the export's memory: %+v", exported)
	}

	// A scope Memax can't read is refused, with what to send.
	for _, bad := range []string{`{"repos": ["web"]}`, `{"repos": "acme/web"}`, `{"owner": "acme"}`} {
		nm := fact(personal, "Something about "+bad)
		nm.Applies = json.RawMessage(bad)
		_, err := f.l.Apply(context.Background(), &ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm})
		if !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("scope %s: %v, want ErrInvalid", bad, err)
		}
	}
	// A null field is left out; paths and agents pass as they are.
	nm := fact(personal, "Run the linter before pushing.")
	nm.Applies = json.RawMessage(`{"paths": ["packages/web/**"], "repos": null}`)
	m := f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm}).Memory
	var got map[string]any
	if err := json.Unmarshal(m.Applies, &got); err != nil || len(got) != 1 || got["paths"] == nil {
		t.Errorf("scope = %s, want paths only", m.Applies)
	}
}

// ScopeMemory: a person widens a memory to everywhere (one scoped
// receipt, the paths untouched, the kept memory's targets recompiled);
// asking again changes nothing; Memax only narrows; agents never change it;
// another person's memory isn't there; a forgotten one can't change.
func TestScopeMemory(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	scope := f.scope(zz)
	nm := fact(personal, "Use the staging database when working on acme.")
	nm.Applies = json.RawMessage(`{"paths": ["services/**"], "repos": ["acme/web"]}`)
	m := f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm}).Memory
	f.brief(zz, personal, 0, demoSections(m.Ref))
	f.target(zz, personal, ledger.TargetAgentsMD)
	gen := f.count(`SELECT dirty_gen FROM v2.targets WHERE space_id = $1`, personal)

	// Memax never widens.
	memax := ledger.Actor{Kind: policy.ActorMemax, Name: "Memax", Credential: policy.CredentialSession}
	refusedWith(t, f.apply(&ledger.ScopeMemory{Meta: meta(memax, scope.Narrow(personal), policy.ViaSystem), Memory: m.Ref}),
		policy.CodePersonMustReview)
	// Agents never change it.
	refusedWith(t, f.apply(&ledger.ScopeMemory{Meta: meta(agentFor(policy.AutonomyWrite), scope, policy.ViaMCP), Memory: m.Ref}),
		policy.CodePersonMustReview)
	// Someone else's memory isn't there.
	if _, err := f.l.Apply(context.Background(), &ledger.ScopeMemory{Meta: meta(person(jy), f.scope(jy), policy.ViaWeb), Memory: m.Ref}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another person: %v, want ErrNotFound", err)
	}
	// An If-Match that isn't the memory's version clashes.
	var clash *ledger.EditClashError
	if _, err := f.l.Apply(context.Background(), &ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 2}); !errors.As(err, &clash) {
		t.Errorf("stale If-Match: %v, want an edit clash", err)
	}

	res := f.apply(&ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 1})
	if res.Outcome != ledger.OutcomeApplied || len(res.Receipts) != 1 || res.Receipts[0].Action != ledger.ActionScoped {
		t.Fatalf("widen: %s %+v", res.Outcome, res.Receipts)
	}
	if repos(t, res.Memory) != nil || res.Memory.Statement != m.Statement || res.Memory.Version != 1 {
		t.Errorf("widened: scope %s, %q v%d", res.Memory.Applies, res.Memory.Statement, res.Memory.Version)
	}
	var got map[string][]string
	if err := json.Unmarshal(res.Memory.Applies, &got); err != nil || !slices.Equal(got["paths"], []string{"services/**"}) {
		t.Errorf("the paths changed: %s", res.Memory.Applies)
	}
	if n := f.count(`SELECT dirty_gen FROM v2.targets WHERE space_id = $1`, personal); n != gen+1 {
		t.Errorf("dirty_gen %d, want %d: a kept memory's scope changes what compiles", n, gen+1)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND action = 'scoped' AND actor_kind = 'person' AND via = 'web'`, m.ID); n != 1 {
		t.Errorf("%d scoped receipts", n)
	}

	// The same scope again is nothing to change; the same key replays.
	if _, err := f.l.Apply(context.Background(), &ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("again: %v, want ErrInvalid", err)
	}
	same := meta(person(zz), scope, policy.ViaWeb)
	first := f.apply(&ledger.ScopeMemory{Meta: same, Memory: m.Ref, Repos: []string{"acme/api"}})
	replay := f.apply(&ledger.ScopeMemory{Meta: same, Memory: m.Ref, Repos: []string{"acme/api"}})
	if !replay.Replayed || len(replay.Receipts) != 1 || replay.Receipts[0].ID != first.Receipts[0].ID {
		t.Errorf("replay: %+v", replay)
	}
	f.apply(&ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref})
	// Memax narrows, from everywhere; and a person narrows it again.
	narrowed := f.apply(&ledger.ScopeMemory{Meta: meta(memax, scope.Narrow(personal), policy.ViaSystem), Memory: m.Ref, Repos: []string{"Acme/Web"}})
	if narrowed.Outcome != ledger.OutcomeApplied || !slices.Equal(repos(t, narrowed.Memory), []string{"acme/web"}) {
		t.Errorf("memax narrows: %s %s", narrowed.Outcome, narrowed.Memory.Applies)
	}
	if _, err := f.l.Apply(context.Background(), &ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, Repos: []string{"web"}}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("a repository without its owner: %v, want ErrInvalid", err)
	}

	f.apply(&ledger.Forget{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 1})
	if _, err := f.l.Apply(context.Background(), &ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref}); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("forgotten: %v, want ErrInvalidTransition", err)
	}
}

// Undo of a person's scope change: the scope comes back exactly (paths and
// repositories), with one undid receipt, and a kept memory's targets
// recompile; once only. Memax's narrowing is nobody's decision to take
// back; a later change is in the way until it is undone itself; and a
// faded memory stays faded.
func TestUndoScope(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	scope := f.scope(zz)
	refusal := func(err error) string {
		var ue *ledger.UndoError
		if errors.As(err, &ue) {
			return ue.Reason
		}
		return ""
	}
	nm := fact(personal, "Use the staging database when working on acme.")
	nm.Applies = json.RawMessage(`{"paths": ["services/**"], "repos": ["acme/web"]}`)
	m := f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm}).Memory
	f.brief(zz, personal, 0, demoSections(m.Ref))
	f.target(zz, personal, ledger.TargetAgentsMD)
	sameScope := func(t *testing.T, got, want json.RawMessage) {
		t.Helper()
		var g, w map[string]any
		if err := json.Unmarshal(got, &g); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want, &w); err != nil {
			t.Fatal(err)
		}
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(w)
		if string(gb) != string(wb) {
			t.Errorf("scope %s, want %s", gb, wb)
		}
	}
	before := f.mem(zz, m.ID).Applies
	widen := func() ledger.Result {
		return f.apply(&ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref})
	}
	gen := func() int { return f.count(`SELECT dirty_gen FROM v2.targets WHERE space_id = $1`, personal) }

	wide := widen()
	g := gen()
	res, err := undo(f, person(zz), scope, wide.Receipts[0].ID)
	if err != nil || res.Outcome != ledger.OutcomeApplied || len(res.Receipts) != 1 || res.Receipts[0].Action != ledger.ActionUndid {
		t.Fatalf("undo: %v %+v", err, res)
	}
	after := f.mem(zz, m.ID)
	sameScope(t, after.Applies, before)
	if after.Lifecycle != "kept" || after.Version != 1 || after.Statement != m.Statement {
		t.Errorf("after undo: %s v%d %q", after.Lifecycle, after.Version, after.Statement)
	}
	if n := gen(); n != g+1 {
		t.Errorf("dirty_gen %d, want %d: the kept set's scope changed again", n, g+1)
	}
	if _, err := undo(f, person(zz), scope, wide.Receipts[0].ID); refusal(err) != ledger.UndoAlreadyUndone {
		t.Errorf("twice: %v", err)
	}

	// Memax's narrowing (the backfill) keeps no undo entry.
	widen()
	memax := ledger.Actor{Kind: policy.ActorMemax, Name: "Memax", Credential: policy.CredentialSession}
	narrowed := f.apply(&ledger.ScopeMemory{Meta: meta(memax, scope.Narrow(personal), policy.ViaSystem), Memory: m.Ref, Repos: []string{"acme/web"}})
	if _, err := undo(f, person(zz), scope, narrowed.Receipts[0].ID); refusal(err) != ledger.UndoNotUndoable {
		t.Errorf("undo Memax's narrowing: %v, want not_undoable", err)
	}

	// A later change is in the way; undone itself, it isn't.
	first := widen()
	second := f.apply(&ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, Repos: []string{"acme/api"}})
	if _, err := undo(f, person(zz), scope, first.Receipts[0].ID); refusal(err) != ledger.UndoLaterChanges {
		t.Errorf("undo under a later change: %v, want later_changes", err)
	}
	if _, err := undo(f, person(zz), scope, second.Receipts[0].ID); err != nil {
		t.Fatalf("undo the later change: %v", err)
	}
	if got := repos(t, f.mem(zz, m.ID)); got != nil {
		t.Errorf("after undoing the second: repos %v, want everywhere", got)
	}
	if _, err := undo(f, person(zz), scope, first.Receipts[0].ID); err != nil {
		t.Fatalf("then the first: %v", err)
	}
	sameScope(t, f.mem(zz, m.ID).Applies, before)
	if n := f.count(`SELECT count(*) FROM v2.undo_entries WHERE space_id = $1 AND command = 'scope'`, personal); n != 4 {
		t.Errorf("%d scope undo entries, want the person's 4", n)
	}

	// A faded memory changes where it applies, and undoing that leaves it faded.
	w := newDreamWorld(t)
	now := time.Now()
	w.publish(now.Truncate(time.Hour), ledger.PlannedAction{Kind: ledger.DreamFade, Memory: w.fd.ID, Version: 1, Unread: &now})
	ws := w.f.scope(w.owner)
	scoped := w.f.apply(&ledger.ScopeMemory{Meta: meta(person(w.owner), ws, policy.ViaWeb), Memory: w.fd.Ref, Repos: []string{"acme/web"}})
	if scoped.Outcome != ledger.OutcomeApplied {
		t.Fatalf("scope a faded memory: %s %s", scoped.Outcome, scoped.Policy.Message)
	}
	if _, err := undo(w.f, person(w.owner), ws, scoped.Receipts[0].ID); err != nil {
		t.Fatalf("undo on a faded memory: %v", err)
	}
	if fd := w.f.mem(w.owner, w.fd.ID); fd.Lifecycle != "faded" || repos(t, fd) != nil {
		t.Errorf("faded after undo: %s, repos %v", fd.Lifecycle, repos(t, fd))
	}
}
