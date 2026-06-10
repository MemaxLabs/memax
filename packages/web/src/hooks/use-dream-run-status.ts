"use client";

import { useDreamSurfaceState } from "@/hooks/use-dream-surface-state";

/**
 * DreamRunStatus discriminates the live bar states.
 *
 * - "idle"            : no run at all, or the latest run is already
 *                       completed and the completion receipt path
 *                       has taken over.
 * - "running"         : a run is currently in flight. Bar shows the
 *                       live "Dreaming…" capsule. Stale-running
 *                       guard: a run that reports "running" but
 *                       started more than STALE_RUN_WINDOW_MS ago
 *                       is treated as abandoned and flipped to
 *                       "stale" — the server sweeper will mark it
 *                       failed on its next tick.
 * - "stale"           : the latest row says running but it is
 *                       almost certainly orphaned. Bar should clear
 *                       any live capsule rather than showing a
 *                       phantom "dreaming".
 * - "failed"          : the latest run ended in catastrophic
 *                       pre-scan error. Not user-facing on the bar
 *                       today, but exposed here so the inbox /
 *                       settings tabs can present it.
 * - "partial_failed"  : phases completed but at least one LLM-
 *                       backed phase hit errors or timeouts. Bar
 *                       should not show the "Dreaming…" capsule
 *                       (the run is done) — but the inbox/settings
 *                       tabs can use it to surface an amber badge.
 */
export type DreamRunStatus =
  | "idle"
  | "running"
  | "stale"
  | "failed"
  | "partial_failed";

export interface UseDreamRunStatusResult {
  status: DreamRunStatus;
  runId?: string;
  startedAt?: string;
}

/**
 * useDreamRunStatus is the dedicated hook for LIVE dream run state.
 * Separate from the `dream_run_completed` notification receipt by
 * design: completed runs flow through the notification framework
 * (bar push + durable inbox receipt + /seen replay guard), while
 * in-flight runs are plain lifecycle state read from dream_runs.
 *
 * The bar consumes this hook directly to drive the "Dreaming…"
 * capsule. It MUST NOT couple to pending decisions or the completed
 * dream_run_completed notification — those are separate surfaces
 * with their own consumers (inbox badge, bar push queue).
 *
 * Optional `hubId` narrows the lookup to a specific hub. When
 * omitted, falls back to `activeHubId` (the bar's default). Per-
 * hub settings surfaces (e.g. the Intelligence tab for a hub that
 * isn't the active one) pass this so the trigger button's
 * enabled/label state reflects the hub being configured, not
 * whichever hub happens to be active.
 */
export function useDreamRunStatus(hubId?: string): UseDreamRunStatusResult {
  const surface = useDreamSurfaceState({ hubId });
  return {
    status: surface.runStatus,
    runId: surface.runId,
    startedAt: surface.startedAt,
  };
}
