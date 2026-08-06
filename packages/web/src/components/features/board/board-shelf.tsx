"use client";

/**
 * BoardShelf — the collapsed form of the embedded pulse board (the
 * memories-page BoardSection). Founder direction (2026-08): the pulse
 * IS the first section of the memories page — collapsed it is a shelf
 * of compact tiles in AT MOST two horizontally-scrollable rows;
 * expanding it in place restores the full vertical card layout.
 *
 * Tile priority, left → right:
 *   1. the 等你 deck tile — the only thing actually blocked on the
 *      user, so it always leads. Shows the top decision + a "还有 N 件"
 *      badge for the pile behind it.
 *   2. live Lane B cards (dreamlog / echo / thread / openq / pattern /
 *      musing / decision_gate — the fresh intelligence).
 *   3. the capsule (real content, but a year old — it can wait).
 *   4. the activity strip-tile (counts; worth knowing, never urgent).
 *   5. custom boards still 酝酿中 (cooking) — a promise, not content.
 *
 * Resolved / dismissed receipts are deliberately EXCLUDED: the shelf
 * is "what's new", receipts belong to the expanded layout's strips.
 *
 * Scrolling is contained: `overflow-x-auto` lives on the shelf's own
 * container so the page never pans horizontally (the root overflow-x
 * clip is global).
 */

import type { ReactNode } from "react";
import type { Board, BoardSlot } from "memax-sdk";
import { BoardKindLabel } from "@memaxlabs/ui";
import { useInterpolate, useLocale } from "@/i18n";
import { boardKindStripSummary } from "./board-kind-registry";
import { boardDisplayTitle } from "./board-custom-boards";
import type { BoardNotificationCardModel } from "./board-notification-cards";

/**
 * Shelf ordering: lower sorts earlier. Lane B kinds (and any unknown
 * future kind — new producers should lead, not trail) share the top
 * group; capsule and activity are explicitly demoted per the priority
 * list above. Sort is stable, so server order survives within a group.
 */
const SHELF_KIND_DEMOTION: Record<string, number> = {
  capsule: 1,
  activity: 2,
};

function shelfSlotPriority(kind: string): number {
  return SHELF_KIND_DEMOTION[kind] ?? 0;
}

/**
 * Live slots in shelf order. Exported for tests: 等你 leads (handled
 * by the caller), resolved receipts never appear.
 */
export function orderShelfSlots(slots: readonly BoardSlot[]): BoardSlot[] {
  return slots
    .filter((slot) => slot.state === "fresh" || slot.state === "seen")
    .sort((a, b) => shelfSlotPriority(a.kind) - shelfSlotPriority(b.kind));
}

export function BoardShelf({
  waiting,
  slots,
  cookingBoards,
  onOpenDeck,
  onOpenSlot,
  onOpenBoards,
}: {
  /** 等你 decisions — first tile shows the top one + depth badge. */
  waiting: readonly BoardNotificationCardModel[];
  /** System-board slots; receipts are filtered out here. */
  slots: readonly BoardSlot[];
  /** Custom boards still cooking — rendered as promise tiles. */
  cookingBoards: readonly Board[];
  /** Deck tile tapped → expand the shelf (deck renders on top). */
  onOpenDeck: () => void;
  /** Slot tile tapped → expand the shelf with this card open. */
  onOpenSlot: (slotKey: string) => void;
  /** Cooking tile tapped → the full /pulse surface owns boards. */
  onOpenBoards: () => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const liveSlots = orderShelfSlots(slots);
  const topWaiting = waiting[0];

  const tiles: ReactNode[] = [];
  if (topWaiting) {
    tiles.push(
      <BoardTile
        key={`deck-${topWaiting.id}`}
        star
        label={t.board.kindWaiting}
        badge={
          waiting.length > 1
            ? interpolate(t.board.deckMore, { n: waiting.length - 1 })
            : undefined
        }
        title={topWaiting.title}
        body={topWaiting.description || undefined}
        onClick={onOpenDeck}
      />,
    );
  }
  for (const slot of liveSlots) {
    const strip = boardKindStripSummary(slot, t);
    tiles.push(
      <BoardTile
        key={slot.slot_key}
        label={strip.label}
        title={slot.title}
        // Strips often repeat the slot title as their detail (echo,
        // thread, …) — a meta line that re-reads the title is noise.
        meta={strip.detail !== slot.title ? strip.detail : undefined}
        onClick={() => onOpenSlot(slot.slot_key)}
      />,
    );
  }
  for (const board of cookingBoards) {
    tiles.push(
      <BoardTile
        key={`board-${board.id}`}
        star
        label={t.board.cookingLabel}
        title={boardDisplayTitle(board, t.board.title)}
        meta={board.instruction || t.board.cookingBody}
        onClick={onOpenBoards}
      />,
    );
  }

  if (tiles.length === 0) return null;

  // ≤4 tiles fit one comfortable row; more stack into two rows that
  // scroll together. `grid-flow-col` fills columns top-to-bottom, so
  // priority still reads left → right in pairs.
  const twoRows = tiles.length > 4;
  return (
    <div className="-mx-0.5 overflow-x-auto px-0.5 pb-1">
      <div
        className={`grid w-max snap-x snap-proximity grid-flow-col gap-2 ${
          twoRows ? "grid-rows-2" : "grid-rows-1"
        }`}
      >
        {tiles}
      </div>
    </div>
  );
}

/**
 * BoardTile — one compact shelf entry. Product-specific composition of
 * the ui atoms (fixed width, clamped text), so it lives here rather
 * than in @memaxlabs/ui.
 */
function BoardTile({
  label,
  star = false,
  badge,
  title,
  body,
  meta,
  onClick,
}: {
  label: string;
  star?: boolean;
  /** Deck-depth pill, e.g. "还有 6 件". */
  badge?: string;
  title: string;
  /** Clamped 3-line description (deck tile only). */
  body?: string;
  /** One-line meta (slot strips, cooking instruction). */
  meta?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="glass-card h-full w-[272px] shrink-0 snap-start cursor-pointer rounded-[16px] px-3.5 py-3 text-left transition-colors [transition-timing-function:var(--ease-spring)] hover:bg-surface-1"
    >
      <div className="flex items-start justify-between gap-2">
        <BoardKindLabel star={star} className="mb-1 min-w-0">
          {label}
        </BoardKindLabel>
        {badge ? (
          <span className="shrink-0 rounded-full bg-surface-2 px-2 py-0.5 text-[10.5px] font-medium text-fg-2">
            {badge}
          </span>
        ) : null}
      </div>
      <p className="m-0 line-clamp-2 text-[13px] leading-snug text-fg-1">
        {title}
      </p>
      {body ? (
        <p className="m-0 mt-1 line-clamp-3 text-[12px] leading-snug text-fg-3">
          {body}
        </p>
      ) : null}
      {meta ? (
        <p className="m-0 mt-1 line-clamp-1 text-[11.5px] leading-snug text-fg-3">
          {meta}
        </p>
      ) : null}
    </button>
  );
}
