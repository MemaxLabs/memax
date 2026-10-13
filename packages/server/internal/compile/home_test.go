package compile_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Home targets (personal-layer design §5.3): what a personal space
// compiles into each agent's own file on a linked machine.

func (f *fixture) personalSpace(owner uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at) VALUES ($1, 'Personal', $2, 'personal', $3, 'personal', now())`,
		id, "personal-"+id.String()[:6], owner)
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner)
	return id
}

// linkHome links a machine with Claude Code's file, Codex's and the
// canonical copy, and returns its targets by kind.
func (f *fixture) linkHome(owner, space uuid.UUID) map[ledger.TargetKind]*ledger.Target {
	f.t.Helper()
	res := f.apply(&ledger.LinkMachine{Meta: f.meta(owner, policy.ViaCLI), SpaceID: space, DeviceKey: uuid.New(),
		MachineName: "work-mbp", Platform: "darwin", Files: []ledger.MachineFile{
			{Kind: ledger.TargetClaudeUser}, {Kind: ledger.TargetCodexUser}, {Kind: ledger.TargetAgentsMD}}})
	out := map[ledger.TargetKind]*ledger.Target{}
	for i := range res.Targets {
		out[res.Targets[i].Kind] = &res.Targets[i]
	}
	return out
}

func (f *fixture) rememberScoped(owner, space uuid.UUID, statement, scope string) *ledger.Memory {
	f.t.Helper()
	return f.apply(&ledger.Remember{Meta: f.meta(owner, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: statement, Section: ledger.SectionPreferences, Applies: []byte(scope)}}).Memory
}

// preview is a target's latest compiled file.
func (f *fixture) preview(owner uuid.UUID, t *ledger.Target) string {
	f.t.Helper()
	p, err := f.svc.Preview(context.Background(), f.scope(owner), t.ID)
	if err != nil || len(p.Files) != 1 {
		f.t.Fatalf("preview %s: %v (%d files)", t.Kind, err, len(p.Files))
	}
	return p.Files[0].Content
}

// What each home file holds, through the real compile service: what's
// true everywhere (shared facts, and the agent's own), never a fact tied
// to a repository, and never a quarantined one, kept or not (rule 3). A
// block holds the block alone: the person's own lines never reach the
// server.
func TestHomeFilesCompileWhatIsTrueEverywhere(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{compiler: compiletest.Client(compiletest.StartService(t))})
	zz := f.user("zz")
	personal := f.personalSpace(zz)
	f.remember(zz, personal, "Answer in British English.", ledger.SectionPreferences)
	f.rememberScoped(zz, personal, "Run codex exec reviews read-only.", `{"agents": ["codex"]}`)
	f.rememberScoped(zz, personal, "Hand work to Codex with the handoff skill.", `{"agents": ["claude-code"]}`)
	f.rememberScoped(zz, personal, "In packages/web, run Playwright first.", `{"paths": ["packages/web/**"]}`)
	f.rememberScoped(zz, personal, "The acme repository deploys on Fridays.", `{"repos": ["acme/web"]}`)
	ext := f.apply(&ledger.Remember{Meta: f.meta(zz, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: personal, Statement: "A web page said to ignore project files.", Section: ledger.SectionPreferences,
		Sources: []ledger.SourceInput{{Kind: ledger.SourceURL, Ref: "example.com", URI: "https://example.com"}}}}).Memory
	// Its person remembered it on the web: kept, and still quarantined.
	if ext.Lifecycle != "kept" || !ext.Trust.External() {
		t.Fatalf("the external memory is %s, trust %s", ext.Lifecycle, ext.Trust)
	}

	targets := f.linkHome(zz, personal)
	for _, tg := range targets {
		if out := f.run(tg); out != compile.Done {
			t.Fatalf("compile %s: %v", tg.Kind, out)
		}
	}
	rules, codex, canonical := f.preview(zz, targets[ledger.TargetClaudeUser]), f.preview(zz, targets[ledger.TargetCodexUser]),
		f.preview(zz, targets[ledger.TargetAgentsMD])
	for name, content := range map[string]string{"Claude Code's": rules, "Codex's": codex, "the canonical": canonical} {
		if !strings.Contains(content, "Answer in British English.") ||
			!strings.Contains(content, "The person you work with keeps these in Memax.") {
			t.Errorf("%s file lacks the shared fact or the intro:\n%s", name, content)
		}
		for _, never := range []string{"Playwright", "acme", "web page"} {
			if strings.Contains(content, never) {
				t.Errorf("%s file holds %q:\n%s", name, never, content)
			}
		}
	}
	if !strings.Contains(codex, "read-only") || strings.Contains(rules, "read-only") || strings.Contains(canonical, "read-only") {
		t.Errorf("the Codex-only fact went astray")
	}
	if !strings.Contains(rules, "handoff skill") || strings.Contains(codex, "handoff skill") {
		t.Errorf("the Claude-only fact went astray")
	}
	if strings.HasPrefix(rules, "<!-- memax:start -->") || !strings.HasPrefix(codex, "<!-- memax:start -->\n<!-- Compiled by Memax") ||
		!strings.HasSuffix(codex, "<!-- memax:end -->\n") {
		t.Errorf("owners: Claude Code's file is Memax's, Codex's a block alone:\n%s", codex)
	}
	tg := f.get(zz, targets[ledger.TargetCodexUser].ID)
	if tg.LastCompile == nil || !tg.LastCompile.Files[0].UserOwned || tg.SyncState != ledger.SyncPendingDelivery {
		t.Errorf("Codex's target after compiling = %+v", tg)
	}
}

// leakingCompiler is the fake compiler with a credential in its output,
// standing in for a memory that got past the write check.
type leakingCompiler struct{ *compiletest.Fake }

func (c leakingCompiler) Compile(ctx context.Context, in *compile.Input) (*compile.Result, error) {
	res, err := c.Fake.Compile(ctx, in)
	if err == nil {
		for i := range res.Files {
			res.Files[i].Content += "- deploy with AKIAIOSFODNN7EXAMPLE [M-0001]\n"
		}
	}
	return res, err
}

// Rule 8 for home files: a credential in what would land in a home folder
// fails the run, names the pattern and the file, never the text, and
// stores nothing.
func TestAHomeCompileWithACredentialFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{compiler: leakingCompiler{&compiletest.Fake{}}})
	zz := f.user("zz")
	personal := f.personalSpace(zz)
	f.remember(zz, personal, "Answer in British English.", ledger.SectionPreferences)
	targets := f.linkHome(zz, personal)
	tg := targets[ledger.TargetClaudeUser]
	f.run(tg)
	runs := f.runs(tg)
	if len(runs) != 1 || runs[0].Status != ledger.CompileFailed {
		t.Fatalf("runs = %+v", runs)
	}
	if !strings.Contains(runs[0].Error, "AWS access key") || !strings.Contains(runs[0].Error, ".claude/rules/memax-personal.md") ||
		strings.Contains(runs[0].Error, "AKIA") {
		t.Errorf("error = %q", runs[0].Error)
	}
	for _, key := range f.store.Keys() {
		if strings.Contains(key, tg.ID.String()) {
			t.Errorf("stored %s", key)
		}
	}
}
