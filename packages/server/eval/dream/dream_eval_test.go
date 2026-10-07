// Package dreameval scores Dream (internal/v2dream, plan 25 §5.10, §11)
// on a small fixture space where every phase has a known answer
// (fixture.json): which notes fold into which kept memory, which notes
// are new facts and which are chatter, which proposals repeat each other,
// which kept facts disagree, what is past its date, what nobody read in
// sixty days, and that the Brief changes are small and cited. Run A is the
// night after the record was written; run B, sixty-one days later.
//
//	go test ./eval/dream/ -v                                                      # the fake model (CI)
//	DREAM_EVAL_LIVE=1 ANTHROPIC_API_KEY=… ANTHROPIC_BASE_URL=… go test ./eval/dream/ -v   # also the DREAM_* tiers
//
// The fake model (internal/v2dream/dreamtest) answers from the fixture's
// oracle, so its run checks the pipeline end to end: the engine's
// validation (an uncited Brief line is dropped), the ledger's re-checks,
// receipts and undo. The live run is the gate for the prompts; it goes
// through eval/livemeter, which checks every call's routing (zero
// retention) and reports cost. Results are in RESULTS.md.
package dreameval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/eval/livemeter"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream/dreamtest"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fixture struct {
	Kept []struct {
		Key, Statement, Section, Kind, Area string
		StaleDays                           int `json:"stale_days"`
	} `json:"kept"`
	Proposals []struct{ Key, Statement string } `json:"proposals"`
	Notes     []note                            `json:"notes"`
	Later     []note                            `json:"later_notes"`
	Brief     []struct {
		Key, Heading string
		Items        []string
	} `json:"brief"`
	Expect struct {
		Fold []struct {
			Memory string
			Notes  []string
		} `json:"fold"`
		Propose []struct {
			Notes []string
			Words []string
		} `json:"propose"`
		SkipNotes   []string   `json:"skip_notes"`
		Dedupe      [][]string `json:"dedupe"`
		Conflict    [][]string `json:"conflict"`
		Stale       []string   `json:"stale"`
		FadeB       []string   `json:"fade_b"`
		NeverFade   []string   `json:"never_fade"`
		BriefPlaces []string   `json:"brief_places"`
	} `json:"expect"`
	Oracle struct {
		Folds []dreamtest.FoldRule
		Facts []dreamtest.FactRule
		Pairs []dreamtest.PairRule
		Brief []dreamtest.BriefRule
	} `json:"oracle"`
}

type note struct{ Key, Agent, Body string }

func load(t *testing.T) fixture {
	t.Helper()
	b, err := os.ReadFile("fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// world is the fixture, written into a fresh database.
type world struct {
	t       *testing.T
	pool    *pgxpool.Pool
	l       *ledger.Ledger
	owner   uuid.UUID
	space   uuid.UUID
	mem     map[string]uuid.UUID // fixture key → memory
	key     map[uuid.UUID]string
	notes   map[string]uuid.UUID
	noteKey map[uuid.UUID]string
}

func build(t *testing.T, f fixture) *world {
	t.Helper()
	ctx := context.Background()
	_, pool := testdb.Acquire(t)
	w := &world{t: t, pool: pool, l: ledger.New(pool, ledger.WithLogger(quiet)), mem: map[string]uuid.UUID{},
		key: map[uuid.UUID]string{}, notes: map[string]uuid.UUID{}, noteKey: map[uuid.UUID]string{}}
	w.owner, w.space = uuid.New(), uuid.New()
	w.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Ziyang')`, w.owner, w.owner.String()[:8]+"@example.com")
	w.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at) VALUES ($1, 'memax-v2', $2, 'team', $3, 'project', now())`,
		w.space, "memax-v2-"+w.space.String()[:8], w.owner)
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, w.space, w.owner)
	scope, err := w.l.UserScope(ctx, w.owner)
	if err != nil {
		t.Fatal(err)
	}
	meta := func() ledger.Meta {
		return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: w.owner}, Scope: scope, Via: policy.ViaWeb,
			IdempotencyKey: uuid.NewString()}
	}
	apply := func(cmd ledger.Command) ledger.Result {
		res, err := w.l.Apply(ctx, cmd)
		if err != nil || res.Outcome == ledger.OutcomeRefused {
			t.Fatalf("%s: %v %+v", cmd.Name(), err, res.Policy)
		}
		return res
	}
	for _, k := range f.Kept {
		nm := ledger.NewMemory{SpaceID: w.space, Statement: k.Statement, Section: ledger.Section(k.Section)}
		if k.Kind == "decision" {
			nm.Kind, nm.Decision = ledger.KindDecision, &ledger.DecisionFields{Area: k.Area}
		}
		if k.StaleDays != 0 {
			at := time.Now().Add(time.Duration(k.StaleDays) * 24 * time.Hour)
			nm.StaleAfter = &at
		}
		m := apply(&ledger.Remember{Meta: meta(), NewMemory: nm}).Memory
		w.mem[k.Key], w.key[m.ID] = m.ID, k.Key
	}
	for _, p := range f.Proposals {
		m := apply(&ledger.Propose{Meta: meta(), NewMemory: ledger.NewMemory{SpaceID: w.space, Statement: p.Statement,
			Section: ledger.SectionConventions}}).Memory
		w.mem[p.Key], w.key[m.ID] = m.ID, p.Key
	}
	w.addNotes(f.Notes, 0)
	var sections []ledger.BriefSection
	for _, s := range f.Brief {
		sec := ledger.BriefSection{Key: s.Key, Heading: s.Heading}
		for _, k := range s.Items {
			sec.Items = append(sec.Items, ledger.BriefItem{Ref: w.ref(k)})
		}
		sections = append(sections, sec)
	}
	apply(&ledger.ReviseBrief{Meta: meta(), SpaceID: w.space, Title: "memax-v2", Sections: sections})
	return w
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.pool.Exec(context.Background(), sql, args...); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) addNotes(ns []note, offset int) {
	for i, n := range ns {
		id := uuid.New()
		w.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content, created_by_type, created_by_slug, source, created_at)
		        VALUES ($1, $2, $3, '', $4, 'agent', $5, 'mcp', now() - make_interval(secs => $6))`,
			id, w.owner, w.space, n.Body, n.Agent, 100-offset-i)
		w.notes[n.Key], w.noteKey[id] = id, n.Key
	}
}

func (w *world) ref(key string) string {
	var seq int64
	if err := w.pool.QueryRow(context.Background(), `SELECT seq FROM v2.memories WHERE id = $1`, w.mem[key]).Scan(&seq); err != nil {
		w.t.Fatal(err)
	}
	return ledger.FormatRef(ledger.PrefixMemory, seq)
}

func (w *world) lifecycle(key string) (lifecycle.Lifecycle, []string) {
	var lc lifecycle.Lifecycle
	var flags []string
	if err := w.pool.QueryRow(context.Background(), `SELECT lifecycle, flags FROM v2.memories WHERE id = $1`, w.mem[key]).Scan(&lc, &flags); err != nil {
		w.t.Fatal(err)
	}
	return lc, flags
}

// mode is one way to run Dream: the fake model, or a live tier.
type mode struct {
	name  string
	model v2dream.Model
	cfg   v2dream.Config
}

// score is precision and recall over one phase's expected actions.
type score struct{ hit, planned, expected int }

func (s score) precision() float64 {
	if s.planned == 0 {
		return 1
	}
	return float64(s.hit) / float64(s.planned)
}

func (s score) recall() float64 {
	if s.expected == 0 {
		return 1
	}
	return float64(s.hit) / float64(s.expected)
}

func (s score) String() string {
	return fmt.Sprintf("precision %.2f (%d/%d), recall %.2f (%d/%d)", s.precision(), s.hit, s.planned, s.recall(), s.hit, s.expected)
}

func TestDreamEval(t *testing.T) {
	f := load(t)
	modes := []mode{{"fake (oracle)", &dreamtest.Oracle{Folds: f.Oracle.Folds, Facts: f.Oracle.Facts, Pairs: f.Oracle.Pairs, Brief: f.Oracle.Brief},
		v2dream.Config{Primary: tier("fake/primary"), Strong: tier("fake/strong"), ZeroDataRetention: true, Log: quiet}}}
	var meter *livemeter.Meter
	if os.Getenv("DREAM_EVAL_LIVE") == "1" {
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			t.Fatal("DREAM_EVAL_LIVE=1 needs ANTHROPIC_API_KEY (and ANTHROPIC_BASE_URL for OpenRouter)")
		}
		base := os.Getenv("ANTHROPIC_BASE_URL")
		if base == "" {
			base = anthropic.DefaultBaseURL
		}
		var err error
		if meter, err = livemeter.Start(base); err != nil {
			t.Fatal(err)
		}
		defer meter.Close()
		cfg := v2dream.ConfigFromEnv(os.LookupEnv)
		if !cfg.Primary.Enabled() {
			t.Fatal("DREAM_EVAL_LIVE=1 needs a DREAM_MODEL other than off")
		}
		cfg.Log = quiet
		modes = append(modes, mode{"live " + cfg.Primary.Model, v2dream.NewAnthropicModel(anthropic.New(key, meter.URL()), cfg.ZeroDataRetention), cfg})
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) { runMode(t, f, m) })
	}
	if meter != nil {
		zdr, err := livemeter.ZDREndpoints(context.Background())
		if err != nil {
			t.Logf("zero-retention list: %v", err)
		}
		sums := livemeter.Summarize(meter.Calls(), zdr)
		t.Log("gateway:\n" + livemeter.Format(sums))
		for _, s := range sums {
			if s.ZDRAsked != s.Calls {
				t.Errorf("%s: %d of %d calls asked for zero retention", s.Model, s.ZDRAsked, s.Calls)
			}
			if len(s.NotZDR) > 0 {
				t.Errorf("%s was served by providers that aren't zero-retention: %v", s.Model, s.NotZDR)
			}
		}
	}
}

func tier(model string) judge.Tier { return judge.Tier{Model: model} }

func runMode(t *testing.T, f fixture, m mode) {
	ctx := context.Background()
	w := build(t, f)
	live := !strings.HasPrefix(m.name, "fake")

	// Run A: the night after.
	nowA := time.Now()
	engA := v2dream.New(w.l, m.model, m.cfg, v2dream.WithSearcher(v2recall.New(w.l)))
	start := time.Now()
	outA, err := engA.Run(ctx, ledger.DreamSpaceArgs{SpaceID: w.space, Slot: nowA.Truncate(time.Second), Trigger: ledger.DreamScheduled})
	if err != nil || !outA.Ran {
		t.Fatalf("run A: %+v %v", outA, err)
	}
	t.Logf("run A in %s: applied %v, skipped %v, model %+v", time.Since(start).Round(time.Millisecond),
		outA.Edition.Counts, outA.Edition.Stats.Skipped, outA.Edition.Stats.Model)
	scope, _ := w.l.SpaceScope(ctx, w.space)
	ed, err := w.l.GetEdition(ctx, scope, w.space, outA.Edition.Ref)
	if err != nil {
		t.Fatal(err)
	}

	// Fold: every expected (memory, note) pair, and no fold that isn't.
	var fold score
	expectFold := map[string]bool{}
	for _, e := range f.Expect.Fold {
		for _, n := range e.Notes {
			expectFold[e.Memory+"|"+n] = true
		}
	}
	fold.expected = len(expectFold)
	var propose score
	propose.expected = len(f.Expect.Propose)
	chatter := 0
	for _, a := range ed.Actions {
		switch a.Kind {
		case ledger.DreamFold:
			for _, n := range noteKeys(w, a) {
				fold.planned++
				if expectFold[w.key[a.Memory.ID]+"|"+n] {
					fold.hit++
				}
			}
		case ledger.DreamPropose:
			propose.planned++
			notes := noteKeys(w, a)
			for _, n := range notes {
				if slices.Contains(f.Expect.SkipNotes, n) {
					chatter++
				}
			}
			for _, e := range f.Expect.Propose {
				if slices.ContainsFunc(notes, func(n string) bool { return slices.Contains(e.Notes, n) }) &&
					(live || containsAll(a.Memory.Statement, e.Words)) {
					propose.hit++
					break
				}
			}
			if a.Memory.Lifecycle != lifecycle.Proposed && a.Memory.Lifecycle != lifecycle.Merged {
				t.Errorf("a new fact is %s, not a proposal: Dream never keeps", a.Memory.Lifecycle)
			}
			if a.Memory.Trust == policy.TrustPerson {
				t.Errorf("a new fact is trusted as a person's: Dream can't raise trust")
			}
		}
	}
	// Dedupe and conflicts, by pair.
	pairScore := func(kind ledger.DreamActionKind, expect [][]string) score {
		s := score{expected: len(expect)}
		for _, a := range ed.Actions {
			if a.Kind != kind {
				continue
			}
			s.planned++
			got := []string{w.key[a.Memory.ID], w.key[a.Related.ID]}
			for _, e := range expect {
				if (got[0] == e[0] && got[1] == e[1]) || (got[0] == e[1] && got[1] == e[0]) {
					s.hit++
					break
				}
			}
		}
		return s
	}
	dedupe := pairScore(ledger.DreamDedupe, f.Expect.Dedupe)
	conflict := pairScore(ledger.DreamConflict, f.Expect.Conflict)
	stale := score{expected: len(f.Expect.Stale)}
	for _, a := range ed.Actions {
		if a.Kind == ledger.DreamStale {
			stale.planned++
			if slices.Contains(f.Expect.Stale, w.key[a.Memory.ID]) {
				stale.hit++
			}
		}
	}
	// The Brief: small, cited, and what it places.
	brief, err := w.l.GetBrief(ctx, scope, w.space)
	if err != nil {
		t.Fatal(err)
	}
	placed := map[string]bool{}
	for _, s := range brief.Sections {
		for _, it := range s.Items {
			if it.Ref != "" {
				placed[it.Ref] = true
			} else if len(it.Cites) == 0 {
				t.Errorf("the Brief holds an uncited line: %q", it.Text)
			}
		}
	}
	briefOK := ed.Counts[ledger.DreamBrief] <= 1
	for _, a := range ed.Actions {
		if a.Kind == ledger.DreamBrief && a.Brief.Ops > m.cfg.BriefMaxOps && m.cfg.BriefMaxOps > 0 {
			briefOK = false
		}
	}
	briefPlaced := 0
	for _, k := range f.Expect.BriefPlaces {
		if placed[w.ref(k)] {
			briefPlaced++
		}
	}

	// Run B: sixty-one days later, after one more note.
	w.addNotes(f.Later, 50)
	nowB := time.Now().Add(61 * 24 * time.Hour)
	lB := ledger.New(w.pool, ledger.WithLogger(quiet), ledger.WithClock(func() time.Time { return nowB }))
	engB := v2dream.New(lB, m.model, m.cfg, v2dream.WithSearcher(v2recall.New(lB)), v2dream.WithClock(func() time.Time { return nowB }))
	outB, err := engB.Run(ctx, ledger.DreamSpaceArgs{SpaceID: w.space, Slot: nowB.Truncate(time.Second), Trigger: ledger.DreamScheduled})
	if err != nil || !outB.Ran {
		t.Fatalf("run B: %+v %v", outB, err)
	}
	// What should fade: the expected ones, unless run A's Brief placed one
	// (the Brief's memories never fade).
	var fade score
	for _, k := range f.Expect.FadeB {
		lc, _ := w.lifecycle(k)
		if placed[w.ref(k)] {
			if lc == lifecycle.Faded {
				t.Errorf("%s faded though the Brief places it", k)
			}
			continue
		}
		fade.expected++
		if lc == lifecycle.Faded {
			fade.hit++
		}
	}
	fade.planned = outB.Edition.Counts[ledger.DreamFade]
	for _, k := range f.Expect.NeverFade {
		if lc, _ := w.lifecycle(k); lc == lifecycle.Faded {
			t.Errorf("%s faded: decisions, the Brief's memories, flagged ones and open conflicts never fade", k)
		}
	}

	t.Logf("fold     %s", fold)
	t.Logf("propose  %s (facts from chatter: %d)", propose, chatter)
	t.Logf("dedupe   %s", dedupe)
	t.Logf("conflict %s", conflict)
	t.Logf("stale    %s", stale)
	t.Logf("fade     %s", fade)
	t.Logf("brief    small and cited: %v, placed %d of %d expected", briefOK, briefPlaced, len(f.Expect.BriefPlaces))

	// Rule 9 over the whole eval: undo every action of run A, newest first.
	undone, refused := 0, 0
	for i := len(ed.Actions) - 1; i >= 0; i-- {
		_, err := w.l.Apply(ctx, &ledger.UndoDreamAction{Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: w.owner},
			Scope: mustScope(t, w), Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()}, Action: ed.Actions[i].ID})
		if err != nil {
			refused++
		} else {
			undone++
		}
	}
	t.Logf("undo     %d of run A's %d actions undone, %d refused (a later change: run B's edition, or a fade)", undone, len(ed.Actions), refused)

	// The bars. The fake answers from the oracle: the pipeline must be exact.
	// A live tier must be precise (a wrong fold or a false conflict costs a
	// person) and find most of it.
	bar := func(what string, s score, p, r float64) {
		if s.precision() < p || s.recall() < r {
			t.Errorf("%s: %s, want precision ≥ %.2f and recall ≥ %.2f", what, s, p, r)
		}
	}
	if !live {
		for what, s := range map[string]score{"fold": fold, "propose": propose, "dedupe": dedupe, "conflict": conflict, "stale": stale, "fade": fade} {
			bar(what, s, 1, 1)
		}
		if !briefOK || briefPlaced != len(f.Expect.BriefPlaces) || outA.Edition.Stats.Considered["brief_dropped"] != 1 {
			t.Errorf("brief: small %v, placed %d, dropped the uncited line %d time(s)", briefOK, briefPlaced, outA.Edition.Stats.Considered["brief_dropped"])
		}
		if chatter != 0 {
			t.Errorf("%d facts from chatter", chatter)
		}
		return
	}
	bar("fold", fold, 0.8, 0.6)
	bar("propose", propose, 0.5, 0.5)
	bar("dedupe", dedupe, 1, 0.5)
	bar("conflict", conflict, 1, 1)
	bar("stale", stale, 1, 1)
	bar("fade", fade, 1, 1)
	if chatter != 0 {
		t.Errorf("%d facts from chatter", chatter)
	}
	if !briefOK {
		t.Error("the Brief changed more than a small, cited edit")
	}
}

func mustScope(t *testing.T, w *world) ledger.Scope {
	s, err := w.l.UserScope(context.Background(), w.owner)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func noteKeys(w *world, a ledger.DreamAction) []string {
	var out []string
	refs := map[string]bool{}
	for _, r := range a.NoteRefs {
		refs[r] = true
	}
	rows, err := w.pool.Query(context.Background(), `SELECT note_id, seq FROM v2.note_refs WHERE space_id = $1`, w.space)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			w.t.Fatal(err)
		}
		if refs[ledger.FormatRef(ledger.PrefixNote, seq)] {
			out = append(out, w.noteKey[id])
		}
	}
	return out
}

func containsAll(s string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(strings.ToLower(s), strings.ToLower(w)) {
			return false
		}
	}
	return true
}
