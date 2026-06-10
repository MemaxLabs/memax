Notes on prompt engineering for the query distillation pipeline.

## Overview

When a user submits a recall query, the system optionally distills it through an LLM to extract structured intent before retrieval. There are two distillers:

1. **Compact distiller** — runs on every query. Uses Claude Haiku. Extracts: keywords, inferred topic, temporal hints, author mentions. Latency budget: <200ms.
2. **Full distiller** — runs on complex or ambiguous queries (>10 words or contains pronouns). Uses Claude Haiku 3.5. Generates: rewritten query, search strategy, expected memory types. Latency budget: <500ms.

## Compact Distiller Prompt

The compact distiller prompt went through 4 iterations:

**v1 (initial):** "Extract keywords from this query." Too naive — returned only nouns, missed intent.

**v2:** Added "Also identify temporal references and person names." Better, but the model would hallucinate temporal references ("last week" even when the query said "recently").

**v3:** Added negative instruction: "Only extract temporal references that are explicitly stated. Do not infer time periods." Fixed the hallucination issue.

**v4 (current):** Structured output with JSON schema. The prompt now includes 3 few-shot examples covering: a simple topical query, a temporal query with author mention, and a negation query ("what did we decide NOT to do"). This version has been stable since March 28.

## Full Distiller Improvements

The full distiller rewrites ambiguous queries into more searchable forms. Key improvements:

- **Pronoun resolution:** "What did he say about that?" becomes "What did [author from context] say about [topic from recent conversation]?" when project_context is available.
- **Compound query decomposition:** "auth and deploy issues this week" gets split into two sub-queries that are searched independently and results are merged.
- **Negation handling:** "What did we decide not to use?" is rewritten to search for decision/rationale memories that mention rejected alternatives.

## Metrics

On our eval corpus, query distillation improves:
- nDCG@5: +0.06 (from 0.73 to 0.79)
- MRR: +0.04 (from 0.81 to 0.85)

The improvement is concentrated on compound queries and queries with temporal references. Simple keyword queries see no improvement (distillation is a no-op for them).

## Cost

Compact distiller: ~$0.0001 per query (Haiku input: ~100 tokens, output: ~50 tokens).
Full distiller: ~$0.0008 per query (Haiku 3.5 input: ~300 tokens, output: ~150 tokens).
At 10K queries/day, total distillation cost: ~$2.50/day.
