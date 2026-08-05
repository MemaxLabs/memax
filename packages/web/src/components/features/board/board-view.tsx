"use client";

import { useEffect, useRef, useState } from "react";
import type { BoardFeedbackVerdict, BoardSlot } from "memax-sdk";
import {
  BoardAction,
  BoardActionRow,
  BoardCard,
  BoardSlotStrip,
  InfoPopover,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useActiveHub } from "@/lib/auth";
import { trackEvent } from "@/lib/posthog";
import { useHubBoard, useResolveBoardSlot } from "@/hooks/use-board";
import { boardKindOptions, renderBoardSlotBody } from "./board-kind-registry";
// Side-effect import: registers the Lane A + Lane B kind renderers
// before the first render so no card flashes through the fallback.
import "./board-kinds";

/** Resolve verbs the host fires; "choose" is fired by the gate body itself. */
type BoardResolveAction = "ack" | "dismiss" | "feedback";

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
          onResolve={(action, verdict) => {
            trackEvent("board_card_action", {
              hub_id: hubId,
              kind: slot.kind,
              slot_key: slot.slot_key,
              action,
            });
            resolve.mutate({ slotKey: slot.slot_key, action, verdict });
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
  onResolve: (
    action: BoardResolveAction,
    verdict?: BoardFeedbackVerdict,
  ) => void;
}) {
  const { t } = useLocale();
  // Per-kind options: resolve verbs ("都对 · 收下" on a trace reads
  // differently from "收下" on an observation), the purpose popover,
  // the collapsed strip, feedback verbs, and whether the kind renders
  // its own actions (decision gates). Kinds without options get the
  // generic defaults.
  const options = boardKindOptions(slot.kind);
  const labels = options?.actions;
  const strip = options?.strip?.(t, slot);
  const purpose = options?.purpose?.(t);
  // Strip-registered kinds can be tucked away into their one-line
  // form. Cards start expanded — a fresh card earns its space — and
  // the strip is a per-session presentation choice, not persisted.
  const [collapsed, setCollapsed] = useState(false);

  if (collapsed && strip) {
    return (
      <BoardSlotStrip
        label={strip.label}
        detail={strip.detail}
        open={false}
        onToggle={() => setCollapsed(false)}
        className="animate-fade-up"
      />
    );
  }

  return (
    <BoardCard
      state={slot.state}
      className="relative animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
      live={
        <BoardActionRow>
          {!options?.hideDefaultActions ? (
            <>
              <BoardAction emphasis="primary" onClick={() => onResolve("ack")}>
                {labels?.ack?.(t) ?? t.board.actionAck}
              </BoardAction>
              <BoardAction
                emphasis="quiet"
                onClick={() => onResolve("dismiss")}
              >
                {labels?.dismiss?.(t) ?? t.board.actionDismiss}
              </BoardAction>
            </>
          ) : null}
          {options?.feedback ? (
            <>
              <BoardAction
                emphasis="quiet"
                onClick={() => onResolve("feedback", "accurate")}
              >
                {t.board.feedbackAccurate}
              </BoardAction>
              <BoardAction
                emphasis="quiet"
                onClick={() => onResolve("feedback", "inaccurate")}
              >
                {t.board.feedbackInaccurate}
              </BoardAction>
            </>
          ) : null}
          {strip ? (
            <BoardAction
              emphasis="quiet"
              className="ml-auto"
              onClick={() => setCollapsed(true)}
            >
              {t.board.actionCollapse}
            </BoardAction>
          ) : null}
        </BoardActionRow>
      }
      receipt={
        slot.resolution?.action === "dismiss"
          ? t.board.receiptDismissed
          : t.board.receiptAcked
      }
    >
      {purpose ? (
        <div className="absolute right-2.5 top-2.5">
          <InfoPopover
            ariaLabel={strip?.label ?? slot.kind}
            title={strip?.label ?? slot.kind}
            body={purpose}
            align="end"
          />
        </div>
      ) : null}
      {renderBoardSlotBody(slot)}
    </BoardCard>
  );
}
