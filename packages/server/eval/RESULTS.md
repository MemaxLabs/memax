# V1 retrieval eval: the recall-pool fix and voyage-4

Run on Oct 6, 2026 with the staging Voyage key, on a fresh database (Postgres 17, pgvector 0.8.0).

This was the check plan 25 asks for before the `v2-recall-pool` fix (commit `e9ab939`) reaches main. That commit is the vector lane's `hnsw.ef_search ≥ LIMIT` and strict-order iterative scans, in `internal/store/postgres_vector_lane.go`.

## How it was run

`TestRetrievalQuality` (180 memories, 82 graded queries), with V1's defaults except two stages:

- **Embeddings:** `voyage-code-3`, V1's default.
- **No reranker.** There's no Cohere key here, and the harness passes no reranker anyway (the eval skill's local baseline).
- **No query distiller.** `ANTHROPIC_API_KEY` was unset. V1's client doesn't ask for zero-retention routing, so its distiller could send the eval's queries to non-ZDR hosts, DeepSeek's own API among them (D14). Its sampling would also have made the A/B noisy. Without it the run is deterministic, and every difference below comes from the fix.

Two test binaries were built from the same tree:

- one with the fix;
- one with only `e9ab939`'s change to `postgres_chunks.go` reversed, so the vector lane queries the pool directly as before.

Both ran against the same database in four shapes:

| Shape            | What's in the database                                                                                                  |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------- |
| natural          | the eval corpus as seeded (464 chunks, 5 eval users); the planner's own choice                                         |
| HNSW forced      | the same, with `enable_seqscan` and `enable_bitmapscan` off on the database, as the fix's integration test does. This is how the planner behaves on a large single-owner database |
| multi-tenant     | plus 20 other tenants, each with a near-copy of the corpus (9,744 chunks; `multitenant_pad.sql`); the planner's choice |
| multi-tenant, HNSW forced | both                                                                                                           |

## Results

| Shape                     | Build | nDCG@5 | nDCG@10 | MRR@10 | Recall@20 | P@5   | Queries that differ |
| ------------------------- | ----- | ------ | ------- | ------ | --------- | ----- | ------------------- |
| natural                   | fix   | 0.683  | 0.729   | 0.701  | 0.789     | 0.258 |                     |
| natural                   | no fix | 0.683 | 0.729   | 0.701  | 0.789     | 0.258 | 0 of 82             |
| HNSW forced               | fix   | 0.683  | 0.729   | 0.701  | 0.789     | 0.258 |                     |
| HNSW forced               | no fix | 0.686 | 0.726   | 0.696  | 0.772     | 0.256 | 50 of 82            |
| multi-tenant              | fix   | 0.683  | 0.729   | 0.701  | 0.789     | 0.258 |                     |
| multi-tenant              | no fix | 0.683 | 0.729   | 0.701  | 0.789     | 0.258 | 0 of 82             |
| multi-tenant, HNSW forced | fix   | 0.683  | 0.729   | 0.701  | 0.789     | 0.258 |                     |
| multi-tenant, HNSW forced | no fix | 0.682 | 0.724   | 0.698  | 0.783     | 0.258 | 6 of 82             |

Harmful@10 is 0 in every row.

**The fix doesn't regress, and it helps where it can matter.**

- **With the planner's own choice** (exact owner-index or sequential plans on a corpus this size, single-owner or multi-tenant), the fix changes nothing. The 20 other tenants change nothing either: isolation is exact.
- **With the HNSW path**, the fix returns exactly the exact-plan results in both shapes.
- **Without the fix on the HNSW path**, ranking drifts on 50 of the 82 queries, and Recall@20 drops 0.017 (single owner) or 0.006 (multi-tenant).
  - On the single-owner database, MRR@10 drops 0.005 and nDCG@10 0.003.
  - nDCG@5 rises by 0.003 by chance: one query (`ml-q2`) ranks better on the truncated pool.

The single-owner gain is small on this corpus, because 464 chunks leave little beyond the 40-row cap to lose. The integration test (`TestVectorLaneIsNotCutShortByHNSW`) shows the cap itself. The fix is safe to merge to main.

The absolute numbers sit just under V1's nDCG@5 floor (0.70 in `thresholds.go`), and this is not the fix's doing. They are identical with and without it. The floor was calibrated with the query distiller, which this run leaves out.

## voyage-4 on V1's corpus

The same harness and database, with the fix, natural plan, and `VOYAGE_MODEL=voyage-4` (V1 uses one model for documents and queries):

| Model         | nDCG@5 | nDCG@10 | MRR@10 | Recall@20 | P@5   | Strong P@3 |
| ------------- | ------ | ------- | ------ | --------- | ----- | ---------- |
| voyage-code-3 | 0.683  | 0.729   | 0.701  | 0.789     | 0.258 | 0.300      |
| voyage-4      | 0.720  | 0.751   | 0.740  | 0.779     | 0.266 | 0.312      |

voyage-4 clears every V1 floor even without the distiller. That backs plan 25's model move (§5.11, §11: "voyage-4 … must not regress"). The V2 eval's own comparison is in `v2/RESULTS.md`.
