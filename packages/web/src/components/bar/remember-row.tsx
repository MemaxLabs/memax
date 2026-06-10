"use client";

import { Check, Plus, Undo2, X } from "lucide-react";
import { useLocale, useInterpolate } from "@/i18n";
import { BarHubBadge } from "./hub-badge";
import { Kbd } from "./kbd";
import { StarGlyph } from "./star-glyph";

export type RememberState = "idle" | "sending" | "sent" | "error";

export function RememberRow({
  state = "idle",
  title,
  fileCount,
  hub,
  highlighted,
  onUndo,
  onRetry,
  onClick,
  showShortcut = true,
}: {
  state?: RememberState;
  title?: string;
  fileCount?: number;
  hub?: string | null;
  highlighted?: boolean;
  onUndo?: () => void;
  onRetry?: () => void;
  onClick?: () => void;
  showShortcut?: boolean;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const baseBg = highlighted ? "bg-surface-1" : "hover:bg-surface-1";

  if (state === "sending") {
    return (
      <div className="flex min-h-11 items-center gap-3 px-4 text-[13px]">
        <StarGlyph breathe />
        <span className="flex-1 truncate text-fg-2">
          {t.bar.remember.sending}
        </span>
        {hub ? <BarHubBadge name={hub} /> : null}
      </div>
    );
  }

  if (state === "sent") {
    return (
      <div className="flex min-h-11 items-center gap-3 px-4 text-[13px]">
        <Check className="h-3.5 w-3.5 shrink-0 text-fg-2" />
        <span className="flex-1 truncate text-fg-2">
          <span className="font-medium text-fg-1">{t.import.doneOne}</span>
          {title ? (
            <span className="text-fg-3"> · “{title.slice(0, 40)}”</span>
          ) : null}
        </span>
        {hub ? <BarHubBadge name={hub} /> : null}
        <button
          type="button"
          onClick={onUndo}
          className="flex cursor-pointer items-center gap-1 text-[12px] text-fg-3 hover:text-fg-2"
        >
          <Undo2 className="h-3 w-3" />
          {t.import.undo}
        </button>
      </div>
    );
  }

  if (state === "error") {
    return (
      <div
        className="flex min-h-11 items-center gap-3 px-4 text-[13px]"
        style={{ background: "oklch(from var(--destructive) l c h / 0.05)" }}
      >
        <X
          className="h-3.5 w-3.5 shrink-0"
          style={{ color: "var(--destructive)" }}
        />
        <span className="flex-1 truncate text-fg-2">
          <span className="font-medium" style={{ color: "var(--destructive)" }}>
            {t.bar.remember.error}
          </span>
          <span className="text-fg-3"> · {t.bar.remember.errorNetwork}</span>
        </span>
        <button
          type="button"
          onClick={onRetry}
          className="cursor-pointer text-[12px] text-fg-3 underline underline-offset-2 hover:text-fg-2"
        >
          {t.common.retry}
        </button>
      </div>
    );
  }

  const label = fileCount
    ? interpolate(t.bar.remember.actionFiles, { count: String(fileCount) })
    : t.bar.remember.action;

  return (
    <button
      type="button"
      onClick={onClick}
      data-nav-highlighted={highlighted || undefined}
      className={`flex min-h-11 w-full cursor-pointer items-center gap-3 px-4 text-[13px] transition-colors ${baseBg}`}
    >
      <span className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded bg-surface-2 text-fg-2">
        <Plus className="h-3 w-3" />
      </span>
      <span className="flex-1 truncate text-left font-medium text-fg-1">
        {label}
      </span>
      {hub ? <BarHubBadge name={hub} /> : null}
      {showShortcut ? (
        <>
          <Kbd>⌘</Kbd>
          <Kbd>↵</Kbd>
        </>
      ) : null}
    </button>
  );
}
