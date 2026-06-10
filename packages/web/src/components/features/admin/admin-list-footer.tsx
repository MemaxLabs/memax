"use client";

import { useInterpolate, useLocale } from "@/i18n";

/**
 * AdminListFooter — shared footer for every paginated admin list.
 *
 * Renders the "N total" counter and the Prev / Next buttons in the
 * same visual slot as the existing users page so all admin lists
 * feel identical. Labels are pulled from the shared
 * `t.admin.pagination` group so individual pages don't each ship
 * their own strings.
 *
 * Renders nothing when total is 0 and no navigation is possible, so
 * callers can drop it in unconditionally and let the component
 * decide when it's worth showing.
 */
export function AdminListFooter({
  total,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
}: {
  total: number;
  hasPrev: boolean;
  hasNext: boolean;
  onPrev: () => void;
  onNext: () => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  if (total <= 0 && !hasPrev && !hasNext) return null;
  return (
    <div className="px-4 py-3 border-t border-border/30 flex items-center justify-between">
      <span className="text-[13px] text-fg-3">
        {interpolate(t.admin.pagination.total, { count: String(total) })}
      </span>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={onPrev}
          disabled={!hasPrev}
          className="px-3 py-1 rounded-md text-[13px] text-fg-2 hover:bg-surface-2 transition-colors disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent"
        >
          {t.admin.pagination.previous}
        </button>
        <button
          type="button"
          onClick={onNext}
          disabled={!hasNext}
          className="px-3 py-1 rounded-md text-[13px] text-fg-2 hover:bg-surface-2 transition-colors disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent"
        >
          {t.admin.pagination.next}
        </button>
      </div>
    </div>
  );
}
