"use client";

/**
 * TopicRowSkeleton — mirrors ModeCTopicRow geometry. Padding (`px-4 py-3`)
 * matches the row itself so skeleton → loaded transition has no layout
 * shift. Outer horizontal rhythm (`px-5 sm:px-8`) is owned by the parent
 * page container, not by individual rows.
 */

import { Skeleton } from "@memaxlabs/ui";

interface TopicRowSkeletonProps {
  showDivider?: boolean;
  seed?: number;
}

const SKELETON_BG = "bg-foreground/[0.07]";

function widthAt(seed: number, offsets: number[]): string {
  return `${offsets[seed % offsets.length]}%`;
}

export function TopicRowSkeleton({
  showDivider = false,
  seed = 0,
}: TopicRowSkeletonProps) {
  const divider = showDivider
    ? "border-t border-[oklch(from_var(--foreground)_l_c_h/0.08)]"
    : "";

  return (
    <div className={`flex items-center gap-3 px-4 py-3 ${divider}`}>
      <Skeleton className={`h-4 w-4 shrink-0 rounded ${SKELETON_BG}`} />
      <Skeleton
        className={`h-3.5 ${SKELETON_BG}`}
        style={{ width: widthAt(seed, [44, 62, 38, 54, 50]) }}
      />
      <Skeleton className={`ml-auto h-3 w-8 ${SKELETON_BG}`} />
    </div>
  );
}

export function TopicRowSkeletonList({ count = 4 }: { count?: number }) {
  return (
    <div>
      {Array.from({ length: count }).map((_, i) => (
        <TopicRowSkeleton key={i} showDivider={i > 0} seed={i} />
      ))}
    </div>
  );
}
