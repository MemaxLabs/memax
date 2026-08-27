---
name: eval
description: "Retrieval eval: run locally before pushing retrieval/ingestion changes. ALWAYS trigger on any work touching packages/server/internal/handler/recall.go, ask.go, packages/server/internal/retrieval/, packages/server/internal/ingest/, packages/server/internal/store/postgres_chunks.go, postgres_memories.go, packages/server/migrations/, or packages/server/eval/. Even 'small' scoring tweaks or filter changes need eval verification — a single constant change can regress retrieval quality across 59 queries."
---

# Retrieval Eval

This skill ensures retrieval and ingestion pipeline changes are verified with the eval before pushing. The eval seeds 80 memories across 8 domains, runs 59 queries with graded relevance labels, and measures IR metrics (nDCG, MRR, Precision, Recall, Harmful).

## The Rule

Any change to retrieval scoring, filtering, distillation, chunking, embedding, fusion, or ingestion MUST be verified locally with the eval before pushing to main. Do not use CI as your test environment.

## How to Run

```bash
cd packages/server
export VOYAGE_API_KEY=$(doppler secrets get VOYAGE_API_KEY --plain) \
  && export ANTHROPIC_API_KEY=$(doppler secrets get ANTHROPIC_API_KEY --plain) \
  && export ANTHROPIC_BASE_URL=$(doppler secrets get ANTHROPIC_BASE_URL --plain 2>/dev/null || echo "") \
  && go test ./eval/ -v -timeout 600s
```

Requirements: `doppler` CLI with access to the project secrets, PostgreSQL running locally, `DATABASE_URL` set.

Runtime: ~2 minutes (80 memories, 59 queries with live embeddings and distillation). First run after enrichment changes takes longer due to re-embedding.

### Reranker note

Local eval and CI run **without Cohere rerank** — it is expensive and heavily rate-limited. The scoring pipeline is tuned to produce good ranking without the reranker; Cohere is an optional lift layer. The nightly eval (`nightly-eval.yml`) includes `COHERE_API_KEY` for full-stack coverage. When tuning locally, focus on the no-rerank baseline.

### Quick validation (no DB/API keys)

```bash
cd packages/server
go test ./eval/ -run TestEvalCorpusLoads -v      # corpus structure
go test ./eval/ -count=1                          # metrics + reports + corpus
```

## Aggregate Thresholds

**数字的唯一来源是 `packages/server/eval/thresholds.go` 的 `Thresholds` 变量** — 断言与报告同源引用。这张表只解释每个指标衡量什么,不引用数字(2026-08 之前这里抄的数字与代码漂移了 0.05–0.10,教训备案)。

| Metric            | Measures                                          |
| ----------------- | ------------------------------------------------- |
| nDCG@5 / nDCG@10  | Graded ranking quality in top 5 / top 10          |
| MRR@10            | First relevant result appears early               |
| Precision@5       | Fraction of top 5 that are relevant               |
| StrongPrecision@3 | Fraction of top 3 that are highly relevant        |
| Recall@20         | Fraction of relevant docs found in top 20         |
| Harmful@10        | Must be 0 — no stale/misleading results in top 10 |

## Per-Query Checks

- Non-negative queries: Harmful@10 == 0 AND MRR@10 > 0
- Negative queries: Harmful@10 == 0 (result count tracked but not enforced without reranker)
- Per-query failure tolerance: up to 5% of graded queries may fail
- Keyword checks run as supplemental assertions

## Corpus Structure

```
packages/server/eval/corpus/
  manifest.json                      shard list
  memories/
    smoke/metadata.json + *.md       developer content (10 memories)
    product/metadata.json + *.md     PM content (8 memories)
    personal/metadata.json + *.md    daily/lifestyle content (8 memories)
    meetings/metadata.json + *.md    team meetings + research (6 memories)
    distractors/metadata.json + *.md hard negatives (12 memories)
    code/metadata.json + *.md        code patterns + stack traces (12 memories)
    multilingual/metadata.json + *.md Chinese + mixed-language (10 memories)
    depth/metadata.json + *.md       detailed notes + cross-domain (14 memories)
  queries/
    smoke.json                       developer queries (12)
    product.json                     PM queries (6)
    personal.json                    daily queries (6)
    meetings.json                    meeting/research queries (5)
    negative.json                    negative queries (6)
    code.json                        code queries (8)
    multilingual.json                multilingual queries (8)
    compound.json                    compound filter queries (8)
  scopes/
    core.json                        5 users, 5 hubs, memberships
```

## When Changing the Pipeline

1. **Run eval locally FIRST** — verify metrics before pushing
2. **If a metric regresses** — fix the pipeline, not the eval labels
3. **If a query fails** — investigate why the memory doesn't rank. Common causes:
   - Chunk 0 has weak content (fix: improve the memory's intro or summary)
   - Cross-project noise (fix: project context penalty in scoring)
   - Author not extracted (fix: distiller prompt)
   - Single-lane fuzzy match noise (fix: multi-lane confirmation)
4. **Only soften eval labels when the label itself is wrong** — not when retrieval fails to meet it
5. **Add new corpus memories and queries** when you discover uncovered scenarios

## Adding to the Corpus

- Put memory bodies in `.md` files, metadata in `metadata.json`
- Include full metadata: owner_id, hub_id, source, source_agent, project_context, timestamps, tags, hint
- Queries need graded relevance: 3=ideal, 2=highly relevant, 1=related, 0=irrelevant, -1=harmful
- Only use -1 for content that would cause an agent to do the wrong thing (stale superseded docs, access violations)
- Add distractor memories for new query types to test precision
- Run `TestEvalCorpusLoads` to validate structure before running the full eval

## Design Reference

See `docs/engineering/retrieval-eval-design.md` for the full evaluation system design including future phases (baseline comparison, nightly evals, production monitoring).
