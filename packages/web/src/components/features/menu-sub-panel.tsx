"use client";

import { ChevronLeft } from "lucide-react";

/**
 * MenuSubPanelHeader — the universal "you are one level deep" header
 * for popover menus that swap their body to a sub-panel (move picker,
 * inline rename, …). Founder feedback (2026-09-14): a sub-panel with
 * no visible way back reads as a dead end — Esc-only return is a
 * keyboard secret.
 *
 * The convention (macOS / Linear / Radix submenu grammar, adapted to
 * our swap-in-place panels):
 *   - one header row: ‹ chevron + the sub-panel's NAME (not "back" —
 *     the name tells you where you are, the chevron tells you it
 *     goes up);
 *   - clicking it returns to the parent menu view;
 *   - Esc does the same — hosts wire it via `subPanelEscapeHandler`
 *     on the sub-panel's container so Esc steps back instead of
 *     closing the whole popover.
 *
 * Every ⋮/⋯ menu that grows a second level renders this header. No
 * sub-panel without it.
 */
export function MenuSubPanelHeader({
  label,
  onBack,
  backAriaLabel,
}: {
  /** The sub-panel's name — 移动主题, 重命名, 移到主题, … */
  label: string;
  onBack: () => void;
  /** Accessible name for the control, e.g. t.common.backToMenu. */
  backAriaLabel: string;
}) {
  return (
    <button
      type="button"
      aria-label={backAriaLabel}
      onClick={(e) => {
        e.stopPropagation();
        onBack();
      }}
      className="flex w-full cursor-pointer items-center gap-1.5 border-b border-border/30 px-2.5 py-2 text-left text-[12px] font-medium text-fg-2 transition-colors hover:text-fg-1"
    >
      <ChevronLeft className="h-3.5 w-3.5 shrink-0 text-fg-3" />
      <span className="min-w-0 truncate">{label}</span>
    </button>
  );
}

/**
 * Keydown handler for a sub-panel container: Esc steps BACK to the
 * parent menu (and stops the popover's own Esc-close), everything
 * else passes through. Spread as `onKeyDown={subPanelEscapeHandler(back)}`.
 */
export function subPanelEscapeHandler(onBack: () => void) {
  return (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onBack();
    }
  };
}
