"use client";

import { MemoryRow } from "./memory-row";
import { useDraggableMemory } from "../topic/topic-dnd-hooks";
import { useIsMobile } from "@/hooks/use-is-mobile";
import type { ComponentProps } from "react";

type MemoryRowProps = ComponentProps<typeof MemoryRow>;

type DraggableMemoryRowProps = Omit<
  MemoryRowProps,
  "dragRef" | "dragAttributes" | "dragListeners" | "isDragging"
>;

/**
 * DraggableMemoryRow — attaches drag-to-topic wiring around MemoryRow.
 *
 * Gesture ownership (Codex review):
 *   - Desktop → forward dragRef/attributes/listeners so MemoryRow renders
 *     a hover-revealed grip that activates drag-to-topic. Row click still
 *     navigates, long-press is a no-op (handled inside MemoryRow).
 *   - Mobile → call useDraggableMemory unconditionally (Rules of Hooks)
 *     but deliberately drop the drag props on the floor. MemoryRow treats
 *     the absence of dragRef as "not draggable" and skips rendering the
 *     grip. Mobile long-press continues to drive multi-select entry, and
 *     there is no drag-to-tree path on mobile (the tree is a bottom sheet;
 *     drag UX there is out of scope for this pass).
 */
export function DraggableMemoryRow(props: DraggableMemoryRowProps) {
  const { memory } = props;
  const isMobile = useIsMobile();
  const { attributes, listeners, setNodeRef, isDragging } = useDraggableMemory({
    id: memory.id,
    title: memory.title,
    hubId: memory.hub_id,
    topicId: memory.topic_id || undefined,
  });

  if (isMobile) {
    return <MemoryRow {...props} />;
  }

  return (
    <MemoryRow
      {...props}
      dragRef={setNodeRef}
      dragAttributes={attributes}
      dragListeners={listeners}
      isDragging={isDragging}
    />
  );
}
