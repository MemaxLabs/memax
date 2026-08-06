"use client";

import { useState, useEffect, useMemo, useCallback } from "react";
import { useRouter, usePathname } from "next/navigation";
import { useTopics } from "@/hooks/use-topics";
import {
  buildMemoryDetailPath,
  buildTopicPath,
  getHubSlugForPath,
} from "@/lib/route-helpers";
import {
  useRecentMemories,
  flattenRecentMemories,
  recentMemoriesTotal,
  recentActorCounts,
  RECENT_PAGE_LIMIT,
  RECENT_PREVIEW_LIMIT,
  TIME_WINDOWS,
  type TimeWindow,
  type RecentActor,
} from "@/hooks/use-recent-memories";
import { pluralize, useLocale, useInterpolate } from "@/i18n";
import { TopicMainView } from "./topic-card";
import {
  TopicCreateDialog,
  type TopicCreateTarget,
} from "./topic-create-dialog";
import { TopicArchivedSection } from "./topic-archived-section";
import { findTopicName } from "@/lib/topic-label";
import type { TopicTree } from "@/hooks/use-topics";
import { DraggableMemoryRow } from "../memory-card/memory-row-draggable";
import { MemoryRowSkeletonList } from "../memory-card/memory-row-skeleton";
import { TopicRowSkeletonList } from "./topic-row-skeleton";
import {
  HubHeader,
  HubHeaderSkeleton,
  HubHeaderUnavailable,
} from "../hub/hub-header";
import type { HubHeaderAuroraMode } from "@memaxlabs/ui";
import {
  Skeleton,
  Popover,
  PopoverTrigger,
  PopoverContent,
  ContentError,
  DataSectionCard,
} from "@memaxlabs/ui";
import {
  ChevronDown,
  Check,
  SlidersHorizontal,
  Clock,
  LayoutGrid,
  List,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import { PinnedNotifications } from "@/components/features/onboarding/onboarding-pinned";
import { BoardSection } from "@/components/features/board/board-view";
import { SelectionProvider, useSelection } from "@/contexts/selection-context";
import { BatchToolbar } from "@/components/features/batch-toolbar";
import { useMemoryForget } from "@/hooks/use-memory-forget";
import { useBarToast } from "@/hooks/use-bar-toast";
import { useMemoryMove } from "@/hooks/use-memory-move";
import { getAgentIdentity } from "@memaxlabs/ui/tokens/agents";
import { useSettings } from "@/hooks/use-settings";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { CENTERED_HEIGHT, MOBILE_CENTERED_HEIGHT } from "@/lib/layout";
import { useTreePanel } from "@/components/features/topic/topic-tree-panel";
import { useActiveHub } from "@/lib/auth";
import { useDreamReport } from "@/hooks/use-dreams";
import { useNotifications } from "@/hooks/use-notifications";
import { getRecentRenderKey, useRecentArrivalIds } from "@/lib/recent-arrivals";
import {
  useHubDetail,
  useHubSummary,
  useMarkHubVisit,
} from "@/hooks/use-hub-management";
import { consumeRecentExpandOnArrival } from "@/lib/recent-navigation";
import { buildTopicPathLookup } from "@/lib/topic-label";
import { countPendingReviewNotifications } from "@/lib/notification-review-count";
import {
  getStoredTopicViewMode,
  resolveTopicViewModeDefault,
  setStoredTopicViewMode,
  type TopicViewMode,
} from "@/lib/topic-view-mode";
import { useUpdateTopic } from "@/hooks/use-topics";
import { FAST, EASE } from "@memaxlabs/ui/tokens/motion";
import { resolveHubHeaderMode } from "@/lib/hub-header-mode";
import { ComposePlusCard } from "../compose/compose-plus-card";
import { ComposePlusCell } from "../compose/compose-plus-cell";
import { TopicTreeContent } from "./topic-tree-content";
import { useTopicTreeController } from "@/hooks/use-topic-tree-controller";
// `ComposePlusCell` is hoisted above DataSectionCard in TopicGrid
// (above) so it stays visible across all phases. RecentSection no
// longer mounts it inside the v2 grid — the dual-render risked two
// `+` cells visible in the loaded phase.
import { DraggableMemoryCard } from "../memory-card/memory-card-draggable";
import { MemoryCardSkeletonList } from "../memory-card/memory-card-skeleton";
/** Main topic cards grid with recent memories section. */
export function TopicGrid() {
  const { activeHub, hubFilter } = useActiveHub();
  const { user } = useAuth();
  const {
    data,
    isLoading,
    isError: topicsError,
    refetch: refetchTopics,
  } = useTopics(hubFilter);
  const { data: notificationListData } = useNotifications({
    status: "pending",
    hub: activeHub?.hub.id ?? undefined,
  });
  const { data: dreamReport } = useDreamReport();
  const { data: settings } = useSettings();
  const {
    data: hubSummary,
    isLoading: hubSummaryLoading,
    refetch: refetchHubSummary,
  } = useHubSummary(activeHub?.hub.id ?? null);
  const { mutate: markHubVisit } = useMarkHubVisit();
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const pathname = usePathname();
  // Hub slug from the current URL — `null` on v1 routes (no /h/<slug>/
  // prefix). Topic / memory navigation builders use this so a click on
  // a team-hub page (`/h/engineering/memories`) stays on that hub
  // instead of falling through to the v1 path that v2 middleware would
  // re-route to `/h/personal/...`.
  const currentHubSlug = getHubSlugForPath(pathname);
  // Post-migration 018 dream settings live on the hub row — the
  // legacy user-prefs `dreams_enabled` is gone (stripped by the
  // backfill) and any stale copy is ignored by the engine. Read
  // from useHubDetail rather than useActiveHub because
  // useUpdateHubSettings (the mutation that flips this toggle)
  // invalidates the TanStack hub-detail cache but NOT the
  // AuthContext.hubs snapshot; reading from AuthContext would
  // keep showing the pre-toggle value until the next /auth/me
  // refresh.
  const { data: activeHubDetail } = useHubDetail(activeHub?.hub.id ?? null);
  const dreamsEnabled = activeHubDetail?.hub.settings?.dreams_enabled !== false;
  // Shell-v2 paints the page-background aurora via useAuroraOnRoot in
  // ShellLayoutV2, so the hub-header banner suppresses its own gradient
  // to avoid stacking the same colors at double saturation. v2 is the
  // only shell now (plan 24 phase 4b), so the banner is unconditionally
  // "none" — avatar, greeting, stats render on the page-bg aurora
  // directly.
  const hubHeaderAuroraMode = "none" as const;
  const isMobile = useIsMobile();
  const { toggle: toggleTree } = useTreePanel();

  useEffect(() => {
    if (!activeHub?.hub.id || !hubSummary) return;
    markHubVisit(activeHub.hub.id);
  }, [activeHub?.hub.id, hubSummary, markHubVisit]);

  const recentFilterStorageKey = `memax-recent-filters-${hubFilter ?? "all"}`;
  const pendingReviewCount = countPendingReviewNotifications(
    notificationListData?.notifications,
  );
  const displayHubSummary = useMemo(() => {
    if (!hubSummary) return null;
    return {
      ...hubSummary,
      stats: {
        ...hubSummary.stats,
        pending_review: pendingReviewCount,
      },
    };
  }, [hubSummary, pendingReviewCount]);

  // All hooks must be called before any early return (Rules of Hooks)
  const topics = useMemo(() => data?.topics ?? [], [data?.topics]);
  const topicPathLookup = useMemo(() => buildTopicPathLookup(topics), [topics]);

  // Pinned-tree desktop offset — recomputed here (also used in the
  // loaded JSX below) so the loading + error early returns can pass
  // it to TopicGridSkeleton + the centered error shell. Without
  // ShellLayoutV2 owns content padding via its rail+panel layout;
  // the legacy pinned-tree offset (`TREE_PANEL_CONTENT_OFFSET`) was
  // shell-v1 only. Plan 24 phase 4b retired v1, so the offset is
  // permanently zero. Kept as a named constant so the remaining JSX
  // sites still read clean (and so the property surface matches what
  // <TopicGridSkeleton> + <HubHeader> expect — they take an offset for
  // shape parity with future v3 chrome experiments).
  const desktopPinnedOffset = 0;

  // Render the compose entry-point alongside top-level loading and
  // error states under v2, so first-page-load and topics-failed users
  // still see a cell. Without this the cell only appears AFTER topics
  // resolve (codex 5.5 M-finding). v1 keeps its skeleton/error shape
  // unchanged — the wide CTA isn't load-bearing across these states.
  //
  // Inner cell-grid only (no max-w-4xl wrapper) — the consumer
  // skeleton / error already has the page-width container, so we just
  // hand it the responsive grid + cell.
  const composeSlot =
    currentHubSlug !== null ? (
      <div className="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <ComposePlusCell />
      </div>
    ) : null;

  if (isLoading) {
    // Pass the slot + hub header geometry props so the skeleton
    // shape mirrors the loaded shape — vertical (codex 5.5 P0k:
    // banner height 172/140) and horizontal (codex 5.5 P0k: pinned
    // tree offset). activeHub may not be loaded yet during very
    // first paint; falls back to a personal/desktop default.
    return (
      <TopicGridSkeleton
        composeSlot={composeSlot}
        hubType={activeHub?.hub.hub_type === "team" ? "team" : "personal"}
        auroraMode={hubHeaderAuroraMode}
        compact={isMobile}
        contentOffset={desktopPinnedOffset}
      />
    );
  }

  if (topicsError && !data) {
    return (
      <div className="animate-content-ready pb-36 md:pb-32">
        {/* Page width container shared with cell + error so they
            read as one column instead of cell-on-top + centered-
            elsewhere. Padding pulled in from `paddingTop: 24vh` to
            a smaller flex offset so the cell stays connected.
            Translate matches the loaded + skeleton paths so pinned-
            tree desktop users don't see a horizontal shift. */}
        <div
          className="mx-auto max-w-4xl px-5 sm:px-8"
          style={{
            transform: desktopPinnedOffset
              ? `translateX(${desktopPinnedOffset}px)`
              : undefined,
            transition: `transform ${FAST}s cubic-bezier(${EASE.join(", ")})`,
          }}
        >
          {composeSlot}
          <div
            className="flex items-center justify-center"
            style={{
              minHeight: isMobile ? MOBILE_CENTERED_HEIGHT : CENTERED_HEIGHT,
            }}
          >
            <div className="w-full max-w-md">
              <ContentError
                plain
                message={t.topics.loadFailed}
                detail={t.topics.loadFailedHint}
                retryLabel={t.states.error.retry}
                onRetry={() => {
                  void refetchTopics();
                }}
              />
            </div>
          </div>
        </div>
      </div>
    );
  }

  const unassigned = data?.unassigned_count ?? 0;
  const totalMemories = topics.reduce(
    (sum, t) => sum + t.total_memory_count,
    0,
  );
  // `desktopPinnedOffset` is computed above the loading/error early
  // returns so the skeleton + error shells use the same offset.

  return (
    <div className="animate-content-ready pb-36 md:pb-32">
      {activeHub && user && (
        <div>
          {hubSummaryLoading ? (
            <HubHeaderSkeleton
              hubType={activeHub.hub.hub_type === "team" ? "team" : "personal"}
              auroraMode={hubHeaderAuroraMode}
              compact={isMobile}
              contentOffset={desktopPinnedOffset}
            />
          ) : displayHubSummary ? (
            <HubHeader
              summary={displayHubSummary}
              viewerName={user.name}
              viewerDisplayName={user.display_name}
              viewerRole={activeHub.role}
              auroraMode={hubHeaderAuroraMode}
              compact={isMobile}
              contentOffset={desktopPinnedOffset}
            />
          ) : (
            <HubHeaderUnavailable
              hubId={activeHub.hub.id}
              hubName={activeHub.hub.name}
              hubIcon={activeHub.hub.icon}
              hubAccent={activeHub.hub.accent}
              hubType={activeHub.hub.hub_type === "team" ? "team" : "personal"}
              viewerName={user.name}
              viewerDisplayName={user.display_name}
              viewerRole={activeHub.role}
              stats={{
                memories: totalMemories + unassigned,
                topics: topics.length,
                inbox: unassigned,
                pendingReview: pendingReviewCount,
              }}
              dreamTopics={dreamReport?.run?.topics_restructured ?? 0}
              retryLabel={t.states.error.retry}
              auroraMode={hubHeaderAuroraMode}
              compact={isMobile}
              contentOffset={desktopPinnedOffset}
              onRetry={() => {
                void refetchHubSummary();
              }}
            />
          )}
        </div>
      )}

      <div
        style={{
          transform: desktopPinnedOffset
            ? `translateX(${desktopPinnedOffset}px)`
            : undefined,
          transition: `transform ${FAST}s cubic-bezier(${EASE.join(", ")})`,
        }}
      >
        <div className="mx-auto max-w-4xl px-5 sm:px-8">
          {/* Plan 18 — pinned super-notif region. Renders the founder
              welcome note + onboarding checklist above the seed
              memory cards. When the user has dismissed / resolved
              both rows, returns null and the page rhythm collapses
              to the pre-plan-18 layout.

              Hub scoping lives in the renderer, not here. Each
              notification's payload carries `pin_scope_hub_kind`
              (`""` = any | `personal` | `team`); PinnedNotifications
              reads the active hub via useActiveHub() and filters
              accordingly. Mounting unconditionally is intentional —
              the data drives visibility, not the mount site. */}
          <div className="mb-3">
            <PinnedNotifications context="memories_hero" />
          </div>

          {/* Pulse board (plan 25) — hub intelligence cards, below the
              header/onboarding, above compose + fresh memories. Zero
              height until the hub's producers have cards. */}
          <BoardSection />

          {/* ── Compose entry-point ──
              v1: wide CTA above the row list.
              v2: card-shape `+` cell above the card grid. The cell
                  was originally the leading cell of the grid (per
                  plan 20 RFC §3.1) but DataSectionCard's empty /
                  filtered-empty / error phases swap out the loaded
                  body, hiding the cell exactly when empty-state
                  users most need it (codex 5.5 finding). Hoisting
                  the cell above the section keeps it visible across
                  every phase. v1 keeps its wide CTA in the same
                  slot since v1 has no card-shape grid. */}
          {currentHubSlug === null ? (
            // v1 wide CTA above the row list — v1 has no card grid so
            // the standalone wide CTA stays the entry point. v2 path
            // moves the new-memory cell INTO the fresh-memories card
            // grid as the leading cell (see RecentSection's leadingCell
            // prop) per user feedback that the new-memory card should
            // sit within the grid, not as a separate surface above it.
            <div className="mb-3">
              <ComposePlusCard />
            </div>
          ) : null}

          {/* ── Recent section — visible on both desktop and mobile.
              Brain tab no longer hosts this feed (it shows only the
              ✦ memories badge and the bar mirror), so Topics/memory
              view carries recent browsing on every surface. ── */}
          <div className="mb-3" id="recent-section">
            <SelectionProvider>
              <RecentSection
                hubId={hubFilter}
                topicPathLookup={topicPathLookup}
                filterStorageKey={recentFilterStorageKey}
                showHeaderLabel
                t={t}
                interpolate={interpolate}
                // v2: render the new-memory CTA as the leading cell of
                // the fresh-memories card grid (plan 26 phase 2 follow-
                // up). v1 path passes nothing — its row list doesn't
                // host a card grid, so the standalone ComposePlusCard
                // above stays the entry point.
                leadingCell={
                  currentHubSlug !== null ? <ComposePlusCell /> : undefined
                }
                onClickMemory={(id) =>
                  router.push(buildMemoryDetailPath(currentHubSlug, id))
                }
              />
            </SelectionProvider>
          </div>

          {/* Topics section — always mounted so empty state is visible.
              Mobile renders an inline tree (plan 22 §3.7 + §5.2a) — the
              tiered card grid is desktop-only because cards waste
              vertical space on phones and bury topics below the fold.
              Desktop keeps the kitchen 29 large/medium/small tier. */}
          <div className="flex flex-col gap-3">
            {isMobile ? (
              <MobileInlineTree />
            ) : (
              <TieredTopicGrid
                topics={topics}
                isMobile={isMobile}
                hubId={hubFilter ?? "personal"}
                dreamsEnabled={dreamsEnabled}
                onToggleTree={toggleTree}
                onClickTopic={(id) =>
                  router.push(buildTopicPath(currentHubSlug, id), {
                    scroll: false,
                  })
                }
              />
            )}
            <TopicArchivedSection />
          </div>
        </div>
      </div>
    </div>
  );
}

/**
 * MobileInlineTree — plan 22 §3.7 inline topic tree for the mobile
 * memories overview. Reuses the same `<TopicTreeContent>` the desktop
 * sidebar paints (single source of truth for hub-root row, drag drop
 * targets, chevrons, create button) and the same expansion controller
 * (`useTopicTreeController`) so a topic the user expanded on desktop
 * stays expanded on mobile and vice versa.
 *
 * Wrapped in a glass card so the tree visually anchors as its own
 * section under the recent feed; the desktop sidebar gets that
 * affordance from the panel chrome (PinnedSecondaryPanel) — mobile
 * doesn't have a panel here so the card frame substitutes.
 */
function MobileInlineTree() {
  const { visibleExpandedIds, onToggleExpand, activeTopic } =
    useTopicTreeController();
  const { data: topicsData } = useTopics();
  const router = useRouter();
  const pathname = usePathname();
  const currentHubSlug = getHubSlugForPath(pathname);
  const [createTarget, setCreateTarget] = useState<TopicCreateTarget | null>(
    null,
  );
  return (
    <div className="rounded-2xl border border-border/40 bg-surface-1/40">
      <TopicTreeContent
        expandedIds={visibleExpandedIds}
        onToggleExpand={onToggleExpand}
        activeTopic={activeTopic}
        onCreateTopic={() => setCreateTarget({})}
        onCreateSubtopic={(parentId) =>
          setCreateTarget({
            parent: {
              id: parentId,
              name: findTopicName(topicsData?.topics, parentId) ?? "",
            },
          })
        }
      />
      <TopicCreateDialog
        target={createTarget}
        onClose={() => setCreateTarget(null)}
        onCreated={(topicId) =>
          router.push(buildTopicPath(currentHubSlug, topicId), {
            scroll: false,
          })
        }
      />
    </div>
  );
}

/* ── Tiered topic grid — large/medium/small from kitchen 29 ── */

function TieredTopicGrid({
  topics,
  isMobile,
  onClickTopic,
  hubId,
  dreamsEnabled,
  onToggleTree,
}: {
  topics: TopicTree[];
  isMobile: boolean;
  onClickTopic: (id: string) => void;
  hubId: string;
  dreamsEnabled: boolean;
  onToggleTree: () => void;
}) {
  const updateTopic = useUpdateTopic();
  const [pendingPinId, setPendingPinId] = useState<string | null>(null);
  const [mode, setMode] = useState<TopicViewMode>(() => {
    const stored = getStoredTopicViewMode(hubId, isMobile);
    return stored ?? resolveTopicViewModeDefault(topics.length, isMobile);
  });

  useEffect(() => {
    const stored = getStoredTopicViewMode(hubId, isMobile);
    setMode(stored ?? resolveTopicViewModeDefault(topics.length, isMobile));
  }, [hubId, topics.length, isMobile]);

  const handleChangeMode = useCallback(
    (nextMode: TopicViewMode) => {
      setMode(nextMode);
      setStoredTopicViewMode(hubId, isMobile, nextMode);
    },
    [hubId, isMobile],
  );

  const handleTogglePin = useCallback(
    (topic: TopicTree) => {
      setPendingPinId(topic.id);
      updateTopic.mutate(
        {
          id: topic.id,
          pinned: !topic.pinned,
        },
        {
          onSettled: () => {
            setPendingPinId((current) =>
              current === topic.id ? null : current,
            );
          },
        },
      );
    },
    [updateTopic],
  );

  return (
    <div className="flex flex-col gap-3">
      <TopicMainView
        topics={topics}
        mode={mode}
        dreamsEnabled={dreamsEnabled}
        onChangeMode={handleChangeMode}
        pendingPinId={pendingPinId}
        onClickTopic={onClickTopic}
        onTogglePin={handleTogglePin}
        onToggleTree={isMobile ? onToggleTree : undefined}
      />
    </div>
  );
}

/* ── Recent section — time-filtered, expandable, with export ── */

interface RecentActorOption {
  value: RecentActor;
  label: string;
  count: number;
}

export function RecentSection({
  hubId,
  topicPathLookup = new Map(),
  filterStorageKey,
  showHeaderLabel = true,
  t,
  interpolate,
  onClickMemory,
  leadingCell,
}: {
  hubId?: string;
  topicPathLookup?: Map<string, { id?: string; name: string; icon?: string }[]>;
  filterStorageKey: string;
  showHeaderLabel?: boolean;
  t: ReturnType<typeof useLocale>["t"];
  interpolate: ReturnType<typeof useInterpolate>;
  onClickMemory: (id: string) => void;
  /**
   * Optional leading cell rendered as the FIRST cell of the v2 card
   * grid (typically `<ComposePlusCell>`). Plan 26 phase 2 follow-up:
   * the new-memory CTA belongs inside the fresh-memories grid as a
   * peer cell, not as a standalone surface above it. Skipped on v1
   * (row layout has no card grid to host it).
   */
  leadingCell?: React.ReactNode;
}) {
  const readFilters = useCallback(() => {
    if (typeof globalThis.localStorage === "undefined") {
      return { window: "7d" as TimeWindow, actor: "all" as RecentActor };
    }
    try {
      const saved = localStorage.getItem(filterStorageKey);
      if (!saved) {
        return { window: "7d" as TimeWindow, actor: "all" as RecentActor };
      }
      const parsed = JSON.parse(saved) as {
        window?: TimeWindow;
        actor?: RecentActor;
      };
      return {
        window:
          parsed.window && TIME_WINDOWS.includes(parsed.window)
            ? parsed.window
            : ("7d" as TimeWindow),
        actor: parsed.actor ?? ("all" as RecentActor),
      };
    } catch {
      return { window: "7d" as TimeWindow, actor: "all" as RecentActor };
    }
  }, [filterStorageKey]);

  const [window, setWindow] = useState<TimeWindow>(() => readFilters().window);
  const [actor, setActor] = useState<RecentActor>(() => readFilters().actor);
  const [visibleCount, setVisibleCount] = useState(RECENT_PREVIEW_LIMIT);
  const [keepExpandedOnReset, setKeepExpandedOnReset] = useState(false);
  // View mode: cards (v2 default) vs rows (legacy / dense). Persisted
  // in localStorage so user preference survives reload. Plan 26
  // follow-up: user wants the row view brought back as an option.
  const viewModeStorageKey = `${filterStorageKey}.viewMode`;
  const [viewMode, setViewMode] = useState<"cards" | "rows">(() => {
    if (typeof globalThis.localStorage === "undefined") return "cards";
    try {
      const saved = localStorage.getItem(viewModeStorageKey);
      return saved === "rows" ? "rows" : "cards";
    } catch {
      return "cards";
    }
  });
  const setViewModePersisted = useCallback(
    (next: "cards" | "rows") => {
      setViewMode(next);
      try {
        localStorage.setItem(viewModeStorageKey, next);
      } catch {
        // localStorage unavailable — preference holds in-memory.
      }
    },
    [viewModeStorageKey],
  );

  useEffect(() => {
    if (typeof globalThis.localStorage === "undefined") return;
    const next = readFilters();
    setWindow((prev) => (prev === next.window ? prev : next.window));
    setActor((prev) => (prev === next.actor ? prev : next.actor));
  }, [readFilters]);

  // Rehydrate viewMode whenever the storage key changes (hub switch
  // reuses the same TopicGrid instance with a different
  // filterStorageKey). Without this, the prior hub's card/row
  // preference bleeds into the new hub until reload. Codex Medium
  // (commit bm17aa442 review).
  useEffect(() => {
    if (typeof globalThis.localStorage === "undefined") return;
    try {
      const saved = localStorage.getItem(viewModeStorageKey);
      const next: "cards" | "rows" = saved === "rows" ? "rows" : "cards";
      setViewMode((prev) => (prev === next ? prev : next));
    } catch {
      // localStorage unavailable — keep current.
    }
  }, [viewModeStorageKey]);

  useEffect(() => {
    const shouldExpand = consumeRecentExpandOnArrival(hubId);
    setKeepExpandedOnReset(shouldExpand);
    setVisibleCount(shouldExpand ? RECENT_PAGE_LIMIT : RECENT_PREVIEW_LIMIT);
  }, [hubId]);

  useEffect(() => {
    setVisibleCount(
      keepExpandedOnReset ? RECENT_PAGE_LIMIT : RECENT_PREVIEW_LIMIT,
    );
  }, [hubId, window, actor, keepExpandedOnReset]);

  const expanded = visibleCount > RECENT_PREVIEW_LIMIT;

  const {
    data: recentPages,
    fetchNextPage,
    hasNextPage = false,
    isFetchingNextPage,
    isError,
    isLoading,
    isPlaceholderData,
    refetch,
  } = useRecentMemories({
    hubId,
    window,
    actor,
    expanded,
  });

  const memories = flattenRecentMemories(recentPages);
  const total = recentMemoriesTotal(recentPages);
  const actorCounts = recentActorCounts(recentPages);
  const hasLoadedData = recentPages !== undefined;
  const filterActive = window !== "7d" || actor !== "all";

  const actorOptions = useMemo<RecentActorOption[]>(() => {
    const options: RecentActorOption[] = [
      {
        value: "all",
        label: t.memoryView.filterActorAll,
        count: total,
      },
    ];

    const entries = Object.entries(actorCounts) as Array<[RecentActor, number]>;
    for (const [value, count] of entries) {
      options.push({
        value,
        label: getRecentActorLabel(value, t),
        count,
      });
    }

    return options.sort((a, b) => {
      if (a.value === "all") return -1;
      if (b.value === "all") return 1;
      if (a.value === "self") return -1;
      if (b.value === "self") return 1;
      return a.label.localeCompare(b.label);
    });
  }, [actorCounts, t, total]);

  useEffect(() => {
    if (actor === "all") return;
    if (!actorOptions.some((option) => option.value === actor)) {
      setActor("all");
    }
  }, [actor, actorOptions]);

  const visible = memories.slice(0, visibleCount);
  const hasMore = total > RECENT_PREVIEW_LIMIT;
  const canLoadMore = visible.length < total;
  const nextIncrement = Math.min(5, Math.max(total - visible.length, 0));
  const recentlyArrived = useRecentArrivalIds();
  // Mobile fresh row collapses the topic pin to its leaf segment. The full
  // breadcrumb eats title space on a ~320px row, and the leaf is the most
  // information-dense answer to "where did this just land?". Desktop keeps
  // the collapsed multi-segment path handled by getCollapsedTopicLabel.
  const isMobile = useIsMobile();
  const selection = useSelection();
  const forget = useMemoryForget();
  const mover = useMemoryMove();
  const toast = useBarToast();
  const pathname = usePathname();
  // v2 routes carry the hub identity in the URL (`/h/<slug>/...`).
  // Cards land in v2 only — v1 keeps the existing row list. Cookie
  // dispatch already gates the chrome at the layout level; here we
  // detect the URL shape because some v2 surfaces will land at v1
  // path shapes (e.g., old `/memories/<id>` deep links rendered under
  // v2 chrome) and we want to leave those as rows.
  const isV2Surface = getHubSlugForPath(pathname) !== null;

  // Register the ordered universe of rendered memory ids with the
  // SelectionProvider. Drives Cmd/Ctrl+A (selectAll) and shift-click
  // range. Reconciles the selected set when visibility changes (items
  // dropped by filter / load-more) — see selection-context.tsx.
  const { setVisibleIds } = selection;
  useEffect(() => {
    setVisibleIds(visible.map((m) => m.id));
  }, [visible, setVisibleIds]);

  const persistFilters = useCallback(
    (nextWindow: TimeWindow, nextActor: RecentActor) => {
      if (typeof globalThis.localStorage === "undefined") return;
      localStorage.setItem(
        filterStorageKey,
        JSON.stringify({ window: nextWindow, actor: nextActor }),
      );
    },
    [filterStorageKey],
  );

  const switchWindow = useCallback(
    (w: TimeWindow) => {
      setWindow(w);
      persistFilters(w, actor);
    },
    [actor, persistFilters],
  );

  const switchActor = useCallback(
    (nextActor: RecentActor) => {
      const resolvedActor =
        nextActor === actor && nextActor !== "all" ? "all" : nextActor;
      setActor(resolvedActor);
      persistFilters(window, resolvedActor);
    },
    [actor, persistFilters, window],
  );

  const resetFilters = useCallback(() => {
    setWindow("7d");
    setActor("all");
    persistFilters("7d", "all");
  }, [persistFilters]);

  const handleLoadMore = useCallback(async () => {
    const target = visibleCount + 5;
    if (target > memories.length && hasNextPage && !isFetchingNextPage) {
      await fetchNextPage();
    }
    setVisibleCount((prev) => prev + 5);
  }, [
    visibleCount,
    memories.length,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  ]);

  const handleCollapse = useCallback(() => {
    setKeepExpandedOnReset(false);
    setVisibleCount(RECENT_PREVIEW_LIMIT);
  }, []);

  const phase =
    isError && !hasLoadedData
      ? "error"
      : isLoading && !hasLoadedData
        ? "loading"
        : memories.length > 0
          ? "loaded"
          : filterActive
            ? "filtered-empty"
            : "empty";

  // v2 cards mode collapses empty / filtered-empty / error into "loaded"
  // for DataSectionCard so the grid (and its leadingCell <ComposePlusCell>)
  // always renders. The non-loaded states render as a messaging row INSIDE
  // children below the grid — that way the new-memory CTA stays reachable
  // exactly when an empty/error user most needs it (this was the
  // codex 5.5 finding the original `composeSlot` hoisting was meant to
  // address; the leadingCell path quietly re-introduced the regression).
  // Loading still flows through DataSectionCard's skeleton path so the
  // skeleton → content transition is unchanged.
  const isV2Cards = isV2Surface && viewMode === "cards";
  const cardsAlwaysGrid =
    isV2Cards &&
    (phase === "empty" || phase === "filtered-empty" || phase === "error");
  const dataSectionPhase = cardsAlwaysGrid ? "loaded" : phase;

  return (
    <>
      <DataSectionCard
        variant="plain"
        icon={showHeaderLabel ? Clock : undefined}
        label={showHeaderLabel ? t.memoryView.freshMemory.other : undefined}
        phase={dataSectionPhase}
        isPlaceholderData={phase === "loaded" && isPlaceholderData}
        skeleton={
          isV2Surface ? (
            // Match the loaded count so the skeleton → content
            // transition has no row-count jump (5 cards loaded =
            // 5 skeleton cards). RECENT_PREVIEW_LIMIT is the truth
            // for both surfaces.
            <MemoryCardSkeletonList count={RECENT_PREVIEW_LIMIT} />
          ) : (
            <MemoryRowSkeletonList count={3} layout="stacked" />
          )
        }
        errorCopy={{
          title: t.memoryView.recentErrorTitle,
          detail: t.memoryView.recentErrorDetail,
          retryLabel: t.memoryView.recentErrorRetry,
          onRetry: () => {
            void refetch();
          },
        }}
        emptyCopy={{
          title: t.memoryView.recentEmptyTitle,
          hint: t.memoryView.recentEmptyHint,
        }}
        filteredEmptyCopy={{
          title: interpolate(t.memoryView.recentFilteredTitle, { window }),
          hint: t.memoryView.recentFilteredHint,
          clearLabel: t.memoryView.clearFilters,
          onClear: resetFilters,
        }}
        trailing={
          <>
            <Popover>
              <PopoverTrigger className="flex items-center gap-1.5 text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer ml-1">
                <SlidersHorizontal className="h-3 w-3" />
                <span>
                  {getRecentFilterSummary({ window, actor, interpolate, t })}
                </span>
                <ChevronDown className="h-2.5 w-2.5" />
              </PopoverTrigger>
              <PopoverContent side="bottom" align="start" sideOffset={4}>
                <RecentFilterPanel
                  windows={TIME_WINDOWS}
                  currentWindow={window}
                  currentActor={actor}
                  actorOptions={actorOptions}
                  onSelectWindow={switchWindow}
                  onSelectActor={switchActor}
                  onReset={resetFilters}
                  interpolate={interpolate}
                  t={t}
                />
              </PopoverContent>
            </Popover>
            {/* Select / Done toggle — hidden in v2 card mode because
                cards don't participate in SelectionContext (P0b judgment:
                cards are read-focused; batch select stays a row-mode
                affordance). Showing a toggle that does nothing visible
                in card mode would be a usability lie. */}
            {memories.length > 0 && (!isV2Surface || viewMode === "rows") && (
              <button
                onClick={
                  selection.active ? selection.exit : () => selection.enter()
                }
                className="ml-2 text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors"
              >
                {selection.active ? t.batch.done : t.batch.select}
              </button>
            )}
            {/* View-mode toggle — cards vs rows. v2 only (v1 always
                rows). Lets the user opt back into the dense row view
                when they want batch select / drag-to-topic / a more
                content-dense scan. Plan 26 follow-up. */}
            {isV2Surface && (
              <button
                type="button"
                onClick={() =>
                  setViewModePersisted(viewMode === "cards" ? "rows" : "cards")
                }
                aria-label={
                  viewMode === "cards"
                    ? t.memoryView.switchToRows
                    : t.memoryView.switchToCards
                }
                title={
                  viewMode === "cards"
                    ? t.memoryView.switchToRows
                    : t.memoryView.switchToCards
                }
                className="ml-2 flex items-center justify-center text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
              >
                {viewMode === "cards" ? (
                  <List className="h-3.5 w-3.5" strokeWidth={2} />
                ) : (
                  <LayoutGrid className="h-3.5 w-3.5" strokeWidth={2} />
                )}
              </button>
            )}
          </>
        }
      >
        <>
          {isV2Cards ? (
            // v2 card mode (default): 1 / 2 / 3 column glass card grid.
            // User can switch to row mode via the trailing toggle —
            // see viewMode state above. `leadingCell` is the
            // <ComposePlusCell> peer card and renders FIRST regardless
            // of phase (loaded / empty / filtered-empty / error) so the
            // new-memory CTA never disappears.
            <>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {leadingCell}
                {phase === "loaded" &&
                  visible.map((m) => (
                    <DraggableMemoryCard
                      key={getRecentRenderKey(m.id)}
                      memory={m}
                      topicLabel={(() => {
                        const path = m.topic_id
                          ? topicPathLookup.get(m.topic_id)
                          : undefined;
                        if (!path || path.length === 0) return undefined;
                        return {
                          path: isMobile ? [path[path.length - 1]] : path,
                        };
                      })()}
                      currentHubId={hubId}
                      isNew={recentlyArrived.has(m.id)}
                      onClick={() => onClickMemory(m.id)}
                    />
                  ))}
              </div>
              {/* State messaging rendered as a peer of the grid (not in
                  place of it) so the leadingCell stays visible. Mirrors
                  the copy DataSectionCard would have rendered for these
                  phases; we just shift the placement so the CTA isn't
                  swapped out. Loading is still owned by DataSectionCard
                  via the skeleton prop. */}
              {phase === "empty" && (
                <p className="mt-3 px-1 text-[13px] text-fg-3">
                  {t.memoryView.recentEmptyHint}
                </p>
              )}
              {phase === "filtered-empty" && (
                <div className="mt-3 px-1">
                  <p className="text-[13px] text-fg-3">
                    {interpolate(t.memoryView.recentFilteredTitle, { window })}
                  </p>
                  <button
                    onClick={resetFilters}
                    className="mt-1.5 cursor-pointer text-[12px] text-fg-3 transition-colors hover:text-fg-2"
                  >
                    {t.memoryView.clearFilters}
                  </button>
                </div>
              )}
              {phase === "error" && (
                <div className="mt-3 px-1">
                  <p className="text-[13px] text-fg-2">
                    {t.memoryView.recentErrorTitle}
                  </p>
                  <p className="mt-1 text-[12px] text-fg-3">
                    {t.memoryView.recentErrorDetail}
                  </p>
                  <button
                    onClick={() => {
                      void refetch();
                    }}
                    className="mt-1.5 cursor-pointer text-[12px] text-fg-3 transition-colors hover:text-fg-2"
                  >
                    {t.memoryView.recentErrorRetry}
                  </button>
                </div>
              )}
            </>
          ) : (
            <div>
              {visible.map((m, i) => (
                <DraggableMemoryRow
                  key={getRecentRenderKey(m.id)}
                  memory={m}
                  surface="recent"
                  topicLabel={(() => {
                    const path = m.topic_id
                      ? topicPathLookup.get(m.topic_id)
                      : undefined;
                    if (!path || path.length === 0) return undefined;
                    return {
                      path: isMobile ? [path[path.length - 1]] : path,
                    };
                  })()}
                  showDivider={i > 0}
                  isNew={recentlyArrived.has(m.id)}
                  onClick={() => onClickMemory(m.id)}
                />
              ))}
            </div>
          )}
          {(hasMore || expanded) && (
            <div className="flex items-center justify-center gap-4 border-t border-border/30 px-4 py-2.5">
              {expanded && (
                <button
                  onClick={handleCollapse}
                  className="text-[13px] text-fg-3 transition-colors hover:text-fg-2 cursor-pointer"
                >
                  {t.memoryView.collapseRecent}
                </button>
              )}
              {canLoadMore && (
                <button
                  onClick={() => void handleLoadMore()}
                  disabled={isFetchingNextPage}
                  className="text-[13px] text-fg-3 transition-colors hover:text-fg-2 cursor-pointer disabled:cursor-wait disabled:text-fg-4"
                >
                  {isFetchingNextPage
                    ? t.memoryView.loadingMore
                    : interpolate(t.memoryView.loadMoreRecent, {
                        n: String(nextIncrement),
                      })}
                </button>
              )}
            </div>
          )}
        </>
      </DataSectionCard>

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
          // card stays mounted on failure (partial-skip, permission
          // denied, infra error). Selection only clears on true.
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
          const mems = memories.filter((m) => ids.includes(m.id));
          const text = mems
            .map((m) => `## ${m.title}\n${m.content || m.summary || ""}`)
            .join("\n\n---\n\n");
          navigator.clipboard.writeText(
            `<memax-context>\n${text}\n</memax-context>`,
          );
          toast.success(t.batch.copied);
        }}
      />
    </>
  );
}

function getRecentActorLabel(
  actor: RecentActor,
  t: ReturnType<typeof useLocale>["t"],
) {
  if (actor === "all") return t.memoryView.filterActorAll;
  if (actor === "self") return t.memoryView.filterActorYou;
  if (actor.startsWith("agent:")) {
    const agentId = getAgentIdentity(actor.slice(6));
    return agentId?.displayName ?? actor.slice(6);
  }
  return actor.slice(7);
}

function getRecentFilterSummary({
  window,
  actor,
  interpolate,
  t,
}: {
  window: TimeWindow;
  actor: RecentActor;
  interpolate: ReturnType<typeof useInterpolate>;
  t: ReturnType<typeof useLocale>["t"];
}) {
  const timeLabel = interpolate(t.memoryView.filterPast, { window });
  if (actor === "all") return timeLabel;
  return `${timeLabel} · ${getRecentActorLabel(actor, t)}`;
}

function RecentFilterPanel({
  windows,
  currentWindow,
  currentActor,
  actorOptions,
  onSelectWindow,
  onSelectActor,
  onReset,
  interpolate,
  t,
}: {
  windows: readonly TimeWindow[];
  currentWindow: TimeWindow;
  currentActor: RecentActor;
  actorOptions: RecentActorOption[];
  onSelectWindow: (w: TimeWindow) => void;
  onSelectActor: (actor: RecentActor) => void;
  onReset: () => void;
  interpolate: ReturnType<typeof useInterpolate>;
  t: ReturnType<typeof useLocale>["t"];
}) {
  return (
    // Parent PopoverContent owns the glass material (via .glass-dropdown
    // + backdrop-blur-sm). Inner body is a bare layout container — no
    // second background/border/shadow, or the two materials stack muddily.
    <div className="w-[280px] overflow-hidden">
      <div className="border-b border-border/30 px-3 py-2.5">
        <p className="text-[12px] font-medium uppercase tracking-wide text-fg-3">
          {t.memoryView.filterBy}
        </p>
      </div>
      <div className="p-1.5">
        <p className="px-2.5 pb-1 pt-1 text-[11px] font-medium uppercase tracking-wide text-fg-4">
          {t.memoryView.filterTimeLabel}
        </p>
        {windows.map((windowOption) => (
          <button
            key={windowOption}
            onClick={() => onSelectWindow(windowOption)}
            className={`flex min-h-11 w-full items-center gap-2.5 rounded-chrome px-3 text-left text-[13px] transition-colors cursor-pointer ${
              windowOption === currentWindow
                ? "bg-foreground/[0.06] text-fg-1"
                : "text-fg-2 hover:bg-foreground/[0.04]"
            }`}
          >
            <span className="flex-1 truncate">
              {interpolate(t.memoryView.filterPast, { window: windowOption })}
            </span>
            {windowOption === currentWindow && (
              <Check className="h-3.5 w-3.5 shrink-0 text-fg-3" />
            )}
          </button>
        ))}
      </div>
      <div className="border-t border-border/30 p-1.5">
        <p className="px-2.5 pb-1 pt-1 text-[11px] font-medium uppercase tracking-wide text-fg-4">
          {t.memoryView.filterActorLabel}
        </p>
        <div className="max-h-56 overflow-y-auto">
          {actorOptions.map((option) => (
            <button
              key={option.value}
              onClick={() => onSelectActor(option.value)}
              className={`flex min-h-11 w-full items-center gap-2.5 rounded-chrome px-3 text-left text-[13px] transition-colors cursor-pointer ${
                option.value === currentActor
                  ? "bg-foreground/[0.06] text-fg-1"
                  : "text-fg-2 hover:bg-foreground/[0.04]"
              }`}
            >
              <span className="flex-1 truncate">{option.label}</span>
              <span className="shrink-0 text-[12px] tabular-nums text-fg-4">
                {option.count}
              </span>
              {option.value === currentActor && (
                <Check className="h-3.5 w-3.5 shrink-0 text-fg-3" />
              )}
            </button>
          ))}
        </div>
      </div>
      <div className="border-t border-border/30 px-3 py-2">
        <button
          onClick={onReset}
          className="text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
        >
          {t.memoryView.filterReset}
        </button>
      </div>
    </div>
  );
}

/**
 * Skeleton that mirrors the actual layout: hub header → fresh section
 * (borderless row-shaped) → topic grid (desktop modes A/B still card-shaped;
 * mobile mode C row-shaped on bare background) → inbox section (borderless).
 * Geometry matches the loaded row within ±2px so there is no visible
 * layout shift when data lands.
 */
function TopicGridSkeleton({
  composeSlot,
  hubType,
  auroraMode,
  compact,
  contentOffset,
}: {
  /**
   * Optional compose slot rendered AFTER the hub header skeleton and
   * BEFORE the recent section placeholder. Mirrors the v2 loaded
   * order (HubHeader → ComposePlusCell → Recent) so the skeleton →
   * loaded transition has no hierarchy inversion.
   */
  composeSlot?: React.ReactNode;
  /**
   * Hub header skeleton matches the LOADED hub-header geometry to
   * avoid a vertical content shift when topics resolve. Defaults
   * to a personal/desktop banner shape; the parent threads through
   * the actual values from `useActiveHub` + `useIsMobile` +
   * `resolveHubHeaderMode` so the skeleton lines up with whatever
   * the loaded chrome ends up rendering. The hand-rolled placeholder
   * (fixed 180px) used to drift mobile/team callers.
   */
  hubType?: "personal" | "team";
  auroraMode?: HubHeaderAuroraMode;
  compact?: boolean;
  contentOffset?: number;
}) {
  return (
    <div className="pb-36 md:pb-32">
      <HubHeaderSkeleton
        hubType={hubType ?? "personal"}
        auroraMode={auroraMode}
        compact={compact}
        contentOffset={contentOffset}
      />

      <div
        className="mx-auto max-w-4xl px-5 sm:px-8"
        // Match the loaded path's translateX so the cell + skeleton
        // content render at the same horizontal position as the
        // resolved cards. Without this, pinned-tree desktop users
        // saw a sideways shift on resolve.
        style={{
          transform: contentOffset
            ? `translateX(${contentOffset}px)`
            : undefined,
          transition: `transform ${FAST}s cubic-bezier(${EASE.join(", ")})`,
        }}
      >
        {composeSlot}

        {/* Fresh section placeholder — borderless, row-shaped */}
        <div className="mb-6">
          <div className="flex items-center gap-2 px-1 mb-2">
            <Skeleton className="h-3.5 w-3.5 rounded bg-foreground/[0.07]" />
            <Skeleton className="h-3.5 w-24 bg-foreground/[0.07]" />
          </div>
          <MemoryRowSkeletonList count={3} layout="stacked" />
        </div>

        {/* Topic grid placeholder — desktop modes A/B stay card-shaped;
            mobile mode C is borderless and row-shaped. */}
        <div className="hidden sm:grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <div
              key={i}
              className="rounded-surface p-4 space-y-2.5"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="flex items-center gap-2.5">
                <Skeleton className="h-4 w-4 rounded bg-foreground/[0.07]" />
                <Skeleton className="h-4 w-24 bg-foreground/[0.07]" />
              </div>
              <Skeleton className="h-3 w-16 bg-foreground/[0.07]" />
              <Skeleton className="h-3 w-full bg-foreground/[0.07]" />
              <Skeleton className="mt-2 h-3 w-12 bg-foreground/[0.07]" />
            </div>
          ))}
        </div>

        {/* Mobile mode C placeholder — borderless rows, full-bleed */}
        <div className="sm:hidden">
          <div className="flex items-center gap-2 px-1 mb-2">
            <Skeleton className="h-3.5 w-3.5 rounded bg-foreground/[0.07]" />
            <Skeleton className="h-3.5 w-16 bg-foreground/[0.07]" />
          </div>
          <TopicRowSkeletonList count={5} />
        </div>

        {/* Inbox section placeholder — borderless */}
        <div className="mt-6">
          <div className="flex items-center gap-2 px-1 mb-2">
            <Skeleton className="h-3.5 w-3.5 rounded bg-foreground/[0.07]" />
            <Skeleton className="h-3.5 w-16 bg-foreground/[0.07]" />
          </div>
          <MemoryRowSkeletonList
            count={2}
            layout="single"
            showSummary={false}
          />
        </div>
      </div>
    </div>
  );
}
