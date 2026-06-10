"use client";

import { useState, useMemo, useCallback, useEffect } from "react";
import { ChevronDown, Loader2, Plus, X } from "lucide-react";
import {
  pillClass,
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useTopics, type TopicTree } from "@/hooks/use-topics";
import { useMemoryMove } from "@/hooks/use-memory-move";
import { TopicIcon } from "./topic-icon";
import { DestinationPicker } from "../destination-picker";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";

function findTopic(
  trees: TopicTree[],
  id: string,
): { name: string; icon: string } | null {
  for (const t of trees) {
    if (t.id === id) return { name: t.name, icon: t.icon };
    const found = findTopic(t.children, id);
    if (found) return found;
  }
  return null;
}

interface TopicLocationProps {
  memoryId: string;
  hubId: string;
  topicId?: string;
  /** Pill size — defaults to "sm" so the chip baseline-aligns with text. */
  size?: "sm" | "md" | "lg";
}

export function TopicLocation({
  memoryId,
  hubId,
  topicId,
  size = "sm",
}: TopicLocationProps) {
  const { t } = useLocale();
  const { data: topicsData } = useTopics(hubId);
  const mover = useMemoryMove();
  const [open, setOpen] = useState(false);

  const currentTopic = useMemo(() => {
    if (!topicId || !topicsData?.topics) return null;
    return findTopic(topicsData.topics, topicId);
  }, [topicId, topicsData]);

  useEffect(() => {
    if (!open) return;
    return acquireBodyScrollLock();
  }, [open]);

  // The picker is a choice surface, not a confirm-success surface. Dismissing
  // before the mutation runs keeps the UI responsive — optimistic cache
  // patches + in-place pending pill cover the "did it work" signal, and any
  // failure surfaces through the hook's error toast.
  const handleSelect = useCallback(
    (
      selectedTopicId: string,
      targetHubId?: string,
      destinationName?: string,
    ) => {
      setOpen(false);
      void mover.moveWithUndo(
        [{ id: memoryId, hubId, topicId }],
        { topicId: selectedTopicId, hubId: targetHubId },
        mover.moveOneSuccess(destinationName),
      );
    },
    [mover, memoryId, hubId, topicId],
  );

  // Clear-topic is semantically distinct from "Moved to X" — there is no
  // destination. Fire the dedicated "Topic cleared." string via moveOneCleared.
  const handleRemove = useCallback(() => {
    if (!topicId) return;
    void mover.moveWithUndo(
      [{ id: memoryId, hubId, topicId }],
      { hubId },
      mover.moveOneCleared(),
    );
  }, [mover, topicId, memoryId, hubId]);

  const handleMoveToHub = useCallback(
    (targetHubId: string, destinationName?: string) => {
      setOpen(false);
      void mover.moveWithUndo(
        [{ id: memoryId, hubId, topicId }],
        { hubId: targetHubId },
        mover.moveOneSuccess(destinationName),
      );
    },
    [mover, memoryId, hubId, topicId],
  );

  return (
    <div className="flex items-center gap-1">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          disabled={mover.isPending}
          className={
            currentTopic
              ? pillClass({
                  variant: "select",
                  size,
                  className: "max-w-[220px] gap-1.5",
                })
              : pillClass({ variant: "add", size })
          }
        >
          {mover.isPending ? (
            <>
              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-fg-3" />
              <span className="truncate max-w-[160px]">{t.topics.moving}</span>
            </>
          ) : currentTopic ? (
            <>
              <TopicIcon
                name={currentTopic.icon}
                className="h-3.5 w-3.5 shrink-0 text-fg-3"
              />
              <span className="truncate max-w-[160px]">
                {currentTopic.name}
              </span>
              <ChevronDown className="h-3 w-3 text-fg-4 shrink-0" />
            </>
          ) : (
            <>
              <Plus className="h-3.5 w-3.5 shrink-0" />
              <span>{t.topics.moveToTopic}</span>
            </>
          )}
        </PopoverTrigger>

        <PopoverContent
          side="bottom"
          align="start"
          sideOffset={4}
          className="min-w-[220px] max-h-[320px] overflow-hidden"
        >
          {/* PopoverContent now always paints the glass-dropdown chrome
              (border + bg + shadow). DestinationPicker's default
              variant="card" would stack a SECOND border/shadow inside
              the glass, which reads as nested boxes. The batch toolbar
              path doesn't hit this because it renders the picker in a
              bare absolute wrapper that owns no chrome of its own —
              keep those two paths in sync by always letting the outer
              container own the material.

              Explicit listHeight={268} matches the card variant's
              default select-mode cap (320px outer - 52px hint row =
              268px list), so switching to plain doesn't drop the
              height constraint and let long topic trees grow
              unbounded (codex High on the prior commit). The
              PopoverContent's own max-h-[320px] + overflow-hidden is
              the outer safety cap. */}
          <DestinationPicker
            variant="plain"
            listHeight={268}
            onSelectTopic={handleSelect}
            onSelectHub={handleMoveToHub}
            onClose={() => setOpen(false)}
            selectedTopicId={topicId}
          />
        </PopoverContent>
      </Popover>

      {currentTopic && (
        <button
          onClick={handleRemove}
          disabled={mover.isPending}
          // Mobile keeps the full 44px touch target; desktop tightens to
          // match the baseline chip height so the row doesn't bloat.
          className="flex h-5 w-5 sm:h-5 sm:w-5 min-h-5 items-center justify-center text-fg-4 hover:text-fg-2 cursor-pointer transition-colors rounded-md"
          aria-label={t.topics.clearTopic}
        >
          <X className="h-3 w-3" />
        </button>
      )}
    </div>
  );
}

export { TopicLocation as TopicPills };
