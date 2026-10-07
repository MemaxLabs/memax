package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// fileItem is a statement read from a repository file.
func fileItem(key, ref, statement string) ledger.ImportItem {
	return ledger.ImportItem{
		Key: key, Ref: ref, Location: ledger.ImportRepository,
		NewMemory: ledger.NewMemory{Statement: statement, Section: ledger.SectionConventions,
			Sources: []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: ref, Locator: json.RawMessage(`{"path":"` + ref + `"}`)}}},
	}
}

func (f *fixture) importAs(actor ledger.Actor, scope ledger.Scope, space uuid.UUID, key string, items ...ledger.ImportItem) *ledger.ImportResult {
	f.t.Helper()
	res, err := f.l.Import(context.Background(), ledger.ImportRequest{
		Meta:    ledger.Meta{Actor: actor, Scope: scope, Via: policy.ViaCLI, IdempotencyKey: key},
		SpaceID: space, Client: "memax-cli test",
		Files:   []ledger.ImportFile{{Path: "CLAUDE.md", Kind: "claude_md", Agent: "claude-code", Location: ledger.ImportRepository, Statements: len(items)}},
		Skipped: []ledger.ImportSkip{{Ref: "CLAUDE.md:31", Reason: ledger.SkipSecret, Detail: "GitHub token"}},
		Items:   items,
	})
	if err != nil {
		f.t.Fatalf("Import: %v", err)
	}
	return res
}

func outcomes(r *ledger.ImportResult) []ledger.ImportOutcome {
	out := make([]ledger.ImportOutcome, len(r.Items))
	for i, it := range r.Items {
		out[i] = it.Outcome
	}
	return out
}

func TestImportProposesFoldsAndSkips(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	kept := f.remember(zz, sp, "Background jobs use River.")

	res := f.importAs(person(zz), f.scope(zz), sp, "init-1",
		fileItem("a", "CLAUDE.md:4", "pnpm workspaces only. Never run npm install at the root."),
		fileItem("b", "AGENTS.md:3", "pnpm workspaces only; never run npm install at the root"),
		fileItem("c", "CLAUDE.md:12", "Run tests with `pnpm test`."),
		fileItem("d", "AGENTS.md:8", "Background jobs use River"),
		fileItem("e", "AGENTS.md:9", "Run npm run test before committing."),
	)
	want := []ledger.ImportOutcome{ledger.ImportProposed, ledger.ImportFolded, ledger.ImportProposed, ledger.ImportExisting, ledger.ImportProposed}
	if got := outcomes(res); !slices.Equal(got, want) {
		t.Fatalf("outcomes %v, want %v", got, want)
	}
	if res.Items[1].FoldedInto != "a" || res.Items[1].Memory.ID != res.Items[0].Memory.ID {
		t.Errorf("b folded into %q (%v), want a's proposal", res.Items[1].FoldedInto, res.Items[1].Memory)
	}
	if res.Items[3].Memory.ID != kept.ID || res.Items[3].Lifecycle != "kept" {
		t.Errorf("d matched %v (%s), want the kept %s", res.Items[3].Memory, res.Items[3].Lifecycle, kept.Ref)
	}
	if c := res.Import.Counts; c.Items != 5 || c.Proposed != 3 || c.Folded != 1 || c.Existing != 1 || c.Refused != 0 {
		t.Errorf("counts %+v", c)
	}
	if len(res.Import.Skipped) != 1 || res.Import.Skipped[0].Detail != "GitHub token" {
		t.Errorf("skipped %+v", res.Import.Skipped)
	}

	// The fold cites both files; every proposal arrived through the import
	// surface, at the repository class, with its own receipt.
	m, err := f.l.GetMemory(context.Background(), f.scope(zz), res.Items[0].Memory.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if m.Lifecycle != lifecycle.Proposed || m.Trust != policy.TrustRepository || len(m.Sources) != 2 {
		t.Fatalf("folded proposal: %s, trust %s, %d sources", m.Lifecycle, m.Trust, len(m.Sources))
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND via = 'import' AND action = 'proposed'`, sp); n != 3 {
		t.Errorf("%d import receipts, want 3 (one per proposal)", n)
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'judge_import'`); n != 1 {
		t.Errorf("%d judge_import jobs, want 1", n)
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'judge_proposal'`); n != 3 {
		t.Errorf("%d judge_proposal jobs, want 3", n)
	}

	// The same key resumes the same import and writes nothing again.
	again := f.importAs(person(zz), f.scope(zz), sp, "init-1",
		fileItem("a", "CLAUDE.md:4", "pnpm workspaces only. Never run npm install at the root."),
		fileItem("b", "AGENTS.md:3", "pnpm workspaces only; never run npm install at the root"),
		fileItem("c", "CLAUDE.md:12", "Run tests with `pnpm test`."),
		fileItem("d", "AGENTS.md:8", "Background jobs use River"),
		fileItem("e", "AGENTS.md:9", "Run npm run test before committing."),
	)
	if !again.Replayed || again.Import.ID != res.Import.ID || !slices.Equal(outcomes(again), want) {
		t.Errorf("replay: replayed=%v id=%v outcomes=%v", again.Replayed, again.Import.ID, outcomes(again))
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND via = 'import'`, sp); n != 3 {
		t.Errorf("after the replay, %d import receipts, want 3", n)
	}

	// Running init again (a new key, line numbers moved) proposes nothing.
	rerun := f.importAs(person(zz), f.scope(zz), sp, "init-2",
		fileItem("a", "CLAUDE.md:5", "pnpm workspaces only. Never run npm install at the root."),
		fileItem("c", "CLAUDE.md:13", "Run tests with `pnpm test`"),
	)
	if got := outcomes(rerun); !slices.Equal(got, []ledger.ImportOutcome{ledger.ImportExisting, ledger.ImportExisting}) {
		t.Errorf("re-run outcomes %v, want both existing", got)
	}

	// The same key for another upload is refused.
	_, err = f.l.Import(context.Background(), ledger.ImportRequest{
		Meta:    ledger.Meta{Actor: person(zz), Scope: f.scope(zz), Via: policy.ViaCLI, IdempotencyKey: "init-1"},
		SpaceID: sp, Items: []ledger.ImportItem{fileItem("z", "GEMINI.md:1", "Something else entirely.")},
	})
	if !errors.Is(err, ledger.ErrIdempotencyKeyReused) {
		t.Errorf("reused key: %v, want ErrIdempotencyKeyReused", err)
	}
}

func TestImportCapsRepositoryTrustAndKeepsExternal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	claimed := fileItem("a", "CLAUDE.md:1", "Use pnpm.")
	claimed.Sources[0].Trust = policy.TrustPerson // a repository file is never one person's word
	branch := fileItem("b", "AGENTS.md:2", "Deploy with the staging script.")
	branch.Sources[0].Trust = policy.TrustExternal // only on a PR branch
	home := fileItem("c", "~/.claude/CLAUDE.md:3", "I prefer short answers.")
	home.Location = ledger.ImportHome
	home.Sources[0].Trust = policy.TrustPerson
	res := f.importAs(person(zz), f.scope(zz), sp, "init", claimed, branch, home)
	trust := func(i int) (policy.Trust, bool) {
		m, err := f.l.GetMemory(context.Background(), f.scope(zz), res.Items[i].Memory.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		return m.Trust, m.Trust.External()
	}
	if tr, _ := trust(0); tr != policy.TrustRepository {
		t.Errorf("repository file claimed person: trust %s, want repository", tr)
	}
	if tr, ext := trust(1); tr != policy.TrustExternal || !ext {
		t.Errorf("branch line: trust %s, want external (quarantined)", tr)
	}
	if tr, _ := trust(2); tr != policy.TrustPerson {
		t.Errorf("the person's own file: trust %s, want person", tr)
	}
}

func TestImportRefusesSecretsAndReadOnlyAgents(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	res := f.importAs(person(zz), f.scope(zz), sp, "init",
		fileItem("a", "CLAUDE.md:1", "Use pnpm."),
		fileItem("b", "CLAUDE.md:2", "The token is ghp_abcdefghijklmnopqrstuvwxyz0123456789."),
	)
	if got := outcomes(res); !slices.Equal(got, []ledger.ImportOutcome{ledger.ImportProposed, ledger.ImportRefused}) {
		t.Fatalf("outcomes %v", got)
	}
	if p := res.Items[1].Policy; p == nil || p.Code != policy.CodeSecret {
		t.Errorf("refusal %+v, want secret_detected", p)
	}
	if res.Import.Counts.Refused != 1 {
		t.Errorf("refused count %d", res.Import.Counts.Refused)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_versions WHERE statement LIKE '%ghp_%'`); n != 0 {
		t.Errorf("the credential was stored %d times", n)
	}

	// A read-only agent writes nothing, the import included.
	agent := agentFor(policy.AutonomyRead)
	scope := f.scope(zz).WithConnection(nil, policy.AutonomyRead)
	out := f.importAs(agent, scope, sp, "agent", fileItem("a", "CLAUDE.md:1", "Use pnpm."))
	if out.Refused == nil || out.Import != nil {
		t.Fatalf("read-only agent's import: %+v", out)
	}
}

// flagGroup records an import check that found one disagreement.
func (f *fixture) checkImport(space, importID uuid.UUID, subject, suggestion string, members ...uuid.UUID) ledger.Result {
	f.t.Helper()
	scope, err := f.l.SpaceScope(context.Background(), space)
	if err != nil {
		f.t.Fatal(err)
	}
	conf := 0.95
	return f.apply(&ledger.RecordImportCheck{
		Meta:    ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax, Name: "Memax"}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: "judge-import:" + importID.String()},
		SpaceID: space, Import: importID, State: ledger.CheckChecked, Tier: ledger.TierPrimary, Model: "fake",
		Conflicts: []ledger.ImportConflictInput{{Members: members, Subject: subject, Suggestion: suggestion, Confidence: &conf}},
	})
}

func TestImportConflictsFlagAndSettleAsAGroup(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	res := f.importAs(person(zz), f.scope(zz), sp, "init",
		fileItem("a", "CLAUDE.md:12", "Run tests with `pnpm test`."),
		fileItem("b", "AGENTS.md:8", "Run `npm run test` before committing."),
		fileItem("c", "testing.mdc:3", "Use `pnpm vitest run` for unit tests."),
		fileItem("d", "CLAUDE.md:4", "Use pnpm workspaces."),
	)
	a, b, c, d := res.Items[0].Memory, res.Items[1].Memory, res.Items[2].Memory, res.Items[3].Memory
	outside := f.remember(zz, sp, "Unrelated kept memory.")
	out := f.checkImport(sp, res.Import.ID, "Test command", "", a.ID, b.ID, c.ID, outside.ID)
	if len(out.Receipts) != 3 {
		t.Fatalf("%d flagged receipts, want 3 (the outside memory isn't the import's)", len(out.Receipts))
	}
	// Recording again (a retried job) changes nothing.
	if again := f.checkImport(sp, res.Import.ID, "Test command", "", a.ID, b.ID, c.ID, outside.ID); !again.Replayed || len(again.Receipts) != 3 {
		t.Errorf("second check: replayed %v, %d receipts", again.Replayed, len(again.Receipts))
	}

	ctx := context.Background()
	// The judge looks at each proposal too: d has nothing to compare
	// with, and a also contradicts a decision in force (already flagged by
	// the import check, it gets the second link).
	dm, err := f.l.GetMemory(ctx, f.scope(zz), d.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	f.judgeAs(sp, dm, ledger.OutcomeNone, nil, ledger.JudgeProposal)
	in := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
		NewMemory: decisionIn(sp, "Tests run with make test.", "test command")}).Memory
	am, err := f.l.GetMemory(ctx, f.scope(zz), a.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if r := f.judgeAs(sp, am, ledger.OutcomeFlagged, in, ledger.JudgeProposal); r.Memory.Judge.Outcome != ledger.OutcomeFlagged {
		t.Fatalf("judge flag on an import-flagged proposal: %+v", r.Memory.Judge)
	}
	v, err := f.l.GetImport(ctx, f.scope(zz), sp, res.Import.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Import.Check.State != ledger.CheckChecked || len(v.Conflicts) != 1 || len(v.Conflicts[0].Members) != 3 {
		t.Fatalf("view: check %s, conflicts %+v", v.Import.Check.State, v.Conflicts)
	}
	bulk := map[uuid.UUID]ledger.ImportMemory{}
	for _, im := range v.Memories {
		bulk[im.Memory.ID] = im
	}
	if bulk[a.ID].Bulk || bulk[a.ID].Held != ledger.HeldConflict || bulk[a.ID].Conflict != 1 {
		t.Errorf("a: %+v, want held for the conflict", bulk[a.ID])
	}
	if !bulk[d.ID].Bulk {
		t.Errorf("d: %+v, want bulk", bulk[d.ID])
	}
	for _, id := range []uuid.UUID{a.ID, b.ID, c.ID} {
		if !bulk[id].Memory.Flags.Has(lifecycle.Conflict) {
			t.Errorf("%s isn't flagged", bulk[id].Memory.Ref)
		}
	}

	// Keeping one side, or settling two at a time, is refused.
	_, err = f.l.Apply(ctx, &ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Memory: a.ID.String()})
	if !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("keep a flagged member: %v", err)
	}
	_, err = f.l.Apply(ctx, &ledger.ResolveConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Memory: b.ID.String(), Choice: ledger.ChooseThis})
	if !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("settle a pair of the group: %v", err)
	}

	// a also contradicts the decision in force, so it can't win yet.
	_, err = f.l.Apply(ctx, &ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
		SpaceID: sp, Import: res.Import.ID, N: 1, Choice: ledger.ChooseKeepOne, Keep: a.Ref})
	var ic *ledger.InConflictError
	if !errors.As(err, &ic) || ic.With != in.Ref {
		t.Fatalf("keep a, still in conflict with %s: %v", in.Ref, err)
	}
	settled := f.apply(&ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
		SpaceID: sp, Import: res.Import.ID, N: 1, Choice: ledger.ChooseKeepOne, Keep: b.Ref})
	states := map[uuid.UUID]ledger.Memory{}
	for _, m := range settled.Memories {
		states[m.ID] = m
	}
	if states[b.ID].Lifecycle != lifecycle.Kept || states[a.ID].Lifecycle != lifecycle.Rejected || states[c.ID].Lifecycle != lifecycle.Rejected {
		t.Errorf("after keep_one: a %s, b %s, c %s", states[a.ID].Lifecycle, states[b.ID].Lifecycle, states[c.ID].Lifecycle)
	}
	if states[b.ID].Flags.Has(lifecycle.Conflict) {
		t.Error("the kept side is still flagged")
	}
	if rc := settled.Receipts[0]; rc.Assurance != "" || rc.Action != ledger.ActionResolved {
		t.Errorf("first receipt %+v", rc)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_links WHERE kind = 'conflicts_with' AND ended_at IS NULL AND space_id = $1`, sp); n != 0 {
		t.Errorf("%d conflict links still open", n)
	}
	v, _ = f.l.GetImport(ctx, f.scope(zz), sp, res.Import.ID)
	if v.Conflicts[0].State != "settled" || v.Conflicts[0].Choice != ledger.ChooseKeepOne || v.Conflicts[0].Chosen.ID != b.ID {
		t.Errorf("group after settling: %+v", v.Conflicts[0])
	}
	_, err = f.l.Apply(ctx, &ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
		SpaceID: sp, Import: res.Import.ID, N: 1, Choice: ledger.ChooseKeepAll})
	if !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("settling twice: %v", err)
	}
}

func TestImportConflictChoices(t *testing.T) {
	t.Parallel()
	for _, choice := range []ledger.ImportChoice{ledger.ChooseKeepAll, ledger.ChooseLeaveOpen, ledger.ChooseKeepSuggestion} {
		t.Run(string(choice), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			zz := f.user("zz")
			sp := f.space(zz, policy.SpaceProject, "memax-v2")
			res := f.importAs(person(zz), f.scope(zz), sp, "init",
				fileItem("a", "CLAUDE.md:4", "pnpm workspaces only."),
				fileItem("b", "codex memory:2", "Use npm for the scripts in /tools."))
			a, b := res.Items[0].Memory, res.Items[1].Memory
			f.checkImport(sp, res.Import.ID, "Package manager", "pnpm at the root, npm inside /tools.", a.ID, b.ID)
			out := f.apply(&ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
				SpaceID: sp, Import: res.Import.ID, N: 1, Choice: choice})
			got := map[uuid.UUID]ledger.Memory{}
			for _, m := range out.Memories {
				got[m.ID] = m
			}
			switch choice {
			case ledger.ChooseKeepAll:
				if got[a.ID].Lifecycle != lifecycle.Kept || got[b.ID].Lifecycle != lifecycle.Kept {
					t.Errorf("keep_all: %s, %s", got[a.ID].Lifecycle, got[b.ID].Lifecycle)
				}
			case ledger.ChooseLeaveOpen:
				for _, id := range []uuid.UUID{a.ID, b.ID} {
					if got[id].Lifecycle != lifecycle.Kept || got[id].Section != ledger.SectionOpenQuestion {
						t.Errorf("leave_open: %s %s in %s", got[id].Ref, got[id].Lifecycle, got[id].Section)
					}
				}
			case ledger.ChooseKeepSuggestion:
				if len(out.Memories) != 3 {
					t.Fatalf("%d memories, want the two members and the suggestion", len(out.Memories))
				}
				var kept *ledger.Memory
				for i := range out.Memories {
					if m := out.Memories[i]; m.ID != a.ID && m.ID != b.ID {
						kept = &out.Memories[i]
					}
				}
				if kept == nil || kept.Lifecycle != lifecycle.Kept || kept.Statement != "pnpm at the root, npm inside /tools." {
					t.Fatalf("suggestion kept: %+v", kept)
				}
				m, err := f.l.GetMemory(context.Background(), f.scope(zz), kept.ID.String())
				if err != nil {
					t.Fatal(err)
				}
				if len(m.Sources) != 2 || m.Trust != policy.TrustRepository {
					t.Errorf("suggestion: %d sources, trust %s; want both files at repository", len(m.Sources), m.Trust)
				}
				if got[a.ID].Lifecycle != lifecycle.Rejected || got[b.ID].Lifecycle != lifecycle.Rejected {
					t.Errorf("members after the suggestion: %s, %s", got[a.ID].Lifecycle, got[b.ID].Lifecycle)
				}
			}
		})
	}
}

func TestImportConflictsFollowKeepRules(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	team := f.space(zz, policy.SpaceTeam, "memax-team")
	decision := func(key, ref, statement string) ledger.ImportItem {
		it := fileItem(key, ref, statement)
		it.Kind, it.Section = ledger.KindDecision, ledger.SectionDecisions
		return it
	}
	res := f.importAs(person(zz), f.scope(zz), team, "init",
		decision("a", "AGENTS.md:21", "The API runs on Fly.io."),
		decision("b", "deploy.mdc:2", "The API deploys to Railway."))
	f.checkImport(team, res.Import.ID, "Where the API deploys", "", res.Items[0].Memory.ID, res.Items[1].Memory.ID)
	// D15: decisions in a team space need a person on the web.
	cli := f.apply(&ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
		SpaceID: team, Import: res.Import.ID, N: 1, Choice: ledger.ChooseKeepOne, Keep: res.Items[0].Memory.Ref})
	if cli.Outcome != ledger.OutcomeRefused || cli.Policy.Code != policy.CodeDecisionNeedsWeb {
		t.Fatalf("CLI settle of a team decision: %s %s", cli.Outcome, cli.Policy.Code)
	}
	web := f.apply(&ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
		SpaceID: team, Import: res.Import.ID, N: 1, Choice: ledger.ChooseKeepOne, Keep: res.Items[0].Memory.Ref})
	if web.Outcome != ledger.OutcomeApplied {
		t.Fatalf("web settle: %s %s", web.Outcome, web.Policy.Code)
	}
	// A quarantined member is kept only on the web.
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	ext := fileItem("x", "codex memory:4", "The MCP spec requires OAuth 2.1 for remote servers.")
	ext.Sources = append(ext.Sources, ledger.SourceInput{Kind: ledger.SourceURL, Ref: "modelcontextprotocol.io", URI: "https://modelcontextprotocol.io"})
	res = f.importAs(person(zz), f.scope(zz), sp, "init-2", ext, fileItem("y", "CLAUDE.md:9", "Remote MCP servers use API keys."))
	f.checkImport(sp, res.Import.ID, "Remote MCP auth", "", res.Items[0].Memory.ID, res.Items[1].Memory.ID)
	out := f.apply(&ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
		SpaceID: sp, Import: res.Import.ID, N: 1, Choice: ledger.ChooseKeepOne, Keep: res.Items[0].Memory.Ref})
	if out.Outcome != ledger.OutcomeRefused || out.Policy.Code != policy.CodeExternalNeedsReview {
		t.Errorf("CLI keep of a quarantined member: %s %s", out.Outcome, out.Policy.Code)
	}
	ok := f.apply(&ledger.SettleImportConflict{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
		SpaceID: sp, Import: res.Import.ID, N: 1, Choice: ledger.ChooseKeepOne, Keep: res.Items[1].Memory.Ref})
	if ok.Outcome != ledger.OutcomeApplied {
		t.Errorf("CLI keep of the repository side: %s %s", ok.Outcome, ok.Policy.Code)
	}
}

func TestImportViewProgressAndList(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		r := f.importAs(person(zz), f.scope(zz), sp, fmt.Sprintf("init-%d", i), fileItem("a", "CLAUDE.md:1", fmt.Sprintf("Statement number %d is about %c.", i, 'x'+i)))
		ids = append(ids, r.Import.ID)
	}
	ctx := context.Background()
	v, err := f.l.GetImport(ctx, f.scope(zz), sp, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	// No judge has run: the proposal is working, and the check pending.
	if v.Progress.Ready || v.Progress.Working != 1 || v.Import.Check.State != ledger.CheckPending || v.Memories[0].Held != ledger.HeldChecking {
		t.Errorf("before the judge: progress %+v, check %s, held %q", v.Progress, v.Import.Check.State, v.Memories[0].Held)
	}
	page, err := f.l.ListImports(ctx, f.scope(zz), sp, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || !page.HasMore || page.Items[0].ID != ids[2] {
		t.Fatalf("first page: %d items, more %v", len(page.Items), page.HasMore)
	}
	next, err := f.l.ListImports(ctx, f.scope(zz), sp, page.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.HasMore || next.Items[0].ID != ids[0] {
		t.Errorf("second page: %+v", next)
	}
	// Another person's space doesn't show.
	jy := f.user("jy")
	if _, err := f.l.GetImport(ctx, f.scope(jy), sp, ids[0]); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a stranger reads the import: %v", err)
	}
}

func TestCreateAndSwitchSpaces(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	ctx := context.Background()
	sp, _, err := f.l.CreateSpace(ctx, person(zz), ledger.NewSpace{Name: "memax", Repository: "MemaxLabs/memax", Key: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	// "memax" is reserved, so the first free slug after it.
	if sp.Slug != "memax-2" || sp.Kind != policy.SpaceProject || sp.Role != policy.RoleOwner || sp.V2EnabledAt == nil ||
		sp.Repository != "MemaxLabs/memax" || sp.TenantID != zz {
		t.Fatalf("created %+v", sp)
	}
	// The same key finds the same space; the same key for another doesn't.
	same, replayed, err := f.l.CreateSpace(ctx, person(zz), ledger.NewSpace{Name: "memax", Repository: "MemaxLabs/memax", Key: "k1"})
	if err != nil || !replayed || same.ID != sp.ID {
		t.Fatalf("retry: %v replayed=%v %v", same, replayed, err)
	}
	if _, _, err := f.l.CreateSpace(ctx, person(zz), ledger.NewSpace{Name: "other", Key: "k1"}); !errors.Is(err, ledger.ErrIdempotencyKeyReused) {
		t.Errorf("reused key: %v", err)
	}
	again, _, err := f.l.CreateSpace(ctx, person(zz), ledger.NewSpace{Name: "memax"})
	if err != nil || again.Slug != "memax-3" {
		t.Fatalf("second: %v %v", again, err)
	}
	if _, _, err := f.l.CreateSpace(ctx, person(zz), ledger.NewSpace{Name: "x", Slug: "memax-2"}); !errors.Is(err, ledger.ErrSlugTaken) {
		t.Errorf("taken slug: %v", err)
	}
	if _, _, err := f.l.CreateSpace(ctx, person(zz), ledger.NewSpace{Name: "x", Slug: "settings"}); !errors.Is(err, ledger.ErrSlugTaken) {
		t.Errorf("reserved slug: %v", err)
	}
	var refused *ledger.SpaceRefusedError
	if _, _, err := f.l.CreateSpace(ctx, person(zz), ledger.NewSpace{Name: "Mine", Kind: policy.SpacePersonal}); !errors.As(err, &refused) {
		t.Errorf("a second personal space: %v", err)
	}
	if _, _, err := f.l.CreateSpace(ctx, agentFor(policy.AutonomyWrite), ledger.NewSpace{Name: "agents"}); !errors.As(err, &refused) {
		t.Errorf("an agent creates a space: %v", err)
	}
	// The person's scope has it now, with the person as owner.
	if g, ok := f.scope(zz).Grant(sp.ID); !ok || g.Role != policy.RoleOwner {
		t.Errorf("scope grant %+v %v", g, ok)
	}

	// Switching: an empty space switches; one with V1 memories doesn't.
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	got, err := f.l.SwitchSpace(ctx, person(zz), f.scope(zz), personal)
	if err != nil || got.V2EnabledAt == nil {
		t.Fatalf("switch empty: %v %v", got, err)
	}
	busy := f.space(zz, policy.SpaceProject, "old")
	f.exec(`INSERT INTO memories (hub_id, owner_id, title, content) VALUES ($1, $2, 'note', 'a V1 memory')`, busy, zz)
	var notes *ledger.SpaceHasNotesError
	if _, err := f.l.SwitchSpace(ctx, person(zz), f.scope(zz), busy); !errors.As(err, &notes) || notes.Notes != 1 {
		t.Errorf("switch with notes: %v", err)
	}
	jy := f.user("jy")
	f.join(busy, jy, "contributor")
	if _, err := f.l.SwitchSpace(ctx, person(jy), f.scope(jy), personal); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a stranger switches: %v", err)
	}
	if _, err := f.l.SwitchSpace(ctx, person(jy), f.scope(jy), busy); !errors.As(err, &refused) {
		t.Errorf("a member switches: %v", err)
	}
}

// Cursor and Gemini CLI start at Read when they connect (the Connect
// board); every other agent at the space's default.
func TestStartAutonomy(t *testing.T) {
	t.Parallel()
	for kind, want := range map[ledger.AgentKind]policy.Autonomy{
		ledger.AgentCursor: policy.AutonomyRead, ledger.AgentGeminiCLI: policy.AutonomyRead,
		ledger.AgentClaudeCode: "", ledger.AgentCodex: "", ledger.AgentOther: "",
	} {
		if got := kind.StartAutonomy(); got != want {
			t.Errorf("%s starts at %q, want %q", kind, got, want)
		}
	}
}
