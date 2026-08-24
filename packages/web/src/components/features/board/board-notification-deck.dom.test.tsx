// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";

// board-notification-cards pulls the inbox renderers, which reach for
// the router and auth at render time; the test tree mounts neither.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => "/h/personal/memories",
}));
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: null, hubs: [] }),
  useActiveHub: () => ({ activeHub: undefined, hubFilter: "h1" }),
}));
vi.mock("@/hooks/use-notifications", () => ({
  useNotifications: () => ({ data: undefined }),
}));
vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => false,
}));

import {
  BoardNotificationDeck,
  type BoardNotificationCardModel,
} from "./board-notification-cards";

function card(id: string, title: string): BoardNotificationCardModel {
  return {
    id,
    kind: "contradiction",
    title,
    description: `${title} — details`,
    actions: [
      { action: "keep_a", label: "Keep A", tone: "dream" },
      { action: "dismiss", label: "Dismiss it", tone: "ghost" },
    ],
    item: {
      id,
      audience: "hub",
      kind: "contradiction",
      status: "pending",
      seen: false,
      title,
      description: `${title} — details`,
      similarity: 0,
      created_at: "2026-08-05T00:00:00Z",
    },
  };
}

describe("BoardNotificationDeck", () => {
  afterEach(cleanup);

  it("renders only the top card, with the pile counted in the badge", () => {
    render(
      <BoardNotificationDeck
        cards={[
          card("a", "First decision"),
          card("b", "Second decision"),
          card("c", "Third decision"),
        ]}
        countLabel="2 more waiting"
        disabled={false}
        onResolve={() => {}}
      />,
    );
    expect(screen.getByText("First decision")).toBeTruthy();
    expect(screen.queryByText("Second decision")).toBeNull();
    expect(screen.queryByText("Third decision")).toBeNull();
    expect(screen.getByText("2 more waiting")).toBeTruthy();
  });

  it("resolving the top card advances the deck to the next one", () => {
    const onResolve = vi.fn();
    const deck = (cards: BoardNotificationCardModel[], countLabel: string) => (
      <BoardNotificationDeck
        cards={cards}
        countLabel={countLabel}
        disabled={false}
        onResolve={onResolve}
      />
    );
    const cards = [card("a", "First decision"), card("b", "Second decision")];
    const { rerender } = render(deck(cards, "1 more waiting"));

    fireEvent.click(screen.getByText("Keep A"));
    expect(onResolve).toHaveBeenCalledWith("a", "keep_a");

    // The resolve mutation removes the row from the pending query; the
    // deck is fully controlled by props, so the next card surfaces.
    rerender(deck(cards.slice(1), "0 more waiting"));
    expect(screen.getByText("Second decision")).toBeTruthy();
    expect(screen.queryByText("First decision")).toBeNull();
    // Single remaining card → no badge, no ghost stack.
    expect(screen.queryByText("0 more waiting")).toBeNull();
  });

  it("↻ cycles to the next card client-side without resolving anything", () => {
    const onResolve = vi.fn();
    render(
      <BoardNotificationDeck
        cards={[
          card("a", "First decision"),
          card("b", "Second decision"),
          card("c", "Third decision"),
        ]}
        countLabel="2 more waiting"
        disabled={false}
        onResolve={onResolve}
      />,
    );
    // Re-query per click — cycling remounts the card (keyed by id).
    fireEvent.click(screen.getByLabelText("Show next card"));
    expect(screen.getByText("Second decision")).toBeTruthy();
    expect(screen.queryByText("First decision")).toBeNull();
    fireEvent.click(screen.getByLabelText("Show next card"));
    expect(screen.getByText("Third decision")).toBeTruthy();
    // Wraps around; the pile itself is untouched.
    fireEvent.click(screen.getByLabelText("Show next card"));
    expect(screen.getByText("First decision")).toBeTruthy();
    expect(onResolve).not.toHaveBeenCalled();
  });

  it("renders nothing for an empty deck", () => {
    const { container } = render(
      <BoardNotificationDeck
        cards={[]}
        countLabel=""
        disabled={false}
        onResolve={() => {}}
      />,
    );
    expect(container.firstChild).toBeNull();
  });
});
