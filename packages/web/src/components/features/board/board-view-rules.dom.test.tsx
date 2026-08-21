// @vitest-environment jsdom

/**
 * BOARD_SHELF_RULES coverage: the useShelfExpansion state machine
 * (R1 expanded default, R6 manual override persists per session) plus
 * the BoardSlotDeck ↻ cycle.
 *
 * R2/R5 were deleted with the 2026-08 R1 revision — auto-expand only
 * existed to rescue a collapsed default, and auto-collapse fought the
 * new default. Their tests went with them rather than being softened.
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

import {
  BoardArchivedSection,
  BoardSlotDeck,
  useShelfExpansion,
} from "./board-view";

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

  function mount() {
    return renderHook(() => useShelfExpansion({ hubId: "h1", enabled: true }));
  }

  it("R1: expanded by default on a fresh visit", () => {
    const { result } = mount();
    expect(result.current.expanded).toBe(true);
  });

  it("R6: a manual collapse persists across a remount in the session", () => {
    const first = mount();
    act(() => first.result.current.setExpanded(false));
    expect(first.result.current.expanded).toBe(false);
    first.unmount();
    // A remount in the same session honors the stored choice instead of
    // re-applying the expanded default.
    const second = mount();
    expect(second.result.current.expanded).toBe(false);
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

describe("BoardArchivedSection (工单 8 — dismiss is archive + undo)", () => {
  afterEach(() => {
    cleanup();
  });

  it("collapses to a count, expands to rows, and restore fires reopen", () => {
    const onRestore = vi.fn();
    render(
      <BoardArchivedSection
        slots={[
          slot({
            slot_key: "0-a",
            title: "旧的洞察",
            state: "dismissed",
            resolution: {
              action: "dismiss",
              resolved_by: "u1",
              resolved_at: "2026-08-10T00:00:00Z",
            },
          }),
          slot({
            slot_key: "0-b",
            title: "old receipt",
            state: "resolved",
            resolution: {
              action: "ack",
              resolved_by: "u1",
              resolved_at: "2026-08-10T00:00:00Z",
            },
          }),
        ]}
        pending={false}
        onRestore={onRestore}
      />,
    );

    // Collapsed: the count header, no rows yet.
    expect(screen.getByText("2")).toBeTruthy();
    expect(screen.queryByText(/旧的洞察/)).toBeNull();

    fireEvent.click(screen.getByText("Archived"));
    expect(screen.getByText(/旧的洞察/)).toBeTruthy();
    expect(screen.getByText(/old receipt/)).toBeTruthy();

    // Restore is per-row and reports the slot key.
    fireEvent.click(screen.getAllByText("Restore")[0]);
    expect(onRestore).toHaveBeenCalledWith("0-a");
  });
});
