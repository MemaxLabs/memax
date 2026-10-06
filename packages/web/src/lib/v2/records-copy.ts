/**
 * Sentences Review, Memories and a memory's page build from records:
 * who did something, rail times, row notes and why a command didn't go
 * through. Pure (catalogue in, string out), so the rules are
 * unit-tested in both locales.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { formatClock, formatShortDate, joinSentences } from "./copy";
import type { CommandFailure } from "./data/command-error";
import type { MemoryNote } from "./data/memories";
import type { Actor, RecordAction } from "./data/records";
import type { Section } from "./data/types";

export type RecordsCopy = Translations["ledger"]["records"];

/** A registry key's display name ("cursor" → "Cursor"). */
export type AgentName = (key: string) => string;

/** What a stamp shows for an actor: a person's initials, or an agent's key. */
export function stampOf(
  actor: Actor | null,
  copy: RecordsCopy,
  you?: { initials: string },
): { person?: string; agent?: string; name?: string } | null {
  if (!actor) return null;
  switch (actor.kind) {
    case "agent":
      return { agent: actor.agent };
    case "dream":
      return { agent: "dream" };
    case "memax":
      return { agent: "memax", name: copy.actor.memax };
    case "repository":
      return { agent: "repository", name: copy.actor.repository };
    case "person": {
      const initials =
        actor.initials ?? (actor.self ? you?.initials : undefined);
      // Receipts don't name other people yet: no stamp rather than a guess.
      if (!initials) return null;
      return {
        person: initials,
        name: actor.self ? copy.actor.you : actor.name,
      };
    }
  }
}

/** A name for sentences ("Codex", "Jiahao", "You", "A teammate"). */
export function actorName(
  actor: Actor | null,
  copy: RecordsCopy,
  agentName: AgentName,
): string {
  if (!actor) return copy.actor.teammate;
  switch (actor.kind) {
    case "agent":
      return agentName(actor.agent);
    case "dream":
      return copy.actor.dream;
    case "memax":
      return copy.actor.memax;
    case "repository":
      return copy.actor.repository;
    case "person":
      if (actor.self) return copy.actor.you;
      return actor.name ?? copy.actor.teammate;
  }
}

export function isYou(actor: Actor | null): boolean {
  return actor?.kind === "person" && actor.self;
}

export type RailVerb = RecordAction | "updated" | "editing";

export function verbOf(copy: RecordsCopy, action: RailVerb): string {
  return copy.verbs[action];
}

export function sectionName(copy: RecordsCopy, section: Section): string {
  return copy.sections[section];
}

function sameDay(a: Date, b: Date, timeZone: string): boolean {
  const day = (d: Date) =>
    new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(d);
  return day(a) === day(b);
}

/**
 * A receipt's time on a row's rail (Review.png, Memories.png): "14 min
 * ago" and "3 h ago" for the last few hours, the clock ("03:12") earlier
 * the same day, then the date ("Oct 2").
 */
export function railTime(
  copy: RecordsCopy,
  iso: string,
  now: Date,
  timeZone: string,
  locale: Locale,
): string {
  const then = new Date(iso);
  const minutes = Math.round((now.getTime() - then.getTime()) / 60000);
  if (minutes < 1) return copy.time.justNow;
  if (minutes < 60) return interpolate(copy.time.minutesAgo, { n: minutes });
  if (minutes < 6 * 60) {
    return interpolate(copy.time.hoursAgo, { n: Math.round(minutes / 60) });
  }
  if (sameDay(then, now, timeZone)) return formatClock(iso, timeZone, locale);
  return formatShortDate(then, timeZone, locale);
}

/** "Oct 2, 10:41" / "10月2日 10:41": lineage and the kept receipt. */
export function dateTime(
  copy: RecordsCopy,
  iso: string,
  timeZone: string,
  locale: Locale,
): string {
  return interpolate(copy.time.dateTime, {
    date: formatShortDate(new Date(iso), timeZone, locale),
    time: formatClock(iso, timeZone, locale),
  });
}

/** The line under a row's statement (Memories.png). */
export function noteText(
  copy: RecordsCopy,
  note: MemoryNote,
  agentName: AgentName,
  timeZone: string,
  locale: Locale,
): string {
  const n = copy.notes;
  switch (note.kind) {
    case "conflict": {
      const first = interpolate(n.conflict, {
        ref: note.with,
        name: actorName(note.keptBy, copy, agentName),
      });
      const asked = note.askedBy
        ? interpolate(n.conflictAsked, { agent: agentName(note.askedBy) })
        : null;
      return joinSentences([first, asked], locale);
    }
    case "stale": {
      const date = formatShortDate(new Date(note.changedAt), timeZone, locale);
      return note.source
        ? interpolate(n.staleSource, { date, source: note.source })
        : interpolate(n.stale, { date });
    }
    case "merged":
      return interpolate(n.merged, {
        ref: note.into,
        name: actorName(note.by, copy, agentName),
      });
    case "faded":
      return interpolate(n.faded, { n: note.days });
  }
}

/** Which command failed, for the first sentence. */
export type FailedCommand = "keep" | "reject" | "edit";

/**
 * Why a command didn't go through and what to do, as one or two
 * sentences: "M-0430 wasn't kept. It quotes an outside source, so only
 * a web sign-in can keep it. …". Policy refusals are worded by their
 * code; an unknown code falls back to the server's English message.
 */
export function failureText(
  copy: RecordsCopy,
  failure: CommandFailure,
  {
    command,
    ref,
    space,
    locale,
  }: { command: FailedCommand; ref: string; space: string; locale: Locale },
): string {
  const f = copy.failure;
  const lead = interpolate(f[command], { ref });
  let reason: string;
  switch (failure.kind) {
    case "refused": {
      const known = failure.code
        ? (f.refused as Record<string, string>)[failure.code]
        : undefined;
      if (known && failure.code !== "other" && failure.code !== "otherBare") {
        reason = interpolate(known, { space, ref });
      } else if (failure.message) {
        reason = interpolate(f.refused.other, { message: failure.message });
      } else {
        reason = f.refused.otherBare;
      }
      break;
    }
    case "rate-limited":
      reason =
        failure.retryAfter !== null
          ? interpolate(f.rateLimited, { n: failure.retryAfter })
          : f.rateLimitedSoon;
      break;
    case "unreachable":
      reason = f.unreachable;
      break;
    case "decided":
      reason = f.decided;
      break;
    case "not-found":
      reason = f.notFound;
      break;
    case "clash":
      reason = f.clash;
      break;
    case "unavailable":
      reason = f.unavailable;
      break;
    case "unknown":
      reason = f.unknown;
      break;
  }
  return joinSentences([lead, reason], locale);
}
