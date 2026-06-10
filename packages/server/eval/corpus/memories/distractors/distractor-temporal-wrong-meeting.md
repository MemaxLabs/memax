## Sprint Review — February 28, 2026

Sprint review for the Feb 14-28 sprint. Attendees: Ziyang, Jiahao, Mira, Sarah.

**What shipped:**
- Basic memory CRUD (push, get, list, delete) — Jiahao
- CLI scaffolding with Commander.js, `memax push` and `memax recall` commands — Ziyang
- Initial Voyage AI embedding integration — Mira
- PostgreSQL schema with pgvector extension, first migrations — Sarah

**Demo:** Jiahao demoed pushing a memory via the CLI and retrieving it. Basic vector similarity search working but accuracy was low — embeddings only, no hybrid search yet.

**Blockers:**
- Voyage AI rate limits causing timeouts on batch imports (>50 memories)
- pgvector HNSW index not yet tuned, recall@10 around 0.6

**Next sprint goals:**
- Implement hybrid search (pgvector + pg_trgm + RRF fusion)
- Add OAuth authentication (GitHub provider first)
- Set up Fly.io staging environment
- Begin web app scaffolding with Next.js

Retro: Team velocity is good for a 4-person team. Need to improve test coverage before adding more features.
