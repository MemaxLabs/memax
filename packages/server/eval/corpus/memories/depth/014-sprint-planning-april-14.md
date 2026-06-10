Sprint planning notes for the April 14–25 sprint.

## Capacity

- **Ziyang:** 8 days (out April 18-25 for Tokyo trip, available first 4 days only)
- **Jiahao:** 10 days (full sprint)
- **Mira:** 7 days (out April 21-22 for personal days)
- **Sarah:** 10 days (full sprint)

Total capacity: 35 person-days (~70 story points at 2 pts/day).

## Sprint Goals

1. Ship the memory detail page redesign (Sarah, P0)
2. Complete embedding model migration Phase 1 — dual-write (Jiahao, P0)
3. Fix staging CORS restriction from auth audit (Ziyang, P0)
4. Add automated hub boundary enforcement tests (Sarah, P1)
5. Implement Cohere reranker integration for retrieval pipeline (Jiahao, P1)
6. Expand eval corpus with depth shard for ranking quality testing (Ziyang, P1)

## Story Assignments

### Ziyang (available April 14-17)
- **[P0]** Fix staging CORS — 2 pts — restrict `Access-Control-Allow-Origin` to `*.memaxlabs.com`
- **[P1]** Eval corpus depth shard — 3 pts — add competing memories for ranking tests
- **[P1]** Review Jiahao's embedding migration PR — 1 pt

### Jiahao
- **[P0]** Embedding migration dual-write — 5 pts — instrument worker to embed with both models
- **[P1]** Cohere reranker — 5 pts — add reranker pass after RRF fusion, behind feature flag
- **[P1]** API key rotation grace period bug — 2 pts — fix edge case where grace period timer starts from creation instead of rotation time

### Mira (available April 14-20, 23-25)
- **[P0]** Auth middleware automated tests — 3 pts — table-driven tests for hub permission split
- **[P1]** Staging CORS code review — 1 pt
- **[P1]** Security documentation update — 2 pts

### Sarah
- **[P0]** Memory detail page — 8 pts — full implementation per design review decisions
- **[P1]** Hub boundary enforcement tests — 3 pts — verify personal memories never leak to other users

## Risks

- Ziyang's availability is front-loaded. Any blockers in the first 4 days will not be resolved until April 26.
- Embedding migration depends on Voyage AI's `voyage-3-large` being generally available by April 14. If delayed, Jiahao pivots to the reranker work.
- Memory detail page is the largest story (8 pts). Sarah plans to split into 3 PRs: layout, metadata panel, and related memories.

## Carry-Over from Last Sprint

- Dream engine improvements (deferred to next sprint — needs design review first)
- CLI `memax agents sync` error handling improvements (low priority, moved to backlog)
