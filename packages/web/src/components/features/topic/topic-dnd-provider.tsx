"use client";

import { useState, useCallback, useMemo, type ReactNode } from "react";
import { createPortal } from "react-dom";
import {
  DndContext,
  DragOverlay,
  MeasuringStrategy,
  useSensor,
  useSensors,
  PointerSensor,
  type DragCancelEvent,
  type DragStartEvent,
  type DragEndEvent,
} from "@dnd-kit/core";
import { useMemoryMove } from "@/hooks/use-memory-move";
import { useTopicMove, type TopicMoveSnapshot } from "@/hooks/use-topic-move";
import { useTopics } from "@/hooks/use-topics";
import { useAuth } from "@/lib/auth";
import { useLocale, useInterpolate } from "@/i18n";
import {
  TopicDragContext,
  HUB_ROOT_DROP_ID,
  collectTopicDescendantIds,
  type TopicDragContextValue,
} from "./topic-dnd-hooks";
import { TopicIcon } from "./topic-icon";
import { useTreePanel } from "./topic-tree-panel";

/**
 * TopicDndProvider — wraps pages that support memory → topic drag-and-drop
 * AND topic → topic (reparent) drag-and-drop within the current hub.
 *
 * Two drag kinds ride on a single shared DndContext, discriminated by
 * `active.data.current.type`:
 *
 *   - "memory": memory dropped onto a topic node.
 *       → route through memories.batchMove (same contract as picker/batch/
 *         detail-route). topics.assignMemory is reserved for ingest + dreams.
 *   - "topic": topic dropped onto another topic node OR the root drop row.
 *       → route through topics.update(parent_id, position) via useTopicMove.
 *         The reparent path is hub-scoped and validated server-side
 *         (invalid_parent / cycle_detected / max_depth_subtree codes).
 *
 * The provider wraps BOTH the pinned sidebar and the main content in the
 * (app) layout, so memory drags from the /memories grid need HORIZONTAL
 * travel across the layout boundary into the sidebar tree. This means:
 *
 *   - PointerSensor only — no KeyboardSensor in this pass; the ⋮ menu picker
 *     in TopicTreeNode is the kbd/touch/mobile accessibility path.
 *   - NO axis-restricting modifiers (restrictToVerticalAxis would break
 *     memory→tree drags).
 *   - NO global auto-scroll modifier (would apply to the main content pane
 *     too). If tree-local auto-scroll is needed later, scope it to the tree
 *     surface via a wrapping scroll container.
 */
export function TopicDndProvider({ children }: { children: ReactNode }) {
  const [activeId, setActiveId] = useState<string | null>(null);
  const [activeType, setActiveType] = useState<"memory" | "topic" | null>(null);
  const [activeTitle, setActiveTitle] = useState<string>("");
  const [activeIcon, setActiveIcon] = useState<string | undefined>(undefined);
  const [activeCount, setActiveCount] = useState<number | undefined>(undefined);
  const [invalidDescendantIds, setInvalidDescendantIds] = useState<
    ReadonlySet<string>
  >(() => new Set());

  const mover = useMemoryMove();
  const topicMover = useTopicMove();
  const topicsQuery = useTopics();
  const { t } = useLocale();
  const interpolate = useInterpolate();
  // Tree-panel drag-session affordance: auto-open the tree sidebar for the
  // duration of any memory/topic drag so drop targets are reachable without
  // the user first clicking the rail toggle. Lifecycle is strictly scoped
  // to begin/end here; the flag never writes to the localStorage-backed
  // open preference (isPinned).
  //
  // `isPinned || isDragSessionOpen` is also the authoritative gate for
  // tree-node droppables (`dropTargetsActive`), exposed via TopicDragContext
  // and read by `useDroppableTopic` / the hub-root droppable. The tree is
  // always mounted in SidebarSlot so droppables stay registered with dnd-kit
  // from page load; the gate excludes them from collision detection when
  // the slot is collapsed, preventing stale 280px bounding boxes from
  // matching drops in the main content area.
  const { isPinned, isDragSessionOpen, beginDragSession, endDragSession } =
    useTreePanel();
  const dropTargetsActive = isPinned || isDragSessionOpen;

  // Resolve the current hub's display name. The hub-root drop row labels
  // itself with this, and success toasts use it as the destination name
  // for both topic ("Moved {name} to {hub}.") and memory ("Moved to
  // {hub}.") root drops. May be an empty string on the first render
  // before auth hydrates — downstream callers fall back to the legacy
  // generic success helpers in that case (Codex review).
  const { activeHubId, hubs } = useAuth();
  const currentHubName = useMemo(() => {
    if (!activeHubId) return "";
    return hubs.find((entry) => entry.hub.id === activeHubId)?.hub.name ?? "";
  }, [activeHubId, hubs]);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 8 }, // 8px threshold prevents accidental drags
    }),
  );

  const handleDragStart = useCallback(
    (event: DragStartEvent) => {
      const payload = event.active.data.current as
        | {
            type?: "memory" | "topic";
            title?: string;
            name?: string;
            icon?: string;
            count?: number;
          }
        | undefined;
      const type = payload?.type ?? null;
      setActiveId(String(event.active.id));
      setActiveType(type);
      setActiveTitle(String(payload?.title ?? payload?.name ?? "Memory"));
      setActiveIcon(payload?.icon);
      setActiveCount(payload?.count);

      // Auto-open the tree sidebar for the lifetime of this drag so drop
      // targets are reachable without the user first clicking the rail
      // toggle. Runs for both memory and topic drags so the affordance is
      // uniform. If `isPinned` is already true the flag is additive and
      // has no visible effect — SidebarSlot's width stays at 280 and the
      // sidebar remains visible across the drag lifecycle.
      beginDragSession();

      // For topic drags, walk the current topic tree and stash the set of
      // descendant ids (including self). Each TopicTreeNode reads this via
      // TopicDragContext to render an invalid-drop ring instead of the
      // default isOver ring. O(n) once per drag-start, not per render.
      if (type === "topic") {
        const forest = topicsQuery.data?.topics ?? [];
        const invalid = collectTopicDescendantIds(
          forest,
          String(event.active.id),
        );
        setInvalidDescendantIds(invalid);
      } else {
        setInvalidDescendantIds(new Set());
      }
    },
    [beginDragSession, topicsQuery.data?.topics],
  );

  const handleMemoryDrop = useCallback(
    (
      activeId: string | number,
      overId: string | number,
      payload:
        | {
            hubId?: string;
            topicId?: string;
          }
        | undefined,
      overData: Record<string, unknown> | undefined,
    ) => {
      const memoryId = String(activeId);
      const targetTopicId = String(overId);
      if (!memoryId || !targetTopicId) return;

      const sourceHubId = payload?.hubId;
      if (!sourceHubId) return;
      const sourceTopicId = payload?.topicId;

      // Drop on the hub-root row → clear the memory's topic assignment
      // within the same hub. No-op if the memory is already unassigned.
      // Success copy reuses moveOneSuccess(hubName) → "Moved to {hub}.";
      // if the hub name hasn't resolved yet, fall back to the generic
      // "Moved." string (Codex review) rather than emitting an empty
      // destination.
      if (targetTopicId === HUB_ROOT_DROP_ID) {
        if (!sourceTopicId) return;
        void mover.moveWithUndo(
          [{ id: memoryId, hubId: sourceHubId, topicId: sourceTopicId }],
          { hubId: sourceHubId, topicId: undefined },
          mover.moveOneSuccess(currentHubName || undefined),
        );
        return;
      }

      if (sourceTopicId === targetTopicId) return;

      const topicName = overData?.topicName as string | undefined;

      void mover.moveWithUndo(
        [{ id: memoryId, hubId: sourceHubId, topicId: sourceTopicId }],
        { hubId: sourceHubId, topicId: targetTopicId },
        mover.moveOneSuccess(topicName),
      );
    },
    [currentHubName, mover],
  );

  const handleTopicDrop = useCallback(
    (
      activeId: string | number,
      overId: string | number,
      payload:
        | {
            name?: string;
            hubId?: string;
            parentId?: string | null;
            position?: number;
          }
        | undefined,
      overData: Record<string, unknown> | undefined,
    ) => {
      const topicId = String(activeId);
      const dropId = String(overId);
      if (!topicId) return;

      const hubId = payload?.hubId;
      if (!hubId) return;

      // Root drop sentinel — reparent to null (topic becomes a root-level
      // entry in the current hub's tree).
      const targetParentId: string | null =
        dropId === HUB_ROOT_DROP_ID ? null : dropId;

      // No-op guard: dropped on own current parent.
      if (
        payload?.parentId !== undefined &&
        payload.parentId === targetParentId
      ) {
        return;
      }

      // Self / descendant guard: reject client-side before a server roundtrip.
      // The invalidDescendantIds Set was computed on drag-start from the same
      // tree the user is looking at, so this is the authoritative client view.
      if (
        targetParentId !== null &&
        (invalidDescendantIds.has(targetParentId) || targetParentId === topicId)
      ) {
        return;
      }

      const snapshot: TopicMoveSnapshot = {
        id: topicId,
        hubId,
        parentId: payload.parentId ?? null,
        position: payload.position ?? 0,
        name: payload.name ?? "",
      };

      // Success copy:
      //   - root drop with a resolved hub name → "Moved {name} to {hub}."
      //   - root drop with an unresolved hub name → neutral fallback
      //     "Moved {name}." (Codex review: fall back to a generic helper
      //     rather than emitting an empty destination)
      //   - drop under a parent topic → "Moved {name} under {parent}."
      const successMessage =
        targetParentId === null
          ? currentHubName
            ? interpolate(t.topics.topicMovedToHub, {
                name: snapshot.name,
                hub: currentHubName,
              })
            : interpolate(t.topics.topicMoved, { name: snapshot.name })
          : interpolate(t.topics.topicMovedUnderParent, {
              name: snapshot.name,
              parent: (overData?.topicName as string | undefined) ?? "",
            });

      void topicMover.moveTopicWithUndo(
        snapshot,
        { parentId: targetParentId },
        successMessage,
      );
    },
    [
      currentHubName,
      interpolate,
      invalidDescendantIds,
      t.topics.topicMoved,
      t.topics.topicMovedToHub,
      t.topics.topicMovedUnderParent,
      topicMover,
    ],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      setActiveId(null);
      setActiveType(null);
      setActiveTitle("");
      setActiveIcon(undefined);
      setActiveCount(undefined);
      setInvalidDescendantIds(new Set());
      // Always close the drag-session auto-open, regardless of whether the
      // drop landed. onDragEnd fires for completed drops; onDragCancel
      // fires for Escape-cancels. Both routes clear the flag. If the tree
      // was already user-open (`isPinned`), the flag was additive and
      // SidebarSlot stays expanded; if it wasn't, the sidebar slides back
      // closed. Restoration is automatic because the flag never mutated
      // the persisted preference.
      endDragSession();

      if (!over) return;

      const payload = active.data.current as
        | {
            type?: "memory" | "topic";
            title?: string;
            name?: string;
            hubId?: string;
            topicId?: string;
            parentId?: string | null;
            position?: number;
          }
        | undefined;
      const type = payload?.type;

      if (type === "topic") {
        if (topicMover.isPending) return; // single-flight guard
        handleTopicDrop(active.id, over.id, payload, over.data.current);
        return;
      }

      // Default: memory drop (including legacy calls where type is absent).
      if (mover.isPending) return;
      handleMemoryDrop(active.id, over.id, payload, over.data.current);
    },
    [endDragSession, handleMemoryDrop, handleTopicDrop, mover, topicMover],
  );

  // Escape-to-cancel lands here in dnd-kit v6 (onDragCancel fires alongside
  // or instead of onDragEnd depending on the activation path). Duplicating
  // the cleanup is cheap and guarantees the drag-session flag can never be
  // stranded open after a cancel.
  const handleDragCancel = useCallback(
    (_event: DragCancelEvent) => {
      setActiveId(null);
      setActiveType(null);
      setActiveTitle("");
      setActiveIcon(undefined);
      setActiveCount(undefined);
      setInvalidDescendantIds(new Set());
      endDragSession();
    },
    [endDragSession],
  );

  const contextValue = useMemo<TopicDragContextValue>(
    () => ({
      activeId,
      activeType,
      invalidDescendantIds,
      dropTargetsActive,
    }),
    [activeId, activeType, invalidDescendantIds, dropTargetsActive],
  );

  // Drag preview content. Branches on active drag kind. Rendered inside
  // <DragOverlay> below — which dnd-kit positions via `position: fixed`
  // and `transform: translate3d(...)` to track the cursor.
  const overlayPreview =
    activeId !== null ? (
      activeType === "topic" ? (
        <div
          className="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[13px] font-medium text-fg-1 shadow-lg"
          style={{
            background: "var(--card)",
            border: "1px solid var(--border)",
          }}
        >
          <TopicIcon
            name={activeIcon ?? "folder"}
            className="h-3.5 w-3.5 shrink-0 text-fg-3"
          />
          <span className="truncate max-w-40">{activeTitle}</span>
          {typeof activeCount === "number" && (
            <span className="tabular-nums text-fg-4 text-[12px]">
              {activeCount}
            </span>
          )}
        </div>
      ) : (
        <div
          className="px-3 py-2 rounded-lg text-[14px] font-medium text-fg-1 max-w-50 truncate shadow-lg"
          style={{
            background: "var(--card)",
            border: "1px solid var(--border)",
          }}
        >
          {activeTitle}
        </div>
      )
    ) : null;

  // Portal DragOverlay to <body> so its `position: fixed` containing
  // block is the viewport — never an ancestor with a transform,
  // backdrop-filter, filter, perspective, or `will-change` of any of
  // those (CSS spec §11.1.2 "Fixed positioning containing block").
  // PinnedSecondaryPanel slides via framer-motion `x` (always-on
  // translate3d, even at rest), and several shell ancestors use
  // `backdrop-filter` for glass — any of these would silently shift
  // the overlay off-cursor without this portal. React context follows
  // the React tree, not the DOM, so DragOverlay still consumes
  // DndContext through the portal boundary.
  const overlayPortal =
    typeof document !== "undefined"
      ? createPortal(
          <DragOverlay dropAnimation={null}>{overlayPreview}</DragOverlay>,
          document.body,
        )
      : null;

  return (
    <TopicDragContext.Provider value={contextValue}>
      <DndContext
        sensors={sensors}
        // Measure droppables on every frame instead of only at drag start.
        // Load-bearing: the topic-tree peek overlay mounts AFTER the drag
        // begins (auto-open-for-drag-session UX), so the default
        // `WhileDragging` strategy — which snapshots droppables once at
        // drag-start — never measures the overlay's tree nodes, and
        // collision detection silently skips them. `Always` has a small
        // per-frame cost but the tree is ~20-50 nodes so it's negligible.
        measuring={{ droppable: { strategy: MeasuringStrategy.Always } }}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
        onDragCancel={handleDragCancel}
      >
        {children}
        {overlayPortal}
      </DndContext>
    </TopicDragContext.Provider>
  );
}
