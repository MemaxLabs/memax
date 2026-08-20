// @vitest-environment jsdom

/**
 * Guard tests for the spring-expansion overlay in useTopicTreeController.
 *
 * The bug these exist to prevent: spring-loading (holding a drag over a
 * collapsed branch) used to call onToggleExpand, permanently rewriting
 * the user's persisted sidebar as a side effect of passing a drag over
 * it. Spring expansions must be a transient overlay — visible while the
 * drag lives, gone when it ends, never in localStorage.
 */

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { TopicDragContext } from "@/components/features/topic/topic-dnd-hooks";
import { useTopicTreeController } from "./use-topic-tree-controller";

vi.mock("next/navigation", () => ({
  usePathname: () => "/h/personal/memories",
}));

vi.mock("./use-memories", () => ({
  useMemory: () => ({ data: undefined }),
}));

vi.mock("./use-topics", () => ({
  useTopics: () => ({ data: { topics: [] } }),
}));

const EXPANDED_STORAGE_KEY = "memax_tree_expanded";

function makeWrapper(activeType: "memory" | "topic" | null) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <TopicDragContext.Provider
        value={{
          activeId: activeType ? "dragged" : null,
          activeType,
          invalidDescendantIds: new Set(),
          dropTargetsActive: activeType !== null,
        }}
      >
        {children}
      </TopicDragContext.Provider>
    );
  };
}

describe("useTopicTreeController spring expansion", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("onSpringExpand shows the branch WITHOUT writing localStorage", () => {
    const { result } = renderHook(() => useTopicTreeController(), {
      wrapper: makeWrapper("memory"),
    });

    act(() => result.current.onSpringExpand("topic-a"));

    expect(result.current.visibleExpandedIds.has("topic-a")).toBe(true);
    // The persisted preference must be untouched — this is the bug.
    expect(localStorage.getItem(EXPANDED_STORAGE_KEY)).toBeNull();
  });

  it("spring expansions revert when the drag ends (Finder snap-back)", () => {
    // Mutable drag state read by the wrapper: renderHook wrappers only
    // receive `children`, so the drag-end transition is driven by
    // mutating this and re-rendering.
    let activeType: "memory" | "topic" | null = "memory";
    const { result, rerender } = renderHook(() => useTopicTreeController(), {
      wrapper: ({ children }: { children: ReactNode }) => {
        const Wrapper = makeWrapper(activeType);
        return <Wrapper>{children}</Wrapper>;
      },
    });

    act(() => result.current.onSpringExpand("topic-a"));
    expect(result.current.visibleExpandedIds.has("topic-a")).toBe(true);

    // Drag ends: provider resets activeType to null.
    activeType = null;
    rerender();

    expect(result.current.visibleExpandedIds.has("topic-a")).toBe(false);
    expect(localStorage.getItem(EXPANDED_STORAGE_KEY)).toBeNull();
  });

  it("chevron-collapsing a spring-only row does not flip it to a persisted expand", () => {
    const { result } = renderHook(() => useTopicTreeController(), {
      wrapper: makeWrapper("memory"),
    });

    act(() => result.current.onSpringExpand("topic-a"));
    // The row reads as expanded; a collapse click routes through
    // onToggleExpand, which used to see "not visible" (spring wasn't a
    // visibility source it knew) and EXPAND persistently instead.
    act(() => result.current.onToggleExpand("topic-a"));

    expect(result.current.visibleExpandedIds.has("topic-a")).toBe(false);
    expect(localStorage.getItem(EXPANDED_STORAGE_KEY)).toBeNull();
  });

  it("click-to-expand still persists (the normal path is untouched)", () => {
    const { result } = renderHook(() => useTopicTreeController(), {
      wrapper: makeWrapper(null),
    });

    act(() => result.current.onToggleExpand("topic-b"));

    expect(result.current.visibleExpandedIds.has("topic-b")).toBe(true);
    expect(
      JSON.parse(localStorage.getItem(EXPANDED_STORAGE_KEY) ?? "[]"),
    ).toContain("topic-b");
  });
});
