## Code: Reciprocal Rank Fusion (RRF) Implementation

Go implementation of Reciprocal Rank Fusion used in the memax retrieval pipeline to merge results from multiple ranking signals: vector similarity (Voyage AI embeddings), BM25 keyword match, and tag/metadata boost.

### Algorithm

RRF combines multiple ranked lists into a single ranking by summing reciprocal ranks. For each document `d` appearing in ranking list `r`, its RRF score is:

```
RRF(d) = Σ 1 / (k + rank_r(d))
```

where `k` is a constant (we use k=60, following the original Cormack et al. 2009 paper). Higher `k` values reduce the influence of high-ranking items, making the fusion more "democratic" across lists.

### Implementation

```go
package retrieval

// RRFResult holds a document ID and its fused score.
type RRFResult struct {
    ChunkID string
    Score   float64
}

// FuseRRF merges multiple ranked result lists into a single ranking
// using Reciprocal Rank Fusion with k=60.
//
// Each input list is a slice of chunk IDs ordered by decreasing relevance
// (index 0 = most relevant). The output is sorted by decreasing RRF score.
func FuseRRF(rankedLists [][]string, k int) []RRFResult {
    if k <= 0 {
        k = 60 // default from Cormack et al. 2009
    }

    scores := make(map[string]float64)

    for _, list := range rankedLists {
        for rank, chunkID := range list {
            // rank is 0-indexed, so rank+1 gives 1-indexed rank
            scores[chunkID] += 1.0 / float64(k+rank+1)
        }
    }

    // Collect and sort by score descending
    results := make([]RRFResult, 0, len(scores))
    for id, score := range scores {
        results = append(results, RRFResult{ChunkID: id, Score: score})
    }

    sort.Slice(results, func(i, j int) bool {
        if results[i].Score == results[j].Score {
            // Tie-break: prefer the ID that appeared in more lists
            return results[i].ChunkID < results[j].ChunkID
        }
        return results[i].Score > results[j].Score
    })

    return results
}
```

### Usage in the Retrieval Pipeline

```go
func (e *Engine) Recall(ctx context.Context, query string, opts RecallOpts) ([]Result, error) {
    // 1. Vector search — top 50 by cosine similarity
    vectorResults, err := e.vectorSearch(ctx, query, 50)
    if err != nil {
        return nil, fmt.Errorf("vector search: %w", err)
    }

    // 2. BM25 keyword search — top 50 by term frequency
    bm25Results, err := e.bm25Search(ctx, query, 50)
    if err != nil {
        return nil, fmt.Errorf("bm25 search: %w", err)
    }

    // 3. Tag boost — top 20 by tag match score
    tagResults, err := e.tagSearch(ctx, query, opts.Tags, 20)
    if err != nil {
        return nil, fmt.Errorf("tag search: %w", err)
    }

    // 4. Fuse with RRF (k=60)
    fused := FuseRRF([][]string{
        extractIDs(vectorResults),
        extractIDs(bm25Results),
        extractIDs(tagResults),
    }, 60)

    // 5. Take top-K and hydrate full results
    topK := fused
    if len(topK) > opts.Limit {
        topK = topK[:opts.Limit]
    }

    return e.hydrateResults(ctx, topK)
}
```

### Why k=60

The `k` parameter controls how much weight high-ranked items get relative to lower-ranked ones:

- **k=1**: Top-ranked item gets score 0.5, second gets 0.33 — steep dropoff, top items dominate
- **k=60**: Top-ranked item gets score 0.0164, second gets 0.0161 — very flat, position matters less
- **k=60** is the standard choice because it makes RRF robust to individual rankers having noisy top results. A document that appears at rank 5 in all three lists will outscore one that's rank 1 in one list but absent from others.

### Performance

- RRF fusion itself is O(n) where n = total items across all lists. For our typical workload (3 lists × 50 items = 150), it completes in <0.1ms.
- The expensive parts are the upstream searches (vector: ~30ms, BM25: ~15ms, tags: ~5ms). RRF is negligible.
- We run vector and BM25 searches concurrently with `errgroup`, so total recall latency is ~max(30, 15, 5) + rerank ≈ 50-80ms.

### Gotchas

1. **0-indexed vs 1-indexed ranks** — The formula uses 1-indexed ranks. Our Go slices are 0-indexed, so we add 1: `k + rank + 1`.
2. **Tie-breaking** — When two documents have the same RRF score, we break ties by chunk ID for deterministic ordering. In production, we could tie-break by recency instead.
3. **Empty lists** — If one ranker returns no results (e.g., no tag matches), RRF still works — those documents just don't get a score contribution from that list.
