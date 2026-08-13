"use client";

import { useState, useRef, useEffect, useMemo, useCallback } from "react";
import { useLocale, useInterpolate } from "@/i18n";
import { useTopics, type TopicTree } from "@/hooks/use-topics";
import { useAuth, useActiveHub } from "@/lib/auth";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import { getHubDisplayName } from "@/lib/hub-display";
import { TopicIcon } from "@/components/features/topic/topic-icon";
import { cn } from "@memaxlabs/ui";
import { ArrowRight, Users, ChevronLeft, Check, Search } from "lucide-react";

/* ── Types ── */

interface DrillLevel {
  label: string;
  hubId?: string;
  items: TopicTree[];
}

/* ── Component ── */

interface DrillDownTreeProps {
  mode: "browse" | "select";
  onNavigate?: (topicId: string) => void;
  /**
   * Called when the user picks a destination in select mode.
   * `destinationName` is the display string the picker rendered for the
   * chosen row (topic name, or hub label for hub-only moves). Consumers
   * pass it to the mover hook so success toasts match exactly what the
   * user clicked.
   */
  onSelect?: (target: {
    topicId?: string;
    hubId?: string;
    destinationName?: string;
  }) => void;
  allowHubSelection?: boolean;
  excludeTopicId?: string;
  selectedTopicId?: string;
  onClose?: () => void;
  listHeight?: number | string;
}

/**
 * Unified drill-down tree — one component for both tree panel and move picker.
 *
 * Animation: CSS keyframes (globals.css), pure translateX, no opacity.
 * Same approach as the kitchen 33b/33f demos. Key change triggers
 * mount animation — no AnimatePresence, no exit animation, no layout issues.
 *
 * Data fetching: cross-hub topics are fetched BEFORE transitioning.
 */
export function DrillDownTree({
  mode,
  onNavigate,
  onSelect,
  allowHubSelection = true,
  excludeTopicId,
  selectedTopicId,
  onClose,
  listHeight,
}: DrillDownTreeProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { user, hubs, switchHub } = useAuth();
  const { activeHub } = useActiveHub();
  const isMobile = useIsMobile();
  const activeHubId = activeHub?.hub.id;
  const { data: activeTopicsData } = useTopics();
  const viewer = useMemo(
    () => ({
      name: user?.name,
      displayName: user?.display_name,
    }),
    [user?.display_name, user?.name],
  );

  const [stack, setStack] = useState<DrillLevel[]>([]);
  const [focusedIndex, setFocusedIndex] = useState(0);
  const [visualFocusIndex, setVisualFocusIndex] = useState<number | null>(null);
  const [direction, setDirection] = useState<"forward" | "back">("forward");
  const [fetchingHubId, setFetchingHubId] = useState<string | null>(null);
  const [showHubs, setShowHubs] = useState(false);
  const [query, setQuery] = useState("");

  const listRef = useRef<HTMLDivElement>(null);
  const keyboardNavigationRef = useRef(false);
  const hubRequestIdRef = useRef(0);

  // Always start at the active hub's topics — not the hub list.
  // Users are already browsing a hub; showing hubs first is an extra click.
  // Multi-hub users can go back to the hub list via back button.
  const multiHub = hubs.length > 1;
  useEffect(() => {
    if (showHubs) return;
    if (stack.length === 0 && activeTopicsData?.topics) {
      setStack([
        {
          label: activeHub
            ? getHubDisplayName(activeHub.hub, t, viewer)
            : t.topics.title,
          hubId: activeHubId,
          items: activeTopicsData.topics,
        },
      ]);
    }
  }, [
    showHubs,
    stack.length,
    activeTopicsData,
    activeHub,
    activeHubId,
    t,
    t.topics.title,
    viewer,
  ]);

  // If the picker mounted before active hub context was ready, backfill the
  // root hub id once it resolves so cross-hub topic moves always carry hub_id.
  useEffect(() => {
    if (!activeHubId) return;
    if (stack.length === 0) return;
    const root = stack[0];
    if (!root || root.hubId) return;
    setStack((prev) => {
      if (prev.length === 0) return prev;
      const first = prev[0];
      if (!first || first.hubId) return prev;
      return [{ ...first, hubId: activeHubId }, ...prev.slice(1)];
    });
  }, [activeHubId, stack]);

  const isAtHubs = stack.length === 0;
  const current = stack[stack.length - 1];
  const currentHubId = useMemo(
    () => stack.find((s) => s.hubId)?.hubId,
    [stack],
  );
  const currentHubLabel =
    currentHubId === activeHubId
      ? activeHub
        ? getHubDisplayName(activeHub.hub, t, viewer)
        : t.topics.title
      : hubs.find((hub) => hub.hub.id === currentHubId)
        ? getHubDisplayName(
            hubs.find((hub) => hub.hub.id === currentHubId)!.hub,
            t,
            viewer,
          )
        : (current?.label ?? t.topics.title);

  // Search flattens the WHOLE forest, not the level you happen to be
  // standing in. A drill-down that only filters its current level is
  // useless for the case that motivates search — a topic buried three
  // levels down whose path you don't remember. Matches therefore carry
  // their ancestry so a bare name like "hk" is still identifiable.
  const { searchMatches, pathById } = useMemo(() => {
    const matches: TopicTree[] = [];
    const paths = new Map<string, string>();
    const q = query.trim().toLowerCase();
    const walk = (nodes: readonly TopicTree[], trail: string[]) => {
      for (const node of nodes) {
        if (node.id === excludeTopicId) continue;
        if (q !== "" && node.name.toLowerCase().includes(q)) {
          matches.push(node);
          paths.set(node.id, trail.join(" / "));
        }
        if (node.children.length > 0)
          walk(node.children, [...trail, node.name]);
      }
    };
    if (q !== "") {
      // The hub level's items ARE the whole forest for this hub, so
      // search stays correct no matter how deep the user has drilled.
      walk(stack.find((level) => level.hubId)?.items ?? [], []);
    }
    return { searchMatches: matches, pathById: paths };
  }, [query, excludeTopicId, stack]);

  const isSearching = query.trim() !== "";

  const visibleItems = useMemo(() => {
    if (isAtHubs) return [];
    if (isSearching) return searchMatches;
    if (!current?.items) return [];
    return excludeTopicId
      ? current.items.filter((t) => t.id !== excludeTopicId)
      : current.items;
  }, [isAtHubs, isSearching, searchMatches, current, excludeTopicId]);

  const itemCount = isAtHubs ? hubs.length : visibleItems.length;

  // Reset the cursor when the visible set changes underneath it —
  // drilling a level, or narrowing the search. Keeping index 3 while
  // the list shrinks to two rows would leave the highlight nowhere.
  useEffect(() => {
    setFocusedIndex(0);
    // While searching, the cursor must be VISIBLE: Enter commits
    // visibleItems[focusedIndex], which is a real move. Leaving the
    // highlight null showed an unmarked list whose Enter silently
    // moved the memory into the first match.
    setVisualFocusIndex(query.trim() === "" ? null : 0);
  }, [stack.length, query]);

  useEffect(() => {
    if (!keyboardNavigationRef.current) return;
    if (focusedIndex < 0) return;
    const el = listRef.current?.querySelector(`[data-index="${focusedIndex}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [focusedIndex]);

  useEffect(() => {
    if (!keyboardNavigationRef.current) return;
    setVisualFocusIndex(focusedIndex);
  }, [focusedIndex]);

  /* ── Actions ── */

  const drillIntoHub = useCallback(
    async (hubId: string, hubName: string) => {
      const requestId = ++hubRequestIdRef.current;
      setDirection("forward");
      setShowHubs(false);
      if (hubId === activeHubId && activeTopicsData?.topics) {
        setStack([{ label: hubName, hubId, items: activeTopicsData.topics }]);
      } else {
        setFetchingHubId(hubId);
        setStack([{ label: hubName, hubId, items: [] }]);
        try {
          const data = await queryClient.fetchQuery({
            queryKey: ["topics", hubId],
            queryFn: () => getMemaxClient().topics.list(hubId),
          });
          if (hubRequestIdRef.current !== requestId) return;
          setStack([{ label: hubName, hubId, items: data.topics }]);
        } finally {
          if (hubRequestIdRef.current === requestId) {
            setFetchingHubId(null);
          }
        }
      }
    },
    [activeHubId, activeTopicsData],
  );

  const drillIntoChildren = useCallback((topic: TopicTree) => {
    if (topic.children.length === 0) return;
    setDirection("forward");
    // Drilling from a search result means "show me inside this one", so
    // the query is spent — leaving it on would keep the flat matches
    // rendered and the new level would never appear.
    setQuery("");
    setStack((s) => [...s, { label: topic.name, items: topic.children }]);
  }, []);

  const selectTopic = useCallback(
    (topic: TopicTree) => {
      const targetHubId = currentHubId ?? activeHubId;
      if (!targetHubId) return;
      onSelect?.({
        topicId: topic.id,
        hubId: targetHubId,
        destinationName: topic.name,
      });
    },
    [onSelect, currentHubId, activeHubId],
  );

  const navigateToTopic = useCallback(
    async (topicId: string) => {
      if (currentHubId && activeHubId && currentHubId !== activeHubId) {
        switchHub(currentHubId);
      }
      onNavigate?.(topicId);
    },
    [activeHubId, currentHubId, onNavigate, switchHub],
  );

  const handleTopic = useCallback(
    (topic: TopicTree) => {
      if (mode === "select") {
        selectTopic(topic);
        return;
      }
      if (onNavigate) {
        void navigateToTopic(topic.id);
        return;
      }
      if (topic.children.length > 0) {
        drillIntoChildren(topic);
      }
    },
    [mode, selectTopic, drillIntoChildren, onNavigate, navigateToTopic],
  );

  const goBack = useCallback(() => {
    if (!multiHub && stack.length <= 1) return;
    hubRequestIdRef.current += 1;
    setDirection("back");
    if (multiHub && stack.length === 1) {
      setShowHubs(true);
      setFetchingHubId(null);
      setStack([]);
      return;
    }
    setStack((s) => s.slice(0, -1));
  }, [multiHub, stack.length]);

  // Can go back: always for deeper levels, hub level only if multi-hub
  const canGoBack = stack.length > 1 || (multiHub && stack.length === 1);

  /* ── Render ── */

  // Key forces remount → CSS animation plays on mount
  const animKey = stack.map((s) => s.label).join("/") || "hubs";
  const animClass =
    direction === "forward" ? "animate-drill-forward" : "animate-drill-back";

  useEffect(() => {
    const id = requestAnimationFrame(() => {
      listRef.current?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(id);
  }, [showHubs, animKey]);

  const handlePointerEnter = useCallback(
    (event: React.PointerEvent<HTMLElement>, index: number) => {
      if (event.pointerType === "touch") return;
      keyboardNavigationRef.current = false;
      setFocusedIndex(index);
      setVisualFocusIndex(index);
    },
    [],
  );

  const handlePointerLeave = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      if (event.pointerType === "touch") return;
      setVisualFocusIndex(null);
    },
    [],
  );

  const setKeyboardFocusIndex = useCallback((nextIndex: number) => {
    setFocusedIndex(nextIndex);
    setVisualFocusIndex(nextIndex);
  }, []);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      keyboardNavigationRef.current = true;
      switch (e.key) {
        case "ArrowDown":
          e.preventDefault();
          setKeyboardFocusIndex(Math.min(focusedIndex + 1, itemCount - 1));
          break;
        case "ArrowUp":
          e.preventDefault();
          setKeyboardFocusIndex(Math.max(focusedIndex - 1, 0));
          break;
        case "Home":
          e.preventDefault();
          setKeyboardFocusIndex(0);
          break;
        case "End":
          e.preventDefault();
          setKeyboardFocusIndex(Math.max(itemCount - 1, 0));
          break;
        case "Enter": {
          e.preventDefault();
          if (isAtHubs && hubs[focusedIndex]) {
            const hub = hubs[focusedIndex];
            const hubLabel = getHubDisplayName(hub.hub, t, viewer);
            if (mode === "select" && allowHubSelection) {
              onSelect?.({ hubId: hub.hub.id, destinationName: hubLabel });
            } else {
              drillIntoHub(hub.hub.id, hubLabel);
            }
          } else if (!isAtHubs && visibleItems[focusedIndex]) {
            handleTopic(visibleItems[focusedIndex]);
          }
          break;
        }
        case "ArrowRight":
          if (!isAtHubs && visibleItems[focusedIndex]?.children.length) {
            e.preventDefault();
            drillIntoChildren(visibleItems[focusedIndex]);
          }
          break;
        case "Escape":
          e.preventDefault();
          // Stop where it is consumed. Hosts listen for Escape too —
          // the batch toolbar on its shell, base-ui's popover dismiss,
          // the mobile modal on window — and none of them check
          // defaultPrevented. Without this, one Escape both went back a
          // level AND tore down the whole picker, so "back" was never
          // observable outside jsdom. (batch-toolbar's comment already
          // documented this contract; it just wasn't true yet.)
          e.stopPropagation();
          if (canGoBack) goBack();
          else onClose?.();
          break;
        case "Backspace":
          if (canGoBack) {
            e.preventDefault();
            goBack();
          }
          break;
      }
    },
    [
      itemCount,
      focusedIndex,
      isAtHubs,
      hubs,
      setKeyboardFocusIndex,
      t,
      viewer,
      mode,
      allowHubSelection,
      onSelect,
      visibleItems,
      handleTopic,
      drillIntoHub,
      drillIntoChildren,
      canGoBack,
      goBack,
      onClose,
    ],
  );

  const describeSelectTopic = useCallback(
    (topic: TopicTree) => {
      const directMemories = topic.memory_count ?? 0;
      const subtopicCount = topic.children.length;
      if (directMemories > 0 && subtopicCount > 0) {
        return interpolate(t.topics.movePickerTopicWithChildren, {
          memories: String(directMemories),
          subtopics: String(subtopicCount),
        });
      }
      if (directMemories > 0) {
        return interpolate(t.topics.movePickerTopicOnly, {
          memories: String(directMemories),
        });
      }
      if (subtopicCount > 0) {
        return interpolate(t.topics.movePickerSubtopicsOnly, {
          subtopics: String(subtopicCount),
        });
      }
      return t.topics.movePickerEmpty;
    },
    [interpolate, t.topics],
  );

  return (
    // flex column + min-h-0 so the list below claims exactly the space
    // the back button and any wrapper chrome leave behind. The old
    // layout gave the list a fixed pixel height that call sites had to
    // hand-compute against the popover cap; the back button only exists
    // once you drill in, so every call site's arithmetic was short by
    // its 44px and the last row was clipped outside the popover.
    <div onKeyDown={handleKeyDown} className="flex min-h-0 flex-col">
      {/* Back button */}
      {canGoBack && (
        <button
          type="button"
          onClick={goBack}
          className={cn(
            "w-full flex items-center gap-2 min-h-11 px-4 text-[14px] text-fg-2 cursor-pointer transition-colors border-b border-border/30",
            !isMobile && "hover:bg-surface-1",
          )}
        >
          <ChevronLeft className="h-3.5 w-3.5 text-fg-3" />
          <span className="font-medium truncate">{current.label}</span>
        </button>
      )}

      {/* Search — above the back button because it is level-agnostic:
          back moves within the hierarchy, search ignores it entirely.
          Only in select mode; browse mode is the mobile tree, where the
          page's own search already covers this. */}
      {mode === "select" && !isAtHubs && (
        <div className="flex shrink-0 items-center gap-2 border-b border-border/30 px-3 py-2">
          <Search className="h-3.5 w-3.5 shrink-0 text-fg-4" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t.topics.searchTopicsPlaceholder}
            aria-label={t.topics.searchTopicsPlaceholder}
            className="min-w-0 flex-1 bg-transparent text-[13px] text-fg-1 placeholder:text-fg-4 focus:outline-none"
            onKeyDown={(e) => {
              // The list owns ↑↓/Enter — preventDefault stops the caret
              // from moving but lets the event bubble to handleKeyDown,
              // so typing and navigating share one keyboard model.
              if (e.key === "ArrowUp" || e.key === "ArrowDown") {
                e.preventDefault();
                return;
              }
              // Text-editing keys belong to the input, full stop. The
              // list treats Backspace as "go back a level" and Home/End
              // as "jump to first/last row", so without this a typo was
              // literally uncorrectable: Backspace threw you out to the
              // hub list with the bad query still in the box.
              if (
                e.key === "Backspace" ||
                e.key === "Home" ||
                e.key === "End"
              ) {
                e.stopPropagation();
                return;
              }
              // Escape is two-stage: clear the query first, and only a
              // second press leaves the level. Otherwise a typo would
              // throw away the user's place in the tree.
              if (e.key === "Escape" && query !== "") {
                e.preventDefault();
                e.stopPropagation();
                setQuery("");
              }
            }}
          />
        </div>
      )}

      {/* List — CSS animation on key change, no framer-motion */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <div
          key={animKey}
          ref={listRef}
          className={cn("min-h-0 flex-1 overflow-y-auto p-1.5", animClass)}
          // listHeight is a CAP, never a fixed size: a short list stays
          // short instead of rendering a tall empty box, and a long one
          // stops at the cap with its own scrollbar.
          style={listHeight ? { maxHeight: listHeight } : undefined}
          role="listbox"
          tabIndex={0}
          aria-label={
            isAtHubs
              ? t.hubs.hubsListLabel
              : (current?.label ?? t.topics.topicLabel)
          }
        >
          {/* Hub list */}
          {isAtHubs &&
            hubs.map((h, i) => {
              const isFetching = fetchingHubId === h.hub.id;
              const isVisuallyFocused = visualFocusIndex === i;
              const hubLabel = getHubDisplayName(h.hub, t, viewer);
              const rowClass = cn(
                "touch-no-hover w-full flex items-center rounded-surface text-[14px] transition-colors",
                isFetching || isVisuallyFocused
                  ? "bg-foreground/[0.06] text-fg-1"
                  : cn("text-fg-2", !isMobile && "hover:bg-foreground/[0.04]"),
              );

              if (mode === "select" && allowHubSelection) {
                return (
                  <div
                    key={h.hub.id}
                    data-index={i}
                    role="option"
                    aria-selected={isVisuallyFocused}
                    onPointerEnter={(event) => handlePointerEnter(event, i)}
                    onPointerLeave={handlePointerLeave}
                    className={rowClass}
                  >
                    <button
                      type="button"
                      onClick={() =>
                        onSelect?.({
                          hubId: h.hub.id,
                          destinationName: hubLabel,
                        })
                      }
                      className="flex min-h-11 min-w-0 flex-1 items-center gap-2.5 px-3 cursor-pointer text-left"
                    >
                      <Users className="h-4 w-4 text-fg-3" />
                      <span className="flex-1 truncate font-medium">
                        {hubLabel}
                      </span>
                    </button>
                    <button
                      type="button"
                      onClick={() => drillIntoHub(h.hub.id, hubLabel)}
                      disabled={!!fetchingHubId}
                      className={cn(
                        "touch-no-hover flex min-h-11 w-10 items-center justify-center cursor-pointer text-fg-4 transition-colors disabled:cursor-wait",
                        !isMobile && "hover:text-fg-2",
                      )}
                      aria-label={hubLabel}
                    >
                      <ArrowRight
                        className={cn(
                          "h-3 w-3 shrink-0",
                          isFetching && "animate-pulse",
                        )}
                      />
                    </button>
                  </div>
                );
              }

              return (
                <button
                  type="button"
                  key={h.hub.id}
                  data-index={i}
                  onClick={() => drillIntoHub(h.hub.id, hubLabel)}
                  role="option"
                  aria-selected={isVisuallyFocused}
                  onPointerEnter={(event) => handlePointerEnter(event, i)}
                  onPointerLeave={handlePointerLeave}
                  disabled={!!fetchingHubId}
                  className={cn(
                    rowClass,
                    "min-h-11 px-3 cursor-pointer gap-2.5",
                  )}
                >
                  <Users className="h-4 w-4 text-fg-3" />
                  <span className="flex-1 text-left font-medium truncate">
                    {hubLabel}
                  </span>
                  <ArrowRight
                    className={cn(
                      "h-3 w-3 text-fg-4 shrink-0",
                      isFetching && "animate-pulse",
                    )}
                  />
                </button>
              );
            })}

          {/* Topics */}
          {!isAtHubs &&
            visibleItems.map((topic, i) => {
              const hasChildren = topic.children.length > 0;
              const isSelected = selectedTopicId === topic.id;
              const isVisuallyFocused = visualFocusIndex === i;
              const rowClass = cn(
                "touch-no-hover w-full rounded-surface text-[14px] transition-colors",
                isVisuallyFocused
                  ? "bg-foreground/[0.06] text-fg-1"
                  : cn("text-fg-2", !isMobile && "hover:bg-foreground/[0.04]"),
              );

              if (mode === "select") {
                return (
                  <div
                    key={topic.id}
                    data-index={i}
                    role="option"
                    aria-selected={isVisuallyFocused}
                    onPointerEnter={(event) => handlePointerEnter(event, i)}
                    onPointerLeave={handlePointerLeave}
                    className={cn(rowClass, "flex items-center")}
                  >
                    <button
                      type="button"
                      onClick={() => selectTopic(topic)}
                      className="flex min-h-12 min-w-0 flex-1 items-center gap-2.5 px-3 py-2 cursor-pointer text-left"
                      aria-current={isSelected ? "true" : undefined}
                    >
                      <TopicIcon
                        name={topic.icon}
                        className="h-4 w-4 text-fg-3"
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{topic.name}</span>
                        <span className="mt-0.5 block truncate text-[12px] text-fg-4">
                          {isSearching
                            ? // A match is shown out of context, so its
                              // ancestry replaces the counts — "hk" alone
                              // says nothing about WHICH hk you're picking.
                              [currentHubLabel, pathById.get(topic.id)]
                                .filter(Boolean)
                                .join(" / ")
                            : describeSelectTopic(topic)}
                        </span>
                      </span>
                      {isSelected && (
                        <Check className="h-3.5 w-3.5 shrink-0 text-fg-3" />
                      )}
                    </button>
                    {hasChildren && (
                      <button
                        type="button"
                        onClick={() => drillIntoChildren(topic)}
                        className={cn(
                          "touch-no-hover flex min-h-12 w-11 items-center justify-center self-stretch border-l border-border/30 cursor-pointer text-fg-4 transition-colors",
                          !isMobile &&
                            "hover:bg-foreground/[0.03] hover:text-fg-2",
                        )}
                        aria-label={interpolate(
                          t.topics.movePickerBrowseSubtopics,
                          {
                            name: topic.name,
                          },
                        )}
                        title={t.topics.movePickerBrowseAction}
                      >
                        <ArrowRight className="h-3 w-3 shrink-0" />
                      </button>
                    )}
                  </div>
                );
              }

              return (
                <div
                  key={topic.id}
                  data-index={i}
                  role="option"
                  aria-selected={isVisuallyFocused}
                  onPointerEnter={(event) => handlePointerEnter(event, i)}
                  onPointerLeave={handlePointerLeave}
                  className={cn(rowClass, "flex items-center")}
                >
                  <button
                    type="button"
                    onClick={() => handleTopic(topic)}
                    className="flex min-h-11 min-w-0 flex-1 items-center gap-2.5 px-3 cursor-pointer text-left"
                  >
                    <TopicIcon
                      name={topic.icon}
                      className="h-4 w-4 text-fg-3"
                    />
                    <span className="flex-1 truncate">{topic.name}</span>
                    <span className="text-[13px] text-fg-4 tabular-nums">
                      {topic.total_memory_count ?? 0}
                    </span>
                  </button>
                  {hasChildren && (
                    <button
                      type="button"
                      onClick={() => drillIntoChildren(topic)}
                      className={cn(
                        "flex min-h-11 w-10 items-center justify-center cursor-pointer text-fg-4 transition-colors",
                        !isMobile && "hover:text-fg-2",
                      )}
                      aria-label={topic.name}
                    >
                      <ArrowRight className="h-3 w-3 shrink-0" />
                    </button>
                  )}
                </div>
              );
            })}

          {/* Empty */}
          {itemCount === 0 &&
            (fetchingHubId === currentHubId ? (
              <div className="flex items-center gap-2 px-3 py-3 text-[13px] text-fg-3">
                <span
                  className="h-1.5 w-1.5 rounded-full state-slow-breathe"
                  style={{ background: "var(--signature)" }}
                />
                <span>{t.topics.loadingHubTopics}</span>
              </div>
            ) : (
              <div className="px-3 py-3">
                <p className="text-[13px] text-fg-3">{t.topics.noTopics}</p>
                {mode === "select" &&
                  allowHubSelection &&
                  currentHubId &&
                  currentHubLabel && (
                    <button
                      type="button"
                      onClick={() =>
                        onSelect?.({
                          hubId: currentHubId,
                          destinationName: currentHubLabel,
                        })
                      }
                      className={cn(
                        "mt-2 inline-flex min-h-11 items-center rounded-chrome border border-border/40 px-3 text-[13px] text-fg-2 transition-colors",
                        !isMobile && "hover:bg-surface-1 hover:text-fg-1",
                      )}
                    >
                      {interpolate(t.topics.moveToHub, {
                        name: currentHubLabel,
                      })}
                    </button>
                  )}
              </div>
            ))}
        </div>
      </div>
    </div>
  );
}
