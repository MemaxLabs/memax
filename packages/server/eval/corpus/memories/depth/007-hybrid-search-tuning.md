Notes from tuning the hybrid search pipeline, updated April 8.

## Current Architecture

The retrieval pipeline combines three signal sources using Reciprocal Rank Fusion (RRF):

1. **Vector search** — pgvector cosine similarity on Voyage AI embeddings (voyage-code-3, 1024 dimensions).
2. **Full-text search** — PostgreSQL tsvector with `english` text search configuration, using `ts_rank_cd` for ranking.
3. **Trigram search** — `pg_trgm` similarity for fuzzy matching, catches typos and partial matches.

## RRF k Parameter Tuning

The RRF formula is: `score = sum(1 / (k + rank_i))` for each signal source.

We tested k values from 10 to 120 on our eval corpus (48 queries at the time):

| k value | nDCG@5 | MRR   | Notes                                          |
|---------|--------|-------|-------------------------------------------------|
| 10      | 0.71   | 0.78  | Too aggressive — vector dominates everything    |
| 30      | 0.76   | 0.82  | Decent balance                                  |
| 60      | 0.79   | 0.85  | Best overall — text search gets fair weight      |
| 90      | 0.77   | 0.83  | Text search starts to dominate on keyword queries|
| 120     | 0.74   | 0.80  | Vector signal washed out                         |

**Decision:** Set k=60 as the default. This gives vector search the semantic understanding advantage while letting exact keyword matches from tsvector break ties.

## Trigram vs tsvector Tradeoffs

- **tsvector** excels at stemmed, language-aware matching. "deploying" matches "deployment." But it misses non-English terms, code identifiers, and brand names.
- **pg_trgm** handles partial matches and typos ("depoy" still matches "deploy") but produces more false positives and is slower on large tables.

We run both but weight tsvector 2x higher than trigram in the RRF fusion. Trigram acts as a safety net for queries that tsvector misses entirely.

## Benchmark Results (April 8)

On the eval corpus with k=60 and the current weighting:
- **nDCG@5:** 0.79
- **MRR:** 0.85
- **Recall@10:** 0.91
- **P95 latency:** 42ms (single-hub), 68ms (multi-hub with 3 hubs)

## Next Steps

- Add a Cohere reranker pass after RRF fusion to improve precision on ambiguous queries.
- Experiment with query-dependent k: use lower k for short queries (where vector is more reliable) and higher k for long queries (where keyword matching helps).
- Build a larger eval corpus to stress-test edge cases.
