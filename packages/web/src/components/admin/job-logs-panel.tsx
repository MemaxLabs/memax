"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowDown,
  Check,
  ChevronRight,
  Copy,
  Pause,
  Play,
} from "lucide-react";

import { useJobLogs } from "@/hooks/use-admin-ops";
import type { AdminOpsJobLogLine } from "@/lib/admin-client";
import { useLocale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";

import { OpsSection, formatRelativeTime } from "./ops-components";

type LogsCopy = Translations["admin"]["infra"]["jobs"]["detail"]["logs"];

// Design decisions:
// - Logs live inside the same OpsSection chrome as the rest of the
//   job detail page (no new Card primitive).
// - Auto-scroll follows the tail by default. If the operator scrolls
//   up even one row we flip to paused; a floating "Jump to latest"
//   pill brings them back, which also resumes following.
// - Level color is the ONLY decorative color on this surface. Every
//   other hue is an oklch derivation of --foreground so the panel
//   stays aligned with the rest of the admin design system.
// - Relative timestamps are the default because operators care about
//   "when in the job's lifetime" far more than wall clock. An 'A'
//   key / click toggle flips to absolute (HH:MM:SS.mmm) for
//   cross-referencing with Grafana's native view.

export function JobLogsPanel({ jobId }: { jobId: number }) {
  const { t } = useLocale();
  const copy = t.admin.infra.jobs.detail.logs;
  const state = useJobLogs(jobId);

  const scrollRef = useRef<HTMLDivElement>(null);
  const [autoFollow, setAutoFollow] = useState(true);
  const [showAbsoluteTime, setShowAbsoluteTime] = useState(false);
  const [copied, setCopied] = useState(false);

  // Scroll-to-bottom effect. When new lines arrive AND autoFollow is
  // on, snap to the bottom. We don't animate the scroll — it should
  // feel like a terminal, not a chat app.
  useEffect(() => {
    if (!autoFollow) return;
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [state.lines, autoFollow]);

  // Detect when the operator scrolls away from the bottom; that
  // pauses auto-follow. Scrolling back to within 32px of the bottom
  // resumes it. The threshold absorbs sub-pixel layout jitter.
  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceFromBottom < 32;
    setAutoFollow(atBottom);
  }, []);

  const jumpToLatest = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    setAutoFollow(true);
  }, []);

  const copyAll = useCallback(async () => {
    const text = state.lines.map((line) => formatLineForCopy(line)).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard may fail in non-secure contexts; silent is fine.
    }
  }, [state.lines]);

  // Keyboard shortcut: `A` toggles absolute/relative timestamps,
  // matching tools like `htop`/`k9s` that use single-key toggles.
  // Only fire when the panel isn't in a text input.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "a" && e.key !== "A") return;
      const target = e.target as HTMLElement | null;
      if (target?.tagName === "INPUT" || target?.tagName === "TEXTAREA") {
        return;
      }
      setShowAbsoluteTime((v) => !v);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <OpsSection
      title={copy.title}
      action={
        <div className="flex items-center gap-2">
          {state.environment ? (
            <span className="inline-flex items-center rounded-full border border-border/60 bg-surface-2 px-2 py-0.5 font-mono text-[10px] uppercase tracking-wider text-fg-3">
              {state.environment}
            </span>
          ) : null}
          <LogsLiveBadge
            connected={state.connected}
            unavailable={state.unavailable}
            hasError={state.error != null && !state.unavailable}
            copy={copy}
          />
        </div>
      }
    >
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/40 px-3 py-2">
        <p className="text-[12px] text-fg-3">
          {state.lines.length > 0
            ? copy.lineCount.replace("{count}", state.lines.length.toString())
            : copy.hint}
        </p>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => setShowAbsoluteTime((v) => !v)}
            className="inline-flex items-center rounded-md border border-border/60 px-2 py-0.5 text-[11px] font-medium text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-1"
            title={copy.toggleTimeHint}
            aria-pressed={showAbsoluteTime}
          >
            {showAbsoluteTime ? copy.showRelative : copy.showAbsolute}
          </button>
          <button
            type="button"
            onClick={() => setAutoFollow((v) => !v)}
            disabled={state.lines.length === 0}
            className="inline-flex items-center gap-1 rounded-md border border-border/60 px-2 py-0.5 text-[11px] font-medium text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-1 disabled:opacity-40"
            aria-pressed={autoFollow}
          >
            {autoFollow ? (
              <>
                <Pause className="h-3 w-3" aria-hidden />
                {copy.pause}
              </>
            ) : (
              <>
                <Play className="h-3 w-3" aria-hidden />
                {copy.resume}
              </>
            )}
          </button>
          <button
            type="button"
            onClick={copyAll}
            disabled={state.lines.length === 0}
            className="inline-flex items-center gap-1 rounded-md border border-border/60 px-2 py-0.5 text-[11px] font-medium text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-1 disabled:opacity-40"
          >
            {copied ? (
              <>
                <Check className="h-3 w-3" aria-hidden />
                {copy.copied}
              </>
            ) : (
              <>
                <Copy className="h-3 w-3" aria-hidden />
                {copy.copyAll}
              </>
            )}
          </button>
        </div>
      </div>

      <div className="relative">
        <div
          ref={scrollRef}
          onScroll={onScroll}
          role="log"
          aria-live="polite"
          aria-label={copy.title}
          // Cap at ~60% viewport height so a flood of log lines
          // never stretches the page. min-h keeps empty/short
          // panels from collapsing to nothing. The scroll container
          // is inside the panel, so the rest of the job detail page
          // stays fixed even if the worker emits 10k lines.
          className="max-h-[60vh] min-h-[240px] overflow-auto rounded-b-2xl bg-[oklch(from_var(--foreground)_l_c_h_/_0.02)] font-mono text-[12px] leading-relaxed"
        >
          {renderBody({ state, showAbsoluteTime, copy })}
        </div>

        {!autoFollow && state.lines.length > 0 ? (
          <button
            type="button"
            onClick={jumpToLatest}
            className="absolute bottom-3 right-3 inline-flex items-center gap-1 rounded-full border border-border/80 bg-surface-1 px-3 py-1.5 text-[11px] font-medium text-fg-1 shadow-md transition-colors hover:bg-surface-2"
          >
            <ArrowDown className="h-3 w-3" aria-hidden />
            {copy.jumpToLatest}
          </button>
        ) : null}
      </div>
    </OpsSection>
  );
}

function renderBody({
  state,
  showAbsoluteTime,
  copy,
}: {
  state: ReturnType<typeof useJobLogs>;
  showAbsoluteTime: boolean;
  copy: LogsCopy;
}) {
  if (state.unavailable) {
    return (
      <div className="px-4 py-8 text-center text-[13px] text-fg-3">
        {copy.unavailable}
      </div>
    );
  }
  if (!state.initialLoaded) {
    return <LogsSkeleton />;
  }
  if (state.error && state.lines.length === 0) {
    return (
      <div className="px-4 py-8 text-center text-[13px] text-fg-3">
        <p>{copy.error}</p>
        <p className="mt-1 font-mono text-[11px] text-fg-3/70">
          {errorMessage(state.error)}
        </p>
      </div>
    );
  }
  if (state.lines.length === 0) {
    return (
      <div className="px-4 py-10 text-center text-[13px] text-fg-3">
        {copy.empty}
      </div>
    );
  }
  return (
    <ul className="divide-y divide-border/20">
      {state.lines.map((line, idx) => (
        <LogRow
          key={logRowKey(line, idx)}
          line={line}
          showAbsoluteTime={showAbsoluteTime}
        />
      ))}
    </ul>
  );
}

function LogRow({
  line,
  showAbsoluteTime,
}: {
  line: AdminOpsJobLogLine;
  showAbsoluteTime: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  const levelClass = useMemo(() => levelTone(line.level), [line.level]);
  const hasFields = line.fields && Object.keys(line.fields).length > 0;

  return (
    <li className="group px-3 py-1.5 hover:bg-[oklch(from_var(--foreground)_l_c_h_/_0.03)]">
      <div className="flex items-start gap-2">
        <span
          className="shrink-0 text-fg-3/80"
          title={new Date(line.timestamp).toISOString()}
        >
          {showAbsoluteTime
            ? formatAbsolute(line.timestamp)
            : formatRelativeTime(line.timestamp)}
        </span>
        <span
          className={`shrink-0 rounded px-1.5 text-[10px] font-semibold uppercase tracking-wider ${levelClass}`}
        >
          {line.level || "LOG"}
        </span>
        <span className="min-w-0 flex-1 break-words text-fg-1">
          {line.message}
          {hasFields ? (
            <>
              {" "}
              <button
                type="button"
                onClick={() => setExpanded((v) => !v)}
                className="inline-flex items-center text-[10px] text-fg-3 hover:text-fg-1"
                aria-expanded={expanded}
              >
                <ChevronRight
                  className={`h-3 w-3 transition-transform ${
                    expanded ? "rotate-90" : ""
                  }`}
                  aria-hidden
                />
                <span className="ml-0.5">
                  {Object.keys(line.fields!).length}
                </span>
              </button>
            </>
          ) : null}
        </span>
      </div>
      {hasFields && expanded ? (
        <dl className="mt-1 ml-[calc(2rem+4.5rem)] grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-[11px]">
          {Object.entries(line.fields!).map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-fg-3">{k}</dt>
              <dd className="break-all font-mono text-fg-2">
                {formatFieldValue(v)}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
    </li>
  );
}

function LogsLiveBadge({
  connected,
  unavailable,
  hasError,
  copy,
}: {
  connected: boolean;
  unavailable: boolean;
  hasError: boolean;
  copy: LogsCopy;
}) {
  if (unavailable) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-border/60 bg-surface-2 px-2.5 py-0.5 text-[11px] font-medium text-fg-3">
        <span className="h-1.5 w-1.5 rounded-full bg-fg-3" aria-hidden />
        {copy.unavailableBadge}
      </span>
    );
  }
  if (hasError) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-[oklch(from_var(--destructive)_l_c_h_/_0.3)] bg-[oklch(from_var(--destructive)_l_c_h_/_0.08)] px-2.5 py-0.5 text-[11px] font-medium text-fg-1">
        <span
          className="h-1.5 w-1.5 rounded-full bg-[var(--destructive)]"
          aria-hidden
        />
        {copy.reconnecting}
      </span>
    );
  }
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[11px] font-medium ${
        connected
          ? "border-[oklch(from_var(--signature)_l_c_h_/_0.3)] bg-[oklch(from_var(--signature)_l_c_h_/_0.08)] text-fg-1"
          : "border-border/60 bg-surface-2 text-fg-3"
      }`}
    >
      <span
        className={`h-1.5 w-1.5 rounded-full ${
          connected ? "animate-pulse bg-[var(--signature)]" : "bg-fg-3"
        }`}
        aria-hidden
      />
      {connected ? copy.live : copy.disconnected}
    </span>
  );
}

function LogsSkeleton() {
  return (
    <div className="space-y-1 px-3 py-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <div
          key={i}
          className="h-3 animate-pulse rounded bg-surface-2/60"
          style={{ width: `${60 + ((i * 7) % 40)}%` }}
        />
      ))}
    </div>
  );
}

// --- formatting helpers ---

function levelTone(level: string): string {
  switch (level.toUpperCase()) {
    case "ERROR":
    case "FATAL":
      return "bg-[oklch(from_var(--destructive)_l_c_h_/_0.15)] text-[oklch(from_var(--destructive)_l_c_h_/_0.9)]";
    case "WARN":
    case "WARNING":
      return "bg-[oklch(from_var(--warning,oklch(0.78_0.14_70))_l_c_h_/_0.18)] text-[oklch(from_var(--warning,oklch(0.78_0.14_70))_l_c_h_/_0.95)]";
    case "DEBUG":
      return "bg-surface-2 text-fg-3";
    case "INFO":
    default:
      return "bg-surface-2 text-fg-2";
  }
}

function formatAbsolute(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const hh = d.getHours().toString().padStart(2, "0");
  const mm = d.getMinutes().toString().padStart(2, "0");
  const ss = d.getSeconds().toString().padStart(2, "0");
  const ms = d.getMilliseconds().toString().padStart(3, "0");
  return `${hh}:${mm}:${ss}.${ms}`;
}

function formatFieldValue(v: unknown): string {
  if (v === null) return "null";
  if (v === undefined) return "";
  if (typeof v === "string") return v;
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}

function formatLineForCopy(line: AdminOpsJobLogLine): string {
  const base = `${line.timestamp} ${line.level || "LOG"} ${line.message}`;
  if (!line.fields || Object.keys(line.fields).length === 0) return base;
  const extras = Object.entries(line.fields)
    .map(([k, v]) => `${k}=${formatFieldValue(v)}`)
    .join(" ");
  return `${base} ${extras}`;
}

function logRowKey(line: AdminOpsJobLogLine, idx: number): string {
  // Stable-ish key: timestamp + idx covers duplicate-timestamp bursts
  // without generating a crypto hash per row.
  return `${line.timestamp}:${idx}`;
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
