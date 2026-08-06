"use client";

import * as React from "react";
import { RefreshCw } from "lucide-react";
import { cn } from "../utils";

/**
 * BoardDeckShell — the generalized "deck" presentation the 等你 band
 * pioneered, now shared by every same-kind card stack (notification
 * decks and same-kind live slots on custom boards alike): one card on
 * top, up to two ghost layers peeking from behind to say how deep the
 * pile is without spending the screen on it.
 *
 * `depth` is the number of cards WAITING BEHIND the visible one
 * (cards.length - 1). Zero renders children untouched — a deck of one
 * is just a card.
 */
export function BoardDeckShell({
  depth,
  children,
  className,
}: {
  depth: number;
  children: React.ReactNode;
  className?: string;
}) {
  if (depth <= 0) return <>{children}</>;
  return (
    <div className={cn("relative", "mb-2", className)}>
      {/* Stacked edges: up to two ghost layers peeking from behind. */}
      {depth > 1 ? (
        <div
          aria-hidden
          className="glass-card absolute inset-x-4 -bottom-2 h-6 rounded-[18px] opacity-40"
        />
      ) : null}
      <div
        aria-hidden
        className="glass-card absolute inset-x-2 -bottom-1 h-6 rounded-[18px] opacity-70"
      />
      <div className="relative">{children}</div>
    </div>
  );
}

/**
 * BoardDeckControls — the pill counting the pile ("还有 N 件") plus
 * the ↻ cycle affordance that advances to the next card in the group
 * client-side. Rendered by the caller inside its card's eyebrow row so
 * the count sits where the deck badge always sat.
 */
export function BoardDeckControls({
  countLabel,
  onCycle,
  cycleAriaLabel,
  className,
}: {
  /** Deck-depth pill, e.g. "还有 6 件". */
  countLabel: string;
  /** Advance to the next card in the group (client-side, no server). */
  onCycle?: () => void;
  cycleAriaLabel?: string;
  className?: string;
}) {
  return (
    <span className={cn("flex shrink-0 items-center gap-1", className)}>
      <span className="rounded-full bg-surface-2 px-2 py-0.5 text-[10.5px] font-medium text-fg-2">
        {countLabel}
      </span>
      {onCycle ? (
        <button
          type="button"
          aria-label={cycleAriaLabel}
          onClick={onCycle}
          className="cursor-pointer rounded-full p-1 text-fg-3 transition-colors [transition-timing-function:var(--ease-spring)] hover:bg-surface-2 hover:text-fg-1"
        >
          <RefreshCw className="h-3 w-3" aria-hidden />
        </button>
      ) : null}
    </span>
  );
}
