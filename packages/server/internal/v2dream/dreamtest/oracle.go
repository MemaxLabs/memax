// Package dreamtest is a deterministic stand-in for Dream's model tiers,
// for tests and the eval (eval/dream): an oracle that answers Dream's fold
// and Brief prompts and the judge's classifier from rules written as data,
// matching record text by substring, the way a labelled fixture says what
// each phase should do.
package dreamtest

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"regexp"
	"strings"
	"sync"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
)

// FoldRule: a note containing Note folds into the memory containing Memory.
type FoldRule struct {
	Note, Memory string
	Confidence   float64
}

// FactRule: the notes containing any of Notes state a new fact.
type FactRule struct {
	Notes                    []string
	Statement, Section, Kind string
	Confidence               float64
}

// PairRule: a statement containing A relates to a candidate containing B.
type PairRule struct {
	A, B       string
	Relation   string
	Confidence float64
	Explicit   bool
}

// BriefRule is one change to the Brief; Item, After and Cites name
// memories by a substring of their words (or a prose id, P:…).
type BriefRule struct {
	Op, Item, Section, After, Text string
	Cites                          []string
}

// Oracle answers from its rules.
type Oracle struct {
	Folds []FoldRule
	Facts []FactRule
	Pairs []PairRule
	Brief []BriefRule

	// Fail makes every call fail (a model that is down).
	Fail bool

	mu    sync.Mutex
	calls []judge.Call
}

// Calls returns the calls made, in order.
func (o *Oracle) Calls() []judge.Call {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]judge.Call(nil), o.calls...)
}

// CallsFor counts the calls whose system prompt starts with prefix
// ("You are Dream, the overnight": folds; "You are Dream, keeping": the
// Brief; "You are the judge": the classifier).
func (o *Oracle) CallsFor(prefix string) int {
	n := 0
	for _, c := range o.Calls() {
		if strings.HasPrefix(c.System, prefix) {
			n++
		}
	}
	return n
}

var (
	noteRe      = regexp.MustCompile(`(?s)<note id="(n\d+)"[^>]*>(.*?)</note>`)
	memoryRe    = regexp.MustCompile(`(?s)<memory id="(M-\d+)"[^>]*>(.*?)</memory>`)
	proseRe     = regexp.MustCompile(`(?s)<prose id="(P:[^"]+)"[^>]*>(.*?)</prose>`)
	proposalRe  = regexp.MustCompile(`(?s)<proposal [^>]*>\n(.*?)\n</proposal>`)
	candidateRe = regexp.MustCompile(`(?s)<candidate id="(M-\d+)"[^>]*>\n(.*?)\n</candidate>`)
)

// Complete implements judge.Model.
func (o *Oracle) Complete(_ context.Context, c judge.Call) (string, error) {
	o.mu.Lock()
	o.calls = append(o.calls, c)
	fail := o.Fail
	o.mu.Unlock()
	if fail {
		return "", errors.New("dreamtest: the model is down")
	}
	switch {
	case strings.HasPrefix(c.System, "You are Dream, the overnight"):
		return o.fold(c.Prompt), nil
	case strings.HasPrefix(c.System, "You are Dream, keeping"):
		return o.brief(c.Prompt), nil
	}
	return o.classify(c.Prompt), nil
}

func contains(text, sub string) bool {
	return sub != "" && strings.Contains(strings.ToLower(html.UnescapeString(text)), strings.ToLower(sub))
}

func (o *Oracle) fold(prompt string) string {
	notes := noteRe.FindAllStringSubmatch(prompt, -1)
	mems := memoryRe.FindAllStringSubmatch(prompt, -1)
	type fold struct {
		Memory     string   `json:"memory"`
		Notes      []string `json:"notes"`
		Confidence float64  `json:"confidence"`
	}
	type fact struct {
		Statement  string   `json:"statement"`
		Section    string   `json:"section"`
		Kind       string   `json:"kind"`
		Notes      []string `json:"notes"`
		Confidence float64  `json:"confidence"`
	}
	out := struct {
		Folds   []fold   `json:"folds"`
		Facts   []fact   `json:"facts"`
		Skipped []string `json:"skipped"`
	}{Folds: []fold{}, Facts: []fact{}, Skipped: []string{}}
	used := map[string]bool{}
	for _, r := range o.Folds {
		for _, m := range mems {
			if !contains(m[2], r.Memory) {
				continue
			}
			var ids []string
			for _, n := range notes {
				if contains(n[2], r.Note) {
					ids = append(ids, n[1])
					used[n[1]] = true
				}
			}
			if len(ids) > 0 {
				out.Folds = append(out.Folds, fold{Memory: m[1], Notes: ids, Confidence: conf(r.Confidence)})
			}
			break
		}
	}
	for _, r := range o.Facts {
		var ids []string
		for _, n := range notes {
			for _, s := range r.Notes {
				if contains(n[2], s) && !containsStr(ids, n[1]) {
					ids = append(ids, n[1])
					used[n[1]] = true
				}
			}
		}
		if len(ids) > 0 {
			kind := r.Kind
			if kind == "" {
				kind = "fact"
			}
			out.Facts = append(out.Facts, fact{Statement: r.Statement, Section: r.Section, Kind: kind, Notes: ids, Confidence: conf(r.Confidence)})
		}
	}
	for _, n := range notes {
		if !used[n[1]] {
			out.Skipped = append(out.Skipped, n[1])
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func (o *Oracle) classify(prompt string) string {
	p := ""
	if m := proposalRe.FindStringSubmatch(prompt); m != nil {
		p = m[1]
	}
	type pair struct {
		Candidate       string  `json:"candidate"`
		Relation        string  `json:"relation"`
		Confidence      float64 `json:"confidence"`
		ExplicitChange  bool    `json:"explicit_change"`
		Rationale       string  `json:"rationale"`
		MergedStatement string  `json:"merged_statement"`
		Question        string  `json:"question"`
		Labels          struct {
			Proposal string `json:"proposal"`
			Decision string `json:"decision"`
			Both     string `json:"both"`
			Open     string `json:"open"`
		} `json:"labels"`
		Suggested string `json:"suggested"`
	}
	out := struct {
		Pairs      []pair `json:"pairs"`
		Conditions []any  `json:"conditions"`
	}{Pairs: []pair{}, Conditions: []any{}}
	for _, c := range candidateRe.FindAllStringSubmatch(prompt, -1) {
		v := pair{Candidate: c[1], Relation: "unrelated", Confidence: 0.9, Rationale: "Compared with " + c[1] + ".", Suggested: "none"}
		for _, r := range o.Pairs {
			if (contains(p, r.A) && contains(c[2], r.B)) || (contains(p, r.B) && contains(c[2], r.A)) {
				v.Relation, v.Confidence, v.ExplicitChange = r.Relation, conf(r.Confidence), r.Explicit
				break
			}
		}
		out.Pairs = append(out.Pairs, v)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func (o *Oracle) brief(prompt string) string {
	mems := memoryRe.FindAllStringSubmatch(prompt, -1)
	prose := proseRe.FindAllStringSubmatch(prompt, -1)
	ref := func(sub string) string {
		if strings.HasPrefix(sub, "P:") || sub == "" {
			return sub
		}
		for _, m := range mems {
			if contains(m[2], sub) {
				return m[1]
			}
		}
		for _, p := range prose {
			if contains(p[2], sub) {
				return p[1]
			}
		}
		return "M-9999"
	}
	type op struct {
		Op       string   `json:"op"`
		Item     string   `json:"item"`
		Section  string   `json:"section"`
		After    string   `json:"after"`
		Text     string   `json:"text"`
		Cites    []string `json:"cites"`
		Evidence string   `json:"evidence"`
	}
	out := struct {
		Ops []op `json:"ops"`
	}{Ops: []op{}}
	for _, r := range o.Brief {
		cites := []string{}
		for _, c := range r.Cites {
			cites = append(cites, ref(c))
		}
		out.Ops = append(out.Ops, op{Op: r.Op, Item: ref(r.Item), Section: r.Section, After: ref(r.After), Text: r.Text,
			Cites: cites, Evidence: "Rests on what it cites."})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func conf(c float64) float64 {
	if c == 0 {
		return 0.95
	}
	return c
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
