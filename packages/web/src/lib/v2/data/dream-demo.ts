import { CommandFailedError } from "./command-error";
import { DREAM, JY, ZZ, agent } from "./demo-review-data";
import {
  NO_COUNTS,
  editionNumberOf,
  type DreamActionKind,
  type DreamActionView,
  type DreamEditionSummary,
  type DreamEditionView,
  type DreamEditionsPage,
  type DreamSettingsView,
  type DreamSource,
} from "./dream";
import type { MemoryListItem } from "./memories";
import type { Actor, DisplayState } from "./records";
import type { Section } from "./types";

/**
 * The demo's Dream (DreamEdition.png): edition No. 214 of memax-v2, read
 * from 34 notes between 18:00 and 03:00 and published at 03:12, with
 * the three editions before it. Undo, Restore and the settings change
 * this session's copy. Data, not copy.
 */

const NIGHT = "2026-10-05";
const on = (date: string, time: string) => `${date}T${time}:00-07:00`;

function row(
  ref: string,
  statement: string,
  state: DisplayState,
  by: Actor,
  action: "kept" | "proposed" | "flagged" | "faded",
  at: string,
  section: Section = "conventions",
): MemoryListItem {
  return {
    ref,
    statement,
    section,
    state,
    receipt: { by, action, at },
    source: null,
    note: null,
    forgotten: null,
  };
}

const notes = (from: number, n: number) =>
  Array.from({ length: n }, (_, i) => `N-${from + i}`);

let nextId = 0;
function action(
  kind: DreamActionKind,
  memory: MemoryListItem | null,
  extra: Partial<DreamActionView> = {},
): DreamActionView {
  nextId += 1;
  return {
    id: `dream-action-${nextId}`,
    n: nextId,
    kind,
    memory,
    version: memory ? 1 : null,
    related: null,
    notes: [],
    noteAuthors: null,
    brief: null,
    undone: null,
    undoable: true,
    ...extra,
  };
}

const FADED: Array<[string, string, Actor]> = [
  ["M-0044", "Use tabs, not spaces, in generated Go files.", agent("cursor")],
  [
    "M-0039",
    "The staging database is reset every Sunday night.",
    agent("codex"),
  ],
  [
    "M-0031",
    "Old release notes live in the wiki, not the repository.",
    agent("codex"),
  ],
  ["M-0052", "The web app's storybook runs on port 6006.", agent("cursor")],
  ["M-0058", "Use pnpm 8 for the docs site.", agent("claude-code")],
  [
    "M-0063",
    "Lint warnings in packages/ui are allowed for now.",
    agent("cursor"),
  ],
  ["M-0067", "The CLI's beta channel is published on Fridays.", agent("codex")],
  ["M-0071", "Benchmarks run on the m5 runner.", agent("claude-code")],
  ["M-0076", "Feature flags live in LaunchDarkly.", agent("codex")],
  ["M-0080", "The old search page is behind /classic.", agent("cursor")],
  ["M-0083", "Screenshots in the docs are 2x PNGs.", agent("claude-code")],
];

function edition214(): DreamEditionView {
  nextId = 0;
  const at = on(NIGHT, "03:12");
  const actions: DreamActionView[] = [
    action(
      "fold",
      row(
        "M-0219",
        "Background jobs run on River, not Temporal.",
        "kept",
        ZZ,
        "kept",
        on("2026-10-02", "10:41"),
        "decisions",
      ),
      {
        notes: notes(1180, 9),
        noteAuthors: [{ kind: "agent", agent: "claude-code", count: 9 }],
      },
    ),
    action(
      "fold",
      row(
        "M-0102",
        "Remote MCP is stateless streamable HTTP with OAuth 2.1.",
        "kept",
        ZZ,
        "kept",
        on("2026-09-30", "16:05"),
        "conventions",
      ),
      {
        notes: ["N-1190", "N-1193", "N-1197", "N-1201"],
        noteAuthors: [{ kind: "agent", agent: "codex", count: 4 }],
      },
    ),
    action(
      "propose",
      row(
        "M-0434",
        "Review cards show the diff against the memory they replace.",
        "proposed",
        DREAM,
        "proposed",
        at,
      ),
      {
        notes: ["N-1189", "N-1191", "N-1194", "N-1198", "N-1202"],
        noteAuthors: [
          { kind: "agent", agent: "codex", count: 3 },
          { kind: "agent", agent: "cursor", count: 2 },
        ],
      },
    ),
    action(
      "propose",
      row(
        "M-0435",
        "The CLI prints receipts in the same order as the web app.",
        "proposed",
        DREAM,
        "proposed",
        at,
      ),
      {
        notes: ["N-1192", "N-1195", "N-1199"],
        noteAuthors: [{ kind: "agent", agent: "claude-code", count: 3 }],
      },
    ),
    action(
      "propose",
      row(
        "M-0437",
        "Prefers short commit messages in the imperative.",
        "proposed",
        DREAM,
        "proposed",
        at,
        "preferences",
      ),
      {
        notes: ["N-1204", "N-1207", "N-1210", "N-1212"],
        noteAuthors: [{ kind: "chat", agent: null, count: 4, sessions: 2 }],
        space: "Personal",
      },
    ),
    action(
      "propose",
      row(
        "M-0436",
        "Compile adapters live in packages/compile, one per target.",
        "kept",
        agent("claude-code"),
        "kept",
        at,
        "conventions",
      ),
      {
        notes: ["N-1196", "N-1200", "N-1203", "N-1205", "N-1206", "N-1208"],
        noteAuthors: [{ kind: "agent", agent: "claude-code", count: 6 }],
      },
    ),
    action("stale", {
      ...row(
        "M-0187",
        "Ask memax answers with the Haiku tier.",
        "stale",
        DREAM,
        "flagged",
        at,
      ),
      note: {
        kind: "stale",
        changedAt: on("2026-09-11", "09:20"),
        source: "PR #198",
      },
    }),
    ...FADED.map(([ref, statement, by]) =>
      action("fade", row(ref, statement, "faded", by, "faded", at)),
    ),
  ];
  return {
    ref: "D-0214",
    n: 214,
    slot: on(NIGHT, "03:00"),
    trigger: "schedule",
    since: on("2026-10-04", "18:00"),
    until: on(NIGHT, "03:00"),
    finishedAt: at,
    seconds: 41,
    notes: 34,
    notesBy: [
      { kind: "agent", agent: "claude-code", count: 14 },
      { kind: "agent", agent: "codex", count: 11 },
      { kind: "agent", agent: "cursor", count: 6 },
      { kind: "chat", agent: null, count: 3, sessions: 2 },
    ],
    noteIds: notes(1180, 34),
    factIds: ["M-0219", "M-0102", "M-0434", "M-0435", "M-0436", "M-0437"],
    counts: { ...NO_COUNTS, fold: 2, propose: 4, stale: 1, fade: FADED.length },
    undone: 0,
    needsYou: 2,
    actions,
    surfaced: [
      {
        memory: row(
          "M-0431",
          `Deploy the v2 API to Fly.io in iad and ams.`,
          "conflict",
          agent("codex"),
          "proposed",
          "2026-10-05T14:26:00-07:00",
          "decisions",
        ),
        with: { ref: "M-0174", keptBy: JY },
      },
    ],
  };
}

/** The editions before No. 214, as Earlier editions lists them. */
const EARLIER: DreamEditionSummary[] = [
  {
    ref: "D-0213",
    n: 213,
    slot: on("2026-10-04", "03:00"),
    finishedAt: on("2026-10-04", "03:08"),
    notes: 12,
    facts: 3,
  },
  {
    ref: "D-0212",
    n: 212,
    slot: on("2026-10-03", "03:00"),
    finishedAt: on("2026-10-03", "03:05"),
    notes: 9,
    facts: 2,
  },
  {
    ref: "D-0211",
    n: 211,
    slot: on("2026-10-02", "03:00"),
    finishedAt: on("2026-10-02", "03:14"),
    notes: 27,
    facts: 5,
  },
];

/** An earlier edition's page: its counts, its actions long since settled. */
function earlier(s: DreamEditionSummary): DreamEditionView {
  const base = 1180 - (214 - s.n) * 40;
  return {
    ref: s.ref,
    n: s.n,
    slot: s.slot,
    trigger: "schedule",
    since: new Date(Date.parse(s.slot) - 9 * 3_600_000).toISOString(),
    until: s.slot,
    finishedAt: s.finishedAt,
    seconds: 30,
    notes: s.notes,
    notesBy: [{ kind: "agent", agent: "claude-code", count: s.notes }],
    noteIds: notes(base, s.notes),
    factIds: [],
    counts: NO_COUNTS,
    undone: 0,
    needsYou: 0,
    actions: [],
    surfaced: [],
  };
}

const NEXT_AT = on("2026-10-06", "03:00");

export function createDemoDream({
  commandDelayMs = 0,
}: { commandDelayMs?: number } = {}): DreamSource {
  // This session's copy: undo and restore change it.
  const editions = new Map<string, DreamEditionView>();
  editions.set("memax-v2", edition214());
  let settings: DreamSettingsView = {
    timeZone: "America/Vancouver",
    timeZoneSource: "observed",
    morningEmail: true,
  };
  const wait = () =>
    commandDelayMs > 0
      ? new Promise((r) => setTimeout(r, commandDelayMs))
      : Promise.resolve();

  const page = (slug: string): DreamEditionsPage => {
    const latest = editions.get(slug);
    if (!latest) return { items: [], hasMore: false, schedule: null };
    return {
      items: [
        {
          ref: latest.ref,
          n: latest.n,
          slot: latest.slot,
          finishedAt: latest.finishedAt,
          notes: latest.notes,
          facts: latest.factIds.length,
        },
        ...EARLIER,
      ],
      hasMore: true,
      schedule: {
        cadence: "nightly",
        timeZone: settings.timeZone,
        nextAt: NEXT_AT,
      },
    };
  };
  const find = (slug: string, ref: string): DreamEditionView | null => {
    const latest = editions.get(slug);
    if (!latest) return null;
    if (ref === "latest") return latest;
    const n = editionNumberOf(ref);
    if (n === latest.n) return latest;
    const s = EARLIER.find((e) => e.n === n);
    return s ? earlier(s) : null;
  };
  /** Puts a changed action (and its memory) back in the edition. */
  const settle = (slug: string, id: string, memoryState?: DisplayState) => {
    const e = editions.get(slug);
    if (!e) return;
    const now = new Date().toISOString();
    editions.set(slug, {
      ...e,
      undone: e.undone + 1,
      actions: e.actions.map((a) =>
        a.id !== id
          ? a
          : {
              ...a,
              undone: { at: now },
              undoable: false,
              memory:
                a.memory && memoryState
                  ? { ...a.memory, state: memoryState }
                  : a.memory,
            },
      ),
    });
  };
  /** What an undo puts back: a proposal is withdrawn, a fade or a flag lifted. */
  const undoneState = (a: DreamActionView): DisplayState | undefined =>
    a.kind === "fade" || a.kind === "stale" || a.kind === "conflict"
      ? "kept"
      : undefined;

  return {
    peekEditions: page,
    peekEdition: find,
    peekSettings: () => settings,
    async editions({ space }) {
      return page(space.slug);
    },
    async edition({ space, ref }) {
      return find(space.slug, ref);
    },
    async undo({ space, action: a }) {
      await wait();
      const current = editions
        .get(space.slug)
        ?.actions.find((x) => x.id === a.id);
      if (!current) throw new CommandFailedError({ kind: "not-found" });
      if (current.undone) {
        throw new CommandFailedError({
          kind: "undo-refused",
          reason: "already_undone",
          ref: current.memory?.ref ?? null,
        });
      }
      settle(space.slug, a.id, undoneState(current));
    },
    async undoAll({ space, kind }) {
      await wait();
      const e = editions.get(space.slug);
      const these = (e?.actions ?? []).filter(
        (a) => a.kind === kind && !a.undone,
      );
      for (const a of these) settle(space.slug, a.id, undoneState(a));
      return { undone: these.length, refused: [] };
    },
    async restore({ space, ref }) {
      await wait();
      const a = editions
        .get(space.slug)
        ?.actions.find((x) => x.kind === "fade" && x.memory?.ref === ref);
      if (!a) throw new CommandFailedError({ kind: "not-found" });
      settle(space.slug, a.id, "kept");
    },
    async run() {
      await wait();
    },
    async settings() {
      return settings;
    },
    async updateSettings({ timeZone, morningEmail }) {
      await wait();
      settings = {
        timeZone: timeZone ?? settings.timeZone,
        timeZoneSource: timeZone ? "set" : settings.timeZoneSource,
        morningEmail: morningEmail ?? settings.morningEmail,
      };
      return settings;
    },
    async unsubscribe() {
      await wait();
      settings = { ...settings, morningEmail: false };
    },
  };
}
