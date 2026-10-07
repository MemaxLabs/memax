/**
 * Sentences the Brief and its targets build from data: the eyebrow's
 * "rewritten by Dream at 03:12", a fact's margin, the drift notice and
 * a compiled file's meta line. Pure (catalogue in, string out), so the
 * rules are unit-tested in both locales.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { count, formatClock, formatShortDate, type AppCopy } from "./copy";
import type { CommandFailure } from "./data/command-error";
import type { BriefRow } from "./data/brief";
import type { RecordsCopy } from "./records-copy";
import type { Actor } from "./data/records";
import {
  driftAgent,
  isShim,
  targetName,
  type DriftChangeView,
  type DriftItemView,
  type TargetView,
} from "./data/targets";

export type BriefCopy = Translations["ledger"]["brief"];

/** Whether two instants fall on the same day in a time zone. */
export function sameDay(a: Date, b: Date, timeZone: string): boolean {
  const day = (d: Date) =>
    new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(d);
  return day(a) === day(b);
}

interface When {
  now: Date;
  timeZone: string;
  locale: Locale;
}

/** "rewritten by Dream at 03:12", or "revised by you on Oct 4". */
export function revisedBy(
  app: AppCopy,
  b: BriefCopy,
  by: Actor | null,
  at: string,
  name: (actor: Actor | null) => string,
  { now, timeZone, locale }: When,
): string {
  const today = sameDay(new Date(at), now, timeZone);
  const time = formatClock(at, timeZone, locale);
  const date = formatShortDate(new Date(at), timeZone, locale);
  if (by?.kind === "dream") {
    return today
      ? interpolate(app.brief.rewritten, { time })
      : interpolate(b.page.rewrittenOn, { date });
  }
  const who = by?.kind === "person" && by.self ? b.page.you : name(by);
  return today
    ? interpolate(b.page.revisedToday, { name: who, time })
    : interpolate(b.page.revisedOn, { name: who, date });
}

/** The Brief's eyebrow: "Brief · memax-v2 · rewritten by Dream at 03:12 · 14 facts". */
export function briefEyebrow(
  app: AppCopy,
  b: BriefCopy,
  {
    space,
    by,
    at,
    facts,
    name,
    when,
  }: {
    space: string;
    by: Actor | null;
    at: string;
    facts: number;
    name: (actor: Actor | null) => string;
    when: When;
  },
): string {
  return [
    interpolate(app.brief.eyebrow, { space }),
    revisedBy(app, b, by, at, name, when),
    count(app.brief.factsOne, app.brief.facts, facts),
  ].join(" · ");
}

/**
 * A fact's second margin line after the ID: its source, "source
 * changed" when stale, "in Review" when waiting, and for prose the
 * memories it cites ("M-0431 ≠ M-0174" when they disagree).
 */
export function marginLine(
  b: BriefCopy,
  row: BriefRow,
): { ids: string; rest: string | null; conflict: boolean } {
  if (row.kind === "prose") {
    const conflict = row.state === "conflict" && row.cites.length === 2;
    return {
      ids: row.cites.join(conflict ? " ≠ " : ", "),
      rest: null,
      conflict,
    };
  }
  const rest =
    row.kind === "waiting"
      ? b.page.inReview
      : row.state === "stale"
        ? b.page.sourceChanged
        : row.source;
  return { ids: row.ref ?? "", rest, conflict: false };
}

/** When a hand edit happened, for a sentence: "on Oct 4" / "today at 16:40". */
function editedWhen(b: BriefCopy, at: string, { now, timeZone, locale }: When) {
  return sameDay(new Date(at), now, timeZone)
    ? interpolate(b.drift.today, { time: formatClock(at, timeZone, locale) })
    : interpolate(b.drift.on, {
        date: formatShortDate(new Date(at), timeZone, locale),
      });
}

/**
 * The Brief's drift notice (Brief.png): "Cursor's copy has one local
 * edit. Someone changed the rules file by hand on Oct 4. Memax never
 * overwrites a hand edit without asking."
 */
export function driftNotice(
  b: BriefCopy,
  target: TargetView,
  item: DriftItemView | undefined,
  agentName: (key: string) => string,
  when: When,
): { title: string; body: string } {
  const d = b.drift;
  const agent = driftAgent(target.kind);
  const edits = Math.max(1, target.openDrift);
  const file = targetName(target);
  const title = agent
    ? count(d.title, d.titleMany, edits, { agent: agentName(agent) })
    : count(d.titleFile, d.titleFileMany, edits, { file });
  const at = item?.observedAt;
  const how = at
    ? target.kind === "cursor_mdc" ||
      target.kind === "copilot" ||
      target.kind === "windsurf" ||
      target.kind === "claude_rules"
      ? interpolate(d.rules, { when: editedWhen(b, at, when) })
      : interpolate(d.file, { file, when: editedWhen(b, at, when) })
    : null;
  return {
    title,
    body: [how, d.never].filter(Boolean).join(when.locale === "zh" ? "" : " "),
  };
}

/** The lines a hand edit took out of Memax's file, and the ones it put in the file now. */
export function driftLines(changes: readonly DriftChangeView[]): {
  removed: Set<number>;
  added: Set<number>;
} {
  const removed = new Set<number>();
  const added = new Set<number>();
  for (const change of changes) {
    if (change.kind === "edit") {
      removed.add(change.oldLine);
      added.add(change.newLine);
    } else if (change.kind === "new") {
      added.add(change.line);
    } else {
      removed.add(change.oldLine);
    }
  }
  return { removed, added };
}

/** "The line", "Both lines", "All 3 lines" (DriftResolve.png's option details). */
export function linesWord(
  words: { one: string; two: string; many: string },
  n: number,
): string {
  if (n <= 1) return words.one;
  if (n === 2) return words.two;
  return interpolate(words.many, { n });
}

/** DriftResolve.png's title: "Cursor's rules file has a hand edit", or the file's name. */
export function driftTitle(
  b: BriefCopy,
  target: TargetView,
  agentName: (key: string) => string,
): string {
  const agent = driftAgent(target.kind);
  return agent && target.kind === "cursor_mdc"
    ? interpolate(b.resolve.titleRules, { agent: agentName(agent) })
    : interpolate(b.resolve.titleFile, { file: targetName(target) });
}

/** "on Oct 4 at 16:40", or "today at 16:40". */
export function driftWhen(b: BriefCopy, at: string, when: When): string {
  const time = formatClock(at, when.timeZone, when.locale);
  return sameDay(new Date(at), when.now, when.timeZone)
    ? interpolate(b.resolve.today, { time })
    : interpolate(b.resolve.on, {
        date: formatShortDate(new Date(at), when.timeZone, when.locale),
        time,
      });
}

/**
 * Why a Brief or target command didn't go through, and what to do, as
 * one sentence after the screen's own lead ("The compile didn't start.").
 * Policy refusals are worded by their code where Review words them too.
 */
export function commandReason(
  rc: RecordsCopy,
  b: BriefCopy,
  failure: CommandFailure,
  space: string,
): string {
  const f = rc.failure;
  switch (failure.kind) {
    case "refused": {
      const known = failure.code
        ? (f.refused as Record<string, string>)[failure.code]
        : undefined;
      if (known && failure.code !== "other" && failure.code !== "otherBare") {
        return interpolate(known, { space, ref: "" });
      }
      return failure.message
        ? interpolate(f.refused.other, { message: failure.message })
        : f.refused.otherBare;
    }
    case "rate-limited":
      return failure.retryAfter !== null
        ? interpolate(f.rateLimited, { n: failure.retryAfter })
        : f.rateLimitedSoon;
    case "unreachable":
      return f.unreachable;
    case "decided":
      return b.toast.alreadyDone;
    case "not-found":
      return f.notFound;
    case "clash":
      return f.clash;
    case "unavailable":
      return f.unavailable;
    case "busy":
      return failure.judge ? f.busyJudge : f.busy;
    case "in-conflict":
    case "undo-refused":
    case "carries":
      return f.unknown;
    case "unknown":
      return failure.message ?? f.unknown;
  }
}

/**
 * "3.0 KB of a 25 KB budget": a file's size with one decimal under
 * 10 KB, as the board writes it; a budget in whole KB when it is one.
 */
export function formatKb(
  bytes: number,
  locale: Locale,
  kind: "size" | "budget" = "size",
): string {
  const kb = bytes / 1024;
  const digits =
    kind === "size" ? (kb < 10 ? 1 : 0) : Number.isInteger(kb) ? 0 : 1;
  const n = new Intl.NumberFormat(locale === "zh" ? "zh-CN" : "en-US", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(kb);
  return `${n} KB`;
}

/** Parses the size budget field ("25 KB", "25", "25kb") into bytes; null when it isn't a size. */
export function parseKb(text: string): number | null {
  const match = /^\s*(\d+(?:[.,]\d+)?)\s*(?:k(?:i?b)?)?\s*$/i.exec(text);
  if (!match) return null;
  const kb = Number(match[1]!.replace(",", "."));
  return Number.isFinite(kb) ? Math.round(kb * 1024) : null;
}

/**
 * A compiled file's meta line (TargetPreview.png): "Compiled at 14:31 ·
 * 3.0 KB of a 25 KB budget · 30 lines · 14 facts · 1 open question ·
 * Delivered by pull request". The state is a mark beside it.
 */
export function targetMeta(
  b: BriefCopy,
  {
    target,
    compiledAt,
    bytes,
    lines,
    facts,
    open,
    dropped,
  }: {
    target: TargetView;
    compiledAt: string | null;
    bytes: number;
    lines: number;
    facts: number;
    open: number;
    dropped: number;
  },
  { now, timeZone, locale }: When,
): string[] {
  const t = b.target;
  if (!compiledAt) return [t.notCompiled, t.delivery[target.delivery]];
  const time = formatClock(compiledAt, timeZone, locale);
  const parts = [
    sameDay(new Date(compiledAt), now, timeZone)
      ? interpolate(t.compiledToday, { time })
      : interpolate(t.compiledOn, {
          date: formatShortDate(new Date(compiledAt), timeZone, locale),
          time,
        }),
    interpolate(t.size, {
      size: formatKb(bytes, locale),
      budget: formatKb(target.settings.sizeBudget, locale, "budget"),
      lines: count(t.linesOne, t.lines, lines),
    }),
  ];
  if (isShim(target.kind)) {
    parts.push(interpolate(t.imports, { file: target.reads ?? "AGENTS.md" }));
  } else {
    const what = [count(t.factsOne, t.facts, facts)];
    if (open > 0) what.push(count(t.openOne, t.open, open));
    parts.push(what.join(" · "));
  }
  if (dropped > 0) parts.push(count(t.droppedOne, t.dropped, dropped));
  parts.push(t.delivery[target.delivery]);
  return parts;
}
