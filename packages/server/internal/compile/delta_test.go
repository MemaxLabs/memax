package compile

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore/mockobjectstore"
)

// The cited lines of a compiled file, as the CLI's session-start hook
// reads them: in the managed block when there is one, by trailing cites,
// a repeat of the same cites keyed #2.
func TestCitedLines(t *testing.T) {
	t.Parallel()
	content := strings.Join([]string{
		"# Notes of my own [M-9999]",
		"Prose a person wrote, citing [M-0001] mid-line.",
		"<!-- memax:start -->",
		"<!-- Compiled by Memax from acme-web at 2026-10-11T09:00:00Z (C-0881). Edit it at https://memax.app/acme-web/brief; edits here come back as proposals. -->",
		"## Conventions",
		"- Use pnpm, not npm. [M-0233]",
		"",
		"Deploys go through Fly, the API in sjc. [M-0219, M-0174]",
		"- A line without cites.",
		"- Use pnpm in CI too. [M-0233]",
		"<!-- memax:end -->",
		"- After the block. [M-0300]",
	}, "\r\n")
	got := citedLines(content)
	var keys []string
	for _, l := range got {
		keys = append(keys, l.key)
	}
	if want := []string{"M-0233", "M-0219,M-0174", "M-0233#2"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if got[1].text != "Deploys go through Fly, the API in sjc. [M-0219, M-0174]" || !slices.Equal(got[1].refs, []string{"M-0219", "M-0174"}) {
		t.Errorf("prose line = %+v", got[1])
	}
	// Without a managed block, the whole file counts.
	if n := len(citedLines("- One. [M-0001]\n- Two. [M-0002]\n")); n != 2 {
		t.Errorf("%d lines without a block", n)
	}
}

type deltaFixture struct {
	t   *testing.T
	s   *Service
	sp  uuid.UUID
	seq int64
}

func (f *deltaFixture) run(path string, lines ...string) *ledger.CompileRun {
	f.t.Helper()
	f.seq++
	ref := ledger.FormatRef(ledger.PrefixCompile, f.seq)
	content := "<!-- Compiled by Memax from acme-web at now (" + ref + "). -->\n# acme-web\n\n## Conventions\n" + strings.Join(lines, "\n") + "\n"
	var refs []string
	for _, l := range citedLines(content) {
		refs = append(refs, l.refs...)
	}
	key := fmt.Sprintf("v2/spaces/%s/targets/t/compiles/%s.json", f.sp, ref)
	a := &Artifact{Version: 1, Compile: ref, Kind: "agents_md", Copies: []ArtifactOutput{},
		Files: []ArtifactOutput{{CompiledOutput: ledger.CompiledOutput{Path: path, Refs: refs}, Content: content}}}
	if err := putJSON(context.Background(), f.s.store, key, a); err != nil {
		f.t.Fatal(err)
	}
	return &ledger.CompileRun{ID: uuid.New(), Ref: ref, SpaceID: f.sp, ArtifactKey: key, Refs: refs,
		Files: []ledger.CompiledOutput{{Path: path, Refs: refs}}}
}

func newDeltaFixture(t *testing.T) *deltaFixture {
	return &deltaFixture{t: t, s: &Service{store: mockobjectstore.New()}, sp: uuid.New()}
}

func lineRefs(ls []DeltaLine) []string {
	var out []string
	for _, l := range ls {
		out = append(out, strings.Join(l.Refs, ","))
	}
	return out
}

func TestDelta(t *testing.T) {
	t.Parallel()
	f := newDeltaFixture(t)
	ctx := context.Background()
	c1 := f.run("AGENTS.md",
		"- Deploy to staging first. [M-0219]",
		"- Prefer yarn. [M-0177]",
		"- Zebra tokens live in the vault. [M-0201]",
		"- Jobs run on River. [M-0301]")
	c2 := f.run("AGENTS.md",
		"- Deploy to staging first: fly deploy. [M-0219]",
		"- Zebra tokens live in the vault. [M-0201]",
		"- Jobs run on River. [M-0301]",
		"- Use pnpm. [M-0233]",
		"- Forgotten since, still compiled. [M-0400]")
	forgotten := map[string]bool{"M-0201": true, "M-0400": true}

	d, err := f.s.Delta(ctx, c1, c2, "AGENTS.md", forgotten)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lineRefs(d.Added), []string{"M-0233"}) || d.Added[0].Text != "- Use pnpm. [M-0233]" {
		t.Errorf("added = %+v", d.Added)
	}
	if !slices.Equal(lineRefs(d.Changed), []string{"M-0219"}) || d.Changed[0].Text != "- Deploy to staging first: fly deploy. [M-0219]" {
		t.Errorf("changed = %+v", d.Changed)
	}
	if !slices.Equal(d.Gone, []string{"M-0177"}) || d.More != 0 {
		t.Errorf("gone = %v, more %d", d.Gone, d.More)
	}
	for _, l := range append(d.Added, d.Changed...) {
		if strings.Contains(l.Text, "Zebra") || strings.Contains(l.Text, "Forgotten since") {
			t.Errorf("a forgotten memory's line was sent: %q", l.Text)
		}
	}

	// The same compile: nothing.
	if d, _ := f.s.Delta(ctx, c2, c2, "AGENTS.md", nil); len(d.Added)+len(d.Changed)+len(d.Gone) != 0 {
		t.Errorf("same compile = %+v", d)
	}

	// The older artifact gone: the record's refs say what it cited.
	if err := f.s.store.Delete(ctx, c1.ArtifactKey); err != nil {
		t.Fatal(err)
	}
	f.s.PurgeSpace(f.sp)
	d, err = f.s.Delta(ctx, c1, c2, "AGENTS.md", forgotten)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lineRefs(d.Added), []string{"M-0233"}) || len(d.Changed) != 0 || !slices.Equal(d.Gone, []string{"M-0177"}) {
		t.Errorf("from refs = %+v", d)
	}

	// The newer artifact gone: an error, so the caller sends no words.
	if err := f.s.store.Delete(ctx, c2.ArtifactKey); err != nil {
		t.Fatal(err)
	}
	f.s.PurgeSpace(f.sp)
	if _, err := f.s.Delta(ctx, c1, c2, "AGENTS.md", nil); err == nil {
		t.Error("no error without the newer artifact")
	}
}

// At most 60 added and changed lines are sent a file, added first; the
// rest are counted.
func TestDeltaCap(t *testing.T) {
	t.Parallel()
	f := newDeltaFixture(t)
	var old, cur []string
	for i := range 50 {
		old = append(old, fmt.Sprintf("- Rule %d. [M-%04d]", i, 1000+i))
		cur = append(cur, fmt.Sprintf("- Rule %d, changed. [M-%04d]", i, 1000+i))
	}
	for i := range 30 {
		cur = append(cur, fmt.Sprintf("- New rule %d. [M-%04d]", i, 2000+i))
	}
	d, err := f.s.Delta(context.Background(), f.run("AGENTS.md", old...), f.run("AGENTS.md", cur...), "AGENTS.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added) != 30 || len(d.Changed) != 30 || d.More != 20 {
		t.Errorf("added %d, changed %d, more %d", len(d.Added), len(d.Changed), d.More)
	}
}

// Forget re-renders an artifact in place: a purge drops the cached copy.
func TestDeltaCachePurge(t *testing.T) {
	t.Parallel()
	f := newDeltaFixture(t)
	ctx := context.Background()
	c1 := f.run("AGENTS.md", "- One. [M-0001]")
	c2 := f.run("AGENTS.md", "- One. [M-0001]", "- Two, with secret words. [M-0002]")
	if d, _ := f.s.Delta(ctx, c1, c2, "AGENTS.md", nil); len(d.Added) != 1 {
		t.Fatalf("added = %+v", d.Added)
	}
	a, err := loadArtifact(ctx, f.s.store, c2.ArtifactKey)
	if err != nil {
		t.Fatal(err)
	}
	a.Files[0].Content, _ = StripCited(a.Files[0].Content, []string{"M-0002"})
	if err := putJSON(ctx, f.s.store, c2.ArtifactKey, a); err != nil {
		t.Fatal(err)
	}
	f.s.PurgeSpace(f.sp)
	if d, _ := f.s.Delta(ctx, c1, c2, "AGENTS.md", nil); len(d.Added) != 0 {
		t.Errorf("after the purge, added = %+v", d.Added)
	}
}
