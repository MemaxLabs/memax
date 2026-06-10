"use client";

import type { ReactNode } from "react";
import { Info } from "lucide-react";
import { Popover, PopoverTrigger, PopoverContent } from "./popover";

interface InfoPopoverProps {
  /** Accessible label for the trigger button. Read by screen readers. */
  ariaLabel: string;
  /** Popover heading — short, 1 line. */
  title: string;
  /** Popover body — plain text, 1–3 sentences. */
  body: string;
  /** Optional brand anchor (e.g. <MemaxStar animated />) rendered left of title. */
  brandMark?: ReactNode;
  /** Popover side. Default: "bottom". */
  side?: "top" | "bottom" | "left" | "right";
  /** Popover alignment. Default: "start". */
  align?: "start" | "center" | "end";
  /** Gap between trigger and popover in px. Default: 8. */
  sideOffset?: number;
  /** Max popover width in px. Default: 320. */
  maxWidth?: number;
  /** Hover open delay in ms. Default: 150. */
  hoverDelay?: number;
  /** Hover close delay in ms. Default: 100. */
  hoverCloseDelay?: number;
}

/**
 * InfoPopover — inline help trigger anchored to a small Info icon.
 *
 * Industry pattern (Linear, Notion, Stripe, GitHub): a small ⓘ in a
 * section header; hover on desktop with a short delay, click anywhere,
 * tap on touch, Tab + Enter for keyboard — all four input modalities
 * open a short educational popover with title + body + optional brand
 * anchor. Users can read at their own pace; cross-device behavior stays
 * consistent.
 *
 * Defaults are tuned for section-header inline help: 150 ms hover delay
 * (faster than @base-ui's default 300 ms), 8 px sideOffset, bottom-start
 * alignment, 320 px max width. The four input modalities are handled
 * automatically by @base-ui's Popover primitive via `openOnHover`.
 *
 * Use for: 1–3 sentence explanations of what a section does and how it
 * works. For richer content (lists, buttons, links, long-form help)
 * reach for Popover directly and compose your own content.
 *
 * Pass `brandMark` with <MemaxStar animated /> when the explanation
 * references brand vocabulary (dream, remember, recall, forget) — the
 * visual anchor ties the copy to the brand symbol in one teaching
 * moment. Skip it for utility help (hub management, team settings).
 */
export function InfoPopover({
  ariaLabel,
  title,
  body,
  brandMark,
  side = "bottom",
  align = "start",
  sideOffset = 8,
  maxWidth = 320,
  hoverDelay = 150,
  hoverCloseDelay = 100,
}: InfoPopoverProps) {
  return (
    <Popover>
      <PopoverTrigger
        aria-label={ariaLabel}
        openOnHover
        delay={hoverDelay}
        closeDelay={hoverCloseDelay}
        className="flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded-full text-fg-4 outline-none transition-colors hover:bg-surface-2 hover:text-fg-2 focus-visible:bg-surface-2 focus-visible:text-fg-2"
      >
        <Info className="h-3.5 w-3.5" strokeWidth={2} />
      </PopoverTrigger>
      <PopoverContent
        side={side}
        align={align}
        sideOffset={sideOffset}
        className="px-4 py-3"
        style={{ maxWidth }}
      >
        <div className="flex items-start gap-2.5">
          {brandMark && <div className="mt-0.5 shrink-0">{brandMark}</div>}
          <div className="min-w-0 space-y-1.5">
            <div className="text-[13px] font-semibold text-fg-1">{title}</div>
            <p className="text-[12px] leading-[1.55] text-fg-2">{body}</p>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
