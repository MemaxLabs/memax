package importseval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/eval/livemeter"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
)

// The live import eval: the tiers the worker's import check uses (the
// judge's primary, then its fallback; JUDGE_*), through the gateway, with
// every call's latency, cost and routing (plan 25 §11). Results are in
// RESULTS.md.
//
//	IMPORT_EVAL_LIVE=1 go test ./eval/imports/ -run Live -v -timeout 20m
//
//	IMPORT_EVAL_SET       the labelled set (default batches.json; holdout.json checks a change on batches it wasn't tuned on)
//	IMPORT_EVAL_TIERS     the runs, comma-separated (default "pipeline"):
//	                        pipeline  the worker's configuration: JUDGE_MODEL, then JUDGE_FALLBACK_MODEL
//	                        primary   JUDGE_MODEL alone
//	                        fallback  JUDGE_FALLBACK_MODEL alone, strict
//	IMPORT_EVAL_BATCH     proposals a call compares (default judge.ImportBatch)
//	IMPORT_EVAL_ONLY      the batches to run, by id, comma-separated (default all); run one small first
//	IMPORT_EVAL_OUT       a directory for each run's groups (<run>.json: refs, confidences, classes, timings;
//	                      never statements or the model's words)
//	IMPORT_EVAL_PARALLEL  imports checked at once (default 2)
//	IMPORT_EVAL_DEBUG=1   print each answer the check couldn't use (fixture words only), and why
//
// The meter (eval/livemeter) sits between the shared client and the
// gateway, so the code under test is exactly the worker's:
// judge.Classifier.FindImportDisagreements over judge.AnthropicModel over
// anthropic.Client.

// The bars of plan 25 §11 and of this eval, on batches.json (RESULTS.md
// says why): every planted conflict found, and precision 0.95, about two
// wrong groups in a run's 55. The prompt of Oct 7 scored 0.98 four times;
// the one before it 0.92 and 0.96.
const (
	minRecall    = 1.0
	minPrecision = 0.95
)

// timedModel records each model call's tier, size and latency, and
// whether the check could use its answer.
type timedModel struct {
	inner judge.Model
	mu    sync.Mutex
	calls []timedCall
}

type timedCall struct {
	tier, model string
	statements  int
	took        time.Duration
	err         error
	// unusable says why the check couldn't use the answer, and answer is
	// its words then (IMPORT_EVAL_DEBUG=1 prints them).
	unusable, answer string
}

// model is the model as one import's check calls it, so each answer can
// be read as the check reads it.
func (m *timedModel) model(b *built) judge.Model { return importModel{m, b} }

type importModel struct {
	m *timedModel
	b *built
}

func (im importModel) Complete(ctx context.Context, c judge.Call) (string, error) {
	start := time.Now()
	text, err := im.m.inner.Complete(ctx, c)
	tc := timedCall{tier: c.Tier.Name, model: c.Tier.Model,
		statements: len(statementID.FindAllStringIndex(c.Prompt, -1)), took: time.Since(start), err: err}
	if err == nil {
		if _, perr := judge.ParseImportAnswer(text, im.b.candidates()); perr != nil {
			tc.unusable, tc.answer = perr.Error(), text
		}
	}
	im.m.mu.Lock()
	im.m.calls = append(im.m.calls, tc)
	im.m.mu.Unlock()
	return text, err
}

func (m *timedModel) mark() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *timedModel) since(mark int) []timedCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls[min(mark, len(m.calls)):])
}

type liveRun struct {
	name string
	cfg  judge.Config
}

func liveRuns(t *testing.T, cfg judge.Config) []liveRun {
	t.Helper()
	names := os.Getenv("IMPORT_EVAL_TIERS")
	if strings.TrimSpace(names) == "" {
		names = "pipeline"
	}
	var runs []liveRun
	for _, name := range strings.Split(names, ",") {
		switch name = strings.TrimSpace(name); name {
		case "pipeline":
			runs = append(runs, liveRun{name, judge.Config{Primary: cfg.Primary, Fallback: cfg.Fallback,
				ZeroDataRetention: cfg.ZeroDataRetention, CallTimeout: cfg.CallTimeout}})
		case "primary":
			runs = append(runs, liveRun{name, judge.Config{Primary: cfg.Primary, ZeroDataRetention: cfg.ZeroDataRetention,
				CallTimeout: cfg.CallTimeout}})
		case "fallback":
			fb := cfg.Fallback
			fb.Strict = true
			runs = append(runs, liveRun{name, judge.Config{Primary: fb, ZeroDataRetention: cfg.ZeroDataRetention,
				CallTimeout: cfg.CallTimeout}})
		default:
			t.Fatalf("IMPORT_EVAL_TIERS: unknown run %q", name)
		}
	}
	return runs
}

// The real tiers (IMPORT_EVAL_LIVE=1, ANTHROPIC_API_KEY, and
// ANTHROPIC_BASE_URL for OpenRouter): the pipeline must find every planted
// conflict in batches.json at ImportConflictBar, with precision ≥
// minPrecision, every call served where its tier pinned it.
func TestLiveModel(t *testing.T) {
	if os.Getenv("IMPORT_EVAL_LIVE") != "1" {
		t.Skip("set IMPORT_EVAL_LIVE=1 (with ANTHROPIC_API_KEY, and ANTHROPIC_BASE_URL for OpenRouter) to score the real model")
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Fatal("IMPORT_EVAL_LIVE=1 needs ANTHROPIC_API_KEY")
	}
	base := os.Getenv("ANTHROPIC_BASE_URL")
	if base == "" {
		base = anthropic.DefaultBaseURL
	}
	meter, err := livemeter.Start(base)
	if err != nil {
		t.Fatal(err)
	}
	defer meter.Close()
	zdr, err := livemeter.ZDREndpoints(context.Background())
	if err != nil {
		t.Logf("no ZDR list, so the routing check is skipped: %v", err)
	}

	cfg := judge.ConfigFromEnv(os.LookupEnv)
	model := &timedModel{inner: judge.NewAnthropicModel(anthropic.New(key, meter.URL()), cfg.ZeroDataRetention)}
	set := os.Getenv("IMPORT_EVAL_SET")
	if set == "" {
		set = mainSet
	}
	sets := buildSet(t, set)
	full := set == mainSet
	if only := strings.TrimSpace(os.Getenv("IMPORT_EVAL_ONLY")); only != "" {
		ids := strings.Split(only, ",")
		sets = slices.DeleteFunc(sets, func(b *built) bool { return !slices.Contains(ids, b.ID) })
		full = false
	}
	size, _ := strconv.Atoi(os.Getenv("IMPORT_EVAL_BATCH"))
	if size <= 0 {
		size = judge.ImportBatch
	}
	full = full && size == judge.ImportBatch
	parallel := 2
	if p, err := strconv.Atoi(os.Getenv("IMPORT_EVAL_PARALLEL")); err == nil && p > 0 {
		parallel = p
	}
	t.Logf("tiers: primary %s, fallback %s; zdr %v; %d imports of %s, %d proposals a call, %d at once",
		cfg.Primary.Model, cfg.Fallback.Model, cfg.ZeroDataRetention, len(sets), set, size, parallel)

	for _, run := range liveRuns(t, cfg) {
		gatewayMark, modelMark := meter.Mark(), model.mark()
		start := time.Now()
		runs, took := checkLive(sets, model, run.cfg, size, parallel)
		wall := time.Since(start)
		tl, _ := at(runs, judge.ImportConflictBar)
		calls := meter.Since(gatewayMark)
		t.Logf("%s (%s) at the bar %.2f: %s\n%s\n%s%s", run.name, run.cfg.Primary.Model, judge.ImportConflictBar, tl,
			tl.breakdown(), sweep(runs), details(runs, judge.ImportConflictBar))
		t.Log(liveReport(run, runs, took, model.since(modelMark), calls, zdr, wall))
		if dir := os.Getenv("IMPORT_EVAL_OUT"); dir != "" {
			if err := writeRun(filepath.Join(dir, run.name+".json"), run, set, size, runs, took); err != nil {
				t.Errorf("write the run: %v", err)
			}
		}
		for _, r := range runs {
			if r.err != nil {
				t.Errorf("%s: %s: the check failed: %v", run.name, r.b.ID, r.err)
			}
		}

		// Routing, as the judge eval checks it: every call asked for zero
		// retention and was served by a zero-retention endpoint of its model
		// (plan 25 D14), from the hosts its tier pinned, at a precision its
		// tier admitted, with its tier's structured output and temperature.
		for _, s := range livemeter.Summarize(calls, zdr) {
			if run.cfg.ZeroDataRetention && s.ZDRAsked != s.Calls {
				t.Errorf("%s: %s: %d of %d calls didn't ask for zero retention", run.name, s.Model, s.Calls-s.ZDRAsked, s.Calls)
			}
			if len(s.NotZDR) > 0 {
				t.Errorf("%s: %s was served by providers that aren't zero-retention for it: %v", run.name, s.Model, s.NotZDR)
			}
			if len(s.Unpinned) > 0 {
				t.Errorf("%s: %s was served by providers its tier didn't pin: %v", run.name, s.Model, s.Unpinned)
			}
			if len(s.BelowFloor) > 0 {
				t.Errorf("%s: %s was served below its tier's precision floor by %v", run.name, s.Model, s.BelowFloor)
			}
		}
		for _, cl := range calls {
			tier := tierOf(run.cfg, cl.Model)
			if tier.Strict != cl.Strict {
				t.Errorf("%s: a %s call had output_config %v, the tier's strict is %v", run.name, cl.Model, cl.Strict, tier.Strict)
				break
			}
			if !slices.Equal(tier.Routing.Providers, cl.Only) || !slices.Equal(tier.Routing.Quantizations, cl.Quantizations) {
				t.Errorf("%s: a %s call pinned %v at %v, the tier %v at %v", run.name, cl.Model, cl.Only, cl.Quantizations,
					tier.Routing.Providers, tier.Routing.Quantizations)
				break
			}
			if (tier.Temperature == nil) != (cl.Temperature == nil) || (cl.Temperature != nil && *cl.Temperature != *tier.Temperature) {
				t.Errorf("%s: a %s call's temperature isn't its tier's", run.name, cl.Model)
				break
			}
		}
		if run.name != "pipeline" || !full {
			continue
		}
		if tl.recall() < minRecall || tl.precision() < minPrecision {
			t.Errorf("at the bar %.2f: recall %.2f (bar %.2f), precision %.2f (bar %.2f)", judge.ImportConflictBar,
				tl.recall(), minRecall, tl.precision(), minPrecision)
		}
	}
}

// checkLive checks every batch, a few at a time, and times each check.
func checkLive(sets []*built, model *timedModel, cfg judge.Config, size, parallel int) ([]checked, []time.Duration) {
	out := make([]checked, len(sets))
	took := make([]time.Duration, len(sets))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, b := range sets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			start := time.Now()
			c := judge.NewClassifier(model.model(b), cfg)
			check, err := c.FindImportDisagreements(context.Background(), b.candidates(), size)
			took[i] = time.Since(start)
			out[i] = checked{b: b, check: check, err: err}
		}()
	}
	wg.Wait()
	return out, took
}

func tierOf(cfg judge.Config, model string) judge.Tier {
	for _, tier := range []judge.Tier{cfg.Primary, cfg.Fallback} {
		if tier.Model == model {
			return tier
		}
	}
	return judge.Tier{}
}

func liveReport(run liveRun, runs []checked, took []time.Duration, calls []timedCall, gateway []livemeter.Call,
	zdr livemeter.ZDRList, wall time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d imports in %v\n", run.name, len(runs), wall.Round(time.Second))
	for i, r := range runs {
		fmt.Fprintf(&b, "  %s: %d proposals, %d batches, %d calls (%s), %v\n", r.b.ID, len(r.b.props), r.check.Batches,
			r.check.Calls, r.check.Tier.Name, took[i].Round(10*time.Millisecond))
	}
	fmt.Fprintf(&b, "import check latency: %s\n", livemeter.QuantilesOf(took))
	byTier := map[string][]time.Duration{}
	errs := map[string]int{}
	var tiers []string
	for _, c := range calls {
		k := c.tier + " " + c.model
		if _, ok := byTier[k]; !ok {
			tiers = append(tiers, k)
		}
		byTier[k] = append(byTier[k], c.took)
		if c.err != nil {
			errs[k]++
		}
	}
	sort.Strings(tiers)
	for _, k := range tiers {
		fmt.Fprintf(&b, "calls %s: %d (%d errors), %s\n", k, len(byTier[k]), errs[k], livemeter.QuantilesOf(byTier[k]))
	}
	unused := 0
	for _, c := range calls {
		fmt.Fprintf(&b, "  call %s: %d statements, %v", c.tier, c.statements, c.took.Round(10*time.Millisecond))
		if c.err != nil {
			fmt.Fprintf(&b, " (error: %s)", firstWords(c.err.Error(), 160))
		}
		if c.unusable != "" {
			unused++
			fmt.Fprintf(&b, " (unusable: %s)", c.unusable)
			if os.Getenv("IMPORT_EVAL_DEBUG") == "1" {
				fmt.Fprintf(&b, "\n%s\n", firstWords(c.answer, 1500))
			}
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "answers the check couldn't use: %d of %d\n", unused, len(calls))
	b.WriteString("gateway:\n")
	b.WriteString(livemeter.Format(livemeter.Summarize(gateway, zdr)))
	return b.String()
}

// runRecord is a run's groups, without any statement or model words.
type runRecord struct {
	Run     string           `json:"run"`
	Date    string           `json:"date"`
	Set     string           `json:"set"`
	Batch   int              `json:"batch_size"`
	Bar     float64          `json:"bar"`
	Routing map[string]any   `json:"routing"`
	Imports []importRecord   `json:"imports"`
	Sweep   []map[string]any `json:"sweep"`
}

type importRecord struct {
	ID        string          `json:"id"`
	Proposals int             `json:"proposals"`
	Batches   int             `json:"batches"`
	Calls     int             `json:"calls"`
	Tier      string          `json:"tier,omitempty"`
	MS        int64           `json:"ms"`
	Error     string          `json:"error,omitempty"`
	Groups    []groupRecord   `json:"groups"`
	Found     map[string]bool `json:"found"`
	Sprung    map[string]bool `json:"sprung"`
}

type groupRecord struct {
	Members    []string `json:"members"`
	Confidence float64  `json:"confidence"`
	Class      string   `json:"class"`
	Credit     float64  `json:"credit"`
	Conflict   string   `json:"conflict,omitempty"`
}

func writeRun(path string, run liveRun, set string, size int, runs []checked, took []time.Duration) error {
	rec := runRecord{Run: run.name, Date: time.Now().UTC().Format(time.RFC3339), Set: set, Batch: size,
		Bar: judge.ImportConflictBar, Routing: map[string]any{}}
	for _, tier := range []judge.Tier{run.cfg.Primary, run.cfg.Fallback} {
		if tier.Enabled() {
			rec.Routing[tier.Model] = map[string]any{"only": tier.Routing.Providers, "quantizations": tier.Routing.Quantizations,
				"temperature": tier.Temperature, "strict": tier.Strict}
		}
	}
	// Every group the model named, scored at bar 0: the sweep's input.
	_, scores := at(runs, 0)
	for i, r := range runs {
		ir := importRecord{ID: r.b.ID, Proposals: len(r.b.props), Batches: r.check.Batches, Calls: r.check.Calls,
			Tier: r.check.Tier.Name, MS: took[i].Milliseconds(), Found: scores[i].found, Sprung: scores[i].sprung,
			Groups: []groupRecord{}}
		if r.err != nil {
			ir.Error = firstWords(r.err.Error(), 160)
		}
		for _, g := range scores[i].groups {
			gr := groupRecord{Confidence: g.confidence, Class: g.class, Credit: g.credit, Conflict: g.conflict}
			for _, m := range g.members {
				gr.Members = append(gr.Members, r.b.props[m].refs[0])
			}
			ir.Groups = append(ir.Groups, gr)
		}
		rec.Imports = append(rec.Imports, ir)
	}
	for _, bar := range sweepBars {
		tl, _ := at(runs, bar)
		sp, of := tl.sprungTotal()
		rec.Sweep = append(rec.Sweep, map[string]any{"bar": bar, "recall": tl.recall(), "found": tl.found,
			"planted": tl.planted, "precision": tl.precision(), "groups": tl.groups, "classes": tl.classes,
			"held": tl.held, "bystanders": tl.bystanders, "sprung": sp, "traps": of})
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func firstWords(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
