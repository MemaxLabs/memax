package dreams

import (
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Report generation is a deterministic formatting function — the
// perfect shape for exhaustive table-driven testing. Each branch
// below corresponds to one action type (merge / contradiction /
// archive / organize / restructure) and one warning variant
// (bootstrap / organize-timeout / organize-error / organize-empty).
// Existing tests covered two branches; this file closes the rest
// so a future refactor of the string templates can't silently drop
// a section.

func TestGenerateReportEmptyScanNoWarnings(t *testing.T) {
	t.Parallel()

	run := &model.DreamRun{MemoriesScanned: 0}
	report := (&Engine{}).generateReport(run, nil)
	if !strings.Contains(report, "your memory is clean") {
		t.Fatalf("empty + no-warnings case should produce the clean message, got: %q", report)
	}
}

func TestGenerateReportMergeSection(t *testing.T) {
	t.Parallel()

	run := &model.DreamRun{
		MemoriesScanned:  10,
		DuplicatesMerged: 2,
	}
	actions := []model.DreamAction{
		{ActionType: "merge", Reason: "near-duplicate notes about lunch"},
		{ActionType: "merge", Reason: "conflicting email drafts"},
	}
	report := (&Engine{}).generateReport(run, actions)
	if !strings.Contains(report, "Merged 2 duplicate(s)") {
		t.Errorf("missing merge heading: %q", report)
	}
	if !strings.Contains(report, "near-duplicate notes about lunch") {
		t.Errorf("merge action reason not rendered: %q", report)
	}
	if !strings.Contains(report, "conflicting email drafts") {
		t.Errorf("second merge action reason not rendered: %q", report)
	}
}

func TestGenerateReportContradictionSection(t *testing.T) {
	t.Parallel()

	run := &model.DreamRun{
		MemoriesScanned:     5,
		ContradictionsFound: 1,
	}
	actions := []model.DreamAction{
		{ActionType: "contradiction", Reason: "conflicting dates on the same project"},
	}
	report := (&Engine{}).generateReport(run, actions)
	if !strings.Contains(report, "Found 1 contradiction(s)") {
		t.Errorf("contradiction heading missing: %q", report)
	}
	if !strings.Contains(report, "conflicting dates") {
		t.Errorf("contradiction reason not rendered: %q", report)
	}
}

func TestGenerateReportArchiveSection(t *testing.T) {
	t.Parallel()

	run := &model.DreamRun{
		MemoriesScanned:  100,
		MemoriesArchived: 3,
	}
	actions := []model.DreamAction{
		{ActionType: "archive", Reason: "stale draft > 60 days"},
	}
	report := (&Engine{}).generateReport(run, actions)
	if !strings.Contains(report, "Archived 3 stale memory(ies)") {
		t.Errorf("archive heading missing: %q", report)
	}
}

func TestGenerateReportOrganizeSection(t *testing.T) {
	t.Parallel()

	run := &model.DreamRun{
		MemoriesScanned:   40,
		MemoriesOrganized: 12,
	}
	actions := []model.DreamAction{
		{ActionType: "organize", Reason: "assigned to Travel/2026-Q2 topic"},
	}
	report := (&Engine{}).generateReport(run, actions)
	if !strings.Contains(report, "Organized 12 memory(ies) into topics") {
		t.Errorf("organize heading missing: %q", report)
	}
}

func TestGenerateReportRestructureSection(t *testing.T) {
	t.Parallel()

	run := &model.DreamRun{
		MemoriesScanned:    20,
		TopicsRestructured: 4,
	}
	actions := []model.DreamAction{
		{ActionType: "restructure", Reason: "moved Coffee under Food"},
	}
	report := (&Engine{}).generateReport(run, actions)
	if !strings.Contains(report, "Restructured 4 topic(s) into hierarchy") {
		t.Errorf("restructure heading missing: %q", report)
	}
}

func TestGenerateReportCombinesAllSections(t *testing.T) {
	t.Parallel()

	// Kitchen-sink shape: every action type fires in one run.
	// Guards against a refactor reordering sections (the UI
	// probably doesn't care, but the test surfaces the change).
	run := &model.DreamRun{
		MemoriesScanned:     50,
		DuplicatesMerged:    1,
		ContradictionsFound: 1,
		MemoriesArchived:    1,
		MemoriesOrganized:   1,
		TopicsRestructured:  1,
	}
	actions := []model.DreamAction{
		{ActionType: "merge", Reason: "m"},
		{ActionType: "contradiction", Reason: "c"},
		{ActionType: "archive", Reason: "a"},
		{ActionType: "organize", Reason: "o"},
		{ActionType: "restructure", Reason: "r"},
	}
	report := (&Engine{}).generateReport(run, actions)
	sections := []string{
		"Merged 1 duplicate(s)",
		"Found 1 contradiction(s)",
		"Archived 1 stale memory(ies)",
		"Organized 1 memory(ies)",
		"Restructured 1 topic(s)",
	}
	last := -1
	for _, s := range sections {
		idx := strings.Index(report, s)
		if idx < 0 {
			t.Errorf("section missing: %q", s)
			continue
		}
		if idx <= last {
			t.Errorf("sections out of order — %q appeared before or at index of previous section", s)
		}
		last = idx
	}
}

func TestGenerateReportEmptyWithWarningsSkipsCleanMessage(t *testing.T) {
	t.Parallel()

	// No actions, but a bootstrap warning. Report should show the
	// warning instead of the "memory is clean" happy path.
	run := &model.DreamRun{
		Mode:            dreamModeBootstrap,
		MemoriesScanned: 200,
	}
	report := (&Engine{}).generateReport(run, nil)
	if strings.Contains(report, "your memory is clean") {
		t.Errorf("bootstrap mode with no actions should not claim 'clean', got: %q", report)
	}
	if !strings.Contains(report, "Bootstrap mode") {
		t.Errorf("bootstrap warning missing: %q", report)
	}
}

func TestDreamReportWarningsNilRun(t *testing.T) {
	t.Parallel()

	if out := dreamReportWarnings(nil); out != nil {
		t.Errorf("nil run should produce nil warnings, got %v", out)
	}
}

func TestDreamReportWarningsOrganizeErrorPath(t *testing.T) {
	t.Parallel()

	// Pure LLM errors (no timeouts) — exercises the second branch
	// of the switch inside dreamReportWarnings.
	run := &model.DreamRun{
		MemoriesOrganized: 4,
		PhaseMetrics: map[string]model.DreamPhaseMetrics{
			"organize": {
				Candidates: 20,
				Batches:    4,
				LLMCalls:   4,
				LLMErrors:  2,
			},
		},
	}
	warnings := dreamReportWarnings(run)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "Organize failed on 2 of 4 batch(es)") {
		t.Errorf("unexpected error warning: %q", warnings[0])
	}
	if !strings.Contains(warnings[0], "16 inbox memory(ies) remain unassigned") {
		t.Errorf("remaining count wrong: %q", warnings[0])
	}
}

func TestDreamReportWarningsOrganizeZeroAssignedPath(t *testing.T) {
	t.Parallel()

	// Candidates reviewed but zero assigned + zero errors/timeouts
	// — the third case in the switch. Signals a suspect LLM pass
	// where the model reviewed memories but picked nothing.
	run := &model.DreamRun{
		MemoriesOrganized: 0,
		PhaseMetrics: map[string]model.DreamPhaseMetrics{
			"organize": {
				Candidates: 15,
				Batches:    2,
				LLMCalls:   2,
			},
		},
	}
	warnings := dreamReportWarnings(run)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "reviewed 15 inbox memory(ies) but did not assign") {
		t.Errorf("unexpected zero-assigned warning: %q", warnings[0])
	}
}

func TestDreamReportWarningsOrganizeRemainingFloorsAtZero(t *testing.T) {
	t.Parallel()

	// Organized count exceeds candidates (rare but possible if the
	// metrics drift) — remaining must floor at 0, never go negative
	// or produce a weird count in the string.
	run := &model.DreamRun{
		MemoriesOrganized: 20,
		PhaseMetrics: map[string]model.DreamPhaseMetrics{
			"organize": {
				Candidates:  10,
				Batches:     2,
				LLMCalls:    2,
				LLMTimeouts: 1,
			},
		},
	}
	warnings := dreamReportWarnings(run)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if !strings.Contains(warnings[0], "0 inbox memory(ies) remain unassigned") {
		t.Errorf("remaining should floor to 0, got: %q", warnings[0])
	}
}

func TestDreamReportWarningsFallsBackToLLMCallsWhenBatchesMissing(t *testing.T) {
	t.Parallel()

	// Metrics have LLMCalls set but Batches = 0 (older runs may
	// have emitted this shape before the batches column existed).
	// The helper should fall back to LLMCalls as the denominator
	// so the message is still coherent.
	run := &model.DreamRun{
		MemoriesOrganized: 1,
		PhaseMetrics: map[string]model.DreamPhaseMetrics{
			"organize": {
				Candidates: 5,
				Batches:    0,
				LLMCalls:   5,
				LLMErrors:  2,
			},
		},
	}
	warnings := dreamReportWarnings(run)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if !strings.Contains(warnings[0], "Organize failed on 2 of 5 batch(es)") {
		t.Errorf("fallback-to-LLMCalls path wrong: %q", warnings[0])
	}
}

func TestContradictionNotificationSourceIDOrderIndependent(t *testing.T) {
	t.Parallel()

	a := contradictionNotificationSourceID("zzz", "aaa")
	b := contradictionNotificationSourceID("aaa", "zzz")
	if a != b {
		t.Errorf("order-independence lost: (zzz,aaa)=%q vs (aaa,zzz)=%q", a, b)
	}
	if !strings.HasPrefix(a, "contradiction:") {
		t.Errorf("prefix missing: %q", a)
	}
}

func TestTopicRestructureNotificationSourceIDCaseInsensitive(t *testing.T) {
	t.Parallel()

	// Parent name is case-folded so "Food" and "FOOD" produce the
	// same dedup key — renaming casing shouldn't resurrect a
	// stale suggestion.
	a := topicRestructureNotificationSourceID("child-1", "Food")
	b := topicRestructureNotificationSourceID("child-1", "FOOD")
	if a != b {
		t.Errorf("case-sensitivity not folded: %q vs %q", a, b)
	}
}

func TestTopicMergeNotificationSourceIDStable(t *testing.T) {
	t.Parallel()

	// Signature is (targetID, []childIDs) — the target is the
	// canonical "winner" of the merge, children are candidates to
	// fold in. The child slice is sorted internally so re-ordering
	// input collapses to the same dedup key.
	id := topicMergeNotificationSourceID("target", []string{"zzz", "aaa"})
	reordered := topicMergeNotificationSourceID("target", []string{"aaa", "zzz"})
	if id != reordered {
		t.Errorf("child slice ordering leaks into dedup key: %q vs %q", id, reordered)
	}
	if !strings.HasPrefix(id, "topic_merge:") {
		t.Errorf("prefix missing: %q", id)
	}
	if !strings.Contains(id, "target") || !strings.Contains(id, "aaa") || !strings.Contains(id, "zzz") {
		t.Errorf("dedup key missing a participant: %q", id)
	}
}

func TestTopicMergeNotificationSourceIDInputNotMutated(t *testing.T) {
	t.Parallel()

	// Caller's slice must not be reordered as a side effect of
	// computing the dedup key — implementations often sort in
	// place and leak that ordering back to callers with stale
	// state expectations.
	input := []string{"zzz", "aaa", "mmm"}
	snapshot := append([]string(nil), input...)
	_ = topicMergeNotificationSourceID("target", input)
	for i := range snapshot {
		if input[i] != snapshot[i] {
			t.Fatalf("caller's slice mutated: before %v, after %v", snapshot, input)
		}
	}
}
