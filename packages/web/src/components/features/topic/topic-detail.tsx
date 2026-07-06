"use client";

import {
  startTransition,
  useMemo,
  useState,
  useCallback,
  useEffect,
  useRef,
} from "react";
import { useRouter, usePathname } from "next/navigation";
import { useQueries } from "@tanstack/react-query";
import {
  buildMemoriesPath,
  buildMemoryDetailPath,
  buildTopicPath,
  getHubSlugForPath,
} from "@/lib/route-helpers";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import {
  Archive,
  ArrowUpRight,
  ChevronRight,
  Pencil,
  Pin,
  Trash2,
} from "lucide-react";
import {
  getTopicMemoriesQueryOptions,
  getTopicQueryOptions,
  useArchiveTopic,
  useDeleteTopic,
  useMarkTopicVisit,
  useRestoreTopic,
  useTopic,
  useTopics,
  useTopicMemories,
  useUpdateTopic,
  type TopicTree,
} from "@/hooks/use-topics";
import { ActionMenu, actionMenuEntries } from "../action-menu";
import { pluralize, useLocale, useInterpolate } from "@/i18n";
import { TopicBreadcrumb } from "./topic-breadcrumb";
import { TopicIcon } from "./topic-icon";
import { DraggableMemoryRow } from "../memory-card/memory-row-draggable";
import { MemoryRowSkeletonList } from "../memory-card/memory-row-skeleton";
import { formatAge } from "@/lib/format-age";
import { ContentError, Skeleton } from "@memaxlabs/ui";
import { NORMAL, EASE } from "@memaxlabs/ui/tokens/motion";
import { useSelection } from "@/contexts/selection-context";
import { BatchToolbar } from "@/components/features/batch-toolbar";
import { useBarToast } from "@/hooks/use-bar-toast";
import { useMemoryForget } from "@/hooks/use-memory-forget";
import { useMemoryMove } from "@/hooks/use-memory-move";
import type { Memory } from "@/hooks/use-memories";
import { useAuth } from "@/lib/auth";
import { queryClient } from "@/lib/query-client";

const DEFAULT_SUBTOPIC_PAGE_SIZE = 20;
const INLINE_EXPAND_THRESHOLD = 100;

function findInTree(trees: TopicTree[], id: string): TopicTree | undefined {
  for (const tree of trees) {
    if (tree.id === id) return tree;
    const found = findInTree(tree.children, id);
    if (found) return found;
  }
  return undefined;
}

function buildAncestorChain(
  trees: TopicTree[],
  id: string,
  path: Array<{ id: string; name: string }> = [],
): Array<{ id: string; name: string }> | null {
  for (const tree of trees) {
    if (tree.id === id) {
      return [...path, { id: tree.id, name: tree.name }];
    }
    const found = buildAncestorChain(tree.children, id, [
      ...path,
      { id: tree.id, name: tree.name },
    ]);
    if (found) return found;
  }
  return null;
}

function getDrillDirection(
  trees: TopicTree[] | undefined,
  currentTopicID: string,
  nextTopicID: string,
): 1 | -1 {
  if (!trees || currentTopicID === nextTopicID) return 1;
  const nextPath = buildAncestorChain(trees, nextTopicID) ?? [];
  if (nextPath.some((item) => item.id === currentTopicID)) {
    return 1;
  }
  const currentPath = buildAncestorChain(trees, currentTopicID) ?? [];
  if (currentPath.some((item) => item.id === nextTopicID)) {
    return -1;
  }
  return 1;
}

function groupDirectMemories(
  memories: Memory[],
  rootTopicId: string,
): {
  rootDirectMemories: Memory[];
  directByTopicId: Map<string, Memory[]>;
} {
  const rootDirectMemories: Memory[] = [];
  const directByTopicId = new Map<string, Memory[]>();

  for (const memory of memories) {
    const resolvedTopicId = memory.topic_id ?? rootTopicId;
    if (resolvedTopicId === rootTopicId) {
      rootDirectMemories.push(memory);
      continue;
    }
    const existing = directByTopicId.get(resolvedTopicId) ?? [];
    existing.push(memory);
    directByTopicId.set(resolvedTopicId, existing);
  }

  return { rootDirectMemories, directByTopicId };
}

function mergeDirectMemoryGroups(
  groups: Array<{
    rootTopicId: string;
    memories: Memory[];
  }>,
): Map<string, Memory[]> {
  const merged = new Map<string, Map<string, Memory>>();

  for (const group of groups) {
    const { rootDirectMemories, directByTopicId } = groupDirectMemories(
      group.memories,
      group.rootTopicId,
    );

    if (rootDirectMemories.length > 0) {
      const existing =
        merged.get(group.rootTopicId) ?? new Map<string, Memory>();
      for (const memory of rootDirectMemories) {
        existing.set(memory.id, memory);
      }
      merged.set(group.rootTopicId, existing);
    }

    for (const [topicId, topicMemories] of directByTopicId.entries()) {
      const existing = merged.get(topicId) ?? new Map<string, Memory>();
      for (const memory of topicMemories) {
        existing.set(memory.id, memory);
      }
      merged.set(topicId, existing);
    }
  }

  return new Map(
    Array.from(merged.entries(), ([topicId, topicMemories]) => [
      topicId,
      Array.from(topicMemories.values()),
    ]),
  );
}

function TopicPinButton({
  pinned,
  disabled,
  onToggle,
}: {
  pinned: boolean;
  disabled?: boolean;
  onToggle: () => void;
}) {
  const { t } = useLocale();

  return (
    <button
      type="button"
      aria-label={pinned ? t.topics.unpin : t.topics.pin}
      aria-pressed={pinned}
      disabled={disabled}
      onClick={onToggle}
      className="cursor-pointer rounded-md p-1 text-fg-4 transition-colors hover:bg-surface-2 hover:text-fg-2 disabled:cursor-wait disabled:opacity-50"
    >
      <Pin
        className="h-3.5 w-3.5"
        style={{
          fill: pinned ? "currentColor" : "none",
          transform: pinned ? "rotate(-45deg)" : "rotate(0deg)",
          transition: `transform ${NORMAL}s ${EASE}`,
        }}
      />
    </button>
  );
}

function ContributorAvatarCluster({
  contributors,
  totalCount,
}: {
  contributors:
    | Array<{
        user_id: string;
        user_name: string;
        user_avatar_url?: string;
      }>
    | undefined;
  totalCount: number;
}) {
  if (!contributors || contributors.length === 0 || totalCount === 0) {
    return null;
  }

  const visible = contributors.slice(0, 3);
  const extra = Math.max(totalCount - visible.length, 0);

  return (
    <div className="flex items-center">
      {visible.map((contributor, index) => (
        <div
          key={contributor.user_id}
          className="h-5 w-5 overflow-hidden rounded-full flex items-center justify-center text-[10px] font-medium"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.08)",
            color: "var(--fg-2)",
            marginLeft: index === 0 ? 0 : -6,
            border: "1.5px solid var(--background)",
            zIndex: 10 - index,
          }}
          title={contributor.user_name}
        >
          {contributor.user_avatar_url ? (
            <img
              src={contributor.user_avatar_url}
              alt=""
              className="h-full w-full"
            />
          ) : (
            (contributor.user_name[0]?.toUpperCase() ?? "?")
          )}
        </div>
      ))}
      {extra > 0 && (
        <span className="ml-1 text-[11px] text-fg-3 tabular-nums">
          +{extra}
        </span>
      )}
    </div>
  );
}

function TopicActivityStrip({
  updatedAt,
  activitySummary,
}: {
  updatedAt: string;
  activitySummary?: {
    window_days: number;
    memory_count: number;
    contributor_count: number;
    contributors_preview?: Array<{
      user_id: string;
      user_name: string;
      user_avatar_url?: string;
    }>;
  };
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const lastTouched = formatAge(updatedAt, t, interpolate);

  return (
    <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2 text-[12px] text-fg-3">
      <span className="tabular-nums">
        {interpolate(t.topics.lastTouched, { time: lastTouched })}
      </span>
      {activitySummary && activitySummary.memory_count > 0 && (
        <>
          <span className="text-fg-4">·</span>
          <span>
            {interpolate(t.topics.activityWindow, {
              n: String(activitySummary.window_days),
            })}
          </span>
          <span className="font-medium text-fg-2 tabular-nums">
            {interpolate(t.topics.activityDelta, {
              n: String(activitySummary.memory_count),
            })}
          </span>
          <ContributorAvatarCluster
            contributors={activitySummary.contributors_preview}
            totalCount={activitySummary.contributor_count}
          />
        </>
      )}
    </div>
  );
}

function TopicMemoryList({
  memories,
  totalMemoryCount,
  onClickMemory,
  onDrill,
}: {
  memories: Memory[];
  totalMemoryCount: number;
  onClickMemory: (id: string) => void;
  onDrill?: () => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const [visible, setVisible] = useState(DEFAULT_SUBTOPIC_PAGE_SIZE);
  const reduced = useReducedMotion();
  const overThreshold = totalMemoryCount > INLINE_EXPAND_THRESHOLD;
  const loadedCount = memories.length;
  const visibleMemories = overThreshold
    ? memories.slice(0, DEFAULT_SUBTOPIC_PAGE_SIZE)
    : memories.slice(0, visible);
  const hasMoreInline = !overThreshold && loadedCount > visible;
  const canCollapse =
    !overThreshold && visible > DEFAULT_SUBTOPIC_PAGE_SIZE && loadedCount > 0;
  const shouldOfferDrill =
    !!onDrill &&
    loadedCount > 0 &&
    (overThreshold || totalMemoryCount > loadedCount || hasMoreInline);

  if (loadedCount === 0) return null;

  return (
    <>
      <AnimatePresence initial={false}>
        {visibleMemories.map((memory, index) => (
          <motion.div
            key={memory.id}
            initial={reduced ? { opacity: 0 } : { opacity: 0, y: 8 }}
            animate={reduced ? { opacity: 1 } : { opacity: 1, y: 0 }}
            exit={reduced ? { opacity: 0 } : { opacity: 0, y: -8 }}
            transition={{ duration: NORMAL, ease: EASE }}
          >
            <DraggableMemoryRow
              memory={memory}
              surface="topic"
              showDivider={index > 0}
              onClick={() => onClickMemory(memory.id)}
            />
          </motion.div>
        ))}
      </AnimatePresence>

      {(hasMoreInline || canCollapse || shouldOfferDrill) && (
        <div className="flex items-center gap-3 border-t border-border/20 px-4">
          {hasMoreInline && (
            <button
              type="button"
              onClick={() =>
                setVisible((current) =>
                  Math.min(current + DEFAULT_SUBTOPIC_PAGE_SIZE, loadedCount),
                )
              }
              className="touch-no-hover cursor-pointer py-2 text-[12px] text-fg-3 transition-colors hover:text-fg-2"
            >
              {interpolate(t.topics.showNMore, {
                n: String(
                  Math.min(DEFAULT_SUBTOPIC_PAGE_SIZE, loadedCount - visible),
                ),
              })}
            </button>
          )}
          {canCollapse && (
            <button
              type="button"
              onClick={() => setVisible(DEFAULT_SUBTOPIC_PAGE_SIZE)}
              className="touch-no-hover cursor-pointer py-2 text-[12px] text-fg-3 transition-colors hover:text-fg-2"
            >
              {t.topics.collapse}
            </button>
          )}
          <div className="flex-1" />
          {shouldOfferDrill && (
            <button
              type="button"
              onClick={onDrill}
              className="touch-no-hover flex cursor-pointer items-center gap-1 py-2 text-[12px] font-medium text-fg-2 transition-colors hover:text-fg-1"
            >
              <span>
                {overThreshold
                  ? interpolate(t.topics.openFullViewCount, {
                      n: String(totalMemoryCount),
                    })
                  : t.topics.openFullView}
              </span>
              <ArrowUpRight className="h-3 w-3" />
            </button>
          )}
        </div>
      )}
    </>
  );
}

function SubtopicGroup({
  topic,
  directByTopicId,
  loadingTopicIds,
  expanded,
  depth = 0,
  onToggle,
  onDrill,
  onClickMemory,
}: {
  topic: TopicTree;
  directByTopicId: Map<string, Memory[]>;
  loadingTopicIds: Set<string>;
  expanded: Record<string, boolean>;
  depth?: number;
  onToggle: (id: string) => void;
  onDrill: (id: string) => void;
  onClickMemory: (id: string) => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const reduced = useReducedMotion();
  const directMemories = directByTopicId.get(topic.id) ?? [];
  const hasChildren = topic.children.length > 0;
  const canExpand = hasChildren || topic.memory_count > 0;
  const isOpen = expanded[topic.id] ?? false;
  const isLoadingDirectMemories = loadingTopicIds.has(topic.id);
  const indentPx = Math.min(depth, 2) * 16;

  return (
    <>
      <div
        className="touch-no-hover flex items-start border-t border-border/20 transition-colors hover:bg-surface-1"
        style={{ paddingLeft: `${indentPx}px` }}
      >
        {canExpand ? (
          <button
            type="button"
            onClick={() => onToggle(topic.id)}
            aria-expanded={isOpen}
            aria-label={
              isOpen
                ? `${t.topics.collapse} ${topic.name}`
                : `${t.topics.openFullView} ${topic.name}`
            }
            className="touch-no-hover flex shrink-0 cursor-pointer items-center justify-center px-4 py-2.5 text-fg-3 transition-colors hover:text-fg-1"
            style={{ height: 40 }}
          >
            <ChevronRight
              className="h-3 w-3"
              style={{
                transform: isOpen ? "rotate(90deg)" : "rotate(0deg)",
                transition: reduced ? "none" : `transform ${NORMAL}s ${EASE}`,
              }}
            />
          </button>
        ) : (
          <span
            className="flex shrink-0 items-center justify-center px-4 py-2.5"
            style={{ height: 40 }}
          >
            <span className="block h-3 w-3" />
          </span>
        )}

        <button
          type="button"
          onClick={() => onDrill(topic.id)}
          className="touch-no-hover flex-1 cursor-pointer py-2.5 pr-4 text-left"
        >
          <div className="flex min-h-5 items-center gap-2">
            <TopicIcon
              name={topic.icon}
              className="h-4 w-4 shrink-0 text-fg-3"
            />
            <span
              className={`min-w-0 flex-1 truncate text-[14px] ${
                isOpen ? "font-medium text-fg-1" : "text-fg-2"
              }`}
            >
              {topic.name}
            </span>
            <span className="shrink-0 text-[12px] text-fg-3 tabular-nums">
              {formatAge(topic.updated_at, t, interpolate)}
            </span>
          </div>
          {isOpen && depth === 0 && topic.description && (
            <p className="mt-0.5 pl-6 text-[12px] text-fg-3">
              {topic.description}
            </p>
          )}
        </button>
      </div>

      <AnimatePresence initial={false}>
        {isOpen && (
          <motion.div
            key={`${topic.id}-content`}
            initial={reduced ? { opacity: 0 } : { height: 0, opacity: 0 }}
            animate={reduced ? { opacity: 1 } : { height: "auto", opacity: 1 }}
            exit={reduced ? { opacity: 0 } : { height: 0, opacity: 0 }}
            transition={{ duration: NORMAL, ease: EASE }}
            style={{ overflow: "hidden" }}
          >
            <div style={{ paddingLeft: `${indentPx + 20}px` }}>
              {isLoadingDirectMemories && directMemories.length === 0 ? (
                <div className="px-4 py-3">
                  <MemoryListSkeleton />
                </div>
              ) : (
                <TopicMemoryList
                  memories={directMemories}
                  totalMemoryCount={topic.total_memory_count}
                  onClickMemory={onClickMemory}
                  onDrill={() => onDrill(topic.id)}
                />
              )}
            </div>

            {topic.children.map((child) => (
              <SubtopicGroup
                key={child.id}
                topic={child}
                directByTopicId={directByTopicId}
                loadingTopicIds={loadingTopicIds}
                expanded={expanded}
                depth={depth + 1}
                onToggle={onToggle}
                onDrill={onDrill}
                onClickMemory={onClickMemory}
              />
            ))}
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}

export function TopicDetail({
  topicId,
  suppressInitialAnimation = false,
}: {
  topicId: string;
  suppressInitialAnimation?: boolean;
}) {
  const [activeTopicId, setActiveTopicId] = useState(topicId);
  const [direction, setDirection] = useState<1 | -1>(1);
  const { activeHubId } = useAuth();
  const {
    data: topic,
    isLoading: loadingTopic,
    isError: topicError,
    refetch: refetchTopic,
  } = useTopic(activeTopicId);
  const {
    data: topicsData,
    isError: topicTreeError,
    refetch: refetchTopics,
  } = useTopics();
  const {
    data: memData,
    isLoading: loadingMems,
    isError: memoriesError,
    refetch: refetchMemories,
  } = useTopicMemories(activeTopicId, 50);
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const pathname = usePathname();
  // Hub slug from the current URL — `null` on v1 routes. Topic / memory
  // navigation builders use this so a click on a team-hub page stays on
  // that hub instead of falling through the v1 path that v2 middleware
  // would redirect to `/h/personal/...`.
  const currentHubSlug = getHubSlugForPath(pathname);
  const updateTopic = useUpdateTopic();
  const archiveTopic = useArchiveTopic();
  const restoreTopic = useRestoreTopic();
  const deleteTopic = useDeleteTopic();
  const [pendingPin, setPendingPin] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [renameValue, setRenameValue] = useState("");
  const renameInputRef = useRef<HTMLInputElement | null>(null);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const selection = useSelection();
  const forget = useMemoryForget();
  const mover = useMemoryMove();
  const toast = useBarToast();
  const reduced = useReducedMotion();
  const hasMountedRef = useRef(false);
  const markTopicVisit = useMarkTopicVisit();
  const { setVisibleIds: selectionSetVisibleIds } = selection;

  useEffect(() => {
    hasMountedRef.current = true;
  }, []);

  // Register a topic visit on real page entry — the clear-anchor for
  // scan-surface dream-delta signals (memory row breadcrumb tint +
  // topic card delta chip).
  //
  // STRICT fire contract (see lifecycle design doc):
  //   - fires on topic page mount only; re-fires when activeTopicId
  //     changes (user drills into a subtopic).
  //   - gated on data actually being loaded (loadingTopic === false +
  //     a topic exists) so prefetch / placeholder-only renders don't
  //     trigger a false visit.
  //   - 300ms dwell timer cleared on unmount — if the user bounces off
  //     in under 300ms, the visit does not register. Matches the
  //     inbox seen-on-expand intent semantics.
  //   - NEVER called from prefetch (queryClient.prefetchQuery paths)
  //     nor from the memory detail page. Grep-gated before merge.
  useEffect(() => {
    if (!activeTopicId || loadingTopic) return;
    const id = activeTopicId;
    const timer = setTimeout(() => {
      markTopicVisit.mutate(id);
    }, 300);
    return () => clearTimeout(timer);
    // markTopicVisit is a stable mutation object from React Query; we
    // depend on activeTopicId (drill target) + loadingTopic (gate on
    // real page render). ESLint rule-of-hooks satisfied.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTopicId, loadingTopic]);

  const suppressEntryAnimation =
    suppressInitialAnimation && !hasMountedRef.current;

  useEffect(() => {
    if (topicId === activeTopicId) return;
    setDirection(getDrillDirection(topicsData?.topics, activeTopicId, topicId));
    setExpanded({});
    if (selection.active) {
      selection.exit();
    }
    startTransition(() => {
      setActiveTopicId(topicId);
    });
  }, [activeTopicId, selection, topicId, topicsData?.topics]);

  const treeNode = useMemo(
    () =>
      topicsData?.topics
        ? findInTree(topicsData.topics, activeTopicId)
        : undefined,
    [activeTopicId, topicsData?.topics],
  );
  const ancestors = useMemo(
    () =>
      topicsData?.topics
        ? buildAncestorChain(topicsData.topics, activeTopicId)
        : null,
    [activeTopicId, topicsData?.topics],
  );
  const displayTopic = useMemo(() => {
    if (topic?.id === activeTopicId) {
      return topic;
    }
    return treeNode;
  }, [activeTopicId, topic, treeNode]);
  const memories = useMemo(() => memData?.memories ?? [], [memData?.memories]);
  // Register the ordered universe of memory ids loaded for this topic.
  // Flat list covers both flat topic views AND subtopic-grouped views
  // — every id lands in `memories` regardless of rendering shape. Drives
  // Cmd/Ctrl+A and shift-click range via selection-context.
  useEffect(() => {
    selectionSetVisibleIds(memories.map((m) => m.id));
  }, [memories, selectionSetVisibleIds]);
  const { rootDirectMemories, directByTopicId } = useMemo(
    () => groupDirectMemories(memories, activeTopicId),
    [activeTopicId, memories],
  );
  const children = useMemo(
    () => treeNode?.children ?? [],
    [treeNode?.children],
  );
  const visibleTopicLookup = useMemo(() => {
    const lookup = new Map<string, TopicTree>();

    const walk = (nodes: TopicTree[]) => {
      for (const node of nodes) {
        lookup.set(node.id, node);
        if (node.children.length > 0) {
          walk(node.children);
        }
      }
    };

    walk(children);
    return lookup;
  }, [children]);
  const expandedLeafTopicIds = useMemo(
    () =>
      Object.entries(expanded)
        .filter(([, isOpen]) => isOpen)
        .map(([id]) => visibleTopicLookup.get(id))
        .filter(
          (childTopic): childTopic is TopicTree =>
            !!childTopic &&
            childTopic.children.length === 0 &&
            childTopic.memory_count > 0 &&
            (directByTopicId.get(childTopic.id)?.length ?? 0) === 0,
        )
        .map((childTopic) => childTopic.id),
    [directByTopicId, expanded, visibleTopicLookup],
  );
  const expandedLeafMemoryQueries = useQueries({
    queries: expandedLeafTopicIds.map((id) => ({
      ...getTopicMemoriesQueryOptions(id, 50, activeHubId || undefined),
      enabled: true,
    })),
  });
  const loadingExpandedLeafTopicIds = useMemo(() => {
    const loading = new Set<string>();

    expandedLeafMemoryQueries.forEach((query, index) => {
      if (query.isLoading || query.isFetching) {
        loading.add(expandedLeafTopicIds[index]);
      }
    });

    return loading;
  }, [expandedLeafMemoryQueries, expandedLeafTopicIds]);
  const mergedDirectByTopicId = useMemo(() => {
    const merged = new Map(directByTopicId);
    const supplemental = mergeDirectMemoryGroups(
      expandedLeafMemoryQueries.flatMap((query, index) =>
        query.data?.memories
          ? [
              {
                rootTopicId: expandedLeafTopicIds[index],
                memories: query.data.memories,
              },
            ]
          : [],
      ),
    );

    for (const [topicId, topicMemories] of supplemental.entries()) {
      merged.set(topicId, topicMemories);
    }

    return merged;
  }, [directByTopicId, expandedLeafMemoryQueries, expandedLeafTopicIds]);

  const handleTogglePin = useCallback(() => {
    if (!displayTopic) return;
    setPendingPin(true);
    updateTopic.mutate(
      { id: displayTopic.id, pinned: !displayTopic.pinned },
      {
        onSettled: () => setPendingPin(false),
      },
    );
  }, [displayTopic, updateTopic]);

  const handleToggle = useCallback((id: string) => {
    setExpanded((current) => ({ ...current, [id]: !current[id] }));
  }, []);

  // Exit route after this topic leaves the tree (archive/delete): the
  // parent topic when one exists, otherwise the hub's topics overview.
  const exitAfterRemoval = useCallback(() => {
    const parent =
      ancestors && ancestors.length > 1
        ? ancestors[ancestors.length - 2]
        : null;
    router.push(
      parent
        ? buildTopicPath(currentHubSlug, parent.id)
        : buildMemoriesPath(currentHubSlug),
      { scroll: false },
    );
  }, [ancestors, currentHubSlug, router]);

  const handleStartRename = useCallback(() => {
    if (!displayTopic) return;
    setRenameValue(displayTopic.name);
    setRenaming(true);
    requestAnimationFrame(() => renameInputRef.current?.select());
  }, [displayTopic]);

  const handleSubmitRename = useCallback(() => {
    if (!displayTopic) return;
    const trimmed = renameValue.trim();
    setRenaming(false);
    if (!trimmed || trimmed === displayTopic.name) return;
    updateTopic.mutate({ id: displayTopic.id, name: trimmed });
  }, [displayTopic, renameValue, updateTopic]);

  const handleArchive = useCallback(() => {
    if (!displayTopic) return;
    const topicId = displayTopic.id;
    archiveTopic.mutate(topicId, {
      onSuccess: () => {
        exitAfterRemoval();
        toast.success(t.topics.archiveToast, {
          detail: t.topics.archiveToastDetail,
          actionLabel: t.common.undo,
          onAction: () => {
            restoreTopic.mutate(topicId, {
              onSuccess: () => toast.success(t.topics.restoreToast),
            });
            toast.dismiss();
          },
        });
      },
    });
  }, [archiveTopic, displayTopic, exitAfterRemoval, restoreTopic, t, toast]);

  // Destructive-confirm contract: resolve true to close the popover,
  // false to keep the confirm sub-panel mounted for retry. The global
  // mutation meta handles the error toast.
  const handleDelete = useCallback(async () => {
    if (!displayTopic) return false;
    try {
      await deleteTopic.mutateAsync(displayTopic.id);
    } catch {
      return false;
    }
    exitAfterRemoval();
    toast.success(t.topics.deleted);
    return true;
  }, [deleteTopic, displayTopic, exitAfterRemoval, t, toast]);

  const navigateTopic = useCallback(
    (nextTopicId: string, nextDirection: 1 | -1) => {
      if (nextTopicId === activeTopicId) return;
      setDirection(nextDirection);
      setExpanded({});
      if (selection.active) {
        selection.exit();
      }
      startTransition(() => {
        setActiveTopicId(nextTopicId);
      });
      void queryClient.prefetchQuery(
        getTopicQueryOptions(nextTopicId, activeHubId || undefined),
      );
      void queryClient.prefetchQuery(
        getTopicMemoriesQueryOptions(nextTopicId, 50, activeHubId || undefined),
      );
      router.push(buildTopicPath(currentHubSlug, nextTopicId), {
        scroll: false,
      });
    },
    [activeHubId, activeTopicId, currentHubSlug, router, selection],
  );

  const handleDrill = useCallback(
    (nextTopicId: string) => {
      navigateTopic(nextTopicId, 1);
    },
    [navigateTopic],
  );

  const handleNavigateUp = useCallback(
    (nextTopicId: string) => {
      navigateTopic(nextTopicId, -1);
    },
    [navigateTopic],
  );

  if (!displayTopic && loadingTopic) return <TopicDetailSkeleton />;

  if (topicTreeError || (!displayTopic && topicError) || !displayTopic) {
    return (
      <div className="mx-auto max-w-md">
        <ContentError
          plain
          message={t.topics.detailFailed}
          detail={t.topics.detailFailedHint}
          retryLabel={t.states.error.retry}
          onRetry={() => {
            void refetchTopic();
            if (topicTreeError) {
              void refetchTopics();
            }
          }}
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <TopicBreadcrumb
        direction={direction}
        suppressInitialAnimation={suppressEntryAnimation}
        items={
          ancestors && ancestors.length > 1
            ? [
                ...ancestors.slice(0, -1).map((ancestor) => ({
                  label: ancestor.name,
                  href: buildTopicPath(currentHubSlug, ancestor.id),
                  onClick: () => handleNavigateUp(ancestor.id),
                })),
                { label: displayTopic.name },
              ]
            : [{ label: displayTopic.name }]
        }
      />

      {/* Outer wrapper: overflow-hidden is preserved unconditionally so the
          ±16px drill x-translate (motion.div below) is clipped cleanly on
          both mobile and desktop. Card chrome (rounded-surface + bg-card/70 +
          p-1) is gated behind sm: so mobile renders on bare --background
          while desktop keeps the card-in-card aesthetic. */}
      <div className="overflow-hidden sm:rounded-surface sm:bg-card/70 sm:p-1">
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={activeTopicId}
            initial={
              suppressEntryAnimation
                ? false
                : reduced
                  ? { opacity: 0 }
                  : { opacity: 0, x: direction * 16 }
            }
            animate={reduced ? { opacity: 1 } : { opacity: 1, x: 0 }}
            exit={reduced ? { opacity: 0 } : { opacity: 0, x: -direction * 16 }}
            transition={{ duration: NORMAL, ease: EASE }}
            // rounded-[calc(...)] nests 4px inside the outer card at sm+;
            // px-5 py-5 is internal padding owned by the motion panel.
            // Mobile drops both — the page container (memories/layout.tsx)
            // already provides px-5 sm:px-8 horizontal rhythm so content
            // aligns to screen edge + 20px, matching /memories/[id].
            className="sm:rounded-[calc(var(--radius-xl)-4px)] sm:px-5 sm:py-5"
          >
            <div className="flex items-start gap-3">
              <TopicIcon
                name={displayTopic.icon}
                className="mt-1 h-7 w-7 shrink-0 text-fg-2"
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-start gap-2">
                  {renaming ? (
                    <input
                      ref={renameInputRef}
                      value={renameValue}
                      onChange={(event) => setRenameValue(event.target.value)}
                      onBlur={handleSubmitRename}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") handleSubmitRename();
                        if (event.key === "Escape") setRenaming(false);
                      }}
                      maxLength={100}
                      aria-label={t.topics.rename}
                      className="min-w-0 flex-1 rounded-chrome border border-border/60 bg-transparent px-1.5 py-0.5 text-[21px] font-bold text-foreground outline-none focus-visible:border-ring"
                      style={{ letterSpacing: "-0.02em" }}
                    />
                  ) : (
                    <h2
                      className="text-[21px] font-bold text-foreground"
                      style={{ letterSpacing: "-0.02em" }}
                    >
                      {displayTopic.name}
                    </h2>
                  )}
                  <div className="ml-auto flex items-center gap-2">
                    {memories.length > 0 && (
                      <button
                        onClick={
                          selection.active
                            ? selection.exit
                            : () => selection.enter()
                        }
                        className="touch-no-hover cursor-pointer text-[12px] text-fg-3 transition-colors hover:text-fg-2"
                      >
                        {selection.active ? t.batch.done : t.batch.select}
                      </button>
                    )}
                    <TopicPinButton
                      pinned={displayTopic.pinned}
                      disabled={pendingPin}
                      onToggle={handleTogglePin}
                    />
                    <ActionMenu
                      triggerAriaLabel={t.topics.moreActions}
                      items={actionMenuEntries(
                        {
                          id: "rename",
                          label: t.topics.rename,
                          icon: Pencil,
                          onSelect: handleStartRename,
                        },
                        {
                          id: "archive",
                          label: t.topics.archive,
                          icon: Archive,
                          onSelect: handleArchive,
                        },
                        "divider",
                        {
                          id: "delete",
                          label: t.topics.delete,
                          icon: Trash2,
                          destructive: true,
                          onConfirm: handleDelete,
                          confirmPrompt: t.topics.deleteConfirmShort,
                          confirmLabel: t.topics.delete,
                          cancelLabel: t.topics.deleteKeep,
                          pendingLabel: t.topics.deletePending,
                        },
                      )}
                    />
                  </div>
                </div>

                {displayTopic.description && (
                  <p className="mt-1 text-[14px] text-fg-2">
                    {displayTopic.description}
                  </p>
                )}

                <TopicActivityStrip
                  updatedAt={displayTopic.updated_at}
                  activitySummary={displayTopic.activity_summary}
                />
              </div>
            </div>

            {memoriesError && (
              <div className="mx-auto max-w-md py-4">
                <ContentError
                  plain
                  message={t.topics.memoriesFailed}
                  detail={t.topics.memoriesFailedHint}
                  retryLabel={t.states.error.retry}
                  onRetry={() => {
                    void refetchMemories();
                  }}
                />
              </div>
            )}

            {memoriesError ? null : loadingMems ? (
              <MemoryListSkeleton />
            ) : memories.length === 0 && children.length === 0 ? (
              <p className="py-8 text-center text-[14px] text-fg-3">
                {t.topics.empty}
              </p>
            ) : (
              // Inner list: card chrome (rounded-surface + border + bg-card
              // + overflow-hidden) gated behind sm: so mobile renders
              // borderless — memory list rows + subtopic groups sit
              // directly on --background, matching the Phase 1 borderless
              // grammar of topic list → memory detail. Desktop keeps the
              // list card for discrete-object affordance at wider widths.
              <div className="mt-6 sm:overflow-hidden sm:rounded-surface sm:border sm:border-border/50 sm:bg-card">
                <TopicMemoryList
                  memories={rootDirectMemories}
                  totalMemoryCount={rootDirectMemories.length}
                  onClickMemory={(id) =>
                    router.push(buildMemoryDetailPath(currentHubSlug, id))
                  }
                />

                {children.map((child) => (
                  <SubtopicGroup
                    key={child.id}
                    topic={child}
                    directByTopicId={mergedDirectByTopicId}
                    loadingTopicIds={loadingExpandedLeafTopicIds}
                    expanded={expanded}
                    onToggle={handleToggle}
                    onDrill={handleDrill}
                    onClickMemory={(id) =>
                      router.push(buildMemoryDetailPath(currentHubSlug, id))
                    }
                  />
                ))}
              </div>
            )}
          </motion.div>
        </AnimatePresence>
      </div>

      <BatchToolbar
        pending={forget.isPending || mover.isPending}
        pendingLabel={
          forget.isPending
            ? pluralize(
                t.batch.forgettingOne,
                t.batch.forgetting,
                selection.selected.size,
              )
            : mover.isPending
              ? pluralize(
                  t.batch.movingOne,
                  t.batch.moving,
                  selection.selected.size,
                )
              : undefined
        }
        onBatchForget={async (ids) => {
          // Return boolean so the toolbar's destructive confirmation
          // card stays mounted on failure. Selection only clears on true.
          const success = await forget.forgetWithConfirm(ids);
          if (success) {
            selection.exit();
          }
          return success;
        }}
        onBatchMove={(ids, target) => {
          const snapshots = memories
            .filter((memory) => ids.includes(memory.id))
            .map((memory) => ({
              id: memory.id,
              hubId: memory.hub_id,
              topicId: memory.topic_id || undefined,
            }));
          void mover
            .moveWithUndo(
              snapshots,
              { topicId: target.topicId, hubId: target.hubId },
              mover.moveManySuccess(ids.length, target.destinationName),
            )
            .then((ok) => {
              if (ok) selection.exit();
            });
        }}
        onCopyContext={(ids) => {
          const selectedMemories = memories.filter((memory) =>
            ids.includes(memory.id),
          );
          const text = selectedMemories
            .map(
              (memory) =>
                `## ${memory.title}\n${memory.content || memory.summary || ""}`,
            )
            .join("\n\n---\n\n");
          navigator.clipboard.writeText(
            `<memax-context>\n${text}\n</memax-context>`,
          );
          toast.success(t.batch.copied);
        }}
      />
    </div>
  );
}

function TopicDetailSkeleton() {
  // Card chrome gated behind sm: so mobile skeleton matches the new
  // mobile borderless structure of the loaded topic-detail surface.
  // Desktop keeps the rounded-surface bg-card/70 p-5 framing.
  return (
    <div className="space-y-4">
      <Skeleton className="h-3 w-40" />
      <div className="sm:rounded-surface sm:bg-card/70 sm:p-5">
        <div className="mb-4 flex items-start gap-3">
          <Skeleton className="h-7 w-7 rounded-lg" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-6 w-40" />
            <Skeleton className="h-4 w-72 max-w-full" />
          </div>
        </div>
        <MemoryListSkeleton />
      </div>
    </div>
  );
}

function MemoryListSkeleton() {
  // Uses the Phase 1 MemoryRowSkeletonList primitive for geometry parity
  // with the loaded topic-detail memory rows (topic surface uses the
  // single-line row layout). Skeleton → loaded transition has zero
  // layout shift.
  return <MemoryRowSkeletonList count={5} layout="single" />;
}
