package compile

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The delta between two compiles of a file (cloud warm-start design §3.4):
// what a session that loaded the older compile hasn't seen. Both compiles'
// stored artifacts are parsed into cited lines (a line ending in its
// citation list, "- Use pnpm. [M-0233]"), paired by their cites, and
// compared:
//
//   - added: a cite list the newer compile has and the older didn't;
//   - changed: the same cite list with other words;
//   - gone: memories the older compile cited and the newer doesn't.
//
// Words come from the newer artifact only, and a line citing a forgotten
// memory is never sent, whatever the artifact still holds (Forget's
// re-render of the artifacts runs after the Forget, within a minute). A
// forgotten memory is never "gone" either: the caller lists it as
// forgotten. When the older artifact is missing, the record's refs stand
// in: lines with a memory the older compile didn't cite are added, and
// nothing is changed.
//
// The cite pairing is the CLI's session-start hook's (lib/hook/compiled.ts
// parseCompiled): the cites joined, with "#n" for the n-th line citing the
// same ones.

// MaxDeltaLines bounds the added and changed lines a delta carries; the
// rest are counted in More.
const MaxDeltaLines = 60

// DeltaLine is one cited line of the newer compile.
type DeltaLine struct {
	Refs []string `json:"refs"`
	Text string   `json:"text"`
}

// FileDelta is what changed in one file between two compiles.
type FileDelta struct {
	Added   []DeltaLine
	Changed []DeltaLine
	Gone    []string
	More    int
}

// trailingCites is a cited line's citation list, at its end.
var trailingCites = regexp.MustCompile(`\s*\[(M-\d+(?:\s*,\s*M-\d+)*)\]$`)

const (
	managedStart = "<!-- memax:start -->"
	managedEnd   = "<!-- memax:end -->"
)

type citedLine struct {
	key  string
	refs []string
	text string
}

// citedLines reads a compiled file's cited lines: within Memax's managed
// block when the file has one, skipping the header, headings and comments.
func citedLines(content string) []citedLine {
	lines := strings.FieldsFunc(strings.ReplaceAll(content, "\r\n", "\n"), func(r rune) bool { return r == '\n' || r == '\r' })
	start := slices.IndexFunc(lines, func(l string) bool { return strings.TrimSpace(l) == managedStart })
	end := slices.IndexFunc(lines, func(l string) bool { return strings.TrimSpace(l) == managedEnd })
	if start >= 0 && end > start {
		lines = lines[start+1 : end]
	}
	var out []citedLine
	seen := map[string]int{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}
		m := trailingCites.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var refs []string
		for _, r := range strings.Split(m[1], ",") {
			refs = append(refs, strings.TrimSpace(r))
		}
		base := strings.Join(refs, ",")
		seen[base]++
		key := base
		if n := seen[base]; n > 1 {
			key = base + "#" + strconv.Itoa(n)
		}
		out = append(out, citedLine{key: key, refs: refs, text: line})
	}
	return out
}

// Delta is what changed in path between the compile from and the compile
// to (a later compile of the same target). forgotten are memories a person
// forgot: their lines are never sent, and they are never gone.
func (s *Service) Delta(ctx context.Context, from, to *ledger.CompileRun, path string, forgotten map[string]bool) (FileDelta, error) {
	d := FileDelta{Added: []DeltaLine{}, Changed: []DeltaLine{}, Gone: []string{}}
	if s == nil || from == nil || to == nil || from.ID == to.ID {
		return d, nil
	}
	var older, newer *Artifact
	var olderErr, newerErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		older, olderErr = s.artifact(ctx, from)
	}()
	newer, newerErr = s.artifact(ctx, to)
	wg.Wait()
	if newerErr != nil {
		return d, newerErr
	}
	nf, ok := newer.file(path)
	if !ok {
		return d, nil
	}
	newLines := citedLines(nf.Content)
	hidden := func(refs []string) bool {
		return slices.ContainsFunc(refs, func(r string) bool { return forgotten[r] })
	}

	var added, changed []DeltaLine
	newRefs := map[string]bool{}
	for _, l := range newLines {
		for _, r := range l.refs {
			newRefs[r] = true
		}
	}
	var oldRefs []string
	of, haveOld := ArtifactOutput{}, false
	if olderErr == nil {
		of, haveOld = older.file(path)
	} else if !isMissing(olderErr) {
		return d, olderErr
	}
	if haveOld {
		old := map[string]string{}
		for _, l := range citedLines(of.Content) {
			old[l.key] = l.text
			oldRefs = append(oldRefs, l.refs...)
		}
		for _, l := range newLines {
			if hidden(l.refs) {
				continue
			}
			text, was := old[l.key]
			switch {
			case !was:
				added = append(added, DeltaLine{Refs: l.refs, Text: l.text})
			case text != l.text:
				changed = append(changed, DeltaLine{Refs: l.refs, Text: l.text})
			}
		}
	} else {
		// The older artifact is gone: the record's refs say which
		// memories it cited, not its words.
		oldRefs = refsAt(from, path)
		had := map[string]bool{}
		for _, r := range oldRefs {
			had[r] = true
		}
		for _, l := range newLines {
			if hidden(l.refs) || !slices.ContainsFunc(l.refs, func(r string) bool { return !had[r] }) {
				continue
			}
			added = append(added, DeltaLine{Refs: l.refs, Text: l.text})
		}
	}
	gone := map[string]bool{}
	for _, r := range oldRefs {
		if !newRefs[r] && !forgotten[r] {
			gone[r] = true
		}
	}
	for r := range gone {
		d.Gone = append(d.Gone, r)
	}
	slices.Sort(d.Gone)
	room := MaxDeltaLines
	take := func(ls []DeltaLine) []DeltaLine {
		if len(ls) <= room {
			room -= len(ls)
			return ls
		}
		d.More += len(ls) - room
		ls = ls[:room]
		room = 0
		return ls
	}
	d.Added = append(d.Added, take(added)...)
	d.Changed = append(d.Changed, take(changed)...)
	return d, nil
}

// refsAt is what a compile's file at path cites, from the record: the
// file's own refs, else the run's.
func refsAt(run *ledger.CompileRun, path string) []string {
	for _, f := range run.Files {
		if f.Path == path && f.Refs != nil {
			return f.Refs
		}
	}
	return run.Refs
}

// The artifacts a delta reads, cached in this process by key: a session
// start reads the same two compiles of a repository again and again.
// Forget re-renders an artifact in place, so a Forget's propagation purges
// the space's copies (PurgeSpace); until it does, a forgotten memory's
// lines are left out by the caller's forgotten refs anyway.
const (
	deltaArtifactTTL  = 10 * time.Minute
	deltaArtifactKeep = 128
)

type cachedDelta struct {
	a     *Artifact
	space uuid.UUID
	at    time.Time
}

type deltaCache struct {
	mu sync.Mutex
	m  map[string]cachedDelta
}

func (s *Service) artifact(ctx context.Context, run *ledger.CompileRun) (*Artifact, error) {
	if run.ArtifactKey == "" {
		return nil, errMissingArtifact
	}
	s.deltas.mu.Lock()
	c, ok := s.deltas.m[run.ArtifactKey]
	s.deltas.mu.Unlock()
	if ok && time.Since(c.at) < deltaArtifactTTL {
		return c.a, nil
	}
	a, err := loadArtifact(ctx, s.store, run.ArtifactKey)
	if err != nil {
		return nil, err
	}
	s.deltas.mu.Lock()
	defer s.deltas.mu.Unlock()
	if s.deltas.m == nil || len(s.deltas.m) >= deltaArtifactKeep {
		s.deltas.m = map[string]cachedDelta{}
	}
	s.deltas.m[run.ArtifactKey] = cachedDelta{a: a, space: run.SpaceID, at: time.Now()}
	return a, nil
}

// PurgeSpace drops this process's cached artifacts of a space (Forget's
// propagation, through forget.Local).
func (s *Service) PurgeSpace(spaceID uuid.UUID) {
	if s == nil {
		return
	}
	s.deltas.mu.Lock()
	defer s.deltas.mu.Unlock()
	for k, c := range s.deltas.m {
		if c.space == spaceID {
			delete(s.deltas.m, k)
		}
	}
}

// errMissingArtifact is a run with no stored artifact (it reads as not
// found).
var errMissingArtifact = missingArtifact{}

type missingArtifact struct{}

func (missingArtifact) Error() string { return "compile: artifact not found" }
