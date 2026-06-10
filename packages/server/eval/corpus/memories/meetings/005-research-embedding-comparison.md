Research: embedding model comparison for Memax retrieval.

## Models evaluated

### Voyage AI (voyage-3-large)
- 1024 dimensions, 16K token context
- MTEB retrieval score: 67.2 (top tier)
- Latency: ~120ms for single document, supports batching up to 128 docs
- Cost: $0.00013 per 1K tokens
- Currently used in Memax production
- Strengths: excellent on short queries against long docs, good code understanding
- Weakness: slightly worse on multilingual than OpenAI

### OpenAI (text-embedding-3-large)
- 3072 dimensions (can truncate to 1024 or 256 via Matryoshka)
- MTEB retrieval score: 66.0
- Latency: ~90ms single doc, batching up to 2048 docs
- Cost: $0.00013 per 1K tokens
- Strengths: largest batch sizes, Matryoshka flexibility for storage/speed tradeoffs
- Weakness: no prefix instruction support, slightly lower retrieval accuracy

### Cohere (embed-v4.0)
- 1024 dimensions, 128K token context
- MTEB retrieval score: 66.8
- Latency: ~150ms single doc, batching up to 96 docs
- Cost: $0.00010 per 1K tokens
- Strengths: longest context window, built-in search/document input types, cheapest per token
- Weakness: higher latency, smaller batch sizes

## Memax-specific benchmark results

Ran our internal eval corpus (50 queries, 200 memories) through all three:

| Model | nDCG@5 | MRR | Recall@5 | p95 latency |
|-------|--------|-----|----------|-------------|
| Voyage 3 Large | 0.74 | 0.82 | 0.68 | 140ms |
| OpenAI 3 Large | 0.70 | 0.79 | 0.65 | 105ms |
| Cohere v4 | 0.72 | 0.80 | 0.67 | 170ms |

## Decision
Staying with Voyage AI. The nDCG@5 advantage is meaningful (4 percentage points over OpenAI), and the code understanding is important since many Memax memories reference code. Cohere is a close second and worth revisiting when we add multilingual support. OpenAI's Matryoshka feature is interesting for a future tiered-storage optimization.
