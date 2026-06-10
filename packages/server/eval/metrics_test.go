package eval

import (
	"math"
	"testing"
)

const floatTol = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < floatTol
}

// ---------------------------------------------------------------------------
// TestNDCG
// ---------------------------------------------------------------------------

func TestNDCG(t *testing.T) {
	tests := []struct {
		name      string
		results   []RankedResult
		relevance map[string]int
		k         int
		want      float64
	}{
		{
			name: "perfect ranking",
			results: []RankedResult{
				{FixtureID: "a", Rank: 1},
				{FixtureID: "b", Rank: 2},
				{FixtureID: "c", Rank: 3},
			},
			relevance: map[string]int{"a": 3, "b": 2, "c": 1},
			k:         3,
			want:      1.0,
		},
		{
			name: "reversed ranking is lower",
			results: []RankedResult{
				{FixtureID: "c", Rank: 1},
				{FixtureID: "b", Rank: 2},
				{FixtureID: "a", Rank: 3},
			},
			relevance: map[string]int{"a": 3, "b": 2, "c": 1},
			k:         3,
		},
		{
			name:      "empty results",
			results:   []RankedResult{},
			relevance: map[string]int{"a": 3},
			k:         5,
			want:      0.0,
		},
		{
			name: "no relevant docs returns 0",
			results: []RankedResult{
				{FixtureID: "a", Rank: 1},
			},
			relevance: map[string]int{"a": 0, "b": 0},
			k:         5,
			want:      0.0,
		},
		{
			name: "k larger than results",
			results: []RankedResult{
				{FixtureID: "a", Rank: 1},
			},
			relevance: map[string]int{"a": 3},
			k:         10,
			want:      1.0,
		},
		{
			name: "harmful treated as 0 for gain",
			results: []RankedResult{
				{FixtureID: "h", Rank: 1},
				{FixtureID: "a", Rank: 2},
			},
			relevance: map[string]int{"h": -1, "a": 3},
			k:         2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NDCG(tt.results, tt.relevance, tt.k)

			switch tt.name {
			case "reversed ranking is lower":
				perfect := NDCG([]RankedResult{
					{FixtureID: "a", Rank: 1},
					{FixtureID: "b", Rank: 2},
					{FixtureID: "c", Rank: 3},
				}, tt.relevance, tt.k)
				if got >= perfect {
					t.Errorf("reversed nDCG (%.6f) should be less than perfect (%.6f)", got, perfect)
				}
				if got <= 0 {
					t.Errorf("reversed nDCG should still be positive, got %.6f", got)
				}
			case "harmful treated as 0 for gain":
				// Harmful at rank 1 should produce lower nDCG than ideal
				if got >= 1.0 {
					t.Errorf("harmful doc at rank 1 should give nDCG < 1.0, got %.6f", got)
				}
				if got <= 0 {
					t.Errorf("should still be positive (relevant doc at rank 2), got %.6f", got)
				}
			default:
				if !approxEqual(got, tt.want) {
					t.Errorf("NDCG = %.6f, want %.6f", got, tt.want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestPrecisionAtK
// ---------------------------------------------------------------------------

func TestPrecisionAtK(t *testing.T) {
	tests := []struct {
		name      string
		results   []RankedResult
		relevance map[string]int
		k         int
		minRel    int
		want      float64
	}{
		{
			name: "all relevant",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "c"},
			},
			relevance: map[string]int{"a": 3, "b": 2, "c": 1},
			k:         3,
			minRel:    1,
			want:      1.0,
		},
		{
			name: "half relevant",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "c"}, {FixtureID: "d"},
			},
			relevance: map[string]int{"a": 2, "b": 0, "c": 1, "d": 0},
			k:         4,
			minRel:    1,
			want:      0.5,
		},
		{
			name: "none relevant",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"},
			},
			relevance: map[string]int{"a": 0, "b": 0},
			k:         2,
			minRel:    1,
			want:      0.0,
		},
		{
			name: "k larger than results",
			results: []RankedResult{
				{FixtureID: "a"},
			},
			relevance: map[string]int{"a": 3},
			k:         5,
			minRel:    1,
			want:      0.2, // 1 relevant out of k=5
		},
		{
			name:      "empty results",
			results:   []RankedResult{},
			relevance: map[string]int{},
			k:         3,
			minRel:    1,
			want:      0.0,
		},
		{
			name: "unlisted fixture treated as 0",
			results: []RankedResult{
				{FixtureID: "unknown"},
			},
			relevance: map[string]int{"a": 3},
			k:         1,
			minRel:    1,
			want:      0.0,
		},
		{
			name: "strong precision minRel=2",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "c"},
			},
			relevance: map[string]int{"a": 3, "b": 1, "c": 2},
			k:         3,
			minRel:    2,
			want:      2.0 / 3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrecisionAtK(tt.results, tt.relevance, tt.k, tt.minRel)
			if !approxEqual(got, tt.want) {
				t.Errorf("PrecisionAtK = %.6f, want %.6f", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRecallAtK
// ---------------------------------------------------------------------------

func TestRecallAtK(t *testing.T) {
	tests := []struct {
		name      string
		results   []RankedResult
		relevance map[string]int
		k         int
		minRel    int
		want      float64
	}{
		{
			name: "full recall",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "c"},
			},
			relevance: map[string]int{"a": 2, "b": 1, "c": 3},
			k:         5,
			minRel:    1,
			want:      1.0,
		},
		{
			name: "partial recall",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"},
			},
			relevance: map[string]int{"a": 2, "b": 0, "c": 3, "d": 1},
			k:         2,
			minRel:    1,
			want:      1.0 / 3.0, // found "a" out of {a,c,d}
		},
		{
			name: "no relevant docs is vacuously true",
			results: []RankedResult{
				{FixtureID: "a"},
			},
			relevance: map[string]int{"a": 0, "b": 0},
			k:         5,
			minRel:    1,
			want:      1.0,
		},
		{
			name:      "empty results with relevant docs",
			results:   []RankedResult{},
			relevance: map[string]int{"a": 3},
			k:         5,
			minRel:    1,
			want:      0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RecallAtK(tt.results, tt.relevance, tt.k, tt.minRel)
			if !approxEqual(got, tt.want) {
				t.Errorf("RecallAtK = %.6f, want %.6f", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestMRRAtK
// ---------------------------------------------------------------------------

func TestMRRAtK(t *testing.T) {
	tests := []struct {
		name      string
		results   []RankedResult
		relevance map[string]int
		k         int
		minRel    int
		want      float64
	}{
		{
			name: "first at rank 1",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"},
			},
			relevance: map[string]int{"a": 3, "b": 1},
			k:         5,
			minRel:    2,
			want:      1.0,
		},
		{
			name: "first at rank 3",
			results: []RankedResult{
				{FixtureID: "x"}, {FixtureID: "y"}, {FixtureID: "a"},
			},
			relevance: map[string]int{"x": 0, "y": 1, "a": 3},
			k:         5,
			minRel:    2,
			want:      1.0 / 3.0,
		},
		{
			name: "not found within k",
			results: []RankedResult{
				{FixtureID: "x"}, {FixtureID: "y"},
			},
			relevance: map[string]int{"x": 0, "y": 0, "a": 3},
			k:         2,
			minRel:    2,
			want:      0.0,
		},
		{
			name:      "empty results",
			results:   []RankedResult{},
			relevance: map[string]int{"a": 3},
			k:         5,
			minRel:    2,
			want:      0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MRRAtK(tt.results, tt.relevance, tt.k, tt.minRel)
			if !approxEqual(got, tt.want) {
				t.Errorf("MRRAtK = %.6f, want %.6f", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestERRAtK
// ---------------------------------------------------------------------------

func TestERRAtK(t *testing.T) {
	tests := []struct {
		name      string
		results   []RankedResult
		relevance map[string]int
		k         int
		maxRel    int
		wantMin   float64 // lower bound (inclusive)
		wantMax   float64 // upper bound (inclusive)
	}{
		{
			name: "ideal doc at rank 1",
			results: []RankedResult{
				{FixtureID: "a"},
			},
			relevance: map[string]int{"a": 3},
			k:         5,
			maxRel:    3,
			// R_1 = (2^3 - 1) / 2^3 = 7/8 = 0.875
			// ERR = 1.0 * 0.875 / 1 = 0.875
			wantMin: 0.874,
			wantMax: 0.876,
		},
		{
			name: "cascade with multiple docs",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "c"},
			},
			relevance: map[string]int{"a": 1, "b": 2, "c": 3},
			k:         3,
			maxRel:    3,
			wantMin:   0.0,
			wantMax:   1.0,
		},
		{
			name: "all irrelevant",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"},
			},
			relevance: map[string]int{"a": 0, "b": 0},
			k:         2,
			maxRel:    3,
			wantMin:   0.0,
			wantMax:   0.0001,
		},
		{
			name: "harmful treated as 0",
			results: []RankedResult{
				{FixtureID: "h"},
			},
			relevance: map[string]int{"h": -1},
			k:         1,
			maxRel:    3,
			wantMin:   0.0,
			wantMax:   0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ERRAtK(tt.results, tt.relevance, tt.k, tt.maxRel)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("ERRAtK = %.6f, want in [%.4f, %.4f]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHarmfulAtK
// ---------------------------------------------------------------------------

func TestHarmfulAtK(t *testing.T) {
	tests := []struct {
		name      string
		results   []RankedResult
		relevance map[string]int
		k         int
		want      int
	}{
		{
			name: "two harmful in top-3",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "c"},
			},
			relevance: map[string]int{"a": -1, "b": 2, "c": -1},
			k:         3,
			want:      2,
		},
		{
			name: "no harmful",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"},
			},
			relevance: map[string]int{"a": 1, "b": 3},
			k:         2,
			want:      0,
		},
		{
			name: "harmful outside k not counted",
			results: []RankedResult{
				{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "h"},
			},
			relevance: map[string]int{"a": 1, "b": 2, "h": -1},
			k:         2,
			want:      0,
		},
		{
			name:      "empty results",
			results:   []RankedResult{},
			relevance: map[string]int{"a": -1},
			k:         5,
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HarmfulAtK(tt.results, tt.relevance, tt.k)
			if got != tt.want {
				t.Errorf("HarmfulAtK = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestComputeMetrics
// ---------------------------------------------------------------------------

func TestComputeMetrics(t *testing.T) {
	qr := QueryResult{
		QueryID:   "q1",
		Query:     "test query",
		QueryType: "factual",
		Results: []RankedResult{
			{FixtureID: "a", Rank: 1, Score: 0.9},
			{FixtureID: "b", Rank: 2, Score: 0.8},
			{FixtureID: "c", Rank: 3, Score: 0.7},
			{FixtureID: "d", Rank: 4, Score: 0.6},
			{FixtureID: "e", Rank: 5, Score: 0.5},
		},
		Relevance: map[string]int{
			"a": 3, "b": 2, "c": 0, "d": 1, "e": -1,
		},
		LatencyMs: 42,
	}

	ms := ComputeMetrics(qr)

	// Verify nDCG@5 is between 0 and 1
	if ms.NDCG5 <= 0 || ms.NDCG5 > 1.0 {
		t.Errorf("NDCG5 = %.6f, want (0, 1]", ms.NDCG5)
	}
	// nDCG@10 should be the same as @5 since we only have 5 results
	if !approxEqual(ms.NDCG5, ms.NDCG10) {
		t.Errorf("NDCG5 (%.6f) != NDCG10 (%.6f) with only 5 results", ms.NDCG5, ms.NDCG10)
	}

	// Precision@5 (minRel=1): a(3), b(2), d(1) are relevant = 3/5
	if !approxEqual(ms.Precision5, 0.6) {
		t.Errorf("Precision5 = %.6f, want 0.6", ms.Precision5)
	}

	// StrongPrecision@3 (minRel=2): a(3), b(2) out of first 3 = 2/3
	if !approxEqual(ms.StrongPrecision3, 2.0/3.0) {
		t.Errorf("StrongPrecision3 = %.6f, want %.6f", ms.StrongPrecision3, 2.0/3.0)
	}

	// MRR@10 (minRel=2): first with rel>=2 is "a" at rank 1 = 1.0
	if !approxEqual(ms.MRR10, 1.0) {
		t.Errorf("MRR10 = %.6f, want 1.0", ms.MRR10)
	}

	// Harmful@10: "e" has rel=-1 = 1
	if ms.Harmful10 != 1 {
		t.Errorf("Harmful10 = %d, want 1", ms.Harmful10)
	}

	// ResultCount
	if ms.ResultCount != 5 {
		t.Errorf("ResultCount = %d, want 5", ms.ResultCount)
	}

	// Latency
	if ms.LatencyMs != 42 {
		t.Errorf("LatencyMs = %d, want 42", ms.LatencyMs)
	}
}

// ---------------------------------------------------------------------------
// TestAggregateMetrics
// ---------------------------------------------------------------------------

func TestAggregateMetrics(t *testing.T) {
	// Two positive queries with known precision values.
	q1 := QueryResult{
		QueryID: "q1",
		Results: []RankedResult{
			{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "c"},
			{FixtureID: "d"}, {FixtureID: "e"},
		},
		Relevance: map[string]int{"a": 3, "b": 2, "c": 1, "d": 0, "e": 0},
		LatencyMs: 10,
	}
	q2 := QueryResult{
		QueryID: "q2",
		Results: []RankedResult{
			{FixtureID: "x"}, {FixtureID: "y"}, {FixtureID: "z"},
			{FixtureID: "w"}, {FixtureID: "v"},
		},
		Relevance: map[string]int{"x": 0, "y": 3, "z": 0, "w": 0, "v": 0},
		LatencyMs: 30,
	}
	// Negative query: should NOT contribute to ranking metrics.
	qNeg := QueryResult{
		QueryID:  "qneg",
		Negative: true,
		Results: []RankedResult{
			{FixtureID: "h"},
		},
		Relevance: map[string]int{"h": -1},
		LatencyMs: 5,
	}

	agg := AggregateMetrics([]QueryResult{q1, q2, qNeg})

	// Precision@5 for q1: a(3),b(2),c(1) relevant = 3/5 = 0.6
	// Precision@5 for q2: y(3) relevant = 1/5 = 0.2
	// Average of positive only: (0.6 + 0.2) / 2 = 0.4
	wantPrecision5 := 0.4
	if !approxEqual(agg.Precision5, wantPrecision5) {
		t.Errorf("Precision5 = %.6f, want %.6f", agg.Precision5, wantPrecision5)
	}

	// Harmful should sum across all queries.
	// q1: 0 harmful, q2: 0 harmful, qNeg: 1 harmful = 1
	if agg.Harmful10 != 1 {
		t.Errorf("Harmful10 = %d, want 1", agg.Harmful10)
	}

	// Average latency: (10 + 30 + 5) / 3 = 15
	if agg.LatencyMs != 15 {
		t.Errorf("LatencyMs = %d, want 15", agg.LatencyMs)
	}

	// Test empty
	empty := AggregateMetrics(nil)
	if empty.NDCG5 != 0 || empty.Harmful10 != 0 {
		t.Errorf("empty aggregate should be zero MetricSet, got %+v", empty)
	}

	// Test all negative queries produce zero ranking metrics.
	allNeg := AggregateMetrics([]QueryResult{qNeg})
	if allNeg.NDCG5 != 0 || allNeg.Precision5 != 0 || allNeg.MRR10 != 0 {
		t.Errorf("all-negative aggregate should have zero ranking metrics, got %+v", allNeg)
	}
}

// ---------------------------------------------------------------------------
// TestAggregateBySlice
// ---------------------------------------------------------------------------

func TestAggregateBySlice(t *testing.T) {
	q1 := QueryResult{
		QueryID: "q1",
		Slices:  []string{"factual", "short"},
		Results: []RankedResult{
			{FixtureID: "a"}, {FixtureID: "b"}, {FixtureID: "c"},
			{FixtureID: "d"}, {FixtureID: "e"},
		},
		Relevance: map[string]int{"a": 3, "b": 2, "c": 1, "d": 0, "e": 0},
		LatencyMs: 10,
	}
	q2 := QueryResult{
		QueryID: "q2",
		Slices:  []string{"factual"},
		Results: []RankedResult{
			{FixtureID: "x"}, {FixtureID: "y"}, {FixtureID: "z"},
			{FixtureID: "w"}, {FixtureID: "v"},
		},
		Relevance: map[string]int{"x": 0, "y": 3, "z": 0, "w": 0, "v": 0},
		LatencyMs: 30,
	}
	q3 := QueryResult{
		QueryID: "q3",
		Slices:  []string{"short"},
		Results: []RankedResult{
			{FixtureID: "p"},
		},
		Relevance: map[string]int{"p": 2},
		LatencyMs: 5,
	}

	reports := AggregateBySlice([]QueryResult{q1, q2, q3})

	// Expect two slices: "factual" and "short", sorted alphabetically.
	if len(reports) != 2 {
		t.Fatalf("got %d slices, want 2", len(reports))
	}
	if reports[0].Slice != "factual" {
		t.Errorf("first slice = %q, want %q", reports[0].Slice, "factual")
	}
	if reports[1].Slice != "short" {
		t.Errorf("second slice = %q, want %q", reports[1].Slice, "short")
	}

	// "factual" has q1 + q2 = 2 queries.
	if reports[0].Count != 2 {
		t.Errorf("factual count = %d, want 2", reports[0].Count)
	}
	// "short" has q1 + q3 = 2 queries.
	if reports[1].Count != 2 {
		t.Errorf("short count = %d, want 2", reports[1].Count)
	}

	// Verify factual slice precision@5 = avg of q1 (0.6) and q2 (0.2) = 0.4.
	if !approxEqual(reports[0].Metrics.Precision5, 0.4) {
		t.Errorf("factual Precision5 = %.6f, want 0.4", reports[0].Metrics.Precision5)
	}

	// Empty input produces no slices.
	empty := AggregateBySlice(nil)
	if len(empty) != 0 {
		t.Errorf("expected no slices for nil input, got %d", len(empty))
	}
}
