/**
 * How a version reads in BriefHistory.png's list: "Dream rewrote it",
 * "You edited one fact", "Jiahao added M-0098", "The first Brief", and
 * "Oct 4, 16:02 · 1 change". Pure, so it's unit-tested.
 */
import type { Locale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import type { BriefCopy } from "@/lib/v2/brief-copy";
import { sameDay } from "@/lib/v2/brief-copy";
import { count, formatClock, formatShortDate } from "@/lib/v2/copy";
import type { BriefVersionView } from "@/lib/v2/data/brief";
import type { Actor } from "@/lib/v2/data/records";
import { onlyChange, type HistoryDiff } from "./history-diff";

export function versionTitle(
  b: BriefCopy,
  version: BriefVersionView,
  diff: HistoryDiff,
  name: (actor: Actor | null) => string,
): string {
  const h = b.history;
  if (version.parent === null) return h.first;
  if (version.by?.kind === "dream") return h.dream;
  const who =
    version.by?.kind === "person" && version.by.self ? h.you : name(version.by);
  const only = onlyChange(diff);
  switch (only?.kind) {
    case "added":
      return interpolate(h.added, { who, ref: only.ref });
    case "removed":
      return interpolate(h.removed, { who, ref: only.ref });
    case "moved":
      return interpolate(h.moved, { who, ref: only.ref });
    case "reworded":
      return interpolate(h.editedOne, { who });
    default:
      return interpolate(h.changed, { who });
  }
}

/** "Today, 03:12" or "Oct 4, 16:02". */
export function versionWhen(
  b: BriefCopy,
  at: string,
  now: Date,
  timeZone: string,
  locale: Locale,
): string {
  const time = formatClock(at, timeZone, locale);
  return sameDay(new Date(at), now, timeZone)
    ? interpolate(b.history.today, { time })
    : interpolate(b.history.on, {
        date: formatShortDate(new Date(at), timeZone, locale),
        time,
      });
}

/** "Oct 2, 10:58 · 1 added", "Today, 03:12 · 3 changes", "Sep 28, 15:20 · 7 facts". */
export function versionMeta(
  b: BriefCopy,
  version: BriefVersionView,
  diff: HistoryDiff,
  when: string,
): string {
  const h = b.history;
  if (version.parent === null) {
    return `${when} · ${count(h.factsOne, h.facts, version.facts)}`;
  }
  const changes = diff.sections.flatMap((s) => s.changes);
  const onlyAdded =
    changes.length > 0 && changes.every((c) => c.kind === "added");
  return `${when} · ${
    onlyAdded
      ? count(h.addedCountOne, h.addedCount, changes.length)
      : count(h.changesOne, h.changes, changes.length)
  }`;
}
