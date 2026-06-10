package store

import (
	"math"
	"testing"
)

// rrfScore computes the weighted RRF contribution for a single lane.
// Mirrors the production formula: weight / (k + rank + 1).
func rrfScore(weight float64, k, rank int) float64 {
	return weight / float64(k+rank+1)
}

func TestWeightedRRFRankingBehavior(t *testing.T) {
	// These tests verify that the per-lane weights produce the intended
	// ranking behavior without touching the database. The constants must
	// match those in postgres_chunks.go.
	const k = rrfK

	t.Run("vector lane outweighs trigram at same rank", func(t *testing.T) {
		vecScore := rrfScore(rrfVectorW, k, 0)
		trgScore := rrfScore(rrfTrigramW, k, 0)
		if vecScore <= trgScore {
			t.Errorf("vector rank 0 (%f) should outscore trigram rank 0 (%f)", vecScore, trgScore)
		}
		ratio := vecScore / trgScore
		if math.Abs(ratio-2.5) > 0.01 {
			t.Errorf("vector/trigram weight ratio = %f, want 2.5", ratio)
		}
	})

	t.Run("vector+field rescue beats triple-lane trigram match", func(t *testing.T) {
		// A memory found by vector (rank 5) + field (rank 0) should beat
		// one found by BM25 (rank 8) + trigram (rank 3) + trigram providing
		// no additional value beyond fuzzy overlap.
		rescueScore := rrfScore(rrfVectorW, k, 5) + rrfScore(rrfFieldW, k, 0)
		fuzzyScore := rrfScore(rrfBM25W, k, 8) + rrfScore(rrfTrigramW, k, 3)

		if rescueScore <= fuzzyScore {
			t.Errorf("vector+field rescue (%f) should outscore BM25+trigram (%f)", rescueScore, fuzzyScore)
		}
	})

	t.Run("field-only rescue is competitive with mid-rank BM25-only", func(t *testing.T) {
		// A field-lane rescue at rank 0 should be competitive with a
		// BM25-only match at mid-rank — the scoring boosts in the handler
		// layer (kind hints, project match) can then push it above.
		fieldOnly := rrfScore(rrfFieldW, k, 0)
		bm25Mid := rrfScore(rrfBM25W, k, 15)

		// Field-only should be higher than mid-rank BM25, so scoring
		// boosts don't need to overcome a huge RRF deficit.
		if fieldOnly <= bm25Mid {
			t.Errorf("field rank 0 (%f) should outscore BM25 rank 15 (%f)", fieldOnly, bm25Mid)
		}
	})

	t.Run("temporal weight equals BM25 when temporal lane fires", func(t *testing.T) {
		// The temporal lane is self-selecting (only fires on temporal
		// queries). When it fires, it should have equal weight to BM25
		// to avoid under/over-valuing temporal matches.
		trlScore := rrfScore(rrfTemporalW, k, 0)
		txtScore := rrfScore(rrfBM25W, k, 0)
		if trlScore != txtScore {
			t.Errorf("temporal rank 0 (%f) should equal BM25 rank 0 (%f)", trlScore, txtScore)
		}
	})

	t.Run("all weights are positive", func(t *testing.T) {
		for name, w := range map[string]float64{
			"vector":   rrfVectorW,
			"bm25":     rrfBM25W,
			"trigram":  rrfTrigramW,
			"temporal": rrfTemporalW,
			"field":    rrfFieldW,
		} {
			if w <= 0 {
				t.Errorf("weight %s = %f, must be positive", name, w)
			}
		}
	})
}
