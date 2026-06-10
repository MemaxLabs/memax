"use client";

/**
 * MemoryCardSkeleton — mirrors `<MemoryCardLayout>` geometry so the
 * skeleton → loaded transition has zero layout shift on v2 card grids.
 *
 * Card structure (header / preview / footer):
 *   - header: 24px file-type icon square + 13px title line + 11px age
 *   - preview: 3-line summary (12px lines) with line-clamp-3
 *   - footer: 11px attribution chunk + topic chip
 *
 * The seed value lets a list of cards stagger their bar widths without
 * randomization (stable across renders), matching MemoryRowSkeleton's
 * `seed` mechanic.
 */

import { Skeleton } from "@memaxlabs/ui";

const SKELETON_BG = "bg-foreground/[0.07]";

function widthAt(seed: number, offsets: number[]): string {
  return `${offsets[seed % offsets.length]}%`;
}

export function MemoryCardSkeleton({ seed = 0 }: { seed?: number }) {
  return (
    <div
      className="glass-card flex h-full flex-col rounded-surface p-4"
      aria-hidden
    >
      {/* Header: icon + title + age. Title sits in a flexing wrapper
          so the seed-based width actually varies — `flex-1` directly
          on a Skeleton overrides the inline width via `flex: 1 1 0%`,
          masking the stagger. The wrapper holds the lane; the inner
          Skeleton owns the width. */}
      <div className="mb-3 flex items-start gap-2">
        <Skeleton className={`h-6 w-6 shrink-0 rounded-md ${SKELETON_BG}`} />
        <div className="min-w-0 flex-1">
          <Skeleton
            className={`h-3.5 ${SKELETON_BG}`}
            style={{ width: widthAt(seed, [82, 64, 92, 70, 78]) }}
          />
        </div>
        <Skeleton className={`h-2.5 w-7 shrink-0 ${SKELETON_BG}`} />
      </div>
      {/* Preview: 3 lines */}
      <div className="mb-3 flex flex-1 flex-col gap-1.5">
        <Skeleton
          className={`h-2.5 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [96, 88, 92, 84, 90]) }}
        />
        <Skeleton
          className={`h-2.5 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [88, 76, 92, 70, 84]) }}
        />
        <Skeleton
          className={`h-2.5 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [62, 48, 70, 54, 66]) }}
        />
      </div>
      {/* Footer: attribution + topic chip */}
      <div className="flex items-center gap-2">
        <Skeleton
          className={`h-2.5 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [40, 28, 36, 44, 32]) }}
        />
        <Skeleton className={`h-3 w-12 rounded ${SKELETON_BG}`} />
      </div>
    </div>
  );
}

/** Grid of N card skeletons matching the card grid breakpoints. */
export function MemoryCardSkeletonList({ count = 3 }: { count?: number }) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {Array.from({ length: count }).map((_, i) => (
        <MemoryCardSkeleton key={i} seed={i} />
      ))}
    </div>
  );
}
