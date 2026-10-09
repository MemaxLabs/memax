package judge_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sync"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// fakeModel is a deterministic stand-in for the model tiers. answer says
// what each tier returns for its n-th call (0-based); the calls are kept.
type fakeModel struct {
	mu     sync.Mutex
	answer func(c judge.Call, n int) (string, error)
	calls  []judge.Call
}

func (f *fakeModel) Complete(_ context.Context, c judge.Call) (string, error) {
	f.mu.Lock()
	n := 0
	for _, x := range f.calls {
		if x.Tier.Name == c.Tier.Name {
			n++
		}
	}
	f.calls = append(f.calls, c)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return "", errors.New("no answer configured")
	}
	return answer(c, n)
}

func (f *fakeModel) tiers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Tier.Name
	}
	return out
}

// verdict is what the fake says about one candidate.
type verdict struct {
	relation   ledger.Relation
	confidence float64
	explicit   bool
	merged     string
}

var candidateRef = regexp.MustCompile(`<candidate id="(M-\d+)"`)

// oracle answers every call from a table of verdicts by candidate ref;
// any other candidate is unrelated. conds are returned as conditions.
func oracle(table map[string]verdict, conds ...judge.Condition) func(judge.Call, int) (string, error) {
	return func(c judge.Call, _ int) (string, error) {
		return answerFor(c.Prompt, table, conds), nil
	}
}

// fakeQuestion is the fake's question about a pair it finds contradicting,
// and fakeLabels its labels.
func fakeQuestion(ref string) string { return "Which holds, the proposal or " + ref + "?" }

var fakeLabels = ledger.ConflictLabels{Proposal: "The proposal", Decision: "As kept", Both: "Both, each scoped", Open: "Leave it open"}

func answerFor(prompt string, table map[string]verdict, conds []judge.Condition) string {
	type pair struct {
		Candidate       string                `json:"candidate"`
		Relation        string                `json:"relation"`
		Confidence      float64               `json:"confidence"`
		ExplicitChange  bool                  `json:"explicit_change"`
		Rationale       string                `json:"rationale"`
		MergedStatement string                `json:"merged_statement"`
		Question        string                `json:"question"`
		Labels          ledger.ConflictLabels `json:"labels"`
		Suggested       string                `json:"suggested"`
	}
	out := struct {
		Pairs      []pair            `json:"pairs"`
		Conditions []judge.Condition `json:"conditions"`
	}{Pairs: []pair{}, Conditions: conds}
	if out.Conditions == nil {
		out.Conditions = []judge.Condition{}
	}
	for _, m := range candidateRef.FindAllStringSubmatch(prompt, -1) {
		v, ok := table[m[1]]
		if !ok {
			v = verdict{relation: ledger.RelationUnrelated, confidence: 0.9}
		}
		p := pair{Candidate: m[1], Relation: string(v.relation), Confidence: v.confidence,
			ExplicitChange: v.explicit, Rationale: "Compared with " + m[1] + ".", MergedStatement: v.merged, Suggested: "none"}
		if v.relation == ledger.RelationContradicts || v.relation == ledger.RelationUpdates {
			p.Question, p.Labels, p.Suggested = fakeQuestion(m[1]), fakeLabels, ledger.SuggestBoth
		}
		out.Pairs = append(out.Pairs, p)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
