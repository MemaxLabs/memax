"use client";

import * as React from "react";
import { cn } from "../utils";
import { BoardReceipt } from "./board-atoms";

/**
 * BoardCard — the L2 molecule every pulse-board kind renders inside.
 *
 * Lifecycle is the whole point: a card is live (`fresh`/`seen`) until
 * the user resolves it, then it stays on the board as a receipt —
 * dashed border, live actions swapped for the `receipt` line — until
 * its slot is replaced with new content. Cards never just vanish;
 * disappearing intelligence reads as noise, receipts read as a
 * relationship.
 *
 *   <BoardCard state={slot.state} live={<BoardActionRow>…</BoardActionRow>}
 *              receipt={<>已收下 · 今天 7:41</>}>
 *     <BoardKindLabel>行迹</BoardKindLabel>
 *     …kind body (persistent)…
 *   </BoardCard>
 *
 * `children` stay visible in every state; `live` is only shown while
 * the card is unresolved; `receipt` replaces it after. `dismissed`
 * additionally dims the card.
 */
export type BoardCardState = "fresh" | "seen" | "resolved" | "dismissed";

const TERMINAL_STATES: ReadonlySet<BoardCardState> = new Set([
  "resolved",
  "dismissed",
]);

export function BoardCard({
  state = "fresh",
  children,
  live,
  receipt,
  className,
  style,
}: {
  state?: BoardCardState;
  /** Persistent card body — kind label, quotes, rows. */
  children: React.ReactNode;
  /** Live-only zone (actions, verdict buttons). Hidden once terminal. */
  live?: React.ReactNode;
  /** Receipt line content shown once terminal. */
  receipt?: React.ReactNode;
  className?: string;
  /** Merged with the card's own style (e.g. entrance animationDelay). */
  style?: React.CSSProperties;
}) {
  const terminal = TERMINAL_STATES.has(state);
  return (
    <div
      data-state={state}
      className={cn(
        "glass-card rounded-[18px] px-4 py-3.5",
        // Spring, not ease-in-out: the resolve swap (border → dashed,
        // dismissed dim) should read as the card settling, per the
        // design language.
        "transition-[opacity,border-color] duration-300 [transition-timing-function:var(--ease-spring)]",
        state === "dismissed" && "opacity-60",
        className,
      )}
      // Inline so the dashed receipt border wins over the .glass-card
      // recipe's border shorthand regardless of stylesheet order.
      style={{ ...style, ...(terminal ? { borderStyle: "dashed" } : null) }}
    >
      {children}
      {!terminal ? live : null}
      {terminal && receipt ? (
        // The receipt fades up into the space the action row vacated —
        // resolution reads as the card exhaling, not content popping.
        <BoardReceipt className="animate-fade-up mt-3">{receipt}</BoardReceipt>
      ) : null}
    </div>
  );
}

/**
 * BoardCardFallbackBody — the unknown-kind safety net (plan-18 §4.2
 * contract carried over to boards): when a producer ships a kind ahead
 * of its renderer, the client prints the card's plain-text title and
 * description literally instead of dropping the card. This is why
 * every payload text field must be a plain user-facing string.
 */
export function BoardCardFallbackBody({
  title,
  description,
  className,
}: {
  title: string;
  description?: string;
  className?: string;
}) {
  return (
    <div className={cn("min-w-0", className)}>
      <p className="m-0 text-[14px] text-fg-1">{title}</p>
      {description ? (
        <p className="m-0 mt-1 text-[12.5px] text-fg-3">{description}</p>
      ) : null}
    </div>
  );
}
