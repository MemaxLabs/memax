"use client";

// =============================================================================
// /admin/notifications/super — plan 18 §7.3 super-notif funnel
// =============================================================================
//
// Operator-only page (route is admin-guarded). Renders the parent
// funnel (status × resolution histogram) AND the per-cohort item
// funnel for a chosen kind + optional source_kind + cohort window.
//
// All data comes through @/lib/admin-client/ — the admin surface
// never goes through memax-sdk per the admin boundary rule.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  adminSuperNotifFunnel,
  adminSuperNotifFunnelCsv,
  type AdminSuperNotifFunnel,
} from "@/lib/admin-client";

type SuperKind = "checklist" | "digest";

const DEFAULT_KIND: SuperKind = "checklist";
const DEFAULT_SOURCE_KIND = "onboarding";
const WINDOW_OPTIONS = [
  { value: 7, label: "Last 7 days" },
  { value: 30, label: "Last 30 days" },
  { value: 90, label: "Last 90 days" },
] as const;

export default function AdminSuperNotifFunnelPage() {
  const [kind, setKind] = useState<SuperKind>(DEFAULT_KIND);
  const [sourceKind, setSourceKind] = useState<string>(DEFAULT_SOURCE_KIND);
  const [windowDays, setWindowDays] = useState<number>(30);

  const query = useQuery<AdminSuperNotifFunnel>({
    queryKey: ["admin", "notifications", "super", kind, sourceKind, windowDays],
    queryFn: () =>
      adminSuperNotifFunnel({
        kind,
        sourceKind: sourceKind || undefined,
        windowDays,
      }),
    staleTime: 30_000,
  });

  return (
    <div className="mx-auto max-w-5xl px-6 py-10">
      <header className="mb-6 space-y-2">
        <h1 className="text-display-2 font-semibold text-fg-1">
          Super-notif funnel
        </h1>
        <p className="text-[13px] text-fg-3">
          Parent histogram + per-cohort item funnel for plan-18 super
          notifications. Source-kind narrows within a kind (e.g.
          <code className="mx-1 px-1 rounded bg-surface-2 text-[12px]">
            onboarding
          </code>{" "}
          under checklist).
        </p>
      </header>

      <FilterBar
        kind={kind}
        setKind={setKind}
        sourceKind={sourceKind}
        setSourceKind={setSourceKind}
        windowDays={windowDays}
        setWindowDays={setWindowDays}
        onExportCsv={() =>
          adminSuperNotifFunnelCsv({
            kind,
            sourceKind: sourceKind || undefined,
            windowDays,
          }).catch((e) => {
            // Surface the error so a 401/network failure doesn't
            // disappear silently. The page-level query has its own
            // error rendering; download errors stay in the console.
            console.error("super-notif CSV export failed", e);
          })
        }
      />

      {query.isLoading && (
        <p className="mt-6 text-[13px] text-fg-3">Loading…</p>
      )}
      {query.isError && (
        <p className="mt-6 text-[13px] text-red-500">
          {String((query.error as Error)?.message ?? "Failed to load funnel.")}
        </p>
      )}
      {query.data && <FunnelResults data={query.data} />}
    </div>
  );
}

function FilterBar({
  kind,
  setKind,
  sourceKind,
  setSourceKind,
  windowDays,
  setWindowDays,
  onExportCsv,
}: {
  kind: SuperKind;
  setKind: (k: SuperKind) => void;
  sourceKind: string;
  setSourceKind: (s: string) => void;
  windowDays: number;
  setWindowDays: (n: number) => void;
  onExportCsv: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-3 p-3 rounded-xl border border-border bg-surface-1">
      <label className="text-[13px] flex items-center gap-2">
        <span className="text-fg-3">Kind</span>
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as SuperKind)}
          className="px-2 py-1 rounded bg-surface-2 text-[13px]"
        >
          <option value="checklist">checklist</option>
          <option value="digest">digest</option>
        </select>
      </label>

      <label className="text-[13px] flex items-center gap-2">
        <span className="text-fg-3">Source kind</span>
        <input
          type="text"
          value={sourceKind}
          onChange={(e) => setSourceKind(e.target.value)}
          placeholder="(any)"
          className="px-2 py-1 rounded bg-surface-2 text-[13px] w-[180px]"
        />
      </label>

      <label className="text-[13px] flex items-center gap-2">
        <span className="text-fg-3">Window</span>
        <select
          value={windowDays}
          onChange={(e) => setWindowDays(Number(e.target.value))}
          className="px-2 py-1 rounded bg-surface-2 text-[13px]"
        >
          {WINDOW_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </label>

      <button
        type="button"
        onClick={onExportCsv}
        className="ml-auto px-3 py-1 rounded bg-surface-2 hover:bg-surface-3 text-[13px] text-fg-1 transition-colors"
      >
        Export CSV
      </button>
    </div>
  );
}

function FunnelResults({ data }: { data: AdminSuperNotifFunnel }) {
  const parentTotal = useMemo(
    () => data.parent.reduce((sum, row) => sum + row.count, 0),
    [data.parent],
  );
  return (
    <div className="mt-6 grid grid-cols-1 gap-6">
      <section>
        <h2 className="text-[15px] font-semibold text-fg-1 mb-3">
          Parent funnel — {data.window_days}d
        </h2>
        <div className="rounded-xl border border-border overflow-hidden">
          <table className="w-full text-[13px]">
            <thead className="bg-surface-2">
              <tr className="text-left">
                <th className="px-3 py-2 font-medium text-fg-3">Status</th>
                <th className="px-3 py-2 font-medium text-fg-3">Resolution</th>
                <th className="px-3 py-2 font-medium text-fg-3 text-right">
                  Count
                </th>
                <th className="px-3 py-2 font-medium text-fg-3 text-right">
                  Share
                </th>
              </tr>
            </thead>
            <tbody>
              {data.parent.length === 0 && (
                <tr>
                  <td
                    colSpan={4}
                    className="px-3 py-3 text-fg-4 italic text-center"
                  >
                    No rows in this window.
                  </td>
                </tr>
              )}
              {data.parent.map((row, i) => (
                <tr
                  key={`${row.status}-${row.resolution}-${i}`}
                  className="border-t border-border"
                >
                  <td className="px-3 py-2 text-fg-1 font-mono text-[12px]">
                    {row.status}
                  </td>
                  <td className="px-3 py-2 text-fg-2 font-mono text-[12px]">
                    {row.resolution || "—"}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">
                    {row.count}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums text-fg-3">
                    {parentTotal > 0
                      ? `${Math.round((row.count / parentTotal) * 100)}%`
                      : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2 className="text-[15px] font-semibold text-fg-1 mb-3">
          Item funnel — {data.items.length} rows
        </h2>
        <div className="rounded-xl border border-border overflow-hidden">
          <table className="w-full text-[13px]">
            <thead className="bg-surface-2">
              <tr className="text-left">
                <th className="px-3 py-2 font-medium text-fg-3">Cohort day</th>
                <th className="px-3 py-2 font-medium text-fg-3">Item</th>
                <th className="px-3 py-2 font-medium text-fg-3 text-right">
                  Recipients
                </th>
                <th className="px-3 py-2 font-medium text-fg-3 text-right">
                  Viewed
                </th>
                <th className="px-3 py-2 font-medium text-fg-3 text-right">
                  Completed
                </th>
                <th className="px-3 py-2 font-medium text-fg-3 text-right">
                  Completion %
                </th>
              </tr>
            </thead>
            <tbody>
              {data.items.length === 0 && (
                <tr>
                  <td
                    colSpan={6}
                    className="px-3 py-3 text-fg-4 italic text-center"
                  >
                    No item rows in this window.
                  </td>
                </tr>
              )}
              {data.items.map((row, i) => {
                const pct =
                  row.recipients > 0
                    ? Math.round((row.completed / row.recipients) * 100)
                    : 0;
                return (
                  <tr
                    key={`${row.cohort_day}-${row.item_id}-${i}`}
                    className="border-t border-border"
                  >
                    <td className="px-3 py-2 text-fg-2 font-mono text-[12px]">
                      {row.cohort_day}
                    </td>
                    <td className="px-3 py-2 text-fg-1 font-mono text-[12px]">
                      {row.item_id}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      {row.recipients}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      {row.viewed}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      {row.completed}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums text-fg-3">
                      {pct}%
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
