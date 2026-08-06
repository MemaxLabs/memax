// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type { Board, BoardSlot } from "memax-sdk";

// Kind renderers navigate on drill-down; the test tree has no router.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

// Side-effect: registers Lane A + Lane B kinds so strip labels (the
// tile eyebrows) resolve through the real registry, not the fallback.
import "./board-kinds";
import { BoardShelf, orderShelfSlots } from "./board-shelf";
import type { BoardNotificationCardModel } from "./board-notification-cards";

function slot(overrides: Partial<BoardSlot>): BoardSlot {
  return {
    id: "s1",
    board_id: "b1",
    slot_key: "0-slot",
    kind: "dreamlog",
    title: "untitled",
    state: "fresh",
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
    ...overrides,
  };
}

function waitingCard(id: string, title: string): BoardNotificationCardModel {
  return {
    id,
    kind: "contradiction",
    title,
    description: "Two memories disagree.",
    actions: [],
    item: {
      id,
      audience: "hub",
      kind: "contradiction",
      status: "pending",
      seen: false,
      title,
      description: "Two memories disagree.",
      similarity: 0,
      created_at: "2026-08-05T00:00:00Z",
    },
  };
}

describe("BoardShelf", () => {
  afterEach(cleanup);

  it("orders tiles 等你 → lane B → capsule → activity → cooking and excludes receipts", () => {
    // Server order deliberately scrambled (activity first) to prove the
    // shelf re-sorts; the resolved slot must not surface at all.
    const slots = [
      slot({ slot_key: "s-act", kind: "activity", title: "Activity card" }),
      slot({ slot_key: "s-cap", kind: "capsule", title: "Capsule card" }),
      slot({
        slot_key: "s-old",
        kind: "dreamlog",
        title: "Resolved dream",
        state: "resolved",
      }),
      slot({ slot_key: "s-echo", kind: "echo", title: "Echo card" }),
    ];
    render(
      <BoardShelf
        waiting={[
          waitingCard("n1", "Keep which memory?"),
          waitingCard("n2", "Second decision"),
        ]}
        slots={slots}
        cookingBoards={[
          {
            id: "b2",
            hub_id: "h1",
            kind: "custom",
            status: "cooking",
            title: "对手动向",
            instruction: "watch competitors",
            created_at: "2026-08-05T00:00:00Z",
            updated_at: "2026-08-05T00:00:00Z",
          } as Board,
        ]}
        onOpenDeck={() => {}}
        onOpenSlot={() => {}}
        onOpenBoards={() => {}}
      />,
    );

    const tiles = screen.getAllByRole("button");
    expect(tiles).toHaveLength(5);
    // 等你 deck tile leads: top decision + depth badge, second decision
    // stays behind the deck (never its own tile).
    expect(tiles[0].textContent).toContain("Waiting on you");
    expect(tiles[0].textContent).toContain("Keep which memory?");
    expect(tiles[0].textContent).toContain("1 more waiting");
    expect(screen.queryByText("Second decision")).toBeNull();
    // Then lane B → capsule → activity → cooking custom board.
    expect(tiles[1].textContent).toContain("Echo card");
    expect(tiles[2].textContent).toContain("Capsule card");
    expect(tiles[3].textContent).toContain("Activity card");
    expect(tiles[4].textContent).toContain("对手动向");
    // Resolved receipts do not earn shelf space.
    expect(screen.queryByText("Resolved dream")).toBeNull();
  });

  it("orderShelfSlots drops terminal states and demotes capsule/activity", () => {
    const ordered = orderShelfSlots([
      slot({ slot_key: "a", kind: "activity" }),
      slot({ slot_key: "b", kind: "capsule" }),
      slot({ slot_key: "c", kind: "thread" }),
      slot({ slot_key: "d", kind: "dreamlog", state: "dismissed" }),
    ]);
    expect(ordered.map((s) => s.slot_key)).toEqual(["c", "b", "a"]);
  });

  it("tile taps route to the right open callback", () => {
    const onOpenDeck = vi.fn();
    const onOpenSlot = vi.fn();
    render(
      <BoardShelf
        waiting={[waitingCard("n1", "Keep which memory?")]}
        slots={[slot({ slot_key: "s-echo", kind: "echo", title: "Echo card" })]}
        cookingBoards={[]}
        onOpenDeck={onOpenDeck}
        onOpenSlot={onOpenSlot}
        onOpenBoards={() => {}}
      />,
    );
    fireEvent.click(screen.getByText("Keep which memory?"));
    expect(onOpenDeck).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByText("Echo card"));
    expect(onOpenSlot).toHaveBeenCalledWith("s-echo");
  });
});
