package locomo

import (
	"strings"
	"testing"
)

func TestRunnerQACutoffsNormalize(t *testing.T) {
	r := &Runner{cfg: Config{QACutoffs: []int{20, 10, 10, 0, -1}}}

	got := r.qaCutoffs()
	want := []int{10, 20}
	if len(got) != len(want) {
		t.Fatalf("qaCutoffs len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("qaCutoffs[%d] = %d, want %d (%v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildEvidenceContext(t *testing.T) {
	corpus := []CorpusDoc{
		{ID: "D1:1", SourceIDs: []string{"D1:1"}, Timestamp: "1:00 pm on 1 May, 2023", Text: "Alice likes tea."},
		{ID: "D1:2", SourceIDs: []string{"D1:2"}, Timestamp: "2:00 pm on 1 May, 2023", Text: "Bob likes coffee."},
	}

	got := buildEvidenceContext(corpus, []string{"D1:2"})
	if got == "" {
		t.Fatal("buildEvidenceContext returned empty context")
	}
	if got == corpus[0].Text {
		t.Fatalf("buildEvidenceContext matched wrong evidence: %q", got)
	}
	if want := "Bob likes coffee."; !strings.Contains(got, want) {
		t.Fatalf("buildEvidenceContext = %q, want it to contain %q", got, want)
	}
}
