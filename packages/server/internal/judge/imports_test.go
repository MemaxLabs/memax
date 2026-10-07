package judge_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// importFiles uploads statements as an import, each from its own file:line.
func (f *fixture) importFiles(owner, space uuid.UUID, lines ...[2]string) (*ledger.ImportResult, []uuid.UUID) {
	f.t.Helper()
	items := make([]ledger.ImportItem, len(lines))
	for i, l := range lines {
		items[i] = ledger.ImportItem{Key: l[0], Location: ledger.ImportRepository,
			NewMemory: ledger.NewMemory{Statement: l[1], Section: ledger.SectionConventions,
				Sources: []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: l[0]}}}}
	}
	res, err := f.l.Import(f.ctx, ledger.ImportRequest{
		Meta:    ledger.Meta{Actor: person(owner), Scope: f.scope(owner), Via: policy.ViaCLI, IdempotencyKey: uuid.NewString()},
		SpaceID: space, Items: items,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	ids := make([]uuid.UUID, len(res.Items))
	for i, it := range res.Items {
		ids[i] = it.Memory.ID
	}
	return res, ids
}

var statementRef = regexp.MustCompile(`<statement id="(M-\d+)"[^>]*>\n([^\n]*)`)

// conflictOracle answers the import check: the statements whose text
// contains one of each pair of words disagree.
func conflictOracle(subject string, confidence float64, words ...string) func(judge.Call, int) (string, error) {
	return func(c judge.Call, _ int) (string, error) {
		var members []string
		for _, m := range statementRef.FindAllStringSubmatch(c.Prompt, -1) {
			for _, w := range words {
				if strings.Contains(m[2], w) {
					members = append(members, m[1])
					break
				}
			}
		}
		out := map[string]any{"conflicts": []any{}}
		if len(members) >= 2 {
			out["conflicts"] = []any{map[string]any{"subject": subject, "members": members, "confidence": confidence,
				"rationale": "They name different commands.", "suggestion": ""}}
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	}
}

func TestImportCheckFlagsDisagreements(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	res, ids := f.importFiles(zz, sp,
		[2]string{"CLAUDE.md:12", "Run tests with `pnpm test`."},
		[2]string{"AGENTS.md:8", "Run `npm run test` before committing."},
		[2]string{"CLAUDE.md:4", "Use pnpm workspaces."},
	)
	model := &fakeModel{answer: conflictOracle("Test command", 0.93, "pnpm test", "npm run test")}
	j := withModel(f, model, judge.Config{Primary: judge.Tier{Model: "primary"}})
	run, err := j.CheckImport(f.ctx, ledger.JudgeImportArgs{ImportID: res.Import.ID, SpaceID: sp})
	if err != nil {
		t.Fatal(err)
	}
	if run.State != ledger.CheckChecked || run.Conflicts != 1 || run.Calls != 1 {
		t.Fatalf("run %+v", run)
	}
	// The prompt holds the statements as data, with where they came from.
	if p := model.calls[0].Prompt; !strings.Contains(p, `from="CLAUDE.md:12"`) || !strings.Contains(p, "Run `npm run test` before committing.") {
		t.Errorf("prompt: %s", p)
	}
	v, err := f.l.GetImport(f.ctx, f.scope(zz), sp, res.Import.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Import.Check.State != ledger.CheckChecked || v.Import.Check.Tier != ledger.TierPrimary || len(v.Conflicts) != 1 {
		t.Fatalf("view: %+v %+v", v.Import.Check, v.Conflicts)
	}
	c := v.Conflicts[0]
	if c.Subject != "Test command" || len(c.Members) != 2 || c.Members[0].ID != ids[0] || c.Members[1].ID != ids[1] {
		t.Errorf("conflict %+v", c)
	}
	for _, im := range v.Memories {
		flagged := im.Memory.Flags.Has(lifecycle.Conflict)
		if want := im.Memory.ID != ids[2]; flagged != want {
			t.Errorf("%s flagged %v, want %v", im.Memory.Ref, flagged, want)
		}
	}
	// A retried job changes nothing.
	again, err := j.CheckImport(f.ctx, ledger.JudgeImportArgs{ImportID: res.Import.ID, SpaceID: sp})
	if err != nil || !again.Skipped {
		t.Errorf("second run %+v %v", again, err)
	}
	if len(model.calls) != 1 {
		t.Errorf("%d calls, want 1", len(model.calls))
	}
}

func TestImportCheckIsCarefulAndDegrades(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	lines := [][2]string{{"CLAUDE.md:12", "Run tests with `pnpm test`."}, {"AGENTS.md:8", "Run `npm run test` before committing."}}

	// Below the bar: nothing is flagged. 0.7 clears the judge's Contradicts
	// threshold but not the import check's own bar.
	res, _ := f.importFiles(zz, sp, lines...)
	low := withModel(f, &fakeModel{answer: conflictOracle("Test command", 0.7, "pnpm test", "npm run test")},
		judge.Config{Primary: judge.Tier{Model: "primary"}})
	run, err := low.CheckImport(f.ctx, ledger.JudgeImportArgs{ImportID: res.Import.ID, SpaceID: sp})
	if err != nil || run.State != ledger.CheckChecked || run.Conflicts != 0 {
		t.Fatalf("low confidence: %+v %v", run, err)
	}

	// No model: only stage 0 runs, and the import says so.
	sp2 := f.space(zz, "other")
	res2, _ := f.importFiles(zz, sp2, lines...)
	run, err = stage0Only(f).CheckImport(f.ctx, ledger.JudgeImportArgs{ImportID: res2.Import.ID, SpaceID: sp2})
	if err != nil || run.State != ledger.CheckNoModel {
		t.Fatalf("no model: %+v %v", run, err)
	}

	// A model that never answers: failed, nothing flagged, the job doesn't error.
	sp3 := f.space(zz, "third")
	res3, ids3 := f.importFiles(zz, sp3, lines...)
	broken := &fakeModel{answer: func(judge.Call, int) (string, error) { return "", errors.New("upstream 529") }}
	run, err = withModel(f, broken, judge.Config{Primary: judge.Tier{Model: "p"}, Fallback: judge.Tier{Model: "f", Strict: true}}).
		CheckImport(f.ctx, ledger.JudgeImportArgs{ImportID: res3.Import.ID, SpaceID: sp3})
	if err != nil || run.State != ledger.CheckFailed || run.Calls != 4 {
		t.Fatalf("failing model: %+v %v", run, err)
	}
	m, err := f.l.GetMemory(f.ctx, f.scope(zz), ids3[0].String())
	if err != nil || m.Flags.Has(lifecycle.Conflict) {
		t.Errorf("flagged after a failed check: %v %v", m.Flags, err)
	}

	// One proposal: nothing to compare.
	sp4 := f.space(zz, "fourth")
	res4, _ := f.importFiles(zz, sp4, lines[0])
	run, err = stage0Only(f).CheckImport(f.ctx, ledger.JudgeImportArgs{ImportID: res4.Import.ID, SpaceID: sp4})
	if err != nil || run.State != ledger.CheckSkipped {
		t.Fatalf("one proposal: %+v %v", run, err)
	}
}

// The primary leaves suggestion out when it has none; at temperature 0 it
// does so again on a retry, so the answer is taken as it is (import eval,
// Oct 7, 2026).
func TestImportCheckTakesAnAnswerWithoutASuggestion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	res, _ := f.importFiles(zz, sp, [2]string{"CLAUDE.md:1", "Use pnpm."}, [2]string{"AGENTS.md:1", "Use yarn."})
	model := &fakeModel{answer: func(c judge.Call, _ int) (string, error) {
		refs := statementRef.FindAllStringSubmatch(c.Prompt, -1)
		return fmt.Sprintf(`{"conflicts":[{"subject":"Package manager","members":[%q,%q],"confidence":0.95,"rationale":"pnpm or yarn."}]}`,
			refs[0][1], refs[1][1]), nil
	}}
	run, err := withModel(f, model, judge.Config{Primary: judge.Tier{Model: "primary"}, Fallback: judge.Tier{Model: "f", Strict: true}}).
		CheckImport(f.ctx, ledger.JudgeImportArgs{ImportID: res.Import.ID, SpaceID: sp})
	if err != nil || run.Conflicts != 1 || run.Calls != 1 {
		t.Fatalf("answer without a suggestion: %+v %v (tiers %v)", run, err, model.tiers())
	}
}

func TestImportCheckIgnoresWhatTheModelInvents(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	res, _ := f.importFiles(zz, sp, [2]string{"CLAUDE.md:1", "Use pnpm."}, [2]string{"AGENTS.md:1", "Use yarn."})
	model := &fakeModel{answer: func(judge.Call, int) (string, error) {
		return `{"conflicts":[{"subject":"Package manager","members":["M-9999","M-9998"],"confidence":0.99,"rationale":"x","suggestion":""}]}`, nil
	}}
	run, err := withModel(f, model, judge.Config{Primary: judge.Tier{Model: "primary"}}).
		CheckImport(f.ctx, ledger.JudgeImportArgs{ImportID: res.Import.ID, SpaceID: sp})
	if err != nil || run.Conflicts != 0 || run.State != ledger.CheckChecked {
		t.Fatalf("invented members: %+v %v", run, err)
	}
}
