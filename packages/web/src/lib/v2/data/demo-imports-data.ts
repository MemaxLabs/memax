import type {
  ImportConflictView,
  ImportMemoryView,
  ImportSummary,
  ImportView,
} from "./imports";

/**
 * The demo's import (FirstRun, Cleanup and ReviewImport boards): what
 * `npx memax-cli init` read in memax-v2 at 09:02 on Oct 5. Six files, 40
 * statements, 33 proposals after folding 7 repeats, 3 disagreements,
 * 2 secrets kept on the machine, and one statement Codex read on a web
 * page. The counts add up the way the server's would; where the boards
 * disagree with each other, Cleanup's words win (ReviewImport's first
 * row is also a member of Cleanup's second disagreement there).
 */

export const DEMO_IMPORT_ID = "0192a7c0-0000-7000-8000-0000000001a1";
export const DEMO_IMPORT_SPACE = "memax-v2";
const SPACE_ID = "0192a7c0-0000-7000-8000-000000000002";
const CREATED = "2026-10-05T09:02:00-07:00";

const CODEX = "~/.codex/memories";

export const DEMO_IMPORT_SUMMARY: ImportSummary = {
  id: DEMO_IMPORT_ID,
  spaceId: SPACE_ID,
  client: "memax-cli 2.0.0",
  createdAt: CREATED,
  files: [
    {
      path: "CLAUDE.md",
      kind: "claude_md",
      agent: "claude-code",
      location: "repository",
      statements: 18,
      skipped: 1,
      hiddenCharacters: 0,
    },
    {
      path: "AGENTS.md",
      kind: "agents_md",
      agent: "codex",
      location: "repository",
      statements: 9,
      skipped: 0,
      hiddenCharacters: 0,
    },
    ...["style", "testing", "deploy"].map((name) => ({
      path: `.cursor/rules/${name}.mdc`,
      kind: "cursor_rule",
      agent: "cursor",
      location: "repository" as const,
      statements: 2,
      skipped: name === "deploy" ? 1 : 0,
      hiddenCharacters: 0,
    })),
    {
      path: CODEX,
      kind: "codex_memory",
      agent: "codex",
      location: "home",
      statements: 7,
      skipped: 0,
      hiddenCharacters: 0,
    },
  ],
  skipped: [
    { ref: "CLAUDE.md:38", reason: "secret", detail: "OpenAI API key" },
    {
      ref: ".cursor/rules/deploy.mdc:6",
      reason: "secret",
      detail: "GitHub token",
    },
  ],
  counts: {
    items: 40,
    proposed: 33,
    folded: 7,
    existing: 0,
    refused: 0,
    conflicts: 3,
  },
  check: "checked",
  origin: "init",
};

const AGENT_OF: Record<string, string> = {
  "CLAUDE.md": "claude-code",
  "AGENTS.md": "codex",
  [CODEX]: "codex",
};

function fileOf(ref: string): string {
  return ref.replace(/:\d+$/, "").replace(/^~\/\.codex\/memories\/.*$/, CODEX);
}

let next = 501;
function memory(
  statement: string,
  refs: string[],
  more: Partial<ImportMemoryView> = {},
): ImportMemoryView {
  const ref = `M-${String(next++).padStart(4, "0")}`;
  const files = [...new Set(refs.map(fileOf))];
  return {
    id: `0192a7c0-0000-7000-8000-00000000${ref.slice(2)}`,
    ref,
    version: 1,
    statement,
    state: "proposed",
    outcome: "proposed",
    items: refs.length,
    refs,
    files,
    agent: AGENT_OF[files[0]!] ?? "cursor",
    bulk: true,
    held: null,
    conflict: null,
    external: null,
    trust: "repository",
    ...more,
  };
}

const inConflict = (n: number): Partial<ImportMemoryView> => ({
  bulk: false,
  held: "conflict",
  conflict: n,
  state: "conflict",
});

// 1 · Test command: three files disagree.
const tests = [
  memory("Run tests with `pnpm test`.", ["CLAUDE.md:12"], inConflict(1)),
  memory(
    "Run `npm run test` before committing.",
    ["AGENTS.md:8"],
    inConflict(1),
  ),
  memory(
    "Use `pnpm vitest run` for unit tests.",
    [".cursor/rules/testing.mdc:2"],
    inConflict(1),
  ),
];
// 2 · Package manager: two, and a statement that holds for both.
const packages = [
  memory(
    "pnpm workspaces only. Never `npm install` at the root.",
    ["CLAUDE.md:4"],
    inConflict(2),
  ),
  memory("Use npm for the scripts in `/tools`.", [`${CODEX}/tools.md:2`], {
    ...inConflict(2),
    trust: "agent_own_work",
  }),
];
// 3 · Where the API deploys.
const deploys = [
  memory(
    "The API runs on Fly.io in iad and ams.",
    ["AGENTS.md:21"],
    inConflict(3),
  ),
  memory(
    "Deploy previews to Railway.",
    [".cursor/rules/deploy.mdc:3"],
    inConflict(3),
  ),
];
const quarantined = memory(
  "The MCP spec requires OAuth 2.1 for remote servers.",
  [`${CODEX}/mcp.md:4`],
  {
    bulk: false,
    held: "quarantined",
    external: "modelcontextprotocol.io",
    trust: "external",
  },
);

// What agrees: 25 proposals, the most files agreeing first.
const agreeing = [
  memory("Background jobs use River.", [
    "CLAUDE.md:27",
    "AGENTS.md:5",
    `${CODEX}/stack.md:1`,
  ]),
  memory("Use TypeScript's strict mode everywhere.", [
    "CLAUDE.md:18",
    "AGENTS.md:7",
    ".cursor/rules/style.mdc:2",
  ]),
  memory("Run `pnpm format` and `pnpm lint` before every commit.", [
    "CLAUDE.md:20",
    "AGENTS.md:9",
    ".cursor/rules/testing.mdc:1",
  ]),
  memory("API errors are RFC 9457 problem+json.", [
    "CLAUDE.md:22",
    "AGENTS.md:12",
  ]),
  memory("Go services live in `packages/server`.", ["CLAUDE.md:9"]),
  memory("The web app is Next.js, deployed on Vercel.", ["CLAUDE.md:31"]),
  memory("Prefer named exports in React components.", [
    ".cursor/rules/style.mdc:1",
  ]),
  memory("Commit messages are short and imperative.", ["AGENTS.md:14"]),
  memory("Workers deploy from main only, after the parity check passes.", [
    "AGENTS.md:19",
  ]),
  memory(
    "Never commit `.env` files; secrets live in the vault.",
    [`${CODEX}/rules.md:2`],
    { trust: "agent_own_work" },
  ),
  memory("This repository is the Memax monorepo, built with Turborepo.", [
    "CLAUDE.md:2",
  ]),
  memory("Read AGENTS.md before changing anything in `packages/`.", [
    "CLAUDE.md:3",
  ]),
  memory("Tests sit next to the code they test, as `*.test.ts`.", [
    "CLAUDE.md:6",
  ]),
  memory("Use table-driven tests in Go.", ["CLAUDE.md:7"]),
  memory("Every API response uses the `ApiResponse` envelope.", [
    "CLAUDE.md:8",
  ]),
  memory("Create migrations with `pnpm migrate:new`, never by hand.", [
    "CLAUDE.md:10",
  ]),
  memory("Prefer small, focused functions over large ones.", ["CLAUDE.md:14"]),
  memory(
    "Every string in the web app goes through i18n, in English and Chinese.",
    ["CLAUDE.md:16"],
  ),
  memory("Never log memory text or tokens.", ["CLAUDE.md:24"]),
  memory("The CLI's binary is `memax`.", ["CLAUDE.md:29"]),
  memory("Open one pull request per change.", ["AGENTS.md:3"]),
  memory("Preview deploys expire after 7 days.", [
    ".cursor/rules/deploy.mdc:5",
  ]),
  memory("Validate request bodies with `zod`.", [`${CODEX}/stack.md:3`], {
    trust: "agent_own_work",
  }),
  memory("Log with `slog`, never `fmt.Println`.", [`${CODEX}/stack.md:5`], {
    trust: "agent_own_work",
  }),
  memory("Feature flags live in PostHog.", [`${CODEX}/stack.md:7`], {
    trust: "agent_own_work",
  }),
];

function member(m: ImportMemoryView) {
  return {
    id: m.id,
    ref: m.ref,
    statement: m.statement,
    refs: m.refs,
    agent: m.agent,
  };
}

const conflicts: ImportConflictView[] = [
  {
    id: "0192a7c0-0000-7000-8000-0000000001c1",
    n: 1,
    subject: "Test command",
    rationale: "Each file names a different command for running the tests.",
    suggestion: null,
    members: tests.map(member),
    state: "open",
    choice: null,
    chosen: null,
  },
  {
    id: "0192a7c0-0000-7000-8000-0000000001c2",
    n: 2,
    subject: "Package manager",
    rationale: "One file says pnpm only; Codex's notes use npm in /tools.",
    suggestion: "Keep both, scoped: pnpm at the root, npm inside `/tools`.",
    members: packages.map(member),
    state: "open",
    choice: null,
    chosen: null,
  },
  {
    id: "0192a7c0-0000-7000-8000-0000000001c3",
    n: 3,
    subject: "Where the API deploys",
    rationale: "The files name different hosts for deploys.",
    suggestion: null,
    members: deploys.map(member),
    state: "open",
    choice: null,
    chosen: null,
  },
];

/** The import as the boards draw it, before anything is settled. */
export function demoImportView(): ImportView {
  return {
    summary: structuredClone(DEMO_IMPORT_SUMMARY),
    memories: structuredClone([
      ...tests,
      ...packages,
      ...deploys,
      quarantined,
      ...agreeing,
    ]),
    conflicts: structuredClone(conflicts),
    progress: {
      proposals: 33,
      working: 0,
      judged: 33,
      failed: 0,
      ready: true,
    },
  };
}
