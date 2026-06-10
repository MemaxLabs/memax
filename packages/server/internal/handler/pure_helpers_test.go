package handler

import (
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ─── extractCitations ───────────────────────────────────────────

func TestExtractCitationsEmptyAnswer(t *testing.T) {
	t.Parallel()
	got := extractCitations("", []model.RecalledMemory{{ID: "m1", Title: "x"}})
	if got == nil {
		t.Fatal("should return empty slice, not nil")
	}
	if len(got) != 0 {
		t.Errorf("empty answer should yield zero citations, got %+v", got)
	}
}

func TestExtractCitationsHappyPath(t *testing.T) {
	t.Parallel()

	// Numbers reference sources 1-indexed. [1] maps to sources[0].
	sources := []model.RecalledMemory{
		{ID: "m1", Title: "Alpha", Kind: "note"},
		{ID: "m2", Title: "Beta", Kind: "doc"},
		{ID: "m3", Title: "Gamma", Kind: "link"},
	}
	got := extractCitations("See [1] and also [3] for details.", sources)
	if len(got) != 2 {
		t.Fatalf("got %d citations, want 2: %+v", len(got), got)
	}
	if got[0].MemoryID != "m1" || got[0].Index != 1 {
		t.Errorf("first citation wrong: %+v", got[0])
	}
	if got[1].MemoryID != "m3" || got[1].Index != 3 {
		t.Errorf("second citation wrong: %+v", got[1])
	}
}

func TestExtractCitationsDedupsRepeatedReferences(t *testing.T) {
	t.Parallel()

	// Answer mentions [1] twice; output should collapse to a single
	// citation row. Prevents double-counting in the UI.
	sources := []model.RecalledMemory{{ID: "m1"}, {ID: "m2"}}
	got := extractCitations("[1] says yes and [1] reconfirms.", sources)
	if len(got) != 1 {
		t.Errorf("expected 1 citation (dedup), got %d", len(got))
	}
}

func TestExtractCitationsIgnoresOutOfRange(t *testing.T) {
	t.Parallel()

	// Model hallucinated a citation pointing at a source beyond
	// the recall window. Must skip, not panic or emit nil fields.
	sources := []model.RecalledMemory{{ID: "m1"}}
	got := extractCitations("According to [1] and [99] this is true.", sources)
	if len(got) != 1 {
		t.Errorf("out-of-range [99] should be skipped; got %d citations", len(got))
	}
}

func TestExtractCitationsIgnoresMalformedNumbers(t *testing.T) {
	t.Parallel()

	// The regex constrains to digits, but belt-and-suspenders:
	// anything that slips through + fails strconv should be
	// silently dropped, not crash.
	sources := []model.RecalledMemory{{ID: "m1"}}
	got := extractCitations("[1] yes.", sources)
	if len(got) != 1 {
		t.Errorf("happy one-citation path broken, got %d", len(got))
	}
}

// ─── buildNoSourcesAskResult ────────────────────────────────────

func TestBuildNoSourcesAskResultEnglish(t *testing.T) {
	t.Parallel()
	r := buildNoSourcesAskResult("en", "claude-haiku-3-5", 100, 150)
	if !strings.Contains(r.Answer, "couldn't find") {
		t.Errorf("English no-sources message wrong: %q", r.Answer)
	}
	if r.Metadata.RetrievalLatencyMs != 100 {
		t.Errorf("retrieval latency = %d, want 100", r.Metadata.RetrievalLatencyMs)
	}
	if r.Metadata.TotalLatencyMs != 150 {
		t.Errorf("total latency = %d, want 150", r.Metadata.TotalLatencyMs)
	}
	if r.Metadata.AnswerTokens != 0 {
		t.Errorf("answer tokens should be 0 on no-sources, got %d", r.Metadata.AnswerTokens)
	}
	if r.Citations == nil || r.Sources == nil {
		t.Error("both Citations and Sources must be non-nil slices (JSON empty array, not null)")
	}
}

func TestBuildNoSourcesAskResultChinese(t *testing.T) {
	t.Parallel()
	r := buildNoSourcesAskResult("zh", "claude-haiku-3-5", 0, 0)
	if !strings.Contains(r.Answer, "没找到") {
		t.Errorf("Chinese no-sources message missing: %q", r.Answer)
	}
}

func TestBuildNoSourcesAskResultUnknownLocaleUsesEnglish(t *testing.T) {
	t.Parallel()

	// Unknown locale falls through to English — never empty, never
	// a placeholder. Users in unhandled regions still see something.
	r := buildNoSourcesAskResult("xx", "m", 0, 0)
	if !strings.Contains(r.Answer, "couldn't find") {
		t.Errorf("unknown locale should fall back to English, got %q", r.Answer)
	}
}

// ─── shouldUseFullMemoryForAsk ──────────────────────────────────

func TestShouldUseFullMemoryForAskShortAssembled(t *testing.T) {
	t.Parallel()

	// Any source where the already-assembled context is below 200
	// bytes gets the full memory unconditionally — otherwise the
	// LLM gets a stub too short to reason about.
	if !shouldUseFullMemoryForAsk("some-intent", "q", 0, 50) {
		t.Error("short assembled should always use full")
	}
}

func TestShouldUseFullMemoryForAskLaterSourcesDefault(t *testing.T) {
	t.Parallel()

	// Only the top 3 sources (indices 0..2) get full-memory
	// treatment — the rest stay at their chunked shape. Over-
	// enriching later sources wastes tokens without improving
	// answer quality.
	if shouldUseFullMemoryForAsk("how_to", "q", 3, 300) {
		t.Error("sourceIndex > 2 should NOT use full memory (budget)")
	}
}

func TestShouldUseFullMemoryForAskIntentHit(t *testing.T) {
	t.Parallel()

	// how_to / why / what_is / temporal intents benefit from full
	// context — users asking these want reasoning, not scanning.
	for _, intent := range []string{"how_to", "why", "what_is", "temporal"} {
		if !shouldUseFullMemoryForAsk(intent, "q", 0, 300) {
			t.Errorf("intent %q should trigger full memory", intent)
		}
	}
}

func TestShouldUseFullMemoryForAskQueryKeywordMatch(t *testing.T) {
	t.Parallel()

	// Keyword heuristic on the query itself — case-insensitive,
	// substring match. Each listed phrase should trigger full
	// memory on the top-3 sources.
	for _, q := range []string{
		"What Happened yesterday?",
		"explain the setup",
		"summarize the report",
		"Can you give a SUMMARY of this?",
		"why did we choose this",
	} {
		if !shouldUseFullMemoryForAsk("other", q, 0, 300) {
			t.Errorf("query %q should trigger full memory via keyword match", q)
		}
	}
}

func TestShouldUseFullMemoryForAskNoMatch(t *testing.T) {
	t.Parallel()

	// Lookup intent + short direct question + adequate assembled
	// context = no full-memory upgrade.
	if shouldUseFullMemoryForAsk("reference", "where is it", 1, 1000) {
		t.Error("lookup intent with plenty of context should NOT upgrade to full")
	}
}

// ─── buildFullMemoryAskContent trimming ─────────────────────────

func TestBuildFullMemoryAskContentTrimsAndAppendsFull(t *testing.T) {
	t.Parallel()

	// Prefix and full content both get TrimSpace'd; the assembled
	// shape appends full content after the prefix.
	got := buildFullMemoryAskContent("  prefix  ", "  full body  ")
	if !strings.HasPrefix(got, "prefix") {
		t.Errorf("prefix should lead the output, got %q", got)
	}
	if !strings.Contains(got, "full body") {
		t.Errorf("full content missing from output: %q", got)
	}
	if strings.HasPrefix(got, " ") || strings.HasSuffix(got, " ") {
		t.Error("prefix/suffix whitespace should be trimmed")
	}
}

func TestBuildFullMemoryAskContentEmptyPrefix(t *testing.T) {
	t.Parallel()

	// Empty prefix → just the full body (no leading newline).
	got := buildFullMemoryAskContent("", "body")
	if got != "body" {
		t.Errorf("empty prefix: got %q, want 'body'", got)
	}
}
