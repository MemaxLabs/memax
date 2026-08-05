"use client";

import { useEffect, useRef } from "react";
import type { BoardSlot } from "memax-sdk";
import { BoardAction, BoardActionRow, BoardCard } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useActiveHub } from "@/lib/auth";
import { trackEvent } from "@/lib/posthog";
import { useHubBoard, useResolveBoardSlot } from "@/hooks/use-board";
import {
  boardKindActionLabels,
  renderBoardSlotBody,
} from "./board-kind-registry";
// Side-effect import: registers the Lane A kind renderers before the
// first render so no card flashes through the fallback.
import "./board-kinds";

/**
 * BoardView — the pulse board host (plan 25). Fetches the hub's board,
 * renders each occupied slot through the kind registry inside the
 * BoardCard lifecycle molecule, and wires the shared resolve verbs.
 * Renders nothing while the board is empty — the surface only earns
 * screen space once it has cards.
 */
export function BoardView({ hubId }: { hubId: string }) {
  const { data, isPending, isError } = useHubBoard(hubId);
  const resolve = useResolveBoardSlot(hubId);

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

  // No skeleton: most hubs have no cards yet (dreams haven't run), and
  // a flash-of-skeleton on every hub home load would make the board
  // feel like a broken feature instead of a quiet surface that appears
  // when it has something to say. The layout wrapper (padding included)
  // renders only here, so the empty state contributes zero height.
  if (isPending || isError || !data || data.slots.length === 0) return null;

  return (
    <div className="mb-3 flex flex-col gap-2.5">
      {data.slots.map((slot, index) => (
        <BoardSlotCard
          key={slot.slot_key}
          slot={slot}
          entranceIndex={index}
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
      ))}
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

function BoardSlotCard({
  slot,
  entranceIndex,
  onResolve,
}: {
  slot: BoardSlot;
  entranceIndex: number;
  onResolve: (action: "ack" | "dismiss") => void;
}) {
  const { t } = useLocale();
  // Per-kind resolve verbs: "都对 · 收下" on a trace reads differently
  // from "收下" on an observation. Kinds without registered labels get
  // the generic pair.
  const labels = boardKindActionLabels(slot.kind);
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
        </BoardActionRow>
      }
      receipt={
        slot.resolution?.action === "dismiss"
          ? t.board.receiptDismissed
          : t.board.receiptAcked
      }
    >
      {renderBoardSlotBody(slot)}
    </BoardCard>
  );
}
