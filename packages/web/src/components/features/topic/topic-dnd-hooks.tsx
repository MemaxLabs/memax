"use client";

import { createContext, useContext } from "react";
import { useDraggable, useDroppable } from "@dnd-kit/core";
import type { TopicTree } from "memax-sdk";

export interface DraggableMemoryData {
  id: string;
  title: string;
  hubId: string;
  topicId?: string;
}

/**
 * Hook for making a memory row draggable.
 * The payload carries hubId + topicId so the drop handler can route the move
 * through the unified memories.batchMove contract (same path as picker/batch).
 */
export function useDraggableMemory(memory: DraggableMemoryData) {
  return useDraggable({
    id: memory.id,
    data: {
      type: "memory",
      title: memory.title,
      hubId: memory.hubId,
      topicId: memory.topicId,
    },
  });
}

export interface DraggableTopicData {
  id: string;
  name: string;
  hubId: string;
  parentId: string | null;
  position: number;
  icon?: string;
  count?: number;
}

/**
 * Hook for making a topic tree node draggable (drag-to-reparent).
 * The payload carries every field useTopicMove needs to build a snapshot,
 * so the drop handler can call moveTopicWithUndo directly without looking
 * anything up. Type="topic" discriminates from memory drags at onDragEnd.
 */
export function useDraggableTopic(topic: DraggableTopicData) {
  return useDraggable({
    id: topic.id,
    data: {
      type: "topic",
      name: topic.name,
      hubId: topic.hubId,
      parentId: topic.parentId,
      position: topic.position,
      icon: topic.icon,
      count: topic.count,
    },
  });
}

/**
 * Hook for making a topic card/tree node a drop target.
 *
 * Gating: reads `dropTargetsActive` from TopicDragContext and passes it to
 * dnd-kit's `disabled` flag. The provider sets this TRUE when the tree is
 * actually usable as a drag surface (`isPinned || isDragSessionOpen`) and
 * FALSE otherwise. The tree is always mounted in SidebarSlot so droppables
 * stay registered from page load — the gate just excludes them from
 * collision detection when the slot is collapsed (width 0, clipped by
 * overflow-hidden), preventing stale 280px bounding boxes from matching
 * drops on the main content area. Peek-without-drag intentionally does
 * NOT enable drop targets (peek is a browse affordance, not a drag one).
 *
 * Usage: const { setNodeRef, isOver } = useDroppableTopic(topicId, topicName);
 *        <div ref={setNodeRef} className={isOver ? "ring-2 ring-blue-500/30" : ""}> ... </div>
 */
export function useDroppableTopic(topicId: string, topicName: string) {
  const { dropTargetsActive } = useContext(TopicDragContext);
  return useDroppable({
    id: topicId,
    data: { topicName, type: "topic" },
    disabled: !dropTargetsActive,
  });
}

/**
 * TopicDragContext carries active-drag metadata from TopicDndProvider down
 * to every TopicTreeNode so nodes can render invalid-drop feedback (e.g. a
 * destructive ring on descendants of the moving topic) without each node
 * walking the tree on every render. The descendant set is computed once on
 * drag-start in the provider, cleared on drag-end.
 */
export interface TopicDragContextValue {
  activeId: string | null;
  activeType: "memory" | "topic" | null;
  /**
   * IDs that CANNOT accept the current drag as a drop target. For topic
   * drags this is the moving topic itself plus all of its descendants
   * (dropping would create a cycle). For memory drags it is empty.
   */
  invalidDescendantIds: ReadonlySet<string>;
  /**
   * Whether tree-node droppables should participate in collision detection.
   * TRUE when `isPinned || isDragSessionOpen` on the TreePanelContext — i.e.
   * the pre-mounted SidebarSlot tree is actually visible and usable as a
   * drag surface. FALSE otherwise, so collapsed-but-mounted tree nodes
   * don't pollute collision detection with stale 280px bounding boxes that
   * would overlap the main content area. Read by `useDroppableTopic` and
   * the hub-root droppable in `TopicTreeContent`.
   */
  dropTargetsActive: boolean;
}

const EMPTY_DRAG_CONTEXT: TopicDragContextValue = {
  activeId: null,
  activeType: null,
  invalidDescendantIds: new Set(),
  dropTargetsActive: false,
};

export const TopicDragContext =
  createContext<TopicDragContextValue>(EMPTY_DRAG_CONTEXT);

export function useTopicDragContext(): TopicDragContextValue {
  return useContext(TopicDragContext);
}

/**
 * Droppable id for the "hub root" row rendered at the top of the tree
 * during an active drag (topic OR memory). The row represents the current
 * hub as a destination: dropping a topic on it reparents to `parent_id:
 * null`, and dropping a memory on it clears the memory's `topic_id`
 * (keeping the same hub). The TopicDndProvider discriminates by
 * `active.data.current.type` and dispatches to the right mover.
 *
 * The internal string value is unchanged from the original phase-5 name
 * to avoid a wider test-fixture rename — it's a sentinel, not a
 * user-visible id, so the symbol rename is sufficient.
 */
export const HUB_ROOT_DROP_ID = "__topic_root__";

/**
 * collectTopicDescendantIds walks the topic forest under rootId and returns
 * the set of every descendant id INCLUDING rootId itself. Used by
 * TopicDndProvider on drag-start to mark invalid drop targets for a topic
 * drag (dropping onto self or any descendant would create a cycle).
 *
 * Pure helper, exported for unit tests — the provider is a React component
 * and unit-testing the whole DnD lifecycle is heavy; this function is
 * where the correctness lives.
 */
export function collectTopicDescendantIds(
  forest: readonly TopicTree[],
  rootId: string,
): Set<string> {
  const ids = new Set<string>();
  const root = findTopicInForest(forest, rootId);
  if (!root) return ids;
  const visit = (node: TopicTree) => {
    ids.add(node.id);
    for (const child of node.children) visit(child);
  };
  visit(root);
  return ids;
}

export function findTopicInForest(
  forest: readonly TopicTree[],
  id: string,
): TopicTree | null {
  for (const node of forest) {
    if (node.id === id) return node;
    const deeper = findTopicInForest(node.children, id);
    if (deeper) return deeper;
  }
  return null;
}
