"use client";

import { MemoryCardLayout } from "./memory-card-layout";
import { useDraggableMemory } from "../topic/topic-dnd-hooks";
import { useIsMobile } from "@/hooks/use-is-mobile";
import type { ComponentProps } from "react";

type MemoryCardLayoutProps = ComponentProps<typeof MemoryCardLayout>;

type DraggableMemoryCardProps = Omit<
  MemoryCardLayoutProps,
  "dragRef" | "dragAttributes" | "dragListeners" | "isDragging"
>;

/**
 * DraggableMemoryCard — attaches drag-to-topic wiring around
 * MemoryCardLayout. Mirror of `<DraggableMemoryRow>` for the card grid.
 *
 * Gesture ownership:
 *   - Desktop → forward dragRef/attributes/listeners onto the card's
 *     `<article>`. `topic-dnd-provider` configures
 *     `PointerSensor.activationConstraint: { distance: 8 }`, so a click
 *     without 8px of motion stays a click (navigates to detail) and
 *     past-threshold motion starts the drag. `isDragging` dims the
 *     source card to 40% opacity while the DragOverlay renders the
 *     preview.
 *   - Mobile → call `useDraggableMemory` unconditionally (Rules of
 *     Hooks) but drop the drag props on the floor. Card behavior on
 *     mobile is unchanged. Drag-to-tree on mobile is out of scope
 *     (the topic tree is a bottom sheet, not a drop target).
 */
export function DraggableMemoryCard(props: DraggableMemoryCardProps) {
  const { memory } = props;
  const isMobile = useIsMobile();
  const { attributes, listeners, setNodeRef, isDragging } = useDraggableMemory({
    id: memory.id,
    title: memory.title,
    hubId: memory.hub_id,
    topicId: memory.topic_id || undefined,
  });

  if (isMobile) {
    return <MemoryCardLayout {...props} />;
  }

  return (
    <MemoryCardLayout
      {...props}
      dragRef={setNodeRef}
      dragAttributes={attributes}
      dragListeners={listeners}
      isDragging={isDragging}
    />
  );
}
