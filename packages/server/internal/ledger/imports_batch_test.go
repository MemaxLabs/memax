package ledger_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// An import, written in one transaction, answers, writes and records what
// it did one statement at a time (ledger.ImportOneByOne, the way it ran
// before): each case runs on two databases built the same way, then again
// with the same key, and a Remember after it gets the same display ID in
// both (the block of IDs it took is given back whole).

type importWorld struct {
	f      *fixture
	zz     uuid.UUID
	space  uuid.UUID
	agents map[policy.Autonomy]ledger.Actor
}

// agent is the world's agent at a level, the same one every time.
func (w *importWorld) agent(level policy.Autonomy) ledger.Actor {
	if a, ok := w.agents[level]; ok {
		return a
	}
	w.agents[level] = agentFor(level)
	return w.agents[level]
}

func newImportWorld(t *testing.T) *importWorld {
	t.Helper()
	f := newCompileFixture(t)
	zz := f.user("zz")
	w := &importWorld{f: f, zz: zz, space: f.space(zz, policy.SpaceProject, "memax-v2"), agents: map[policy.Autonomy]ledger.Actor{}}
	f.remember(zz, w.space, "Background jobs use River.")         // M-0001
	f.remember(zz, w.space, "Deploys go through Fly in sjc.")     // M-0002
	f.remember(zz, w.space, "Releases are cut on Tuesdays only.") // M-0003
	return w
}

func homeItem(key, ref, statement string) ledger.ImportItem {
	return ledger.ImportItem{
		Key: key, Ref: ref, Location: ledger.ImportHome,
		NewMemory: ledger.NewMemory{Statement: statement, Section: ledger.SectionPreferences,
			Sources: []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: ref, Trust: policy.TrustPerson}}},
	}
}

type importCase struct {
	name  string
	actor func(w *importWorld) ledger.Actor
	items []ledger.ImportItem
}

func TestImportMatchesOneByOne(t *testing.T) {
	t.Parallel()
	in := time.Date(2027, 1, 4, 0, 0, 0, 0, time.UTC)
	decision := fileItem("d", "AGENTS.md:20", "Deploy previews to Fly.io machines.")
	decision.Section, decision.Kind = ledger.SectionDecisions, ledger.KindDecision
	decision.Decision = &ledger.DecisionFields{Area: "deploy target", Why: "One host for every environment."}
	decision.Applies = json.RawMessage(`{"paths": ["packages/web/**"]}`)
	decision.StaleAfter = &in
	cited := fileItem("m", "AGENTS.md:21", "Background jobs retry five times on River.")
	cited.Sources = append(cited.Sources, ledger.SourceInput{Kind: ledger.SourceMemory, Ref: "M-0001", Quote: "Background jobs use River."})
	hidden := fileItem("h", "AGENTS.md:22", "Lint with eslint before every commit.")
	hidden.HiddenCharacters = 2
	cases := []importCase{
		{name: "folds, repeats, the space's memories and new statements", items: []ledger.ImportItem{
			fileItem("a", "CLAUDE.md:4", "pnpm workspaces only. Never run npm install at the root."),
			fileItem("b", "AGENTS.md:3", "pnpm workspaces only; never run npm install at the root"),
			fileItem("c", "CLAUDE.md:12", "Run tests with `pnpm test`."),
			fileItem("e", "AGENTS.md:8", "Background jobs use River"),
			fileItem("f", "AGENTS.md:9", "Run npm run test before committing."),
			fileItem("g", "GEMINI.md:9", "Releases are cut on Tuesdays only"),
		}},
		{name: "a repeat that couldn't fold is the earlier statement's proposal", items: []ledger.ImportItem{
			fileItem("a", "CLAUDE.md:4", "Answer in short paragraphs, no bullet lists."),
			homeItem("b", "~/.claude/CLAUDE.md:2", "Answer in short paragraphs, no bullet lists"),
			homeItem("c", "~/.claude/CLAUDE.md:3", "Answer in short paragraphs, no bullet lists."),
			fileItem("d", "CLAUDE.md:5", "Answer in short paragraphs; no bullet lists."),
		}},
		{name: "a credential is refused and takes no number", items: []ledger.ImportItem{
			fileItem("a", "CLAUDE.md:1", "Use pnpm."),
			fileItem("b", "CLAUDE.md:2", "The token is ghp_abcdefghijklmnopqrstuvwxyz0123456789."),
			fileItem("c", "CLAUDE.md:3", "Use eslint flat config."),
		}},
		{name: "decisions, scopes, hidden characters and memories as sources", items: []ledger.ImportItem{
			decision, cited, hidden,
			homeItem("p", "~/.codex/AGENTS.md:1", "I prefer answers without emoji."),
		}},
		{name: "an agent's upload", actor: func(w *importWorld) ledger.Actor { return w.agent(policy.AutonomyPropose) },
			items: []ledger.ImportItem{fileItem("a", "CLAUDE.md:1", "Use pnpm."), fileItem("b", "CLAUDE.md:2", "Deploys go through Fly in sjc")}},
		{name: "an agent that only reads can't import", actor: func(w *importWorld) ledger.Actor { return w.agent(policy.AutonomyRead) },
			items: []ledger.ImportItem{fileItem("a", "CLAUDE.md:1", "Use pnpm.")}},
		{name: "an upload of no statements"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			batch, alone := newImportWorld(t), newImportWorld(t)
			for round := range 2 {
				got, gerr := batch.f.l.Import(context.Background(), batch.request(c, "init-1"))
				want, werr := ledger.ImportOneByOne(context.Background(), alone.f.l, alone.request(c, "init-1"), -1)
				if g, w := describeImport(got, gerr), describeImport(want, werr); g != w {
					t.Errorf("round %d:\n batch      %s\n one by one %s", round, g, w)
				}
				if round == 1 && gerr == nil && got.Refused == nil && !got.Replayed {
					t.Error("the retry didn't replay")
				}
				if g, w := importRecordOf(batch), importRecordOf(alone); g != w {
					t.Errorf("round %d: the records differ\n%s", round, lineDiff(g, w))
				}
			}
			after := batch.f.remember(batch.zz, batch.space, "Written after the import.")
			if want := alone.f.remember(alone.zz, alone.space, "Written after the import."); after.Ref != want.Ref {
				t.Errorf("the next memory is %s, want %s", after.Ref, want.Ref)
			}
		})
	}
}

func (w *importWorld) request(c importCase, key string) ledger.ImportRequest {
	actor := person(w.zz)
	if c.actor != nil {
		actor = c.actor(w)
	}
	items := make([]ledger.ImportItem, len(c.items))
	for i, it := range c.items {
		items[i] = it
		items[i].Sources = slices.Clone(it.Sources)
	}
	return ledger.ImportRequest{
		Meta:    ledger.Meta{Actor: actor, Scope: w.f.scope(w.zz), Via: policy.ViaCLI, IdempotencyKey: key, SessionRef: "init-test"},
		SpaceID: w.space, Client: "memax-cli test",
		Files:   []ledger.ImportFile{{Path: "CLAUDE.md", Kind: "claude_md", Agent: "claude-code", Location: ledger.ImportRepository, Statements: len(items)}},
		Skipped: []ledger.ImportSkip{{Ref: "CLAUDE.md:31", Reason: ledger.SkipSecret, Detail: "GitHub token"}},
		Items:   items,
	}
}

// describeImport is what an import's answer says, ids aside.
func describeImport(r *ledger.ImportResult, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	if r.Refused != nil {
		return "refused: " + r.Refused.Code
	}
	var b strings.Builder
	imp := r.Import
	fmt.Fprintf(&b, "replayed=%v counts=%+v files=%d skipped=%d origin=%s check=%s client=%q actor=%s uploaded=%v\n",
		r.Replayed, imp.Counts, len(imp.Files), len(imp.Skipped), imp.Origin, imp.Check.State, imp.Client, imp.ActorKind,
		imp.UploadedAt != nil)
	for _, it := range r.Items {
		fmt.Fprintf(&b, "  %s %d %s %s", it.Key, it.Position, it.Ref, it.Outcome)
		if it.Memory != nil {
			fmt.Fprintf(&b, " %s %s", it.Memory.Ref, it.Lifecycle)
		}
		if it.FoldedInto != "" {
			fmt.Fprintf(&b, " into %s", it.FoldedInto)
		}
		if it.Policy != nil {
			fmt.Fprintf(&b, " %s %q", it.Policy.Code, it.Policy.Message)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// importRecordOf is the space's record as rows: memories, versions,
// sources, receipts, the import and its items, the idempotency records,
// the jobs and the display-ID counter.
func importRecordOf(w *importWorld) string {
	w.f.t.Helper()
	ctx := context.Background()
	var b strings.Builder
	q := func(sql string, args ...any) {
		rows, err := w.f.pool.Query(ctx, sql, args...)
		if err != nil {
			w.f.t.Fatalf("%s: %v", firstLine(sql), err)
		}
		defer rows.Close()
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				w.f.t.Fatal(err)
			}
			fmt.Fprintln(&b, vals...)
		}
		if err := rows.Err(); err != nil {
			w.f.t.Fatal(err)
		}
	}
	q(`SELECT m.seq, m.section, m.kind, m.lifecycle, m.flags, m.trust, m.current_version, m.stream_version, m.stale_after,
	          m.conditions::text, COALESCE(m.decision::text, ''), m.scope::text, m.content_sha256, m.minhash_bands,
	          m.search::text, v.statement, v.version
	     FROM v2.memories m JOIN v2.memory_versions v ON v.memory_id = m.id
	    WHERE m.space_id = $1 ORDER BY m.seq, v.version`, w.space)
	q(`SELECT m.seq, s.kind, s.ref, COALESCE(s.uri, ''), CASE WHEN s.kind = 'memory' THEN '' ELSE s.locator::text END, s.external, s.trust_class, COALESCE(s.quote, ''),
	          COALESCE(s.content_hash, ''), r.object_ref
	     FROM v2.memory_sources ms JOIN v2.sources s ON s.id = ms.source_id JOIN v2.memories m ON m.id = ms.memory_id
	     JOIN v2.receipts r ON r.id = s.created_receipt_id
	    WHERE ms.space_id = $1 ORDER BY m.seq, s.id`, w.space)
	q(`SELECT object_ref, action, stream_version, actor_kind, via, COALESCE(session_ref, ''), COALESCE(source::text, ''),
	          COALESCE(reason, ''), COALESCE(agent, '')
	     FROM v2.receipts WHERE space_id = $1 ORDER BY seq`, w.space)
	q(`SELECT actor_kind, COALESCE(client, ''), files::text, skipped::text, items_total, uploaded_at IS NOT NULL, origin,
	          idempotency_key
	     FROM v2.imports WHERE space_id = $1 ORDER BY created_at`, w.space)
	q(`SELECT i.position, i.item_key, i.ref, i.location, i.outcome, m.seq, i.folded_into, COALESCE(i.code, ''), i.hidden_characters
	     FROM v2.import_items i LEFT JOIN v2.memories m ON m.id = i.memory_id
	    WHERE i.space_id = $1 ORDER BY i.position`, w.space)
	q(`SELECT k.idempotency_key, k.command, k.outcome, k.policy::text, m.seq, cardinality(k.receipt_ids)
	     FROM v2.command_keys k LEFT JOIN v2.memories m ON m.id = k.object_id
	    WHERE k.space_id = $1 AND k.command = 'import_statement' ORDER BY k.idempotency_key`, w.space)
	q(`SELECT j.kind, COALESCE(m.seq, 0), COALESCE(j.args ->> 'version', ''), COALESCE(j.args ->> 'mode', ''), j.queue
	     FROM river_job j LEFT JOIN v2.memories m ON m.id = (j.args ->> 'memory_id')::uuid
	    WHERE j.kind IN ('judge_proposal', 'index_memory', 'judge_import') ORDER BY j.kind, m.seq`)
	q(`SELECT prefix, next FROM v2.id_counters ORDER BY prefix`)
	return b.String()
}

// An upload a server before this one left half done (statement by
// statement, cut off after two) finishes with the same key: the two replay,
// the rest are written, and the import ends as the whole upload would have.
func TestImportFinishesAnUploadLeftHalfDone(t *testing.T) {
	t.Parallel()
	c := importCase{items: []ledger.ImportItem{
		fileItem("a", "CLAUDE.md:4", "pnpm workspaces only. Never run npm install at the root."),
		fileItem("b", "AGENTS.md:3", "pnpm workspaces only; never run npm install at the root"),
		fileItem("c", "CLAUDE.md:12", "Run tests with `pnpm test`."),
		fileItem("d", "CLAUDE.md:13", "The token is ghp_abcdefghijklmnopqrstuvwxyz0123456789."),
		fileItem("e", "AGENTS.md:8", "Background jobs use River"),
		fileItem("f", "AGENTS.md:9", "Run npm run test before committing."),
	}}
	ctx := context.Background()
	resumed, whole := newImportWorld(t), newImportWorld(t)
	if _, err := ledger.ImportOneByOne(ctx, resumed.f.l, resumed.request(c, "init-1"), 2); err != nil {
		t.Fatal(err)
	}
	got, err := resumed.f.l.Import(ctx, resumed.request(c, "init-1"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := ledger.ImportOneByOne(ctx, whole.f.l, whole.request(c, "init-1"), -1)
	if err != nil {
		t.Fatal(err)
	}
	want.Replayed = true // the resumed one had opened before
	if g, w := describeImport(got, nil), describeImport(want, nil); g != w {
		t.Errorf("resumed:\n %s\nwhole:\n %s", g, w)
	}
	if g, w := importRecordOf(resumed), importRecordOf(whole); g != w {
		t.Errorf("the records differ\n%s", lineDiff(g, w))
	}
}

// Two uploads with one key at once: one writes, the other waits for it
// and replays it.
func TestImportsWithOneKeyAtOnce(t *testing.T) {
	t.Parallel()
	w := newImportWorld(t)
	var items []ledger.ImportItem
	for i := range 60 {
		items = append(items, fileItem(fmt.Sprintf("k%d", i), fmt.Sprintf("CLAUDE.md:%d", i+1), fmt.Sprintf("Rule %d: keep the build green.", i)))
	}
	c := importCase{items: items}
	results := make([]*ledger.ImportResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = w.f.l.Import(context.Background(), w.request(c, "init-same"))
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("upload %d: %v", i, err)
		}
	}
	if results[0].Replayed == results[1].Replayed {
		t.Errorf("replayed: %v and %v, want one of each", results[0].Replayed, results[1].Replayed)
	}
	if g, w := describeImport(results[0], nil), describeImport(results[1], nil); strings.ReplaceAll(g, "replayed=true", "replayed=false") !=
		strings.ReplaceAll(w, "replayed=true", "replayed=false") {
		t.Errorf("the answers differ:\n%s\n%s", g, w)
	}
	if n := w.f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND via = 'import'`, w.space); n != 60 {
		t.Errorf("%d import receipts, want 60", n)
	}
}

// Display IDs stay one sequence while an import and Remembers in the same
// tenant run at once: no gap, no repeat, and the import's proposals in the
// order of its statements.
func TestImportNumbersInOrderBesideOtherWrites(t *testing.T) {
	t.Parallel()
	w := newImportWorld(t)
	var items []ledger.ImportItem
	for i := range 200 {
		items = append(items, fileItem(fmt.Sprintf("k%03d", i), fmt.Sprintf("CLAUDE.md:%d", i+1), fmt.Sprintf("Rule %d: keep the build green.", i)))
	}
	var wg sync.WaitGroup
	var res *ledger.ImportResult
	var ierr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, ierr = w.f.l.Import(context.Background(), w.request(importCase{items: items}, "init-1"))
	}()
	for i := range 20 {
		w.f.remember(w.zz, w.space, fmt.Sprintf("Written beside the import (%d).", i))
	}
	wg.Wait()
	if ierr != nil {
		t.Fatal(ierr)
	}
	if n, max := w.f.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1`, w.space),
		w.f.count(`SELECT max(seq) FROM v2.memories WHERE space_id = $1`, w.space); n != max || n != 3+200+20 {
		t.Errorf("%d memories numbered up to %d, want 223 with no gap", n, max)
	}
	for i := 1; i < len(res.Items); i++ {
		_, prev, _ := ledger.ParseRef(res.Items[i-1].Memory.Ref)
		_, n, _ := ledger.ParseRef(res.Items[i].Memory.Ref)
		if n != prev+1 {
			t.Fatalf("item %d is %s after %s: the import's numbers aren't one run", i, res.Items[i].Memory.Ref, res.Items[i-1].Memory.Ref)
		}
	}
}
