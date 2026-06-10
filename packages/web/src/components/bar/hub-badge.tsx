"use client";

import { ArrowRight } from "lucide-react";

export function BarHubBadge({ name }: { name: string }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded bg-surface-2 px-1.5 py-0.5 text-[11px] text-fg-2">
      <ArrowRight className="h-2.5 w-2.5" />
      {name}
    </span>
  );
}
