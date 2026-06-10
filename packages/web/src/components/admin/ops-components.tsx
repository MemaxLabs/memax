"use client";

// Shared primitives for the /admin/infra ops views. Keeps the
// page-level files small and ensures visual consistency between
// Pulse / Ingestion / Jobs (same stat cards, same state pills,
// same live indicator).

import type { ReactNode } from "react";
import { AlertTriangle, AlertOctagon, Info } from "lucide-react";
import type {
  AdminOpsJobState,
  AdminOpsRedFlag,
  AdminOpsRedFlagSeverity,
} from "@/lib/admin-client";

/**
 * StatCard is the minimal card for ops counters. Number-first, label
 * underneath, optional hint on a third line. Mobile-first: stacks to
 * one column below sm.
 */
export function StatCard({
  label,
  value,
  hint,
  tone = "default",
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  tone?: "default" | "warn" | "error" | "signature";
}) {
  const toneStyles: Record<string, string> = {
    default: "",
    warn: "bg-[oklch(from_var(--warning)_l_c_h_/_0.08)]",
    error: "bg-[oklch(from_var(--destructive)_l_c_h_/_0.08)]",
    signature: "bg-[oklch(from_var(--signature)_l_c_h_/_0.06)]",
  };
  return (
    <div
      className={`flex flex-col gap-1 rounded-2xl border border-border/60 px-4 py-3 transition-colors ${toneStyles[tone]}`}
    >
      <div className="text-[11px] font-medium uppercase tracking-wider text-fg-3">
        {label}
      </div>
      <div className="text-[26px] font-semibold tabular-nums leading-none text-fg-1">
        {value}
      </div>
      {hint ? <div className="mt-1 text-[12px] text-fg-3">{hint}</div> : null}
    </div>
  );
}

/**
 * Section wraps a titled block. Uses the admin sectioning convention
 * (small uppercase label, subtle body). Used for "Workers", "Queues",
 * etc.
 */
export function OpsSection({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-[11px] font-medium uppercase tracking-wider text-fg-3">
            {title}
          </h2>
          {description ? (
            <p className="mt-1 text-[13px] text-fg-3">{description}</p>
          ) : null}
        </div>
        {action ? <div className="shrink-0">{action}</div> : null}
      </div>
      <div className="rounded-2xl border border-border/60 bg-surface-1/40">
        {children}
      </div>
    </section>
  );
}

/**
 * LiveBadge shows the SSE connection status. Pulses when connected
 * — that's the ONE place signature violet is allowed on this page
 * (signature = "memax is actively working").
 */
export function LiveBadge({
  connected,
  label,
  labelDisconnected,
}: {
  connected: boolean;
  label: string;
  labelDisconnected: string;
}) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[11px] font-medium tabular-nums ${
        connected
          ? "border-[oklch(from_var(--signature)_l_c_h_/_0.3)] bg-[oklch(from_var(--signature)_l_c_h_/_0.08)] text-fg-1"
          : "border-border/60 bg-surface-2 text-fg-3"
      }`}
      aria-live="polite"
    >
      <span
        className={`inline-block h-1.5 w-1.5 rounded-full ${
          connected ? "animate-pulse bg-[var(--signature)]" : "bg-fg-3"
        }`}
        aria-hidden
      />
      {connected ? label : labelDisconnected}
    </span>
  );
}

/**
 * JobStatePill returns a colored chip for a River job state. State
 * values that don't match are rendered with the default tone — we'd
 * rather show "???" than crash on a future River state.
 */
export function JobStatePill({
  state,
  label,
}: {
  state: AdminOpsJobState | string;
  label: string;
}) {
  const tones: Record<string, string> = {
    available: "border-border/60 bg-surface-2 text-fg-2",
    scheduled: "border-border/60 bg-surface-2 text-fg-3",
    running:
      "border-[oklch(from_var(--signature)_l_c_h_/_0.3)] bg-[oklch(from_var(--signature)_l_c_h_/_0.1)] text-fg-1",
    pending: "border-border/60 bg-surface-2 text-fg-3",
    retryable:
      "border-[oklch(from_var(--warning)_l_c_h_/_0.3)] bg-[oklch(from_var(--warning)_l_c_h_/_0.1)] text-fg-1",
    completed: "border-emerald-500/30 bg-emerald-500/10 text-fg-1",
    discarded:
      "border-[oklch(from_var(--destructive)_l_c_h_/_0.3)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.1)] text-fg-1",
    cancelled: "border-border/60 bg-surface-2 text-fg-3",
  };
  const cls = tones[state] ?? "border-border/60 bg-surface-2 text-fg-3";
  return (
    <span
      className={`inline-flex items-center rounded-md border px-1.5 py-0.5 text-[11px] font-medium ${cls}`}
    >
      {label}
    </span>
  );
}

/**
 * RedFlagCallout renders OpsRedFlag. Uses severity-scoped tints.
 */
export function RedFlagCallout({ flag }: { flag: AdminOpsRedFlag }) {
  const icon = severityIcon(flag.severity);
  const styles: Record<AdminOpsRedFlagSeverity, string> = {
    info: "border-border/60 bg-surface-1/40 text-fg-2",
    warn: "border-[oklch(from_var(--warning)_l_c_h_/_0.25)] bg-[oklch(from_var(--warning)_l_c_h_/_0.08)] text-fg-1",
    error:
      "border-[oklch(from_var(--destructive)_l_c_h_/_0.3)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.08)] text-fg-1",
  };
  const cls = styles[flag.severity as AdminOpsRedFlagSeverity] ?? styles.info;
  return (
    <div
      className={`flex items-start gap-2.5 rounded-xl border px-3.5 py-2.5 ${cls}`}
      role={flag.severity === "error" ? "alert" : "status"}
    >
      <span className="mt-0.5 shrink-0" aria-hidden>
        {icon}
      </span>
      <div className="text-[13px] leading-snug">{flag.message}</div>
    </div>
  );
}

function severityIcon(sev: string): ReactNode {
  switch (sev) {
    case "error":
      return <AlertOctagon className="h-4 w-4" />;
    case "warn":
      return <AlertTriangle className="h-4 w-4" />;
    default:
      return <Info className="h-4 w-4" />;
  }
}

/**
 * RelativeTime formats a duration-since-timestamp as a short label
 * ("3s", "2m", "1h"). Stable between re-renders within a test —
 * pure function, no side effects.
 */
export function formatRelativeTime(
  isoOrDate: string | Date,
  now: Date = new Date(),
): string {
  const then = typeof isoOrDate === "string" ? new Date(isoOrDate) : isoOrDate;
  const sec = Math.max(0, Math.round((now.getTime() - then.getTime()) / 1000));
  if (sec < 60) return `${sec}s`;
  const min = Math.round(sec / 60);
  if (min < 60) return `${min}m`;
  const hr = Math.round(min / 60);
  if (hr < 24) return `${hr}h`;
  const day = Math.round(hr / 24);
  return `${day}d`;
}

/**
 * EmptyState renders a centered muted block inside a Section.
 */
export function EmptyState({ children }: { children: ReactNode }) {
  return (
    <div className="px-4 py-8 text-center text-[13px] text-fg-3">
      {children}
    </div>
  );
}
