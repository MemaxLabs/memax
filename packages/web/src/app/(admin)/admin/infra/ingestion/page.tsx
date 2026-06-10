"use client";

import { useState } from "react";
import { AlertTriangle, Clock, Link as LinkIcon } from "lucide-react";
import Link from "next/link";
import { useLocale, interpolate } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import {
  useAdminOpsIngestion,
  useForceActiveMemory,
} from "@/hooks/use-admin-ops";
import type {
  AdminOpsFailedIngestion,
  AdminOpsIngestionBySource,
  AdminOpsStuckMemory,
} from "@/lib/admin-client";
import {
  EmptyState,
  OpsSection,
  formatRelativeTime,
} from "@/components/admin/ops-components";

export default function AdminInfraIngestionPage() {
  const { t } = useLocale();
  const copy = t.admin.infra.ingestion;

  const { data, isLoading, error } = useAdminOpsIngestion();

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-[16px] font-semibold text-fg-1">{copy.title}</h2>
        <p className="mt-1 max-w-2xl text-[13px] text-fg-3">
          {copy.description}
        </p>
      </div>

      {error ? (
        <div className="rounded-xl border border-[oklch(from_var(--destructive)_l_c_h_/_0.3)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.08)] px-4 py-3 text-[13px] text-fg-1">
          {t.admin.infra.ops.loadError}
        </div>
      ) : null}

      {isLoading ? (
        <IngestionSkeleton />
      ) : data ? (
        <div className="animate-content-ready space-y-6">
          <StuckList memories={data.stuck_memories} copy={copy.stuck} />
          <FailedList failed={data.failed_ingestions} copy={copy.failed} />
          <BySourceTable
            rows={data.by_source}
            windowMinutes={data.window_minutes}
            copy={copy.bySource}
          />
        </div>
      ) : null}
    </div>
  );
}

function StuckList({
  memories,
  copy,
}: {
  memories: AdminOpsStuckMemory[];
  copy: Translations["admin"]["infra"]["ingestion"]["stuck"];
}) {
  if (memories.length === 0) {
    return (
      <OpsSection title={copy.title}>
        <EmptyState>{copy.empty}</EmptyState>
      </OpsSection>
    );
  }
  return (
    <OpsSection title={copy.title}>
      <ul className="divide-y divide-border/40">
        {memories.map((m) => (
          <StuckRow key={m.id} memory={m} copy={copy} />
        ))}
      </ul>
    </OpsSection>
  );
}

function StuckRow({
  memory,
  copy,
}: {
  memory: AdminOpsStuckMemory;
  copy: Translations["admin"]["infra"]["ingestion"]["stuck"];
}) {
  const [mode, setMode] = useState<"view" | "confirming">("view");
  const [note, setNote] = useState("");
  const mutation = useForceActiveMemory();
  const title = memory.title || copy.noTitle;

  const handleForceActive = () => {
    mutation.mutate(
      { id: memory.id, note: note.trim() || undefined },
      {
        onSuccess: () => {
          setMode("view");
          setNote("");
        },
      },
    );
  };

  return (
    <li
      className={`transition-colors ${
        mode === "confirming"
          ? "bg-[oklch(from_var(--destructive)_l_c_h_/_0.04)]"
          : ""
      }`}
    >
      <div className="px-3.5 py-3">
        <div className="flex items-start gap-2.5">
          <AlertTriangle
            className="mt-0.5 h-4 w-4 shrink-0 text-[oklch(from_var(--warning)_l_c_h_/_0.9)]"
            aria-hidden
          />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
              <span className="truncate text-[14px] font-medium text-fg-1">
                {title}
              </span>
              <span className="text-[11px] text-fg-3 tabular-nums">
                <Clock className="mr-0.5 inline h-3 w-3" aria-hidden />
                {interpolate(copy.age, {
                  duration: formatRelativeTime(memory.created_at),
                })}
              </span>
            </div>
            <div className="mt-0.5 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-fg-3">
              <span>
                {copy.source}:{" "}
                <span className="font-mono">{memory.source}</span>
                {memory.source_agent ? ` (${memory.source_agent})` : ""}
              </span>
              <span>
                {copy.contentType}:{" "}
                <span className="font-mono">{memory.content_type}</span>
              </span>
              {memory.associated_job ? (
                <Link
                  href={`/admin/infra/jobs/${memory.associated_job.id}`}
                  className="inline-flex items-center gap-1 text-fg-2 hover:text-fg-1"
                >
                  <LinkIcon className="h-3 w-3" aria-hidden />
                  {copy.actions.viewJob}
                </Link>
              ) : null}
            </div>
          </div>
        </div>

        {/* Action row — container-morph confirmation. Clicking
            Force active puts the row in "confirming" state where
            the primary button is [Force active] on the LEFT and
            [Keep] on the RIGHT. Design-skill rule: destructive
            left, safe right, so thumb momentum after tapping
            "Force active" lands on Keep by default. */}
        {mode === "view" ? (
          <div className="mt-2.5 flex justify-end">
            <button
              type="button"
              onClick={() => setMode("confirming")}
              className="inline-flex items-center rounded-md border border-border/60 px-2.5 py-1 text-[12px] font-medium text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg-1"
            >
              {copy.actions.forceActive}
            </button>
          </div>
        ) : (
          <div className="mt-3 space-y-2">
            <label className="block">
              <span className="block text-[11px] font-medium uppercase tracking-wider text-fg-3">
                {copy.forceActivePrompt}
              </span>
              <input
                type="text"
                value={note}
                onChange={(e) => setNote(e.target.value)}
                placeholder={copy.forceActivePromptPlaceholder}
                className="mt-1 block w-full rounded-md border border-border/60 bg-surface-1 px-2.5 py-1.5 text-[13px] text-fg-1 placeholder:text-fg-3 focus:border-fg-3 focus:outline-none"
                autoFocus
              />
            </label>
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={handleForceActive}
                disabled={mutation.isPending}
                className="inline-flex items-center rounded-md border border-[oklch(from_var(--destructive)_l_c_h_/_0.4)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.1)] px-2.5 py-1 text-[12px] font-medium text-fg-1 transition-colors hover:bg-[oklch(from_var(--destructive)_l_c_h_/_0.15)] disabled:opacity-50"
              >
                {copy.actions.forceActiveConfirm}
              </button>
              <button
                type="button"
                onClick={() => {
                  setMode("view");
                  setNote("");
                }}
                className="inline-flex items-center rounded-md border border-border/60 px-2.5 py-1 text-[12px] font-medium text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg-1"
              >
                {copy.actions.keep}
              </button>
            </div>
          </div>
        )}
      </div>
    </li>
  );
}

function FailedList({
  failed,
  copy,
}: {
  failed: AdminOpsFailedIngestion[];
  copy: Translations["admin"]["infra"]["ingestion"]["failed"];
}) {
  if (failed.length === 0) {
    return (
      <OpsSection title={copy.title} description={copy.description}>
        <EmptyState>{copy.empty}</EmptyState>
      </OpsSection>
    );
  }
  return (
    <OpsSection title={copy.title} description={copy.description}>
      <ul className="divide-y divide-border/40">
        {failed.map((f) => (
          <li key={f.id} className="px-3.5 py-3">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <span className="text-[14px] font-medium text-fg-1">
                {f.title || copy.untitled}
              </span>
              <span className="text-[11px] text-fg-3 tabular-nums">
                {interpolate(copy.age, {
                  ago: `${formatRelativeTime(f.created_at)} ${copy.agoSuffix}`,
                })}
              </span>
            </div>
            <div className="mt-1 rounded-md bg-surface-2/60 px-2.5 py-1.5 font-mono text-[12px] leading-relaxed text-fg-2">
              {f.summary}
            </div>
          </li>
        ))}
      </ul>
    </OpsSection>
  );
}

function BySourceTable({
  rows,
  windowMinutes,
  copy,
}: {
  rows: AdminOpsIngestionBySource[];
  windowMinutes: number;
  copy: Translations["admin"]["infra"]["ingestion"]["bySource"];
}) {
  return (
    <OpsSection
      title={copy.title}
      description={copy.description}
      action={
        <span className="text-[11px] text-fg-3 tabular-nums">
          {interpolate(copy.windowSuffix, { minutes: windowMinutes })}
        </span>
      }
    >
      {rows.length === 0 ? (
        <EmptyState>{copy.empty}</EmptyState>
      ) : (
        // Mobile: card stack. Desktop: true table. Same data,
        // different layouts, both driven by the same `rows` prop.
        <>
          <table className="hidden w-full table-fixed text-[13px] md:table">
            <thead>
              <tr className="text-left text-[11px] uppercase tracking-wider text-fg-3">
                <th className="px-3.5 py-2 font-medium">
                  {copy.columns.source}
                </th>
                <th className="px-3.5 py-2 font-medium tabular-nums">
                  {copy.columns.created}
                </th>
                <th className="px-3.5 py-2 font-medium tabular-nums">
                  {copy.columns.processing}
                </th>
                <th className="px-3.5 py-2 font-medium tabular-nums">
                  {copy.columns.failed}
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.source} className="border-t border-border/30">
                  <td className="px-3.5 py-2 font-mono">{r.source}</td>
                  <td className="px-3.5 py-2 tabular-nums">
                    {r.memories_created}
                  </td>
                  <td className="px-3.5 py-2 tabular-nums text-fg-2">
                    {r.still_processing}
                  </td>
                  <td
                    className={`px-3.5 py-2 tabular-nums ${
                      r.failed_silent > 0
                        ? "text-[oklch(from_var(--destructive)_l_c_h_/_0.9)]"
                        : "text-fg-2"
                    }`}
                  >
                    {r.failed_silent}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <ul className="divide-y divide-border/40 md:hidden">
            {rows.map((r) => (
              <li
                key={r.source}
                className="flex items-baseline justify-between px-3.5 py-2.5 text-[13px]"
              >
                <span className="font-mono text-fg-1">{r.source}</span>
                <span className="text-[11px] text-fg-3 tabular-nums">
                  {interpolate(copy.mobileSummary, {
                    created: r.memories_created,
                    processing: r.still_processing,
                    failed: r.failed_silent,
                  })}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </OpsSection>
  );
}

function IngestionSkeleton() {
  return (
    <div className="space-y-6">
      {Array.from({ length: 3 }).map((_, i) => (
        <div key={i} className="h-40 animate-pulse rounded-2xl bg-surface-2" />
      ))}
    </div>
  );
}
