# Memax — Copilot Instructions

## Core Instructions

Read and follow all instructions in `AGENTS.md`. This is the single source of truth for the project's architecture, conventions, and rules.

## Unified Agent Skills

- This project maintains specialized expert guidance and workflows in `.agents/skills/`.
- **Mandate:** Before performing specialized tasks (e.g., frontend development, infrastructure setup), check for relevant skill files in `.agents/skills/`.
- If the task touches frontend styling or components, use the guidance in `.agents/skills/frontend/`.

## Product Overview

Memax is a universal context & memory hub for AI agents. Cloud-hosted memory layer between users and their AI coding agents, providing persistent, shared, secure access to knowledge.

- CLI (`memax-cli` on npm, binary: `memax`) — TypeScript, Commander.js
- Web App (memax.app) — Next.js 16, Tailwind, Radix UI, TanStack Query
- Developer Hub (docs.memax.app) — Fumadocs (Next.js), Pagefind, Scalar
- API Server — Go (Chi/Fiber)
- Shared Design System — @memaxlabs/ui (Tailwind + Radix)
- Public SDK/CLI source — `MemaxLabs/memax`

## Monorepo Layout

packages/server (Go API), packages/web (Next.js memax.app), packages/docs-site (Fumadocs docs.memax.app), packages/ui (@memaxlabs/ui design system).

## Architecture Rules

- Private by default — all memories private unless explicitly shared
- Boundaries enforced at data layer (PostgreSQL RLS)
- Retrieval: precision over recall
- Graceful degradation — never block user's agent
- Idempotent pushes — content-hash dedup
- Agent-agnostic — design for CLI piping

## Security

- No secrets in code
- Every memory access must verify requester's access level
- Secret detection on push
- TLS 1.3, encryption at rest
- Audit log all memory access
- Access tokens: 1h expiry. Refresh tokens: 30d.

## Design Docs

Architecture decisions are documented in docs/plans/ (01 through 11). Read the relevant plan before implementing features.
