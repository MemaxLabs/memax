package compile

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// TestHomeTargetsLeaveOutRepoBoundMemories: a personal fact tied to one
// repository (scope.repos, as init sets it on that repository's auto
// memory and CLAUDE.local.md) or to paths in one never reaches a home
// file, which loads in every project; a repository target compiles all of
// them as before (personal-layer design §3.3 and §5.3).
func TestHomeTargetsLeaveOutRepoBoundMemories(t *testing.T) {
	t.Parallel()
	kept := func(ref, scope string) ledger.Memory {
		return ledger.Memory{Ref: ref, Statement: "Statement " + ref, Section: ledger.SectionPreferences,
			Kind: ledger.KindFact, Lifecycle: lifecycle.Kept, Trust: policy.TrustPerson, Applies: json.RawMessage(scope)}
	}
	memories := []ledger.Memory{
		kept("M-0001", `{}`),
		kept("M-0002", `{"repos": ["acme/web"]}`),
		kept("M-0003", `{"paths": ["packages/web/**"]}`),
		kept("M-0004", `{"agents": ["codex"]}`),
		kept("M-0005", `{"repos": []}`),
		kept("M-0006", `{"repos": "acme/web"}`), // not a list: counts as tied
	}
	snapshot := func(kind ledger.TargetKind, root ledger.TargetRoot, path string) *ledger.CompileSnapshot {
		return &ledger.CompileSnapshot{
			View:  &ledger.TargetView{Target: &ledger.Target{ID: uuid.New(), Kind: kind, Root: root, Path: path}},
			Space: ledger.Space{ID: uuid.New(), Slug: "personal", Name: "Personal", Kind: policy.SpacePersonal},
			Brief: &ledger.Brief{Ref: "B-0001", Version: 1, Title: "Personal"},
			Kept:  memories,
		}
	}
	refs := func(b *built) []string {
		var out []string
		for _, m := range b.input.Memories {
			out = append(out, m.Ref)
		}
		return out
	}
	at := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		kind ledger.TargetKind
		root ledger.TargetRoot
		path string
		want []string
	}{
		{ledger.TargetClaudeUser, ledger.RootHome, ".claude/rules/memax-personal.md", []string{"M-0001", "M-0004", "M-0005"}},
		{ledger.TargetCodexUser, ledger.RootHome, ".codex/AGENTS.md", []string{"M-0001", "M-0004", "M-0005"}},
		{ledger.TargetAgentsMD, ledger.RootHome, ledger.HomeCanonicalPath, []string{"M-0001", "M-0004", "M-0005"}},
		{ledger.TargetAgentsMD, ledger.RootRepo, "AGENTS.md", []string{"M-0001", "M-0002", "M-0003", "M-0004", "M-0005", "M-0006"}},
		{ledger.TargetClaudeMD, ledger.RootRepo, "CLAUDE.md", []string{"M-0001", "M-0002", "M-0003", "M-0004", "M-0005", "M-0006"}},
	} {
		b, err := buildInput(snapshot(c.kind, c.root, c.path), "https://memax.app", at)
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if got := refs(b); !slices.Equal(got, c.want) {
			t.Errorf("%s compiles %v, want %v", c.kind, got, c.want)
		}
	}
}
