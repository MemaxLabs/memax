# V2 retrieval eval: live results

Run on Oct 6, 2026 with the staging Voyage key, from a dev machine (not Fly's `sjc`):

```bash
cd packages/server && V2_EVAL_LIVE=1 go test ./eval/v2/ -run TestV2Retrieval -v
```

The corpus (`corpus.json`, version 2) was too small to calibrate anything as Phase 1 left it: 39 statements, 24 queries and three negative queries. It now holds 118 statements (101 in the project space, 17 in a second tenant's) and 66 graded queries:

| Type       | Queries |
| ---------- | ------- |
| lexical    | 16      |
| paraphrase | 24      |
| typo       | 6       |
| negative   | 14      |
| isolation  | 6       |

The new statements are written in the same style, and the version-1 queries were regraded where a new statement bears on them. The labels are mine: a second person's grading would make the numbers firmer.

The quality modes share one embedding per query, with a 2 s deadline, and rerank without the 150 ms deadline. Their numbers therefore don't depend on this machine's distance from Voyage. The last row runs the production deadlines from here.

## Results, at the shipped defaults

The recall floor is 0.35 (see below). Search limit is 10, so Recall@20 is in effect recall at 10.

| Mode                                                | nDCG@5    | nDCG@10 | MRR@10    | Recall@20 | Harmful@10 |
| --------------------------------------------------- | --------- | ------- | --------- | --------- | ---------- |
| lexical                                             | 0.571     | 0.592   | 0.546     | 0.615     | 0          |
| hybrid, fake embedder                               | 0.616     | 0.637   | 0.601     | 0.641     | 0          |
| hybrid, voyage-code-3 (V1's model)                  | 0.773     | 0.816   | 0.708     | 0.968     | 0          |
| hybrid, voyage-4 queries → voyage-4                 | 0.857     | 0.860   | 0.782     | 0.848     | 0          |
| **hybrid, voyage-4-lite → voyage-4 (the default)**  | **0.843** | 0.855   | **0.798** | 0.848     | 0          |
| **+ rerank-3-lite (the default)**                   | **0.890** | 0.891   | **0.832** | 0.848     | 0          |
| + rerank-3-lite, production deadlines from here     | 0.890     | 0.891   | 0.832     | 0.848     | 0          |

nDCG@5 by query type, for the default pair, with rerank-3-lite in brackets:

| Type       | nDCG@5      |
| ---------- | ----------- |
| lexical    | 0.97 (0.98) |
| paraphrase | 0.82 (0.88) |
| typo       | 0.94 (0.94) |
| isolation  | 0.50 (0.67) |

Lexical alone scores 0.35 on paraphrase.

Every query with a grade-2+ answer finds one in the top 10 in every Voyage mode. Lexical misses 10 queries, all of them paraphrases.

**The models ship.** The §5.11 gate is that retrieval doesn't regress, and the harness now asserts it on every live run:

- **The default pair against V1's model:** voyage-4 for statements and voyage-4-lite for queries score above voyage-code-3 on the same corpus (nDCG@5 +0.07, MRR@10 +0.09).
  - voyage-code-3's higher Recall@20 comes from a floor it doesn't fit: at 0.35 it still returns a full page of 10 for 8 of the 14 queries nothing answers.
  - On V1's own corpus, voyage-4 also beats voyage-code-3 (`eval/RESULTS.md`).
- **The query model:** voyage-4-lite queries score as well as voyage-4 queries (nDCG@5 0.843 against 0.857, MRR 0.798 against 0.782) and are the faster call.
  - In one run, voyage-4 queries missed the 120 ms deadline 27 times in 66 from here, against once for voyage-4-lite. In two later runs the two were level (1–2 misses each).
- **The reranker:** rerank-3-lite adds +0.05 nDCG@5 and +0.03 MRR@10, most of it on paraphrase and isolation queries.
  - It runs only past 8 candidates: on 12 of the 66 queries at the 0.35 floor, and on 21 at 0.30.
  - There's no Cohere key here, so it couldn't be compared with V1's reranker.

## The recall floor: 0.30 → 0.35

`V2_RECALL_VECTOR_FLOOR`, `DefaultVectorFloor` in `internal/v2recall/vector.go`.

| Floor    | nDCG@5 / MRR@10 / Recall@20 | with rerank-3-lite     | Negative queries the vector lane added results to |
| -------- | --------------------------- | ---------------------- | ------------------------------------------------- |
| 0.20     | 0.811 / 0.733 / 0.952       | 0.899 / 0.827 / 0.933  | 14 of 14 (113 results)                            |
| 0.25     | 0.821 / 0.749 / 0.936       | 0.900 / 0.812 / 0.942  | 12 of 14 (59)                                     |
| 0.30     | 0.827 / 0.752 / 0.897       | 0.886 / 0.803 / 0.897  | 9 of 14 (22)                                      |
| 0.33     | 0.832 / 0.782 / 0.859       | 0.889 / 0.829 / 0.859  | 5 of 14 (12)                                      |
| **0.35** | **0.843 / 0.798 / 0.848**   | **0.890 / 0.832 / 0.848** | **2 of 14 (3)**                                |
| 0.37     | 0.813 / 0.776 / 0.772       | 0.830 / 0.795 / 0.779  | 0                                                 |
| 0.40     | 0.783 / 0.756 / 0.747       | 0.801 / 0.776 / 0.753  | 0                                                 |
| 0.50     | 0.708 / 0.708 / 0.654       | 0.715 / 0.708 / 0.660  | 0                                                 |

Query → statement cosine, voyage-4-lite to voyage-4:

| Pairs                                    | n     | min  | p10  | median | p90  | max  |
| ---------------------------------------- | ----- | ---- | ---- | ------ | ---- | ---- |
| ideal match (grade 3)                    | 47    | 0.36 | 0.42 | 0.54   | 0.66 | 0.70 |
| grade 2                                  | 10    | 0.28 | 0.28 | 0.41   | 0.48 | 0.48 |
| grade 1                                  | 56    | 0.12 | 0.22 | 0.37   | 0.51 | 0.54 |
| ungraded                                 | 5,139 | -0.04 | 0.07 | 0.14  | 0.24 | 0.50 |
| a negative query's best match            | 14    | 0.24 | 0.24 | 0.32   | 0.36 | 0.42 |

At 0.30, more than half of the queries nothing answers got statements back from the vector lane, which is the failure the floor exists to stop (precision over recall). At 0.35:

- 2 of 14 such queries get anything (3 results);
- every ideal match stays (the lowest is 0.36);
- nDCG@5 and MRR@10 both rise, with and without the reranker.

The price is Recall@20, 0.897 → 0.848: grade-1 and grade-2 statements at 0.28–0.35 drop out unless a lexical lane finds them.

**It sits near a cliff.** At 0.37, ideal matches start to fall out (nDCG@5 0.813). Don't raise it on this evidence. Recheck it on real staging queries, whose phrasing this corpus can only approximate. The floor is model-specific: voyage-code-3 puts most nonsense queries above 0.35.

## Latency from here

The API runs in `sjc`, and Voyage's region decides the real numbers.

| Call                                 | p50       | p95        | Over its deadline              |
| ------------------------------------ | --------- | ---------- | ------------------------------ |
| voyage-4-lite query embedding        | 90–100 ms | 100–110 ms | 120 ms: 1 of 66, in each of three runs |
| voyage-4 query embedding             | 90–100 ms | 110–340 ms | 120 ms: 2–27 of 66             |
| rerank-3-lite                        | 110 ms    | 130–150 ms | 150 ms: 3–5% of calls          |

With the real deadlines from here, 0–2 of 66 queries answered lexically and 1–2 reranks timed out, which cost at most 0.02 nDCG@5. The query deadline (120 ms) and rerank budget (150 ms) are tight from this machine and should be measured from `sjc` before N2's nightly run.

## Not changed

- `V2_EMBED_MODEL` voyage-4, `V2_EMBED_QUERY_MODEL` voyage-4-lite and `V2_RERANK_MODEL` rerank-3-lite: confirmed, as above.
- RRF weights (V1's 1.5 / 1.0 / 0.6), the 120 ms query deadline and the 150 ms rerank budget: no evidence here to move them.

Voyage cost for these runs was negligible: a few thousand short texts, and about 400 rerank calls of at most 20 documents each.
