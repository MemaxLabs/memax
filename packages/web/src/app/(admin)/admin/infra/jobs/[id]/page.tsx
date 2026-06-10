"use client";

import { use, useState } from "react";
import Link from "next/link";
import { ArrowLeft, Copy, Check } from "lucide-react";
import { useLocale, interpolate } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import {
  useAdminOpsJob,
  useRetryJob,
  useCancelJob,
} from "@/hooks/use-admin-ops";
import type { AdminOpsJobDetail } from "@/lib/admin-client";
import {
  JobStatePill,
  OpsSection,
  formatRelativeTime,
} from "@/components/admin/ops-components";
import { JobLogsPanel } from "@/components/admin/job-logs-panel";

type JobsCopy = Translations["admin"]["infra"]["jobs"];

export default function AdminInfraJobDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const jobId = Number(id);
  const { t } = useLocale();
  const copy = t.admin.infra.jobs;

  const { data, isLoading, error } = useAdminOpsJob(jobId);

  return (
    <div className="space-y-5">
      <div>
        <Link
          href="/admin/infra/jobs"
          className="inline-flex items-center gap-1 text-[13px] font-medium text-fg-3 transition-colors hover:text-fg-1"
        >
          <ArrowLeft className="h-3.5 w-3.5" aria-hidden />
          {copy.detail.backToList}
        </Link>
      </div>

      {isLoading ? (
        <DetailSkeleton />
      ) : error ? (
        <div className="rounded-xl border border-[oklch(from_var(--destructive)_l_c_h_/_0.3)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.08)] px-4 py-3 text-[13px] text-fg-1">
          {t.admin.infra.ops.loadError}
        </div>
      ) : data ? (
        <JobDetailView job={data} copy={copy} />
      ) : null}
    </div>
  );
}

function JobDetailView({
  job,
  copy,
}: {
  job: AdminOpsJobDetail;
  copy: JobsCopy;
}) {
  const retry = useRetryJob();
  const cancel = useCancelJob();
  const [actionMode, setActionMode] = useState<"view" | "retry" | "cancel">(
    "view",
  );

  // Only allow actions if server reports River is wired. We don't
  // have an explicit flag for that — but the handler returns 503 on
  // all action routes when river is nil, so the mutation's error
  // callback handles messaging. The UX choice is to show the
  // buttons always and let errors surface through the global toast
  // pipeline.

  const title = interpolate(copy.detail.title, { id: job.id });

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <h2 className="text-[18px] font-semibold tabular-nums text-fg-1">
            {title}
          </h2>
          {job.product_context?.label ? (
            <p className="mt-0.5 text-[14px] text-fg-2">
              {job.product_context.label}
            </p>
          ) : null}
          {job.product_context?.sub_label ? (
            <p className="mt-0.5 text-[12px] text-fg-3">
              {job.product_context.sub_label}
            </p>
          ) : null}
        </div>
        <JobStatePill
          state={job.state}
          label={copy.state[job.state as keyof typeof copy.state] ?? job.state}
        />
      </div>

      {/* Summary grid */}
      <OpsSection title={copy.detail.sections.summary}>
        <div className="grid grid-cols-2 divide-border/40 text-[13px] md:grid-cols-4">
          <SummaryField
            label={copy.table.kind}
            value={<span className="font-mono">{job.kind}</span>}
          />
          <SummaryField
            label={copy.table.queue}
            value={<span className="font-mono">{job.queue}</span>}
          />
          <SummaryField
            label={copy.table.attempt}
            value={interpolate(copy.detail.attemptOf, {
              attempt: job.attempt,
              max: job.max_attempts,
            })}
          />
          <SummaryField
            label={copy.table.created}
            value={formatRelativeTime(job.created_at)}
          />
        </div>
      </OpsSection>

      {/* Args JSON */}
      <OpsSection
        title={copy.detail.sections.args}
        action={
          <CopyButton
            text={JSON.stringify(job.args, null, 2)}
            copy={copy.detail}
          />
        }
      >
        <pre className="max-h-96 overflow-auto rounded-b-2xl bg-surface-2/40 px-3.5 py-2.5 font-mono text-[12px] leading-relaxed text-fg-1">
          {JSON.stringify(job.args, null, 2)}
        </pre>
      </OpsSection>

      {/* Logs — live tail from Loki scoped to this job's id + env. */}
      <JobLogsPanel jobId={job.id} />

      {/* Errors */}
      <OpsSection title={copy.detail.sections.errors}>
        {job.errors.length === 0 ? (
          <div className="px-4 py-6 text-center text-[13px] text-fg-3">
            {copy.detail.noErrors}
          </div>
        ) : (
          <ul className="divide-y divide-border/40">
            {job.errors.map((e, i) => (
              <li key={i} className="px-3.5 py-3">
                <div className="mb-1 flex items-center justify-between text-[11px] text-fg-3 tabular-nums">
                  <span>
                    {interpolate(copy.detail.attemptOf, {
                      attempt: e.attempt,
                      max: job.max_attempts,
                    })}
                  </span>
                  <span>{formatRelativeTime(e.at)}</span>
                </div>
                <div className="font-mono text-[12px] leading-relaxed text-[oklch(from_var(--destructive)_l_c_h_/_0.9)]">
                  {e.error}
                </div>
                {e.trace ? (
                  <pre className="mt-1 max-h-48 overflow-auto rounded-md bg-surface-2/40 px-2 py-1.5 font-mono text-[11px] text-fg-3">
                    {e.trace}
                  </pre>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </OpsSection>

      {/* Actions — container-morph confirm */}
      <div className="flex flex-wrap justify-end gap-2">
        {actionMode === "view" ? (
          <>
            <button
              type="button"
              onClick={() => setActionMode("cancel")}
              disabled={cancel.isPending || isTerminal(job.state)}
              className="inline-flex items-center rounded-md border border-border/60 px-3 py-1.5 text-[13px] font-medium text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg-1 disabled:opacity-40"
            >
              {copy.actions.cancel}
            </button>
            <button
              type="button"
              onClick={() => setActionMode("retry")}
              disabled={retry.isPending}
              className="inline-flex items-center rounded-md border border-border/60 bg-surface-1 px-3 py-1.5 text-[13px] font-medium text-fg-1 transition-colors hover:bg-surface-2 disabled:opacity-40"
            >
              {copy.actions.retry}
            </button>
          </>
        ) : actionMode === "retry" ? (
          // Retry is a positive action but NOT "memax is working
          // right now" — neutral chrome. Container morph communicates
          // intent via layout, not color.
          <div className="flex w-full items-center justify-end gap-2 rounded-xl border border-border/60 bg-surface-1/60 px-3.5 py-2.5">
            <button
              type="button"
              onClick={() => {
                retry.mutate(job.id, {
                  onSettled: () => setActionMode("view"),
                });
              }}
              disabled={retry.isPending}
              className="inline-flex items-center rounded-md border border-border/60 bg-surface-2 px-2.5 py-1 text-[12px] font-medium text-fg-1 transition-colors hover:bg-surface-2/80 disabled:opacity-50"
            >
              {copy.actions.retryConfirm}
            </button>
            <button
              type="button"
              onClick={() => setActionMode("view")}
              className="inline-flex items-center rounded-md border border-border/60 px-2.5 py-1 text-[12px] font-medium text-fg-2 hover:bg-surface-2 hover:text-fg-1"
            >
              {copy.actions.keep}
            </button>
          </div>
        ) : (
          <div className="flex w-full items-center justify-end gap-2 rounded-xl border border-[oklch(from_var(--destructive)_l_c_h_/_0.3)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.06)] px-3.5 py-2.5">
            <button
              type="button"
              onClick={() => {
                cancel.mutate(job.id, {
                  onSettled: () => setActionMode("view"),
                });
              }}
              disabled={cancel.isPending}
              className="inline-flex items-center rounded-md border border-[oklch(from_var(--destructive)_l_c_h_/_0.4)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.1)] px-2.5 py-1 text-[12px] font-medium text-fg-1 transition-colors hover:bg-[oklch(from_var(--destructive)_l_c_h_/_0.15)] disabled:opacity-50"
            >
              {copy.actions.cancelConfirm}
            </button>
            <button
              type="button"
              onClick={() => setActionMode("view")}
              className="inline-flex items-center rounded-md border border-border/60 px-2.5 py-1 text-[12px] font-medium text-fg-2 hover:bg-surface-2 hover:text-fg-1"
            >
              {copy.actions.keep}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function isTerminal(state: string): boolean {
  return (
    state === "completed" || state === "cancelled" || state === "discarded"
  );
}

function SummaryField({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-0.5 px-3.5 py-3">
      <span className="text-[11px] font-medium uppercase tracking-wider text-fg-3">
        {label}
      </span>
      <span className="text-[13px] text-fg-1">{value}</span>
    </div>
  );
}

function CopyButton({
  text,
  copy,
}: {
  text: string;
  copy: JobsCopy["detail"];
}) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 1200);
        } catch {
          // Clipboard may fail in non-secure contexts — silent is fine.
        }
      }}
      className="inline-flex items-center gap-1 text-[11px] font-medium text-fg-3 transition-colors hover:text-fg-1"
    >
      {copied ? (
        <>
          <Check className="h-3 w-3" aria-hidden />
          {copy.copied}
        </>
      ) : (
        <>
          <Copy className="h-3 w-3" aria-hidden />
          {copy.copyArgs}
        </>
      )}
    </button>
  );
}

function DetailSkeleton() {
  return (
    <div className="space-y-6">
      <div className="h-8 w-64 animate-pulse rounded-md bg-surface-2" />
      <div className="h-28 animate-pulse rounded-2xl bg-surface-2" />
      <div className="h-56 animate-pulse rounded-2xl bg-surface-2" />
    </div>
  );
}
