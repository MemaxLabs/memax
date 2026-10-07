package judgeeval

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
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The live judge eval: the real model tiers through the gateway, each tier
// alone and the whole pipeline, with every call's latency, cost and
// routing (plan 25 §5.8, §11). Results are in RESULTS.md.
//
//	JUDGE_EVAL_LIVE=1 go test ./eval/judge/ -run Live -v -timeout 30m
//
//	JUDGE_EVAL_TIERS      the runs, comma-separated (default "pipeline"):
//	                        pipeline  the worker's configuration (JUDGE_*): primary, fallback, strong
//	                        primary   JUDGE_MODEL alone
//	                        fallback  JUDGE_FALLBACK_MODEL alone, strict
//	                        strong    JUDGE_STRONG_MODEL alone, strict, on the pairs whose candidate is a
//	                                  decision in force (the only verdicts it confirms in the pipeline)
//	JUDGE_EVAL_SET        the labelled set (default pairs.json; holdout.json checks a change on pairs it wasn't tuned on)
//	JUDGE_EVAL_PAIRS      a stratified sample of about this many pairs (default: all of the set); run small first
//	JUDGE_EVAL_PRIMARY_STRICT=1  send the primary tier the schema as output_config.format (strict
//	                      structured output) instead of in the prompt
//	JUDGE_EVAL_OUT        a directory for each run's verdicts (<run>.json: pair ids, labels, relation,
//	                      confidence, tier, latency; never memory text or rationales)
//	JUDGE_EVAL_PARALLEL   verdicts in flight (default 4)
//
// The meter (eval/livemeter) sits between the shared client and the
// gateway, so the code under test is exactly the worker's: judge.Classifier
// over judge.AnthropicModel over anthropic.Client.

// timedModel records each model call's tier and latency.
type timedModel struct {
	inner judge.Model
	mu    sync.Mutex
	calls []timedCall
}

type timedCall struct {
	tier, model string
	took        time.Duration
	err         error
}

func (m *timedModel) Complete(ctx context.Context, c judge.Call) (string, error) {
	start := time.Now()
	text, err := m.inner.Complete(ctx, c)
	m.mu.Lock()
	m.calls = append(m.calls, timedCall{tier: c.Tier.Name, model: c.Tier.Model, took: time.Since(start), err: err})
	m.mu.Unlock()
	return text, err
}

func (m *timedModel) since(mark int) []timedCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]timedCall(nil), m.calls[min(mark, len(m.calls)):]...)
}

func (m *timedModel) mark() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// sample takes about n pairs, the same share of each class, in file order.
func sample(pairs []pair, n int) []pair {
	if n <= 0 || n >= len(pairs) {
		return pairs
	}
	per := (n + len(classes) - 1) / len(classes)
	taken := map[ledger.Relation]int{}
	var out []pair
	for _, p := range pairs {
		if taken[p.Class] < per {
			taken[p.Class]++
			out = append(out, p)
		}
	}
	return out
}

type liveRun struct {
	name  string
	cfg   judge.Config
	pairs []pair
}

func liveRuns(t *testing.T, cfg judge.Config, pairs []pair) []liveRun {
	t.Helper()
	single := func(tier judge.Tier, strict bool, maxTokens int) judge.Config {
		tier.Strict, tier.MaxTokens = strict, maxTokens
		return judge.Config{Primary: tier, ZeroDataRetention: cfg.ZeroDataRetention, CallTimeout: cfg.CallTimeout}
	}
	var inForce []pair
	for _, p := range pairs {
		if p.Candidate.InForce {
			inForce = append(inForce, p)
		}
	}
	names := os.Getenv("JUDGE_EVAL_TIERS")
	if strings.TrimSpace(names) == "" {
		names = "pipeline"
	}
	var runs []liveRun
	for _, name := range strings.Split(names, ",") {
		switch name = strings.TrimSpace(name); name {
		case "pipeline":
			runs = append(runs, liveRun{name, cfg, pairs})
		case "primary":
			runs = append(runs, liveRun{name, single(cfg.Primary, cfg.Primary.Strict, 0), pairs})
		case "fallback":
			runs = append(runs, liveRun{name, single(cfg.Fallback, true, 0), pairs})
		case "strong":
			// As in the pipeline: the strong tier's budget covers its thinking.
			runs = append(runs, liveRun{name, single(cfg.Strong, true, 8000), inForce})
		default:
			t.Fatalf("JUDGE_EVAL_TIERS: unknown run %q", name)
		}
	}
	return runs
}

// The real model tiers (JUDGE_EVAL_LIVE=1, ANTHROPIC_API_KEY, and
// ANTHROPIC_BASE_URL for OpenRouter): the pipeline must reach contradicts
// precision ≥ 0.90 and recall ≥ 0.75, and a verdict p95 under 5 s.
func TestLiveModel(t *testing.T) {
	if os.Getenv("JUDGE_EVAL_LIVE") != "1" {
		t.Skip("set JUDGE_EVAL_LIVE=1 (with ANTHROPIC_API_KEY, and ANTHROPIC_BASE_URL for OpenRouter) to score the real model")
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Fatal("JUDGE_EVAL_LIVE=1 needs ANTHROPIC_API_KEY")
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
	ctx := context.Background()
	zdr, err := livemeter.ZDREndpoints(ctx)
	if err != nil {
		t.Logf("no ZDR list, so the routing check is skipped: %v", err)
	}

	cfg := judge.ConfigFromEnv(os.LookupEnv)
	if os.Getenv("JUDGE_EVAL_PRIMARY_STRICT") == "1" {
		cfg.Primary.Strict = true
	}
	model := &timedModel{inner: judge.NewAnthropicModel(anthropic.New(key, meter.URL()), cfg.ZeroDataRetention)}
	set := os.Getenv("JUDGE_EVAL_SET")
	if set == "" {
		set = "pairs.json"
	}
	all := loadSet(t, set)
	n, _ := strconv.Atoi(os.Getenv("JUDGE_EVAL_PAIRS"))
	pairs := sample(all, n)
	// The bars are asserted on the whole labelled set only.
	full := set == "pairs.json" && len(pairs) == len(all)
	parallel := 4
	if p, err := strconv.Atoi(os.Getenv("JUDGE_EVAL_PARALLEL")); err == nil && p > 0 {
		parallel = p
	}
	t.Logf("tiers: primary %s (strict %v), fallback %s, strong %s; zdr %v; %d pairs of %s, %d in flight",
		cfg.Primary.Model, cfg.Primary.Strict, cfg.Fallback.Model, cfg.Strong.Model, cfg.ZeroDataRetention,
		len(pairs), set, parallel)

	for _, run := range liveRuns(t, cfg, pairs) {
		c := judge.NewClassifier(model, run.cfg)
		if c == nil {
			t.Errorf("%s: the primary tier is off", run.name)
			continue
		}
		gatewayMark, modelMark := meter.Mark(), model.mark()
		start := time.Now()
		outs := classifyAll(t, c, run.pairs, parallel, judge.DefaultThresholds)
		wall := time.Since(start)
		per, _ := report(t, run.name+" ("+run.cfg.Primary.Model+")", outs)
		calls := meter.Since(gatewayMark)
		t.Log(liveReport(run, outs, model.since(modelMark), calls, zdr, wall))
		if dir := os.Getenv("JUDGE_EVAL_OUT"); dir != "" {
			if err := writeVerdicts(filepath.Join(dir, run.name+".json"), run, outs); err != nil {
				t.Errorf("write verdicts: %v", err)
			}
		}

		// Routing: every call asked for zero retention, and was served by a
		// zero-retention endpoint of its model (plan 25 D14), from the hosts
		// its tier pinned, at a precision its tier admitted; strict tiers
		// sent their schema as output_config.format, and every call its
		// tier's temperature.
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
		con := per[ledger.RelationContradicts]
		if con.precision() < minContradictsPrecision || con.recall() < minContradictsRecall {
			t.Errorf("contradicts: precision %.2f (bar %.2f), recall %.2f (bar %.2f)", con.precision(), minContradictsPrecision,
				con.recall(), minContradictsRecall)
		}
		if q := livemeter.QuantilesOf(durations(outs)); q.P95 > 5*time.Second {
			t.Errorf("verdict p95 %v, over the 5 s target", q.P95.Round(10*time.Millisecond))
		}
	}
}

// tierOf is the run's tier for a model slug.
func tierOf(cfg judge.Config, model string) judge.Tier {
	for _, tier := range []judge.Tier{cfg.Primary, cfg.Fallback, cfg.Strong} {
		if tier.Model == model {
			return tier
		}
	}
	return judge.Tier{}
}

func durations(outs []outcome) []time.Duration {
	ds := make([]time.Duration, len(outs))
	for i, o := range outs {
		ds[i] = o.took
	}
	return ds
}

func liveReport(run liveRun, outs []outcome, calls []timedCall, gateway []livemeter.Call, zdr livemeter.ZDRList, wall time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d verdicts in %v\n", run.name, len(outs), wall.Round(time.Second))

	// Verdict latency, overall and by whether the strong tier confirmed it.
	var all, escalated, direct []time.Duration
	var retried, fellBack, strong, escFailed, unconfirmed, failed int
	for _, o := range outs {
		all = append(all, o.took)
		if o.calls[ledger.TierStrong] > 0 {
			escalated = append(escalated, o.took)
			strong++
		} else {
			direct = append(direct, o.took)
		}
		if o.calls[ledger.TierPrimary] > 1 {
			retried++
		}
		if o.calls[ledger.TierFallback] > 0 {
			fellBack++
		}
		if o.escalation != "" {
			escFailed++
		}
		if o.unconfirmed {
			unconfirmed++
		}
		if o.err != nil {
			failed++
		}
	}
	fmt.Fprintf(&b, "verdict latency: %s\n", livemeter.QuantilesOf(all))
	if len(escalated) > 0 {
		fmt.Fprintf(&b, "  confirmed by the strong tier (%d): %s\n  not escalated (%d): %s\n", len(escalated),
			livemeter.QuantilesOf(escalated), len(direct), livemeter.QuantilesOf(direct))
	}
	fmt.Fprintf(&b, "verdicts: %d retried the primary, %d fell back, %d escalated, %d escalations failed, %d unconfirmed, %d failed\n",
		retried, fellBack, strong, escFailed, unconfirmed, failed)

	// Model call latency by tier, as the classifier saw it.
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

	// Accuracy by trap.
	type acc struct{ right, of int }
	traps := map[string]*acc{}
	var names []string
	for _, o := range outs {
		tr := o.pair.Trap
		if tr == "" {
			tr = "(none)"
		}
		if traps[tr] == nil {
			traps[tr] = &acc{}
			names = append(names, tr)
		}
		traps[tr].of++
		if o.relation == o.pair.Class {
			traps[tr].right++
		}
	}
	sort.Strings(names)
	b.WriteString("right by trap:")
	for _, tr := range names {
		fmt.Fprintf(&b, " %s %d/%d", tr, traps[tr].right, traps[tr].of)
	}
	b.WriteString("\n")

	// The confidence bar for acting: contradicts as a label, and the flag on
	// decisions in force, at each contradicts threshold.
	b.WriteString("contradicts threshold sweep (label: precision/recall at confidence ≥ θ; flag: the judge's outcome on decisions in force):\n")
	for _, th := range []float64{0, 0.5, 0.6, 0.7, 0.8, 0.85, 0.9, 0.95} {
		var lab, fl prf
		for _, o := range outs {
			predicted := o.relation == ledger.RelationContradicts && o.confidence >= th
			switch labelled := o.pair.Class == ledger.RelationContradicts; {
			case labelled && predicted:
				lab.tp++
			case !labelled && predicted:
				lab.fp++
			case labelled && !predicted:
				lab.fn++
			}
			if !o.pair.Candidate.InForce || o.err != nil {
				continue
			}
			thr := judge.DefaultThresholds
			thr.Contradicts = th
			prop, cand := toJudge(o.pair)
			d := judge.Decide(ledger.JudgeProposal, prop.Kind == "decision", prop.Statement, []judge.Candidate{cand},
				[]judge.Pair{{Ref: cand.Ref, Relation: o.relation, Confidence: o.confidence, ExplicitChange: o.explicit,
					Unconfirmed: o.unconfirmed}}, thr)
			switch want, got := wantFlag(o.pair), d.Outcome == ledger.OutcomeFlagged; {
			case want && got:
				fl.tp++
			case !want && got:
				fl.fp++
			case want && !got:
				fl.fn++
			}
		}
		fmt.Fprintf(&b, "  θ %.2f: label %.2f/%.2f (tp %d fp %d fn %d), flag %.2f/%.2f (tp %d fp %d fn %d)\n", th,
			lab.precision(), lab.recall(), lab.tp, lab.fp, lab.fn, fl.precision(), fl.recall(), fl.tp, fl.fp, fl.fn)
	}

	b.WriteString("gateway:\n")
	b.WriteString(livemeter.Format(livemeter.Summarize(gateway, zdr)))
	return b.String()
}

// verdictRecord is one pair's verdict, without any memory text.
type verdictRecord struct {
	ID          string         `json:"id"`
	Class       string         `json:"class"`
	Trap        string         `json:"trap,omitempty"`
	InForce     bool           `json:"in_force"`
	Explicit    bool           `json:"explicit_label"`
	Relation    string         `json:"relation"`
	Confidence  float64        `json:"confidence"`
	Change      bool           `json:"explicit_change"`
	Tier        string         `json:"tier,omitempty"`
	Unconfirmed bool           `json:"unconfirmed,omitempty"`
	Outcome     string         `json:"outcome,omitempty"`
	Flagged     bool           `json:"flagged"`
	WantFlag    bool           `json:"want_flag"`
	Calls       map[string]int `json:"calls"`
	MS          int64          `json:"ms"`
	Error       string         `json:"error,omitempty"`
}

func writeVerdicts(path string, run liveRun, outs []outcome) error {
	recs := make([]verdictRecord, len(outs))
	for i, o := range outs {
		r := verdictRecord{ID: o.pair.ID, Class: string(o.pair.Class), Trap: o.pair.Trap, InForce: o.pair.Candidate.InForce,
			Explicit: o.pair.Explicit, Relation: string(o.relation), Confidence: o.confidence, Change: o.explicit, Tier: o.tier,
			Unconfirmed: o.unconfirmed, Outcome: string(o.decided), Flagged: o.flagged, WantFlag: wantFlag(o.pair) && o.pair.Candidate.InForce,
			Calls: o.calls, MS: o.took.Milliseconds()}
		if o.err != nil {
			r.Error = firstWords(o.err.Error(), 120)
		}
		recs[i] = r
	}
	hosts := map[string]any{}
	for _, tier := range []judge.Tier{run.cfg.Primary, run.cfg.Fallback, run.cfg.Strong} {
		if tier.Enabled() {
			hosts[tier.Model] = map[string]any{"only": tier.Routing.Providers, "quantizations": tier.Routing.Quantizations,
				"temperature": tier.Temperature}
		}
	}
	doc := map[string]any{
		"run": run.name, "date": time.Now().UTC().Format(time.RFC3339),
		"primary": run.cfg.Primary.Model, "primary_strict": run.cfg.Primary.Strict,
		"fallback": run.cfg.Fallback.Model, "strong": run.cfg.Strong.Model, "routing": hosts, "verdicts": recs,
	}
	b, err := json.MarshalIndent(doc, "", "  ")
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
