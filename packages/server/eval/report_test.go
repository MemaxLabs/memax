package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestReport() EvalReport {
	return EvalReport{
		Timestamp:   "2026-04-12T10:00:00Z",
		CorpusHash:  "abc123",
		MemoryCount: 50,
		QueryCount:  3,
		Aggregate: MetricSet{
			NDCG5:            0.84,
			NDCG10:           0.87,
			Precision5:       0.82,
			StrongPrecision3: 0.63,
			Recall20:         0.92,
			MRR10:            0.83,
			ERR10:            0.55,
			Harmful10:        0,
			ResultCount:      15,
			LatencyMs:        145,
		},
		Slices: []SliceReport{
			{
				Slice: "factual",
				Count: 2,
				Metrics: MetricSet{
					NDCG5:      0.88,
					Precision5: 0.85,
					MRR10:      0.90,
				},
			},
			{
				Slice: "semantic",
				Count: 1,
				Metrics: MetricSet{
					NDCG5:      0.80,
					Precision5: 0.79,
					MRR10:      0.76,
				},
			},
		},
		Queries: []QueryReport{
			{
				QueryID:   "q1",
				Query:     "what is memax?",
				QueryType: "factual",
				Metrics: MetricSet{
					NDCG5:     1.00,
					MRR10:     1.00,
					Harmful10: 0,
					LatencyMs: 120,
				},
				TopResults: []ResultDetail{
					{Rank: 1, FixtureID: "mem-1", Title: "Memax Overview", Score: 0.95, Relevance: 3},
				},
				Pass: true,
			},
			{
				QueryID:   "q2",
				Query:     "how does retrieval work in the memax pipeline?",
				QueryType: "factual",
				Metrics: MetricSet{
					NDCG5:     0.75,
					MRR10:     0.50,
					Harmful10: 0,
					LatencyMs: 180,
				},
				TopResults: []ResultDetail{
					{Rank: 1, FixtureID: "mem-5", Title: "Retrieval Pipeline", Score: 0.88, Relevance: 2},
					{Rank: 2, FixtureID: "mem-1", Title: "Memax Overview", Score: 0.72, Relevance: 1},
				},
				Pass:       false,
				FailReason: "nDCG@5 below threshold",
			},
			{
				QueryID:   "q3",
				Query:     "unrelated topic with zero results expected",
				QueryType: "semantic",
				Metrics: MetricSet{
					NDCG5:     0.00,
					MRR10:     0.00,
					Harmful10: 1,
					LatencyMs: 95,
				},
				TopResults: []ResultDetail{},
				Pass:       false,
				FailReason: "harmful result detected",
			},
		},
	}
}

func TestFormatMarkdownReport(t *testing.T) {
	report := newTestReport()
	md := FormatMarkdownReport(report)

	// Check top-level headers
	requireContains(t, md, "# Retrieval Eval Report")
	requireContains(t, md, "## Aggregate Metrics")
	requireContains(t, md, "## Per-Slice Metrics")
	requireContains(t, md, "## Per-Query Results")

	// Check report metadata
	requireContains(t, md, "2026-04-12T10:00:00Z")
	requireContains(t, md, "50 memories")
	requireContains(t, md, "3 queries")

	// Check aggregate metric values
	requireContains(t, md, "0.84")  // nDCG@5
	requireContains(t, md, "0.87")  // nDCG@10
	requireContains(t, md, "0.82")  // Precision@5 (both value and target)
	requireContains(t, md, "0.63")  // StrongPrecision@3
	requireContains(t, md, "0.92")  // Recall@20
	requireContains(t, md, "0.83")  // MRR@10
	requireContains(t, md, "145ms") // Avg Latency

	// Check aggregate targets (synced with eval_test.go thresholds)
	requireContains(t, md, "0.70") // nDCG@5, nDCG@10, and MRR@10 target
	requireContains(t, md, "0.20") // Precision@5 target
	requireContains(t, md, "0.25") // StrongPrecision@3 target
	requireContains(t, md, "0.60") // Recall@20 target

	// Check metric labels
	requireContains(t, md, "nDCG@5")
	requireContains(t, md, "nDCG@10")
	requireContains(t, md, "Precision@5")
	requireContains(t, md, "StrongPrecision@3")
	requireContains(t, md, "Recall@20")
	requireContains(t, md, "MRR@10")
	requireContains(t, md, "Harmful@10")
	requireContains(t, md, "Avg Latency")

	// Check PASS/FAIL in aggregate (all should pass since values >= targets)
	// Count PASS occurrences — at least 6 aggregate metrics should pass
	passCount := strings.Count(md, "PASS")
	if passCount < 6 {
		t.Errorf("expected at least 6 PASS entries in aggregate metrics, got %d", passCount)
	}

	// Check slice rows
	requireContains(t, md, "factual")
	requireContains(t, md, "semantic")
	requireContains(t, md, "0.88") // factual nDCG@5
	requireContains(t, md, "0.85") // factual P@5

	// Check per-query rows
	requireContains(t, md, "q1")
	requireContains(t, md, "q2")
	requireContains(t, md, "q3")
	requireContains(t, md, "what is memax?")
	requireContains(t, md, "120ms")
	requireContains(t, md, "180ms")
	requireContains(t, md, "95ms")

	// Check FAIL appears for failing queries
	requireContains(t, md, "FAIL")
}

func TestFormatMarkdownReport_NoSlices(t *testing.T) {
	report := newTestReport()
	report.Slices = nil

	md := FormatMarkdownReport(report)

	// Should still have aggregate and per-query sections
	requireContains(t, md, "## Aggregate Metrics")
	requireContains(t, md, "## Per-Query Results")

	// Should NOT have per-slice section
	if strings.Contains(md, "## Per-Slice Metrics") {
		t.Error("expected no Per-Slice Metrics section when slices are empty")
	}
}

func TestFormatMarkdownReport_NoQueries(t *testing.T) {
	report := newTestReport()
	report.Queries = nil

	md := FormatMarkdownReport(report)

	requireContains(t, md, "## Aggregate Metrics")

	if strings.Contains(md, "## Per-Query Results") {
		t.Error("expected no Per-Query Results section when queries are empty")
	}
}

func TestFormatMarkdownReport_LongQueryTruncated(t *testing.T) {
	report := EvalReport{
		Timestamp:   "2026-04-12T10:00:00Z",
		MemoryCount: 10,
		QueryCount:  1,
		Queries: []QueryReport{
			{
				QueryID: "q-long",
				Query:   "this is a very long query string that should definitely be truncated because it exceeds the maximum allowed length for display",
				Pass:    true,
			},
		},
	}

	md := FormatMarkdownReport(report)

	// The truncated query should end with "..."
	requireContains(t, md, "...")
	// Should not contain the full query
	if strings.Contains(md, "maximum allowed length for display") {
		t.Error("expected long query to be truncated")
	}
}

func TestFormatMarkdownReport_HarmfulFails(t *testing.T) {
	report := newTestReport()
	report.Aggregate.Harmful10 = 2

	md := FormatMarkdownReport(report)

	// Find the Harmful@10 row and verify it says FAIL
	lines := strings.Split(md, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Harmful@10") {
			if !strings.Contains(line, "FAIL") {
				t.Errorf("expected Harmful@10 row to show FAIL when harmful > 0, got: %s", line)
			}
			break
		}
	}
}

func TestWriteJSONReport(t *testing.T) {
	report := newTestReport()

	dir := t.TempDir()
	path := filepath.Join(dir, "eval-report.json")

	if err := WriteJSONReport(report, path); err != nil {
		t.Fatalf("WriteJSONReport: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report file: %v", err)
	}

	var loaded EvalReport
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}

	// Verify top-level fields
	if loaded.Timestamp != report.Timestamp {
		t.Errorf("timestamp: got %q, want %q", loaded.Timestamp, report.Timestamp)
	}
	if loaded.CorpusHash != report.CorpusHash {
		t.Errorf("corpus_hash: got %q, want %q", loaded.CorpusHash, report.CorpusHash)
	}
	if loaded.MemoryCount != report.MemoryCount {
		t.Errorf("memory_count: got %d, want %d", loaded.MemoryCount, report.MemoryCount)
	}
	if loaded.QueryCount != report.QueryCount {
		t.Errorf("query_count: got %d, want %d", loaded.QueryCount, report.QueryCount)
	}

	// Verify aggregate metrics
	if loaded.Aggregate.NDCG5 != report.Aggregate.NDCG5 {
		t.Errorf("aggregate.ndcg_5: got %f, want %f", loaded.Aggregate.NDCG5, report.Aggregate.NDCG5)
	}
	if loaded.Aggregate.MRR10 != report.Aggregate.MRR10 {
		t.Errorf("aggregate.mrr_10: got %f, want %f", loaded.Aggregate.MRR10, report.Aggregate.MRR10)
	}
	if loaded.Aggregate.Harmful10 != report.Aggregate.Harmful10 {
		t.Errorf("aggregate.harmful_10: got %d, want %d", loaded.Aggregate.Harmful10, report.Aggregate.Harmful10)
	}
	if loaded.Aggregate.LatencyMs != report.Aggregate.LatencyMs {
		t.Errorf("aggregate.latency_ms: got %d, want %d", loaded.Aggregate.LatencyMs, report.Aggregate.LatencyMs)
	}

	// Verify slices
	if len(loaded.Slices) != len(report.Slices) {
		t.Fatalf("slices: got %d, want %d", len(loaded.Slices), len(report.Slices))
	}
	for i, s := range loaded.Slices {
		if s.Slice != report.Slices[i].Slice {
			t.Errorf("slice[%d].slice: got %q, want %q", i, s.Slice, report.Slices[i].Slice)
		}
		if s.Count != report.Slices[i].Count {
			t.Errorf("slice[%d].count: got %d, want %d", i, s.Count, report.Slices[i].Count)
		}
	}

	// Verify queries
	if len(loaded.Queries) != len(report.Queries) {
		t.Fatalf("queries: got %d, want %d", len(loaded.Queries), len(report.Queries))
	}
	for i, q := range loaded.Queries {
		if q.QueryID != report.Queries[i].QueryID {
			t.Errorf("query[%d].query_id: got %q, want %q", i, q.QueryID, report.Queries[i].QueryID)
		}
		if q.Pass != report.Queries[i].Pass {
			t.Errorf("query[%d].pass: got %v, want %v", i, q.Pass, report.Queries[i].Pass)
		}
		if q.FailReason != report.Queries[i].FailReason {
			t.Errorf("query[%d].fail_reason: got %q, want %q", i, q.FailReason, report.Queries[i].FailReason)
		}
		if len(q.TopResults) != len(report.Queries[i].TopResults) {
			t.Errorf("query[%d].top_results: got %d, want %d", i, len(q.TopResults), len(report.Queries[i].TopResults))
		}
	}

	// Verify the JSON is pretty-printed (indented)
	if !strings.Contains(string(data), "\n  ") {
		t.Error("expected formatted (indented) JSON output")
	}

	// Verify the JSON ends with a newline
	if data[len(data)-1] != '\n' {
		t.Error("expected JSON file to end with a newline")
	}
}

func TestWriteJSONReport_InvalidPath(t *testing.T) {
	report := newTestReport()

	err := WriteJSONReport(report, "/nonexistent/dir/report.json")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

// requireContains is a test helper that fails if s does not contain substr.
func requireContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected output to contain %q, but it did not.\nFull output:\n%s", substr, s)
	}
}
