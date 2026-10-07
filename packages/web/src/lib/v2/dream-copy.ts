/**
 * The words of Dream's edition page (DreamEdition.png), built from an
 * edition (catalogue in, text out) so they're unit-tested in both
 * locales. The catalogue is `t.ledger.dream` (dream-en.ts, dream-zh.ts).
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import {
  count,
  formatClock,
  formatDayTitle,
  formatShortDate,
  joinList,
  joinSentences,
  spell,
} from "./copy";
import type { CommandFailure } from "./data/command-error";
import type {
  DreamActionView,
  DreamEditionView,
  NoteAuthors,
} from "./data/dream";
import type { Actor } from "./data/records";
import type { MemoryListItem } from "./data/memories";
import { actorName } from "./records-copy";
import { weekdayName } from "./today-copy";

export type DreamCopy = Translations["ledger"]["dream"];
type AppCopy = Translations["ledger"]["app"];
type RecordsCopy = Translations["ledger"]["records"];

export interface DreamWords {
  dc: DreamCopy;
  app: AppCopy;
  rc: RecordsCopy;
  locale: Locale;
  timeZone: string;
  agentName: (key: string) => string;
}

const notesText = (w: DreamWords, n: number) =>
  count(w.dc.notesOne, w.dc.notes, n);

/** "three agents and two chats", "one agent and you". */
export function whoText(w: DreamWords, by: readonly NoteAuthors[]): string {
  const { who } = w.dc;
  const agents = by.filter((a) => a.kind === "agent").length;
  const chats = by.filter((a) => a.kind === "chat");
  const sessions = chats.reduce((sum, c) => sum + (c.sessions ?? 0), 0);
  const parts: string[] = [];
  if (agents > 0) {
    parts.push(
      count(who.agentsOne, who.agents, agents, { n: spell(w.app, agents) }),
    );
  }
  if (chats.length > 0) {
    parts.push(
      sessions > 0
        ? count(who.chatsOne, who.chats, sessions, {
            n: spell(w.app, sessions),
          })
        : who.chatsSome,
    );
  }
  if (by.some((a) => a.kind === "person")) parts.push(who.you);
  return joinList(parts, w.locale);
}

/** "Monday, October 5, overnight", or "…, run at 14:20" for a run-now edition. */
export function headingText(w: DreamWords, e: DreamEditionView): string {
  const date = formatDayTitle(new Date(e.slot), w.timeZone, w.locale);
  return e.trigger === "manual"
    ? interpolate(w.dc.headingRun, {
        date,
        time: formatClock(e.slot, w.timeZone, w.locale),
      })
    : interpolate(w.dc.heading, { date });
}

/**
 * "Dream read 34 notes from three agents and two chats between 18:00 and
 * 03:00. …": the window is the record's since the edition before, so it
 * says the dates when it spans more than a night.
 */
export function ledeText(w: DreamWords, e: DreamEditionView): string {
  const { dc } = w;
  if (e.notes === 0) return dc.ledeNoNotes;
  const notes = notesText(w, e.notes);
  const who = whoText(w, e.notesBy);
  if (!e.since || !who) {
    return interpolate(dc.ledeFirst, { notes, who: who || "" });
  }
  const span = Date.parse(e.until) - Date.parse(e.since);
  if (span > 24 * 3_600_000) {
    return interpolate(dc.ledeSince, {
      notes,
      who,
      from: formatShortDate(new Date(e.since), w.timeZone, w.locale),
    });
  }
  return interpolate(dc.lede, {
    notes,
    who,
    from: formatClock(e.since, w.timeZone, w.locale),
    to: formatClock(e.until, w.timeZone, w.locale),
  });
}

/** Whether N- refs run without a gap ("N-1180 to N-1188" says them all). */
function contiguous(refs: readonly string[]): boolean {
  const ns = refs.map((r) => Number(/^N-0*(\d+)$/.exec(r)?.[1] ?? NaN));
  if (ns.some(Number.isNaN)) return false;
  return ns.every((n, i) => i === 0 || n === ns[i - 1]! + 1);
}

/** "Dream folded 9 notes into it: N-1180 to N-1188". */
export function foldNote(w: DreamWords, a: DreamActionView): string {
  const f = w.dc.folded;
  const n = a.notes.length;
  if (n === 1) return interpolate(f.noteOne, { ref: a.notes[0]! });
  if (n > 1 && contiguous(a.notes)) {
    return interpolate(f.note, {
      n,
      from: a.notes[0]!,
      to: a.notes[n - 1]!,
    });
  }
  return interpolate(f.notePlain, { n });
}

/**
 * Where a new fact came from and where it stands: "From 5 notes by Codex
 * and Cursor · waiting in Review", "From 6 notes by Claude Code, which
 * may write here".
 */
export function factNote(w: DreamWords, a: DreamActionView): string {
  const f = w.dc.facts;
  const notes = notesText(w, a.notes.length);
  const authors = a.noteAuthors ?? [];
  const agents = [
    ...new Set(
      authors.filter((x) => x.kind === "agent" && x.agent).map((x) => x.agent!),
    ),
  ];
  const chats = authors.filter((x) => x.kind === "chat");
  let from: string;
  if (agents.length > 0) {
    from = interpolate(f.by, {
      notes,
      who: joinList(agents.map(w.agentName), w.locale),
    });
  } else if (chats.length > 0) {
    from = interpolate(f.inChats, {
      notes,
      chats: whoText(w, chats),
    });
  } else {
    from = interpolate(f.plain, { notes });
  }
  const m = a.memory;
  if (m?.state === "proposed") return `${from} · ${f.waiting}`;
  if (m?.state === "kept" && m.receipt?.by?.kind === "agent") {
    return w.locale === "zh"
      ? `${from}，${f.mayWrite}`
      : `${from}, ${f.mayWrite}`;
  }
  return from;
}

/** "Contradicts M-0174, kept by Jiahao. Compare them side by side." */
export function conflictNote(
  w: DreamWords,
  other: { ref: string; keptBy: Actor | null } | null,
): string {
  const n = w.dc.needsYou;
  if (!other) return n.conflictPlain;
  if (!other.keptBy) return interpolate(n.conflictBare, { ref: other.ref });
  return interpolate(n.conflict, {
    ref: other.ref,
    name: actorName(other.keptBy, w.rc, w.agentName),
  });
}

/** "Its source changed on Sep 11 in PR #198. Verify it.", or its date passed. */
export function staleNote(w: DreamWords, m: MemoryListItem): string {
  const n = w.dc.needsYou;
  const note = m.note;
  if (note?.kind === "stale" && note.source && !/^D-\d+$/.test(note.source)) {
    return interpolate(n.staleSource, {
      date: formatShortDate(new Date(note.changedAt), w.timeZone, w.locale),
      source: note.source,
    });
  }
  return n.staleDate;
}

/** "No. 213 · Sun 4 Oct" in Earlier editions. */
export function issueLine(w: DreamWords, n: number, slot: string): string {
  const date = new Intl.DateTimeFormat(w.locale === "zh" ? "zh-CN" : "en-GB", {
    timeZone: w.timeZone,
    weekday: "short",
    day: "numeric",
    month: "short",
  }).format(new Date(slot));
  return interpolate(w.dc.side.issue, { n, date });
}

/** "12 notes became 3 facts." */
export function becameText(
  w: DreamWords,
  notes: number,
  facts: number,
): string {
  return interpolate(w.dc.side.became, {
    notes: notesText(w, notes),
    facts: count(w.dc.side.factsOne, w.dc.side.factsCount, facts),
  });
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

/** "Next edition Tuesday at 03:00." in the viewer's zone. */
export function nextText(
  w: DreamWords,
  nextAt: string,
  template: string = w.dc.side.next,
): string {
  const short = new Intl.DateTimeFormat("en-US", {
    timeZone: w.timeZone,
    weekday: "short",
  }).format(new Date(nextAt));
  const day = weekdayName(Math.max(0, WEEKDAYS.indexOf(short)), w.locale);
  return interpolate(template, {
    day,
    time: formatClock(nextAt, w.timeZone, w.locale),
  });
}

/** Why an undo of Dream's (or a restore) didn't go through, and what to do. */
export function dreamFailureText(
  w: DreamWords,
  failure: CommandFailure,
  ref: string,
): string {
  const r = w.dc.refused;
  switch (failure.kind) {
    case "undo-refused":
      return interpolate(r[failure.reason], { ref: failure.ref ?? ref });
    case "unreachable":
    case "busy":
      return r.unreachable;
    case "rate-limited":
      return r.runSoon;
    case "refused":
      return failure.code === "dream_run_by_owner" ? r.runOwner : r.failed;
    default:
      return r.failed;
  }
}

/** A run-now that didn't go: too soon, the plan's cap, or not the owner. */
export function runFailureText(w: DreamWords, failure: CommandFailure): string {
  const r = w.dc.refused;
  if (failure.kind === "rate-limited") {
    // Within a minute is "already on its way"; longer is the plan's cap.
    return failure.retryAfter !== null && failure.retryAfter > 90
      ? r.runCap
      : r.runSoon;
  }
  if (failure.kind === "refused" && failure.code === "dream_run_by_owner") {
    return r.runOwner;
  }
  return failure.kind === "unreachable" ? r.unreachable : r.failed;
}

/** The toast after Undo all or Restore all. */
export function undoAllText(
  w: DreamWords,
  restore: boolean,
  undone: number,
  refused: number,
): string {
  const d = w.dc.done;
  const done = restore
    ? count(d.restoredOne, d.restoredMany, undone)
    : count(d.undoneOne, d.undoneMany, undone);
  return joinSentences(
    [
      undone > 0 ? done : null,
      refused > 0 ? count(d.someRefusedOne, d.someRefused, refused) : null,
    ],
    w.locale,
  );
}
