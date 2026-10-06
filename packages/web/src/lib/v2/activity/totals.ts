import type { ActivityEntry, WeeklyTotals } from "../data/activity";

const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

export interface CountedWeek {
  totals: WeeklyTotals;
  /**
   * Whether the loaded receipts cover the whole week: there are no older
   * pages, or the oldest loaded receipt is from before the week began.
   * When false the counts are a floor, and the page says so.
   */
  complete: boolean;
}

/**
 * This week's totals counted from the receipts the page has loaded: the
 * last 7 days, as the API's AgentWeek counts them. A stand-in until /v2
 * serves a space's receipt totals (PLACEHOLDER in activity-sdk.ts).
 *
 * `readsListed` says whether `entries` holds every read of the week.
 * Receipts never include reads (plan §5.3), and the reads list counts
 * its own week (ReadsPage.week), so Activity passes false and takes the
 * reads from there.
 */
export function countWeek(
  entries: readonly ActivityEntry[],
  {
    now,
    hasMore,
    readsListed,
  }: { now: Date; hasMore: boolean; readsListed: boolean },
): CountedWeek {
  const since = now.getTime() - WEEK_MS;
  const totals: WeeklyTotals = {
    reads: readsListed ? 0 : null,
    proposals: 0,
    kept: 0,
    rejected: 0,
    forgotten: 0,
    compiles: 0,
  };
  let oldest = Infinity;
  for (const entry of entries) {
    const t = new Date(entry.at).getTime();
    oldest = Math.min(oldest, t);
    if (t < since || t > now.getTime()) continue;
    switch (entry.action) {
      case "read":
        if (totals.reads !== null) totals.reads += 1;
        break;
      case "proposed":
        totals.proposals += 1;
        break;
      case "kept":
        totals.kept += 1;
        break;
      case "rejected":
        totals.rejected += 1;
        break;
      case "forgot":
        totals.forgotten += 1;
        break;
      case "compiled":
        totals.compiles += 1;
        break;
    }
  }
  return { totals, complete: !hasMore || oldest < since };
}
