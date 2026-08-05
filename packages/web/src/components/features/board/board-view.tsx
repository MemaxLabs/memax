"use client";

import type { BoardSlot } from "memax-sdk";
import {
  BoardAction,
  BoardActionRow,
  BoardCard,
  Skeleton,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useHubBoard, useResolveBoardSlot } from "@/hooks/use-board";
import { renderBoardSlotBody } from "./board-kind-registry";

/**
 * BoardView — the pulse board host (plan 25). Fetches the hub's board,
 * renders each occupied slot through the kind registry inside the
 * BoardCard lifecycle molecule, and wires the shared resolve verbs.
 *
 * P0: mechanism only — the board is empty until Lane A producers land
 * (P1), so this component renders nothing when there are no slots and
 * is not yet mounted on the hub home. Kitchen section 44 is the visual
 * reference for everything it composes.
 */
export function BoardView({ hubId }: { hubId: string }) {
  const { data, isPending, isError } = useHubBoard(hubId);
  const resolve = useResolveBoardSlot(hubId);

  if (isPending) {
    return (
      <div className="flex flex-col gap-2.5" data-testid="board-loading">
        <Skeleton className="h-24 w-full rounded-[18px]" />
        <Skeleton className="h-24 w-full rounded-[18px]" />
      </div>
    );
  }
  // Board errors and empty boards are silent in P0: the surface only
  // earns screen space once it has cards to show.
  if (isError || !data || data.slots.length === 0) return null;

  return (
    <div className="flex flex-col gap-2.5">
      {data.slots.map((slot) => (
        <BoardSlotCard
          key={slot.slot_key}
          slot={slot}
          onResolve={(action) =>
            resolve.mutate({ slotKey: slot.slot_key, action })
          }
        />
      ))}
    </div>
  );
}

function BoardSlotCard({
  slot,
  onResolve,
}: {
  slot: BoardSlot;
  onResolve: (action: "ack" | "dismiss") => void;
}) {
  const { t } = useLocale();
  return (
    <BoardCard
      state={slot.state}
      live={
        <BoardActionRow>
          <BoardAction emphasis="quiet" onClick={() => onResolve("ack")}>
            {t.board.actionAck}
          </BoardAction>
          <BoardAction emphasis="quiet" onClick={() => onResolve("dismiss")}>
            {t.board.actionDismiss}
          </BoardAction>
        </BoardActionRow>
      }
      receipt={
        slot.resolution?.action === "dismiss"
          ? t.board.receiptDismissed
          : t.board.receiptAcked
      }
    >
      {renderBoardSlotBody(slot, t)}
    </BoardCard>
  );
}
