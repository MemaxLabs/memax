import type { ImportMemoryView, ImportView } from "./imports";
import type { SwitchPreviewView } from "./switch";
import type { SpaceSummary } from "./types";

/**
 * The demo's space still on V1 (Switch to V2, plan §10): acme-web, a V1
 * team hub with three people, 112 memories, a CLAUDE.md that synced, two
 * agents and four V1 Dream runs. After the switch, seven of the owner's
 * own memories wait in Review as the V1 import ("From V1"), each citing
 * its note (N-); the counts add up the way the server's would.
 */

export const DEMO_V1_SPACE: SpaceSummary = {
  id: "0192a7c0-0000-7000-8000-000000000005",
  slug: "acme-web",
  name: "acme-web",
  kind: "team",
  role: "owner",
  kept: null,
  agents: null,
  people: 3,
  waiting: null,
  onV2: false,
};

export const DEMO_V1_IMPORT_ID = "0192a7c0-0000-7000-8000-0000000001b1";
const SWITCHED_AT = "2026-10-06T08:40:00-07:00";

export const DEMO_SWITCH_PREVIEW: SwitchPreviewView = {
  kind: "team",
  kinds: ["team", "project"],
  repository: null,
  suggestedRepository: "acme/web",
  members: [
    { name: "Ziyang Zeng", v1Role: "owner", role: "owner", canForget: true },
    { name: "Jiahao", v1Role: "admin", role: "member", canForget: true },
    { name: "Mina", v1Role: "viewer", role: "viewer", canForget: false },
  ],
  notes: {
    total: 112,
    candidates: 7,
    fold: 101,
    kept: 4,
    archived: 3,
    secret: 1,
  },
  personas: 0,
  configs: 1,
  targets: ["agents_md", "claude_md"],
  agents: [
    {
      name: "Claude Code",
      agent: "claude-code",
      autonomy: "propose",
      connected: false,
    },
    { name: "Cursor", agent: "cursor", autonomy: "read", connected: false },
  ],
  gates: 1,
  dreamRuns: 4,
  plan: "pro",
};

const STATEMENTS: Array<[ref: string, note: string, statement: string]> = [
  ["M-0601", "N-0001", "We use pnpm workspaces for every package."],
  ["M-0602", "N-0002", "Run pnpm format before every commit."],
  ["M-0603", "N-0004", "The staging database lives on Neon."],
  [
    "M-0604",
    "N-0007",
    "Feature flags live in the config service, not in env vars.",
  ],
  ["M-0605", "N-0009", "API errors follow RFC 9457 problem+json."],
  [
    "M-0606",
    "N-0012",
    "Vitest runs the unit tests and Playwright the end-to-end ones.",
  ],
  ["M-0607", "N-0015", "Deploys go out from main after CI passes."],
];

function memory(
  [ref, note, statement]: (typeof STATEMENTS)[number],
  i: number,
): ImportMemoryView {
  return {
    id: `0192a7c0-0000-7000-8000-0000000006${String(i + 1).padStart(2, "0")}`,
    ref,
    version: 1,
    statement,
    state: "proposed",
    outcome: "proposed",
    items: 1,
    refs: [note],
    files: [note],
    agent: null,
    // The last one the check hasn't reached yet.
    bulk: i < STATEMENTS.length - 1,
    held: i < STATEMENTS.length - 1 ? null : "checking",
    conflict: null,
    external: null,
    trust: "person",
  };
}

/** The V1 import the switch leaves in Review, before anything is kept. */
export function demoV1ImportView(): ImportView {
  return {
    summary: {
      id: DEMO_V1_IMPORT_ID,
      spaceId: DEMO_V1_SPACE.id,
      client: "Memax V1",
      createdAt: SWITCHED_AT,
      files: [],
      skipped: [],
      counts: {
        items: STATEMENTS.length,
        proposed: STATEMENTS.length,
        folded: 0,
        existing: 0,
        refused: 0,
        conflicts: 0,
      },
      check: "checked",
      origin: "v1",
    },
    memories: STATEMENTS.map(memory),
    conflicts: [],
    progress: {
      proposals: STATEMENTS.length,
      working: 1,
      judged: STATEMENTS.length - 1,
      failed: 0,
      ready: true,
    },
  };
}

export { SWITCHED_AT as DEMO_SWITCHED_AT };
