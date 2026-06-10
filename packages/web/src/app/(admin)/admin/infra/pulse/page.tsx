"use client";

import Link from "next/link";
import { useMemo } from "react";
import { ArrowRight, Crown } from "lucide-react";
import { useLocale } from "@/i18n";
import { useAdminOpsPulse, useAdminOpsStream } from "@/hooks/use-admin-ops";
import type {
  AdminOpsPulse,
  AdminOpsQueueDepth,
  AdminOpsWorkerClient,
} from "@/lib/admin-client";
import {
  EmptyState,
  JobStatePill,
  LiveBadge,
  OpsSection,
  RedFlagCallout,
  StatCard,
  formatRelativeTime,
} from "@/components/admin/ops-components";
import { interpolate } from "@/i18n";

/**
 * /admin/infra/pulse — live system pulse. SSE-driven counters +
 * worker list + queue depth + red-flag banner.
 *
 * UX contract:
 *   - First paint renders from useAdminOpsPulse (one-shot HTTP) so
 *     the page is not blank while SSE is still handshaking.
 *   - useAdminOpsStream replaces the pulse once an SSE event lands.
 *     If SSE fails, we keep showing the HTTP snapshot (stale, but
 *     useful) and the badge flips to "Reconnecting…".
 */
export default function AdminInfraPulsePage() {
  const { t } = useLocale();
  const copy = t.admin.infra.ops;

  const snapshot = useAdminOpsPulse(60);
  const stream = useAdminOpsStream();

  // Prefer stream data when present — it's strictly fresher. Fall
  // back to the HTTP snapshot while SSE is still warming up or
  // after it disconnects.
  //
  // Defensive normalization: Go nil slices serialize to JSON null,
  // not []. The server normalizes before sending, but older server
  // builds + any mid-deploy window would crash `.length` / `.map`
  // calls below. Cheap to guard here; keeps the UI robust to
  // wire-contract drift. Wrapped in useMemo so downstream consumers
  // (e.g. the headerMeta useMemo) see stable references between
  // renders that don't produce fresh data.
  const pulse: AdminOpsPulse | null = useMemo(() => {
    const rawPulse = stream.pulse ?? snapshot.data ?? null;
    if (!rawPulse) return null;
    return {
      ...rawPulse,
      workers: {
        ...rawPulse.workers,
        clients: rawPulse.workers?.clients ?? [],
      },
      queues: rawPulse.queues ?? [],
      red_flags: rawPulse.red_flags ?? [],
    };
  }, [stream.pulse, snapshot.data]);

  const headerMeta = useMemo(() => {
    if (!pulse) return null;
    const ago = formatRelativeTime(pulse.generated_at);
    return interpolate(copy.lastUpdated, { ago: `${ago} ago` });
  }, [copy.lastUpdated, pulse]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-[16px] font-semibold text-fg-1">{copy.title}</h2>
        <div className="flex items-center gap-3">
          {headerMeta ? (
            <span className="text-[12px] tabular-nums text-fg-3">
              {headerMeta}
            </span>
          ) : null}
          <LiveBadge
            connected={stream.connected}
            label={copy.liveBadge}
            labelDisconnected={copy.liveBadgeDisconnected}
          />
        </div>
      </div>

      {!pulse ? (
        <PulseSkeleton />
      ) : (
        <>
          {pulse.red_flags.length > 0 ? (
            <div className="animate-content-ready space-y-2">
              {pulse.red_flags.map((flag, i) => (
                <RedFlagCallout key={`${flag.kind}-${i}`} flag={flag} />
              ))}
            </div>
          ) : null}

          {/* Stat grid: mobile 2-col, tablet 4-col. Signature only
              on emitsents — subtle product signal that live work is
              happening when the number > 0. */}
          <div className="animate-content-ready grid grid-cols-2 gap-3 md:grid-cols-4">
            <StatCard
              label={copy.ingestionCards.inProcessing}
              value={pulse.ingestion.in_processing}
            />
            <StatCard
              label={copy.ingestionCards.stuck}
              value={pulse.ingestion.stuck_over_five_min}
              tone={
                pulse.ingestion.stuck_over_five_min > 0 ? "warn" : "default"
              }
            />
            <StatCard
              label={copy.ingestionCards.failedHour}
              value={pulse.ingestion.failed_last_hour}
              tone={pulse.ingestion.failed_last_hour > 0 ? "error" : "default"}
            />
            <StatCard
              label={copy.throughput.memoriesProcessed}
              value={pulse.throughput.memories_processed}
              hint={interpolate(copy.throughput.windowSuffix, {
                minutes: pulse.throughput.window_minutes,
              }).trim()}
              // Historical count, not live work — stays neutral.
              // Signature violet is reserved for "memax is actively
              // working right now."
              tone="default"
            />
          </div>

          <div className="flex justify-end">
            <Link
              href="/admin/infra/ingestion"
              className="inline-flex items-center gap-1 text-[12px] font-medium text-fg-3 transition-colors hover:text-fg-1"
            >
              {copy.ingestionCards.viewDetails}
              <ArrowRight className="h-3 w-3" aria-hidden />
            </Link>
          </div>

          <div className="grid gap-6 md:grid-cols-2">
            <WorkersSection
              pulse={pulse}
              copy={{
                title: copy.workers.title,
                healthy: copy.workers.healthy,
                stale: copy.workers.stale,
                noneConnected: copy.workers.noneConnected,
                leader: copy.workers.leader,
                queueSummary: copy.workers.queueSummary,
              }}
            />
            <QueuesSection
              queues={pulse.queues}
              copy={{
                title: copy.queues.title,
                empty: copy.queues.empty,
                labels: {
                  available: copy.queues.available,
                  running: copy.queues.running,
                  scheduled: copy.queues.scheduled,
                  retryable: copy.queues.retryable,
                  pending: copy.queues.pending,
                  discarded: copy.queues.discarded,
                },
              }}
            />
          </div>

          {/* Throughput detail — second row of stats so the primary
              grid stays "what's happening right now" and this row
              is "what's happened in the window." */}
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <StatCard
              label={copy.throughput.memoriesFailed}
              value={pulse.throughput.memories_failed}
              tone={pulse.throughput.memories_failed > 0 ? "error" : "default"}
            />
            <StatCard
              label={copy.throughput.dreamsCompleted}
              value={pulse.throughput.dreams_completed}
            />
            <StatCard
              label={copy.throughput.emailsSent}
              value={pulse.throughput.emails_sent}
            />
            <StatCard
              label={copy.workers.title}
              value={pulse.workers.healthy}
              hint={
                pulse.workers.stale > 0
                  ? interpolate(copy.workers.stale, {
                      count: pulse.workers.stale,
                    })
                  : null
              }
              tone={pulse.workers.healthy === 0 ? "error" : "default"}
            />
          </div>
        </>
      )}
    </div>
  );
}

function WorkersSection({
  pulse,
  copy,
}: {
  pulse: AdminOpsPulse;
  copy: {
    title: string;
    healthy: string;
    stale: string;
    noneConnected: string;
    leader: string;
    queueSummary: string;
  };
}) {
  return (
    <OpsSection title={copy.title}>
      {pulse.workers.clients.length === 0 ? (
        <EmptyState>{copy.noneConnected}</EmptyState>
      ) : (
        <ul className="divide-y divide-border/40">
          {pulse.workers.clients.map((c) => (
            <WorkerRow
              key={c.client_id}
              client={c}
              leaderCopy={copy.leader}
              queueSummaryCopy={copy.queueSummary}
            />
          ))}
        </ul>
      )}
    </OpsSection>
  );
}

function WorkerRow({
  client,
  leaderCopy,
  queueSummaryCopy,
}: {
  client: AdminOpsWorkerClient;
  leaderCopy: string;
  queueSummaryCopy: string;
}) {
  const ago = formatRelativeTime(client.heartbeat_at);
  return (
    <li className="flex items-center gap-3 px-3.5 py-2.5">
      <span
        className={`h-2 w-2 shrink-0 rounded-full ${
          client.healthy ? "bg-emerald-500" : "bg-fg-3"
        }`}
        aria-hidden
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-mono text-[13px] text-fg-1">
            {client.client_id}
          </span>
          {client.is_leader ? (
            // Leader is a role, not "live work" — neutral chip.
            // Signature violet stays reserved for the SSE pulse
            // dot and the running-job pill.
            <span
              title={leaderCopy}
              className="inline-flex items-center gap-1 rounded-md border border-border/60 bg-surface-2 px-1.5 py-0.5 text-[10px] font-medium text-fg-2"
            >
              <Crown className="h-3 w-3" aria-hidden />
              {leaderCopy}
            </span>
          ) : null}
        </div>
        <div className="mt-0.5 text-[11px] text-fg-3 tabular-nums">
          {interpolate(queueSummaryCopy, { count: client.queues.length })}
          {" · "}
          {ago}
        </div>
      </div>
    </li>
  );
}

function QueuesSection({
  queues,
  copy,
}: {
  queues: AdminOpsQueueDepth[];
  copy: {
    title: string;
    empty: string;
    labels: {
      available: string;
      running: string;
      scheduled: string;
      retryable: string;
      pending: string;
      discarded: string;
    };
  };
}) {
  return (
    <OpsSection title={copy.title}>
      {queues.length === 0 ? (
        <EmptyState>{copy.empty}</EmptyState>
      ) : (
        <ul className="divide-y divide-border/40">
          {queues.map((q) => (
            <li key={q.queue} className="px-3.5 py-3">
              <div className="mb-1.5 flex items-center justify-between">
                <span className="font-mono text-[13px] text-fg-1">
                  {q.queue}
                </span>
                <span className="text-[11px] text-fg-3 tabular-nums">
                  {q.available +
                    q.running +
                    q.scheduled +
                    q.retryable +
                    q.pending}
                </span>
              </div>
              <QueueBar queue={q} labels={copy.labels} />
            </li>
          ))}
        </ul>
      )}
    </OpsSection>
  );
}

function QueueBar({
  queue,
  labels,
}: {
  queue: AdminOpsQueueDepth;
  labels: {
    available: string;
    running: string;
    scheduled: string;
    retryable: string;
    pending: string;
    discarded: string;
  };
}) {
  const total =
    queue.available +
    queue.running +
    queue.scheduled +
    queue.retryable +
    queue.pending;
  const safeTotal = total > 0 ? total : 1;

  // Segments in intentional order: running first (active work),
  // then waiting (available/scheduled/pending), then problematic
  // (retryable). Discarded is excluded from the bar — terminal
  // state not worth visualizing on a live dashboard.
  const segments: Array<{
    key: string;
    label: string;
    count: number;
    bg: string;
  }> = [
    {
      key: "running",
      label: labels.running,
      count: queue.running,
      bg: "bg-[var(--signature)]",
    },
    {
      key: "available",
      label: labels.available,
      count: queue.available,
      bg: "bg-fg-2/60",
    },
    {
      key: "scheduled",
      label: labels.scheduled,
      count: queue.scheduled,
      bg: "bg-fg-3/50",
    },
    {
      key: "pending",
      label: labels.pending,
      count: queue.pending,
      bg: "bg-fg-3/30",
    },
    {
      key: "retryable",
      label: labels.retryable,
      count: queue.retryable,
      bg: "bg-[oklch(from_var(--warning)_l_c_h_/_0.6)]",
    },
  ];

  return (
    <>
      <div
        className="flex h-2 overflow-hidden rounded-full bg-surface-2"
        role="img"
        aria-label={`Queue ${queue.queue} composition`}
      >
        {segments.map((seg) =>
          seg.count > 0 ? (
            <div
              key={seg.key}
              className={seg.bg}
              style={{ width: `${(seg.count / safeTotal) * 100}%` }}
              title={`${seg.label}: ${seg.count}`}
            />
          ) : null,
        )}
      </div>
      <div className="mt-1.5 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-fg-3 tabular-nums">
        {segments
          .filter((s) => s.count > 0)
          .map((seg) => (
            <span key={seg.key}>
              {seg.label}: {seg.count}
            </span>
          ))}
      </div>
    </>
  );
}

function PulseSkeleton() {
  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            className="h-[88px] animate-pulse rounded-2xl bg-surface-2"
          />
        ))}
      </div>
      <div className="grid gap-6 md:grid-cols-2">
        <div className="h-56 animate-pulse rounded-2xl bg-surface-2" />
        <div className="h-56 animate-pulse rounded-2xl bg-surface-2" />
      </div>
    </div>
  );
}
