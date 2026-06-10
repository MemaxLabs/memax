"use client";

import { cn } from "../utils";

interface ContentErrorProps {
  /** Error message shown to user. */
  message?: string;
  /** Optional secondary hint shown below the main message. */
  detail?: string;
  /** Label for retry button. */
  retryLabel?: string;
  /** Retry callback. Shows retry button when provided. */
  onRetry?: () => void;
  /** Compact single-line variant for bar expand slot. */
  compact?: boolean;
  /** Borderless centered variant for page/body error states. */
  plain?: boolean;
  className?: string;
}

/**
 * ContentError — inline error card for API failures.
 *
 * Two variants:
 *   <ContentError />                          — card with border (pages, grids)
 *   <ContentError compact />                  — single-line (bar expand slot)
 *
 * Visual spec: uiux-strategy.md § Error States
 * Design ref: kitchen 13-forget destructive palette
 */
export function ContentError({
  message = "Something went wrong",
  detail,
  retryLabel = "Retry",
  onRetry,
  compact,
  plain,
  className,
}: ContentErrorProps) {
  if (compact) {
    return (
      <div
        className={cn(
          "flex items-center gap-2 px-3 py-2 text-[14px] text-fg-2",
          className,
        )}
      >
        <span
          className="inline-block h-1.5 w-1.5 shrink-0 rounded-full state-flash"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.6)",
          }}
        />
        <span className="flex-1 min-w-0">{message}</span>
        {onRetry && (
          <button
            onClick={onRetry}
            className="shrink-0 text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
          >
            {retryLabel}
          </button>
        )}
      </div>
    );
  }

  if (plain) {
    return (
      <div
        className={cn("flex flex-col items-center text-center py-8", className)}
      >
        <span
          className="mb-3 inline-block h-2 w-2 shrink-0 rounded-full state-flash"
          style={{
            backgroundColor: "oklch(from var(--destructive) l c h / 0.75)",
          }}
        />
        <p className="text-[15px] text-fg-2">{message}</p>
        {detail && <p className="mt-1 text-[13px] text-fg-3">{detail}</p>}
        {onRetry && (
          <button
            onClick={onRetry}
            className="mt-4 rounded-lg px-3 py-1.5 text-[13px] text-fg-2 hover:bg-surface-1 hover:text-fg-1 transition-colors cursor-pointer"
          >
            {retryLabel}
          </button>
        )}
      </div>
    );
  }

  return (
    <div
      className={cn(
        "rounded-lg border px-4 py-3",
        "flex items-start gap-3",
        className,
      )}
      style={{
        backgroundColor: "oklch(from var(--destructive) l c h / 0.05)",
        borderColor: "oklch(from var(--destructive) l c h / 0.10)",
      }}
    >
      <span
        className="mt-0.5 inline-block h-2 w-2 shrink-0 rounded-full state-flash"
        style={{
          backgroundColor: "oklch(from var(--destructive) l c h / 0.6)",
        }}
      />
      <div className="flex-1 min-w-0">
        <p className="text-[14px] text-fg-2 leading-relaxed">{message}</p>
        {onRetry && (
          <button
            onClick={onRetry}
            className="mt-2 text-[14px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
          >
            {retryLabel}
          </button>
        )}
      </div>
    </div>
  );
}
