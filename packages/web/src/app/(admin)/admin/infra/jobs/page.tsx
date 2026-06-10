"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ChevronRight, Filter, X } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@memaxlabs/ui";
import { useLocale, interpolate } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { useAdminOpsJobs } from "@/hooks/use-admin-ops";
import type { AdminOpsJobSummary } from "@/lib/admin-client";
import {
  EmptyState,
  JobStatePill,
  formatRelativeTime,
} from "@/components/admin/ops-components";

type OpsJobsCopy = Translations["admin"]["infra"]["jobs"];

// Keep these in sync with model.JobState* constants in
// packages/server/internal/model/ops.go. Fixed list — River doesn't
// add new states often, and the filter dropdown should render known
// states even when the current page has no job in that state.
const ALL_STATES = [
  "available",
  "running",
  "scheduled",
  "completed",
  "discarded",
  "cancelled",
  "retryable",
  "pending",
] as const;

export default function AdminInfraJobsPage() {
  const { t } = useLocale();
  const copy = t.admin.infra.jobs;

  const [filters, setFilters] = useState({
    state: "",
    kind: "",
    queue: "",
  });

  const params = useMemo(
    () => ({
      states: filters.state ? [filters.state] : undefined,
      // Partial match (ILIKE) on both kind and queue. The exact-set
      // params (`kinds` / `queues`) exist for future multi-select.
      kind_search: filters.kind || undefined,
      queue_search: filters.queue || undefined,
      limit: 50,
    }),
    [filters.state, filters.kind, filters.queue],
  );

  // useInfiniteQuery gives us:
  //   - Automatic page accumulation in the TanStack cache (persists
  //     across navigate-away + back).
  //   - Correct stitching even when "Load more" is clicked fast.
  //   - Invalidation after retry/cancel refetches all loaded pages,
  //     so the stitched list stays consistent.
  const {
    data,
    isLoading,
    isFetching,
    isFetchingNextPage,
    hasNextPage,
    fetchNextPage,
    error,
  } = useAdminOpsJobs(params);

  const jobs = useMemo(
    () => (data ? data.pages.flatMap((p) => p.jobs) : []),
    [data],
  );

  const setFilter = (key: "state" | "kind" | "queue", val: string) => {
    setFilters((prev) => ({ ...prev, [key]: val }));
  };

  const clearFilters = () => {
    setFilters({ state: "", kind: "", queue: "" });
  };

  const hasActiveFilter = !!(filters.state || filters.kind || filters.queue);

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-[16px] font-semibold text-fg-1">{copy.title}</h2>
        <p className="mt-1 max-w-2xl text-[13px] text-fg-3">
          {copy.description}
        </p>
      </div>

      <FilterBar
        state={filters.state}
        kind={filters.kind}
        queue={filters.queue}
        onFilter={setFilter}
        onClear={clearFilters}
        hasActiveFilter={hasActiveFilter}
        copy={copy}
      />

      {error ? (
        <div className="rounded-xl border border-[oklch(from_var(--destructive)_l_c_h_/_0.3)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.08)] px-4 py-3 text-[13px] text-fg-1">
          {t.admin.infra.ops.loadError}
        </div>
      ) : null}

      {isLoading ? (
        <JobsSkeleton />
      ) : jobs.length === 0 ? (
        <div className="rounded-2xl border border-border/60 bg-surface-1/40">
          <EmptyState>{copy.empty}</EmptyState>
        </div>
      ) : (
        <div className="animate-content-ready">
          <JobTable jobs={jobs} copy={copy} />
          <div className="mt-3 flex items-center justify-between gap-3">
            <span className="text-[11px] tabular-nums text-fg-3">
              {jobs.length}
              {hasNextPage ? "+" : ""}
            </span>
            {hasNextPage ? (
              <button
                type="button"
                onClick={() => fetchNextPage()}
                disabled={isFetchingNextPage}
                className="inline-flex items-center gap-1.5 rounded-md border border-border/60 px-3 py-1.5 text-[13px] font-medium text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg-1 disabled:opacity-50"
              >
                {isFetchingNextPage ? copy.loading : copy.loadMore}
              </button>
            ) : isFetching ? (
              <span className="text-[11px] text-fg-3">{copy.loading}</span>
            ) : null}
          </div>
        </div>
      )}
    </div>
  );
}

// Shared input styling matches /admin/hubs toolbar (rounded-lg
// bg-card, border-border/50, focus:border-fg-3). h-10 on all
// controls so the row visually aligns.
const FILTER_INPUT_CLASS =
  "h-10 w-full rounded-lg border border-border/50 bg-card px-3 text-[14px] text-fg-1 placeholder:text-fg-4 outline-none transition-colors focus:border-fg-3";

/**
 * FilterBar. Layout contract:
 *   - Mobile (< sm): each control on its own row, full width. Keeps
 *     every control thumb-reachable and hit-target friendly, and
 *     nothing wraps awkwardly to half a second row.
 *   - Desktop (sm+): inline row with consistent control heights.
 *     State dropdown + Kind + Queue + Clear. Kind / Queue are the
 *     primary free-text filters so they get more width than the
 *     state picker.
 *   - All controls share h-10 + rounded-lg + border-border/50 so
 *     they look like one coherent filter bar, not three mismatched
 *     widgets.
 *   - Kind / Queue inputs have a leading Filter icon (design-system
 *     cue that this is a filter field, not free text).
 */
function FilterBar({
  state,
  kind,
  queue,
  onFilter,
  onClear,
  hasActiveFilter,
  copy,
}: {
  state: string;
  kind: string;
  queue: string;
  onFilter: (key: "state" | "kind" | "queue", val: string) => void;
  onClear: () => void;
  hasActiveFilter: boolean;
  copy: OpsJobsCopy;
}) {
  return (
    <div className="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,180px)_minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center">
      {/* State — design-system Select. "all" sentinel maps to
          empty filter (Base UI doesn't accept "" as a value). */}
      <Select
        value={state || "all"}
        onValueChange={(v: string) => onFilter("state", v === "all" ? "" : v)}
      >
        <SelectTrigger
          className={FILTER_INPUT_CLASS}
          placeholder={copy.filters.anyState}
        >
          <SelectValue placeholder={copy.filters.anyState} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{copy.filters.anyState}</SelectItem>
          {ALL_STATES.map((s) => (
            <SelectItem key={s} value={s}>
              {copy.state[s]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <FilterTextInput
        value={kind}
        onChange={(v) => onFilter("kind", v)}
        placeholder={copy.filters.anyKind}
        ariaLabel={copy.filters.kind}
      />
      <FilterTextInput
        value={queue}
        onChange={(v) => onFilter("queue", v)}
        placeholder={copy.filters.anyQueue}
        ariaLabel={copy.filters.queue}
      />

      {hasActiveFilter ? (
        <button
          type="button"
          onClick={onClear}
          className="inline-flex h-10 items-center justify-center gap-1 rounded-lg border border-border/50 px-3 text-[13px] font-medium text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-1"
        >
          <X className="h-3.5 w-3.5" aria-hidden />
          {copy.filters.clear}
        </button>
      ) : (
        // Preserve the grid slot so State / Kind / Queue don't
        // resize when the clear button toggles.
        <span aria-hidden className="hidden sm:block" />
      )}
    </div>
  );
}

function FilterTextInput({
  value,
  onChange,
  placeholder,
  ariaLabel,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  ariaLabel: string;
}) {
  return (
    <div className="relative">
      <Filter
        className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-fg-4"
        aria-hidden
      />
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={ariaLabel}
        className={`${FILTER_INPUT_CLASS} pl-9 font-mono`}
      />
      {value ? (
        <button
          type="button"
          onClick={() => onChange("")}
          aria-label={`Clear ${ariaLabel}`}
          className="absolute right-2 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded text-fg-4 transition-colors hover:bg-surface-2 hover:text-fg-1"
        >
          <X className="h-3.5 w-3.5" aria-hidden />
        </button>
      ) : null}
    </div>
  );
}

function JobTable({
  jobs,
  copy,
}: {
  jobs: AdminOpsJobSummary[];
  copy: OpsJobsCopy;
}) {
  return (
    <>
      {/* Desktop: table-fixed with explicit percentage widths per
          column so the layout is stable regardless of the widest
          row's content. Without this, browsers inflate the Job
          column (the "expanding" column in table-auto) whenever
          one row has a long product_context label, leaving
          awkward whitespace next to short rows like "#3810".
          Percentages sum to 100; Job gets the largest share
          because it's where labels appear, but never more than
          36% — enough for most labels while keeping other columns
          readable. Mobile: card stack below. */}
      <div className="hidden overflow-hidden rounded-2xl border border-border/60 md:block">
        <table className="w-full table-fixed text-[13px]">
          <colgroup>
            <col className="w-[36%]" />
            <col className="w-[18%]" />
            <col className="w-[12%]" />
            <col className="w-[12%]" />
            <col className="w-[12%]" />
            <col className="w-[10%]" />
          </colgroup>
          <thead>
            <tr className="bg-surface-1/40 text-left text-[11px] uppercase tracking-wider text-fg-3">
              <th className="px-3.5 py-2 font-medium">{copy.table.job}</th>
              <th className="px-3.5 py-2 font-medium">{copy.table.kind}</th>
              <th className="px-3.5 py-2 font-medium">{copy.table.queue}</th>
              <th className="px-3.5 py-2 font-medium">{copy.table.state}</th>
              <th className="px-3.5 py-2 font-medium tabular-nums">
                {copy.table.attempt}
              </th>
              <th className="px-3.5 py-2 font-medium tabular-nums">
                {copy.table.created}
              </th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((j) => (
              <JobTableRow key={j.id} job={j} copy={copy} />
            ))}
          </tbody>
        </table>
      </div>
      <ul className="divide-y divide-border/40 overflow-hidden rounded-2xl border border-border/60 md:hidden">
        {jobs.map((j) => (
          <JobCard key={j.id} job={j} copy={copy} />
        ))}
      </ul>
    </>
  );
}

function JobTableRow({
  job,
  copy,
}: {
  job: AdminOpsJobSummary;
  copy: OpsJobsCopy;
}) {
  const router = useRouter();
  const stateLabel =
    copy.state[job.state as keyof typeof copy.state] ?? job.state;
  const label = job.product_context?.label;
  const href = `/admin/infra/jobs/${job.id}`;

  // Row-level click handler so any cell navigates, not just the
  // first one. We keep a real <Link> on the label so:
  //   - Cmd/Ctrl-click and right-click "Open in new tab" still work
  //     on the primary target
  //   - Keyboard Tab focuses a real link (screen readers announce
  //     it as the row's semantic target)
  //   - Shift-click for text selection inside the label is preserved
  // onClick bails if the user is selecting text or already clicking
  // a nested link/button, to avoid hijacking those interactions.
  const onRowClick = (e: React.MouseEvent<HTMLTableRowElement>) => {
    if (e.defaultPrevented) return;
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) {
      return;
    }
    const target = e.target as HTMLElement;
    if (target.closest("a, button, input, select, textarea")) return;
    if (window.getSelection()?.toString()) return;
    router.push(href);
  };

  return (
    <tr
      className="cursor-pointer border-t border-border/30 hover:bg-surface-1/60"
      onClick={onRowClick}
    >
      {/* First column: id + label with a real <Link>. The Link
          handles cmd-click / keyboard / right-click, while the row
          onClick covers clicks anywhere else in the row. The inner
          flex is capped with max-w so very long labels truncate
          rather than forcing the whole row wide. */}
      <td className="px-3.5 py-2">
        <Link
          href={href}
          className="flex min-w-0 flex-col rounded-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-fg-3/40"
          title={label ?? `Job ${job.id}`}
        >
          <div className="flex min-w-0 items-center gap-2">
            <span className="shrink-0 font-mono text-[12px] tabular-nums text-fg-3">
              #{job.id}
            </span>
            {label ? (
              <span className="min-w-0 truncate text-fg-1">{label}</span>
            ) : null}
          </div>
          {job.last_error ? (
            <div
              className="mt-0.5 min-w-0 truncate text-[11px] text-[oklch(from_var(--destructive)_l_c_h_/_0.9)]"
              title={job.last_error}
            >
              {job.last_error}
            </div>
          ) : null}
        </Link>
      </td>
      <td
        className="whitespace-nowrap px-3.5 py-2 font-mono text-[12px]"
        title={job.kind}
      >
        {job.kind}
      </td>
      <td
        className="whitespace-nowrap px-3.5 py-2 font-mono text-[12px]"
        title={job.queue}
      >
        {job.queue}
      </td>
      <td className="whitespace-nowrap px-3.5 py-2">
        <JobStatePill state={job.state} label={stateLabel} />
      </td>
      <td className="whitespace-nowrap px-3.5 py-2 tabular-nums">
        {interpolate(copy.detail.attemptOf, {
          attempt: job.attempt,
          max: job.max_attempts,
        })}
      </td>
      <td
        className="whitespace-nowrap px-3.5 py-2 tabular-nums text-fg-3"
        title={new Date(job.created_at).toLocaleString()}
      >
        {formatRelativeTime(job.created_at)}
      </td>
    </tr>
  );
}

function JobCard({
  job,
  copy,
}: {
  job: AdminOpsJobSummary;
  copy: OpsJobsCopy;
}) {
  const stateLabel =
    copy.state[job.state as keyof typeof copy.state] ?? job.state;
  const label = job.product_context?.label;
  return (
    <li>
      <Link
        href={`/admin/infra/jobs/${job.id}`}
        className="flex items-start gap-2.5 px-3.5 py-3 hover:bg-surface-1/60"
      >
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="font-mono text-[12px] tabular-nums text-fg-3">
              #{job.id}
            </span>
            <JobStatePill state={job.state} label={stateLabel} />
          </div>
          {/* min-w-0 + truncate so long titles don't push the
              chevron off-screen. title fallback via the full element
              value for desktop hover; mobile gets an indicator via
              the ChevronRight that tapping opens the full detail. */}
          <div
            className="mt-1 min-w-0 truncate text-[13px] text-fg-1"
            title={label}
          >
            {label ?? (
              <span className="font-mono text-[12px] text-fg-2">
                {job.kind}
              </span>
            )}
          </div>
          <div className="mt-0.5 flex flex-wrap gap-x-2 gap-y-0.5 text-[11px] text-fg-3 tabular-nums">
            <span className="max-w-[12ch] truncate" title={job.queue}>
              {job.queue}
            </span>
            <span aria-hidden>·</span>
            <span>
              {interpolate(copy.detail.attemptOf, {
                attempt: job.attempt,
                max: job.max_attempts,
              })}
            </span>
            <span aria-hidden>·</span>
            <span>{formatRelativeTime(job.created_at)}</span>
          </div>
          {job.last_error ? (
            <div
              className="mt-1 truncate text-[11px] text-[oklch(from_var(--destructive)_l_c_h_/_0.9)]"
              title={job.last_error}
            >
              {job.last_error}
            </div>
          ) : null}
        </div>
        <ChevronRight className="mt-1 h-4 w-4 shrink-0 text-fg-3" aria-hidden />
      </Link>
    </li>
  );
}

function JobsSkeleton() {
  return (
    <div className="space-y-2">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="h-14 animate-pulse rounded-xl bg-surface-2" />
      ))}
    </div>
  );
}
