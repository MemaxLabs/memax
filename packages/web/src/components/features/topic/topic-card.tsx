"use client";

import {
  useCallback,
  useEffect,
  useRef,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { motion, LayoutGroup, useReducedMotion } from "framer-motion";
import {
  GripVertical,
  Hash,
  LayoutGrid,
  List,
  Network,
  Pin,
  Rows3,
} from "lucide-react";
import { NORMAL, EASE } from "@memaxlabs/ui/tokens/motion";
import {
  InfoPopover,
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@memaxlabs/ui";
import { MemaxStar } from "../memax-star";
import { useLocale, useInterpolate } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import type { TopicTree } from "@/hooks/use-topics";
import type { TopicViewMode } from "@/lib/topic-view-mode";
import { TopicIcon } from "./topic-icon";
import { useActiveHub } from "@/lib/auth";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { queryClient } from "@/lib/query-client";
import { isTextSelectionActiveIn } from "@/lib/text-selection";

// Shared click guard for topic cards: if the user is in the middle of a
// drag-select (typical way to copy a topic title), the mouseup that ends
// the drag still fires onClick, which would yank them to the topic
// detail. Swallow the click when a non-collapsed selection lives inside
// this card. Keyboard activation is unaffected (no selection is in
// play when a user presses Enter on a focused card).
function handleTopicCardClick(
  event: ReactMouseEvent<HTMLElement>,
  navigate: () => void,
) {
  if (isTextSelectionActiveIn(event.currentTarget)) return;
  navigate();
}
import {
  getTopicMemoriesQueryOptions,
  getTopicQueryOptions,
} from "@/hooks/use-topics";
import { useDraggableTopic } from "./topic-dnd-hooks";

/**
 * useTopicCardDrag — DRY wrapper around useDraggableTopic for the three
 * main-grid card variants. Shares the drag payload shape with the pinned
 * tree node so drags originating here route through the same useTopicMove
 * pipeline as drags from the sidebar tree (same undo + error mapping).
 *
 * The hook is called unconditionally per variant. hubId falls back to the
 * currently active hub when topic.hub_id is missing (defensive — topics
 * from useTopics() always carry hub_id, but the ?? chain mirrors the
 * pattern used in topic-tree-node.tsx for parity).
 */
function useTopicCardDrag(topic: TopicTree) {
  const { activeHub } = useActiveHub();
  return useDraggableTopic({
    id: topic.id,
    name: topic.name,
    hubId: topic.hub_id ?? activeHub?.hub.id ?? "",
    parentId: topic.parent_id ?? null,
    position: topic.position,
    icon: topic.icon,
    count: topic.total_memory_count,
  });
}

/**
 * TopicCardGrip — hover-revealed drag handle absolutely positioned at the
 * top-left of a card (or left-edge of a row). Matches the left-edge grip
 * pattern from TopicTreeNode so the affordance is consistent across both
 * tree and main-grid surfaces.
 *
 * Listeners live on the grip ONLY so card click still navigates (same
 * gesture-ownership split as memory rows). setNodeRef stays on the card
 * root via the caller — dnd-kit's drag overlay uses the card bbox, not
 * the tiny grip hitbox.
 */
function TopicCardGrip({
  attributes,
  listeners,
  topicName,
  position = "card",
}: {
  // dnd-kit draggable attrs/listeners — typed loose to avoid coupling.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  attributes: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  listeners: any;
  topicName: string;
  /** "card" → top-left corner; "row" → left-edge centered vertically. */
  position?: "card" | "row";
}) {
  const { t } = useLocale();
  const placement =
    position === "row" ? "left-1 top-1/2 -translate-y-1/2" : "left-2 top-2";
  return (
    <span
      {...attributes}
      {...listeners}
      onClick={(e) => e.stopPropagation()}
      className={`absolute ${placement} z-10 flex items-center justify-center p-0.5 text-fg-4 opacity-0 group-hover:opacity-100 transition-opacity cursor-grab active:cursor-grabbing`}
      role="button"
      tabIndex={-1}
      aria-label={t.topics.dragHandleAria.replace("{name}", topicName)}
    >
      <GripVertical className="h-3 w-3" />
    </span>
  );
}

interface TopicMainViewProps {
  topics: TopicTree[];
  mode: TopicViewMode;
  dreamsEnabled: boolean;
  onChangeMode: (mode: TopicViewMode) => void;
  onClickTopic: (id: string) => void;
  onTogglePin: (topic: TopicTree) => void;
  pendingPinId?: string | null;
  onPrefetchTopic?: (id: string) => void;
  /** Opens the hierarchical topic tree bottom sheet. Mobile-only entry —
   *  desktop has a dedicated sidebar entrance elsewhere. When provided, a
   *  Network icon button renders in the section header trailing slot. */
  onToggleTree?: () => void;
}

interface TopicItemProps {
  topic: TopicTree;
  age: string;
  pendingPinId?: string | null;
  onClickTopic: (id: string) => void;
  onTogglePin: (topic: TopicTree) => void;
  onPrefetchTopic?: (id: string) => void;
}

/**
 * TopicDeltaChip — secondary signal for `topic.lifecycle.delta_since_visit`.
 *
 * Signature-tinted, compact. Stays subordinate to the topic name by
 * rendering in the trailing meta cluster, after the topic name/count
 * but before the age/pin controls. Dual form:
 *
 *   - Full chip ("✦ +N · M reorganized") on sm+ — topic cards and
 *     desktop mode C.
 *   - Bare ✦ mark on mobile — preserves mode C row rhythm when space is
 *     tight (name > age > pin take precedence).
 *
 * Nothing renders when added = 0 AND reorganized = 0 (server already
 * filters the zero case, but the guard is cheap and defensive).
 */
function TopicDeltaChip({
  delta,
}: {
  delta: NonNullable<TopicTree["lifecycle"]>["delta_since_visit"];
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  if (!delta) return null;
  if (delta.added === 0 && delta.reorganized === 0) return null;

  const fullLabel = interpolate(t.lifecycle.topicDeltaSummary, {
    added: String(delta.added),
    reorganized: String(delta.reorganized),
  });
  // Pluralized line items for the hover-card body. The chip itself
  // stays compact; the rich detail lives inside the popover so we
  // don't crowd the topic card row.
  const addedLabel =
    delta.added > 0
      ? interpolate(t.lifecycle.topicDeltaAdded, {
          n: String(delta.added),
        })
      : null;
  const reorganizedLabel =
    delta.reorganized > 0
      ? interpolate(t.lifecycle.topicDeltaReorganized, {
          n: String(delta.reorganized),
        })
      : null;

  return (
    <HoverCard>
      <HoverCardTrigger
        // Trigger defaults to a <button>. We stop its click from
        // bubbling so it doesn't navigate when nested inside an
        // interactive topic card; the button is purely a hover/focus
        // surface for the rich popover.
        onClick={(e) => e.stopPropagation()}
        className="inline-flex shrink-0 cursor-default appearance-none bg-transparent p-0 m-0 border-0 outline-none"
        aria-label={fullLabel}
      >
        {/* Full chip — desktop and topic cards */}
        <span
          className="hidden sm:inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium"
          style={{
            background: "oklch(from var(--signature) l c h / 0.1)",
            color: "var(--signature)",
            lineHeight: "1.25",
          }}
        >
          <span className="text-[10px] leading-none">✦</span>
          <span className="tabular-nums">{fullLabel}</span>
        </span>
        {/* Compact mark — mobile mode C; preserves name/age priority. */}
        <span
          className="sm:hidden shrink-0 text-[11px] leading-none"
          style={{ color: "var(--signature)" }}
        >
          ✦
        </span>
      </HoverCardTrigger>
      <HoverCardContent
        side="top"
        sideOffset={8}
        className="w-[260px] text-[12.5px] leading-snug"
      >
        <div className="flex items-center gap-2 mb-2">
          <span
            className="inline-flex items-center justify-center w-5 h-5 rounded-md text-[11px]"
            style={{
              background: "oklch(from var(--signature) l c h / 0.14)",
              color: "var(--signature)",
            }}
            aria-hidden
          >
            ✦
          </span>
          <span className="font-medium text-fg-1">
            {t.lifecycle.topicDeltaHeading}
          </span>
        </div>
        <ul className="flex flex-col gap-1 text-fg-2">
          {addedLabel && (
            <li className="flex items-baseline gap-1.5">
              <span
                className="inline-block w-1 h-1 rounded-full mt-1.5 shrink-0"
                style={{ background: "var(--signature)" }}
                aria-hidden
              />
              <span>{addedLabel}</span>
            </li>
          )}
          {reorganizedLabel && (
            <li className="flex items-baseline gap-1.5">
              <span
                className="inline-block w-1 h-1 rounded-full mt-1.5 shrink-0 bg-fg-3"
                aria-hidden
              />
              <span>{reorganizedLabel}</span>
            </li>
          )}
        </ul>
        <p className="text-[11.5px] text-fg-3 mt-2">
          {t.lifecycle.topicDeltaHint}
        </p>
      </HoverCardContent>
    </HoverCard>
  );
}

function TopicPinButton({
  topic,
  pendingPinId,
  onTogglePin,
}: Pick<TopicItemProps, "topic" | "pendingPinId" | "onTogglePin">) {
  const { t } = useLocale();
  const disabled = pendingPinId === topic.id;

  return (
    <button
      type="button"
      aria-label={topic.pinned ? t.topics.unpin : t.topics.pin}
      aria-pressed={topic.pinned}
      disabled={disabled}
      onClick={(event) => {
        event.stopPropagation();
        onTogglePin(topic);
      }}
      className="cursor-pointer rounded-md p-1 text-fg-4 transition-colors hover:bg-surface-2 hover:text-fg-2 disabled:cursor-wait disabled:opacity-50"
    >
      <Pin
        className="h-3.5 w-3.5"
        style={{
          fill: topic.pinned ? "currentColor" : "none",
          transform: topic.pinned ? "rotate(-45deg)" : "rotate(0deg)",
          transition: `transform ${NORMAL}s ${EASE}`,
        }}
      />
    </button>
  );
}

function ModeATopicCard({
  topic,
  age,
  pendingPinId,
  onClickTopic,
  onTogglePin,
  onPrefetchTopic,
}: TopicItemProps) {
  const drag = useTopicCardDrag(topic);
  return (
    <motion.div
      ref={drag.setNodeRef}
      layout
      role="button"
      tabIndex={0}
      onClick={(event) =>
        handleTopicCardClick(event, () => onClickTopic(topic.id))
      }
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onClickTopic(topic.id);
        }
      }}
      onPointerEnter={() => onPrefetchTopic?.(topic.id)}
      onFocus={() => onPrefetchTopic?.(topic.id)}
      className={`touch-no-hover group relative w-full cursor-pointer rounded-card glass-card p-4 text-left transition-colors duration-200 ${
        drag.isDragging ? "opacity-50" : ""
      }`}
      transition={{ duration: NORMAL, ease: EASE }}
    >
      <TopicCardGrip
        attributes={drag.attributes}
        listeners={drag.listeners}
        topicName={topic.name}
        position="card"
      />
      <div className="mb-3 flex items-start gap-3">
        {/* Folder/topic visual: TopicIcon doubles as the "this is a topic
            folder, not a memory" affordance per plan 26 phase 2. Same
            slot position as MemoryCardLayout's file-type icon, so cards
            in a mixed grid (memories + topics) read as the same family
            with different content kinds. Hover comes from the shared
            `.glass-card:hover` recipe in globals.css — no Tailwind
            hover utility needed (would have replaced the glass fill). */}
        <TopicIcon
          name={topic.icon}
          className="mt-0.5 h-5 w-5 shrink-0 text-fg-3"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-start gap-2">
            <span className="truncate text-[15px] font-medium text-fg-1">
              {topic.name}
            </span>
            <div className="ml-auto shrink-0">
              <TopicPinButton
                topic={topic}
                pendingPinId={pendingPinId}
                onTogglePin={onTogglePin}
              />
            </div>
          </div>
          {topic.description && (
            <p className="mt-1 line-clamp-2 text-[14px] text-fg-2">
              {topic.description}
            </p>
          )}
        </div>
      </div>

      {topic.children.length > 0 && (
        <div className="mb-3 flex flex-wrap gap-1.5">
          {topic.children.slice(0, 3).map((child) => (
            <button
              key={child.id}
              type="button"
              className="touch-no-hover cursor-pointer rounded-md bg-surface-1 px-1.5 py-0.5 text-[12px] text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-2"
              onClick={(event) => {
                event.stopPropagation();
                onClickTopic(child.id);
              }}
            >
              {child.name}
            </button>
          ))}
        </div>
      )}

      <div className="flex items-center gap-1.5 text-[12px] text-fg-3">
        <span className="tabular-nums">{age}</span>
        {topic.lifecycle?.delta_since_visit && (
          <TopicDeltaChip delta={topic.lifecycle.delta_since_visit} />
        )}
      </div>
    </motion.div>
  );
}

function ModeBTopicCard({
  topic,
  age,
  pendingPinId,
  onClickTopic,
  onTogglePin,
  onPrefetchTopic,
}: TopicItemProps) {
  const drag = useTopicCardDrag(topic);
  return (
    <motion.div
      ref={drag.setNodeRef}
      layout
      role="button"
      tabIndex={0}
      onClick={(event) =>
        handleTopicCardClick(event, () => onClickTopic(topic.id))
      }
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onClickTopic(topic.id);
        }
      }}
      onPointerEnter={() => onPrefetchTopic?.(topic.id)}
      onFocus={() => onPrefetchTopic?.(topic.id)}
      className={`touch-no-hover group relative w-full cursor-pointer rounded-card glass-card p-3 text-left transition-colors duration-200 ${
        drag.isDragging ? "opacity-50" : ""
      }`}
      transition={{ duration: NORMAL, ease: EASE }}
    >
      <TopicCardGrip
        attributes={drag.attributes}
        listeners={drag.listeners}
        topicName={topic.name}
        position="card"
      />
      <div className="mb-2 flex items-start gap-2">
        {/* Folder/topic visual — see ModeATopicCard comment. */}
        <TopicIcon
          name={topic.icon}
          className="mt-0.5 h-4 w-4 shrink-0 text-fg-3"
        />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[14px] font-medium text-fg-1">
            {topic.name}
          </div>
          <div className="mt-1 flex items-center gap-1.5 text-[12px] text-fg-3">
            <span className="tabular-nums">{age}</span>
            {topic.lifecycle?.delta_since_visit && (
              <TopicDeltaChip delta={topic.lifecycle.delta_since_visit} />
            )}
          </div>
        </div>
        <TopicPinButton
          topic={topic}
          pendingPinId={pendingPinId}
          onTogglePin={onTogglePin}
        />
      </div>
    </motion.div>
  );
}

function ModeCTopicRow({
  topic,
  age,
  pendingPinId,
  onClickTopic,
  onTogglePin,
  onPrefetchTopic,
}: TopicItemProps) {
  const drag = useTopicCardDrag(topic);
  // Border-top token matches the borderless frame used by MemoryRowSkeleton
  // and the memory detail page rhythm — softer than border-border/30 so the
  // mobile borderless list reads as "rows on background," not "cells in a
  // table." Desktop mode C sits inside a card wrapper (via TopicItems) and
  // the same softer divider works there too.
  const divider = "border-t border-[oklch(from_var(--foreground)_l_c_h/0.08)]";
  return (
    <motion.div
      ref={drag.setNodeRef}
      layout
      role="button"
      tabIndex={0}
      onClick={(event) =>
        handleTopicCardClick(event, () => onClickTopic(topic.id))
      }
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onClickTopic(topic.id);
        }
      }}
      onPointerEnter={() => onPrefetchTopic?.(topic.id)}
      onFocus={() => onPrefetchTopic?.(topic.id)}
      className={`touch-no-hover group relative flex w-full cursor-pointer items-center gap-3 px-4 py-3 text-left outline-none transition-colors first:border-t-0 hover:bg-surface-1 focus-visible:bg-surface-1 ${divider} ${
        drag.isDragging ? "opacity-50" : ""
      }`}
      transition={{ duration: NORMAL, ease: EASE }}
    >
      <TopicCardGrip
        attributes={drag.attributes}
        listeners={drag.listeners}
        topicName={topic.name}
        position="row"
      />
      {/* Leading pin indicator: only rendered for pinned topics. Additive
          signal that the trailing pin button (toggle action) is unchanged,
          so pin discoverability / affordance is not regressed. */}
      {topic.pinned && (
        <Pin
          aria-hidden
          className="h-3.5 w-3.5 shrink-0 text-fg-2"
          style={{
            fill: "currentColor",
            transform: "rotate(-45deg)",
          }}
        />
      )}
      <TopicIcon name={topic.icon} className="h-4 w-4 shrink-0 text-fg-3" />
      <span className="min-w-0 flex-1 truncate text-[14px] text-fg-1">
        {topic.name}
      </span>
      {/* Delta chip sits after the name but before age/pin so the row
          reads: [icon] [name ≫ flex-1] [✦ delta] [age] [pin]. On mobile
          the TopicDeltaChip renders as a bare ✦ mark to keep the row
          scannable; full chip with counts appears at sm+. */}
      {topic.lifecycle?.delta_since_visit && (
        <TopicDeltaChip delta={topic.lifecycle.delta_since_visit} />
      )}
      <span className="shrink-0 text-[12px] text-fg-3 tabular-nums">{age}</span>
      <TopicPinButton
        topic={topic}
        pendingPinId={pendingPinId}
        onTogglePin={onTogglePin}
      />
    </motion.div>
  );
}

function TopicViewToggle({
  mode,
  onChangeMode,
}: Pick<TopicMainViewProps, "mode" | "onChangeMode">) {
  const { t } = useLocale();
  const reduced = useReducedMotion();
  const options: Array<{
    value: TopicViewMode;
    label: string;
    icon: typeof LayoutGrid;
  }> = [
    { value: "a", label: t.topics.viewGrid, icon: LayoutGrid },
    { value: "b", label: t.topics.viewDense, icon: Rows3 },
    { value: "c", label: t.topics.viewList, icon: List },
  ];

  return (
    <div
      role="radiogroup"
      aria-label={t.topics.viewLabel}
      className="relative inline-flex items-center rounded-full p-1"
      style={{
        background: "oklch(from var(--card) l c h / 0.6)",
        backdropFilter: "blur(12px) saturate(140%)",
        border: "1px solid oklch(from var(--border) l c h / 0.5)",
      }}
    >
      {options.map((option) => {
        const Icon = option.icon;
        const active = mode === option.value;
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={active}
            aria-label={option.label}
            onClick={() => onChangeMode(option.value)}
            className={`relative flex cursor-pointer items-center gap-1.5 rounded-full px-3 py-1.5 text-[12px] transition-colors ${
              active ? "font-medium text-fg-1" : "text-fg-3 hover:text-fg-2"
            }`}
          >
            {active && (
              <motion.span
                layoutId="topic-view-toggle-indicator"
                className="absolute inset-0 rounded-full"
                style={{
                  background: "var(--card)",
                  boxShadow:
                    "0 1px 3px oklch(from var(--foreground) l c h / 0.12)",
                  border: "1px solid oklch(from var(--border) l c h / 0.5)",
                }}
                transition={{
                  duration: reduced ? 0 : NORMAL,
                  ease: EASE,
                }}
              />
            )}
            <Icon
              className="relative z-10 h-3.5 w-3.5 shrink-0"
              strokeWidth={2}
            />
            <span className="relative z-10 hidden md:inline">
              {option.label}
            </span>
          </button>
        );
      })}
    </div>
  );
}

function TopicItems({
  topics,
  mode,
  pendingPinId,
  onClickTopic,
  onTogglePin,
  onPrefetchTopic,
}: Omit<TopicMainViewProps, "onChangeMode" | "dreamsEnabled">) {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  if (topics.length === 0) return null;

  const content =
    mode === "a" ? (
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        {topics.map((topic) => (
          <ModeATopicCard
            key={topic.id}
            topic={topic}
            age={formatAge(topic.updated_at, t, interpolate)}
            pendingPinId={pendingPinId}
            onClickTopic={onClickTopic}
            onTogglePin={onTogglePin}
            onPrefetchTopic={onPrefetchTopic}
          />
        ))}
      </div>
    ) : mode === "b" ? (
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {topics.map((topic) => (
          <ModeBTopicCard
            key={topic.id}
            topic={topic}
            age={formatAge(topic.updated_at, t, interpolate)}
            pendingPinId={pendingPinId}
            onClickTopic={onClickTopic}
            onTogglePin={onTogglePin}
            onPrefetchTopic={onPrefetchTopic}
          />
        ))}
      </div>
    ) : (
      // Mode C: mobile is borderless (bare --background, full-bleed rows);
      // desktop keeps the card wrapper via sm:-prefixed utilities so the
      // list reads as a discrete object at wider widths. ModeCTopicRow's
      // internal padding is uniform px-4 across breakpoints — outer
      // responsive rhythm (px-5 sm:px-8) is owned by the parent page
      // container, not the row.
      <div className="sm:overflow-hidden sm:rounded-surface sm:border sm:border-border/50 sm:bg-card">
        {topics.map((topic) => (
          <ModeCTopicRow
            key={topic.id}
            topic={topic}
            age={formatAge(topic.updated_at, t, interpolate)}
            pendingPinId={pendingPinId}
            onClickTopic={onClickTopic}
            onTogglePin={onTogglePin}
            onPrefetchTopic={onPrefetchTopic}
          />
        ))}
      </div>
    );

  return <>{content}</>;
}

function TopicEmptyState({ dreamsEnabled }: { dreamsEnabled: boolean }) {
  const { t } = useLocale();
  return (
    <div
      className="flex flex-col items-center gap-3 rounded-surface px-6 py-10 text-center"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
      }}
    >
      <MemaxStar
        className="text-[20px] state-slow-breathe"
        style={{ color: "var(--signature)" }}
      />
      <div className="space-y-1">
        <p className="text-[14px] font-medium text-fg-2">
          {dreamsEnabled ? t.topics.organizing : t.topics.dreamsOff}
        </p>
        <p className="mx-auto max-w-xs text-[12px] text-fg-3">
          {dreamsEnabled ? t.topics.organizingHint : t.topics.dreamsOffHint}
        </p>
      </div>
      {!dreamsEnabled && (
        <a
          href="/settings"
          className="text-[12px] font-medium transition-colors hover:opacity-80"
          style={{ color: "var(--signature)" }}
        >
          {t.topics.dreamsOffEnable}
        </a>
      )}
    </div>
  );
}

export function TopicMainView({
  topics,
  mode,
  dreamsEnabled,
  onChangeMode,
  onClickTopic,
  onTogglePin,
  pendingPinId,
  onToggleTree,
}: TopicMainViewProps) {
  const { t } = useLocale();
  const { isTeamHub, hubFilter } = useActiveHub();
  const isMobile = useIsMobile();
  const prefetchedRef = useRef(new Set<string>());
  const prefetchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const schedulePrefetch = useCallback(
    (topicId: string) => {
      if (isMobile) return;
      if (prefetchedRef.current.has(topicId)) return;
      if (prefetchTimerRef.current) return;
      prefetchTimerRef.current = setTimeout(() => {
        prefetchTimerRef.current = null;
        if (prefetchedRef.current.has(topicId)) return;
        prefetchedRef.current.add(topicId);
        void queryClient.prefetchQuery(
          getTopicQueryOptions(topicId, hubFilter || undefined),
        );
        void queryClient.prefetchQuery(
          getTopicMemoriesQueryOptions(topicId, 50, hubFilter || undefined),
        );
      }, 80);
    },
    [hubFilter, isMobile],
  );

  useEffect(() => {
    return () => {
      if (prefetchTimerRef.current) {
        clearTimeout(prefetchTimerRef.current);
      }
    };
  }, []);
  const orderedTopics = [
    ...topics.filter((topic) => topic.pinned),
    ...topics.filter((topic) => !topic.pinned),
  ];

  return (
    <LayoutGroup id="topic-main-view">
      <section className="space-y-3">
        <div className="flex items-center gap-2 px-1">
          <Hash className="h-3.5 w-3.5 shrink-0 text-fg-3" />
          <span className="text-[14px] font-semibold text-fg-2">
            {isTeamHub ? t.topics.titleTeam : t.topics.titlePersonal}
          </span>
          <InfoPopover
            ariaLabel={t.topics.howItWorksAria}
            title={t.topics.howItWorksTitle}
            body={
              isMobile
                ? t.topics.howItWorksBodyMobile
                : t.topics.howItWorksBodyDesktop
            }
            brandMark={
              <MemaxStar
                animated
                className="text-[14px] leading-none"
                style={{ color: "var(--signature)" }}
              />
            }
          />
          <div className="flex-1" />
          {onToggleTree && (
            <button
              type="button"
              onClick={onToggleTree}
              aria-label={t.topics.treeTitle}
              className="flex h-11 w-11 items-center justify-center rounded-full text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-2 cursor-pointer"
            >
              <Network className="h-4 w-4" strokeWidth={2} />
            </button>
          )}
          {topics.length > 0 && (
            <TopicViewToggle mode={mode} onChangeMode={onChangeMode} />
          )}
        </div>
        {topics.length > 0 ? (
          <TopicItems
            topics={orderedTopics}
            mode={mode}
            pendingPinId={pendingPinId}
            onClickTopic={onClickTopic}
            onTogglePin={onTogglePin}
            onPrefetchTopic={schedulePrefetch}
          />
        ) : (
          <TopicEmptyState dreamsEnabled={dreamsEnabled} />
        )}
      </section>
    </LayoutGroup>
  );
}
