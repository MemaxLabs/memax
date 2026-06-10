Research notes: RAG evaluation approaches and methodology.

## Benchmark suites surveyed

**BEIR (Benchmarking IR)** — heterogeneous retrieval benchmark with 18 datasets spanning bio-medical, finance, and open-domain QA. Uses nDCG@10 as the primary metric. Good for testing generalization across domains, but datasets are all public web text — not representative of personal/team knowledge bases like Memax.

**MTEB (Massive Text Embedding Benchmark)** — broader than BEIR, covers retrieval, classification, clustering, and STS. 56 datasets across 8 tasks. Useful for comparing embedding models holistically, but retrieval is only one of eight task categories so the ranking doesn't always predict retrieval-specific performance.

**MIRACL** — multilingual retrieval benchmark. Relevant for Memax's future i18n support but not a priority for the English-first MVP.

## Metrics deep dive

- **nDCG@k (Normalized Discounted Cumulative Gain)** — our primary metric. Rewards relevant results appearing earlier in the ranked list. Using k=5 for Memax evals since we typically inject top-5 memories into agent context.
- **MRR (Mean Reciprocal Rank)** — position of first relevant result. Useful for single-answer queries but doesn't capture multi-memory scenarios well.
- **Recall@k** — fraction of relevant memories retrieved. Important for ensuring we don't miss critical context, but doesn't penalize irrelevant noise.
- **Precision@k** — fraction of retrieved results that are relevant. Critical for Memax since injecting irrelevant memories wastes agent context window.

## Memax-specific considerations
- Our corpus is small (hundreds, not millions of docs) so statistical significance requires careful bootstrap sampling.
- Graded relevance (0-3 scale) is better than binary for our use case — a tangentially related memory (grade 1) is different from a perfect match (grade 3).
- Must test cross-hub retrieval: personal + team memories in the same query.
- Temporal signals matter — a standup from yesterday should rank higher than one from 3 months ago for "what are the current blockers?"
