// @vitest-environment jsdom

/**
 * BOARD_SHELF_RULES coverage: the useShelfExpansion state machine
 * (R1 collapsed default, R2 auto-expand on decision/highlight, R5
 * auto-collapse after the last resolve, R6 manual override persists)
 * plus the BoardSlotDeck ↻ cycle.
 */

import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react";
import type { BoardSlot } from "memax-sdk";

// board-view's module graph reaches the router, auth, posthog and the
// data hooks at import time; the rules under test need none of them.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => "/h/personal/memories",
}));
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: null, hubs: [] }),
  useActiveHub: () => ({ activeHub: undefined, hubFilter: "h1" }),
}));
vi.mock("@/lib/posthog", () => ({ trackEvent: vi.fn() }));
vi.mock("@/hooks/use-board", () => ({
  useHubBoard: () => ({ data: undefined, isPending: true, isError: false }),
  useHubBoards: () => ({ data: undefined }),
  useCustomBoardsWithSlots: () => [],
  useCreateBoard: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteBoard: () => ({ mutate: vi.fn(), isPending: false }),
  useResolveBoardSlot: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@/hooks/use-notifications", () => ({
  useNotifications: () => ({ data: undefined }),
  useResolveNotification: () => ({ mutate: vi.fn(), isPending: false }),
  useNotificationDismiss: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@/hooks/use-board-continue", () => ({
  useBoardCardActions: () => ({
    continueInMemax: vi.fn(),
    copyForAgent: vi.fn(),
    copiedSlotKey: null,
    isContinuing: false,
  }),
}));
vi.mock("@/components/features/onboarding/onboarding-pinned", () => ({
  PinnedDispatch: () => null,
}));

import { BoardSlotDeck, useShelfExpansion } from "./board-view";

function slot(overrides: Partial<BoardSlot>): BoardSlot {
  return {
    id: "s1",
    board_id: "b1",
    slot_key: "0-slot",
    kind: "pattern",
    title: "untitled",
    state: "fresh",
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
    ...overrides,
  };
}

describe("useShelfExpansion (BOARD_SHELF_RULES)", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  function mount(initial: { needsAttention: boolean; liveCount: number }) {
    return renderHook(
      (props: { needsAttention: boolean; liveCount: number }) =>
        useShelfExpansion({ hubId: "h1", enabled: true, ...props }),
      { initialProps: initial },
    );
  }

  it("R1: collapsed by default when nothing needs the user", () => {
    const { result } = mount({ needsAttention: false, liveCount: 3 });
    expect(result.current.expanded).toBe(false);
  });

  it("R2: auto-expands when a pending decision / fresh highlight exists", () => {
    const { result } = mount({ needsAttention: true, liveCount: 3 });
    expect(result.current.expanded).toBe(true);
  });

  it("R5: auto-collapses ~800ms after the last live card resolves", () => {
    vi.useFakeTimers();
    const { result, rerender } = mount({ needsAttention: true, liveCount: 2 });
    expect(result.current.expanded).toBe(true);
    // Both cards resolved — the board exhales after a beat.
    rerender({ needsAttention: false, liveCount: 0 });
    expect(result.current.expanded).toBe(true);
    act(() => {
      vi.advanceTimersByTime(800);
    });
    expect(result.current.expanded).toBe(false);
  });

  it("R6: a manual choice overrides auto rules and persists per session", () => {
    const first = mount({ needsAttention: true, liveCount: 2 });
    // User collapses despite the pending decision…
    act(() => first.result.current.setExpanded(false));
    // …auto-expand must not fight the choice.
    first.rerender({ needsAttention: true, liveCount: 2 });
    expect(first.result.current.expanded).toBe(false);
    first.unmount();
    // A remount in the same session honors the stored choice.
    const second = mount({ needsAttention: true, liveCount: 2 });
    expect(second.result.current.expanded).toBe(false);
  });

  it("R5 does not fire over a manual expand", () => {
    vi.useFakeTimers();
    const { result, rerender } = mount({ needsAttention: false, liveCount: 1 });
    act(() => result.current.setExpanded(true));
    rerender({ needsAttention: false, liveCount: 0 });
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(result.current.expanded).toBe(true);
  });
});

describe("BoardSlotDeck", () => {
  afterEach(cleanup);

  it("fronts the first slot and ↻ advances client-side through the group", () => {
    const group = [
      slot({ slot_key: "p1", title: "First pattern" }),
      slot({ slot_key: "p2", title: "Second pattern" }),
    ];
    render(
      <BoardSlotDeck
        group={group}
        countLabel="1 more"
        cycleAriaLabel="Show next card"
      >
        {(current, controls) => (
          <div>
            <span>{current.title}</span>
            {controls}
          </div>
        )}
      </BoardSlotDeck>,
    );
    expect(screen.getByText("First pattern")).toBeTruthy();
    expect(screen.getByText("1 more")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Show next card"));
    expect(screen.getByText("Second pattern")).toBeTruthy();
    expect(screen.queryByText("First pattern")).toBeNull();
    // Wraps back around — browsing, not consuming.
    fireEvent.click(screen.getByLabelText("Show next card"));
    expect(screen.getByText("First pattern")).toBeTruthy();
  });

  it("a deck of one renders without controls or ghost edges", () => {
    const { container } = render(
      <BoardSlotDeck
        group={[slot({ slot_key: "p1", title: "Only pattern" })]}
        countLabel="0 more"
        cycleAriaLabel="Show next card"
      >
        {(current, controls) => (
          <div>
            <span>{current.title}</span>
            {controls}
          </div>
        )}
      </BoardSlotDeck>,
    );
    expect(screen.getByText("Only pattern")).toBeTruthy();
    expect(screen.queryByLabelText("Show next card")).toBeNull();
    expect(container.querySelector(".glass-card.absolute")).toBeNull();
  });
});
