"use client";

import type { ReactNode } from "react";

export const MOBILE_THUMB_BAR_STYLE = {
  background: "oklch(from var(--card) l c h / 0.92)",
  backdropFilter: "blur(40px) saturate(180%)",
  WebkitBackdropFilter: "blur(40px) saturate(180%)",
  boxShadow: "0 2px 8px oklch(0 0 0 / 0.06)",
} as const;

export function MobileThumbBarShell({
  chips,
  input,
  actions,
  row,
  className = "",
}: {
  chips?: ReactNode;
  input: ReactNode;
  actions: ReactNode;
  row?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={`overflow-hidden rounded-surface border border-border/50 ${className}`}
      style={MOBILE_THUMB_BAR_STYLE}
    >
      {chips ? <div className="flex flex-col">{chips}</div> : null}
      {row ? (
        <div className="flex min-h-14 items-center gap-2 px-4 py-2">{row}</div>
      ) : (
        <div className="flex min-h-14 items-center gap-2 px-4 py-2">
          <div className="min-w-0 flex-1">{input}</div>
          {actions ? (
            <div className="flex shrink-0 items-center gap-1.5">{actions}</div>
          ) : null}
        </div>
      )}
    </div>
  );
}
