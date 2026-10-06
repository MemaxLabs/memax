// Package compiletest is an in-process stand-in for the compile service,
// for tests that exercise the compile path without Node.
//
// Fake renders a small, deterministic file per target (a header naming the
// compile, the Brief title, one "## heading" per section, and one
// "- statement [M-id]" line per memory) and parses hand edits back by
// cites, the way the real compiler does in spirit. It is not the compiler:
// golden output and parse-back edge cases are tested in packages/compiler,
// and the gate test runs the real service.
package compiletest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Fake implements compile.Compiler.
type Fake struct {
	// Calls counts Compile calls.
	Calls atomic.Int64
	// OnCompile, when set, runs inside every Compile call, before it
	// answers: a test can change the record "while compiling".
	OnCompile func(*compile.Input)

	mu sync.Mutex
	// fail, when set, is returned by the next Compile calls.
	fail error
	last *compile.Input
}

var _ compile.Compiler = (*Fake)(nil)

// Fail makes Compile return err until Fail(nil).
func (f *Fake) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

// LastInput is the input of the latest Compile call.
func (f *Fake) LastInput() *compile.Input {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// Compile renders every target of the input.
func (f *Fake) Compile(_ context.Context, in *compile.Input) (*compile.Result, error) {
	f.Calls.Add(1)
	f.mu.Lock()
	err := f.fail
	f.last = in
	f.mu.Unlock()
	if f.OnCompile != nil {
		f.OnCompile(in)
	}
	if err != nil {
		return nil, err
	}
	res := &compile.Result{
		Version: 1, CompileID: in.Compile.ID, CompiledAt: in.Compile.At, BriefID: in.Brief.ID, Space: in.Space.Slug,
		Files: []compile.File{}, Copies: []compile.Copy{}, Warnings: []compile.Warning{},
	}
	for _, t := range in.Targets {
		body, refs := render(in, t)
		text := compile.Text{Target: t.Kind, Content: body, Bytes: len(body), Lines: strings.Count(body, "\n"),
			SHA256: sum(body), Refs: refs, Cites: refs, DroppedForBudget: []string{}}
		tr := compile.TargetResult{Kind: t.Kind, Delivery: "file", Refs: refs, DroppedForBudget: []string{}, Files: []string{}}
		switch t.Kind {
		case "chatgpt":
			tr.Delivery = "copy"
			res.Copies = append(res.Copies, compile.Copy{Text: text, Label: "ChatGPT project instructions"})
		case "cursor_mdc":
			// Scoped rules only for path-scoped memories.
			reads := "AGENTS.md"
			tr.Reads = &reads
			path := t.Path
			tr.Path = &path
			if scopedBody, scopedRefs := renderScoped(in); scopedBody != "" {
				p := t.Path + "/memax-scoped.mdc"
				res.Files = append(res.Files, file(t.Kind, p, scopedBody, scopedRefs))
				tr.Files = []string{p}
			}
		default:
			path := t.Path
			tr.Path = &path
			res.Files = append(res.Files, file(t.Kind, t.Path, body, refs))
			tr.Files = []string{t.Path}
		}
		res.Targets = append(res.Targets, tr)
	}
	return res, nil
}

func file(kind, path, body string, refs []string) compile.File {
	return compile.File{Text: compile.Text{Target: kind, Content: body, Bytes: len(body), Lines: strings.Count(body, "\n"),
		SHA256: sum(body), Refs: refs, Cites: refs, DroppedForBudget: []string{}}, Path: path, DriftSHA256: sum(body)}
}

func render(in *compile.Input, t compile.TargetSettings) (string, []string) {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- Compiled by Memax from %s (%s at %s). -->\n# %s\n", in.Space.Slug, in.Compile.ID, in.Compile.At, in.Brief.Title)
	byRef := map[string]compile.InputMemory{}
	for _, m := range in.Memories {
		byRef[m.Ref] = m
	}
	var refs []string
	placed := map[string]bool{}
	for _, s := range in.Brief.Sections {
		fmt.Fprintf(&b, "\n## %s\n", s.Heading)
		for _, it := range s.Items {
			if it.Ref != "" {
				if m, ok := byRef[it.Ref]; ok && (t.Include != "kept_only" || m.State != "open") {
					fmt.Fprintf(&b, "- %s [%s]\n", m.Statement, m.Ref)
					refs = append(refs, m.Ref)
					placed[m.Ref] = true
				}
				continue
			}
			fmt.Fprintf(&b, "- %s [%s]\n", it.Text, strings.Join(it.Cites, ", "))
		}
		// Memories kept after the Brief go to the end of their section.
		for _, m := range in.Memories {
			if !placed[m.Ref] && sectionKey(m) == s.Key {
				fmt.Fprintf(&b, "- %s [%s]\n", m.Statement, m.Ref)
				refs = append(refs, m.Ref)
				placed[m.Ref] = true
			}
		}
	}
	slices.Sort(refs)
	return b.String(), nonNil(refs)
}

func sectionKey(m compile.InputMemory) string { return m.Section }

func renderScoped(in *compile.Input) (string, []string) {
	var lines []string
	var refs []string
	for _, m := range in.Memories {
		if m.Scope != nil && len(m.Scope.Paths) > 0 {
			lines = append(lines, fmt.Sprintf("- %s [%s]", m.Statement, m.Ref))
			refs = append(refs, m.Ref)
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	return fmt.Sprintf("---\nglobs: scoped\n---\n<!-- Compiled by Memax (%s). -->\n%s\n", in.Compile.ID, strings.Join(lines, "\n")), refs
}

var item = regexp.MustCompile(`^- (.*?)(?: \[(M-\d+(?:, M-\d+)*)\])?$`)

type line struct {
	n       int
	text    string
	refs    []string
	section *string
}

func parse(content string) []line {
	var out []line
	var section *string
	for i, raw := range strings.Split(strings.TrimRight(strings.ReplaceAll(content, "\r\n", "\n"), "\n"), "\n") {
		if h, ok := strings.CutPrefix(raw, "## "); ok {
			s := h
			section = &s
			continue
		}
		m := item.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		l := line{n: i + 1, text: m[1], section: section}
		if m[2] != "" {
			l.refs = strings.Split(m[2], ", ")
		}
		out = append(out, l)
	}
	return out
}

// ParseBack pairs cited lines by their cites and reports edits, new lines
// and removals.
func (f *Fake) ParseBack(_ context.Context, req *compile.ParseBackRequest) (*compile.ParseBackResult, error) {
	old, cur := parse(req.Last.Content), parse(req.Current)
	res := &compile.ParseBackResult{DriftSHA256: sum(normalize(req.Current))}
	res.Drift.Changed = normalize(req.Last.Content) != normalize(req.Current)
	res.Drifted = req.Last.DriftSHA256 == "" || req.Last.DriftSHA256 != res.DriftSHA256
	key := func(l line) string { return strings.Join(l.refs, ",") }
	used := map[int]bool{}
	var removes []ledger.DriftChange
	for _, o := range old {
		if len(o.refs) == 0 {
			continue
		}
		j := slices.IndexFunc(cur, func(c line) bool { return !used[c.n] && key(c) == key(o) })
		if j < 0 {
			removes = append(removes, ledger.DriftChange{Kind: ledger.ChangeRemove, Ref: o.refs[0], Refs: o.refs, OldText: o.text, OldLine: o.n})
			continue
		}
		used[cur[j].n] = true
		if cur[j].text != o.text {
			res.Changes = append(res.Changes, ledger.DriftChange{Kind: ledger.ChangeEdit, Ref: o.refs[0], Refs: o.refs,
				OldText: o.text, NewText: cur[j].text, OldLine: o.n, NewLine: cur[j].n})
		}
	}
	oldTexts := map[string]int{}
	for _, o := range old {
		if len(o.refs) == 0 {
			oldTexts[o.text]++
		}
	}
	for _, c := range cur {
		if used[c.n] {
			continue
		}
		if len(c.refs) == 0 && oldTexts[c.text] > 0 {
			oldTexts[c.text]--
			continue
		}
		res.Changes = append(res.Changes, ledger.DriftChange{Kind: ledger.ChangeNew, Text: c.text, Line: c.n,
			Section: c.section, Paths: []string{}, Cites: nonNil(c.refs)})
	}
	slices.SortStableFunc(res.Changes, func(a, b ledger.DriftChange) int { return at(a) - at(b) })
	res.Changes = append(res.Changes, removes...)
	if res.Changes == nil {
		res.Changes = []ledger.DriftChange{}
	}
	return res, nil
}

func at(c ledger.DriftChange) int {
	if c.Kind == ledger.ChangeEdit {
		return c.NewLine
	}
	return c.Line
}

func normalize(s string) string {
	return strings.ReplaceAll(strings.TrimPrefix(s, "\uFEFF"), "\r\n", "\n")
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
