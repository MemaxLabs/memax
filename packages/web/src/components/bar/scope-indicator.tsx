"use client";

import { Command, Folder, X, type LucideIcon } from "lucide-react";

export type BarScope =
  | { kind: "recall" }
  | { kind: "topic"; label: string; Icon?: LucideIcon }
  | { kind: "command"; label: string; command: "hub" };

export function ScopeIndicator({
  scope,
  title,
  onClear,
}: {
  scope: BarScope;
  title?: string;
  onClear?: () => void;
}) {
  if (scope.kind === "recall") return null;

  const Icon =
    scope.kind === "topic"
      ? (scope.Icon ?? Folder)
      : scope.command === "hub"
        ? Folder
        : Command;

  return (
    <button
      type="button"
      title={title}
      onClick={onClear}
      className="relative flex h-9 shrink-0 items-center gap-1.5 rounded-full px-3 text-[13px] font-medium text-fg-2"
      style={{ background: "oklch(from var(--foreground) l c h / 0.06)" }}
    >
      <Icon className="h-3.5 w-3.5" />
      <span>{scope.label}</span>
      <X className="ml-0.5 h-3 w-3 text-fg-3" />
    </button>
  );
}
