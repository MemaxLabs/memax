"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronRight, GripVertical, MoreVertical, Plus } from "lucide-react";
import { useRouter, usePathname } from "next/navigation";
import { TopicIcon } from "./topic-icon";
import { buildTopicPath, getHubSlugForPath } from "@/lib/route-helpers";
import {
  collectTopicDescendantIds,
  useDraggableTopic,
  useDroppableTopic,
  useTopicDragContext,
} from "./topic-dnd-hooks";
import { TopicMovePicker } from "./topic-move-picker";
import { Popover, PopoverTrigger, PopoverContent } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useAuth } from "@/lib/auth";
import { useTopics } from "@/hooks/use-topics";
import { useTopicMove } from "@/hooks/use-topic-move";
import type { TopicTree } from "@/hooks/use-topics";

/**
 * Indentation math — capped at depth 4 to prevent layout breakage.
 * Desktop: 16px/level, Mobile: 12px/level (narrower viewport).
 * Depths 5+ flatten to the depth-4 indent (still expandable, just not deeper visually).
 * isMobile passed as prop from tree content (computed once, not per-node).
 *
 * INDENT_BASE is 4 (not 8) so depth-0 topic rows sit almost flush with the
 * hub row above them. The grip is absolute-positioned out of flex flow
 * (see the `span` below), so the ~22px that used to be reserved for an
 * opacity-0 hover-only handle no longer pushes content right at depth 0.
 */
const MAX_INDENT_DEPTH = 4;
const INDENT_BASE = 4;
const INDENT_STEP_DESKTOP = 16;
const INDENT_STEP_MOBILE = 12;

function getIndent(depth: number, isMobile: boolean): number {
  const capped = Math.min(depth, MAX_INDENT_DEPTH);
  const step = isMobile ? INDENT_STEP_MOBILE : INDENT_STEP_DESKTOP;
  return INDENT_BASE + capped * step;
}

interface TreeNodeProps {
  topic: TopicTree;
  depth: number;
  isMobile: boolean;
  expandedIds: Set<string>;
  onToggleExpand: (id: string) => void;
  /** Spring-load expansion — transient, reverts when the drag ends. */
  onSpringExpand: (id: string) => void;
  activeTopic?: string;
  onCreateSubtopic?: (parentId: string) => void;
}

/**
 * Hold before the cue starts. Hold + cue = 600ms total from hover to
 * open, the Finder-class spring delay — the earlier 600ms hold put the
 * actual open at 860ms, which read as "is this going to open or not".
 */
const AUTO_EXPAND_MS = 340;
/** Two 130ms blinks, then the branch opens. Matches animate-spring-flash. */
const SPRING_FLASH_MS = 260;

export function TopicTreeNode({
  topic,
  depth,
  isMobile,
  expandedIds,
  onToggleExpand,
  onSpringExpand,
  activeTopic,
  onCreateSubtopic,
}: TreeNodeProps) {
  const router = useRouter();
  const pathname = usePathname();
  const currentHubSlug = getHubSlugForPath(pathname);
  const { t } = useLocale();
  const { activeHubId, hubs } = useAuth();
  const topicsQuery = useTopics();
  const topicMover = useTopicMove();

  // Current hub name — used as the "root destination" label/token for
  // the ⋮ picker and the success toast when the user moves a topic to
  // the top level of the tree (which IS the hub). Falls back to the
  // neutral topicMoved helper string when unresolved (auth not yet
  // hydrated).
  const currentHubName = useMemo(() => {
    if (!activeHubId) return "";
    return hubs.find((entry) => entry.hub.id === activeHubId)?.hub.name ?? "";
  }, [activeHubId, hubs]);

  const { setNodeRef: setDroppableRef, isOver } = useDroppableTopic(
    topic.id,
    topic.name,
  );
  const {
    setNodeRef: setDraggableRef,
    attributes: dragAttributes,
    listeners: dragListeners,
    isDragging,
  } = useDraggableTopic({
    id: topic.id,
    name: topic.name,
    hubId: topic.hub_id ?? activeHubId ?? "",
    parentId: topic.parent_id ?? null,
    position: topic.position,
    icon: topic.icon,
    count: topic.total_memory_count,
  });

  // Compose droppable + draggable refs on the same element. dnd-kit's
  // useDraggable/useDroppable each install their own ref callback, so we
  // forward the DOM node to both.
  const setNodeRef = (node: HTMLDivElement | null) => {
    setDroppableRef(node);
    setDraggableRef(node);
  };

  const hasChildren = topic.children.length > 0;
  const isExpanded = expandedIds.has(topic.id);
  const isActive = activeTopic === topic.id;
  const [hovered, setHovered] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  // Keep the action cluster mounted while the move popover is open —
  // otherwise the cursor leaving the row (to reach the popover) flips
  // `hovered` false, unmounts the trigger, and either closes the popover
  // or breaks its anchor (Codex review).
  const actionsVisible = hovered || menuOpen;

  const dragContext = useTopicDragContext();
  const isInvalidDropTarget =
    dragContext.activeType === "topic" &&
    dragContext.activeId !== null &&
    dragContext.activeId !== topic.id &&
    dragContext.invalidDescendantIds.has(topic.id);
  const isValidOver =
    isOver && !isInvalidDropTarget && dragContext.activeId !== topic.id;

  // Spring-loaded expand on hover-over-collapsed, for EITHER kind of
  // drag. 600ms total (hold + cue) matches Finder / Linear / Notion —
  // long enough to avoid accidental expansions while the user passes
  // over a collapsed folder, short enough to feel responsive when the
  // intent is "open this branch so I can drop inside". The expansion
  // goes through onSpringExpand — a transient overlay that reverts on
  // drag end — never through onToggleExpand, which would permanently
  // rewrite the user's persisted sidebar as a side effect of passing
  // a drag over it.
  //
  // It used to fire only while dragging a topic, which left the far
  // more common case — dragging a memory into a nested topic — with no
  // way to reach a collapsed branch at all.
  //
  // The hold ends with two quick flashes before the branch opens, the
  // Finder cue: it confirms the hold registered and marks WHICH row is
  // about to move, so the expansion doesn't read as the tree shifting
  // underfoot mid-drag.
  const [springFlashing, setSpringFlashing] = useState(false);
  const autoExpandTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const springOpenTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    const dragging =
      dragContext.activeType === "topic" || dragContext.activeType === "memory";
    if (
      isOver &&
      dragging &&
      hasChildren &&
      !isExpanded &&
      !isInvalidDropTarget
    ) {
      autoExpandTimerRef.current = setTimeout(() => {
        setSpringFlashing(true);
        springOpenTimerRef.current = setTimeout(() => {
          setSpringFlashing(false);
          onSpringExpand(topic.id);
        }, SPRING_FLASH_MS);
      }, AUTO_EXPAND_MS);
    }
    return () => {
      // Leaving the row mid-hold cancels everything, flash included —
      // a half-played cue on a row the pointer already left is worse
      // than no cue.
      if (autoExpandTimerRef.current) {
        clearTimeout(autoExpandTimerRef.current);
        autoExpandTimerRef.current = null;
      }
      if (springOpenTimerRef.current) {
        clearTimeout(springOpenTimerRef.current);
        springOpenTimerRef.current = null;
      }
      setSpringFlashing(false);
    };
  }, [
    isOver,
    dragContext.activeType,
    hasChildren,
    isExpanded,
    isInvalidDropTarget,
    onSpringExpand,
    topic.id,
  ]);

  return (
    <div>
      {/* Node row */}
      <div
        ref={setNodeRef}
        {...dragAttributes}
        className={`
          touch-no-hover group relative flex items-center gap-1 py-1.5 pr-2 rounded-lg cursor-pointer transition-colors
          ${isActive ? "bg-foreground/6" : isMobile ? "" : "hover:bg-foreground/3"}
          ${isValidOver ? "ring-1 ring-foreground/20 bg-foreground/4" : ""}
          ${springFlashing ? "animate-spring-flash" : ""}
          ${isInvalidDropTarget && isOver ? "ring-1 ring-destructive/40 opacity-60" : ""}
          ${isDragging ? "opacity-50" : ""}
        `}
        style={{ paddingLeft: `${getIndent(depth, isMobile)}px` }}
        onClick={() =>
          router.push(buildTopicPath(currentHubSlug, topic.id), {
            scroll: false,
          })
        }
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        {/* Drag handle — hover-revealed grip, absolute-positioned OUT of
            flex flow so the 16px it used to reserve in the row layout
            doesn't push depth-0 content right of the hub row above.
            Tracks indentation via `left: getIndent - 16` (12px grip +
            4px breathing gap before the chevron). pointer-events-none
            when hidden so the invisible span can't intercept a chevron
            or row click. */}
        <span
          {...dragListeners}
          onClick={(e) => e.stopPropagation()}
          className={`absolute top-1/2 -translate-y-1/2 flex items-center justify-center text-fg-4 cursor-grab active:cursor-grabbing transition-opacity ${
            hovered
              ? "opacity-100 pointer-events-auto"
              : "opacity-0 pointer-events-none"
          }`}
          style={{ left: `${getIndent(depth, isMobile) - 16}px` }}
          aria-label={t.topics.dragHandleAria.replace("{name}", topic.name)}
          role="button"
          tabIndex={-1}
        >
          <GripVertical className="h-3 w-3" />
        </span>

        {/* Expand chevron */}
        <button
          onClick={(e) => {
            e.stopPropagation();
            if (hasChildren) onToggleExpand(topic.id);
          }}
          className={`touch-no-hover p-0.5 rounded shrink-0 transition-transform ${
            hasChildren
              ? `text-fg-4 ${isMobile ? "" : "hover:text-fg-3"}`
              : "text-transparent pointer-events-none"
          } ${isExpanded ? "rotate-90" : ""}`}
        >
          <ChevronRight className="h-3 w-3" />
        </button>

        {/* Icon */}
        <TopicIcon
          name={topic.icon}
          className="h-3.5 w-3.5 text-fg-3 shrink-0"
        />

        {/* Name — 14px per kitchen row-title rule (b1f8fa7a / §29 rules
            32-34). Hierarchy via opacity + weight, never size: inactive
            rows are `font-normal text-fg-2` (scaffolding), active rows
            flip to `font-medium text-fg-1` (accent). No 15px bumps. */}
        <span
          className={`text-[14px] truncate flex-1 ${
            isActive ? "font-medium text-fg-1" : "font-normal text-fg-2"
          }`}
        >
          {topic.name}
        </span>

        {/* Hover actions: ⋮ move menu + (subtopic) plus button + count.
            Visibility is `hovered || menuOpen` (Codex review) so the
            popover's trigger button stays mounted while the popover is
            open — otherwise moving the cursor from the row into the
            popover unmounts the trigger and either closes the popover
            or breaks its anchor. */}
        {actionsVisible ? (
          <>
            <Popover open={menuOpen} onOpenChange={setMenuOpen}>
              <PopoverTrigger
                onClick={(e) => e.stopPropagation()}
                className={`touch-no-hover p-0.5 rounded text-fg-4 shrink-0 ${
                  isMobile ? "" : "hover:text-fg-3"
                }`}
                title={t.topics.moveTopic}
                aria-label={t.topics.moveTopic}
              >
                <MoreVertical className="h-3 w-3" />
              </PopoverTrigger>
              <PopoverContent side="right" align="start" sideOffset={4}>
                <TopicMovePicker
                  topic={topic}
                  hubId={topic.hub_id ?? activeHubId ?? ""}
                  hubName={currentHubName}
                  forest={topicsQuery.data?.topics ?? []}
                  excludedIds={collectTopicDescendantIds(
                    topicsQuery.data?.topics ?? [],
                    topic.id,
                  )}
                  onSelect={(targetParentId, parentName) => {
                    setMenuOpen(false);
                    // Root destination success copy matches the drag path
                    // (topic-dnd-provider.handleTopicDrop). The picker
                    // passes `currentHubName` as parentName when the user
                    // selects the root entry, so we can reuse that here.
                    // Fall back to the neutral topicMoved string when the
                    // hub name hasn't resolved — Codex: never emit an
                    // empty destination.
                    const successMessage =
                      targetParentId === null
                        ? parentName
                          ? t.topics.topicMovedToHub
                              .replace("{name}", topic.name)
                              .replace("{hub}", parentName)
                          : t.topics.topicMoved.replace("{name}", topic.name)
                        : t.topics.topicMovedUnderParent
                            .replace("{name}", topic.name)
                            .replace("{parent}", parentName ?? "");
                    void topicMover.moveTopicWithUndo(
                      {
                        id: topic.id,
                        hubId: topic.hub_id ?? activeHubId ?? "",
                        parentId: topic.parent_id ?? null,
                        position: topic.position,
                        name: topic.name,
                      },
                      { parentId: targetParentId },
                      successMessage,
                    );
                  }}
                />
              </PopoverContent>
            </Popover>
            {onCreateSubtopic && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onCreateSubtopic(topic.id);
                }}
                className={`touch-no-hover p-0.5 rounded text-fg-4 shrink-0 ${
                  isMobile ? "" : "hover:text-fg-3"
                }`}
                title={t.topics.newSubtopic}
              >
                <Plus className="h-3 w-3" />
              </button>
            )}
          </>
        ) : (
          <span className="text-[12px] text-fg-4 tabular-nums shrink-0">
            {topic.total_memory_count}
          </span>
        )}
      </div>

      {/* Children */}
      {isExpanded &&
        hasChildren &&
        topic.children.map((child) => (
          <TopicTreeNode
            key={child.id}
            topic={child}
            depth={depth + 1}
            isMobile={isMobile}
            expandedIds={expandedIds}
            onToggleExpand={onToggleExpand}
            onSpringExpand={onSpringExpand}
            activeTopic={activeTopic}
            onCreateSubtopic={onCreateSubtopic}
          />
        ))}
    </div>
  );
}
