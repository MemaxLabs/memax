"use client";

/**
 * MemoryNavRow — shared row for cross-memory navigation cards inside
 * `<MemoryDetailView>` (Related Memories + Topic Siblings).
 *
 * Both surfaces had byte-identical button markup:
 *   - flex item with title (truncated) + age (tabular-nums)
 *   - same hover bg, same border, same padding
 *   - same `formatAge` formatter + locale args
 * Plan 26 phase 6 dedup carryover from the earlier audit. One row
 * primitive means the two surfaces can never drift in spacing or
 * type ramp, and a future tweak (e.g. add an icon, change density)
 * lands in one file.
 */

import { useRouter, usePathname } from "next/navigation";
import { useLocale, useInterpolate } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { buildMemoryDetailPath, getHubSlugForPath } from "@/lib/route-helpers";

interface MemoryNavRowProps {
  memoryId: string;
  title: string;
  /** ISO date string used for the right-aligned age label. */
  ageISO: string;
}

export function MemoryNavRow({ memoryId, title, ageISO }: MemoryNavRowProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const pathname = usePathname();
  const currentHubSlug = getHubSlugForPath(pathname);
  return (
    <button
      onClick={() =>
        router.push(buildMemoryDetailPath(currentHubSlug, memoryId))
      }
      className="touch-no-hover flex w-full cursor-pointer items-center gap-2 border-t border-border/30 px-4 py-2.5 text-left transition-colors hover:bg-surface-1 first:border-t-0"
    >
      <span className="text-[13px] text-fg-2 truncate flex-1">{title}</span>
      <span className="text-[12px] text-fg-3 shrink-0 tabular-nums">
        {formatAge(ageISO, t, interpolate)}
      </span>
    </button>
  );
}
