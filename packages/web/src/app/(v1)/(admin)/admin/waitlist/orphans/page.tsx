"use client";

import { useCallback, useMemo, useState } from "react";
import Link from "next/link";
import {
  ArrowLeft,
  RefreshCw,
  ExternalLink,
  CheckCircle2,
  AlertCircle,
  CircleSlash,
} from "lucide-react";
import { Button, Badge, Skeleton } from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import {
  useAdminOrphans,
  useReconcileOrphans,
} from "@/hooks/use-admin-orphans";
import type {
  AdminOrphanCandidate,
  AdminReconcileItemResult,
} from "@/lib/admin-client/types";

// Default scan window: 60 days covers the 2026-04 regression
// (Apr 14–20) plus any straggler reports. Operators can widen
// by editing the input — server caps LIMIT at 500.
const DEFAULT_SCAN_DAYS = 60;
const DEFAULT_LIMIT = 200;

/**
 * Operator surface for repairing the 2026-04 orphan cohort.
 *
 * Flow:
 *   1. Page lands → orphan list fetched with a sensible since window.
 *   2. Operator reviews, optionally toggles checkboxes.
 *   3. Click "Reconcile N" → POST /v1/admin/waitlist/reconcile.
 *   4. Response's per-user results render in the same table with
 *      action badges (reconciled / skipped / error). Successful
 *      rows invalidate the list cache → auto-disappear. Failed
 *      rows stay so the operator can investigate.
 *
 * Crosslinks:
 *   - Back arrow → /admin/waitlist
 *   - Email column → /admin/users/{user_id} (new tab) for deep
 *     inspection of plan state / usage / hubs.
 */
export default function OrphansPage() {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const defaultSince = useMemo(
    () =>
      new Date(Date.now() - DEFAULT_SCAN_DAYS * 24 * 60 * 60 * 1000)
        .toISOString()
        .slice(0, 10),
    [],
  );

  // since is stored as an ISO date (YYYY-MM-DD) for the input; the
  // hook gets the full RFC3339 at midnight UTC.
  const [sinceDate, setSinceDate] = useState(defaultSince);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [lastResults, setLastResults] = useState<
    Map<string, AdminReconcileItemResult>
  >(new Map());

  const sinceRFC = `${sinceDate}T00:00:00Z`;
  const { data, isLoading, isError, refetch, isFetching } = useAdminOrphans({
    since: sinceRFC,
    limit: DEFAULT_LIMIT,
  });
  const reconcile = useReconcileOrphans();

  const candidates = useMemo(() => data?.candidates ?? [], [data?.candidates]);
  const selectedCount = selectedIds.size;

  const toggleSelect = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const allSelected =
    candidates.length > 0 &&
    candidates.every((c) => selectedIds.has(c.user_id));

  const toggleSelectAll = useCallback(() => {
    if (allSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(candidates.map((c) => c.user_id)));
    }
  }, [allSelected, candidates]);

  const handleReconcile = useCallback(() => {
    const ids = Array.from(selectedIds);
    if (ids.length === 0) return;
    reconcile.mutate(
      { user_ids: ids },
      {
        onSuccess: (resp) => {
          const next = new Map<string, AdminReconcileItemResult>();
          for (const r of resp.results) next.set(r.user_id, r);
          setLastResults(next);
          setSelectedIds(new Set());
        },
      },
    );
  }, [selectedIds, reconcile]);

  return (
    <div className="px-4 sm:px-6 md:px-8 py-6 md:py-8 max-w-6xl">
      {/* Header */}
      <div className="mb-6">
        <Link
          href="/admin/waitlist"
          className="inline-flex items-center gap-1.5 text-[13px] text-fg-3 hover:text-fg-1 transition-colors duration-[var(--dur-fast)] ease-[var(--ease-spring)] mb-3"
        >
          <ArrowLeft className="h-3.5 w-3.5" aria-hidden />
          {t.admin.waitlist.orphans.backToWaitlist}
        </Link>
        <h1 className="text-xl font-semibold tracking-tight text-fg-1">
          {t.admin.waitlist.orphans.title}
        </h1>
        <p className="mt-1 text-[14px] text-fg-3 max-w-2xl">
          {t.admin.waitlist.orphans.description}
        </p>
      </div>

      {/* Toolbar */}
      <div className="mb-4 flex flex-col sm:flex-row sm:items-end gap-3">
        <div className="flex flex-col gap-1">
          <label
            htmlFor="orphans-since"
            className="text-[12px] font-medium text-fg-2"
          >
            {t.admin.waitlist.orphans.sinceLabel}
          </label>
          <input
            id="orphans-since"
            type="date"
            value={sinceDate}
            onChange={(e) => setSinceDate(e.target.value)}
            className="h-9 rounded-lg border border-border/50 bg-card px-3 text-[13px] text-fg-1 outline-none focus:border-fg-3 transition-colors"
          />
          <p className="text-[11px] text-fg-4 max-w-sm">
            {t.admin.waitlist.orphans.sinceHint}
          </p>
        </div>
        <div className="flex items-center gap-2 sm:ml-auto">
          <Button
            variant="outline"
            size="sm"
            onClick={() => refetch()}
            disabled={isFetching}
            className="gap-1.5"
          >
            <RefreshCw
              className={`h-3.5 w-3.5 ${isFetching ? "animate-spin" : ""}`}
              aria-hidden
            />
            {t.admin.waitlist.orphans.refresh}
          </Button>
          {selectedCount > 0 && (
            <>
              <span className="text-[13px] text-fg-3 tabular-nums">
                {interpolate(t.admin.waitlist.orphans.selectedCount, {
                  count: String(selectedCount),
                })}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setSelectedIds(new Set())}
              >
                {t.admin.waitlist.orphans.clearSelection}
              </Button>
              <Button
                size="sm"
                onClick={handleReconcile}
                disabled={reconcile.isPending}
              >
                {reconcile.isPending
                  ? t.admin.waitlist.orphans.reconcileRunning
                  : interpolate(t.admin.waitlist.orphans.reconcileSelected, {
                      count: String(selectedCount),
                    })}
              </Button>
            </>
          )}
        </div>
      </div>

      {/* Reconcile result summary — visible only for the most
          recent POST. Stays until the next reconcile or refetch. */}
      {reconcile.isSuccess && reconcile.data && (
        <ReconcileSummary
          reconciled={reconcile.data.reconciled}
          skipped={reconcile.data.skipped}
          errored={reconcile.data.errored}
          template={t.admin.waitlist.orphans.reconcileSuccess}
          interpolate={interpolate}
        />
      )}

      {/* Content */}
      {isLoading ? (
        <TableSkeleton />
      ) : isError ? (
        <ErrorState />
      ) : candidates.length === 0 ? (
        <EmptyState />
      ) : (
        <OrphanTable
          candidates={candidates}
          selectedIds={selectedIds}
          toggleSelect={toggleSelect}
          toggleSelectAll={toggleSelectAll}
          allSelected={allSelected}
          lastResults={lastResults}
        />
      )}
    </div>
  );
}

function ReconcileSummary({
  reconciled,
  skipped,
  errored,
  template,
  interpolate,
}: {
  reconciled: number;
  skipped: number;
  errored: number;
  template: string;
  interpolate: (s: string, v: Record<string, string>) => string;
}) {
  // Amber when any errors, neutral otherwise.
  const hasErrors = errored > 0;
  return (
    <div
      className={`mb-4 flex items-center gap-2 rounded-surface border px-4 py-3 text-[13px] ${
        hasErrors
          ? "border-amber-500/30 bg-amber-500/[0.06] text-fg-1"
          : "border-border/50 bg-surface-1 text-fg-2"
      }`}
    >
      {hasErrors ? (
        <AlertCircle
          className="h-4 w-4 text-amber-600 dark:text-amber-400 shrink-0"
          aria-hidden
        />
      ) : (
        <CheckCircle2
          className="h-4 w-4 text-emerald-600 dark:text-emerald-400 shrink-0"
          aria-hidden
        />
      )}
      <span>
        {interpolate(template, {
          reconciled: String(reconciled),
          skipped: String(skipped),
          errored: String(errored),
        })}
      </span>
    </div>
  );
}

function OrphanTable({
  candidates,
  selectedIds,
  toggleSelect,
  toggleSelectAll,
  allSelected,
  lastResults,
}: {
  candidates: AdminOrphanCandidate[];
  selectedIds: Set<string>;
  toggleSelect: (id: string) => void;
  toggleSelectAll: () => void;
  allSelected: boolean;
  lastResults: Map<string, AdminReconcileItemResult>;
}) {
  const { t } = useLocale();

  return (
    <div className="overflow-x-auto rounded-surface border border-border/50 bg-card animate-content-ready">
      <table className="w-full min-w-[720px] border-collapse">
        <thead>
          <tr className="border-b border-border/50 text-[12px] font-medium uppercase tracking-wider text-fg-3">
            <th className="px-3 py-2 text-left w-10">
              <input
                type="checkbox"
                checked={allSelected}
                onChange={toggleSelectAll}
                aria-label={t.admin.waitlist.orphans.selectAll}
                className="h-3.5 w-3.5 cursor-pointer"
              />
            </th>
            <th className="px-3 py-2 text-left">
              {t.admin.waitlist.orphans.columnEmail}
            </th>
            <th className="px-3 py-2 text-left hidden md:table-cell">
              {t.admin.waitlist.orphans.columnPlan}
            </th>
            <th className="px-3 py-2 text-left hidden lg:table-cell">
              {t.admin.waitlist.orphans.columnCreatedAt}
            </th>
            <th className="px-3 py-2 text-left hidden lg:table-cell">
              {t.admin.waitlist.orphans.columnInviteExpires}
            </th>
            <th className="px-3 py-2 text-right">
              {t.admin.waitlist.orphans.columnActions}
            </th>
          </tr>
        </thead>
        <tbody>
          {candidates.map((c) => {
            const result = lastResults.get(c.user_id);
            return (
              <OrphanRow
                key={c.user_id}
                candidate={c}
                selected={selectedIds.has(c.user_id)}
                onToggle={() => toggleSelect(c.user_id)}
                result={result}
              />
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function OrphanRow({
  candidate,
  selected,
  onToggle,
  result,
}: {
  candidate: AdminOrphanCandidate;
  selected: boolean;
  onToggle: () => void;
  result?: AdminReconcileItemResult;
}) {
  const { t } = useLocale();

  return (
    <tr className="border-b border-border/30 last:border-b-0 hover:bg-surface-1 transition-colors">
      <td className="px-3 py-3">
        <input
          type="checkbox"
          checked={selected}
          onChange={onToggle}
          aria-label={t.admin.waitlist.orphans.reconcileOne}
          className="h-3.5 w-3.5 cursor-pointer"
        />
      </td>
      <td className="px-3 py-3 min-w-0">
        <Link
          href={`/admin/users/${candidate.user_id}`}
          className="inline-flex items-center gap-1.5 text-[13px] font-medium text-fg-1 hover:text-fg-2 transition-colors break-all"
          target="_blank"
          rel="noopener"
        >
          <span className="truncate">{candidate.user_email}</span>
          <ExternalLink className="h-3 w-3 shrink-0 text-fg-4" aria-hidden />
        </Link>
      </td>
      <td className="px-3 py-3 hidden md:table-cell">
        <Badge variant="outline" className="text-[11px]">
          {candidate.user_plan_id || "—"}
        </Badge>
      </td>
      <td className="px-3 py-3 hidden lg:table-cell text-[12px] text-fg-3 tabular-nums">
        {formatDate(candidate.user_created_at)}
      </td>
      <td className="px-3 py-3 hidden lg:table-cell text-[12px] text-fg-3 tabular-nums">
        {formatDate(candidate.invite_expires_at)}
      </td>
      <td className="px-3 py-3 text-right">
        {result ? <ResultBadge result={result} /> : null}
      </td>
    </tr>
  );
}

function ResultBadge({ result }: { result: AdminReconcileItemResult }) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const reason = resultReasonCopy(result.reason, t);

  if (result.action === "reconciled") {
    return (
      <div className="inline-flex flex-col items-end gap-0.5">
        <span className="inline-flex items-center gap-1 text-[12px] font-medium text-emerald-700 dark:text-emerald-400">
          <CheckCircle2 className="h-3 w-3" aria-hidden />
          {t.admin.waitlist.orphans.resultReconciled}
        </span>
        {result.before_plan && result.after_plan && (
          <span className="text-[11px] text-fg-4 tabular-nums">
            {interpolate(t.admin.waitlist.orphans.planFlowLabel, {
              before: result.before_plan,
              after: result.after_plan,
            })}
          </span>
        )}
      </div>
    );
  }
  if (result.action === "error") {
    return (
      <div className="inline-flex flex-col items-end gap-0.5">
        <span className="inline-flex items-center gap-1 text-[12px] font-medium text-amber-700 dark:text-amber-400">
          <AlertCircle className="h-3 w-3" aria-hidden />
          {t.admin.waitlist.orphans.resultError}
        </span>
        {reason && (
          <span className="text-[11px] text-fg-4 text-right max-w-[200px]">
            {reason}
          </span>
        )}
      </div>
    );
  }
  return (
    <div className="inline-flex flex-col items-end gap-0.5">
      <span className="inline-flex items-center gap-1 text-[12px] text-fg-3">
        <CircleSlash className="h-3 w-3" aria-hidden />
        {t.admin.waitlist.orphans.resultSkipped}
      </span>
      {reason && (
        <span className="text-[11px] text-fg-4 text-right max-w-[200px]">
          {reason}
        </span>
      )}
    </div>
  );
}

function resultReasonCopy(
  reason: string | undefined,
  t: ReturnType<typeof useLocale>["t"],
): string | null {
  if (!reason) return null;
  switch (reason) {
    case "not_an_orphan":
      return t.admin.waitlist.orphans.reasonNotOrphan;
    case "already_upgraded":
      return t.admin.waitlist.orphans.reasonAlreadyUpgraded;
    case "duplicate_in_batch":
      return t.admin.waitlist.orphans.reasonDuplicateInBatch;
    case "user_not_found":
      return t.admin.waitlist.orphans.reasonUserNotFound;
    case "plan_upgrade_did_not_apply":
      return t.admin.waitlist.orphans.reasonPlanUpgradeFailed;
    case "post_consume_user_lookup_failed":
      return t.admin.waitlist.orphans.reasonPostConsumeLookupFailed;
    default:
      return t.admin.waitlist.orphans.reasonGeneric;
  }
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  } catch {
    return iso;
  }
}

function TableSkeleton() {
  return (
    <div
      className="rounded-surface border border-border/50 bg-card overflow-hidden"
      aria-hidden
    >
      {Array.from({ length: 4 }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-3 px-3 py-3 border-b border-border/30 last:border-b-0"
        >
          <Skeleton className="h-3.5 w-3.5 shrink-0" />
          <Skeleton className="h-3 w-48 sm:w-64" />
          <Skeleton className="h-3 w-20 hidden md:block" />
          <Skeleton className="h-3 w-24 hidden lg:block ml-auto" />
        </div>
      ))}
    </div>
  );
}

function EmptyState() {
  const { t } = useLocale();
  return (
    <div className="rounded-surface border border-border/50 bg-card px-6 py-10 text-center">
      <p className="text-[14px] font-medium text-fg-2">
        {t.admin.waitlist.orphans.emptyTitle}
      </p>
      <p className="mt-1 text-[13px] text-fg-3 max-w-md mx-auto">
        {t.admin.waitlist.orphans.emptyBody}
      </p>
    </div>
  );
}

function ErrorState() {
  const { t } = useLocale();
  return (
    <div className="rounded-surface border border-amber-500/30 bg-amber-500/[0.06] px-6 py-10 text-center">
      <p className="text-[14px] font-medium text-fg-1">
        {t.admin.waitlist.orphans.errorTitle}
      </p>
      <p className="mt-1 text-[13px] text-fg-3">
        {t.admin.waitlist.orphans.errorBody}
      </p>
    </div>
  );
}
