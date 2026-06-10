package longmemeval

import (
	"math"
	"testing"
)

const epsilon = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

// TestDCG verifies our DCG matches the Python implementation.
// Python reference:
//
//	def dcg(relevances, k):
//	    relevances = np.asfarray(relevances)[:k]
//	    return relevances[0] + np.sum(relevances[1:] / np.log2(np.arange(2, relevances.size + 1)))
func TestDCG(t *testing.T) {
	tests := []struct {
		name string
		rels []float64
		k    int
		want float64
	}{
		{
			name: "single relevant",
			rels: []float64{1, 0, 0, 0, 0},
			k:    5,
			want: 1.0, // just rel[0] = 1
		},
		{
			name: "two relevant",
			rels: []float64{1, 1, 0, 0, 0},
			k:    5,
			want: 1.0 + 1.0/math.Log2(2), // 1 + 1/1 = 2
		},
		{
			name: "all relevant k=3",
			rels: []float64{1, 1, 1, 0, 0},
			k:    3,
			// 1 + 1/log2(2) + 1/log2(3) = 1 + 1 + 0.6309... = 2.6309...
			want: 1 + 1/math.Log2(2) + 1/math.Log2(3),
		},
		{
			name: "empty",
			rels: []float64{},
			k:    5,
			want: 0,
		},
		{
			name: "k larger than rels",
			rels: []float64{1, 0},
			k:    10,
			want: 1.0,
		},
		{
			name: "relevant at position 3 only",
			rels: []float64{0, 0, 1, 0, 0},
			k:    5,
			want: 0 + 0/math.Log2(2) + 1/math.Log2(3),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dcg(tt.rels, tt.k)
			if !approxEqual(got, tt.want) {
				t.Errorf("dcg(%v, %d) = %f, want %f", tt.rels, tt.k, got, tt.want)
			}
		})
	}
}

// TestNDCG verifies NDCG computation matches Python.
func TestNDCG(t *testing.T) {
	// Corpus: 5 documents, docs at index 0 and 3 are correct.
	corpusIDs := []string{"doc_a", "answer_b", "doc_c", "answer_d", "doc_e"}
	correct := map[string]bool{"answer_b": true, "answer_d": true}

	tests := []struct {
		name     string
		rankings []int // indices into corpusIDs
		k        int
		want     float64
	}{
		{
			name:     "perfect retrieval k=2",
			rankings: []int{1, 3, 0, 2, 4}, // both correct docs first
			k:        2,
			want:     1.0,
		},
		{
			name:     "worst k=2",
			rankings: []int{0, 2, 4, 1, 3}, // no correct in top 2
			k:        2,
			want:     0.0,
		},
		{
			name:     "one correct at pos 0",
			rankings: []int{1, 0, 2, 4, 3},
			k:        5,
			// Actual: rel at positions = [1, 0, 0, 0, 1]
			// DCG = 1 + 0 + 0 + 0 + 1/log2(5) = 1 + 1/2.3219... = 1.4306...
			// Ideal: [1, 1, 0, 0, 0]
			// IDCG = 1 + 1/log2(2) = 2
			want: (1 + 1/math.Log2(5)) / (1 + 1/math.Log2(2)),
		},
		{
			name:     "no correct docs",
			rankings: []int{0, 2, 4, 1, 3},
			k:        2,
			want:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ndcg(tt.rankings, correct, corpusIDs, tt.k)
			if !approxEqual(got, tt.want) {
				t.Errorf("ndcg(%v, k=%d) = %f, want %f", tt.rankings, tt.k, got, tt.want)
			}
		})
	}
}

// TestEvaluateRetrieval verifies the combined metrics match Python.
func TestEvaluateRetrieval(t *testing.T) {
	corpusIDs := []string{"doc_1", "answer_a", "doc_2", "answer_b", "doc_3"}
	correct := map[string]bool{"answer_a": true, "answer_b": true}

	tests := []struct {
		name      string
		rankings  []int
		k         int
		wantAny   float64
		wantAll   float64
		wantNDCG  float64
	}{
		{
			name:     "both correct in top 2",
			rankings: []int{1, 3, 0, 2, 4},
			k:        2,
			wantAny:  1.0,
			wantAll:  1.0,
			wantNDCG: 1.0,
		},
		{
			name:     "one correct in top 2",
			rankings: []int{1, 0, 3, 2, 4},
			k:        2,
			wantAny:  1.0,
			wantAll:  0.0, // answer_b not in top 2
			wantNDCG: (1 + 0/math.Log2(2)) / (1 + 1/math.Log2(2)), // actual=1, ideal=2
		},
		{
			name:     "none in top 2",
			rankings: []int{0, 2, 4, 1, 3},
			k:        2,
			wantAny:  0.0,
			wantAll:  0.0,
			wantNDCG: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := EvaluateRetrieval(tt.rankings, correct, corpusIDs, tt.k)
			if !approxEqual(m.RecallAny, tt.wantAny) {
				t.Errorf("recall_any = %f, want %f", m.RecallAny, tt.wantAny)
			}
			if !approxEqual(m.RecallAll, tt.wantAll) {
				t.Errorf("recall_all = %f, want %f", m.RecallAll, tt.wantAll)
			}
			if !approxEqual(m.NDCGAny, tt.wantNDCG) {
				t.Errorf("ndcg_any = %f, want %f", m.NDCGAny, tt.wantNDCG)
			}
		})
	}
}

// TestStripTurnID verifies turn-to-session ID stripping.
func TestStripTurnID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"answer_abc123_1", "answer_abc123"},
		{"answer_abc123_2_3", "answer_abc123_2"},
		{"doc_xyz_5", "doc_xyz"},
		{"nodash", "nodash"},
	}

	for _, tt := range tests {
		got := stripTurnID(tt.input)
		if got != tt.want {
			t.Errorf("stripTurnID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestEvaluateRetrievalTurn2Session verifies the turn→session conversion.
func TestEvaluateRetrievalTurn2Session(t *testing.T) {
	// 2 sessions, each with 2 user turns.
	// Session "answer_a" has turns at indices 0, 1 — turn 0 has the answer.
	// Session "doc_b" has turns at indices 2, 3 — no answer.
	corpusIDs := []string{
		"answer_a_1", // evidence turn
		"noans_a_3",  // non-evidence turn from answer session
		"doc_b_1",    // filler
		"doc_b_3",    // filler
	}
	correct := map[string]bool{"answer_a_1": true}

	// Ranking: answer turn first.
	rankings := []int{0, 1, 2, 3}

	m := EvaluateRetrievalTurn2Session(rankings, correct, corpusIDs, 1)

	// After stripping: correct = {"answer_a"}, corpus = ["answer_a", "noans_a", "doc_b", "doc_b"]
	// effective_k=1: unique sessions = {"answer_a"} — we have 1 unique, want 1. OK.
	// top-1 = "answer_a" which is correct.
	if !approxEqual(m.RecallAny, 1.0) {
		t.Errorf("turn2session recall_any = %f, want 1.0", m.RecallAny)
	}
	if !approxEqual(m.RecallAll, 1.0) {
		t.Errorf("turn2session recall_all = %f, want 1.0", m.RecallAll)
	}
}

// TestSortFloat64Desc verifies the sort helper.
func TestSortFloat64Desc(t *testing.T) {
	s := []float64{0, 1, 0, 1, 0}
	sortFloat64Desc(s)
	want := []float64{1, 1, 0, 0, 0}
	for i := range s {
		if s[i] != want[i] {
			t.Errorf("sorted[%d] = %f, want %f", i, s[i], want[i])
		}
	}
}
