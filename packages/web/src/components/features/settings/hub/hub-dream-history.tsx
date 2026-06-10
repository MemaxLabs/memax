"use client";

import type { DreamRun } from "memax-sdk";
import { useLocale, useInterpolate } from "@/i18n";
import { useDreamRunHistory } from "@/hooks/use-dream-runs";
import { formatAge } from "@/lib/format-age";

/**
 * HubDreamHistory — paginated log of past dream runs for ONE hub.
 *
 * Post-migration 018 every dream setting is per-hub, so the history
 * view is also per-hub: the parent HubIntelligence passes its
 * scope's hubId and we fetch only that hub's runs. No hub filter
 * dropdown (redundant — the scope switcher above already picks the
 * hub).
 *
 * Nested sub-view inside Settings → {hub} → Intelligence. Parent
 * owns the overview / history toggle via local state and passes
 * `onBack` to return.
 *
 * Pagination is cursor-based via useDreamRunHistory. "Load more"
 * is explicit (button), mirroring the prior YouIntelligence flow.
 */
export function HubDreamHistory({
  hubId,
  onBack,
}: {
  hubId: string;
  onBack: () => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const {
    runs,
    isLoading,
    isError,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
    refetch,
  } = useDreamRunHistory({ hubId });

  return (
    <section className="space-y-4">
      <header className="flex items-center justify-between gap-3 px-1">
        <button
          type="button"
          onClick={onBack}
          className="text-[13px] text-fg-3 hover:text-fg-1 transition-colors duration-[var(--dur-fast)] ease-[var(--ease-spring)] cursor-pointer"
        >
          {t.dreams.history.back}
        </button>
      </header>

      <h2 className="text-[20px] font-semibold text-fg-1 px-1">
        {t.dreams.history.title}
      </h2>

      {isLoading ? (
        <HistorySkeleton />
      ) : isError ? (
        <HistoryError onRetry={() => refetch()} />
      ) : runs.length === 0 ? (
        <HistoryEmpty />
      ) : (
        <div className="space-y-2">
          {runs.map((run) => (
            <DreamRunCard key={run.id} run={run} interpolate={interpolate} />
          ))}
          {hasNextPage && (
            <div className="pt-2">
              <button
                type="button"
                onClick={() => fetchNextPage()}
                disabled={isFetchingNextPage}
                className="w-full inline-flex items-center justify-center gap-2 rounded-xl border border-border/60 px-4 py-2.5 text-[13px] font-medium text-fg-2 hover:bg-surface-1 transition-colors duration-[var(--dur-fast)] ease-[var(--ease-spring)] disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
              >
                {isFetchingNextPage
                  ? t.dreams.history.loadingMore
                  : t.dreams.history.loadMore}
              </button>
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function HistorySkeleton() {
  // Three skeleton rows match the average first-paint shape (page
  // size 20, viewport shows ~3-4 above the fold). Matching shape
  // prevents layout shift on data arrival.
  return (
    <div className="space-y-2" aria-hidden>
      {[0, 1, 2].map((i) => (
        <div
          key={i}
          className="rounded-xl border border-border/30 px-4 py-3 animate-pulse"
          style={{ animationDelay: `${i * 80}ms` }}
        >
          <div className="h-3 w-32 bg-surface-2 rounded mb-2" />
          <div className="h-4 w-56 bg-surface-2 rounded" />
        </div>
      ))}
    </div>
  );
}

function HistoryEmpty() {
  const { t } = useLocale();
  return (
    <div className="rounded-xl border border-border/40 px-5 py-6 text-center">
      <p className="text-[14px] font-medium text-fg-2">
        {t.dreams.history.emptyTitle}
      </p>
      <p className="mt-1 text-[13px] text-fg-3">{t.dreams.history.emptyBody}</p>
    </div>
  );
}

function HistoryError({ onRetry }: { onRetry: () => void }) {
  const { t } = useLocale();
  return (
    <div className="rounded-xl border border-amber-500/30 bg-amber-500/5 px-5 py-4">
      <p className="text-[14px] font-medium text-fg-1">
        {t.dreams.history.errorTitle}
      </p>
      <button
        type="button"
        onClick={onRetry}
        className="mt-2 text-[13px] text-fg-2 hover:text-fg-1 underline-offset-2 hover:underline cursor-pointer"
      >
        {t.dreams.history.errorRetry}
      </button>
    </div>
  );
}

function DreamRunCard({
  run,
  interpolate,
}: {
  run: DreamRun;
  interpolate: (
    template: string,
    vars: Record<string, string | number>,
  ) => string;
}) {
  const { t } = useLocale();
  const when = formatAge(run.started_at, t, interpolate);
  const summary = buildSummaryParts(run, t, interpolate);
  const statusLabel = statusLabelFor(run.status, t);
  const statusTone = statusToneFor(run.status);

  return (
    <details className="group rounded-xl border border-border/40 overflow-hidden transition-colors hover:border-border/60">
      <summary
        className="cursor-pointer select-none list-none px-4 py-3 flex flex-col gap-1"
        aria-label={interpolate(t.dreams.history.expandAriaLabel, { when })}
      >
        <div className="flex items-center gap-2">
          <StatusDot tone={statusTone} />
          <span className="text-[13px] text-fg-3">{when}</span>
          <span className="ml-auto text-[11px] font-medium uppercase tracking-wider text-fg-3">
            {statusLabel}
          </span>
        </div>
        <p className="text-[14px] text-fg-1 truncate">
          {summary || t.dreams.history.summaryNoActions}
        </p>
        {run.status === "partial_failed" && (
          <p className="text-[12px] text-amber-600 dark:text-amber-400">
            {t.dreams.history.partialFailedHint}
          </p>
        )}
        {run.status === "skipped" && run.report && (
          <p className="text-[12px] text-fg-3">
            {t.dreams.history.skippedPrefix}: {run.report}
          </p>
        )}
      </summary>
      <div className="px-4 pb-4 pt-1 border-t border-border/30 bg-surface-1/30">
        <PhaseBreakdown run={run} />
      </div>
    </details>
  );
}

function PhaseBreakdown({ run }: { run: DreamRun }) {
  const { t } = useLocale();
  // topics_restructured is optional in the SDK type (server always
  // sends it); ?? 0 keeps the column visible without rendering
  // "undefined".
  const rows: { label: string; value: number | string }[] = [
    {
      label: t.dreams.history.phaseMemoriesScanned,
      value: run.memories_scanned,
    },
    { label: t.dreams.history.phaseMerge, value: run.duplicates_merged },
    {
      label: t.dreams.history.phaseContradictions,
      value: run.contradictions_found,
    },
    { label: t.dreams.history.phaseArchive, value: run.memories_archived },
    { label: t.dreams.history.phaseOrganize, value: run.memories_organized },
    {
      label: t.dreams.history.phaseRestructure,
      value: run.topics_restructured ?? 0,
    },
  ];
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-2 mt-3">
      {rows.map((row) => (
        <div key={row.label} className="flex items-baseline justify-between">
          <dt className="text-[12px] text-fg-3">{row.label}</dt>
          <dd className="text-[13px] font-medium tabular-nums text-fg-1">
            {row.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}

type StatusTone = "success" | "warn" | "neutral" | "danger" | "info";

function statusToneFor(status: string): StatusTone {
  switch (status) {
    case "completed":
      return "success";
    case "partial_failed":
      return "warn";
    case "skipped":
      return "neutral";
    case "failed":
      return "danger";
    case "running":
    case "stale":
      return "info";
    default:
      return "neutral";
  }
}

function statusLabelFor(
  status: string,
  t: ReturnType<typeof useLocale>["t"],
): string {
  switch (status) {
    case "completed":
      return t.dreams.history.statusCompleted;
    case "partial_failed":
      return t.dreams.history.statusPartialFailed;
    case "skipped":
      return t.dreams.history.statusSkipped;
    case "failed":
      return t.dreams.history.statusFailed;
    case "running":
      return t.dreams.history.statusRunning;
    case "stale":
      return t.dreams.history.statusStale;
    default:
      return status;
  }
}

function StatusDot({ tone }: { tone: StatusTone }) {
  const color =
    tone === "success"
      ? "bg-emerald-500/80"
      : tone === "warn"
        ? "bg-amber-500/80"
        : tone === "danger"
          ? "bg-rose-500/80"
          : tone === "info"
            ? "bg-violet-500/80"
            : "bg-fg-3/40";
  return (
    <span
      aria-hidden
      className={`inline-block h-2 w-2 rounded-full shrink-0 ${color}`}
    />
  );
}

function buildSummaryParts(
  run: DreamRun,
  t: ReturnType<typeof useLocale>["t"],
  interpolate: (
    template: string,
    vars: Record<string, string | number>,
  ) => string,
): string {
  const parts: string[] = [];
  if (run.duplicates_merged > 0) {
    parts.push(
      interpolate(t.dreams.history.summaryMerged, {
        n: String(run.duplicates_merged),
      }),
    );
  }
  if (run.contradictions_found > 0) {
    parts.push(
      interpolate(t.dreams.history.summaryContradictions, {
        n: String(run.contradictions_found),
      }),
    );
  }
  if (run.memories_archived > 0) {
    parts.push(
      interpolate(t.dreams.history.summaryArchived, {
        n: String(run.memories_archived),
      }),
    );
  }
  if (run.memories_organized > 0) {
    parts.push(
      interpolate(t.dreams.history.summaryOrganized, {
        n: String(run.memories_organized),
      }),
    );
  }
  if ((run.topics_restructured ?? 0) > 0) {
    parts.push(
      interpolate(t.dreams.history.summaryRestructured, {
        n: String(run.topics_restructured ?? 0),
      }),
    );
  }
  return parts.join(" · ");
}
