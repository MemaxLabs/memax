# LoCoMo Benchmark for Memax

Evaluates Memax on the LoCoMo long-term conversational memory benchmark from Snap Research.

The harness is designed to be comparable with the methodology used by the Mem0 paper and the `mem0ai/memory-benchmarks` LoCoMo runner:

- Uses the official `locomo10.json` dataset from `snap-research/locomo`.
- Defaults to categories `1,2,3,4`, excluding adversarial category 5.
- Builds raw-dialog corpus entries by default, one dialogue turn per memory, matching the turn-level ingestion used by memory-system benchmark runners.
- Reports evidence recall at configurable cutoffs, defaulting to `10,20,50,200`.
- Optional `--qa` generates short answers from configurable retrieved-memory cutoffs. The default QA cutoff is `10`, matching Memax's production context depth.
- Optional `--judge` adds LLM-as-judge scoring for generated answers. This is useful for Mem0-style benchmark reports, but it is intentionally separate from retrieval-only and lexical QA runs because it adds model cost and variance.
- Reports search latency and total latency p50/p95 in seconds.

It does not store third-party baseline scores. Compare by running the same dataset, categories, top-k cutoffs, answerer model, judge model, and repeat count as the baseline you care about.

## Quick Start

```bash
cd packages/server/eval/locomo

# Quick smoke run: downloads the dataset, runs 10 questions, retrieval only.
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ./run.sh

# Full comparison-ready retrieval run.
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ./run.sh --full

# Full retrieval + production-shaped answer generation at top-10.
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... ./run.sh --full --qa

# Same QA run, but with query distillation disabled.
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... ./run.sh --full --qa --no-distill

# Mem0-style cutoff QA on the two production-relevant/default depths.
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... ./run.sh --full --qa --judge --qa-cutoffs=10,20

# Full Mem0 benchmark curve.
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... ./run.sh --full --qa --judge --qa-cutoffs=10,20,50,200
```

## Direct Go Usage

```bash
cd packages/server
go run ./cmd/locomo/ \
  -dataset eval/locomo/data/locomo10.json \
  -corpus-mode dialog \
  -limit 200 \
  -cutoffs 10,20,50,200 \
  -distill=false \
  -qa \
  -qa-cutoffs 10,20 \
  -judge \
  -categories 1,2,3,4 \
  -output eval/locomo/results/run
```

## Flags

| Flag | Default | Description |
|---|---:|---|
| `-dataset` | required | Path to `locomo10.json` |
| `-corpus-mode` | `dialog` | `dialog`, `observation`, or `summary` |
| `-limit` | `200` | Max memories returned by Memax recall |
| `-cutoffs` | `10,20,50,200` | Evidence recall cutoffs |
| `-distill` | `true` | Enable query distillation when `ANTHROPIC_API_KEY` is configured |
| `-categories` | `1,2,3,4` | LoCoMo categories; excludes adversarial by default |
| `-max-questions` | `0` | Limit for smoke runs |
| `-samples` | empty | Comma-separated sample IDs |
| `-qa` | false | Generate short answers from retrieved memories |
| `-qa-cutoffs` | `10` | Retrieved-memory cutoffs used for QA generation |
| `-qa-model` | `claude-haiku-4-5-20251001` | Anthropic model used for answer generation |
| `-qa-max-tokens` | `64` | Max answer tokens |
| `-judge` | false | Run LLM-as-judge for each generated answer |
| `-judge-model` | `claude-haiku-4-5-20251001` | Anthropic model used for judge scoring |
| `-judge-evidence` | false | Include gold evidence context in judge prompts |
| `-output` | `.` | Output directory |

`-limit` must be greater than or equal to every value in `-cutoffs` and `-qa-cutoffs`; otherwise the runner exits instead of producing inflated cutoff metrics from non-returned corpus items.

## Recommended Setups

| Goal | Flags |
|---|---|
| Production retrieval | `-limit 10 -cutoffs 10` |
| Production QA | `-limit 10 -cutoffs 10 -qa -qa-cutoffs 10` |
| Production QA without distillation | `-limit 10 -cutoffs 10 -distill=false -qa -qa-cutoffs 10` |
| Mem0 OSS-default comparison | `-limit 20 -cutoffs 10,20 -qa -qa-cutoffs 10,20 -judge` |
| Mem0 OSS-default comparison without distillation | `-limit 20 -cutoffs 10,20 -distill=false -qa -qa-cutoffs 10,20 -judge` |
| Mem0 benchmark curve | `-limit 200 -cutoffs 10,20,50,200 -qa -qa-cutoffs 10,20,50,200 -judge` |
| Cheaper diagnostic curve | `-limit 200 -cutoffs 10,20,50,200` |

## Deferred Run Plan

When we have time and API budget, run these in order:

1. `./run.sh --full --qa --judge --qa-cutoffs=10,20`
2. `./run.sh --full --qa --judge --qa-cutoffs=10,20 --no-distill`
3. Optional full benchmark curve: `./run.sh --full --qa --judge --qa-cutoffs=10,20,50,200`

The first two runs answer the immediate question: top-10 production quality, Mem0 OSS-default top-20 comparison, and the effect of query distillation. The third run is only needed for paper-style cutoff curves.

## Comparison Checklist

For apples-to-apples comparison against Mem0-style LoCoMo reports:

1. Use the official dataset and categories `1,2,3,4`.
2. Use `-corpus-mode dialog`, unless explicitly comparing against observation or summary RAG.
3. Use the same retrieval depth and reported cutoffs, commonly top-k `200` with cutoffs `10,20,50,200`.
4. Use the same QA cutoffs. Memax production should emphasize `10`; Mem0 OSS defaults to `20`; the Mem0 LoCoMo benchmark runner uses top-k `200` to report the `10,20,50,200` curve.
5. Use the same answerer and judge model. This Go runner supports Anthropic answerer/judge models; exact GPT-family comparability still requires running the same external answerer/judge.
6. Repeat stochastic LLM judge runs the same number of times and report mean plus standard deviation.

## Output

`memax_locomo_results.jsonl` contains one row per question with:

- ranked retrieved corpus items and source dialog IDs
- evidence recall and nDCG metrics at each cutoff
- optional generated answers and lexical/LLM-judge QA metrics under `qa_results`, keyed by cutoff
- per-question search and total latency in the benchmark log and summary

The dataset is not committed because LoCoMo is distributed under CC BY-NC 4.0.
