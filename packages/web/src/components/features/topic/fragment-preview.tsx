"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import type { Memory } from "memax-sdk";
import { PreviewCard } from "@base-ui/react/preview-card";
import { Popover, PopoverContent, PopoverTrigger } from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { useAuth } from "@/lib/auth";
import { formatAge } from "@/lib/format-age";
import { resolveMemoryAttribution } from "@/lib/memory-attribution";
import { sanitizeSummary } from "@/lib/sanitize-metadata";
import { useMemoryForget } from "@/hooks/use-memory-forget";
import { useMemoryMove } from "@/hooks/use-memory-move";
import { AgentInlineIdentity } from "@/components/features/agent-inline-identity";
import { DestinationPicker } from "@/components/features/destination-picker";

/** Dwell before the preview opens; the close grace lets the pointer
 *  cross from the row into the card. */
const OPEN_DELAY_MS = 400;
const CLOSE_DELAY_MS = 120;

/**
 * FragmentPreviewCard — the thread-style quick look for a 记忆片段 row
 * (2026-09-17 founder spec, revised 09-18: a FLOATING card, never an
 * insertion — rows below must not move). Wraps the row as the hover
 * anchor; base-ui PreviewCard owns the hover timing and keeps the card
 * open while the pointer is inside it.
 *
 * Three actions, all existing capabilities: open, move to topic (same
 * DestinationPicker as the detail view), and forget — two-step, with
 * 留着 sitting where 忘记 was so a double click lands on the safe
 * target. While the move picker is open the card holds itself open
 * (base-ui would otherwise close it when the pointer enters the portal).
 *
 * `open` is controlled by the host so it can gate previews (mobile,
 * selection mode, drag) and guarantee at most one open card.
 */
export function FragmentPreviewCard({
  memory: m,
  topicPath,
  open,
  onOpenChange,
  onOpen,
  children,
}: {
  memory: Memory;
  topicPath?: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onOpen: () => void;
  /** The row this card previews (becomes the hover anchor). */
  children: ReactNode;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { user } = useAuth();
  const forget = useMemoryForget();
  const mover = useMemoryMove();
  const [moveOpen, setMoveOpen] = useState(false);
  const [confirmingForget, setConfirmingForget] = useState(false);
  // A press on the row vetoes a PENDING hover-open until the pointer
  // re-enters: base-ui's 400ms timer is not cancelled by a controlled
  // close, so without this a quick click would pop the card over the
  // detail panel it just opened.
  const pressedRef = useRef(false);

  // The confirm arm disarms itself after a moment so a stray second
  // click a minute later doesn't delete anything.
  useEffect(() => {
    if (!confirmingForget) return;
    const timer = setTimeout(() => setConfirmingForget(false), 4000);
    return () => clearTimeout(timer);
  }, [confirmingForget]);

  // Closing resets transient state so the next open starts clean.
  useEffect(() => {
    if (open) return;
    setMoveOpen(false);
    setConfirmingForget(false);
  }, [open]);

  const setMoveOpenHeld = (next: boolean) => {
    setMoveOpen(next);
    // The picker closed with the pointer somewhere in the portal — the
    // hover contract is broken, so the card closes with it.
    if (!next) onOpenChange(false);
  };

  const attribution = resolveMemoryAttribution(m, user?.id);
  const body = (m.content || sanitizeSummary(m.summary) || "").trim();
  const actionClass =
    "rounded-full border border-border/50 px-3 py-1 text-[12px] text-fg-2 transition-colors cursor-pointer hover:bg-surface-2 hover:text-fg-1";

  return (
    <PreviewCard.Root
      open={open}
      onOpenChange={(next, details) => {
        // Mouse-only: keyboard focus on the row must not pop a card the
        // keyboard can't reach (the row's own Enter → open still works).
        if (next && details.reason === "trigger-focus") return;
        if (next && pressedRef.current) return;
        // A held card (move picker open) ignores base-ui's hover-out.
        if (!next && moveOpen) return;
        onOpenChange(next);
      }}
    >
      <PreviewCard.Trigger
        render={<div />}
        delay={OPEN_DELAY_MS}
        closeDelay={CLOSE_DELAY_MS}
        // A press on the ROW (drag grip included) cancels the preview so
        // dnd-kit never measures a card that is about to vanish. React
        // re-dispatches events from the portaled popup through this
        // ancestor too, so check DOM containment: a press inside the
        // card must not close it before its click lands.
        onPointerDownCapture={(e) => {
          if (!e.currentTarget.contains(e.target as Node)) return;
          pressedRef.current = true;
          onOpenChange(false);
        }}
        onPointerLeave={() => {
          pressedRef.current = false;
        }}
      >
        {children}
      </PreviewCard.Trigger>
      <PreviewCard.Portal>
        <PreviewCard.Positioner
          side="bottom"
          align="start"
          sideOffset={-4}
          alignOffset={40}
          className="z-popover outline-none"
        >
          <PreviewCard.Popup
            className="fragment-preview-enter glass-dropdown backdrop-blur-sm w-[560px] max-w-[calc(100vw-32px)] px-3.5 py-3 outline-none"
            role="region"
            aria-label={t.memoryView.previewAria}
          >
            <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11.5px] text-fg-3">
              {attribution.hasAgent ? (
                <AgentInlineIdentity attribution={attribution} />
              ) : m.author_name ? (
                <span>{m.author_name}</span>
              ) : (
                <span>{t.attribution.you}</span>
              )}
              <span className="text-fg-4">·</span>
              <span>{formatAge(m.created_at, t, interpolate)}</span>
              {topicPath && topicPath.length > 0 ? (
                <>
                  <span className="text-fg-4">·</span>
                  <span className="truncate">{topicPath.join(" / ")}</span>
                </>
              ) : null}
            </div>
            {body ? (
              <p className="mt-1.5 line-clamp-5 whitespace-pre-line text-[13px] leading-relaxed text-fg-2">
                {body}
              </p>
            ) : (
              <p className="mt-1.5 text-[13px] text-fg-3">
                {t.memoryView.remembering}
              </p>
            )}
            <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                onClick={() => {
                  onOpenChange(false);
                  onOpen();
                }}
                className="rounded-full bg-foreground px-3 py-1 text-[12px] font-medium text-background transition-opacity cursor-pointer hover:opacity-85"
              >
                {t.memoryView.previewOpen}
              </button>
              <Popover open={moveOpen} onOpenChange={setMoveOpenHeld}>
                <PopoverTrigger className={actionClass}>
                  {t.topics.moveToTopic}
                </PopoverTrigger>
                <PopoverContent
                  side="bottom"
                  align="start"
                  sideOffset={6}
                  className="w-[300px] p-0"
                  onClick={(e) => e.stopPropagation()}
                >
                  <DestinationPicker
                    variant="plain"
                    selectedTopicId={m.topic_id}
                    onSelectTopic={(selectedTopicId, targetHubId, name) => {
                      setMoveOpenHeld(false);
                      void mover.moveWithUndo(
                        [{ id: m.id, hubId: m.hub_id, topicId: m.topic_id }],
                        { topicId: selectedTopicId, hubId: targetHubId },
                        mover.moveOneSuccess(name),
                      );
                    }}
                    onSelectHub={(targetHubId, name) => {
                      setMoveOpenHeld(false);
                      void mover.moveWithUndo(
                        [{ id: m.id, hubId: m.hub_id, topicId: m.topic_id }],
                        { hubId: targetHubId },
                        mover.moveOneSuccess(name),
                      );
                    }}
                    onClose={() => setMoveOpenHeld(false)}
                  />
                </PopoverContent>
              </Popover>
              {confirmingForget ? (
                // Armed: the SAFE target (留着) sits where 忘记 was, so a
                // double click lands on it; the destructive confirm is a
                // separate, clearly different button to its right.
                <>
                  <button
                    type="button"
                    autoFocus
                    onClick={() => setConfirmingForget(false)}
                    className={actionClass}
                  >
                    {t.memoryView.previewKeep}
                  </button>
                  <button
                    type="button"
                    disabled={forget.isPending}
                    onClick={() => {
                      setConfirmingForget(false);
                      onOpenChange(false);
                      void forget.forgetWithConfirm([m.id]);
                    }}
                    className={`${actionClass} border-destructive/50 bg-destructive/10 text-destructive hover:bg-destructive/15 hover:text-destructive disabled:cursor-wait disabled:opacity-60`}
                  >
                    {t.memoryView.previewForgetConfirm}
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  disabled={forget.isPending}
                  onClick={() => setConfirmingForget(true)}
                  className={`${actionClass} text-destructive/80 hover:text-destructive disabled:cursor-wait disabled:opacity-60`}
                >
                  {t.memoryView.previewForget}
                </button>
              )}
            </div>
          </PreviewCard.Popup>
        </PreviewCard.Positioner>
      </PreviewCard.Portal>
    </PreviewCard.Root>
  );
}
