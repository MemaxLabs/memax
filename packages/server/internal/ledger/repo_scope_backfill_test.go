package ledger_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// BackfillRepoScope ties what an earlier init brought into Personal from
// CLAUDE.local.md and Claude Code's auto memory, without scope.repos, to
// the repository of the project space the same run imported into: a dry
// run writes nothing, a run scopes each such memory once as Memax, and
// running it again changes nothing. A memory also from a file that applies
// everywhere, from a run whose repository is unknown, written some other
// way, or one a person already chose a scope for, stays as it is; another
// person's Personal space is read under their own scope.
func TestBackfillRepoScope(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "acme-web")
	f.exec(`UPDATE hubs SET repository = 'Acme/Web' WHERE id = $1`, project)
	other := f.user("jy")
	otherPersonal := f.space(other, policy.SpacePersonal, "Personal")

	// What an init run before scope.repos sent: the project's files to the
	// project space, the machine's to Personal, keyed by the run.
	f.importRun(zz, project, "init-0000aaaa", []ledger.ImportFile{{Path: "AGENTS.md", Kind: "agents_md", Location: ledger.ImportRepository}},
		fileItem("p", "AGENTS.md:2", "Run pnpm test before pushing."))
	written := f.remember(zz, personal, "Write tests first.")
	res := f.importRun(zz, personal, "init-0000aaaa", personalFiles,
		personalItem("a", "CLAUDE.local.md:3", "Use the staging database when working on acme."),
		personalItem("b", "~/.claude/projects/…/memory/notes.md:2", "The web app's e2e tests need the whole stack up."),
		personalItem("c", "~/.claude/CLAUDE.md:1", "Answer in British English."),
		personalItem("d", "CLAUDE.local.md:5", "Prefer small commits."),
		personalItem("e", "~/.claude/CLAUDE.md:4", "Prefer small commits"),
		personalItem("f", "CLAUDE.local.md:9", "Write tests first"),
	)
	// A run outside any project space: its repository is unknown.
	alone := f.importRun(zz, personal, "init-0000bbbb", personalFiles[:1],
		personalItem("g", "CLAUDE.local.md:1", "Keep the feature flags in flags.ts."))
	// Another person's run, which zz's backfill never reads.
	f.importRun(other, otherPersonal, "init-0000cccc", personalFiles[:1], personalItem("h", "CLAUDE.local.md:1", "Use tabs."))
	scope := f.scope(zz)
	f.apply(&ledger.Keep{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: res.Items[0].Memory.Ref})

	dry, err := f.l.BackfillRepoScope(context.Background(), ledger.RepoScopeOptions{Users: []uuid.UUID{zz}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Spaces != 1 || dry.Imports != 2 || dry.Scoped != 2 || dry.Everywhere != 2 || dry.NoRepository != 1 || dry.Already != 0 || dry.Failed != 0 {
		t.Errorf("dry run: %+v", dry)
	}
	if len(dry.Changes) != 2 || dry.Changes[0].Memory != res.Items[0].Memory.Ref || !slices.Equal(dry.Changes[0].Repos, []string{"acme/web"}) ||
		!slices.Equal(dry.Changes[0].Files, []string{"CLAUDE.local.md"}) || dry.Changes[0].Outcome != "planned" {
		t.Errorf("dry run changes: %+v", dry.Changes)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE action = 'scoped'`); n != 0 {
		t.Fatalf("a dry run wrote %d scoped receipts", n)
	}

	run, err := f.l.BackfillRepoScope(context.Background(), ledger.RepoScopeOptions{Users: []uuid.UUID{zz}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Scoped != 2 || run.Failed != 0 || run.Changes[0].Outcome != "scoped" {
		t.Errorf("run: %+v", run)
	}
	for _, it := range []ledger.ImportItemResult{res.Items[0], res.Items[1]} {
		m, err := f.l.GetMemory(context.Background(), scope, it.Memory.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(repos(t, m), []string{"acme/web"}) || m.Version != 1 {
			t.Errorf("%s: scope %s v%d", m.Ref, m.Applies, m.Version)
		}
	}
	for _, id := range []uuid.UUID{res.Items[2].Memory.ID, res.Items[3].Memory.ID, alone.Items[0].Memory.ID, written.ID} {
		m, err := f.l.GetMemory(context.Background(), scope, id.String())
		if err != nil {
			t.Fatal(err)
		}
		if repos(t, m) != nil {
			t.Errorf("%s was tied to %v", m.Ref, repos(t, m))
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE action = 'scoped' AND actor_kind = 'memax' AND via = 'system'
		AND reason LIKE 'Applies only in acme/web: memax init imported it from that repository''s own notes (%'`); n != 2 {
		t.Errorf("%d scoped receipts by Memax with a reason, want 2", n)
	}
	// Again: nothing to do. And a memory a person widened stays everywhere.
	f.apply(&ledger.ScopeMemory{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: res.Items[1].Memory.Ref})
	again, err := f.l.BackfillRepoScope(context.Background(), ledger.RepoScopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Scoped != 0 || again.Already != 2 || again.Spaces != 2 {
		t.Errorf("again: %+v", again)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE action = 'scoped'`); n != 3 {
		t.Errorf("%d scoped receipts after running again, want 3 (two by Memax, one by the person)", n)
	}
}
