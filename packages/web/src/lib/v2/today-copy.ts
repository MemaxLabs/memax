/**
 * Sentences Today builds from its data (Main.png, MobileToday.png): the
 * lede from real counts, the phone's headline, an agent's day and the
 * footer line. Pure (catalogue in, string out), so the rules are
 * unit-tested in both locales.
 */
import type { Locale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import type { Translations } from "@/i18n/locales/en";
import { sameDay } from "./brief-copy";
import {
  count,
  formatClock,
  formatShortDate,
  joinList,
  spell,
  todayLede,
  type AppCopy,
} from "./copy";
import type { AgentToday, TodayData } from "./data/today";
import type { SpaceOverview } from "./data/types";

export type TodayCopy = Translations["ledger"]["today"];

function capitalise(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * Main.png's lede, from what Today read: "Overnight, Dream folded 34
 * notes into 6 facts. Four proposals, one stale fact and a question from
 * Codex are waiting on you." Without an edition, only the second
 * sentence; null when nothing waits and there was no edition.
 */
export function todayLedeOf(
  app: AppCopy,
  locale: Locale,
  overview: SpaceOverview,
  data: TodayData,
  agentName: (key: string) => string,
): string | null {
  const edition = data.dream.kind === "edition" ? data.dream.edition : null;
  return todayLede(
    app,
    locale,
    {
      ...overview,
      waiting: data.waiting.total,
      dream: edition ? { notes: edition.notes, facts: edition.facts } : null,
      waitingBreakdown: {
        proposals: data.waiting.proposals,
        stale: data.waiting.stale,
        questions: data.waiting.questions,
      },
    },
    agentName,
  );
}

/** MobileToday.png's headline: "Four proposals and a question are waiting on you." */
export function todayHeadline(
  app: AppCopy,
  td: TodayCopy,
  locale: Locale,
  data: TodayData,
): string {
  const t = app.today;
  const { proposals, stale, questions } = data.waiting;
  const items: string[] = [];
  if (proposals > 0) {
    items.push(
      count(t.proposalsOne, t.proposals, proposals, {
        n: spell(app, proposals),
      }),
    );
  }
  if (questions.length > 0) {
    items.push(
      questions.length === 1
        ? td.waiting.question
        : interpolate(td.waiting.questions, {
            n: spell(app, questions.length),
          }),
    );
  }
  // A phone has room for one line: stale facts only when nothing else waits.
  let total = proposals + questions.length;
  if (items.length === 0 && stale > 0) {
    items.push(count(t.staleOne, t.stale, stale, { n: spell(app, stale) }));
    total = stale;
  }
  if (items.length === 0) return td.waiting.nothing;
  return capitalise(
    interpolate(total === 1 ? t.waitingOne : t.waitingMany, {
      list: joinList(items, locale),
    }),
  );
}

/** "41 read · 3 kept", "18 read · 2 proposed", "12 read · none". */
export function agentDay(td: TodayCopy, agent: AgentToday): string {
  const a = td.agents;
  const wrote: string[] = [];
  if (agent.kept > 0) wrote.push(interpolate(a.kept, { n: agent.kept }));
  if (agent.proposed > 0) {
    wrote.push(interpolate(a.proposed, { n: agent.proposed }));
  }
  const parts = [
    // "41 read"; an agent that read nothing today doesn't say "0 read".
    agent.reads ? interpolate(a.read, { n: agent.reads }) : null,
    wrote.length > 0 ? wrote.join(" · ") : a.none,
  ];
  return parts.filter(Boolean).join(" · ");
}

/** Main.png's footer: "Compiled at 14:31 · Dream runs nightly at 03:00 · 214 memories kept in memax-v2". */
export function todayFooter(
  td: TodayCopy,
  {
    compiledAt,
    dreamAt,
    dreamWeekday = null,
    kept,
    space,
  }: {
    compiledAt: string | null;
    dreamAt: string | null;
    /** When Dream runs weekly, the day (0 is Sunday). */
    dreamWeekday?: number | null;
    kept: number | null;
    space: string;
  },
  { now, timeZone, locale }: { now: Date; timeZone: string; locale: Locale },
): string | null {
  const f = td.footer;
  const parts: string[] = [];
  if (compiledAt) {
    const time = formatClock(compiledAt, timeZone, locale);
    parts.push(
      sameDay(new Date(compiledAt), now, timeZone)
        ? interpolate(f.compiledAt, { time })
        : interpolate(f.compiledOn, {
            date: formatShortDate(new Date(compiledAt), timeZone, locale),
            time,
          }),
    );
  }
  if (dreamAt) {
    parts.push(
      dreamWeekday !== null
        ? interpolate(f.dreamWeekly, {
            day: weekdayName(dreamWeekday, locale),
            time: dreamAt,
          })
        : interpolate(f.dreamAt, { time: dreamAt }),
    );
  }
  if (kept !== null) parts.push(count(f.keptOne, f.kept, kept, { space }));
  return parts.length > 0 ? parts.join(" · ") : null;
}

/** "Tuesday" / "星期二" for a weekday (0 is Sunday). */
export function weekdayName(day: number, locale: Locale): string {
  // 4 Oct 2026 was a Sunday; noon UTC, formatted in UTC, stays on its day.
  return new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-US", {
    timeZone: "UTC",
    weekday: "long",
  }).format(new Date(Date.UTC(2026, 9, 4 + (((day % 7) + 7) % 7), 12)));
}

/** The Dream card's date: "Mon 5 Oct" / "10月5日周一". */
export function dreamDate(
  iso: string,
  timeZone: string,
  locale: Locale,
): string {
  return new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-GB", {
    timeZone,
    weekday: "short",
    day: "numeric",
    month: "short",
  }).format(new Date(iso));
}
