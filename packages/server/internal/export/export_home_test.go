package export_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/export"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Rule 14 for the personal layer: a repository target exports exactly as
// it did before targets had roots (the ledger now sets root "repo" on every
// one), so every existing space's export is byte-identical; a home target
// says its root and machine.
func TestTargetsExportARootOnlyForHomeFiles(t *testing.T) {
	f := newFixture(t)
	before := f.render(t)[export.TargetsFile]
	for i := range f.targets {
		f.targets[i].Root = ledger.RootRepo
	}
	if after := f.render(t)[export.TargetsFile]; !bytes.Equal(after, before) {
		t.Fatalf("a repository target's export changed with its root:\n%s", after)
	}
	home := f.targets[0]
	home.ID, home.Kind, home.Root, home.Path, home.Label = uuid.New(), ledger.TargetCodexUser, ledger.RootHome,
		".codex/AGENTS.md", "~/.codex/AGENTS.md"
	machine := uuid.New()
	home.Machine = &ledger.TargetMachine{ID: machine, Label: "machine-1", Name: "work-mbp"}
	f.targets = append(f.targets, home)
	f.machines = []ledger.Machine{{ID: machine, Label: "machine-1", Name: "work-mbp", Platform: "darwin", Linked: true,
		LinkedAt: f.targets[0].CreatedAt}}
	got := string(f.render(t)[export.TargetsFile])
	if strings.Count(got, `"root"`) != 1 || !strings.Contains(got, `"root": "home"`) || !strings.Contains(got, `"label": "machine-1"`) ||
		!strings.Contains(got, `"platform": "darwin"`) || strings.Count(got, `"machine"`) != 1 || strings.Contains(got, `"unlinked_at"`) {
		t.Errorf("targets.json with a home file:\n%s", got)
	}
}
