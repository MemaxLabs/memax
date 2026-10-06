/**
 * The words for a memory's reads and reach (Memory.png: "Read 214 times ·
 * reaches 4 files and 5 agents"). Pure, catalogue in and string out, so
 * the plural rules and the parts left out are unit-tested in both
 * locales. A part the source doesn't know is left out, never shown as 0.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { count } from "./copy";
import type { MemoryRecord } from "./data/memories";

type PageCopy = Translations["ledger"]["memory"]["page"];

const NUMBER_TAGS: Record<Locale, string> = { en: "en-US", zh: "zh-CN" };

/** A count with the locale's grouping: "1,284". */
export function formatCount(n: number, locale: Locale): string {
  return n.toLocaleString(NUMBER_TAGS[locale]);
}

/**
 * "Read 214 times", "Read once", "Not read yet". In a compiled file
 * whose loads Memax can't see, the count is a floor: "Read at least 214
 * times". Null when the reads weren't counted.
 */
export function memoryReadsText(
  p: PageCopy,
  record: Pick<MemoryRecord, "reads" | "readsUnobserved">,
  locale: Locale,
): string | null {
  const n = record.reads;
  if (n === null) return null;
  const vars = { n: formatCount(n, locale) };
  if (record.readsUnobserved) {
    return n === 0
      ? p.readsNoneCounted
      : count(p.readsAtLeastOne, p.readsAtLeast, n, vars);
  }
  return n === 0 ? p.readsNone : count(p.readsOne, p.reads, n, vars);
}

/** The known, non-zero parts of a reach: "4 files", "5 agents". */
function reachParts(
  p: PageCopy,
  reach: MemoryRecord["reach"],
  locale: Locale,
): { files: string | null; agents: string | null } {
  const part = (one: string, other: string, n: number | null | undefined) =>
    n ? count(one, other, n, { n: formatCount(n, locale) }) : null;
  return {
    files: part(p.filesOne, p.files, reach?.files),
    agents: part(p.agentsOne, p.agents, reach?.agents),
  };
}

/** The head's "reaches 4 files and 5 agents" (or "reaches 5 agents"); null with nothing to say. */
export function reachText(
  p: PageCopy,
  reach: MemoryRecord["reach"],
  locale: Locale,
): string | null {
  const { files, agents } = reachParts(p, reach, locale);
  if (files && agents) return interpolate(p.reach, { files, agents });
  const what = files ?? agents;
  return what ? interpolate(p.reachOnly, { what }) : null;
}

/** The Reaches panel's meta: "4 files · 5 agents" (or one of them); null with nothing to say. */
export function reachMeta(
  p: PageCopy,
  reach: MemoryRecord["reach"],
  locale: Locale,
): string | null {
  const { files, agents } = reachParts(p, reach, locale);
  if (files && agents) return interpolate(p.reachesMeta, { files, agents });
  return files ?? agents;
}
