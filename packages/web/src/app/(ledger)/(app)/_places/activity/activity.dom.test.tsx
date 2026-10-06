// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LedgerProvider } from "@memaxlabs/ledger";
import type { ActivityEntry, ActivityPage } from "@/lib/v2/data/activity";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { SpaceViewContext } from "../../_lib/space-context";
import { OverlayProvider } from "../../_lib/overlays";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { ActivityPlace } from "./activity-place";

// Activity over a fake source: rows by day, the list's own keys (↓ ↑,
// Enter opens the memory), filters, older pages by cursor, and totals
// and CSV that say they're only what's loaded.

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  usePathname: () => "/memax-v2/activity",
  useSearchParams: () => new URLSearchParams(),
}));

const source = {
  kind: "sdk" as const,
  now: () => new Date("2026-10-05T14:40:00-07:00"),
  viewer: {
    id: "me",
    initials: "ZZ",
    name: "Ziyang",
    timeZone: "America/Vancouver",
  },
  activity: vi.fn(),
  undo: vi.fn(),
};
vi.mock("../../_lib/data", () => ({
  useSource: () => source,
  useViewer: () => source.viewer,
  ledgerQueryKeys: {
    overview: (kind: string, slug: string) =>
      ["v2", kind, "spaces", slug, "overview"] as const,
  },
}));

const SPACE: SpaceSummary = {
  id: "s1",
  slug: "memax-v2",
  name: "memax-v2",
  kind: "project",
  role: "owner",
  kept: null,
  agents: null,
  people: null,
  waiting: null,
};

function entry(n: number, over: Partial<ActivityEntry> = {}): ActivityEntry {
  return {
    id: `r${n}`,
    at: "2026-10-05T14:00:00-07:00",
    actor: { kind: "you", initials: "ZZ" },
    action: "kept",
    object: { kind: "memory", ref: `M-000${n}`, id: `m${n}` },
    via: [{ kind: "via", via: "web" }],
    rawVia: "web",
    session: null,
    source: null,
    reason: null,
    ...over,
  };
}

const PAGE_1: ActivityPage = {
  entries: [
    entry(1),
    entry(2, {
      actor: { kind: "agent", agent: "claude-code" },
      action: "proposed",
      at: "2026-10-05T09:00:00-07:00",
    }),
    entry(3, { action: "forgot", at: "2026-10-04T10:00:00-07:00" }),
  ],
  nextCursor: "p2",
  totals: null,
};
const PAGE_2: ActivityPage = {
  entries: [entry(4, { at: "2026-09-20T10:00:00-07:00" })],
  nextCursor: null,
  totals: null,
};

function Frame({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <LedgerProvider locale="en">
        <ToastProvider>
          <OverlayProvider>
            <SpaceViewContext
              value={{
                space: SPACE,
                overview: undefined,
                overviewFailed: false,
                retryOverview: () => {},
              }}
            >
              {children}
            </SpaceViewContext>
          </OverlayProvider>
          <ToastViewport />
        </ToastProvider>
      </LedgerProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  source.activity.mockImplementation(({ cursor }: { cursor?: string }) =>
    Promise.resolve(cursor === "p2" ? PAGE_2 : PAGE_1),
  );
});
afterEach(cleanup);

describe("Activity", () => {
  it("lists receipts by day, and moves through them from the keyboard", async () => {
    render(
      <Frame>
        <ActivityPlace />
      </Frame>,
    );
    const today = await screen.findByRole("list", { name: "Today" });
    expect(within(today).getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByRole("list", { name: "Yesterday" })).toBeTruthy();
    const rows = screen.getAllByRole("listitem");
    // One tab stop: the first row.
    expect(rows.map((r) => r.tabIndex)).toEqual([0, -1, -1]);
    expect(rows[0]!.textContent).toContain("You kept a memory.");
    expect(rows[1]!.textContent).toContain("Claude Code proposed a memory.");
    rows[0]!.focus();
    fireEvent.keyDown(rows[0]!, { key: "ArrowDown" });
    expect(document.activeElement).toBe(rows[1]);
    fireEvent.keyDown(rows[1]!, { key: "End" });
    expect(document.activeElement).toBe(rows[2]);
    fireEvent.keyDown(rows[2]!, { key: "ArrowUp" });
    expect(document.activeElement).toBe(rows[1]);
    fireEvent.keyDown(rows[1]!, { key: "Enter" });
    expect(push).toHaveBeenCalledWith("/memax-v2/memories/M-0002");
  });

  it("filters what's loaded, and pages back by cursor", async () => {
    render(
      <Frame>
        <ActivityPlace />
      </Frame>,
    );
    await screen.findByRole("list", { name: "Today" });
    fireEvent.click(screen.getByRole("radio", { name: "Forgets" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    fireEvent.click(screen.getByRole("radio", { name: "Reads" }));
    expect(screen.getByText("No reads in what's loaded.")).toBeTruthy();
    expect(screen.getByText(/^Reads aren't receipts/)).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: "All" }));

    fireEvent.click(
      screen.getByRole("button", { name: /Load older receipts/ }),
    );
    await screen.findByText("Sunday, September 20");
    expect(source.activity).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: "p2" }),
    );
    expect(screen.getAllByRole("listitem")).toHaveLength(4);
    expect(
      screen.queryByRole("button", { name: /Load older receipts/ }),
    ).toBeNull();
  });

  it("says its totals and CSV are only what's loaded", async () => {
    render(
      <Frame>
        <ActivityPlace />
      </Frame>,
    );
    await screen.findByRole("list", { name: "Today" });
    const week = screen.getByRole("region", { name: "This week" });
    expect(within(week).getByText("From what's loaded")).toBeTruthy();
    const kept = within(week).getByText("Kept").nextElementSibling;
    expect(kept?.textContent).toBe("1");
    // Reads aren't receipts: unknown, not zero.
    expect(
      within(week).getByText("Reads").nextElementSibling?.textContent,
    ).toBe("—Not recorded yet");
    const button = screen.getByRole("button", {
      name: "Export CSV · 3 loaded",
    });
    expect(button.title).toMatch(/^Exports the 3 receipts loaded on this page/);
    fireEvent.click(
      screen.getByRole("button", { name: /Load older receipts/ }),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Export CSV" })).toBeTruthy(),
    );
    // The week is covered now that a receipt from before it is loaded.
    expect(within(week).queryByText("From what's loaded")).toBeNull();
  });

  it("says what the judge folded into what, with Undo while it can", async () => {
    const fold = (n: number, over: Partial<ActivityEntry> = {}) =>
      entry(n, {
        actor: { kind: "memax" },
        action: "merged",
        via: [{ kind: "via", via: "system" }],
        rawVia: "system",
        source: { kind: "memory", ref: "M-0001" },
        ...over,
      });
    source.activity.mockResolvedValue({
      entries: [
        fold(5),
        // Already undone: an `undid` receipt cites it.
        fold(6),
        entry(7, {
          action: "undid",
          source: { kind: "receipt", ref: "r6" },
        }),
        // Older than the 14 days.
        fold(8, { at: "2026-09-01T10:00:00-07:00" }),
      ],
      nextCursor: null,
      totals: null,
    });
    render(
      <Frame>
        <ActivityPlace />
      </Frame>,
    );
    await screen.findByRole("list", { name: "Today" });
    const rows = screen.getAllByRole("listitem").map((r) => r.textContent);
    expect(rows[0]).toContain("Memax folded M-0005 into M-0001.");
    expect(rows[1]).toContain("Memax folded M-0006 into M-0001.");
    const undo = screen.getAllByRole("button", { name: /^Undo the fold of/ });
    expect(undo.map((b) => b.getAttribute("aria-label"))).toEqual([
      "Undo the fold of M-0005",
    ]);
    source.undo = vi.fn().mockResolvedValue({ refs: ["M-0005"] });
    fireEvent.click(undo[0]!);
    await waitFor(() =>
      expect(source.undo).toHaveBeenCalledWith(
        expect.objectContaining({ receipt: "r5" }),
      ),
    );
    expect(
      await screen.findByText("Unfolded M-0005. It's back in Review."),
    ).toBeTruthy();
  });
});
