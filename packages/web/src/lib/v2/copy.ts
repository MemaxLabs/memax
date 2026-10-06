/**
 * Sentences the V2 frame builds from data: page ledes, the status
 * line, the switcher's meta lines, times. Pure (catalogue in, string
 * out) so the plural and list rules are unit-tested in both locales.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import type {
  SpaceKind,
  SpaceOverview,
  SpaceSummary,
  SyncLine,
} from "./data/types";

export type AppCopy = Translations["ledger"]["app"];

const LIST_TAGS: Record<Locale, string> = { en: "en-GB", zh: "zh-CN" };
const DATE_TAGS: Record<Locale, string> = { en: "en-US", zh: "zh-CN" };

/**
 * "a, b and c" without a serial comma (house style). In Chinese
 * "a、b和c", with the space mixed-script copy puts between 和 and a
 * following numeral or Latin word ("和 Codex 的一个问题").
 */
export function joinList(items: string[], locale: Locale): string {
  if (locale === "zh" && items.length > 1) {
    const last = items[items.length - 1];
    const and = /^[A-Za-z0-9]/.test(last) ? "和 " : "和";
    return `${items.slice(0, -1).join("、")}${and}${last}`;
  }
  return new Intl.ListFormat(LIST_TAGS[locale], { type: "conjunction" }).format(
    items,
  );
}

/** Sentences in a row: a space between them in English, none in Chinese. */
export function joinSentences(
  sentences: (string | null | undefined)[],
  locale: Locale,
): string {
  return sentences.filter(Boolean).join(locale === "zh" ? "" : " ");
}

/** Picks `one` for 1, else `other`, then fills {n} (and any other vars). */
export function count(
  one: string,
  other: string,
  n: number,
  vars: Record<string, string | number> = {},
): string {
  return interpolate(n === 1 ? one : other, { n, ...vars });
}

/** A small count spelled out ("four"), as the boards write them; larger ones stay numerals. */
export function spell(copy: AppCopy, n: number): string {
  return copy.numbers[n] ?? String(n);
}

function capitalise(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export function statusLabel(
  copy: AppCopy,
  line: SyncLine,
  agentName: (key: string) => string,
): string {
  const s = copy.frame.status;
  switch (line.kind) {
    case "in-sync":
      return count(s.inSyncOne, s.inSync, line.agents);
    case "drifted":
      return interpolate(s.drifted, { agent: agentName(line.agent) });
    case "compiling":
      return s.compiling;
    case "no-agents":
      return s.noAgents;
    case "not-compiling":
      return s.notCompiling;
  }
}

/** The StateMark the status line wears: kept when in sync, ochre when a person must act. */
export function statusState(line: SyncLine) {
  switch (line.kind) {
    case "in-sync":
      return "kept" as const;
    case "drifted":
      return "proposed" as const;
    case "compiling":
      return "working" as const;
    default:
      return "off" as const;
  }
}

/** The switcher's second line: "Project · 214 memories · 5 agents". */
export function spaceMeta(copy: AppCopy, space: SpaceSummary): string {
  const s = copy.spaces;
  const memories =
    space.kept === null ? null : count(s.memoriesOne, s.memories, space.kept);
  const agents =
    space.agents === null ? null : count(s.agentsOne, s.agents, space.agents);
  const people =
    space.people === null ? null : count(s.peopleOne, s.people, space.people);
  const kind = copy.frame.spaceKind[space.kind];
  if (space.kind === "personal") {
    return memories ? interpolate(s.personalMeta, { memories }) : kind;
  }
  if (space.kind === "team") {
    return people && agents
      ? interpolate(s.teamMeta, { people, agents })
      : kind;
  }
  if (space.kept === 0 && space.agents === 0) return s.projectNew;
  return memories && agents
    ? interpolate(s.projectMeta, { memories, agents })
    : kind;
}

/** "memax-v2 · Project space"; a team adds "2 people, 7 agents". */
export function eyebrow(copy: AppCopy, space: SpaceSummary): string {
  const f = copy.frame;
  const kind = f.spaceKindSpace[space.kind as SpaceKind];
  if (space.kind === "team" && space.people !== null && space.agents !== null) {
    return interpolate(f.eyebrowTeam, {
      space: space.name,
      kind,
      people: count(copy.spaces.peopleOne, copy.spaces.people, space.people),
      agents: count(copy.spaces.agentsOne, copy.spaces.agents, space.agents),
    });
  }
  return interpolate(f.eyebrow, { space: space.name, kind });
}

/**
 * Today's lede: "Overnight, Dream folded 34 notes into 6 facts. Four
 * proposals, one stale fact and a question from Codex are waiting on
 * you." Null when nothing is waiting and there was no Dream.
 */
export function todayLede(
  copy: AppCopy,
  locale: Locale,
  overview: SpaceOverview,
  agentName: (key: string) => string,
): string | null {
  const t = copy.today;
  const sentences: string[] = [];
  if (overview.dream) {
    sentences.push(
      interpolate(t.dream, {
        notes: count(t.notesOne, t.notes, overview.dream.notes),
        facts: count(t.factsOne, t.facts, overview.dream.facts),
      }),
    );
  }
  const items: string[] = [];
  const breakdown = overview.waitingBreakdown;
  if (breakdown) {
    if (breakdown.proposals > 0) {
      items.push(
        count(t.proposalsOne, t.proposals, breakdown.proposals, {
          n: spell(copy, breakdown.proposals),
        }),
      );
    }
    if (breakdown.stale > 0) {
      items.push(
        count(t.staleOne, t.stale, breakdown.stale, {
          n: spell(copy, breakdown.stale),
        }),
      );
    }
    if (breakdown.questions.length > 0) {
      const agents = joinList(
        [...new Set(breakdown.questions)].map(agentName),
        locale,
      );
      items.push(
        count(t.question, t.questions, breakdown.questions.length, {
          n: spell(copy, breakdown.questions.length),
          agents,
        }),
      );
    }
  } else if (overview.waiting > 0) {
    items.push(
      count(t.thingsOne, t.things, overview.waiting, {
        n: spell(copy, overview.waiting),
      }),
    );
  }
  if (items.length > 0) {
    const total = breakdown
      ? breakdown.proposals + breakdown.stale + breakdown.questions.length
      : overview.waiting;
    const template = total === 1 ? t.waitingOne : t.waitingMany;
    sentences.push(
      capitalise(interpolate(template, { list: joinList(items, locale) })),
    );
  }
  if (sentences.length === 0) return null;
  return sentences.join(locale === "zh" ? "" : " ");
}

/** Memories' lede: "214 kept, 5 waiting on you and 3 forgotten. Kept is the quiet default…". */
export function memoriesLede(
  copy: AppCopy,
  locale: Locale,
  overview: SpaceOverview,
): string | null {
  const m = copy.memories;
  const { kept, forgotten } = overview.memories;
  // Unknown, or nothing at all: the empty state says it instead.
  if (kept === null || (kept === 0 && !overview.waiting && !forgotten)) {
    return null;
  }
  const parts = [interpolate(m.kept, { n: kept })];
  if (overview.waiting > 0) {
    parts.push(interpolate(m.waiting, { n: overview.waiting }));
  }
  if (forgotten) parts.push(interpolate(m.forgotten, { n: forgotten }));
  const counts = joinList(parts, locale);
  const end = locale === "zh" ? "。" : ". ";
  return `${counts}${end}${m.ledeRest}`;
}

/** Agents' lede: "Six agents are connected and five are active. Each one reads…". */
export function agentsLede(
  copy: AppCopy,
  locale: Locale,
  overview: SpaceOverview,
): string | null {
  const a = copy.agents;
  if (!overview.agents || overview.agents.connected === 0) return null;
  const { connected, active } = overview.agents;
  const sentence = interpolate(a.counts, {
    connected: count(a.connectedOne, a.connected, connected, {
      N: capitalise(spell(copy, connected)),
    }),
    active: count(a.activeOne, a.active, active, { n: spell(copy, active) }),
  });
  return `${sentence}${locale === "zh" ? "" : " "}${a.ledeRest}`;
}

// Times. Receipts and Today use the viewer's zone.

function parts(date: Date, timeZone: string, locale: Locale) {
  const fmt = new Intl.DateTimeFormat(DATE_TAGS[locale], {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  });
  const get = (type: string) =>
    fmt.formatToParts(date).find((p) => p.type === type)?.value ?? "";
  return {
    day: `${get("year")}-${get("month")}-${get("day")}`,
    clock: `${get("hour")}:${get("minute")}`,
  };
}

/** Today's title: "Monday, October 5" / "10月5日星期一". */
export function formatDayTitle(
  date: Date,
  timeZone: string,
  locale: Locale,
): string {
  return new Intl.DateTimeFormat(DATE_TAGS[locale], {
    timeZone,
    weekday: "long",
    month: "long",
    day: "numeric",
  }).format(date);
}

/** A 24-hour clock in the viewer's zone: "03:12". */
export function formatClock(
  iso: string,
  timeZone: string,
  locale: Locale,
): string {
  return parts(new Date(iso), timeZone, locale).clock;
}

/** A short date: "Oct 2" / "10月2日". */
export function formatShortDate(
  date: Date,
  timeZone: string,
  locale: Locale,
): string {
  return new Intl.DateTimeFormat(DATE_TAGS[locale], {
    timeZone,
    month: "short",
    day: "numeric",
  }).format(date);
}

/** "today at 09:41", or "Oct 3 at 09:41" on another day. */
export function formatWhen(
  copy: AppCopy,
  iso: string,
  now: Date,
  timeZone: string,
  locale: Locale,
): string {
  const then = parts(new Date(iso), timeZone, locale);
  const today = parts(now, timeZone, locale);
  if (then.day === today.day) {
    return interpolate(copy.time.today, { time: then.clock });
  }
  return interpolate(copy.time.on, {
    date: formatShortDate(new Date(iso), timeZone, locale),
    time: then.clock,
  });
}

/** A receipt's time: "today 14:31" within the day, otherwise "Oct 2". */
export function formatReceiptTime(
  copy: AppCopy,
  iso: string,
  now: Date,
  timeZone: string,
  locale: Locale,
): string {
  const then = parts(new Date(iso), timeZone, locale);
  if (then.day === parts(now, timeZone, locale).day) {
    return interpolate(copy.time.todayShort, { time: then.clock });
  }
  return formatShortDate(new Date(iso), timeZone, locale);
}

/** An age, short: "14 min", "3 h", "2 d". */
export function formatAge(copy: AppCopy, iso: string, now: Date): string {
  const minutes = Math.max(
    0,
    Math.round((now.getTime() - new Date(iso).getTime()) / 60000),
  );
  if (minutes < 60) return interpolate(copy.time.minutes, { n: minutes });
  const hours = Math.round(minutes / 60);
  if (hours < 24) return interpolate(copy.time.hours, { n: hours });
  return interpolate(copy.time.days, { n: Math.round(hours / 24) });
}

/** An age in words: "22 minutes ago", "1 hour ago". */
export function formatAgo(copy: AppCopy, iso: string, now: Date): string {
  const minutes = Math.round((now.getTime() - new Date(iso).getTime()) / 60000);
  if (minutes < 1) return copy.time.justNow;
  if (minutes < 60) {
    return count(copy.time.minutesAgoOne, copy.time.minutesAgo, minutes);
  }
  const hours = Math.round(minutes / 60);
  return count(copy.time.hoursAgoOne, copy.time.hoursAgo, hours);
}
