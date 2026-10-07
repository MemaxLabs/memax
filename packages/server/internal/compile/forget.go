package compile

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Forget's reach into object storage (plan 25 §5.13 step 2). The compile
// artifacts that held a forgotten memory are re-rendered without it: every
// line that cites it ("… [M-0201]", or prose citing it with others) is
// taken out of each file and copy-out the run produced, in place, under
// the same key. A run's hashes stay as they were, since they describe the
// file Memax delivered (drift detection still recognises that file on a
// disk, and so the daemon still writes over it). The drift observations
// (hand-edited files a device reported) lose the same lines. A retired
// space's artifacts are deleted outright.

// citeList matches a compiled line's citation list ("[M-0219]",
// "[M-0431, M-0174]").
var citeList = regexp.MustCompile(`\[(M-\d{4,}(?:\s*,\s*M-\d{4,})*)\]`)

// citesAny reports whether a line cites any of refs.
func citesAny(line string, refs map[string]bool) bool {
	for _, m := range citeList.FindAllStringSubmatch(line, -1) {
		for _, r := range strings.Split(m[1], ",") {
			if refs[strings.TrimSpace(r)] {
				return true
			}
		}
	}
	return false
}

// StripCited removes every line of content that cites one of refs, and
// says whether it removed any.
func StripCited(content string, refs []string) (string, bool) {
	if len(refs) == 0 || content == "" {
		return content, false
	}
	set := make(map[string]bool, len(refs))
	for _, r := range refs {
		set[r] = true
	}
	lines := strings.SplitAfter(content, "\n")
	out := lines[:0]
	removed := false
	for _, l := range lines {
		if citesAny(l, set) {
			removed = true
			continue
		}
		out = append(out, l)
	}
	if !removed {
		return content, false
	}
	return strings.Join(out, ""), true
}

// RedactArtifacts re-renders the stored artifacts without the forgotten
// refs: each compile run's outputs, and each observed file. It returns how
// many objects changed. A missing object (never uploaded, or already
// deleted) is skipped.
func (s *Service) RedactArtifacts(ctx context.Context, set ledger.ArtifactSet, refs []string) (int, error) {
	if s == nil {
		return 0, nil
	}
	changed := 0
	for _, key := range set.Compiles {
		a, err := loadArtifact(ctx, s.store, key)
		if err != nil {
			if isMissing(err) {
				continue
			}
			return changed, err
		}
		any := false
		for i := range a.Files {
			if c, ok := StripCited(a.Files[i].Content, refs); ok {
				a.Files[i].Content, any = c, true
			}
		}
		for i := range a.Copies {
			if c, ok := StripCited(a.Copies[i].Content, refs); ok {
				a.Copies[i].Content, any = c, true
			}
		}
		if !any {
			continue
		}
		if err := putJSON(ctx, s.store, key, a); err != nil {
			return changed, fmt.Errorf("compile: re-render %s: %w", key, err)
		}
		changed++
	}
	for _, key := range set.Observations {
		raw, err := get(ctx, s.store, key)
		if err != nil {
			if isMissing(err) {
				continue
			}
			return changed, err
		}
		c, ok := StripCited(string(raw), refs)
		if !ok {
			continue
		}
		if err := putText(ctx, s.store, key, c); err != nil {
			return changed, fmt.Errorf("compile: re-render %s: %w", key, err)
		}
		changed++
	}
	return changed, nil
}

// DeleteArtifacts deletes objects by key (a retired space's).
func (s *Service) DeleteArtifacts(ctx context.Context, keys []string) (int, error) {
	if s == nil {
		return 0, nil
	}
	n := 0
	for _, k := range keys {
		if err := s.store.Delete(ctx, k); err != nil {
			return n, fmt.Errorf("compile: delete %s: %w", k, err)
		}
		n++
	}
	return n, nil
}

// PutJSON stores a JSON object (the forget ledger's copy).
func (s *Service) PutJSON(ctx context.Context, key string, v any) error {
	if s == nil {
		return nil
	}
	return putJSON(ctx, s.store, key, v)
}

// isMissing reports a get of an object that isn't there.
func isMissing(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "nosuchkey") || strings.Contains(msg, "404")
}

// stripChanges drops the parse-back changes that cite a forgotten memory.
func stripChanges(cs ledger.ChangeSet, forgotten []string) ledger.ChangeSet {
	if len(forgotten) == 0 {
		return cs
	}
	has := func(r string) bool { return slices.Contains(forgotten, r) }
	kept := cs.Changes[:0]
	set := make(map[string]bool, len(forgotten))
	for _, r := range forgotten {
		set[r] = true
	}
	for _, ch := range cs.Changes {
		if has(ch.Ref) || slices.ContainsFunc(ch.Refs, has) || slices.ContainsFunc(ch.Cites, has) ||
			citesAny(ch.OldText, set) || citesAny(ch.NewText, set) || citesAny(ch.Text, set) {
			continue
		}
		kept = append(kept, ch)
	}
	cs.Changes = kept
	return cs
}
