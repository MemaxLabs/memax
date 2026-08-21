"use client";

/**
 * useTopicTreeController — controller logic for the topic tree.
 *
 * Owns the four pieces of state that drive `<TopicTreeContent>`:
 *
 *   1. `expandedIds`            — persistent expansion preferences, mirrored
 *                                 to localStorage so reloads preserve which
 *                                 nodes the user opened. Modified only by
 *                                 explicit click-to-toggle — never by
 *                                 spring-loaded drag expansion (see 5).
 *   2. `collapsedRouteExpandedIds` — transient route-scoped collapses. When
 *                                 the user navigates to a topic, the panel
 *                                 auto-expands ancestors. If the user then
 *                                 explicitly collapses one of those ancestor
 *                                 rows, we record that collapse here (NOT
 *                                 in localStorage) so it stays collapsed for
 *                                 the duration of the route, but a future
 *                                 visit starts fresh.
 *   3. `activeTopic`            — derived from the URL pathname + the
 *                                 memory's topic_id (when on a memory
 *                                 detail route).
 *   4. `visibleExpandedIds`     — derived: `(expandedIds ∪ ancestorIds −
 *                                 collapsedRouteExpandedIds) ∪
 *                                 springExpandedIds`. What
 *                                 `<TopicTreeContent>` actually consumes.
 *   5. `springExpandedIds`      — transient drag-scoped expansions from
 *                                 spring-loading (hovering a drag over a
 *                                 collapsed branch). Cleared when the drag
 *                                 ends — Finder semantics: a folder the
 *                                 drag sprung open snaps back closed, it
 *                                 does NOT become a persisted preference.
 *                                 Keeping this out of `expandedIds` is the
 *                                 point: passing a drag over the tree must
 *                                 not rewrite the user's sidebar.
 *
 * Extracted from `PinnedTreeSidebar` so v1 (the existing pinned sidebar)
 * AND v2 (`<PinnedSecondaryPanel>` in shell-v2) share one source of truth
 * — the user's expansion preferences carry across shells, and the auto-
 * expand-on-active-topic behavior stays identical.
 *
 * Pure hook — no rendering, no JSX. Caller wires the result into a
 * `<TopicTreeContent>` mount.
 */

import { useEffect, useState, useMemo, useCallback, useRef } from "react";
import { usePathname } from "next/navigation";
import { useTopicDragContext } from "@/components/features/topic/topic-dnd-hooks";
import { useMemory } from "./use-memories";
import { useTopics } from "./use-topics";
import {
  buildAncestorTopicIds,
  getActiveTopicIdForPath,
  getMemoryIdForPath,
} from "@/lib/topic-tree-active";

const EXPANDED_STORAGE_KEY = "memax_tree_expanded";

function getStoredExpanded(): Set<string> {
  if (typeof window === "undefined") return new Set();
  try {
    const raw = localStorage.getItem(EXPANDED_STORAGE_KEY);
    return raw ? new Set(JSON.parse(raw)) : new Set();
  } catch {
    return new Set();
  }
}

function storeExpanded(ids: Set<string>) {
  try {
    localStorage.setItem(EXPANDED_STORAGE_KEY, JSON.stringify([...ids]));
  } catch {
    // localStorage unavailable (private mode, quota exceeded) — silent
    // degrade. The session keeps working with in-memory state; preferences
    // simply don't survive a reload. Acceptable.
  }
}

export interface TopicTreeController {
  /** What `<TopicTreeContent>` should render as expanded. */
  visibleExpandedIds: Set<string>;
  /** Caller passes this to `<TopicTreeContent>`'s onToggleExpand prop. */
  onToggleExpand: (id: string) => void;
  /**
   * Caller passes this to `<TopicTreeContent>`'s onSpringExpand prop.
   * Records a transient drag-scoped expansion (spring-load) that reverts
   * when the drag ends, instead of writing the persistent preference.
   */
  onSpringExpand: (id: string) => void;
  /**
   * The active topic id derived from the URL — for highlighting in the
   * tree. Matches `getActiveTopicIdForPath`'s `string | undefined` return
   * shape so it threads directly into `<TopicTreeContent>`'s
   * `activeTopic?: string` prop with no narrowing dance.
   */
  activeTopic: string | undefined;
}

export function useTopicTreeController(): TopicTreeController {
  const pathname = usePathname();
  const memoryId = getMemoryIdForPath(pathname) ?? "";
  const { data: memory } = useMemory(memoryId);
  const { data: topicsData } = useTopics();

  const activeTopic = getActiveTopicIdForPath({
    pathname,
    memoryTopicId: memory?.topic_id,
  });

  const [expandedIds, setExpandedIds] =
    useState<Set<string>>(getStoredExpanded);
  const [collapsedRouteExpandedIds, setCollapsedRouteExpandedIds] = useState<
    Set<string>
  >(new Set());
  const [springExpandedIds, setSpringExpandedIds] = useState<Set<string>>(
    new Set(),
  );

  const activeTopicAncestorIds = useMemo(
    () =>
      activeTopic && topicsData?.topics
        ? (buildAncestorTopicIds(topicsData.topics, activeTopic) ?? [])
        : [],
    [activeTopic, topicsData?.topics],
  );

  const visibleExpandedIds = useMemo(() => {
    const next = new Set(expandedIds);
    for (const id of activeTopicAncestorIds) {
      if (collapsedRouteExpandedIds.has(id)) continue;
      next.add(id);
    }
    // Spring expansions layer on top unconditionally — including over a
    // route-scoped collapse, because holding a drag over the row is a
    // fresher signal of intent than the earlier collapse click.
    for (const id of springExpandedIds) next.add(id);
    return next;
  }, [
    activeTopicAncestorIds,
    collapsedRouteExpandedIds,
    expandedIds,
    springExpandedIds,
  ]);

  // Keep the toggle handler stable across renders while still seeing the
  // latest derived state. Reading from a ref inside the handler avoids
  // re-creating onToggleExpand on every render (which would force every
  // <TopicTreeNode> to re-render).
  const toggleStateRef = useRef({
    activeTopicAncestorIds,
    expandedIds,
    collapsedRouteExpandedIds,
    springExpandedIds,
  });
  toggleStateRef.current = {
    activeTopicAncestorIds,
    expandedIds,
    collapsedRouteExpandedIds,
    springExpandedIds,
  };

  // Route changes reset the route-scoped collapse set. The persistent
  // `expandedIds` survives — that's the point of the two-set model.
  useEffect(() => {
    setCollapsedRouteExpandedIds(new Set());
  }, [pathname]);

  // Spring expansions live exactly as long as the drag. `activeType`
  // goes null on drop AND on cancel (both handleDragEnd and
  // handleDragCancel reset it), so this cannot strand a sprung branch
  // open. Outside a TopicDndProvider the context defaults to
  // activeType: null and the set stays empty — a no-op.
  const { activeType: dragActiveType } = useTopicDragContext();
  useEffect(() => {
    if (dragActiveType === null) {
      setSpringExpandedIds((prev) => (prev.size === 0 ? prev : new Set()));
    }
  }, [dragActiveType]);

  const onSpringExpand = useCallback((id: string) => {
    setSpringExpandedIds((prev) => {
      if (prev.has(id)) return prev;
      const next = new Set(prev);
      next.add(id);
      return next;
    });
  }, []);

  const onToggleExpand = useCallback((id: string) => {
    const {
      activeTopicAncestorIds: ancestorIds,
      expandedIds: persistedExpandedIds,
      collapsedRouteExpandedIds: routeCollapsedIds,
      springExpandedIds: springIds,
    } = toggleStateRef.current;
    const isAncestor = ancestorIds.includes(id);
    const isInPersistent = persistedExpandedIds.has(id);
    const isRouteCollapsed = routeCollapsedIds.has(id);
    const isVisible = (isInPersistent || isAncestor) && !isRouteCollapsed;

    // A row held open only by a spring expansion: clicking its chevron
    // collapses the spring, and must NOT fall through to the expand
    // branch below (which would write the persistent preference — the
    // exact rewrite spring-loading exists to avoid).
    if (springIds.has(id)) {
      setSpringExpandedIds((prev) => {
        if (!prev.has(id)) return prev;
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      if (!isVisible) return;
    }

    if (isVisible) {
      // A collapse click should remove every source keeping the row open.
      // That means stripping persisted expansion too when the row is also
      // an active-topic ancestor. Re-expanding a route-collapsed row stays
      // transient and is handled by the branch below.
      if (isAncestor) {
        setCollapsedRouteExpandedIds((prev) => {
          if (prev.has(id)) return prev;
          const next = new Set(prev);
          next.add(id);
          return next;
        });
      }
      if (isInPersistent) {
        setExpandedIds((prev) => {
          if (!prev.has(id)) return prev;
          const next = new Set(prev);
          next.delete(id);
          storeExpanded(next);
          return next;
        });
      }
      return;
    }

    if (isRouteCollapsed) {
      setCollapsedRouteExpandedIds((prev) => {
        if (!prev.has(id)) return prev;
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      return;
    }

    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      storeExpanded(next);
      return next;
    });
  }, []);

  return { visibleExpandedIds, onToggleExpand, onSpringExpand, activeTopic };
}
