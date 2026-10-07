// Package importseval scores the import conflict check (internal/judge,
// imports.go) on labelled import batches (plan 25 §11, Import cleanup:
// every planted conflict found).
//
//	go test ./eval/imports/ -v                               # the sets, the scorer, the harness on a fake model
//	IMPORT_EVAL_LIVE=1 go test ./eval/imports/ -run Live -v  # also the real tiers (needs ANTHROPIC_API_KEY); see live_test.go
//
// A batch is one import as `memax init` sends it (packages/cli,
// src/lib/init): the statements its agent files split into, file by file
// in init's order, with their file:line, the headings above them and the
// file's kind, which sets their section, kind and trust. The harness
// builds the import's proposals as the ledger does (ledger.FoldImportItems
// folds repeats into one proposal citing each file) and hands them to the
// check as ledger.ImportCheckSnapshot would (judge.ImportCandidate), then
// scores the model's groups against the labels at every bar. Results and
// the labelling rules are in RESULTS.md.
package importseval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The labelled sets: the one tuned on, and the one written before any
// tuning.
const (
	mainSet    = "batches.json"
	holdoutSet = "holdout.json"
)

type evalSet struct {
	Version     int     `json:"version"`
	Description string  `json:"description"`
	Batches     []batch `json:"batches"`
}

type batch struct {
	ID string `json:"id"`
	// Space is project (repository files) or personal (this machine's).
	Space     string  `json:"space"`
	About     string  `json:"about"`
	Files     []file  `json:"files"`
	Conflicts []label `json:"conflicts"`
	Traps     []label `json:"traps"`
}

type file struct {
	// Path is the file as init labels it ("AGENTS.md", "~/.claude/CLAUDE.md").
	Path string `json:"path"`
	// Kind is init's FileKind (packages/cli src/lib/init/files.ts).
	Kind string `json:"kind"`
	// Globs are where the file applies, from its frontmatter (globs,
	// applyTo, paths): the proposals' scope.
	Globs      []string    `json:"globs,omitempty"`
	Statements []statement `json:"statements"`
}

type statement struct {
	Line int `json:"line"`
	// Under is the headings above it, outermost first, joined with " › "
	// as init's locator joins them. It sets the section (sectionFor); the
	// check doesn't see it.
	Under string `json:"under,omitempty"`
	Text  string `json:"text"`
}

// label is a planted conflict (Subject) or a trap (Trap). Members are
// statement refs ("AGENTS.md:5").
type label struct {
	ID      string `json:"id"`
	Subject string `json:"subject,omitempty"`
	// Hinge says what a conflict turns on, when it is a number, a date or a
	// version.
	Hinge string `json:"hinge,omitempty"`
	Trap  string `json:"trap,omitempty"`
	// Members must all be in one group for a conflict to count as found.
	Members []string `json:"members"`
	// Also are statements that take a side in the conflict: a group may
	// hold them without penalty, and needn't.
	Also []string `json:"also,omitempty"`
	Note string   `json:"note"`
}

var (
	subjects = []string{"test", "build", "lint", "package_manager", "version", "deploy", "error_format", "style",
		"preference", "schedule", "architecture", "api", "data", "other"}
	hinges    = []string{"", "number", "date", "version"}
	trapKinds = []string{"scope", "complementary", "repeat", "number", "date", "version"}
)

// fileKind is what init does with a file of a kind: where it goes, at
// what trust, and in what order findFiles reads it.
type fileKind struct {
	home  bool
	trust policy.Trust
	order int
}

var fileKinds = map[string]fileKind{
	"agents_md":            {false, policy.TrustRepository, 1},
	"claude_md":            {false, policy.TrustRepository, 2},
	"claude_rule":          {false, policy.TrustRepository, 3},
	"cursor_rule":          {false, policy.TrustRepository, 4},
	"cursorrules":          {false, policy.TrustRepository, 5},
	"copilot_instructions": {false, policy.TrustRepository, 6},
	"copilot_scoped":       {false, policy.TrustRepository, 7},
	"gemini_md":            {false, policy.TrustRepository, 8},
	"windsurf_rule":        {false, policy.TrustRepository, 9},
	"devin_rule":           {false, policy.TrustRepository, 10},
	"claude_local":         {true, policy.TrustPerson, 11},
	"claude_user":          {true, policy.TrustPerson, 12},
	"claude_memory":        {true, policy.TrustAgentOwnWork, 13},
	"codex_user":           {true, policy.TrustPerson, 14},
	"codex_memory":         {true, policy.TrustAgentOwnWork, 15},
	"gemini_user":          {true, policy.TrustAgentOwnWork, 16},
	"gemini_memory":        {true, policy.TrustAgentOwnWork, 17},
}

// sectionFor is init's sectionFor (packages/cli src/lib/init/split.ts):
// the innermost heading that names a section decides it, else the file's
// location does.
var (
	decisionHeading   = regexp.MustCompile(`\bdecisions?\b|\badrs?\b|\bdecided\b`)
	questionHeading   = regexp.MustCompile(`\bopen questions?\b|\bquestions\b|\btodo\b|\bto do\b|\bunresolved\b|\btbd\b`)
	preferenceHeading = regexp.MustCompile(`\bpreferences?\b|\bprefer\b|\babout me\b|\bpersonal\b|\btone\b`)
)

func sectionFor(under string, home bool) (ledger.Section, ledger.Kind) {
	var hs []string
	if under != "" {
		hs = strings.Split(under, " › ")
	}
	for i := len(hs) - 1; i >= 0; i-- {
		h := strings.ToLower(hs[i])
		switch {
		case decisionHeading.MatchString(h):
			return ledger.SectionDecisions, ledger.KindDecision
		case questionHeading.MatchString(h):
			return ledger.SectionOpenQuestion, ledger.KindFact
		case preferenceHeading.MatchString(h):
			return ledger.SectionPreferences, ledger.KindFact
		}
	}
	if home {
		return ledger.SectionPreferences, ledger.KindFact
	}
	return ledger.SectionConventions, ledger.KindFact
}

func loadSet(t *testing.T, name string) []batch {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var s evalSet
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s.Batches
}

// planted is a label resolved to proposals.
type planted struct {
	label
	core    []int        // the proposals its members became, each once
	allowed map[int]bool // core and also
}

// proposal is one of an import's proposals: what the check sees, and the
// statements folded into it.
type proposal struct {
	cand  ledger.ImportCandidate
	refs  []string
	trust policy.Trust
}

// built is a batch as the check receives it.
type built struct {
	batch
	props     []proposal
	byID      map[uuid.UUID]int
	byRef     map[string]int // statement ref → proposal
	conflicts []planted
	traps     []planted
}

func (b *built) candidates() []ledger.ImportCandidate {
	out := make([]ledger.ImportCandidate, len(b.props))
	for i, p := range b.props {
		out[i] = p.cand
	}
	return out
}

// conflictOf is the planted conflict whose core holds proposal i, if any.
func (b *built) conflictOf(i int) *planted {
	for k := range b.conflicts {
		if slices.Contains(b.conflicts[k].core, i) {
			return &b.conflicts[k]
		}
	}
	return nil
}

// innocent reports whether proposal i takes no side in any planted
// conflict: a group that holds it holds it wrongly.
func (b *built) innocent(i int) bool {
	for _, c := range b.conflicts {
		if c.allowed[i] {
			return false
		}
	}
	return true
}

// bulk reports whether proposal i could be kept in bulk if nothing flagged
// it: `memax init` offers only repository and person statements
// (bulkCandidates, src/lib/init/settle.ts).
func (b *built) bulk(i int) bool {
	t := b.props[i].trust
	return t == policy.TrustRepository || t == policy.TrustPerson
}

var idSpace = uuid.MustParse("6f0c2a52-5d0e-4f43-9a55-8f7e4c0b9a10")

// build turns a batch into its proposals, as `memax init` uploads it and
// the ledger folds and snapshots it, and resolves its labels.
func build(b batch) (*built, error) {
	var items []ledger.ImportItem
	var trusts []policy.Trust
	var globs [][]string
	for fi, f := range b.Files {
		fk, ok := fileKinds[f.Kind]
		if !ok {
			return nil, fmt.Errorf("%s: file kind %q", f.Path, f.Kind)
		}
		loc := ledger.ImportRepository
		if fk.home {
			loc = ledger.ImportHome
		}
		var applies json.RawMessage
		if len(f.Globs) > 0 {
			applies, _ = json.Marshal(map[string]any{"paths": f.Globs})
		}
		for _, st := range f.Statements {
			ref := fmt.Sprintf("%s:%d", f.Path, st.Line)
			section, kind := sectionFor(st.Under, fk.home)
			items = append(items, ledger.ImportItem{Key: fmt.Sprintf("f%dl%d", fi, st.Line), Ref: ref, Location: loc,
				NewMemory: ledger.NewMemory{Statement: st.Text, Section: section, Kind: kind, Applies: applies,
					Sources: []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: ref, Trust: fk.trust}}}})
			trusts = append(trusts, fk.trust)
			globs = append(globs, f.Globs)
		}
	}
	bt := &built{batch: b, byID: map[uuid.UUID]int{}, byRef: map[string]int{}}
	for gi, g := range ledger.FoldImportItems(items) {
		lead := items[g[0]]
		p := proposal{trust: trusts[g[0]]}
		p.cand = ledger.ImportCandidate{ID: uuid.NewSHA1(idSpace, []byte(b.ID+"\x00"+lead.Ref)), Ref: fmt.Sprintf("M-%04d", gi+1),
			Statement: lead.Statement, Kind: lead.Kind, Section: lead.Section, Paths: globs[g[0]], Trust: trusts[g[0]]}
		for _, i := range g {
			p.refs = append(p.refs, items[i].Ref)
			p.cand.Sources = append(p.cand.Sources, items[i].Ref)
			p.trust = policy.MinTrust(p.trust, trusts[i])
			bt.byRef[items[i].Ref] = gi
		}
		p.cand.Trust = p.trust
		bt.byID[p.cand.ID] = gi
		bt.props = append(bt.props, p)
	}
	resolve := func(l label) (planted, error) {
		pl := planted{label: l, allowed: map[int]bool{}}
		for _, r := range l.Members {
			i, ok := bt.byRef[r]
			if !ok {
				return pl, fmt.Errorf("%s: no statement %s", l.ID, r)
			}
			if slices.Contains(pl.core, i) {
				return pl, fmt.Errorf("%s: %s folds into another member's proposal", l.ID, r)
			}
			pl.core = append(pl.core, i)
			pl.allowed[i] = true
		}
		for _, r := range l.Also {
			i, ok := bt.byRef[r]
			if !ok {
				return pl, fmt.Errorf("%s: no statement %s", l.ID, r)
			}
			pl.allowed[i] = true
		}
		if len(pl.core) < 2 {
			return pl, fmt.Errorf("%s: fewer than two members", l.ID)
		}
		return pl, nil
	}
	for _, l := range b.Conflicts {
		pl, err := resolve(l)
		if err != nil {
			return nil, err
		}
		bt.conflicts = append(bt.conflicts, pl)
	}
	for _, l := range b.Traps {
		pl, err := resolve(l)
		if err != nil {
			return nil, err
		}
		bt.traps = append(bt.traps, pl)
	}
	return bt, nil
}

func buildSet(t *testing.T, name string) []*built {
	t.Helper()
	var out []*built
	for _, b := range loadSet(t, name) {
		bt, err := build(b)
		if err != nil {
			t.Fatalf("%s: %s: %v", name, b.ID, err)
		}
		out = append(out, bt)
	}
	return out
}

// checkSet checks a set against the labelling rules and counts what it
// holds.
func checkSet(t *testing.T, name string) setCounts {
	t.Helper()
	var n setCounts
	n.subjects, n.hinges, n.traps = map[string]int{}, map[string]int{}, map[string]int{}
	ids := map[string]bool{}
	for _, bt := range buildSet(t, name) {
		if ids[bt.ID] {
			t.Errorf("%s: batch %s twice", name, bt.ID)
		}
		ids[bt.ID] = true
		if bt.Space != "project" && bt.Space != "personal" {
			t.Errorf("%s: space %q", bt.ID, bt.Space)
		}
		order := 0
		for _, f := range bt.Files {
			fk := fileKinds[f.Kind]
			if fk.home != (bt.Space == "personal") {
				t.Errorf("%s: %s (%s) doesn't go to a %s space: init sends repository files to the project's space and "+
					"this machine's to Personal", bt.ID, f.Path, f.Kind, bt.Space)
			}
			if fk.order < order {
				t.Errorf("%s: %s comes before files init reads first", bt.ID, f.Path)
			}
			order = fk.order
			line := 0
			for _, st := range f.Statements {
				if st.Line <= line {
					t.Errorf("%s: %s:%d: lines must increase", bt.ID, f.Path, st.Line)
				}
				line = st.Line
				if strings.TrimSpace(st.Text) == "" {
					t.Errorf("%s: %s:%d: no words", bt.ID, f.Path, st.Line)
				}
			}
		}
		labelIDs := map[string]bool{}
		for _, c := range bt.conflicts {
			if labelIDs[c.ID] {
				t.Errorf("%s: label %s twice", bt.ID, c.ID)
			}
			labelIDs[c.ID] = true
			if !slices.Contains(subjects, c.Subject) || c.Trap != "" {
				t.Errorf("%s: subject %q", c.ID, c.Subject)
			}
			if !slices.Contains(hinges, c.Hinge) {
				t.Errorf("%s: hinge %q", c.ID, c.Hinge)
			}
			for _, i := range c.core {
				if other := bt.conflictOf(i); other.ID != c.ID {
					t.Errorf("%s: %s is also a member of %s", c.ID, bt.props[i].refs[0], other.ID)
				}
			}
			n.conflicts++
			n.subjects[c.Subject]++
			n.hinges[c.Hinge]++
			if len(c.core) > 2 {
				n.threeWay++
			}
		}
		for _, tr := range bt.traps {
			if labelIDs[tr.ID] {
				t.Errorf("%s: label %s twice", bt.ID, tr.ID)
			}
			labelIDs[tr.ID] = true
			if !slices.Contains(trapKinds, tr.Trap) || tr.Subject != "" || tr.Hinge != "" || len(tr.Also) > 0 {
				t.Errorf("%s: trap %q", tr.ID, tr.Trap)
			}
			// A trap is sprung by a group holding two of its members, so no
			// two of them may take sides in the same planted conflict.
			for _, c := range bt.conflicts {
				in := 0
				for _, i := range tr.core {
					if c.allowed[i] {
						in++
					}
				}
				if in > 1 {
					t.Errorf("%s: two of its members are in %s", tr.ID, c.ID)
				}
			}
			n.trapCount++
			n.traps[tr.Trap]++
		}
		n.statements += len(bt.byRef)
		n.proposals += len(bt.props)
		n.folded += len(bt.byRef) - len(bt.props)
		n.largest = max(n.largest, len(bt.props))
		n.batches++
	}
	return n
}

type setCounts struct {
	batches, statements, proposals, folded, largest int
	conflicts, threeWay, trapCount                  int
	subjects, hinges, traps                         map[string]int
}

func (n setCounts) String() string {
	return fmt.Sprintf("%d batches, %d statements, %d proposals (%d folded), the largest %d; %d conflicts (%d of three), "+
		"subjects %v, hinges %v; %d traps %v", n.batches, n.statements, n.proposals, n.folded, n.largest, n.conflicts,
		n.threeWay, n.subjects, n.hinges, n.trapCount, n.traps)
}

func TestBatchesAreWellFormed(t *testing.T) {
	t.Parallel()
	n := checkSet(t, mainSet)
	t.Log(n)
	if n.largest <= judge.ImportBatch {
		t.Errorf("the largest batch has %d proposals; one needs more than %d (judge.ImportBatch) to exercise the chunking",
			n.largest, judge.ImportBatch)
	}
	if n.conflicts < 40 || n.threeWay < 3 || n.trapCount < 40 {
		t.Errorf("%d conflicts (%d of three), %d traps: want at least 40, 3 and 40", n.conflicts, n.threeWay, n.trapCount)
	}
	for s, least := range map[string]int{"test": 3, "build": 2, "lint": 3, "package_manager": 3, "version": 4, "deploy": 3,
		"error_format": 2, "style": 4, "preference": 2} {
		if n.subjects[s] < least {
			t.Errorf("subject %s: %d conflicts, want at least %d", s, n.subjects[s], least)
		}
	}
	for h, least := range map[string]int{"number": 3, "date": 3, "version": 4} {
		if n.hinges[h] < least {
			t.Errorf("hinge %s: %d conflicts, want at least %d", h, n.hinges[h], least)
		}
	}
	for tr, least := range map[string]int{"scope": 6, "complementary": 5, "repeat": 6, "number": 4, "date": 3, "version": 3} {
		if n.traps[tr] < least {
			t.Errorf("trap %s: %d, want at least %d", tr, n.traps[tr], least)
		}
	}
}

// The held-out set follows the same rules and shares no batch or statement
// with batches.json, so a prompt tuned on one can be checked on the other.
func TestHoldoutIsWellFormed(t *testing.T) {
	t.Parallel()
	n := checkSet(t, holdoutSet)
	t.Log(n)
	if n.conflicts < 12 || n.trapCount < 12 {
		t.Errorf("%d conflicts, %d traps: want at least 12 of each", n.conflicts, n.trapCount)
	}
	seen := map[string]bool{}
	for _, b := range loadSet(t, mainSet) {
		seen["batch:"+b.ID] = true
		for _, f := range b.Files {
			for _, st := range f.Statements {
				seen[st.Text] = true
			}
		}
	}
	for _, b := range loadSet(t, holdoutSet) {
		if seen["batch:"+b.ID] {
			t.Errorf("%s: also a batch of %s", b.ID, mainSet)
		}
		for _, f := range b.Files {
			for _, st := range f.Statements {
				if seen[st.Text] {
					t.Errorf("%s: %s:%d is word for word in %s", b.ID, f.Path, st.Line, mainSet)
				}
			}
		}
	}
}

// The scoring. A planted conflict is found when one group holds all of its
// members. Each group gets a credit in [0, 1], and precision is the mean
// credit over the groups:
//
//   - exact (1): it holds every member of one planted conflict and nothing
//     but members and also-members of it;
//   - partial (1): it holds two or more of one conflict's members and
//     nothing else, but not all (a three-way conflict found as two): every
//     statement in it does disagree, so it is right as far as it goes; the
//     missing member counts against recall instead;
//   - merged: it holds statements of two or more planted conflicts and
//     nothing else (two conflicts a person must settle as one);
//   - extra: it holds a conflict and statements that take no side in any;
//   - false (0): no two of its statements are members of one conflict.
//
// A merged or extra group's credit is the share of its statements that
// belong to the conflict it holds most of (members and also-members), at
// least two of whose members it must hold; so a group of two two-way
// conflicts scores 0.5, and a two-way conflict plus one bystander 2/3.
//
// The cost of a wrong group is the statements it holds out of bulk keep:
// held is the share of the statements that take no side in any planted
// conflict, and that `memax init` would otherwise offer to keep in bulk,
// that some group flags.
type groupScore struct {
	members    []int
	confidence float64
	class      string
	credit     float64
	conflict   string // the planted conflict it holds most of
	subject    string
}

const (
	classExact   = "exact"
	classPartial = "partial"
	classMerged  = "merged"
	classExtra   = "extra"
	classFalse   = "false"
)

var groupClasses = []string{classExact, classPartial, classMerged, classExtra, classFalse}

func scoreGroup(b *built, members []int) groupScore {
	g := groupScore{members: members, class: classFalse}
	best, bestAllowed := -1, 0
	for k, c := range b.conflicts {
		core, allowed := 0, 0
		for _, i := range members {
			if slices.Contains(c.core, i) {
				core++
			}
			if c.allowed[i] {
				allowed++
			}
		}
		if core >= 2 && allowed > bestAllowed {
			best, bestAllowed = k, allowed
		}
	}
	if best < 0 {
		return g
	}
	c := b.conflicts[best]
	g.conflict = c.ID
	g.credit = float64(bestAllowed) / float64(len(members))
	if bestAllowed == len(members) {
		g.class = classPartial
		if all(c.core, func(i int) bool { return slices.Contains(members, i) }) {
			g.class = classExact
		}
		return g
	}
	g.class = classMerged
	for _, i := range members {
		if b.innocent(i) {
			g.class = classExtra
		}
	}
	return g
}

func all[T any](s []T, f func(T) bool) bool {
	for _, x := range s {
		if !f(x) {
			return false
		}
	}
	return true
}

// batchScore is one batch's groups at one bar, scored.
type batchScore struct {
	groups []groupScore
	found  map[string]bool // conflict → found
	sprung map[string]bool // trap → two of its members in one group
	// held counts the bystanders a group flags, of bystanders: those memax
	// init would offer to keep in bulk, and all of them.
	held, bystanders       int
	heldAll, bystandersAll int
}

// score scores the groups the check would record.
func score(b *built, conflicts []ledger.ImportConflictInput) batchScore {
	s := batchScore{found: map[string]bool{}, sprung: map[string]bool{}}
	grouped := map[int]bool{}
	var sets [][]int
	for _, c := range conflicts {
		var members []int
		for _, id := range c.Members {
			if i, ok := b.byID[id]; ok && !slices.Contains(members, i) {
				members = append(members, i)
				grouped[i] = true
			}
		}
		sets = append(sets, members)
		g := scoreGroup(b, members)
		if c.Confidence != nil {
			g.confidence = *c.Confidence
		}
		g.subject = c.Subject
		s.groups = append(s.groups, g)
	}
	for _, c := range b.conflicts {
		s.found[c.ID] = slices.ContainsFunc(sets, func(set []int) bool {
			return all(c.core, func(i int) bool { return slices.Contains(set, i) })
		})
	}
	for _, tr := range b.traps {
		s.sprung[tr.ID] = slices.ContainsFunc(sets, func(set []int) bool {
			n := 0
			for _, i := range tr.core {
				if slices.Contains(set, i) {
					n++
				}
			}
			return n >= 2
		})
	}
	for i := range b.props {
		if !b.innocent(i) {
			continue
		}
		s.bystandersAll++
		if grouped[i] {
			s.heldAll++
		}
		if b.bulk(i) {
			s.bystanders++
			if grouped[i] {
				s.held++
			}
		}
	}
	return s
}

// tally adds up batch scores.
type tally struct {
	planted, found     int
	groups             int
	credit             float64
	classes            map[string]int
	held, bystanders   int
	heldAll, bystAll   int
	traps, sprung      map[string]int
	bySubject, byHinge map[string][2]int // found, planted
	bySize             map[int][2]int
}

func newTally() *tally {
	return &tally{classes: map[string]int{}, traps: map[string]int{}, sprung: map[string]int{},
		bySubject: map[string][2]int{}, byHinge: map[string][2]int{}, bySize: map[int][2]int{}}
}

func (t *tally) add(b *built, s batchScore) {
	for _, c := range b.conflicts {
		t.planted++
		f := 0
		if s.found[c.ID] {
			t.found++
			f = 1
		}
		inc := func(m map[string][2]int, k string) {
			v := m[k]
			m[k] = [2]int{v[0] + f, v[1] + 1}
		}
		inc(t.bySubject, c.Subject)
		inc(t.byHinge, c.Hinge)
		v := t.bySize[len(c.core)]
		t.bySize[len(c.core)] = [2]int{v[0] + f, v[1] + 1}
	}
	for _, g := range s.groups {
		t.groups++
		t.credit += g.credit
		t.classes[g.class]++
	}
	for _, tr := range b.traps {
		t.traps[tr.Trap]++
		if s.sprung[tr.ID] {
			t.sprung[tr.Trap]++
		}
	}
	t.held += s.held
	t.bystanders += s.bystanders
	t.heldAll += s.heldAll
	t.bystAll += s.bystandersAll
}

func (t *tally) recall() float64 {
	if t.planted == 0 {
		return 1
	}
	return float64(t.found) / float64(t.planted)
}

// precision is the mean credit of the groups (1 with none).
func (t *tally) precision() float64 {
	if t.groups == 0 {
		return 1
	}
	return t.credit / float64(t.groups)
}

// heldShare is the share of bulk-keepable bystanders a group flags.
func (t *tally) heldShare() float64 {
	if t.bystanders == 0 {
		return 0
	}
	return float64(t.held) / float64(t.bystanders)
}

func (t *tally) sprungTotal() (sprung, of int) {
	for k, n := range t.traps {
		of += n
		sprung += t.sprung[k]
	}
	return sprung, of
}

func (t *tally) String() string {
	var b strings.Builder
	sp, of := t.sprungTotal()
	fmt.Fprintf(&b, "recall %.2f (%d of %d planted conflicts found); precision %.2f over %d groups (", t.recall(), t.found,
		t.planted, t.precision(), t.groups)
	for i, c := range groupClasses {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %d", c, t.classes[c])
	}
	fmt.Fprintf(&b, "); bystanders held out of bulk keep %d of %d (%.1f%%; %d of %d of any trust); traps sprung %d of %d",
		t.held, t.bystanders, 100*t.heldShare(), t.heldAll, t.bystAll, sp, of)
	return b.String()
}

// breakdown is recall by subject, hinge and size, and traps by kind.
func (t *tally) breakdown() string {
	var b strings.Builder
	keys := func(m map[string][2]int) []string {
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	b.WriteString("found by subject:")
	for _, k := range keys(t.bySubject) {
		fmt.Fprintf(&b, " %s %d/%d", k, t.bySubject[k][0], t.bySubject[k][1])
	}
	b.WriteString("\nfound by hinge:")
	for _, k := range keys(t.byHinge) {
		name := k
		if name == "" {
			name = "(none)"
		}
		fmt.Fprintf(&b, " %s %d/%d", name, t.byHinge[k][0], t.byHinge[k][1])
	}
	b.WriteString("\nfound by size:")
	for _, n := range []int{2, 3, 4} {
		if v, ok := t.bySize[n]; ok {
			fmt.Fprintf(&b, " %d-way %d/%d", n, v[0], v[1])
		}
	}
	b.WriteString("\ntraps sprung:")
	for _, k := range trapKinds {
		fmt.Fprintf(&b, " %s %d/%d", k, t.sprung[k], t.traps[k])
	}
	return b.String()
}

// The bars that sweep over: ImportConflictBar is one of them.
var sweepBars = []float64{0, 0.5, 0.6, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95}

// checked is one batch's check: the model's groups before any bar.
type checked struct {
	b     *built
	check judge.ImportCheck
	err   error
}

// at scores every batch's groups at a bar.
func at(runs []checked, bar float64) (*tally, []batchScore) {
	t := newTally()
	scores := make([]batchScore, len(runs))
	for i, r := range runs {
		scores[i] = score(r.b, judge.ImportConflicts(r.check.Groups, bar))
		t.add(r.b, scores[i])
	}
	return t, scores
}

func sweep(runs []checked) string {
	var b strings.Builder
	b.WriteString("bar sweep (recall; precision; groups exact/partial/merged/extra/false; bystanders held; traps sprung):\n")
	for _, bar := range sweepBars {
		t, _ := at(runs, bar)
		sp, of := t.sprungTotal()
		mark := ""
		if bar == judge.ImportConflictBar {
			mark = "  ← ImportConflictBar"
		}
		fmt.Fprintf(&b, "  ≥ %.2f: recall %.2f (%d/%d); precision %.2f; %d/%d/%d/%d/%d; held %d/%d (%.1f%%); sprung %d/%d%s\n", bar,
			t.recall(), t.found, t.planted, t.precision(), t.classes[classExact], t.classes[classPartial],
			t.classes[classMerged], t.classes[classExtra], t.classes[classFalse], t.held, t.bystanders, 100*t.heldShare(),
			sp, of, mark)
	}
	return b.String()
}

// details lists what a run missed and every group that wasn't exact.
func details(runs []checked, bar float64) string {
	var b strings.Builder
	_, scores := at(runs, bar)
	for i, r := range runs {
		s := scores[i]
		for _, c := range r.b.conflicts {
			if !s.found[c.ID] {
				fmt.Fprintf(&b, "  missed %s/%s %v\n", r.b.ID, c.ID, c.Members)
			}
		}
		for _, g := range s.groups {
			if g.class == classExact {
				continue
			}
			var refs []string
			for _, m := range g.members {
				refs = append(refs, r.b.props[m].refs[0])
			}
			fmt.Fprintf(&b, "  %s group in %s (%.2f, credit %.2f, %q): %v", g.class, r.b.ID, g.confidence, g.credit, g.subject, refs)
			if g.conflict != "" {
				fmt.Fprintf(&b, " ~ %s", g.conflict)
			}
			b.WriteString("\n")
		}
		for _, tr := range r.b.traps {
			if s.sprung[tr.ID] {
				fmt.Fprintf(&b, "  sprung %s/%s (%s)\n", r.b.ID, tr.ID, tr.Trap)
			}
		}
		if r.err != nil {
			fmt.Fprintf(&b, "  %s failed: %v\n", r.b.ID, r.err)
		}
	}
	return b.String()
}

// oracle answers the import check with the labels, as far as a call can
// see them: each planted conflict whose members the call holds, two at
// least, at 0.95; then each trap whose members it holds at 0.3, which no
// bar from 0.5 up admits; and a member the batch doesn't have, which the
// parser drops. Prompted tiers get their answer in a Markdown fence.
type oracle struct{ b *built }

var statementID = regexp.MustCompile(`<statement id="(M-\d+)"`)

func (o oracle) Complete(_ context.Context, c judge.Call) (string, error) {
	present := map[int]bool{}
	byRef := map[string]int{}
	for i, p := range o.b.props {
		byRef[p.cand.Ref] = i
	}
	for _, m := range statementID.FindAllStringSubmatch(c.Prompt, -1) {
		present[byRef[m[1]]] = true
	}
	type group struct {
		Subject    string   `json:"subject"`
		Members    []string `json:"members"`
		Confidence float64  `json:"confidence"`
		Rationale  string   `json:"rationale"`
		Suggestion string   `json:"suggestion"`
	}
	ans := struct {
		Conflicts []group `json:"conflicts"`
	}{Conflicts: []group{}}
	add := func(pl planted, conf float64) {
		var refs []string
		for _, i := range pl.core {
			if present[i] {
				refs = append(refs, o.b.props[i].cand.Ref)
			}
		}
		if len(refs) >= 2 {
			ans.Conflicts = append(ans.Conflicts, group{Subject: pl.ID, Members: refs, Confidence: conf,
				Rationale: "Labelled " + pl.ID + ".", Suggestion: ""})
		}
	}
	for _, pl := range o.b.conflicts {
		add(pl, 0.95)
	}
	for _, pl := range o.b.traps {
		add(pl, 0.3)
	}
	if len(ans.Conflicts) > 0 {
		ans.Conflicts[0].Members = append(ans.Conflicts[0].Members, "M-9999")
	}
	raw, err := json.Marshal(ans)
	if c.Tier.Strict {
		return string(raw), err
	}
	return "```json\n" + string(raw) + "\n```", err
}

func fakeTiers() judge.Config {
	return judge.Config{Primary: judge.Tier{Model: "fake-primary"}, Fallback: judge.Tier{Model: "fake-fallback", Strict: true}}
}

// checkAll runs the import check over every batch.
func checkAll(t *testing.T, sets []*built, newModel func(*built) judge.Model, cfg judge.Config, batchSize int) []checked {
	t.Helper()
	out := make([]checked, len(sets))
	for i, b := range sets {
		c := judge.NewClassifier(newModel(b), cfg)
		check, err := c.FindImportDisagreements(context.Background(), b.candidates(), batchSize)
		out[i] = checked{b: b, check: check, err: err}
	}
	return out
}

// The harness, on a model that answers with the labels as far as each
// call can see them: at the bar every planted conflict is found, every
// group is exact and no trap is sprung, on both sets. This checks the
// prompt (every proposal reaches a call, with its id), the parsing, the
// bar, the scoring, and the chunking: a planted conflict the batches split
// apart is a recall loss no model can win back.
func TestHarnessOnAFakeModel(t *testing.T) {
	t.Parallel()
	for _, name := range []string{mainSet, holdoutSet} {
		sets := buildSet(t, name)
		runs := checkAll(t, sets, func(b *built) judge.Model { return oracle{b} }, fakeTiers(), 0)
		calls := 0
		for _, r := range runs {
			if r.err != nil {
				t.Fatalf("%s: %s: %v", name, r.b.ID, r.err)
			}
			calls += r.check.Calls
			if want := (len(r.b.props) + judge.ImportBatch - 1) / judge.ImportBatch; r.check.Batches != want {
				t.Errorf("%s: %d batches for %d proposals, want %d", r.b.ID, r.check.Batches, len(r.b.props), want)
			}
		}
		tl, _ := at(runs, judge.ImportConflictBar)
		t.Logf("%s on the fake model, %d calls: %s\n%s\n%s", name, calls, tl, tl.breakdown(), sweep(runs))
		if tl.recall() != 1 || tl.precision() != 1 || tl.classes[classExact] != tl.groups || tl.held != 0 {
			t.Errorf("%s: the labels themselves score %s\n%s", name, tl, details(runs, judge.ImportConflictBar))
		}
		if sp, _ := tl.sprungTotal(); sp != 0 {
			t.Errorf("%s: %d traps sprung by the labels", name, sp)
		}
		// Below the bar, the decoys come through: the sweep sees what the
		// bar keeps out.
		if low, _ := at(runs, 0); low.classes[classFalse] == 0 {
			t.Errorf("%s: no decoy came through at bar 0: %s", name, low)
		}
	}
}

// Why an import is one call (judge.ImportBatch): cut at 120 proposals by
// section, as the check did before, the batches alone lose planted
// conflicts that no model could find, because their members are never in
// one call.
func TestCuttingAnImportLosesConflicts(t *testing.T) {
	t.Parallel()
	runs := checkAll(t, buildSet(t, mainSet), func(b *built) judge.Model { return oracle{b} }, fakeTiers(), 120)
	tl, _ := at(runs, judge.ImportConflictBar)
	t.Logf("cut at 120: %s\n%s", tl, details(runs, judge.ImportConflictBar))
	if tl.found == tl.planted {
		t.Errorf("cut at 120, the labels still find every conflict: the large batches no longer test the cut")
	}
}

// The scorer, on groups planted by hand.
func TestScore(t *testing.T) {
	t.Parallel()
	b := batch{ID: "s", Space: "project", Files: []file{{Path: "AGENTS.md", Kind: "agents_md", Statements: []statement{
		{Line: 1, Text: "Run tests with `pnpm test`."},
		{Line: 2, Text: "Run tests with `make test`."},
		{Line: 3, Text: "Use Node 20."},
		{Line: 4, Text: "Use Node 22."},
		{Line: 5, Text: "Use Node 24."},
		{Line: 6, Text: "Deploy from `main`."},
		{Line: 7, Text: "Deploy from `release`."},
		{Line: 8, Text: "Handlers return JSON errors."},
		{Line: 9, Text: "Error codes are snake_case."},
		{Line: 10, Text: "`.nvmrc` holds the version."},
	}}},
		Conflicts: []label{
			{ID: "test", Subject: "test", Members: []string{"AGENTS.md:1", "AGENTS.md:2"}},
			{ID: "node", Subject: "version", Members: []string{"AGENTS.md:3", "AGENTS.md:4", "AGENTS.md:5"}, Also: []string{"AGENTS.md:10"}},
			{ID: "deploy", Subject: "deploy", Members: []string{"AGENTS.md:6", "AGENTS.md:7"}},
		},
		Traps: []label{{ID: "errors", Trap: "complementary", Members: []string{"AGENTS.md:8", "AGENTS.md:9"}}},
	}
	bt, err := build(b)
	if err != nil {
		t.Fatal(err)
	}
	group := func(lines ...int) ledger.ImportConflictInput {
		var g ledger.ImportConflictInput
		for _, l := range lines {
			g.Members = append(g.Members, bt.props[bt.byRef[fmt.Sprintf("AGENTS.md:%d", l)]].cand.ID)
		}
		return g
	}
	cases := []struct {
		name   string
		groups []ledger.ImportConflictInput
		class  []string
		credit []float64
		found  []string
		held   int
		sprung bool
	}{
		{"exact", []ledger.ImportConflictInput{group(1, 2)}, []string{classExact}, []float64{1}, []string{"test"}, 0, false},
		{"exact with an also-member", []ledger.ImportConflictInput{group(3, 4, 5, 10)}, []string{classExact}, []float64{1},
			[]string{"node"}, 0, false},
		{"partial", []ledger.ImportConflictInput{group(3, 4)}, []string{classPartial}, []float64{1}, nil, 0, false},
		{"merged", []ledger.ImportConflictInput{group(1, 2, 6, 7)}, []string{classMerged}, []float64{0.5},
			[]string{"test", "deploy"}, 0, false},
		{"extra", []ledger.ImportConflictInput{group(1, 2, 8)}, []string{classExtra}, []float64{2.0 / 3}, []string{"test"}, 1,
			false},
		{"false", []ledger.ImportConflictInput{group(8, 9)}, []string{classFalse}, []float64{0}, nil, 2, true},
		{"one of each conflict", []ledger.ImportConflictInput{group(1, 6)}, []string{classFalse}, []float64{0}, nil, 0, false},
	}
	for _, c := range cases {
		s := score(bt, c.groups)
		for i, g := range s.groups {
			if g.class != c.class[i] || g.credit != c.credit[i] {
				t.Errorf("%s: group %d is %s at %.3f, want %s at %.3f", c.name, i, g.class, g.credit, c.class[i], c.credit[i])
			}
		}
		var found []string
		for _, l := range b.Conflicts {
			if s.found[l.ID] {
				found = append(found, l.ID)
			}
		}
		if !slices.Equal(found, c.found) {
			t.Errorf("%s: found %v, want %v", c.name, found, c.found)
		}
		if s.held != c.held || s.bystanders != 2 {
			t.Errorf("%s: held %d of %d, want %d of 2", c.name, s.held, s.bystanders, c.held)
		}
		if s.sprung["errors"] != c.sprung {
			t.Errorf("%s: sprung %v", c.name, s.sprung["errors"])
		}
	}
	// The tally: one of each above, as one batch's groups.
	var groups []ledger.ImportConflictInput
	for _, c := range cases[:6] {
		groups = append(groups, c.groups...)
	}
	tl := newTally()
	tl.add(bt, score(bt, groups))
	if want := (1 + 1 + 1 + 0.5 + 2.0/3) / 6; tl.found != 3 || tl.planted != 3 || tl.groups != 6 ||
		math.Abs(tl.precision()-want) > 1e-9 {
		t.Errorf("tally: %s", tl)
	}
}

// sectionFor follows init's: the innermost heading that names a section.
func TestSectionFor(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		under   string
		home    bool
		section ledger.Section
		kind    ledger.Kind
	}{
		{"Orbit › Decisions", false, ledger.SectionDecisions, ledger.KindDecision},
		{"Orbit › Open questions", false, ledger.SectionOpenQuestion, ledger.KindFact},
		{"About me", true, ledger.SectionPreferences, ledger.KindFact},
		{"Decisions › Commands", false, ledger.SectionDecisions, ledger.KindDecision},
		{"Orbit › Setup", false, ledger.SectionConventions, ledger.KindFact},
		{"Codex", true, ledger.SectionPreferences, ledger.KindFact},
		{"", false, ledger.SectionConventions, ledger.KindFact},
	} {
		s, k := sectionFor(c.under, c.home)
		if s != c.section || k != c.kind {
			t.Errorf("%q: %s %s, want %s %s", c.under, s, k, c.section, c.kind)
		}
	}
}
