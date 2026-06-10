Reading notes from "The Pragmatic Programmer" by David Thomas and Andrew Hunt (20th Anniversary Edition). Finished reading March 20, 2026.

## Core Philosophy

The book advocates for pragmatism over dogma in software development. Key themes: take responsibility for your craft, think critically about your tools and processes, and always be learning. The "good enough software" principle resonated strongly — ship when value exceeds remaining defect cost, don't polish forever.

## Favorite Concepts

**DRY (Don't Repeat Yourself):** Not just about code duplication — it's about knowledge duplication. If a business rule exists in the database schema, don't also encode it in validation logic and again in the UI. This is exactly what we struggle with in Memax: the API contract, SDK types, and web hooks all encode the same memory shape.

**Tracer Bullets:** Build a thin end-to-end slice of functionality first, then iterate. The memax MVP followed this approach — we built push + recall as a CLI-only flow before adding the web app or MCP integration.

**Orthogonality:** Components should be independent. Changing one should not require changes in others. This is why public API types live in `memax-sdk` — client contracts change in one place rather than being duplicated across CLI, web, and SDK consumers.

**Broken Windows Theory:** Don't tolerate small defects. One unformatted file leads to a codebase where nobody formats. This is why our AGENTS.md mandates `pnpm format && pnpm lint` before every commit.

## Favorite Quotes

> "You can't write perfect software. Did that hurt? It shouldn't. Accept it as an axiom of life."

> "Don't live with broken windows."

> "Remember the big picture. Don't get so engrossed in the details that you forget to check what's happening around you."

## How This Applies to Memax

The "programming by coincidence" chapter describes our early retrieval pipeline — we had heuristic ranking that happened to work on our test data but fell apart on real queries. Building the eval corpus was our way of moving from coincidence to deliberate design.

The "domain languages" chapter inspired the `memax-context` tag format for Claude Code hooks. We created a mini-DSL for injecting context rather than using raw text.

Recommended as required reading for anyone joining the team. The chapter on estimation is especially relevant for sprint planning.
