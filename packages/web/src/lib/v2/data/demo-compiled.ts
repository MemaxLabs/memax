/**
 * What @memaxlabs/compiler writes for the demo Brief B-0043
 * (demo-brief-data.ts): generated once by running the real compiler on
 * the demo record, then pasted here, so the demo's compiled files are
 * exactly what Memax writes (D2: AGENTS.md is canonical, CLAUDE.md is an
 * `@AGENTS.md` shim, Cursor gets a rule only for path-scoped facts, and
 * ChatGPT is copy-out text). Regenerate it when the demo record changes.
 * Data, not copy.
 */
import type { IncludeMode, StaleMode } from "./targets";

export interface DemoCompiled {
  content: string;
  bytes: number;
  lines: number;
  refs: string[];
  cites: string[];
  dropped: string[];
}

export type DemoVariant = `${IncludeMode}:${StaleMode}`;

/** AGENTS.md (C-0881), for each Include and Stale facts setting. */
export const DEMO_AGENTS_MD: Record<DemoVariant, DemoCompiled> = {
  "kept_and_open:mark": {
    content:
      "<!-- Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0881). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n# Memax V2 engineering brief\n\nWhat every agent on this project reads before it writes code.\nEvery line cites a memory. Ask the memax MCP server for anything else.\n\n## Decisions\n- Background jobs run on River, Postgres‑backed. We do not use Temporal. [M-0219]\n- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. [M-0102]\n- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n- (Being verified) Ask memax answers with the Haiku tier. [M-0187]\n\n## Conventions\n- pnpm workspaces only. Never run `npm install` at the root. [M-0071]\n- Every write tool returns a receipt ID the caller can cite. [M-0112]\n- Compile adapters live in packages/compiler, one per target. [M-0436]\n\n### In `packages/web/**`\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n\n## Open\n- Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]\n\n## Live context\nUse the memax MCP server for anything not here:\nmemax_recall, memax_search, memax_get. Propose with memax_push.\n",
    bytes: 1263,
    lines: 27,
    refs: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0187",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    cites: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0174",
      "M-0187",
      "M-0219",
      "M-0431",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    dropped: [],
  },
  "kept_and_open:omit": {
    content:
      "<!-- Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0881). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n# Memax V2 engineering brief\n\nWhat every agent on this project reads before it writes code.\nEvery line cites a memory. Ask the memax MCP server for anything else.\n\n## Decisions\n- Background jobs run on River, Postgres‑backed. We do not use Temporal. [M-0219]\n- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. [M-0102]\n- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n\n## Conventions\n- pnpm workspaces only. Never run `npm install` at the root. [M-0071]\n- Every write tool returns a receipt ID the caller can cite. [M-0112]\n- Compile adapters live in packages/compiler, one per target. [M-0436]\n\n### In `packages/web/**`\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n\n## Open\n- Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]\n\n## Live context\nUse the memax MCP server for anything not here:\nmemax_recall, memax_search, memax_get. Propose with memax_push.\n",
    bytes: 1196,
    lines: 26,
    refs: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    cites: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0174",
      "M-0219",
      "M-0431",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    dropped: [],
  },
  "kept_only:mark": {
    content:
      "<!-- Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0881). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n# Memax V2 engineering brief\n\nWhat every agent on this project reads before it writes code.\nEvery line cites a memory. Ask the memax MCP server for anything else.\n\n## Decisions\n- Background jobs run on River, Postgres‑backed. We do not use Temporal. [M-0219]\n- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. [M-0102]\n- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n- (Being verified) Ask memax answers with the Haiku tier. [M-0187]\n\n## Conventions\n- pnpm workspaces only. Never run `npm install` at the root. [M-0071]\n- Every write tool returns a receipt ID the caller can cite. [M-0112]\n- Compile adapters live in packages/compiler, one per target. [M-0436]\n\n### In `packages/web/**`\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n\n## Live context\nUse the memax MCP server for anything not here:\nmemax_recall, memax_search, memax_get. Propose with memax_push.\n",
    bytes: 1153,
    lines: 24,
    refs: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0187",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    cites: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0187",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    dropped: [],
  },
  "kept_only:omit": {
    content:
      "<!-- Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0881). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n# Memax V2 engineering brief\n\nWhat every agent on this project reads before it writes code.\nEvery line cites a memory. Ask the memax MCP server for anything else.\n\n## Decisions\n- Background jobs run on River, Postgres‑backed. We do not use Temporal. [M-0219]\n- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. [M-0102]\n- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n\n## Conventions\n- pnpm workspaces only. Never run `npm install` at the root. [M-0071]\n- Every write tool returns a receipt ID the caller can cite. [M-0112]\n- Compile adapters live in packages/compiler, one per target. [M-0436]\n\n### In `packages/web/**`\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n\n## Live context\nUse the memax MCP server for anything not here:\nmemax_recall, memax_search, memax_get. Propose with memax_push.\n",
    bytes: 1086,
    lines: 23,
    refs: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    cites: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    dropped: [],
  },
};

/** The ChatGPT project instructions (C-0884), for each setting. */
export const DEMO_CHATGPT: Record<DemoVariant, DemoCompiled> = {
  "kept_and_open:mark": {
    content:
      "Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0884). Edit it at https://memax.app/memax-v2/brief.\n\nMemax V2 engineering brief\nWhat every agent on this project reads before it writes code.\nEvery line cites a memory.\n\nDecisions\n- Background jobs run on River, Postgres‑backed. We do not use Temporal. [M-0219]\n- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. [M-0102]\n- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n- (Being verified) Ask memax answers with the Haiku tier. [M-0187]\n\nConventions\n- pnpm workspaces only. Never run `npm install` at the root. [M-0071]\n- Every write tool returns a receipt ID the caller can cite. [M-0112]\n- Compile adapters live in packages/compiler, one per target. [M-0436]\n\nIn packages/web/**\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n\nOpen\n- Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]\n\nLive context\nUse the memax connector for anything not here: search_memories, get_memory. Propose new memories with save_memory.\n",
    bytes: 1158,
    lines: 26,
    refs: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0187",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    cites: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0174",
      "M-0187",
      "M-0219",
      "M-0431",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    dropped: [],
  },
  "kept_and_open:omit": {
    content:
      "Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0884). Edit it at https://memax.app/memax-v2/brief.\n\nMemax V2 engineering brief\nWhat every agent on this project reads before it writes code.\nEvery line cites a memory.\n\nDecisions\n- Background jobs run on River, Postgres‑backed. We do not use Temporal. [M-0219]\n- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. [M-0102]\n- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n\nConventions\n- pnpm workspaces only. Never run `npm install` at the root. [M-0071]\n- Every write tool returns a receipt ID the caller can cite. [M-0112]\n- Compile adapters live in packages/compiler, one per target. [M-0436]\n\nIn packages/web/**\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n\nOpen\n- Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config. [M-0431, M-0174]\n\nLive context\nUse the memax connector for anything not here: search_memories, get_memory. Propose new memories with save_memory.\n",
    bytes: 1091,
    lines: 25,
    refs: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    cites: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0174",
      "M-0219",
      "M-0431",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    dropped: [],
  },
  "kept_only:mark": {
    content:
      "Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0884). Edit it at https://memax.app/memax-v2/brief.\n\nMemax V2 engineering brief\nWhat every agent on this project reads before it writes code.\nEvery line cites a memory.\n\nDecisions\n- Background jobs run on River, Postgres‑backed. We do not use Temporal. [M-0219]\n- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. [M-0102]\n- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n- (Being verified) Ask memax answers with the Haiku tier. [M-0187]\n\nConventions\n- pnpm workspaces only. Never run `npm install` at the root. [M-0071]\n- Every write tool returns a receipt ID the caller can cite. [M-0112]\n- Compile adapters live in packages/compiler, one per target. [M-0436]\n\nIn packages/web/**\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n\nLive context\nUse the memax connector for anything not here: search_memories, get_memory. Propose new memories with save_memory.\n",
    bytes: 1051,
    lines: 23,
    refs: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0187",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    cites: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0187",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    dropped: [],
  },
  "kept_only:omit": {
    content:
      "Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0884). Edit it at https://memax.app/memax-v2/brief.\n\nMemax V2 engineering brief\nWhat every agent on this project reads before it writes code.\nEvery line cites a memory.\n\nDecisions\n- Background jobs run on River, Postgres‑backed. We do not use Temporal. [M-0219]\n- Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28. [M-0102]\n- API errors are RFC 9457 problem+json, never bare strings. [M-0098]\n\nConventions\n- pnpm workspaces only. Never run `npm install` at the root. [M-0071]\n- Every write tool returns a receipt ID the caller can cite. [M-0112]\n- Compile adapters live in packages/compiler, one per target. [M-0436]\n\nIn packages/web/**\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n\nLive context\nUse the memax connector for anything not here: search_memories, get_memory. Propose new memories with save_memory.\n",
    bytes: 984,
    lines: 22,
    refs: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    cites: [
      "M-0071",
      "M-0098",
      "M-0102",
      "M-0112",
      "M-0219",
      "M-0436",
      "M-0441",
      "M-0442",
    ],
    dropped: [],
  },
};

/** The CLAUDE.md shim (C-0882). */
export const DEMO_CLAUDE_MD: DemoCompiled = {
  content:
    "<!-- Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0882). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n@AGENTS.md\n",
  bytes: 166,
  lines: 2,
  refs: [],
  cites: [],
  dropped: [],
};

/** Cursor's one scoped rule (C-0883): the facts scoped to packages/web/**. */
export const DEMO_CURSOR_FILE: DemoCompiled & { path: string } = {
  path: ".cursor/rules/memax-packages-web.mdc",
  ...{
    content:
      "---\ndescription: Facts from memax-v2 for packages/web/**\nglobs: packages/web/**\nalwaysApply: false\n---\n<!-- Compiled by Memax from memax-v2 at 2026-10-05 21:31 UTC (C-0883). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n\n## Conventions\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n",
    bytes: 401,
    lines: 10,
    refs: ["M-0441", "M-0442"],
    cites: ["M-0441", "M-0442"],
    dropped: [],
  },
};

/** The rule as Memax last wrote it to disk (C-0868, Oct 4 09:10). */
export const DEMO_CURSOR_DELIVERED =
  "---\ndescription: Facts from memax-v2 for packages/web/**\nglobs: packages/web/**\nalwaysApply: false\n---\n<!-- Compiled by Memax from memax-v2 at 2026-10-04 16:10 UTC (C-0868). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n\n## Conventions\n- Run tests with `pnpm test`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n";

/** The same file after the hand edit (Oct 4 16:40, commit a41e9c2). */
export const DEMO_CURSOR_OBSERVED =
  "---\ndescription: Facts from memax-v2 for packages/web/**\nglobs: packages/web/**\nalwaysApply: false\n---\n<!-- Compiled by Memax from memax-v2 at 2026-10-04 16:10 UTC (C-0868). Edit it at https://memax.app/memax-v2/brief; edits here come back as proposals. -->\n\n## Conventions\n- Run tests with `pnpm test -- --run`. [M-0441]\n- Web screens use only packages/ledger components; no Tailwind under (ledger). [M-0442]\n- Prefer named exports in React components.\n";

/** What parseBack read from the hand edit. */
export const DEMO_CURSOR_CHANGES = [
  {
    kind: "edit",
    ref: "M-0441",
    refs: ["M-0441"],
    old_text: "Run tests with `pnpm test`.",
    new_text: "Run tests with `pnpm test -- --run`.",
    old_line: 9,
    new_line: 9,
  },
  {
    kind: "new",
    text: "Prefer named exports in React components.",
    line: 11,
    section: "Conventions",
    paths: ["packages/web/**"],
    cites: [],
  },
] as const;
