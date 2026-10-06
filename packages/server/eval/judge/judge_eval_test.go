// Package judgeeval scores the V2 judge (internal/judge) on labelled
// proposal/candidate pairs (plan 25 §11: contradicts precision ≥ 0.90,
// recall ≥ 0.75).
//
//	go test ./eval/judge/ -v                      # the set, stage 0, the harness on a fake model, the candidate sets (fake embedder)
//	JUDGE_EVAL_LIVE=1 go test ./eval/judge/ -v    # also the real model tiers (needs ANTHROPIC_API_KEY)
//	V2_EVAL_LIVE=1 go test ./eval/judge/ -run Candidates -v   # candidates and floors on voyage-4 (needs VOYAGE_API_KEY)
//
// The live run reads the same JUDGE_* configuration as the worker
// (judge.ConfigFromEnv) and scores each pair's relation, plus the outcome
// the judge would act on for decisions in force (flag or not).
package judgeeval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

type side struct {
	Statement string `json:"statement"`
	Kind      string `json:"kind"`
	Area      string `json:"area"`
	InForce   bool   `json:"in_force"`
}

type pair struct {
	ID        string          `json:"id"`
	Class     ledger.Relation `json:"class"`
	Stage0    string          `json:"stage0"`
	Trap      string          `json:"trap"`
	Explicit  bool            `json:"explicit"`
	Proposal  side            `json:"proposal"`
	Candidate side            `json:"candidate"`
}

// The bars of plan 25 §11.
const (
	minContradictsPrecision = 0.90
	minContradictsRecall    = 0.75
)

var classes = []ledger.Relation{ledger.RelationDuplicate, ledger.RelationUpdates, ledger.RelationExtends,
	ledger.RelationContradicts, ledger.RelationUnrelated}

func load(t *testing.T) []pair {
	t.Helper()
	raw, err := os.ReadFile("pairs.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Version     int    `json:"version"`
		Description string `json:"description"`
		Pairs       []pair `json:"pairs"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("pairs.json: %v", err)
	}
	return f.Pairs
}

func kind(s side) string {
	if s.Kind == "" {
		return string(ledger.KindFact)
	}
	return s.Kind
}

func TestPairsAreWellFormed(t *testing.T) {
	t.Parallel()
	pairs := load(t)
	if len(pairs) < 120 {
		t.Errorf("%d pairs; the set needs at least 120", len(pairs))
	}
	ids := map[string]bool{}
	byClass := map[ledger.Relation]int{}
	traps := map[string]int{}
	for _, p := range pairs {
		if ids[p.ID] {
			t.Errorf("%s: duplicate id", p.ID)
		}
		ids[p.ID] = true
		if !slices.Contains(classes, p.Class) {
			t.Errorf("%s: class %q", p.ID, p.Class)
		}
		byClass[p.Class]++
		traps[p.Trap]++
		if p.Proposal.Statement == "" || p.Candidate.Statement == "" {
			t.Errorf("%s: empty statement", p.ID)
		}
		if p.Stage0 != "" && p.Class != ledger.RelationDuplicate {
			t.Errorf("%s: only duplicates are found by stage 0", p.ID)
		}
		if p.Explicit && (p.Class != ledger.RelationUpdates || !p.Candidate.InForce || p.Proposal.Kind != "decision") {
			t.Errorf("%s: an explicit change is a decision updating a decision in force", p.ID)
		}
		for _, s := range []side{p.Proposal, p.Candidate} {
			if s.Kind != "" && s.Kind != "fact" && s.Kind != "decision" {
				t.Errorf("%s: kind %q", p.ID, s.Kind)
			}
			if s.InForce && s.Kind != "decision" {
				t.Errorf("%s: only a decision is in force", p.ID)
			}
		}
	}
	for _, c := range classes {
		if byClass[c] < 15 {
			t.Errorf("%s: %d pairs, want at least 15", c, byClass[c])
		}
	}
	for trap, least := range map[string]int{"number": 8, "date": 2, "qualifier": 4, "negation": 8, "reversal": 12, "paraphrase": 8, "verbatim": 10} {
		if traps[trap] < least {
			t.Errorf("trap %s: %d pairs, want at least %d", trap, traps[trap], least)
		}
	}
	t.Logf("%d pairs: %v; traps %v", len(pairs), byClass, traps)
}

// Stage 0 as the judge runs it: the exact hash, then LSH candidates
// verified by Jaccard and the salient-token guard. It must never fold a
// pair that isn't a duplicate (precision 1.0); its recall on duplicates is
// reported.
func TestStage0(t *testing.T) {
	t.Parallel()
	pairs := load(t)
	var caught, exact, near, duplicates, falseFolds, lshMisses int
	for _, p := range pairs {
		c := ledger.JudgeCandidate{Ref: "M-0002", Statement: p.Candidate.Statement, Lifecycle: "kept"}
		var exactSet, nearSet []ledger.JudgeCandidate
		if textsig.ContentSHA256(p.Proposal.Statement) == textsig.ContentSHA256(p.Candidate.Statement) {
			exactSet = append(exactSet, c)
		}
		if shareBand(textsig.BandsOf(p.Proposal.Statement), textsig.BandsOf(p.Candidate.Statement)) {
			nearSet = append(nearSet, c)
		} else if textsig.NearDuplicate(p.Proposal.Statement, p.Candidate.Statement) {
			lshMisses++ // a near-duplicate LSH didn't retrieve: a recall loss to watch
		}
		m, hit := judge.Stage0(p.Proposal.Statement, exactSet, nearSet)
		got := ""
		if hit {
			got = string(m.Stage)
		}
		if p.Class == ledger.RelationDuplicate {
			duplicates++
		}
		switch {
		case hit && p.Class != ledger.RelationDuplicate:
			falseFolds++
			t.Errorf("%s (%s, trap %q): stage 0 folded a non-duplicate: %q / %q", p.ID, p.Class, p.Trap,
				p.Proposal.Statement, p.Candidate.Statement)
		case got != p.Stage0:
			t.Errorf("%s: stage 0 found %q, the label says %q (jaccard %.2f, salient %v)", p.ID, got, p.Stage0,
				textsig.Jaccard(p.Proposal.Statement, p.Candidate.Statement), textsig.SameSalient(p.Proposal.Statement, p.Candidate.Statement))
		}
		if hit {
			caught++
			if m.Stage == ledger.StageExact {
				exact++
			} else {
				near++
			}
		}
	}
	precision := 1.0
	if caught > 0 {
		precision = float64(caught-falseFolds) / float64(caught)
	}
	t.Logf("stage 0: %d of %d duplicates folded (%d exact, %d near): recall %.2f, precision %.2f; "+
		"%d false folds; %d near-duplicates LSH missed",
		caught-falseFolds, duplicates, exact, near, float64(caught-falseFolds)/float64(duplicates), precision, falseFolds, lshMisses)
	if lshMisses > 0 {
		t.Errorf("LSH missed %d near-duplicates", lshMisses)
	}
}

func shareBand(a, b []int64) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// toJudge is a pair as the classifier sees it.
func toJudge(p pair) (judge.Proposal, judge.Candidate) {
	section := func(s side) string {
		if s.Kind == "decision" {
			return "decisions"
		}
		return "conventions"
	}
	prop := judge.Proposal{Ref: "M-0001", Statement: p.Proposal.Statement, Kind: kind(p.Proposal),
		Section: section(p.Proposal), Area: textsig.NormalizeKey(p.Proposal.Area)}
	area := textsig.NormalizeKey(p.Candidate.Area)
	keyed := p.Candidate.InForce && area != "" && (area == prop.Area || textsig.Mentions(p.Proposal.Statement, area))
	cand := judge.Candidate{Ref: "M-0002", Statement: p.Candidate.Statement, Kind: kind(p.Candidate),
		Section: section(p.Candidate), Area: area, InForce: p.Candidate.InForce, Keyed: keyed}
	return prop, cand
}

// wantFlag is what the judge should do with a pair whose candidate is a
// decision in force: flag a contradiction, or an implicit update.
func wantFlag(p pair) bool {
	return p.Candidate.InForce && (p.Class == ledger.RelationContradicts || (p.Class == ledger.RelationUpdates && !p.Explicit))
}

type outcome struct {
	pair     pair
	relation ledger.Relation
	flagged  bool
	err      error
}

// classifyAll runs the classifier over every pair, a few at a time.
func classifyAll(t *testing.T, c *judge.Classifier, pairs []pair, parallel int) []outcome {
	t.Helper()
	out := make([]outcome, len(pairs))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, p := range pairs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			prop, cand := toJudge(p)
			cls, err := c.Classify(context.Background(), prop, []judge.Candidate{cand})
			o := outcome{pair: p, err: err, relation: ledger.RelationNone}
			if err == nil && len(cls.Pairs) == 1 {
				o.relation = cls.Pairs[0].Relation
				d := judge.Decide(ledger.JudgeProposal, prop.Kind == "decision", prop.Statement, []judge.Candidate{cand},
					cls.Pairs, judge.DefaultThresholds)
				o.flagged = d.Outcome == ledger.OutcomeFlagged
			}
			out[i] = o
		}()
	}
	wg.Wait()
	return out
}

// prf is precision, recall and their counts.
type prf struct{ tp, fp, fn int }

func (s prf) precision() float64 {
	if s.tp+s.fp == 0 {
		return 1
	}
	return float64(s.tp) / float64(s.tp+s.fp)
}

func (s prf) recall() float64 {
	if s.tp+s.fn == 0 {
		return 1
	}
	return float64(s.tp) / float64(s.tp+s.fn)
}

// score counts, per class, the pairs labelled and predicted.
func score(outs []outcome) (map[ledger.Relation]prf, prf) {
	per := map[ledger.Relation]prf{}
	var flag prf
	for _, o := range outs {
		for _, c := range classes {
			s := per[c]
			switch {
			case o.pair.Class == c && o.relation == c:
				s.tp++
			case o.pair.Class != c && o.relation == c:
				s.fp++
			case o.pair.Class == c && o.relation != c:
				s.fn++
			}
			per[c] = s
		}
		if o.pair.Candidate.InForce {
			switch want := wantFlag(o.pair); {
			case want && o.flagged:
				flag.tp++
			case !want && o.flagged:
				flag.fp++
			case want && !o.flagged:
				flag.fn++
			}
		}
	}
	return per, flag
}

func report(t *testing.T, name string, outs []outcome) (map[ledger.Relation]prf, prf) {
	t.Helper()
	per, flag := score(outs)
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d pairs\n%-12s %9s %7s %4s %4s %4s\n", name, len(outs), "class", "precision", "recall", "tp", "fp", "fn")
	for _, c := range classes {
		s := per[c]
		fmt.Fprintf(&b, "%-12s %9.2f %7.2f %4d %4d %4d\n", c, s.precision(), s.recall(), s.tp, s.fp, s.fn)
	}
	fmt.Fprintf(&b, "%-12s %9.2f %7.2f %4d %4d %4d   (the judge's flag on decisions in force)\n", "flagged",
		flag.precision(), flag.recall(), flag.tp, flag.fp, flag.fn)
	var wrong []string
	for _, o := range outs {
		if o.err != nil {
			wrong = append(wrong, fmt.Sprintf("%s: error %v", o.pair.ID, o.err))
		} else if o.relation != o.pair.Class {
			wrong = append(wrong, fmt.Sprintf("%s: %s, labelled %s", o.pair.ID, o.relation, o.pair.Class))
		}
	}
	sort.Strings(wrong)
	if len(wrong) > 0 {
		fmt.Fprintf(&b, "misclassified:\n  %s\n", strings.Join(wrong, "\n  "))
	}
	t.Log(b.String())
	return per, flag
}

// oracle answers each pair with its label: the harness's own check. Its
// strong tier agrees, so escalation runs too.
type oracle struct{ byStatement map[string]pair }

func (o oracle) Complete(_ context.Context, c judge.Call) (string, error) {
	for stmt, p := range o.byStatement {
		if strings.Contains(c.Prompt, ">\n"+stmt+"\n</proposal>") {
			ans := map[string]any{"pairs": []map[string]any{{
				"candidate": "M-0002", "relation": string(p.Class), "confidence": 0.95, "explicit_change": p.Explicit,
				"rationale": "Labelled " + string(p.Class) + ".", "merged_statement": "",
			}}, "conditions": []any{}}
			b, err := json.Marshal(ans)
			return string(b), err
		}
	}
	return "", fmt.Errorf("no pair for this prompt")
}

func fakeTiers() judge.Config {
	return judge.Config{Primary: judge.Tier{Model: "fake-primary"}, Fallback: judge.Tier{Model: "fake-fallback", Strict: true},
		Strong: judge.Tier{Model: "fake-strong", Strict: true}}
}

// The harness, on a model that answers with the labels: every class
// scores 1.0, and the judge's flag follows the labels on decisions in
// force. This checks the prompt, parsing, escalation and scoring, not a
// model.
func TestHarnessOnAFakeModel(t *testing.T) {
	t.Parallel()
	pairs := load(t)
	byStatement := map[string]pair{}
	for _, p := range pairs {
		if _, dup := byStatement[p.Proposal.Statement]; dup {
			t.Fatalf("%s: two pairs share a proposal; the oracle can't tell them apart", p.ID)
		}
		byStatement[strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(p.Proposal.Statement)] = p
	}
	outs := classifyAll(t, judge.NewClassifier(oracle{byStatement}, fakeTiers()), pairs, 8)
	per, flag := report(t, "fake model", outs)
	for _, c := range classes {
		if s := per[c]; s.precision() != 1 || s.recall() != 1 {
			t.Errorf("%s: %.2f / %.2f on the labels themselves", c, s.precision(), s.recall())
		}
	}
	if flag.precision() != 1 || flag.recall() != 1 {
		t.Errorf("flag: %.2f / %.2f", flag.precision(), flag.recall())
	}
}

// The scorer, on a planted confusion matrix.
func TestScore(t *testing.T) {
	t.Parallel()
	mk := func(class, got ledger.Relation, inForce bool) outcome {
		return outcome{pair: pair{Class: class, Candidate: side{InForce: inForce, Kind: "decision"}}, relation: got,
			flagged: got == ledger.RelationContradicts && inForce}
	}
	outs := []outcome{
		mk(ledger.RelationContradicts, ledger.RelationContradicts, true),
		mk(ledger.RelationContradicts, ledger.RelationContradicts, true),
		mk(ledger.RelationContradicts, ledger.RelationExtends, true),
		mk(ledger.RelationExtends, ledger.RelationContradicts, true),
		mk(ledger.RelationUnrelated, ledger.RelationUnrelated, false),
	}
	per, flag := score(outs)
	c := per[ledger.RelationContradicts]
	if c.tp != 2 || c.fp != 1 || c.fn != 1 || c.precision() != 2.0/3 || c.recall() != 2.0/3 {
		t.Errorf("contradicts = %+v", c)
	}
	if per[ledger.RelationUnrelated].precision() != 1 || per[ledger.RelationDuplicate].recall() != 1 {
		t.Errorf("empty classes score 1: %+v", per)
	}
	if flag.tp != 2 || flag.fp != 1 || flag.fn != 1 {
		t.Errorf("flag = %+v", flag)
	}
}

// The real model tiers (JUDGE_EVAL_LIVE=1, ANTHROPIC_API_KEY): contradicts
// precision ≥ 0.90 and recall ≥ 0.75.
func TestLiveModel(t *testing.T) {
	if os.Getenv("JUDGE_EVAL_LIVE") != "1" {
		t.Skip("set JUDGE_EVAL_LIVE=1 (with ANTHROPIC_API_KEY, and ANTHROPIC_BASE_URL for OpenRouter) to score the real model")
	}
	client := anthropic.NewFromEnv()
	if client == nil {
		t.Fatal("JUDGE_EVAL_LIVE=1 needs ANTHROPIC_API_KEY")
	}
	cfg := judge.ConfigFromEnv(os.LookupEnv)
	c := judge.NewClassifier(judge.NewAnthropicModel(client, cfg.ZeroDataRetention), cfg)
	if c == nil {
		t.Fatal("JUDGE_MODEL is off")
	}
	start := time.Now()
	outs := classifyAll(t, c, load(t), 4)
	t.Logf("tiers: primary %s, fallback %s, strong %s; %s", cfg.Primary.Model, cfg.Fallback.Model, cfg.Strong.Model, time.Since(start).Round(time.Second))
	per, _ := report(t, "live model", outs)
	con := per[ledger.RelationContradicts]
	if con.precision() < minContradictsPrecision || con.recall() < minContradictsRecall {
		t.Errorf("contradicts: precision %.2f (bar %.2f), recall %.2f (bar %.2f)", con.precision(), minContradictsPrecision,
			con.recall(), minContradictsRecall)
	}
}
