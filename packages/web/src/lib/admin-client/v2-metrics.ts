// Admin client for the V2 gate metrics (GET /v1/admin/v2/metrics). Kept
// beside the ops client because the panel lives in /admin/infra.

import { adminReq } from "./transport";
import type { AdminV2Metrics } from "./v2-metrics-types";

/** A range of signup weeks: from (inclusive) and to (exclusive), YYYY-MM-DD in UTC. */
export interface AdminV2MetricsRange {
  from: string;
  to: string;
}

export const adminV2MetricsClient = {
  get(range?: AdminV2MetricsRange): Promise<AdminV2Metrics> {
    const qs = range
      ? `?${new URLSearchParams({ from: range.from, to: range.to })}`
      : "";
    return adminReq<AdminV2Metrics>("GET", `/v1/admin/v2/metrics${qs}`);
  },
};

/**
 * The last `weeks` signup weeks, this one included: from the Monday
 * (UTC) `weeks - 1` weeks before this week's, up to next Monday. The
 * server counts cohorts in the same Monday-based UTC weeks.
 */
export function lastWeeks(weeks: number, now: Date): AdminV2MetricsRange {
  const day = new Date(
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()),
  );
  const monday = new Date(day);
  monday.setUTCDate(day.getUTCDate() - ((day.getUTCDay() + 6) % 7));
  const to = new Date(monday);
  to.setUTCDate(monday.getUTCDate() + 7);
  const from = new Date(to);
  from.setUTCDate(to.getUTCDate() - 7 * weeks);
  const iso = (d: Date) => d.toISOString().slice(0, 10);
  return { from: iso(from), to: iso(to) };
}
