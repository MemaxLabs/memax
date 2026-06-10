"use client";

/**
 * `<AgentGridCardSkeleton>` — phase=loading placeholder for the agent
 * grid (plan 28 §B-4). Mirrors `<AgentGridCard>` geometry so the
 * skeleton → loaded transition has zero layout shift.
 *
 * Card structure (header / metrics / footer):
 *   - header: 36px avatar square + 14px name line + 11px status row
 *   - metrics: 12px metrics row
 *   - footer: 11px hint text
 *
 * The seed value lets a list of skeletons stagger their bar widths
 * without randomization (stable across renders) — same convention as
 * `<MemoryCardSkeleton>`.
 */

import { Skeleton } from "@memaxlabs/ui";

const SKELETON_BG = "bg-foreground/[0.07]";

function widthAt(seed: number, offsets: number[]): string {
  return `${offsets[seed % offsets.length]}%`;
}

export function AgentGridCardSkeleton({ seed = 0 }: { seed?: number }) {
  return (
    <div
      className="glass-card flex h-full min-h-35 flex-col rounded-surface p-4"
      aria-hidden
    >
      <div className="mb-3 flex items-start gap-3">
        <Skeleton
          className={`h-9 w-9 shrink-0 rounded-chrome ${SKELETON_BG}`}
        />
        <div className="min-w-0 flex-1">
          <Skeleton
            className={`h-3.5 ${SKELETON_BG}`}
            style={{ width: widthAt(seed, [82, 64, 92, 70, 78]) }}
          />
          <Skeleton
            className={`mt-1.5 h-2.5 ${SKELETON_BG}`}
            style={{ width: widthAt(seed, [40, 28, 36, 44, 32]) }}
          />
        </div>
      </div>
      <div className="mb-3 flex flex-1 items-center gap-3">
        <Skeleton
          className={`h-2.5 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [44, 36, 52, 40, 48]) }}
        />
        <Skeleton
          className={`h-2.5 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [28, 22, 32, 26, 30]) }}
        />
      </div>
      <div className="mt-auto">
        <Skeleton
          className={`h-2.5 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [36, 28, 40, 32, 30]) }}
        />
      </div>
    </div>
  );
}

/**
 * Grid of N skeleton cards matching the agent grid breakpoints. The
 * first cell stays as a real `<ConnectAgentTile>` even during loading
 * so the connect-an-agent affordance is reachable while phase=loading.
 */
export function AgentGridCardSkeletonList({ count = 5 }: { count?: number }) {
  return (
    <>
      {Array.from({ length: count }).map((_, i) => (
        <AgentGridCardSkeleton key={i} seed={i} />
      ))}
    </>
  );
}
