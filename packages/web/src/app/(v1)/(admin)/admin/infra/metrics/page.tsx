"use client";

import { useMemo, useState } from "react";
import { interpolate, useLocale } from "@/i18n";
import { useAdminV2Metrics } from "@/hooks/use-admin-v2-metrics";
import { lastWeeks } from "@/lib/admin-client";
import { GateMetricsView } from "@/components/admin/gate-metrics";

const WEEK_CHOICES = [4, 8, 12, 26] as const;

/**
 * /admin/infra/metrics: the V2 plan's phase gates (activation and first
 * file for the private alpha, week-4 keeping for the beta, team pull for
 * Team), by the week people signed up, with the north star and review
 * health beside them. Computed by the server from receipts, as counts.
 */
export default function AdminInfraMetricsPage() {
  const { t } = useLocale();
  const copy = t.admin.infra.metrics;
  const [weeks, setWeeks] = useState<number>(8);
  const range = useMemo(() => lastWeeks(weeks, new Date()), [weeks]);
  const { data, error, isLoading } = useAdminV2Metrics(range);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-[16px] font-semibold text-fg-1">{copy.title}</h2>
          <p className="mt-1 max-w-2xl text-[13px] text-fg-3">
            {copy.description}
          </p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <div
            role="group"
            aria-label={copy.weeks}
            className="inline-flex rounded-lg border border-border/60 p-0.5"
          >
            {WEEK_CHOICES.map((n) => (
              <button
                key={n}
                type="button"
                aria-pressed={weeks === n}
                onClick={() => setWeeks(n)}
                className={`rounded-md px-2.5 py-1 text-[12px] font-medium tabular-nums transition-colors ${
                  weeks === n
                    ? "bg-surface-2 text-fg-1"
                    : "text-fg-3 hover:text-fg-1"
                }`}
              >
                {interpolate(copy.weeksOption, { n })}
              </button>
            ))}
          </div>
          {data ? (
            <span className="text-[12px] tabular-nums text-fg-3">
              {interpolate(copy.asOf, {
                time: data.as_of.slice(0, 16).replace("T", " "),
              })}
            </span>
          ) : null}
        </div>
      </div>

      {error ? (
        <div
          role="alert"
          className="rounded-xl border border-[oklch(from_var(--destructive)_l_c_h_/_0.3)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.08)] px-4 py-3 text-[13px] text-fg-1"
        >
          {copy.loadError}
        </div>
      ) : null}

      {isLoading ? (
        <div className="space-y-6">
          {Array.from({ length: 3 }).map((_, i) => (
            <div
              key={i}
              className="h-40 animate-pulse rounded-2xl bg-surface-2"
            />
          ))}
        </div>
      ) : data ? (
        <div className="animate-content-ready">
          <GateMetricsView data={data} />
        </div>
      ) : null}
    </div>
  );
}
