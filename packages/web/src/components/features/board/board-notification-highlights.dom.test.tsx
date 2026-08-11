// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import {
  render,
  renderHook,
  screen,
  fireEvent,
  cleanup,
} from "@testing-library/react";
import type { Notification } from "memax-sdk";

// board-notification-cards pulls the inbox renderers, which reach for
// the router and auth at render time; the test tree mounts neither.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => "/h/personal/memories",
}));
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: { id: "u1", name: "derek" }, hubs: [] }),
  useActiveHub: () => ({ activeHub: undefined, hubFilter: "h1" }),
}));
vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => false,
}));

// Mutable row store the useNotifications mock reads — vi.mock is
// hoisted above imports, so the factory can't close over a plain
// module-level const.
const pendingRows = vi.hoisted(() => ({ rows: [] as unknown[] }));
vi.mock("@/hooks/use-notifications", () => ({
  useNotifications: () => ({
    data: { notifications: pendingRows.rows },
  }),
}));

// The bucket hook reads settings (pulse_hidden_hub_ids) via React
// Query; mock it like the other hooks so the test tree needs no
// QueryClientProvider.
vi.mock("@/hooks/use-settings", () => ({
  useSettings: () => ({ data: undefined }),
}));

import {
  BoardHighlightCard,
  useBoardNotificationCards,
  type BoardNotificationCardModel,
} from "./board-notification-cards";

function notif(overrides: Partial<Notification>): Notification {
  return {
    id: "n1",
    audience: "hub",
    hub_id: "h1",
    kind: "hub_member_joined",
    status: "pending",
    priority: 0,
    source_kind: "membership",
    created_at: "2026-08-05T00:00:00Z",
    ...overrides,
  } as Notification;
}

function highlightModel(): BoardNotificationCardModel {
  return {
    id: "n1",
    kind: "hub_member_joined",
    title: "Ada joined Team Rocket",
    description: "",
    actions: [],
    item: {
      id: "n1",
      audience: "hub",
      kind: "hub_member_joined",
      status: "pending",
      seen: false,
      title: "Ada joined Team Rocket",
      description: "",
      similarity: 0,
      created_at: "2026-08-05T00:00:00Z",
    },
  };
}

describe("hub_member_joined highlights", () => {
  afterEach(() => {
    cleanup();
    pendingRows.rows = [];
  });

  it("buckets member_joined into highlights, not the 最近 receipts", () => {
    pendingRows.rows = [
      notif({
        id: "n-member",
        kind: "hub_member_joined",
        payload: {
          hub: { id: "h1", name: "Team Rocket" },
          member: { id: "u2", display: "Ada" },
        },
      }),
      notif({ id: "n-receipt", kind: "hub_invite_accepted" }),
    ];

    const { result } = renderHook(() => useBoardNotificationCards("h1", true));

    expect(result.current.highlights.map((c) => c.id)).toEqual(["n-member"]);
    expect(result.current.highlights[0].kind).toBe("hub_member_joined");
    // The receipt stays in 最近; the highlight never lands there.
    expect(result.current.recent.map((c) => c.id)).toEqual(["n-receipt"]);
    // And it is not a decision either.
    expect(result.current.waiting).toHaveLength(0);
  });

  it("renders as a standalone card: ✦ NEW MEMBER label + single dismiss", () => {
    const onDismiss = vi.fn();
    render(
      <BoardHighlightCard
        card={highlightModel()}
        entranceIndex={0}
        disabled={false}
        onDismiss={onDismiss}
      />,
    );

    // Inbox kind label reused so "NEW MEMBER" reads the same wherever
    // it surfaces.
    expect(screen.getByText("NEW MEMBER")).toBeTruthy();
    expect(screen.getByText("Ada joined Team Rocket")).toBeTruthy();

    fireEvent.click(screen.getByText("Dismiss"));
    expect(onDismiss).toHaveBeenCalledWith("n1");
  });
});
