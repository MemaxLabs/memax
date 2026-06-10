package longmemeval

import (
	"math"
	"strings"
)

// ---------------------------------------------------------------------------
// LongMemEval-compatible retrieval metrics.
//
// These MUST match the Python implementation in eval_utils.py exactly.
// Key differences from our internal eval/metrics.go:
//
//   1. Binary relevance (1/0), not graded (0–3).
//   2. DCG formula: rel[0] + Σ(rel[i] / log2(i+1)) for i≥1.
//      (Position 0 and 1 both get discount=1 in this formulation.)
//   3. Three metrics: recall_any@k, recall_all@k, ndcg_any@k.
//   4. Session-to-turn conversion with expanding k for dedup.
// ---------------------------------------------------------------------------

// RetrievalMetrics holds the three retrieval metrics for a specific k value.
type RetrievalMetrics struct {
	RecallAny float64 `json:"recall_any"`
	RecallAll float64 `json:"recall_all"`
	NDCGAny   float64 `json:"ndcg_any"`
}

// MetricsAtK holds metrics at multiple k values for a single granularity.
type MetricsAtK map[string]float64

// StandardKValues are the k values used in LongMemEval evaluation.
var StandardKValues = []int{1, 3, 5, 10, 30, 50}

// ---------------------------------------------------------------------------
// Core metric functions — exact ports of eval_utils.py
// ---------------------------------------------------------------------------

// dcg computes Discounted Cumulative Gain at k.
// Matches Python: relevances[0] + sum(relevances[1:] / log2(arange(2, size+1)))
func dcg(relevances []float64, k int) float64 {
	n := k
	if n > len(relevances) {
		n = len(relevances)
	}
	if n == 0 {
		return 0
	}

	// Position 0: no discount (divide by 1).
	result := relevances[0]

	// Positions 1..n-1: discount by log2(i+1) where i is 1-indexed from 2.
	// Python: np.log2(np.arange(2, relevances.size + 1))
	// For i=1: log2(2), for i=2: log2(3), etc.
	for i := 1; i < n; i++ {
		result += relevances[i] / math.Log2(float64(i+1))
	}

	return result
}

// ndcg computes Normalized DCG at k with binary relevance.
// rankings: indices into corpus_ids ordered by retrieval score (best first).
// correctDocs: set of corpus IDs that are evidence.
// corpusIDs: all corpus document IDs.
//
// Matches Python ndcg() in eval_utils.py.
func ndcg(rankings []int, correctDocs map[string]bool, corpusIDs []string, k int) float64 {
	// Build full binary relevance vector over corpus.
	relevances := make([]float64, len(corpusIDs))
	for i, id := range corpusIDs {
		if correctDocs[id] {
			relevances[i] = 1
		}
	}

	// Get relevances in ranked order.
	n := k
	if n > len(rankings) {
		n = len(rankings)
	}
	sortedRels := make([]float64, n)
	for i := 0; i < n; i++ {
		sortedRels[i] = relevances[rankings[i]]
	}

	// Ideal: sort all relevances descending.
	idealRels := make([]float64, len(relevances))
	copy(idealRels, relevances)
	sortFloat64Desc(idealRels)

	idealDCG := dcg(idealRels, k)
	if idealDCG == 0 {
		return 0
	}

	actualDCG := dcg(sortedRels, k)
	return actualDCG / idealDCG
}

// EvaluateRetrieval computes recall_any, recall_all, and ndcg_any at k.
// Matches Python evaluate_retrieval() in eval_utils.py.
//
// rankings: indices into corpusIDs ordered by retrieval score (best first).
// correctDocs: set of evidence corpus IDs.
// corpusIDs: all corpus document IDs.
func EvaluateRetrieval(rankings []int, correctDocs map[string]bool, corpusIDs []string, k int) RetrievalMetrics {
	// Collect recalled docs in top-k.
	n := k
	if n > len(rankings) {
		n = len(rankings)
	}
	recalled := make(map[string]bool)
	for i := 0; i < n; i++ {
		recalled[corpusIDs[rankings[i]]] = true
	}

	// recall_any: did ANY correct doc appear in top-k?
	recallAny := 0.0
	for doc := range correctDocs {
		if recalled[doc] {
			recallAny = 1.0
			break
		}
	}

	// recall_all: did ALL correct docs appear in top-k?
	recallAll := 1.0
	for doc := range correctDocs {
		if !recalled[doc] {
			recallAll = 0.0
			break
		}
	}

	// ndcg_any: NDCG with binary relevance.
	ndcgScore := ndcg(rankings, correctDocs, corpusIDs, k)

	return RetrievalMetrics{
		RecallAny: recallAny,
		RecallAll: recallAll,
		NDCGAny:   ndcgScore,
	}
}

// EvaluateRetrievalTurn2Session converts turn-level retrieval results to
// session-level and evaluates. Matches Python evaluate_retrieval_turn2session().
//
// This strips the turn suffix from corpus IDs and adjusts k upward so that
// the effective top-k contains k unique sessions.
func EvaluateRetrievalTurn2Session(rankings []int, correctDocs map[string]bool, corpusIDs []string, k int) RetrievalMetrics {
	// Strip turn IDs to get session IDs.
	sessionCorrect := make(map[string]bool)
	for doc := range correctDocs {
		sessionCorrect[stripTurnID(doc)] = true
	}

	sessionCorpusIDs := make([]string, len(corpusIDs))
	for i, id := range corpusIDs {
		sessionCorpusIDs[i] = stripTurnID(id)
	}

	// Expand effective_k until we have k unique session IDs in top results.
	effectiveK := k
	uniqueSessionIDs := make(map[string]bool)
	for i := 0; i < effectiveK && i < len(rankings); i++ {
		uniqueSessionIDs[sessionCorpusIDs[rankings[i]]] = true
	}
	for effectiveK <= len(corpusIDs) && len(uniqueSessionIDs) < k {
		effectiveK++
		if effectiveK <= len(rankings) {
			uniqueSessionIDs[sessionCorpusIDs[rankings[effectiveK-1]]] = true
		}
	}

	return EvaluateRetrieval(rankings, sessionCorrect, sessionCorpusIDs, effectiveK)
}

// stripTurnID removes the last underscore-separated segment from a corpus ID.
// Matches Python: '_'.join(docid.split('_')[:-1])
func stripTurnID(docID string) string {
	lastUnderscore := strings.LastIndex(docID, "_")
	if lastUnderscore < 0 {
		return docID
	}
	return docID[:lastUnderscore]
}

// ComputeAllMetrics computes metrics at all standard k values for a single
// entry's retrieval results. Returns metrics for both the native granularity
// and (if turn-level) the session-level aggregation.
func ComputeAllMetrics(rankings []int, correctDocs map[string]bool, corpusIDs []string, granularity Granularity) (native MetricsAtK, session MetricsAtK) {
	native = make(MetricsAtK)
	session = make(MetricsAtK)

	for _, k := range StandardKValues {
		m := EvaluateRetrieval(rankings, correctDocs, corpusIDs, k)
		native[metricKey("recall_any", k)] = m.RecallAny
		native[metricKey("recall_all", k)] = m.RecallAll
		native[metricKey("ndcg_any", k)] = m.NDCGAny

		if granularity == TurnGranularity {
			sm := EvaluateRetrievalTurn2Session(rankings, correctDocs, corpusIDs, k)
			session[metricKey("recall_any", k)] = sm.RecallAny
			session[metricKey("recall_all", k)] = sm.RecallAll
			session[metricKey("ndcg_any", k)] = sm.NDCGAny
		}
	}

	return native, session
}

func metricKey(name string, k int) string {
	return name + "@" + itoa(k)
}

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

// AggregateMetricsMap computes the mean of each metric key across multiple
// entries. Abstention questions and questions without target turns are excluded
// from aggregation, matching the Python evaluation.
func AggregateMetricsMap(entries []MetricsAtK) MetricsAtK {
	if len(entries) == 0 {
		return nil
	}
	agg := make(MetricsAtK)
	counts := make(map[string]int)

	for _, e := range entries {
		for k, v := range e {
			agg[k] += v
			counts[k]++
		}
	}

	for k := range agg {
		if counts[k] > 0 {
			agg[k] /= float64(counts[k])
		}
	}

	return agg
}

// ---------------------------------------------------------------------------
// Sort helper
// ---------------------------------------------------------------------------

func sortFloat64Desc(s []float64) {
	// Simple insertion sort — relevance arrays are tiny.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] > s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
