"use client";

import { useMemo } from "react";
import { useRouter, usePathname } from "next/navigation";
import { buildMemoriesPath, getHubSlugForPath } from "@/lib/route-helpers";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { Layout, Plus } from "lucide-react";
import { useDroppable } from "@dnd-kit/core";
import { useTopics } from "@/hooks/use-topics";
import { useSettingsDialog } from "@/contexts/settings-dialog-context";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { useAuth } from "@/lib/auth";
import { useHubDetail, useHubSummary } from "@/hooks/use-hub-management";
import { useLocale } from "@/i18n";
import { DREAM_PURPLE } from "@memaxlabs/ui/tokens/dream";
import { TopicTreeNode } from "./topic-tree-node";
import { HubBadge } from "../hub/hub-badge";
import { ContentError, Skeleton } from "@memaxlabs/ui";
import type { TopicTree } from "@/hooks/use-topics";
import { HUB_ROOT_DROP_ID, useTopicDragContext } from "./topic-dnd-hooks";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";

interface TreeContentProps {
  expandedIds: Set<string>;
  onToggleExpand: (id: string) => void;
  /** Transient drag-scoped expansion (spring-load) — see controller. */
  onSpringExpand: (id: string) => void;
  activeTopic?: string;
  onCreateTopic?: () => void;
  onCreateSubtopic?: (parentId: string) => void;
}

export function TopicTreeContent({
  expandedIds,
  onToggleExpand,
  onSpringExpand,
  activeTopic,
  onCreateTopic,
  onCreateSubtopic,
}: TreeContentProps) {
  const { data, isLoading, isError, refetch } = useTopics();
  const settingsDialog = useSettingsDialog();
  const isMobile = useIsMobile();
  const { t } = useLocale();
  const { activeHubId } = useAuth();
  // Dream enablement is now per-hub (migration 018 moved it out of
  // user_preferences). Read from the active hub's settings. Missing
  // hubId or missing settings row → default true, matching the
  // server's DefaultSettings() floor.
  const { data: activeHub } = useHubDetail(activeHubId ?? null);
  const dreamsEnabled = activeHub?.hub.settings?.dreams_enabled !== false;

  if (isLoading) {
    return (
      <div className="space-y-2 p-3">
        {[1, 2, 3, 4, 5].map((i) => (
          <Skeleton key={i} className="h-7 rounded-lg" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div className="p-3">
        <ContentError
          plain
          message={t.topics.treeFailed}
          detail={t.topics.treeFailedHint}
          retryLabel={t.states.error.retry}
          onRetry={() => {
            void refetch();
          }}
        />
      </div>
    );
  }

  const topics = data?.topics ?? [];

  return (
    <div className="flex flex-col h-full">
      {/* Tree */}
      {/* pb-20 clears the floating bar (80px) so the last items are reachable.
          pl-3 reserves a 12px left gutter for absolute-positioned row grips
          (TopicTreeNode) so depth-0 topic rows land ~2px right of the hub
          row instead of the old 24px drift. Smallest gutter that gives the
          grip a clean 4px breathing gap before the chevron — any smaller and
          grip overlaps content; any larger and the hub row shifts further
          right without gain. */}
      <div className="flex-1 overflow-y-auto pt-2 pb-20 pl-3 pr-1">
        {/* Hub-root row — always visible as a structural header for the
            tree (identifies which hub you're browsing) AND becomes an
            active drop target during topic/memory drags. Decorative when
            not dragging (no click handler, no navigation). */}
        <HubRootRow />
        {topics.length === 0 ? (
          <div className="flex flex-col items-center text-center py-8 px-4 gap-2">
            {dreamsEnabled ? (
              <>
                <span
                  className="text-[14px]"
                  style={{ color: "var(--signature)" }}
                >
                  ✦
                </span>
                <p className="text-[14px] text-fg-2">{t.topics.organizing}</p>
                <p className="text-[13px] text-fg-3 leading-relaxed">
                  {t.topics.organizingHint}
                </p>
              </>
            ) : (
              <>
                <span style={{ color: DREAM_PURPLE, fontSize: 15 }}>✦</span>
                <p className="text-[14px] text-fg-2">{t.topics.dreamsOff}</p>
                <p className="text-[13px] text-fg-3 leading-relaxed">
                  {t.topics.dreamsOffHint}
                </p>
                <button
                  onClick={() =>
                    settingsDialog.open(
                      activeHubId
                        ? {
                            kind: "hub",
                            hubId: activeHubId,
                            section: "intelligence",
                          }
                        : { kind: "you", section: "profile" },
                    )
                  }
                  className="text-[13px] cursor-pointer transition-colors hover:opacity-80 underline mt-1"
                  style={{ color: DREAM_PURPLE }}
                >
                  {t.topics.dreamsOffEnable}
                </button>
              </>
            )}
          </div>
        ) : (
          topics.map((topic: TopicTree) => (
            <TopicTreeNode
              key={topic.id}
              topic={topic}
              depth={0}
              isMobile={isMobile}
              expandedIds={expandedIds}
              onToggleExpand={onToggleExpand}
              onSpringExpand={onSpringExpand}
              activeTopic={activeTopic}
              onCreateSubtopic={onCreateSubtopic}
            />
          ))
        )}
      </div>

      {/* end tree scroll area */}

      {/* Footer: create */}
      <div className="px-3 py-2.5">
        {onCreateTopic && (
          <button
            onClick={onCreateTopic}
            className={
              isMobile
                ? "flex items-center gap-1.5 w-full px-2 py-1.5 rounded-lg text-[14px] text-fg-3 transition-colors cursor-pointer"
                : "flex items-center gap-1.5 w-full px-2 py-1.5 rounded-lg text-[14px] text-fg-3 hover:text-fg-2 hover:bg-foreground/[0.03] transition-colors cursor-pointer"
            }
          >
            <Plus className="h-3 w-3" />
            {t.topics.create}
          </button>
        )}
      </div>
    </div>
  );
}

/**
 * HubRootRow — "Overview" entry per kitchen 43-shell-v2 spec.
 *
 * One-line button with a Layout icon + "Overview" label. Active state
 * is NEUTRAL (surface-3 bg + foreground text) per the design language
 * — signature/purple is reserved for AI/dream moments, not for
 * structural nav.
 *
 * Triple role:
 *   - Click navigates to the hub's memories overview route
 *     (/h/<slug>/memories or /memories on v1)
 *   - Drop target for memory/topic drags (HUB_ROOT_DROP_ID stays wired)
 *   - Visual "you are here" anchor when the user is on the overview
 *     route — active state highlights the row
 *
 * Plan 26 follow-up: the previous `[HubBadge] [hub name]` rendering
 * repeated the hub identity already shown in the panel's header chip,
 * which the user flagged as "repetitive hub icon + name". Replaced
 * with the kitchen 43 overview pattern.
 */
function HubRootRow() {
  const router = useRouter();
  const pathname = usePathname();
  const { t } = useLocale();
  const { activeType, dropTargetsActive } = useTopicDragContext();
  const { setNodeRef, isOver } = useDroppable({
    id: HUB_ROOT_DROP_ID,
    data: { topicName: "", type: "topic" as const },
    disabled: !dropTargetsActive,
  });
  const { activeHubId, hubs, user } = useAuth();
  // Only consume hubSummary as a side query — the row doesn't render
  // stats anymore (kitchen 43 keeps Overview as a clean nav row), but
  // having the query warm here means hub stat consumers below the fold
  // (footer "N unassigned memories") share one cache entry.
  void useHubSummary(activeHubId ?? null);

  if (!activeHubId) return null;
  const isActiveDrag = activeType === "topic" || activeType === "memory";
  const isOverview =
    pathname === "/memories" || /^\/h\/[^/]+\/memories\/?$/.test(pathname);
  // Same resolution as bar-context.goMemory + topic-breadcrumb.
  // pathname slug wins; active-hub fallback covers any future hub-less
  // surface that mounts the tree; v1 fallback last.
  const pathSlug = getHubSlugForPath(pathname);
  const activeHubEntry = hubs.find((h) => h.hub.id === activeHubId) ?? null;
  const activeSlug =
    activeHubEntry && user ? hubRouteSlug(activeHubEntry.hub, user.id) : null;
  const memoriesPath = buildMemoriesPath(pathSlug ?? activeSlug);

  return (
    <div className="px-2 mb-1">
      <button
        ref={setNodeRef}
        type="button"
        onClick={() => router.push(memoriesPath, { scroll: false })}
        // hover:bg-surface-2 only when NOT active (active inline bg
        // wins via specificity but we keep both selectors clean).
        // Matches the topic rows below so Overview reads as a peer
        // in the nav stack, not a special button. Plan 26 follow-up.
        className={`flex h-8 w-full items-center gap-2 rounded-lg px-2 text-left transition-colors cursor-pointer ${
          isOverview ? "" : "hover:bg-surface-2"
        } ${
          isActiveDrag
            ? isOver
              ? "border border-dashed border-foreground/40 bg-foreground/4"
              : "border border-dashed border-border/40"
            : ""
        }`}
        style={{
          background:
            isOverview && !isActiveDrag ? "var(--surface-3)" : undefined,
          color: isOverview ? "var(--foreground)" : "var(--fg-2)",
        }}
        aria-current={isOverview ? "page" : undefined}
        aria-label={t.nav.overview}
      >
        <Layout className="h-4 w-4 shrink-0" strokeWidth={2} />
        <span className="text-[13px] font-medium">{t.nav.overview}</span>
      </button>
    </div>
  );
}
