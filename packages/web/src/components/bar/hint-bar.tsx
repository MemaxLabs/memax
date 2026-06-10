"use client";

import { Kbd } from "./kbd";

export function HintBar({
  hints,
  trailing,
}: {
  hints: { label: string; key: string }[];
  trailing?: { label: string; key: string };
}) {
  return (
    <div className="flex items-center gap-4 border-t border-border/30 px-5 py-2 text-[11px] text-fg-3">
      {hints.map((hint) => (
        <span
          key={`${hint.key}-${hint.label}`}
          className="flex items-center gap-1.5"
        >
          <Kbd>{hint.key}</Kbd>
          <span>{hint.label}</span>
        </span>
      ))}
      {trailing ? (
        <span className="ml-auto flex items-center gap-1.5">
          <Kbd>{trailing.key}</Kbd>
          <span>{trailing.label}</span>
        </span>
      ) : null}
    </div>
  );
}
