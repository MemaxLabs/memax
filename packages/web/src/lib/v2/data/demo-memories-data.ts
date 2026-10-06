import type { MemoryListItem, MemoryRecord } from "./memories";
import type { Actor } from "./records";
import {
  agent,
  at,
  DEMO_FOLD,
  DREAM,
  JY,
  M0071,
  M0102,
  M0156,
  M0174,
  V2_REACHES,
  ZZ,
} from "./demo-review-data";

const MEMAX: Actor = { kind: "memax" };

/**
 * The demo's Memories and Memory records (Memories.png, Memory.png), on
 * the same dataset as Review's (demo-review-data.ts). Data, not copy:
 * it stays in English like the boards' own content.
 */

const row = (
  fields: Partial<MemoryListItem> &
    Pick<MemoryListItem, "ref" | "statement" | "section">,
): MemoryListItem => ({
  state: "kept",
  receipt: null,
  source: null,
  note: null,
  forgotten: null,
  ...fields,
});
const kept = (by: Actor, when: string) => ({
  receipt: { by, action: "kept" as const, at: when },
});

/** Memories.png's rows, newest-first pages as the demo serves them. */
export const DEMO_MEMORY_PAGES: Record<string, MemoryListItem[][]> = {
  "memax-v2": [
    [
      row({
        ref: "M-0219",
        statement:
          "Background jobs run on River, Postgres‑backed. We do not use Temporal.",
        section: "decisions",
        ...kept(ZZ, at("10:58", "2026-10-02")),
        source: "PR #212",
      }),
      row({
        ref: "M-0431",
        statement: "Deploy the v2 API to Fly.io in iad and ams.",
        section: "decisions",
        state: "conflict",
        receipt: { by: agent("codex"), action: "proposed", at: at("14:26") },
        note: {
          kind: "conflict",
          with: "M-0174",
          keptBy: JY,
          askedBy: "codex",
        },
      }),
      row({
        ref: "M-0174",
        statement: M0174.statement,
        section: "decisions",
        ...kept(JY, M0174.at),
      }),
      row({
        ref: "M-0102",
        statement:
          "Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026‑07‑28.",
        section: "decisions",
        ...kept(ZZ, M0102.at),
        source: "session 3e1a",
      }),
      row({
        ref: "M-0071",
        statement: M0071.statement,
        section: "conventions",
        ...kept(JY, M0071.at),
      }),
      row({
        ref: "M-0430",
        statement:
          "MCP write tools must ask for confirmation with input_required.",
        section: "conventions",
        state: "proposed",
        receipt: {
          by: agent("claude-code"),
          action: "proposed",
          at: at("13:40"),
        },
      }),
      row({
        ref: "M-0187",
        statement: "Ask memax answers with the Haiku tier.",
        section: "conventions",
        state: "stale",
        receipt: { by: DREAM, action: "flagged", at: at("03:12") },
        note: {
          kind: "stale",
          changedAt: at("16:00", "2026-09-11"),
          source: "PR #198",
        },
      }),
      row({
        ref: "M-0098",
        statement: "API errors are RFC 9457 problem+json, never bare strings.",
        section: "conventions",
        ...kept(JY, at("12:40", "2026-09-12")),
        source: "PR #188",
      }),
      row({
        ref: "N-1187",
        statement: "River handles retries; don't add a second queue.",
        section: "conventions",
        state: "merged",
        receipt: { by: agent("codex"), action: "merged", at: at("03:12") },
        note: { kind: "merged", into: "M-0219", by: DREAM },
      }),
      row({
        ref: "M-0044",
        statement: "Use tabs, not spaces, in generated Go files.",
        section: "preferences",
        state: "faded",
        receipt: {
          by: agent("cursor"),
          action: "faded",
          at: at("03:12", "2026-08-06"),
        },
        note: { kind: "faded", days: 60 },
      }),
      row({
        ref: "M-0201",
        statement: "",
        section: "preferences",
        state: "forgotten",
        forgotten: {
          at: at("09:14", "2026-10-03"),
          by: null,
          detail: "removed from 4 files and 5 agents",
        },
      }),
    ],
    [
      row({
        ref: M0156.ref,
        statement: M0156.statement,
        section: "conventions",
        ...kept(M0156.by, M0156.at),
        source: "session 2b7d",
      }),
      row({
        ref: "M-0096",
        statement:
          "Every /v2 command takes an Idempotency-Key; edits take If-Match.",
        section: "decisions",
        ...kept(ZZ, at("17:22", "2026-09-10")),
        source: "PR #176",
      }),
      row({
        ref: "M-0088",
        statement:
          "Display IDs are per-tenant counters; internal keys are uuidv7.",
        section: "decisions",
        ...kept(JY, at("09:48", "2026-09-04")),
      }),
      row({
        ref: "M-0079",
        statement: "Colours come from ledger tokens, never literals.",
        section: "conventions",
        ...kept(ZZ, at("14:05", "2026-09-01")),
        source: "PR #161",
      }),
      row({
        ref: "M-0063",
        statement: "Table-driven tests for every Go handler.",
        section: "conventions",
        ...kept(JY, at("10:15", "2026-08-22")),
      }),
      row({
        ref: "M-0058",
        statement: "Run pnpm format and pnpm lint before every commit.",
        section: "conventions",
        ...kept(ZZ, at("18:40", "2026-08-19")),
      }),
      row({
        ref: "M-0041",
        statement: "Prefers small pull requests with a test plan.",
        section: "preferences",
        ...kept(ZZ, at("08:55", "2026-08-02")),
      }),
      row({
        ref: "M-0037",
        statement: "Short commit subjects in the imperative mood.",
        section: "preferences",
        ...kept(JY, at("12:10", "2026-07-30")),
      }),
      row({
        ref: "M-0432",
        statement: "Pin shared dependency versions with the pnpm catalog.",
        section: "conventions",
        state: "proposed",
        receipt: { by: agent("codex"), action: "proposed", at: at("14:18") },
      }),
      row({
        ref: "M-0405",
        statement: "Does the CLI daemon need its own space token?",
        section: "open_question",
        ...kept(ZZ, at("16:30", "2026-10-04")),
      }),
    ],
  ],
  personal: [
    [
      row({
        ref: "M-0433",
        statement: "Prefers diffs over prose summaries in code review.",
        section: "preferences",
        state: "proposed",
        receipt: { by: agent("chatgpt"), action: "proposed", at: at("11:40") },
      }),
      row({
        ref: "M-0012",
        statement: "Reply in English unless the thread is in Chinese.",
        section: "preferences",
        ...kept(ZZ, at("21:05", "2026-07-12")),
      }),
      row({
        ref: "M-0019",
        statement: "Explain a diff before proposing a rewrite.",
        section: "preferences",
        ...kept(ZZ, at("09:30", "2026-08-03")),
      }),
      row({
        ref: "M-0027",
        statement: "Use the gh CLI for GitHub, never raw API calls.",
        section: "conventions",
        ...kept(ZZ, at("17:45", "2026-09-02")),
      }),
    ],
  ],
  "memax-team": [
    [
      row({
        ref: "M-0444",
        statement:
          "The web app reads feature flags from the API, never from env vars.",
        section: "conventions",
        state: "proposed",
        receipt: {
          by: agent("claude-code"),
          action: "proposed",
          at: at("14:35"),
        },
      }),
      row({
        ref: "M-0445",
        statement: "Release notes go out on Thursdays, after the staging soak.",
        section: "decisions",
        state: "proposed",
        receipt: { by: agent("codex"), action: "proposed", at: at("13:20") },
      }),
      row({
        ref: "M-0310",
        statement: "Reviews need one member, never the author.",
        section: "decisions",
        ...kept(JY, at("10:20", "2026-09-22")),
      }),
      row({
        ref: DEMO_FOLD.item.ref,
        statement: DEMO_FOLD.item.statement,
        section: "decisions",
        state: "merged",
        receipt: { by: MEMAX, action: "merged", at: DEMO_FOLD.at },
        note: { kind: "merged", into: DEMO_FOLD.into, by: MEMAX },
      }),
    ],
  ],
};

/** The fold's Undo: the judge's folds can be undone for 14 days. */
const FOLD_UNDO = {
  receipt: DEMO_FOLD.receipt,
  until: new Date(Date.parse(DEMO_FOLD.at) + 14 * 86_400_000).toISOString(),
};

/** Memories.png's section sizes, across the whole record. */
export const DEMO_SECTION_COUNTS: Record<string, Record<string, number>> = {
  "memax-v2": { decisions: 38, conventions: 61, preferences: 12 },
};

/** Memories.png's "All 222": 214 kept, 5 waiting and 3 forgotten. */
export const DEMO_TOTALS: Record<string, number> = { "memax-v2": 222 };

/** Memory.png: M-0219 under its seal; and the team space's fold, both ways. */
export const DEMO_RECORDS: Record<string, Partial<MemoryRecord>> = {
  [DEMO_FOLD.item.ref]: {
    lifecycle: "merged",
    lineage: [
      {
        key: "f1",
        action: "proposed",
        by: DEMO_FOLD.item.by,
        at: DEMO_FOLD.item.at,
        detail: null,
        count: null,
        to: null,
      },
      {
        key: "f2",
        action: "merged",
        by: MEMAX,
        at: DEMO_FOLD.at,
        detail: `A near-verbatim repeat of ${DEMO_FOLD.into}.`,
        count: null,
        to: null,
        into: DEMO_FOLD.into,
        undo: FOLD_UNDO,
      },
    ],
  },
  [DEMO_FOLD.into]: {
    merged: {
      total: 1,
      notes: [
        {
          ref: DEMO_FOLD.item.ref,
          statement: DEMO_FOLD.item.statement,
          by: DEMO_FOLD.item.by,
          at: DEMO_FOLD.at,
          undo: FOLD_UNDO,
        },
      ],
    },
  },
  "M-0219": {
    kept: { by: ZZ, at: at("10:58", "2026-10-02") },
    reads: 214,
    reach: { files: 4, agents: 5 },
    lineage: [
      {
        key: "r1",
        action: "proposed",
        by: agent("claude-code"),
        at: at("10:41", "2026-10-02"),
        detail: "While moving the dream workers in session 3e1a.",
        count: null,
        to: null,
      },
      {
        key: "r2",
        action: "kept",
        by: ZZ,
        at: at("10:58", "2026-10-02"),
        detail: "Edited “prefer River” to “River, not Temporal”.",
        count: null,
        to: null,
      },
      {
        key: "r3",
        action: "merged",
        by: DREAM,
        at: at("03:12"),
        detail: "Notes from Codex, Cursor and two chats said the same thing.",
        count: 9,
        to: null,
      },
      {
        key: "r4",
        action: "handed_off",
        by: agent("codex"),
        at: at("14:20"),
        detail: "Part of the MCP migration handoff.",
        count: null,
        to: { agent: "codex", ref: "H-0093" },
      },
      {
        key: "r5",
        action: "verified",
        by: agent("claude-code"),
        at: at("14:31"),
        detail: "PR #212 is merged and River is in go.mod. Still true.",
        count: null,
        to: null,
      },
    ],
    merged: {
      total: 9,
      notes: [
        ["N-1187", "River handles retries; don't add a second queue.", "codex"],
        ["N-1203", "Jobs live in packages/server/internal/queue.", "cursor"],
        [
          "N-1190",
          "Dream workers enqueue through River, not goroutines.",
          "codex",
        ],
        ["N-1192", "Temporal needs a second cluster to run.", "chatgpt"],
        [
          "N-1195",
          "River jobs commit in the same transaction as their rows.",
          "claude-code",
        ],
        [
          "N-1198",
          "Use River's unique jobs for the nightly Dream run.",
          "cursor",
        ],
        ["N-1199", "Retries back off with River's default policy.", "codex"],
        [
          "N-1201",
          "The worker process is the only River client that works jobs.",
          "claude",
        ],
        ["N-1202", "Queue tables live in the public schema.", "codex"],
      ].map(([ref, statement, by]) => ({
        ref: ref!,
        statement: statement!,
        by: agent(by!),
        at: at("03:12"),
      })),
    },
    sources: [
      {
        key: "s1",
        kind: "pr",
        label: "PR #212 · Move jobs to River",
        provider: "GitHub",
        url: "https://github.com/MemaxLabs/memax/pull/212",
      },
      {
        key: "s2",
        kind: "session",
        label: "Session 3e1a, lines 212–260",
        provider: "Claude Code",
        url: null,
      },
      {
        key: "s3",
        kind: "file",
        label: "docs/adr/004-queues.md",
        // A file in the repository: the catalogue words its kind.
        provider: null,
        url: null,
      },
    ],
    reaches: V2_REACHES,
    conditions: [
      { key: "c1", code: "github.com/riverqueue/river", text: "is in go.mod" },
      { key: "c2", code: null, text: "ADR 004 is unchanged" },
    ],
    checked: { at: at("14:31"), by: agent("claude-code") },
  },
};
