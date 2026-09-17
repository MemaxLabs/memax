"use client";

import { useEffect, useState } from "react";
import type { Memory } from "memax-sdk";
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

/**
 * FragmentPreview — the thread-style quick look that unfolds UNDER a
 * 记忆片段 row after a short hover (2026-09-17 founder spec). It pushes
 * the rows below it down instead of floating over them, so a reader can
 * keep sliding down the list. Three actions, all existing capabilities:
 * open, move to topic (same DestinationPicker as the detail view), and
 * forget — two-step (忘记 → 确认忘记) because forget is irreversible.
 *
 * `onHold` tells the host to keep the preview mounted while the move
 * picker is open (the pointer leaves the row to reach the popover).
 */
export function FragmentPreview({
  memory: m,
  topicPath,
  onOpen,
  onHold,
}: {
  memory: Memory;
  topicPath?: string[];
  onOpen: () => void;
  onHold: (held: boolean) => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { user } = useAuth();
  const forget = useMemoryForget();
  const mover = useMemoryMove();
  const [moveOpen, setMoveOpen] = useState(false);
  const [confirmingForget, setConfirmingForget] = useState(false);

  useEffect(() => {
    onHold(moveOpen);
  }, [moveOpen, onHold]);

  // The confirm arm disarms itself after a moment so a stray second
  // click a minute later doesn't delete anything.
  useEffect(() => {
    if (!confirmingForget) return;
    const timer = setTimeout(() => setConfirmingForget(false), 4000);
    return () => clearTimeout(timer);
  }, [confirmingForget]);

  const attribution = resolveMemoryAttribution(m, user?.id);
  const body = (m.content || sanitizeSummary(m.summary) || "").trim();
  const actionClass =
    "rounded-full border border-border/50 px-3 py-1 text-[12px] text-fg-2 transition-colors cursor-pointer hover:bg-surface-2 hover:text-fg-1";

  return (
    <div
      className="fragment-preview-enter mx-3 mb-3 max-w-[560px] rounded-xl border border-border/50 bg-card px-3.5 py-3 shadow-glow sm:ml-12"
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
          onClick={onOpen}
          className="rounded-full bg-foreground px-3 py-1 text-[12px] font-medium text-background transition-opacity cursor-pointer hover:opacity-85"
        >
          {t.memoryView.previewOpen}
        </button>
        <Popover open={moveOpen} onOpenChange={setMoveOpen}>
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
                setMoveOpen(false);
                void mover.moveWithUndo(
                  [{ id: m.id, hubId: m.hub_id, topicId: m.topic_id }],
                  { topicId: selectedTopicId, hubId: targetHubId },
                  mover.moveOneSuccess(name),
                );
              }}
              onSelectHub={(targetHubId, name) => {
                setMoveOpen(false);
                void mover.moveWithUndo(
                  [{ id: m.id, hubId: m.hub_id, topicId: m.topic_id }],
                  { hubId: targetHubId },
                  mover.moveOneSuccess(name),
                );
              }}
              onClose={() => setMoveOpen(false)}
            />
          </PopoverContent>
        </Popover>
        <button
          type="button"
          disabled={forget.isPending}
          onClick={() => {
            if (!confirmingForget) {
              setConfirmingForget(true);
              return;
            }
            setConfirmingForget(false);
            void forget.forgetWithConfirm([m.id]);
          }}
          className={`${actionClass} ${
            confirmingForget
              ? "border-destructive/50 bg-destructive/10 text-destructive hover:bg-destructive/15 hover:text-destructive"
              : "text-destructive/80 hover:text-destructive"
          } disabled:cursor-wait disabled:opacity-60`}
        >
          {confirmingForget
            ? t.memoryView.previewForgetConfirm
            : t.memoryView.previewForget}
        </button>
      </div>
    </div>
  );
}
