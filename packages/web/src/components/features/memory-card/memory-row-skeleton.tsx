"use client";

/**
 * MemoryRowSkeleton — mirrors MemoryRow geometry so the skeleton → loaded
 * transition has zero layout shift. One variant per surface layout:
 *
 * - "stacked" (recent): two-line stack (meta line 12px + title line 14px +
 *   optional summary line 13px), matching MemoryRow's `useStackedRecent` path.
 * - "single" (topic/list/recall): single flex row (indicator + title +
 *   trailing meta), matching MemoryRow's default path.
 *
 * Padding (`px-4 py-2.5`) and divider (`border-t border-border/20`) match
 * MemoryRow exactly. Divider uses the borderless-frame token; surfaces that
 * still sit inside card chrome render the same skeleton without issue.
 */

import { Skeleton } from "@memaxlabs/ui";

export type MemoryRowSkeletonLayout = "stacked" | "single";

interface MemoryRowSkeletonProps {
  layout?: MemoryRowSkeletonLayout;
  showDivider?: boolean;
  /** Include a summary line (stacked layout only). Defaults to true. */
  showSummary?: boolean;
  /** Seed width variance. Used to stagger bar widths across N skeletons
   *  without actually randomizing (stable across renders). */
  seed?: number;
}

const SKELETON_BG = "bg-foreground/[0.07]";

function widthAt(seed: number, offsets: number[]): string {
  return `${offsets[seed % offsets.length]}%`;
}

export function MemoryRowSkeleton({
  layout = "stacked",
  showDivider = false,
  showSummary = true,
  seed = 0,
}: MemoryRowSkeletonProps) {
  const divider = showDivider
    ? "border-t border-[oklch(from_var(--foreground)_l_c_h/0.08)]"
    : "";
  const wrapperClass = `px-4 py-2.5 ${divider}`;

  if (layout === "single") {
    return (
      <div className={`${wrapperClass} flex items-center gap-2`}>
        <Skeleton className={`h-5 w-5 shrink-0 rounded-full ${SKELETON_BG}`} />
        <Skeleton
          className={`h-3.5 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [58, 72, 46, 64, 52]) }}
        />
        <div className="ml-auto flex items-center gap-1.5">
          <Skeleton className={`h-4 w-14 ${SKELETON_BG}`} />
          <Skeleton className={`h-3 w-8 ${SKELETON_BG}`} />
        </div>
      </div>
    );
  }

  return (
    <div className={wrapperClass}>
      {/* Line 1: indicator + attribution + meta (12px rhythm) */}
      <div className="mb-0.5 flex items-center gap-1.5">
        <Skeleton className={`h-5 w-5 shrink-0 rounded-full ${SKELETON_BG}`} />
        <Skeleton
          className={`h-3 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [28, 32, 24, 36, 30]) }}
        />
        <Skeleton className={`ml-auto h-3 w-7 ${SKELETON_BG}`} />
      </div>
      {/* Line 2: title (14px) */}
      <Skeleton
        className={`h-3.5 ${SKELETON_BG}`}
        style={{ width: widthAt(seed, [72, 84, 62, 78, 56]) }}
      />
      {/* Line 3 (optional): summary (13px) */}
      {showSummary && (
        <Skeleton
          className={`mt-1 h-3 ${SKELETON_BG}`}
          style={{ width: widthAt(seed, [56, 48, 66, 52, 44]) }}
        />
      )}
    </div>
  );
}

/** Stack of N memory-row skeletons with dividers between, for borderless lists. */
export function MemoryRowSkeletonList({
  count = 3,
  layout = "stacked",
  showSummary = true,
}: {
  count?: number;
  layout?: MemoryRowSkeletonLayout;
  showSummary?: boolean;
}) {
  return (
    <div>
      {Array.from({ length: count }).map((_, i) => (
        <MemoryRowSkeleton
          key={i}
          layout={layout}
          showDivider={i > 0}
          showSummary={showSummary}
          seed={i}
        />
      ))}
    </div>
  );
}
