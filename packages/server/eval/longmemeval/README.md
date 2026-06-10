# LongMemEval Benchmark for Memax

Evaluates Memax's retrieval pipeline against the [LongMemEval benchmark](https://github.com/xiaowu0162/LongMemEval) (ICLR 2025) — 500 questions testing long-term memory retrieval across 6 task types.

## Quick Start

```bash
# From packages/server/eval/longmemeval/

# Quick smoke test: oracle dataset, 10 questions
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ./run.sh

# Full benchmark: s_cleaned dataset, all 500 questions
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... ./run.sh --full

# Full + QA generation + GPT-4o evaluation
DATABASE_URL=postgres://... VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... OPENAI_API_KEY=... \
  ./run.sh --full --qa --eval
```

## What It Tests

LongMemEval measures retrieval quality over long conversational histories. Each question comes with a haystack of timestamped chat sessions — evidence buried among fillers.

| Task Type | Count | Description |
|---|---|---|
| single-session-user | 70 | Extract info from user's past messages |
| single-session-assistant | 56 | Extract info from assistant's past messages |
| single-session-preference | 30 | Recall user preferences |
| multi-session | 133 | Reason across multiple sessions |
| knowledge-update | 78 | Recall updated (not outdated) information |
| temporal-reasoning | 133 | Time-based reasoning over sessions |
| Abstention | 30 | Correctly identify unanswerable questions |

## How It Works

For each of the 500 questions:

1. **Build corpus**: Convert the haystack's chat sessions into corpus documents (one per session or per user turn, depending on granularity)
2. **Ingest**: Chunk, embed (Voyage AI), and insert into PostgreSQL as Memax memories
3. **Query**: Run Memax's full recall pipeline (hybrid search: vector + BM25 + trigram + field, with optional distillation and reranking)
4. **Evaluate**: Compute retrieval metrics by mapping recalled memories back to corpus IDs
5. **Clean up**: Remove per-question data to avoid cross-contamination

## Metrics

### Retrieval (main focus)

| Metric | Description |
|---|---|
| recall_any@k | At least one evidence document in top-k |
| recall_all@k | All evidence documents in top-k |
| ndcg_any@k | NDCG with binary relevance |

Reported at k = 1, 3, 5, 10, 30, 50. Both session-level and turn-level (when using turn granularity).

### QA (optional)

Overall accuracy and per-task accuracy, evaluated by GPT-4o judge (matching LongMemEval's original methodology).

## Dataset Sizes

| Dataset | Haystack Size | Avg Sessions | Purpose |
|---|---|---|---|
| oracle | Evidence only | ~2 | Validation / upper bound |
| s_cleaned | ~115k tokens | ~48 | Standard benchmark |
| m_cleaned | ~500 sessions | ~500 | Stress test |

## CLI Usage

```bash
cd packages/server

# Direct Go invocation
go run ./cmd/longmemeval/ \
  -dataset eval/longmemeval/data/longmemeval_s_cleaned.json \
  -granularity session \
  -output eval/longmemeval/results/ \
  -limit 50

# With QA generation
go run ./cmd/longmemeval/ \
  -dataset eval/longmemeval/data/longmemeval_s_cleaned.json \
  -qa \
  -output eval/longmemeval/results/

# Quick test: first 10 questions of a specific type
go run ./cmd/longmemeval/ \
  -dataset eval/longmemeval/data/longmemeval_oracle.json \
  -max-questions 10 \
  -types temporal-reasoning
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-dataset` | (required) | Path to LongMemEval JSON file |
| `-granularity` | session | Corpus granularity: `session` or `turn` |
| `-limit` | 50 | Max recall results per query |
| `-max-questions` | 0 (all) | Limit to first N questions |
| `-types` | (all) | Comma-separated question types |
| `-skip-abstention` | false | Skip abstention questions |
| `-qa` | false | Enable QA hypothesis generation |
| `-output` | . | Output directory |

## Output Files

| File | Format | Consumer |
|---|---|---|
| `memax_retrievallog_session.jsonl` | JSONL | LongMemEval's `print_retrieval_metrics.py` |
| `memax_qa_hypotheses.jsonl` | JSONL | LongMemEval's `evaluate_qa.py` |
| `benchmark.log` | Text | Human-readable run log |

### Using LongMemEval's Python Scripts

The output files are directly compatible with LongMemEval's evaluation scripts:

```bash
# Retrieval metrics
python3 LongMemEval/src/evaluation/print_retrieval_metrics.py \
  results/memax_retrievallog_session.jsonl

# QA evaluation (requires OPENAI_API_KEY)
python3 LongMemEval/src/evaluation/evaluate_qa.py \
  gpt-4o \
  results/memax_qa_hypotheses.jsonl \
  data/longmemeval_s_cleaned.json

# QA metrics
python3 LongMemEval/src/evaluation/print_qa_metrics.py \
  results/memax_qa_hypotheses.jsonl.eval-results-gpt-4o \
  data/longmemeval_s_cleaned.json
```

## Environment Variables

| Variable | Required | Purpose |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL connection |
| `VOYAGE_API_KEY` | Yes | Voyage AI embeddings |
| `ANTHROPIC_API_KEY` | No | Query distillation + QA synthesis |
| `OPENAI_API_KEY` | No | GPT-4o QA judge (Python eval only) |

## Cost Estimates

| Component | s_cleaned (500 questions) | Notes |
|---|---|---|
| Embeddings | ~25,000 calls | ~50 sessions × 500 questions |
| Distillation | ~500 Haiku calls | 1 per query (if ANTHROPIC_API_KEY set) |
| QA synthesis | ~500 Haiku calls | 1 per query (if --qa) |
| QA evaluation | ~500 GPT-4o calls | 1 per question (if --eval) |

## Methodology Faithfulness

This implementation reproduces LongMemEval's evaluation methodology exactly:

- **Corpus construction** matches `process_item_flat_index` (session-level: concatenate user turns; turn-level: one doc per user turn; "noans" reclassification for answer sessions without has_answer=true turns)
- **Retrieval metrics** match `eval_utils.py` (DCG formula, binary relevance, recall_any/all, NDCG)
- **Turn-to-session conversion** matches `evaluate_retrieval_turn2session` (strip turn suffix, expand k for dedup)
- **Abstention exclusion** matches the Python: questions with `_abs` in ID and questions without any has_answer=true turns are excluded from retrieval metrics
- **QA output format** matches evaluate_qa.py input: `{"question_id": "...", "hypothesis": "..."}`
- **QA judge prompts** are run via LongMemEval's Python scripts directly (not reimplemented)

## Interpreting Results

LongMemEval baseline results (from the paper, s_cleaned, session-level):

| Retriever | recall_all@5 | ndcg_any@5 | recall_all@10 | ndcg_any@10 |
|---|---|---|---|---|
| BM25 | 0.30 | 0.44 | 0.42 | 0.49 |
| Contriever | 0.21 | 0.35 | 0.32 | 0.40 |
| Stella 1.5B | 0.47 | 0.57 | 0.56 | 0.61 |
| GTE-Qwen2 7B | 0.54 | 0.63 | 0.65 | 0.68 |

Memax uses Voyage AI embeddings + BM25 + trigram hybrid search with RRF fusion, so we expect to outperform single-method baselines. The query distillation and reranking stages should further improve results.
