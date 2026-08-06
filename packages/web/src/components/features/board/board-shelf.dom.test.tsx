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
import { BoardShelf, groupSlotsByKind, orderShelfSlots } from "./board-shelf";
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

function highlightCard(id: string, title: string): BoardNotificationCardModel {
  return {
    id,
    kind: "hub_member_joined",
    title,
    description: "",
    actions: [],
    item: {
      id,
      audience: "hub",
      kind: "hub_member_joined",
      status: "pending",
      seen: false,
      title,
      description: "",
      similarity: 0,
      created_at: "2026-08-05T00:00:00Z",
    },
  };
}

const noHandlers = {
  onOpenDeck: () => {},
  onOpenSlot: () => {},
  onOpenBoards: () => {},
};

function tileEls(container: HTMLElement): HTMLElement[] {
  return Array.from(
    container.querySelectorAll<HTMLElement>("[data-board-tile]"),
  );
}

describe("BoardShelf", () => {
  afterEach(cleanup);

  it("orders tiles 等你 → highlight → lane B → capsule → activity → custom → cooking → ghost and excludes receipts", () => {
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
    const fitnessBoard = {
      id: "b3",
      hub_id: "h1",
      kind: "custom",
      status: "active",
      title: "健身 & 睡眠",
      instruction: "watch my sleep",
      created_at: "2026-08-05T00:00:00Z",
      updated_at: "2026-08-05T00:00:00Z",
    } as Board;
    const { container } = render(
      <BoardShelf
        waiting={[
          waitingCard("n1", "Keep which memory?"),
          waitingCard("n2", "Second decision"),
        ]}
        highlights={[highlightCard("n3", "Ada joined your hub")]}
        slots={slots}
        customBoards={[
          {
            board: fitnessBoard,
            slots: [
              slot({
                slot_key: "s-custom",
                kind: "pattern",
                title: "Custom board card",
              }),
            ],
          },
        ]}
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
        {...noHandlers}
      />,
    );

    const tiles = tileEls(container);
    // 7 content tiles + the trailing ghost tile.
    expect(tiles).toHaveLength(8);
    // 等你 deck tile leads: top decision + depth badge, second decision
    // stays behind the deck (never its own tile).
    expect(tiles[0].textContent).toContain("Waiting on you");
    expect(tiles[0].textContent).toContain("Keep which memory?");
    expect(tiles[0].textContent).toContain("1 more");
    expect(screen.queryByText("Second decision")).toBeNull();
    // The member-joined highlight gets its OWN tile, right after the
    // decisions — it does not hide in the 最近 receipts.
    expect(tiles[1].textContent).toContain("NEW MEMBER");
    expect(tiles[1].textContent).toContain("Ada joined your hub");
    // Then lane B → capsule → activity → custom live card (tagged
    // with its board title) → cooking custom board → ghost.
    expect(tiles[2].textContent).toContain("Echo card");
    expect(tiles[3].textContent).toContain("Capsule card");
    expect(tiles[4].textContent).toContain("Activity card");
    expect(tiles[5].textContent).toContain("Custom board card");
    expect(tiles[5].textContent).toContain("健身 & 睡眠");
    expect(tiles[6].textContent).toContain("对手动向");
    expect(tiles[7].dataset.boardTile).toBe("ghost");
    // Resolved receipts do not earn shelf space.
    expect(screen.queryByText("Resolved dream")).toBeNull();
  });

  it("renders exactly ONE row — no second grid row, horizontal flex only", () => {
    // Enough tiles that the old layout would have wrapped to two rows.
    const manySlots = ["echo", "thread", "openq", "pattern", "musing"].map(
      (kind, i) =>
        slot({ slot_key: `s-${kind}`, kind, title: `${kind} card ${i}` }),
    );
    const { container } = render(
      <BoardShelf
        waiting={[waitingCard("n1", "Keep which memory?")]}
        highlights={[highlightCard("n2", "Ada joined")]}
        slots={manySlots}
        customBoards={[]}
        cookingBoards={[]}
        {...noHandlers}
      />,
    );
    expect(container.querySelector(".grid-rows-2")).toBeNull();
    const row = container.querySelector(".flex.w-max");
    expect(row).toBeTruthy();
    expect(row!.className).toContain("flex-nowrap");
  });

  it("sizes tiles by kind purpose: wide decision / standard insight / square capsule / slim activity", () => {
    const { container } = render(
      <BoardShelf
        waiting={[waitingCard("n1", "Keep which memory?")]}
        highlights={[]}
        slots={[
          slot({ slot_key: "s-echo", kind: "echo", title: "Echo card" }),
          slot({ slot_key: "s-cap", kind: "capsule", title: "Capsule card" }),
          slot({ slot_key: "s-act", kind: "activity", title: "Activity card" }),
        ]}
        customBoards={[]}
        cookingBoards={[]}
        {...noHandlers}
      />,
    );
    const byKind = (kind: string) =>
      container.querySelector<HTMLElement>(`[data-board-tile="${kind}"]`);
    expect(byKind("waiting")!.dataset.size).toBe("wide");
    expect(byKind("waiting")!.className).toContain("w-[320px]");
    expect(byKind("echo")!.dataset.size).toBe("standard");
    expect(byKind("echo")!.className).toContain("w-[272px]");
    expect(byKind("capsule")!.dataset.size).toBe("square");
    expect(byKind("capsule")!.className).toContain("w-[200px]");
    expect(byKind("activity")!.dataset.size).toBe("slim");
    expect(byKind("activity")!.className).toContain("w-[180px]");
  });

  it("left-aligns tile text and shows a quiet relative generated-at line", () => {
    const threeDaysAgo = new Date(
      Date.now() - 3 * 24 * 60 * 60 * 1000,
    ).toISOString();
    const { container } = render(
      <BoardShelf
        waiting={[]}
        highlights={[]}
        slots={[
          slot({
            slot_key: "s-echo",
            kind: "echo",
            title: "Echo card",
            content_updated_at: threeDaysAgo,
          }),
        ]}
        customBoards={[]}
        cookingBoards={[]}
        {...noHandlers}
      />,
    );
    const tile = container.querySelector<HTMLElement>(
      '[data-board-tile="echo"]',
    )!;
    // The tap surface itself is a left-aligned vertical stack.
    const button = tile.querySelector("button")!;
    expect(button.className).toContain("text-left");
    expect(button.className).toContain("flex-col");
    expect(button.className).toContain("items-start");
    // Generated-at from content_updated_at, relative + i18n'd.
    expect(tile.textContent).toContain("3d ago");
  });

  it("stacks same-kind live slots into one tile with a depth badge", () => {
    const { container } = render(
      <BoardShelf
        waiting={[]}
        highlights={[]}
        slots={[
          slot({ slot_key: "s-p1", kind: "pattern", title: "First pattern" }),
          slot({ slot_key: "s-p2", kind: "pattern", title: "Second pattern" }),
        ]}
        customBoards={[]}
        cookingBoards={[]}
        {...noHandlers}
      />,
    );
    const patternTiles = container.querySelectorAll(
      '[data-board-tile="pattern"]',
    );
    expect(patternTiles).toHaveLength(1);
    expect(patternTiles[0].textContent).toContain("First pattern");
    expect(patternTiles[0].textContent).toContain("1 more");
    expect(screen.queryByText("Second pattern")).toBeNull();
  });

  it("tile taps route to the right open callback", () => {
    const onOpenDeck = vi.fn();
    const onOpenSlot = vi.fn();
    render(
      <BoardShelf
        waiting={[waitingCard("n1", "Keep which memory?")]}
        highlights={[]}
        slots={[slot({ slot_key: "s-echo", kind: "echo", title: "Echo card" })]}
        customBoards={[]}
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

  it("tile × dismisses without opening; decision tiles carry no ×", () => {
    const onOpenSlot = vi.fn();
    const onDismissSlot = vi.fn();
    const onDismissNotification = vi.fn();
    const { container } = render(
      <BoardShelf
        waiting={[waitingCard("n1", "Keep which memory?")]}
        highlights={[highlightCard("n2", "Ada joined your hub")]}
        slots={[slot({ slot_key: "s-echo", kind: "echo", title: "Echo card" })]}
        customBoards={[]}
        cookingBoards={[]}
        onOpenDeck={() => {}}
        onOpenSlot={onOpenSlot}
        onOpenBoards={() => {}}
        onDismissSlot={onDismissSlot}
        onDismissNotification={onDismissNotification}
      />,
    );
    // Slot tile × → resolve action="dismiss" path, tap surface untouched.
    const echoTile = container.querySelector<HTMLElement>(
      '[data-board-tile="echo"]',
    )!;
    fireEvent.click(echoTile.querySelector('[aria-label="Not interested"]')!);
    expect(onDismissSlot).toHaveBeenCalledWith("s-echo");
    expect(onOpenSlot).not.toHaveBeenCalled();
    // Highlight tile × → notification dismiss path.
    const hlTile = container.querySelector<HTMLElement>(
      '[data-board-tile="highlight"]',
    )!;
    fireEvent.click(hlTile.querySelector('[aria-label="Not interested"]')!);
    expect(onDismissNotification).toHaveBeenCalledWith("n2");
    // 等你 needs an answer, not a swipe-away: no × on decision tiles.
    const deckTile = container.querySelector<HTMLElement>(
      '[data-board-tile="waiting"]',
    )!;
    expect(deckTile.querySelector('[aria-label="Not interested"]')).toBeNull();
  });

  it("closes the shelf with the ghost tile → the boards surface", () => {
    const onOpenBoards = vi.fn();
    const { container } = render(
      <BoardShelf
        waiting={[]}
        highlights={[]}
        slots={[slot({ slot_key: "s-echo", kind: "echo", title: "Echo card" })]}
        customBoards={[]}
        cookingBoards={[]}
        onOpenDeck={() => {}}
        onOpenSlot={() => {}}
        onOpenBoards={onOpenBoards}
      />,
    );
    const ghost = container.querySelector<HTMLElement>(
      '[data-board-tile="ghost"]',
    )!;
    expect(ghost.textContent).toContain("Have memax watch one thing");
    fireEvent.click(ghost);
    expect(onOpenBoards).toHaveBeenCalledTimes(1);
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

  it("groupSlotsByKind stacks live same-kind slots and skips receipts", () => {
    const groups = groupSlotsByKind([
      slot({ slot_key: "a", kind: "pattern" }),
      slot({ slot_key: "b", kind: "echo" }),
      slot({ slot_key: "c", kind: "pattern" }),
      slot({ slot_key: "d", kind: "pattern", state: "resolved" }),
    ]);
    expect(groups.map((g) => g.map((s) => s.slot_key))).toEqual([
      ["a", "c"],
      ["b"],
    ]);
  });
});
