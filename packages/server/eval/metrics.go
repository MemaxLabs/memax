// Package eval provides retrieval quality evaluation for Memax.
//
// This file implements standard information retrieval metrics over graded
// relevance judgments. Relevance grades:
//
//	-1  harmful (actively wrong / dangerous)
//	 0  irrelevant
//	 1  related (tangentially useful)
//	 2  highly relevant
//	 3  ideal (perfect answer)
package eval

import (
	"math"
	"sort"
)

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// RankedResult represents a single item in a ranked result list.
type RankedResult struct {
	FixtureID string  `json:"fixture_id"`
	Rank      int     `json:"rank"`
	Score     float64 `json:"score"`
}

// QueryResult captures a single evaluation query together with the retrieval
// system's ranked output and the ground-truth relevance judgments.
type QueryResult struct {
	QueryID   string         `json:"query_id"`
	Query     string         `json:"query"`
	QueryType string         `json:"query_type"`
	Slices    []string       `json:"slices"`
	Results   []RankedResult `json:"results"`
	Relevance map[string]int `json:"relevance"` // fixture_id -> grade
	Negative  bool           `json:"negative"`  // true = negative query (no ideal ranking)
	LatencyMs int64          `json:"latency_ms"`
}

// MetricSet holds all computed metrics for a single query or an aggregate.
type MetricSet struct {
	NDCG5            float64 `json:"ndcg_5"`
	NDCG10           float64 `json:"ndcg_10"`
	Precision5       float64 `json:"precision_5"`
	StrongPrecision3 float64 `json:"strong_precision_3"`
	Recall20         float64 `json:"recall_20"`
	MRR10            float64 `json:"mrr_10"`
	ERR10            float64 `json:"err_10"`
	Harmful10        int     `json:"harmful_10"`
	ResultCount      int     `json:"result_count"`
	LatencyMs        int64   `json:"latency_ms"`
}

// ResultDetail describes a single result with its relevance judgment.
type ResultDetail struct {
	Rank      int     `json:"rank"`
	FixtureID string  `json:"fixture_id"`
	Title     string  `json:"title"`
	Score     float64 `json:"score"`
	Relevance int     `json:"relevance"`
}

// QueryReport is the per-query section of an evaluation report.
type QueryReport struct {
	QueryID    string         `json:"query_id"`
	Query      string         `json:"query"`
	QueryType  string         `json:"query_type"`
	Metrics    MetricSet      `json:"metrics"`
	TopResults []ResultDetail `json:"top_results"`
	Pass       bool           `json:"pass"`
	FailReason string         `json:"fail_reason,omitempty"`
}

// SliceReport aggregates metrics for a single evaluation slice.
type SliceReport struct {
	Slice   string    `json:"slice"`
	Count   int       `json:"count"`
	Metrics MetricSet `json:"metrics"`
}

// EvalReport is the top-level evaluation report.
type EvalReport struct {
	Timestamp   string        `json:"timestamp"`
	CorpusHash  string        `json:"corpus_hash"`
	MemoryCount int           `json:"memory_count"`
	QueryCount  int           `json:"query_count"`
	Aggregate   MetricSet     `json:"aggregate"`
	Slices      []SliceReport `json:"slices"`
	Queries     []QueryReport `json:"queries"`
}

// ---------------------------------------------------------------------------
// Metric functions
// ---------------------------------------------------------------------------

// NDCG computes Normalized Discounted Cumulative Gain at position k.
//
// Gain function: gain(rel) = 2^rel - 1 (relevance -1 is treated as 0).
// Discount function: discount(rank) = 1 / log2(rank + 1).
// Returns 0.0 when no relevant documents exist (IDCG = 0).
func NDCG(results []RankedResult, relevance map[string]int, k int) float64 {
	if k <= 0 || len(results) == 0 {
		return 0.0
	}

	// DCG over the top-k results.
	dcg := 0.0
	n := min(k, len(results))
	for i := 0; i < n; i++ {
		rel := clampRel(relevance[results[i].FixtureID])
		gain := math.Pow(2, float64(rel)) - 1
		discount := math.Log2(float64(i+1) + 1) // rank is 1-indexed
		dcg += gain / discount
	}

	// IDCG: sort all relevance values descending, take top-k.
	idcg := idealDCG(relevance, k)
	if idcg == 0 {
		return 0.0
	}
	return dcg / idcg
}

// PrecisionAtK computes the fraction of top-k results whose relevance is
// >= minRel. Documents not in the relevance map are treated as 0.
func PrecisionAtK(results []RankedResult, relevance map[string]int, k int, minRel int) float64 {
	if k <= 0 {
		return 0.0
	}
	n := min(k, len(results))
	if n == 0 {
		return 0.0
	}
	count := 0
	for i := 0; i < n; i++ {
		if relevance[results[i].FixtureID] >= minRel {
			count++
		}
	}
	return float64(count) / float64(k)
}

// RecallAtK computes the fraction of all documents with relevance >= minRel
// that appear in the top-k results.
//
// Returns 1.0 when no relevant documents exist (vacuously true).
func RecallAtK(results []RankedResult, relevance map[string]int, k int, minRel int) float64 {
	if k <= 0 {
		return 0.0
	}

	// Count total relevant documents.
	totalRelevant := 0
	for _, rel := range relevance {
		if rel >= minRel {
			totalRelevant++
		}
	}
	if totalRelevant == 0 {
		return 1.0 // vacuously true
	}

	// Count relevant in top-k.
	n := min(k, len(results))
	found := 0
	for i := 0; i < n; i++ {
		if relevance[results[i].FixtureID] >= minRel {
			found++
		}
	}
	return float64(found) / float64(totalRelevant)
}

// MRRAtK computes Mean Reciprocal Rank at k: 1/rank of the first result
// with relevance >= minRel. Returns 0.0 if no relevant result is found
// within the top-k.
func MRRAtK(results []RankedResult, relevance map[string]int, k int, minRel int) float64 {
	if k <= 0 {
		return 0.0
	}
	n := min(k, len(results))
	for i := 0; i < n; i++ {
		if relevance[results[i].FixtureID] >= minRel {
			return 1.0 / float64(i+1)
		}
	}
	return 0.0
}

// ERRAtK computes Expected Reciprocal Rank at k.
//
// The cascade model assumes a user scans results top-to-bottom and stops
// with probability R_i at rank i, where R_i = (2^rel - 1) / 2^maxRel.
// Relevance -1 is treated as 0 for R computation.
func ERRAtK(results []RankedResult, relevance map[string]int, k int, maxRel int) float64 {
	if k <= 0 || maxRel <= 0 {
		return 0.0
	}
	n := min(k, len(results))
	denom := math.Pow(2, float64(maxRel))

	errVal := 0.0
	trust := 1.0 // probability user is still looking
	for i := 0; i < n; i++ {
		rel := clampRel(relevance[results[i].FixtureID])
		ri := (math.Pow(2, float64(rel)) - 1) / denom
		errVal += trust * ri / float64(i+1)
		trust *= (1 - ri)
	}
	return errVal
}

// HarmfulAtK counts results with relevance == -1 in the top-k.
func HarmfulAtK(results []RankedResult, relevance map[string]int, k int) int {
	if k <= 0 {
		return 0
	}
	n := min(k, len(results))
	count := 0
	for i := 0; i < n; i++ {
		if relevance[results[i].FixtureID] == -1 {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// Composite helpers
// ---------------------------------------------------------------------------

// ComputeMetrics computes the full MetricSet for a single query result using
// standard k values.
func ComputeMetrics(qr QueryResult) MetricSet {
	r := qr.Results
	rel := qr.Relevance
	return MetricSet{
		NDCG5:            NDCG(r, rel, 5),
		NDCG10:           NDCG(r, rel, 10),
		Precision5:       PrecisionAtK(r, rel, 5, 1),
		StrongPrecision3: PrecisionAtK(r, rel, 3, 2),
		Recall20:         RecallAtK(r, rel, 20, 1),
		MRR10:            MRRAtK(r, rel, 10, 2),
		ERR10:            ERRAtK(r, rel, 10, 3),
		Harmful10:        HarmfulAtK(r, rel, 10),
		ResultCount:      len(r),
		LatencyMs:        qr.LatencyMs,
	}
}

// AggregateMetrics computes the mean MetricSet across all queries.
// Negative queries (those with no ideal ranking) are excluded from ranking
// metric averages but still contribute to Harmful counts and latency.
func AggregateMetrics(results []QueryResult) MetricSet {
	if len(results) == 0 {
		return MetricSet{}
	}

	var agg MetricSet
	rankingCount := 0
	totalCount := len(results)

	for _, qr := range results {
		ms := ComputeMetrics(qr)
		agg.Harmful10 += ms.Harmful10
		agg.ResultCount += ms.ResultCount
		agg.LatencyMs += ms.LatencyMs

		if !qr.Negative {
			rankingCount++
			agg.NDCG5 += ms.NDCG5
			agg.NDCG10 += ms.NDCG10
			agg.Precision5 += ms.Precision5
			agg.StrongPrecision3 += ms.StrongPrecision3
			agg.Recall20 += ms.Recall20
			agg.MRR10 += ms.MRR10
			agg.ERR10 += ms.ERR10
		}
	}

	if rankingCount > 0 {
		agg.NDCG5 /= float64(rankingCount)
		agg.NDCG10 /= float64(rankingCount)
		agg.Precision5 /= float64(rankingCount)
		agg.StrongPrecision3 /= float64(rankingCount)
		agg.Recall20 /= float64(rankingCount)
		agg.MRR10 /= float64(rankingCount)
		agg.ERR10 /= float64(rankingCount)
	}

	if totalCount > 0 {
		agg.ResultCount /= totalCount
		agg.LatencyMs /= int64(totalCount)
	}

	return agg
}

// AggregateBySlice groups queries by their slice labels and computes
// aggregate metrics per slice. The returned slice is sorted by slice name.
func AggregateBySlice(results []QueryResult) []SliceReport {
	groups := make(map[string][]QueryResult)
	for _, qr := range results {
		for _, s := range qr.Slices {
			groups[s] = append(groups[s], qr)
		}
	}

	reports := make([]SliceReport, 0, len(groups))
	for name, queries := range groups {
		reports = append(reports, SliceReport{
			Slice:   name,
			Count:   len(queries),
			Metrics: AggregateMetrics(queries),
		})
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Slice < reports[j].Slice
	})
	return reports
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// clampRel treats -1 as 0 for gain calculations, leaving all other values
// unchanged.
func clampRel(rel int) int {
	if rel < 0 {
		return 0
	}
	return rel
}

// idealDCG computes the ideal DCG at k from the full set of relevance
// judgments. It sorts all grades descending and sums the top-k gains.
func idealDCG(relevance map[string]int, k int) float64 {
	rels := make([]int, 0, len(relevance))
	for _, r := range relevance {
		rels = append(rels, clampRel(r))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(rels)))

	idcg := 0.0
	n := min(k, len(rels))
	for i := 0; i < n; i++ {
		gain := math.Pow(2, float64(rels[i])) - 1
		discount := math.Log2(float64(i+1) + 1)
		idcg += gain / discount
	}
	return idcg
}
