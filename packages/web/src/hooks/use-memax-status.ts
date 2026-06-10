"use client";

import { useMemories, flattenMemories } from "@/hooks/use-memories";
import { useDreamSurfaceState } from "@/hooks/use-dream-surface-state";
import { useBar } from "@/contexts/bar-context";

/**
 * Derives memax's real activity status from existing data.
 * No fake cycling — shows what memax is actually doing right now.
 *
 * Priority (highest first):
 *   1. processing  — memories being chunked/embedded/classified
 *   2. dreaming    — dream cycle running
 *   3. recalling   — actively searching (bar loading state)
 *   4. dreamed     — unseen completed-dream receipt exists
 *   5. idle        — connected, nothing happening
 */

export type MemaxStatus =
  | { state: "processing"; count: number }
  | { state: "dreaming" }
  | { state: "recalling" }
  | { state: "dreamed" }
  | { state: "idle" };

export function useMemaxStatus(): MemaxStatus {
  const { data: memoriesPages } = useMemories();
  const dreamSurface = useDreamSurfaceState();
  const { phase } = useBar();

  const memories = flattenMemories(memoriesPages);
  const processingCount = memories.filter(
    (m) => m.state === "processing",
  ).length;

  // 1. Memories being processed
  if (processingCount > 0) {
    return { state: "processing", count: processingCount };
  }

  // 2. Dream cycle actively running
  if (dreamSurface.runStatus === "running") {
    return { state: "dreaming" };
  }

  // 3. Bar is actively recalling
  if (phase === "loading") {
    return { state: "recalling" };
  }

  // 4. Unseen completed-dream receipt exists
  if (dreamSurface.hasUnseenCompletedReceipt) {
    return { state: "dreamed" };
  }

  // 5. Default
  return { state: "idle" };
}
