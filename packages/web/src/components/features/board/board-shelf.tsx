"use client";

/**
 * BoardShelf — the embedded pulse preview on the memories page (the
 * BoardSection). Founder direction (2026-09 revision): the shelf is a
 * fixed 2×2 GRID of compact tiles — two per row, at most two rows.
 * It never expands in place anymore; tapping a tile NAVIGATES to the
 * /pulse surface with that card focused (`?focus=<slot_key>`), because
 * the memories page is a preview and the pulse page is the real
 * surface. The earlier in-place expansion (BOARD_SHELF_RULES R3/R6)
 * is retired — one content, one home.
 *
 * Tile priority, reading order (left→right, top→bottom):
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
 *
 * Overflow: with more than 4 tiles, the 4th cell becomes an overflow
 * tile ("还有 N 条动态 · 查看全部") routing to /pulse — content is
 * never silently hidden. Same-kind LIVE slots still collapse into ONE
 * tile with a depth badge — the 等你 deck metaphor extended to every
 * kind. No ghost tile (2026-09): creation is chrome on the /pulse
 * page's header, not preview content — a free cell stays free.
 *
 * Resolved / dismissed receipts are deliberately EXCLUDED: the shelf
 * is "what's new", receipts belong to the pulse page's archive. A
 * tile's hover/long-press × dismisses in place (optimistic — the tile
 * leaves immediately and lives on only as a receipt). Decision tiles
 * carry no ×: a decision needs an answer, not a swipe-away (and the
 * server refuses plain dismiss on decision kinds).
 */

import { useRef, useState, type ReactNode } from "react";
import type { Board, BoardSlot } from "memax-sdk";
import { ArrowRight, X } from "lucide-react";
import { BoardKindLabel } from "@memaxlabs/ui";
import { useInterpolate, useLocale } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import type { CustomBoardWithSlots } from "@/hooks/use-board";
import { boardKindStripSummary, slotContentTime } from "./board-kind-registry";
import { boardDisplayTitle } from "./board-custom-boards";
import {
  boardKindVisual,
  COOKING_KIND,
  HIGHLIGHT_KIND,
  WAITING_KIND,
} from "./board-kind-visuals";
import {
  groupWaitingByKind,
  type BoardNotificationCardModel,
} from "./board-notification-cards";

/** The grid is 两行两个 — two columns, at most two rows. */
const SHELF_CAPACITY = 4;

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
  slots,
  customBoards,
  cookingBoards,
  onOpenDeck,
  onOpenSlot,
  onOpenBoards,
  onDismissSlot,
}: {
  /** 等你 decisions — first tile shows the top one + depth badge. */
  waiting: readonly BoardNotificationCardModel[];
  /** System-board slots; receipts are filtered out here. */
  slots: readonly BoardSlot[];
  /**
   * Every custom board with its slots — live cards join the tile
   * ordering after the system tiles, tagged with the board title.
   */
  customBoards: readonly CustomBoardWithSlots[];
  /** Custom boards still cooking — rendered as promise tiles. */
  cookingBoards: readonly Board[];
  /** Deck/highlight tile tapped → the pulse surface (top of stream). */
  onOpenDeck: () => void;
  /** Slot tile tapped → the pulse surface focused on this card. */
  onOpenSlot: (slotKey: string) => void;
  /** Cooking/overflow tile tapped → the full /pulse surface. */
  onOpenBoards: () => void;
  /** Tile × on a slot tile → resolve action="dismiss" (optimistic). */
  onDismissSlot?: (slotKey: string, boardId: string) => void;
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
          onDismissSlot
            ? () => onDismissSlot(slot.slot_key, slot.board_id)
            : undefined
        }
        dismissLabel={t.board.actionDismiss}
      />,
    );
  }
  // Custom-board live cards — after the system tiles, each tagged
  // with its board title (the badge pill doubles as the tag here).
  // Tap → the pulse surface focused on that card.
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
          onClick={() => onOpenSlot(slot.slot_key)}
          onDismiss={
            onDismissSlot
              ? () => onDismissSlot(slot.slot_key, slot.board_id)
              : undefined
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

  // 两行两个 — the grid holds at most 4 cells. Overflowing content is
  // never silently dropped: the last cell becomes an overflow tile
  // counting what's behind it. A free cell stays free — the shelf is
  // a preview, and board creation lives on the /pulse page's header.
  const overflow = tiles.length - SHELF_CAPACITY;
  const cells =
    overflow > 0
      ? [
          ...tiles.slice(0, SHELF_CAPACITY - 1),
          <BoardOverflowTile
            key="overflow"
            count={overflow + 1}
            onClick={onOpenBoards}
          />,
        ]
      : tiles;

  return (
    <div className="grid grid-cols-2 items-stretch gap-2 pb-1">{cells}</div>
  );
}

/**
 * BoardTile — one compact shelf entry. Product-specific composition of
 * the ui atoms (clamped text on the shared grid cell), so it lives
 * here rather than in @memaxlabs/ui. All text is LEFT-aligned in a
 * strict vertical stack: eyebrow (icon + kind) → title → body/meta →
 * quiet generated-at line. No eyebrow dot — the kind icon already
 * carries the category, and dot+icon double-encoded it (founder
 * feedback, 2026-09).
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
  /** Kind (or pseudo-kind) — resolves eyebrow visuals. */
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
      className="group relative min-w-0"
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
 * BoardOverflowTile — the honest 4th cell when the shelf holds more
 * than it can show: counts what's behind it and routes to /pulse.
 */
function BoardOverflowTile({
  count,
  onClick,
}: {
  count: number;
  onClick: () => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  return (
    <button
      type="button"
      data-board-tile="overflow"
      onClick={onClick}
      className="glass-card flex h-full w-full min-w-0 cursor-pointer flex-col items-start justify-center gap-1 rounded-[16px] px-3.5 py-3 text-left transition-colors [transition-timing-function:var(--ease-spring)] hover:bg-surface-1"
    >
      <span className="text-[13px] leading-snug text-fg-2">
        {interpolate(t.board.shelfOverflow, { n: count })}
      </span>
      <span className="inline-flex items-center gap-1 text-[11.5px] text-fg-3">
        {t.board.shelfViewAll}
        <ArrowRight className="h-3 w-3" aria-hidden />
      </span>
    </button>
  );
}
