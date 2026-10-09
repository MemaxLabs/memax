"use client";

import { useLocale } from "@/i18n";
import { TIME_WINDOWS, type TimeWindow } from "@/hooks/use-recent-memories";

export type FragmentsMode = "recent" | "all";
export type RecentWindow = Exclude<TimeWindow, "all">;
export const RECENT_WINDOWS = TIME_WINDOWS.filter(
  (w): w is RecentWindow => w !== "all",
);
export const DEFAULT_RECENT_WINDOW: RecentWindow = "3d";

export function isRecentWindow(w: string): w is RecentWindow {
  return (RECENT_WINDOWS as readonly string[]).includes(w);
}

export function windowLabel(
  w: TimeWindow,
  t: ReturnType<typeof useLocale>["t"],
) {
  return w === "all" ? t.memoryView.modeAll : t.memoryView.windowLabel[w];
}

/**
 * 最近 | 全部 segmented control. Mode is a VIEW choice, not a filter:
 * 全部 is the full timeline, 最近 is one window of what agents just
 * filed.
 */
export function FragmentsModeToggle({
  mode,
  onSwitchMode,
}: {
  mode: FragmentsMode;
  onSwitchMode: (mode: FragmentsMode) => void;
}) {
  const { t } = useLocale();
  return (
    <div
      role="group"
      aria-label={t.memoryView.modeAria}
      className="ml-1 inline-flex rounded-full bg-surface-2 p-0.5 text-[12px]"
    >
      {(["recent", "all"] as const).map((m) => (
        <button
          key={m}
          type="button"
          aria-pressed={mode === m}
          onClick={() => onSwitchMode(m)}
          className={`rounded-full px-2.5 py-0.5 transition-colors cursor-pointer ${
            mode === m
              ? "bg-card text-fg-1 shadow-sm font-medium"
              : "text-fg-3 hover:text-fg-2"
          }`}
        >
          {m === "recent" ? t.memoryView.modeRecent : t.memoryView.modeAll}
        </button>
      ))}
    </div>
  );
}

/**
 * The recent-window pills (12h / 1d / 3d / 7d). Only meaningful in 最近
 * mode — the host renders them inline in the header on desktop and as a
 * row under the header on phones, where the header has no room.
 */
export function RecentWindowPills({
  window,
  onSwitchWindow,
  className = "",
}: {
  window: TimeWindow;
  onSwitchWindow: (window: RecentWindow) => void;
  className?: string;
}) {
  const { t } = useLocale();
  return (
    <div
      role="group"
      aria-label={t.memoryView.filterPastLabel}
      className={`items-center gap-0.5 text-[12px] ${className}`}
    >
      {RECENT_WINDOWS.map((w) => (
        <button
          key={w}
          type="button"
          aria-pressed={window === w}
          onClick={() => onSwitchWindow(w)}
          className={`rounded-full border px-2 py-0.5 whitespace-nowrap transition-colors cursor-pointer ${
            window === w
              ? "border-border/60 text-fg-1"
              : "border-transparent text-fg-3 hover:text-fg-2"
          }`}
        >
          {windowLabel(w, t)}
        </button>
      ))}
    </div>
  );
}
