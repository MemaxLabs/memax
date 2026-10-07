/**
 * Dream's editions (plan §5.10, epic 2.2; DreamEdition.png, the Dream
 * card on Main.png): each night Dream reads a space's new notes and
 * changes and publishes an edition (D-) of small actions, each one
 * undoable. Part of LedgerDataSource (source.ts) as `source.dream`;
 * dream-sdk.ts (memax.v2.dream) and dream-demo.ts implement it.
 *
 * No words live here: statements are the record's own, and the page
 * words everything else from the catalogue (dream-en.ts, dream-zh.ts).
 */
import type { MemoryListItem } from "./memories";
import type { Actor } from "./records";
import type { DreamEdition, DreamLine, TodayDream } from "./today";
import type { SpaceSummary } from "./types";

/** What one of an edition's actions did (spec DreamActionKind). */
export type DreamActionKind =
  | "fold"
  | "propose"
  | "dedupe"
  | "conflict"
  | "stale"
  | "fade"
  | "brief";

export const DREAM_ACTION_KINDS: readonly DreamActionKind[] = [
  "fold",
  "propose",
  "dedupe",
  "conflict",
  "stale",
  "fade",
  "brief",
];

/** Who wrote an edition's notes: an agent (a registry key), a person, or chats. */
export interface NoteAuthors {
  kind: "agent" | "person" | "chat";
  agent: string | null;
  count: number;
  /** For chats: how many chats the notes came from, when the source knows. */
  sessions?: number;
}

/** One of an edition's actions, with its memory as it is now. */
export interface DreamActionView {
  id: string;
  /** Its place in the edition, from 1. */
  n: number;
  kind: DreamActionKind;
  /** The memory it changed or proposed, as Memories lists it; null for the Brief. */
  memory: MemoryListItem | null;
  /** That memory's version now, for Restore's If-Match. */
  version: number | null;
  /** The space it was proposed into, when that isn't this one (a personal preference). */
  space?: string;
  /** The proposal a duplicate folded into, or the memory a conflict is with. */
  related: { ref: string; keptBy: Actor | null } | null;
  /** The notes it rests on (fold, propose), oldest first. */
  notes: string[];
  /** Who wrote them, when the source knows (the demo does; /v2 counts per edition). */
  noteAuthors: NoteAuthors[] | null;
  /** A Brief action: the version it wrote, and how many small changes. */
  brief: { ref: string | null; ops: number } | null;
  /** When a person undid it; null while it stands. */
  undone: { at: string } | null;
  /** Whether Undo may still apply (a later change can still refuse it). */
  undoable: boolean;
}

/** Something the edition lists because it needs a person, though Dream didn't do it. */
export interface DreamSurfacedView {
  memory: MemoryListItem;
  with: { ref: string; keptBy: Actor | null } | null;
}

export type DreamCounts = Record<DreamActionKind, number>;

/** An edition as its page shows it. */
export interface DreamEditionView {
  ref: string;
  n: number;
  /** The night it answers (or the run-now moment). */
  slot: string;
  trigger: "schedule" | "manual";
  /** The window of record changes it read: after `since` (null: the first edition), to `until`. */
  since: string | null;
  until: string;
  finishedAt: string;
  /** How long the run took. */
  seconds: number;
  notes: number;
  notesBy: NoteAuthors[];
  noteIds: string[];
  /** The memories the notes became: folded into, or proposed. */
  factIds: string[];
  counts: DreamCounts;
  undone: number;
  /** Conflicts and stale facts it flagged or found that still wait on a person. */
  needsYou: number;
  actions: DreamActionView[];
  surfaced: DreamSurfacedView[];
}

/** An edition in a list (Earlier editions). */
export interface DreamEditionSummary {
  ref: string;
  n: number;
  slot: string;
  finishedAt: string;
  notes: number;
  facts: number;
}

/** When the next edition is due, in the owner's zone. */
export interface DreamScheduleView {
  cadence: "nightly" | "weekly";
  timeZone: string;
  nextAt: string;
}

export interface DreamEditionsPage {
  items: DreamEditionSummary[];
  hasMore: boolean;
  /** Null until the sweep has scheduled the space. */
  schedule: DreamScheduleView | null;
}

/** A person's Dream settings: their zone (Dream runs in their night) and the morning email. */
export interface DreamSettingsView {
  timeZone: string;
  /** default: Memax doesn't know it yet (UTC); observed: from the app's clock; set: by them. */
  timeZoneSource: "default" | "observed" | "set";
  morningEmail: boolean;
}

/** What undoing every action of a kind did. */
export interface UndoAllResult {
  undone: number;
  /** The ones that couldn't be undone, by memory. */
  refused: { ref: string | null; reason: string }[];
}

export interface DreamSource {
  /** The demo answers at once, so screenshots never catch a loading frame. */
  peekEditions?(slug: string): DreamEditionsPage | undefined;
  peekEdition?(slug: string, ref: string): DreamEditionView | null | undefined;
  peekSettings?(): DreamSettingsView | undefined;
  /** The space's editions, newest first, and its schedule. */
  editions(input: {
    space: SpaceSummary;
    limit?: number;
    signal?: AbortSignal;
  }): Promise<DreamEditionsPage>;
  /** One edition ("latest", "D-0214" or "214"); null when there's none. */
  edition(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<DreamEditionView | null>;
  /** Undo one action; throws (command-error.ts toFailure reads it). */
  undo(input: {
    space: SpaceSummary;
    action: DreamActionView;
    idempotencyKey: string;
  }): Promise<void>;
  /** Undo every undoable action of a kind ("Undo both", "Restore all"). */
  undoAll(input: {
    space: SpaceSummary;
    edition: string;
    kind: DreamActionKind;
    idempotencyKey: string;
  }): Promise<UndoAllResult>;
  /** Bring a faded memory back. */
  restore(input: {
    space: SpaceSummary;
    ref: string;
    version: number | null;
    idempotencyKey: string;
  }): Promise<void>;
  /** Ask Dream to run on the space now (its owner; a few times a day). */
  run(input: { space: SpaceSummary; idempotencyKey: string }): Promise<void>;
  settings(input?: { signal?: AbortSignal }): Promise<DreamSettingsView>;
  updateSettings(input: {
    timeZone?: string;
    morningEmail?: boolean;
    idempotencyKey: string;
  }): Promise<DreamSettingsView>;
  /** Turn the morning email off with its link's token (no sign-in). */
  unsubscribe(input: { token: string }): Promise<void>;
}

export const NO_COUNTS: DreamCounts = {
  fold: 0,
  propose: 0,
  dedupe: 0,
  conflict: 0,
  stale: 0,
  fade: 0,
  brief: 0,
};

/** The actions of a kind that still stand. */
export function standing(
  edition: DreamEditionView,
  kind: DreamActionKind,
): DreamActionView[] {
  return edition.actions.filter((a) => a.kind === kind && !a.undone);
}

/** What waits on a person: conflicts still flagged (Dream's and the judge's), then stale facts. */
export function needsYouOf(edition: DreamEditionView): MemoryListItem[] {
  const out: MemoryListItem[] = [];
  const seen = new Set<string>();
  const add = (m: MemoryListItem | null) => {
    if (!m || seen.has(m.ref)) return;
    seen.add(m.ref);
    out.push(m);
  };
  for (const a of edition.actions) {
    if (a.undone || !a.memory) continue;
    if (a.kind === "conflict" && a.memory.state === "conflict") add(a.memory);
    if (a.kind === "stale" && a.memory.state === "stale") add(a.memory);
  }
  for (const s of edition.surfaced) {
    if (s.memory.state === "conflict") add(s.memory);
  }
  // Conflicts first (DreamEdition.png), then what to verify.
  return [
    ...out.filter((m) => m.state === "conflict"),
    ...out.filter((m) => m.state !== "conflict"),
  ];
}

/**
 * Main.png's three lines: one of each kind first (a conflict, the
 * biggest fold, what faded), then more of them.
 */
function pickLines(lines: DreamLine[]): DreamLine[] {
  const firsts = (["conflict", "merged", "faded"] as const).flatMap((kind) => {
    const first = lines.find((l) => l.kind === kind);
    return first ? [first] : [];
  });
  return [...firsts, ...lines.filter((l) => !firsts.includes(l))].slice(0, 3);
}

/** The edition as Today's card reads it (DreamCard's lines: conflicts first, at most three). */
export function cardOf(edition: DreamEditionView): DreamEdition {
  const lines: DreamLine[] = [];
  for (const m of needsYouOf(edition)) {
    if (m.state !== "conflict" || !m.statement) continue;
    lines.push({
      kind: "conflict",
      text: m.statement,
      meta: { kind: "needs-you" },
    });
  }
  const folds = standing(edition, "fold")
    .filter((a) => a.memory?.statement)
    .sort((a, b) => b.notes.length - a.notes.length);
  for (const a of folds) {
    lines.push({
      kind: "merged",
      text: a.memory!.statement,
      meta: { kind: "folded", notes: a.notes.length, into: a.memory!.ref },
    });
  }
  const faded = standing(edition, "fade").filter(
    (a) => a.memory?.state === "faded",
  ).length;
  if (faded > 0) {
    lines.push({
      kind: "faded",
      text: null,
      count: faded,
      meta: { kind: "restorable" },
    });
  }
  return {
    n: edition.n,
    ref: edition.ref,
    at: edition.finishedAt,
    seconds: edition.seconds,
    notes: edition.notes,
    facts: edition.factIds.length,
    noteIds: edition.noteIds,
    factIds: edition.factIds,
    lines: pickLines(lines),
  };
}

const PERIOD_MS: Record<DreamScheduleView["cadence"], number> = {
  nightly: 24 * 3_600_000,
  weekly: 7 * 24 * 3_600_000,
};

/**
 * Today's Dream: last night's edition, "no edition last night" when the
 * last scheduled night came and went with nothing new to read, or "no
 * edition yet". An edition counts as last night's when it finished after
 * the night before the next one (a run-now edition since then counts too).
 */
export function todayDreamOf(
  latest: DreamEditionView | null,
  schedule: DreamScheduleView | null,
  now: Date,
): TodayDream {
  if (!latest) return { kind: "unavailable" };
  const lastNight = schedule
    ? Date.parse(schedule.nextAt) - PERIOD_MS[schedule.cadence]
    : now.getTime() - 36 * 3_600_000;
  // A few hours' grace: an edition lands after its slot, and the sweep
  // computes the next slot when it queues a run.
  const since = Math.min(lastNight, now.getTime()) - 6 * 3_600_000;
  return Date.parse(latest.finishedAt) >= since
    ? { kind: "edition", edition: cardOf(latest) }
    : { kind: "quiet" };
}

/** "03:00" in the zone, and the weekday (0 Sunday) for a weekly cadence. */
export function dreamTimeOf(
  schedule: DreamScheduleView | null,
  timeZone: string,
): { at: string | null; weekday: number | null } {
  if (!schedule) return { at: null, weekday: null };
  const next = new Date(schedule.nextAt);
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone,
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
    weekday: "short",
  }).formatToParts(next);
  const get = (type: string) => parts.find((p) => p.type === type)?.value;
  const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  return {
    at: `${get("hour")}:${get("minute")}`,
    weekday:
      schedule.cadence === "weekly" ? days.indexOf(get("weekday") ?? "") : null,
  };
}

/** An edition's number from "D-0214", "214" or "latest" (null). */
export function editionNumberOf(ref: string): number | null {
  const m = /^(?:D-)?0*(\d+)$/i.exec(ref.trim());
  return m ? Number(m[1]) : null;
}
