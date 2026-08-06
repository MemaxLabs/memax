"use client";

/**
 * BoardShelf — the collapsed form of the embedded pulse board (the
 * memories-page BoardSection). Founder direction (2026-08): the pulse
 * IS the first section of the memories page — collapsed it is a shelf
 * of compact tiles in EXACTLY ONE horizontally-scrollable row (two
 * rows read as a wall, not a shelf); expanding it in place restores
 * the full vertical card layout.
 *
 * Tile priority, left → right:
 *   1. the 等你 deck tile — the only thing actually blocked on the
 *      user, so it always leads. Shows the top decision + a "还有 N 件"
 *      badge for the pile behind it.
 *   2. highlights (a new member joined) — news, right behind the
 *      decisions.
 *   3. live Lane B cards (dreamlog / echo / thread / openq / pattern /
 *      musing / decision_gate — the fresh intelligence).
 *   4. the capsule (real content, but a year old — it can wait).
 *   5. the activity strip-tile (counts; worth knowing, never urgent).
 *   6. custom-board live cards — tagged with their board title.
 *   7. custom boards still 酝酿中 (cooking) — a promise, not content.
 *   8. the ghost tile — the latent new-board affordance, always last.
 *
 * Tiles are NOT uniform: shape follows the card's purpose via the size
 * variant in board-kind-visuals (wide decision / standard insight /
 * square artifact / slim counter), on one shared vertical rhythm. All
 * text is left-aligned in a strict eyebrow → title → meta stack, with
 * a quiet relative generated-at line closing each tile. Same-kind LIVE
 * slots collapse into ONE tile with a depth badge — the 等你 deck
 * metaphor extended to every kind.
 *
 * Resolved / dismissed receipts are deliberately EXCLUDED: the shelf
 * is "what's new", receipts belong to the expanded layout's strips. A
 * tile's hover/long-press × dismisses in place (optimistic — the tile
 * leaves immediately and lives on only as a receipt). Decision tiles
 * carry no ×: a decision needs an answer, not a swipe-away (and the
 * server refuses plain dismiss on decision kinds).
 *
 * Scrolling is contained: `overflow-x-auto` lives on the shelf's own
 * container so the page never pans horizontally (the root overflow-x
 * clip is global).
 */

import { useRef, useState, type ReactNode } from "react";
import type { Board, BoardSlot } from "memax-sdk";
import { X } from "lucide-react";
import { BoardKindLabel, BoardVoiceStar } from "@memaxlabs/ui";
import { useInterpolate, useLocale } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { inboxKindLabel } from "@/components/features/inbox/inbox-control";
import type { CustomBoardWithSlots } from "@/hooks/use-board";
import { boardKindStripSummary, slotContentTime } from "./board-kind-registry";
import { boardDisplayTitle } from "./board-custom-boards";
import {
  boardKindVisual,
  COOKING_KIND,
  HIGHLIGHT_KIND,
  WAITING_KIND,
  type BoardTileSize,
} from "./board-kind-visuals";
import {
  groupWaitingByKind,
  type BoardNotificationCardModel,
} from "./board-notification-cards";

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

/**
 * Same-kind stacking: LIVE slots sharing a kind (possible on custom
 * boards) group into one deck — one tile/card with a depth count, the
 * same metaphor as the 等你 pile. Grouping preserves the input order's
 * first appearance; terminal slots never group (they're receipts).
 * Exported for BoardView, which decks the expanded stream identically.
 */
export function groupSlotsByKind(slots: readonly BoardSlot[]): BoardSlot[][] {
  const order: string[] = [];
  const byKind = new Map<string, BoardSlot[]>();
  for (const slot of slots) {
    if (slot.state !== "fresh" && slot.state !== "seen") continue;
    if (!byKind.has(slot.kind)) {
      byKind.set(slot.kind, []);
      order.push(slot.kind);
    }
    byKind.get(slot.kind)!.push(slot);
  }
  return order.map((kind) => byKind.get(kind)!);
}

export function BoardShelf({
  waiting,
  highlights,
  slots,
  customBoards,
  cookingBoards,
  onOpenDeck,
  onOpenSlot,
  onOpenBoards,
  onDismissSlot,
  onDismissNotification,
}: {
  /** 等你 decisions — first tile shows the top one + depth badge. */
  waiting: readonly BoardNotificationCardModel[];
  /**
   * Highlights (hub_member_joined) — one tile each, right after the
   * 等你 deck tiles; tapping expands the shelf where the standalone
   * BoardHighlightCard renders.
   */
  highlights: readonly BoardNotificationCardModel[];
  /** System-board slots; receipts are filtered out here. */
  slots: readonly BoardSlot[];
  /**
   * Every custom board with its slots — live cards join the tile
   * ordering after the system tiles, tagged with the board title.
   */
  customBoards: readonly CustomBoardWithSlots[];
  /** Custom boards still cooking — rendered as promise tiles. */
  cookingBoards: readonly Board[];
  /** Deck tile tapped → expand the shelf (deck renders on top). */
  onOpenDeck: () => void;
  /** Slot tile tapped → expand the shelf with this card open. */
  onOpenSlot: (slotKey: string) => void;
  /** Cooking/ghost tile tapped → the full /pulse surface owns boards. */
  onOpenBoards: () => void;
  /** Tile × on a slot tile → resolve action="dismiss" (optimistic). */
  onDismissSlot?: (slotKey: string) => void;
  /** Tile × on a highlight tile → notification dismiss (optimistic). */
  onDismissNotification?: (id: string) => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const age = (iso: string) => formatAge(iso, t, interpolate);
  const stackBadge = (extra: number) =>
    interpolate(t.board.stackCount, { n: extra });

  const tiles: ReactNode[] = [];
  // One tile per same-kind deck: contradictions stack, but an invite
  // is a different decision and gets its own tile.
  for (const group of groupWaitingByKind(waiting)) {
    const top = group[0];
    tiles.push(
      <BoardTile
        key={`deck-${top.kind}`}
        kind={WAITING_KIND}
        star
        label={t.board.kindWaiting}
        badge={group.length > 1 ? stackBadge(group.length - 1) : undefined}
        title={top.title}
        body={top.description || undefined}
        when={age(top.item.created_at)}
        onClick={onOpenDeck}
      />,
    );
  }
  // Highlights (new member joined) — high-signal news, right behind
  // the decisions and ahead of the lane B intelligence.
  for (const card of highlights) {
    tiles.push(
      <BoardTile
        key={`hl-${card.id}`}
        kind={HIGHLIGHT_KIND}
        star
        label={inboxKindLabel(card.item, t)}
        title={card.title}
        when={age(card.item.created_at)}
        onClick={onOpenDeck}
        onDismiss={
          onDismissNotification
            ? () => onDismissNotification(card.id)
            : undefined
        }
        dismissLabel={t.board.actionDismiss}
      />,
    );
  }
  for (const group of groupSlotsByKind(orderShelfSlots(slots))) {
    const slot = group[0];
    const strip = boardKindStripSummary(slot, t);
    tiles.push(
      <BoardTile
        key={slot.slot_key}
        kind={slot.kind}
        label={strip.label}
        badge={group.length > 1 ? stackBadge(group.length - 1) : undefined}
        title={slot.title}
        // Strips often repeat the slot title as their detail (echo,
        // thread, …) — a meta line that re-reads the title is noise.
        meta={strip.detail !== slot.title ? strip.detail : undefined}
        when={age(slotContentTime(slot))}
        onClick={() => onOpenSlot(slot.slot_key)}
        onDismiss={
          onDismissSlot ? () => onDismissSlot(slot.slot_key) : undefined
        }
        dismissLabel={t.board.actionDismiss}
      />,
    );
  }
  // Custom-board live cards — after the system tiles, each tagged
  // with its board title (the badge pill doubles as the tag here).
  // Tap → expand in place; the tagged card renders in the stream.
  for (const { board, slots: boardSlots } of customBoards) {
    for (const group of groupSlotsByKind(orderShelfSlots(boardSlots))) {
      const slot = group[0];
      const strip = boardKindStripSummary(slot, t);
      const tag = boardDisplayTitle(board, t.board.title);
      tiles.push(
        <BoardTile
          key={`custom-${board.id}-${slot.slot_key}`}
          kind={slot.kind}
          label={strip.label}
          badge={
            group.length > 1 ? `${tag} · ${stackBadge(group.length - 1)}` : tag
          }
          title={slot.title}
          meta={strip.detail !== slot.title ? strip.detail : undefined}
          when={age(slotContentTime(slot))}
          onClick={onOpenDeck}
          onDismiss={
            onDismissSlot ? () => onDismissSlot(slot.slot_key) : undefined
          }
          dismissLabel={t.board.actionDismiss}
        />,
      );
    }
  }
  for (const board of cookingBoards) {
    tiles.push(
      <BoardTile
        key={`board-${board.id}`}
        kind={COOKING_KIND}
        star
        label={t.board.cookingLabel}
        title={boardDisplayTitle(board, t.board.title)}
        meta={board.instruction || t.board.cookingBody}
        when={age(board.created_at)}
        onClick={onOpenBoards}
      />,
    );
  }

  if (tiles.length === 0) return null;

  return (
    <div className="-mx-0.5 overflow-x-auto px-0.5 pb-1">
      {/* ONE row, always — priority reads strictly left → right. */}
      <div className="flex w-max snap-x snap-proximity flex-row flex-nowrap items-stretch gap-2">
        {tiles}
        <BoardGhostTile onClick={onOpenBoards} />
      </div>
    </div>
  );
}

/** Tile size variants — width follows the card's purpose. */
const TILE_WIDTH: Record<BoardTileSize, string> = {
  wide: "w-[320px]",
  standard: "w-[272px]",
  square: "w-[200px]",
  slim: "w-[180px]",
};

/**
 * BoardTile — one compact shelf entry. Product-specific composition of
 * the ui atoms (kind-sized width, clamped text), so it lives here
 * rather than in @memaxlabs/ui. All text is LEFT-aligned in a strict
 * vertical stack: eyebrow (dot + icon + kind) → title → body/meta →
 * quiet generated-at line.
 */
function BoardTile({
  kind,
  label,
  star = false,
  badge,
  title,
  body,
  meta,
  when,
  onClick,
  onDismiss,
  dismissLabel,
}: {
  /** Kind (or pseudo-kind) — resolves size + eyebrow visuals. */
  kind: string;
  label: string;
  star?: boolean;
  /** Deck-depth pill, e.g. "还有 6 件", or the custom-board tag. */
  badge?: string;
  title: string;
  /** Clamped 3-line description (deck tile only). */
  body?: string;
  /** One-line meta (slot strips, cooking instruction). */
  meta?: string;
  /** Quiet relative generated-at line ("3 天前"). */
  when?: string;
  onClick: () => void;
  /** Hover/long-press × — dismiss in place without opening. */
  onDismiss?: () => void;
  dismissLabel?: string;
}) {
  const visual = boardKindVisual(kind);
  // Long-press reveals the × on touch surfaces (no hover there).
  const [pressRevealed, setPressRevealed] = useState(false);
  const pressTimer = useRef<number | undefined>(undefined);
  const clearPress = () => {
    if (pressTimer.current !== undefined) {
      window.clearTimeout(pressTimer.current);
      pressTimer.current = undefined;
    }
  };
  return (
    <div
      data-board-tile={kind}
      data-size={visual.tile}
      className={`group relative shrink-0 snap-start ${TILE_WIDTH[visual.tile]}`}
      onTouchStart={
        onDismiss
          ? () => {
              clearPress();
              pressTimer.current = window.setTimeout(
                () => setPressRevealed(true),
                450,
              );
            }
          : undefined
      }
      onTouchEnd={onDismiss ? clearPress : undefined}
      onTouchMove={onDismiss ? clearPress : undefined}
    >
      <button
        type="button"
        onClick={onClick}
        className="glass-card flex h-full w-full cursor-pointer flex-col items-start rounded-[16px] px-3.5 py-3 text-left transition-colors [transition-timing-function:var(--ease-spring)] hover:bg-surface-1"
      >
        <div className="flex w-full items-start justify-between gap-2">
          <BoardKindLabel
            star={star}
            dotColor={visual.dot}
            icon={visual.icon}
            className="mb-1 min-w-0"
          >
            {label}
          </BoardKindLabel>
          {badge ? (
            <span className="shrink-0 rounded-full bg-surface-2 px-2 py-0.5 text-[10.5px] font-medium text-fg-2">
              {badge}
            </span>
          ) : null}
        </div>
        <p className="m-0 line-clamp-2 text-left text-[13px] leading-snug text-fg-1">
          {title}
        </p>
        {body ? (
          <p className="m-0 mt-1 line-clamp-3 text-left text-[12px] leading-snug text-fg-3">
            {body}
          </p>
        ) : null}
        {meta ? (
          <p className="m-0 mt-1 line-clamp-1 text-left text-[11.5px] leading-snug text-fg-3">
            {meta}
          </p>
        ) : null}
        {when ? (
          <p className="m-0 mt-auto pt-1 text-left text-[10.5px] leading-snug text-fg-4">
            {when}
          </p>
        ) : null}
      </button>
      {onDismiss ? (
        <button
          type="button"
          aria-label={dismissLabel}
          onClick={onDismiss}
          className={`absolute right-1.5 top-1.5 cursor-pointer rounded-full bg-surface-2 p-1 text-fg-3 transition-opacity [transition-timing-function:var(--ease-spring)] hover:text-fg-1 focus-visible:opacity-100 group-focus-within:opacity-100 group-hover:opacity-100 ${
            pressRevealed ? "opacity-100" : "opacity-0"
          }`}
        >
          <X className="h-3 w-3" aria-hidden />
        </button>
      ) : null}
    </div>
  );
}

/**
 * BoardGhostTile — the latent new-board affordance closing the shelf:
 * dashed glass (the ComposePlusCell "fill me in" idiom), quiet ✦,
 * placeholder copy. Tapping routes to /pulse where the ghost CARD
 * morphs into the composer — the embedded shelf never composes.
 */
function BoardGhostTile({ onClick }: { onClick: () => void }) {
  const { t } = useLocale();
  return (
    <button
      type="button"
      data-board-tile="ghost"
      onClick={onClick}
      className="flex w-[200px] shrink-0 snap-start cursor-pointer flex-col items-center justify-center gap-1 rounded-[16px] bg-transparent px-3.5 py-3 transition-colors [transition-timing-function:var(--ease-spring)] hover:bg-surface-1"
      style={{ border: "1px dashed var(--glass-border)" }}
    >
      <BoardVoiceStar className="text-[13px]" />
      <span className="text-center text-[11.5px] leading-snug text-fg-4">
        {t.board.ghostTitle}
      </span>
    </button>
  );
}
