"use client";

import type { LucideIcon } from "lucide-react";

export function CommandRow({
  icon: Icon,
  name,
  desc,
  highlighted,
  onClick,
}: {
  icon: LucideIcon;
  name: string;
  desc: string;
  highlighted?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      data-nav-highlighted={highlighted || undefined}
      className={`flex min-h-10 w-full cursor-pointer items-center gap-3 px-4 text-[13px] transition-colors ${
        highlighted ? "bg-surface-1" : "hover:bg-surface-1"
      }`}
    >
      <Icon className="h-3.5 w-3.5 shrink-0 text-fg-3" />
      <span className="w-32 shrink-0 truncate text-left font-medium text-fg-1">
        {name}
      </span>
      <span className="min-w-0 flex-1 truncate text-left text-fg-3">
        {desc}
      </span>
    </button>
  );
}
