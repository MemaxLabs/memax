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
 * 最近 | 全部 segmented control plus the recent-window pills. Mode is a
 * VIEW choice, not a filter: 全部 is the full timeline, 最近 is one
 * window of what agents just filed. Pills only exist in 最近; they hide
 * at phone width where the header has no room — there the window lives
 * in the filter popover's (phone-only) time section instead.
 */
export function FragmentsModeControls({
  mode,
  window,
  onSwitchMode,
  onSwitchWindow,
}: {
  mode: FragmentsMode;
  window: TimeWindow;
  onSwitchMode: (mode: FragmentsMode) => void;
  onSwitchWindow: (window: RecentWindow) => void;
}) {
  const { t } = useLocale();
  return (
    <>
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
      {mode === "recent" && (
        <div
          role="group"
          aria-label={t.memoryView.filterPastLabel}
          className="ml-1.5 hidden items-center gap-0.5 text-[12px] sm:inline-flex"
        >
          {RECENT_WINDOWS.map((w) => (
            <button
              key={w}
              type="button"
              aria-pressed={window === w}
              onClick={() => onSwitchWindow(w)}
              className={`rounded-full border px-2 py-0.5 transition-colors cursor-pointer ${
                window === w
                  ? "border-border/60 text-fg-1"
                  : "border-transparent text-fg-3 hover:text-fg-2"
              }`}
            >
              {windowLabel(w, t)}
            </button>
          ))}
        </div>
      )}
    </>
  );
}
