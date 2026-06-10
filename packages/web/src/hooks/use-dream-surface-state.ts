"use client";

import { useMemo } from "react";
import { useAuth } from "@/lib/auth";
import { useDreamReport } from "@/hooks/use-dreams";
import { useNotificationSummary } from "@/hooks/use-notifications";

export type DreamRunSurfaceStatus =
  | "idle"
  | "running"
  | "stale"
  | "failed"
  | "partial_failed";

export interface DreamSurfaceState {
  runStatus: DreamRunSurfaceStatus;
  runId?: string;
  startedAt?: string;
  hasUnseenCompletedReceipt: boolean;
  unseenCompletedReceiptCount: number;
  latestRun: {
    merged: number;
    contradictionsFound: number;
    archived: number;
    organized: number;
    restructured: number;
  } | null;
  report: ReturnType<typeof useDreamReport>["data"];
}

// Matches the server's dream worker timeout + buffer. If a run still reports
// "running" after this window, clients treat it as stale rather than showing a
// phantom live state.
const STALE_RUN_WINDOW_MS = 15 * 60 * 1000;

/**
 * Shared adapter for dream-aware surfaces in the web app.
 *
 * It centralizes:
 * - live run status from `dreams/report`
 * - unseen completed-run receipt state from `notifications/summary`
 * - latest-run semantic counts from `DreamReport.intelligence.latest_run`
 *
 * This keeps bar, settings, inbox, and ambient status surfaces aligned on one
 * set of semantics instead of each re-interpreting dream state locally.
 */
export function useDreamSurfaceState(opts?: {
  hubId?: string;
  receiptScope?: "global" | "hub";
}): DreamSurfaceState {
  const { activeHubId } = useAuth();
  const hubId = opts?.hubId ?? activeHubId;
  const receiptScope = opts?.receiptScope ?? "global";
  const { data: report } = useDreamReport(hubId);
  const { data: notificationSummary } = useNotificationSummary(
    receiptScope === "hub" && hubId ? { hub: hubId } : undefined,
  );

  return useMemo<DreamSurfaceState>(() => {
    const run = report?.run;
    const latest = report?.intelligence.latest_run;
    const startedAt = run?.started_at;
    const runId = run?.id;

    let runStatus: DreamRunSurfaceStatus = "idle";
    if (run?.status === "running") {
      const startedMs = startedAt ? Date.parse(startedAt) : NaN;
      runStatus =
        Number.isFinite(startedMs) &&
        Date.now() - startedMs > STALE_RUN_WINDOW_MS
          ? "stale"
          : "running";
    } else if (run?.status === "failed") {
      runStatus = "failed";
    } else if (run?.status === "partial_failed") {
      // Partial-failure is distinct from "failed": phase mutations
      // landed but at least one LLM-backed phase hit errors/timeouts.
      // Surface it so the user knows the run wasn't clean; downstream
      // UI can pick its own treatment (e.g. amber pill vs. red pill).
      runStatus = "partial_failed";
    }

    const unseenCompletedReceiptCount =
      notificationSummary?.by_kind?.dream_run_completed?.unseen ?? 0;

    return {
      runStatus,
      runId,
      startedAt,
      hasUnseenCompletedReceipt: unseenCompletedReceiptCount > 0,
      unseenCompletedReceiptCount,
      latestRun: latest
        ? {
            merged: latest.merged,
            contradictionsFound: latest.contradictions_found,
            archived: latest.archived,
            organized: latest.organized,
            restructured: latest.restructured,
          }
        : null,
      report,
    };
  }, [notificationSummary, report]);
}
