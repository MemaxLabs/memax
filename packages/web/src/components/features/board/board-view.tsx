"use client";

import { useEffect, useRef, useState } from "react";
import type { BoardSlot } from "memax-sdk";
import {
  BoardAction,
  BoardActionRow,
  BoardCard,
  BoardSlotStrip,
  BoardVoiceStar,
  InfoPopover,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useActiveHub } from "@/lib/auth";
import { trackEvent } from "@/lib/posthog";
import { useHubBoard, useResolveBoardSlot } from "@/hooks/use-board";
import {
  boardKindActionLabels,
  boardKindPurpose,
  boardKindStripSummary,
  renderBoardSlotBody,
} from "./board-kind-registry";
// Side-effect import: registers the Lane A kind renderers before the
// first render so no card flashes through the fallback.
import "./board-kinds";

/**
 * BoardView — the pulse board host (plan 25). The layout answer to
 * "cards eat the page": banding. Only the FIRST live card renders
 * expanded (the hero); every other slot collapses to a one-line
 * SlotStrip that expands on tap and can be collapsed again. Resolved
 * receipts always render as strips. The whole surface is headed by a
 * one-line board title with a purpose explainer, so the surface names
 * itself instead of appearing as anonymous cards.
 */
export function BoardView({ hubId }: { hubId: string }) {
  const { t } = useLocale();
  const { data, isPending, isError } = useHubBoard(hubId);
  const resolve = useResolveBoardSlot(hubId);
  const [openSlots, setOpenSlots] = useState<ReadonlySet<string>>(new Set());

  // One impression event per board load (not per re-render).
  const trackedFor = useRef<string | null>(null);
  const slotCount = data?.slots.length ?? 0;
  useEffect(() => {
    if (!data || slotCount === 0 || trackedFor.current === data.board.id) {
      return;
    }
    trackedFor.current = data.board.id;
    trackEvent("board_viewed", {
      hub_id: hubId,
      slot_count: slotCount,
      kinds: data.slots.map((s) => s.kind),
    });
  }, [data, hubId, slotCount]);

  if (isPending || isError || !data || data.slots.length === 0) return null;

  const liveSlots = data.slots.filter(
    (s) => s.state === "fresh" || s.state === "seen",
  );
  const heroKey = liveSlots[0]?.slot_key;

  const toggleSlot = (slotKey: string, willOpen: boolean) => {
    setOpenSlots((prev) => {
      const next = new Set(prev);
      if (willOpen) {
        next.add(slotKey);
      } else {
        next.delete(slotKey);
      }
      return next;
    });
    if (willOpen) {
      trackEvent("board_card_expand", { hub_id: hubId, slot_key: slotKey });
    }
  };

  return (
    <div className="mb-3 flex flex-col gap-2">
      <div className="flex items-center gap-1 px-0.5">
        <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-fg-3">
          <BoardVoiceStar /> {t.board.title}
        </span>
        <InfoPopover
          ariaLabel={t.board.purposeAria}
          title={t.board.title}
          body={t.board.purpose}
        />
      </div>
      {data.slots.map((slot, index) => {
        const expanded =
          slot.slot_key === heroKey || openSlots.has(slot.slot_key);
        return (
          <BoardSlotEntry
            key={slot.slot_key}
            slot={slot}
            expanded={expanded}
            entranceIndex={index}
            onToggle={(willOpen) => toggleSlot(slot.slot_key, willOpen)}
            onResolve={(action) => {
              trackEvent("board_card_action", {
                hub_id: hubId,
                kind: slot.kind,
                slot_key: slot.slot_key,
                action,
              });
              resolve.mutate({ slotKey: slot.slot_key, action });
            }}
          />
        );
      })}
    </div>
  );
}

/**
 * BoardSection — mounts inside TopicGrid's content column, below the
 * hub header and pinned notifications (the parent provides the
 * max-width column and horizontal padding). Renders nothing — zero
 * height — until the hub has cards, so card-less hubs keep the exact
 * pre-board layout.
 */
export function BoardSection() {
  const { hubFilter } = useActiveHub();
  if (!hubFilter) return null;
  return <BoardView hubId={hubFilter} />;
}

function BoardSlotEntry({
  slot,
  expanded,
  entranceIndex,
  onToggle,
  onResolve,
}: {
  slot: BoardSlot;
  expanded: boolean;
  entranceIndex: number;
  onToggle: (willOpen: boolean) => void;
  onResolve: (action: "ack" | "dismiss") => void;
}) {
  const { t } = useLocale();

  if (!expanded) {
    // Collapsed band: one line — kind name + per-kind summary. Resolved
    // receipts also live here so they stop costing vertical space.
    const terminal = slot.state === "resolved" || slot.state === "dismissed";
    return (
      <BoardSlotStrip
        label={boardKindStripSummary(slot, t).label}
        detail={
          terminal
            ? slot.resolution?.action === "dismiss"
              ? t.board.receiptDismissed
              : t.board.receiptAcked
            : boardKindStripSummary(slot, t).detail
        }
        open={false}
        onToggle={() => onToggle(true)}
        className={terminal ? "opacity-70" : undefined}
      />
    );
  }

  const labels = boardKindActionLabels(slot.kind);
  const purpose = boardKindPurpose(slot.kind, t);
  return (
    <BoardCard
      state={slot.state}
      className="animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
      live={
        <BoardActionRow>
          <BoardAction emphasis="primary" onClick={() => onResolve("ack")}>
            {labels?.ack?.(t) ?? t.board.actionAck}
          </BoardAction>
          <BoardAction emphasis="quiet" onClick={() => onResolve("dismiss")}>
            {labels?.dismiss?.(t) ?? t.board.actionDismiss}
          </BoardAction>
          <BoardAction
            emphasis="quiet"
            className="ml-auto"
            onClick={() => onToggle(false)}
          >
            {t.board.collapse}
          </BoardAction>
        </BoardActionRow>
      }
      receipt={
        slot.resolution?.action === "dismiss"
          ? t.board.receiptDismissed
          : t.board.receiptAcked
      }
    >
      {purpose ? (
        <div className="float-right ml-2">
          <InfoPopover
            ariaLabel={t.board.purposeAria}
            title={t.board.title}
            body={purpose}
            side="left"
          />
        </div>
      ) : null}
      {renderBoardSlotBody(slot)}
    </BoardCard>
  );
}
