"use client";

import { useLocale, useInterpolate } from "@/i18n";

export function LoadMoreRow({
  count,
  onClick,
}: {
  count: number;
  onClick?: () => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full cursor-pointer items-center justify-center border-t border-border/20 px-4 py-2 text-[12px] text-fg-3 hover:text-fg-2"
    >
      {interpolate(t.bar.loadMore, { count: String(count) })}
    </button>
  );
}
