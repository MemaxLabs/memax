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
import type {
  ActivityEntry,
  ActivityPage,
  ReadsPage,
  SealView,
} from "@/lib/v2/data/activity";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { SpaceViewContext } from "../../_lib/space-context";
import { OverlayProvider } from "../../_lib/overlays";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { ActivityPlace } from "./activity-place";

// Activity over a fake source: rows by day, the list's own keys (↓ ↑,
// Enter opens the memory), filters, older pages by cursor, the reads
// beside the receipts, the seal line, and totals and CSV that say
// they're only what's loaded.

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
  reads: vi.fn(),
  seal: vi.fn(),
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

function read(n: number, at: string): ActivityEntry {
  return entry(n, {
    id: `d${n}`,
    at,
    actor: { kind: "agent", agent: "cursor", connectionId: "cu" },
    action: "read",
    object: { kind: "read", ref: `R-55${n}0`, id: `d${n}` },
    via: [{ kind: "via", via: "mcp" }],
    rawVia: "mcp",
    detail: { kind: "read", memories: n, brief: false },
  });
}
const NO_READS: ReadsPage = { entries: [], nextCursor: null, week: 57 };
const SEALED: SealView = {
  sealed: 1284,
  sealedAt: "2026-10-05T14:02:00-07:00",
  unsealed: 0,
  signed: true,
  keyId: "k1",
  verified: null,
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
  source.reads.mockResolvedValue(NO_READS);
  source.seal.mockResolvedValue(SEALED);
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

  it("opens a decision gate's card in Review from its asked, answered and withdrawn rows", async () => {
    const gate = (n: number, action: "asked" | "answered" | "withdrawn") =>
      entry(n, {
        actor:
          action === "answered"
            ? { kind: "you", initials: "ZZ" }
            : { kind: "agent", agent: "codex" },
        action,
        object: { kind: "gate", ref: "G-0012", id: "g12" },
        source:
          action === "answered" ? { kind: "memory", ref: "M-0447" } : null,
      });
    source.activity.mockResolvedValue({
      entries: [
        gate(1, "answered"),
        gate(2, "withdrawn"),
        gate(3, "asked"),
        // The board's handoff-era ID isn't a gate the API can address.
        entry(4, {
          actor: { kind: "agent", agent: "codex" },
          action: "asked",
          object: { kind: "gate", ref: "H-0093", id: "h93" },
        }),
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
    const links = screen
      .getAllByRole("link", { name: "G-0012" })
      .map((a) => a.getAttribute("href"));
    expect(links).toEqual([
      "/memax-v2/review?gate=G-0012",
      "/memax-v2/review?gate=G-0012",
      "/memax-v2/review?gate=G-0012",
    ]);
    const rows = screen.getAllByRole("listitem");
    expect(rows[0]!.textContent).toContain("You answered a question.");
    expect(rows[1]!.textContent).toContain(
      "Codex withdrew a question (G-0012).",
    );
    expect(screen.queryByRole("link", { name: "H-0093" })).toBeNull();
    rows[2]!.focus();
    fireEvent.keyDown(rows[2]!, { key: "Enter" });
    expect(push).toHaveBeenCalledWith("/memax-v2/review?gate=G-0012");
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
    expect(
      screen.getByText(
        "When an agent reads this space over MCP or the API, the read shows here.",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: "All" }));

    fireEvent.click(
      screen.getByRole("button", { name: /Load older activity/ }),
    );
    await screen.findByText("Sunday, September 20");
    expect(source.activity).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: "p2" }),
    );
    expect(screen.getAllByRole("listitem")).toHaveLength(4);
    expect(
      screen.queryByRole("button", { name: /Load older activity/ }),
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
    // Reads aren't receipts: the reads list counts its own week.
    expect(
      within(week).getByText("Reads").nextElementSibling?.textContent,
    ).toBe("57");
    const button = screen.getByRole("button", {
      name: "Export CSV · 3 loaded",
    });
    expect(button.title).toMatch(/^Exports the 3 receipts loaded on this page/);
    fireEvent.click(
      screen.getByRole("button", { name: /Load older activity/ }),
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

describe("Activity's reads and seal", () => {
  it("lists reads beside the receipts, and pages them on their own", async () => {
    source.activity.mockResolvedValue({
      entries: [
        entry(1),
        entry(2, { at: "2026-10-01T10:00:00-07:00", action: "proposed" }),
      ],
      nextCursor: null,
      totals: null,
    });
    source.reads.mockImplementation(({ cursor }: { cursor?: string }) =>
      Promise.resolve(
        cursor === "R2"
          ? {
              entries: [read(5, "2026-10-04T12:30:00-07:00")],
              nextCursor: null,
              week: 693,
            }
          : {
              entries: [
                read(3, "2026-10-05T14:30:00-07:00"),
                read(4, "2026-10-05T13:00:00-07:00"),
              ],
              nextCursor: "R2",
              week: 693,
            },
      ),
    );
    render(
      <Frame>
        <ActivityPlace />
      </Frame>,
    );
    const today = await screen.findByRole("list", { name: "Today" });
    // Newest first across both; Oct 1's receipt waits for older reads.
    expect(
      within(today)
        .getAllByRole("listitem")
        .map((r) => r.textContent),
    ).toEqual([
      expect.stringContaining("Cursor read 3 memories."),
      expect.stringContaining("You kept a memory."),
      expect.stringContaining("Cursor read 4 memories."),
    ]);
    expect(screen.getByText("R-5530")).toBeTruthy();
    expect(screen.queryByText("M-0002")).toBeNull();
    const week = screen.getByRole("region", { name: "This week" });
    expect(
      within(week).getByText("Reads").nextElementSibling?.textContent,
    ).toBe("693");

    fireEvent.click(screen.getByRole("radio", { name: "Reads" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: /Load older reads/ }));
    await screen.findByText("R-5550");
    expect(source.reads).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: "R2" }),
    );
    expect(screen.getAllByRole("listitem")).toHaveLength(3);

    // Every read is loaded now: All shows the older receipt too.
    fireEvent.click(screen.getByRole("radio", { name: "All" }));
    expect(screen.getByText("M-0002")).toBeTruthy();
    expect(screen.getAllByRole("listitem")).toHaveLength(5);
    // The CSV is receipts only.
    expect(screen.getByRole("button", { name: "Export CSV" })).toBeTruthy();
  });

  it("says how far the receipts are sealed, and when checkpoints aren't signed", async () => {
    source.seal.mockResolvedValue({ ...SEALED, signed: false });
    render(
      <Frame>
        <ActivityPlace />
      </Frame>,
    );
    const line = await screen.findByText(
      "Sealed through receipt 1,284, today at 14:02.",
    );
    expect(line.parentElement?.textContent).toBe(
      "Sealed through receipt 1,284, today at 14:02. Its checkpoints aren't signed: this server has no signing key.",
    );
    expect(line.parentElement?.getAttribute("title")).toMatch(
      /^Every receipt is chained by SHA-256/,
    );
  });

  it("shows the receipts when the reads don't load, and says so under Reads", async () => {
    source.reads.mockRejectedValue(new Error("down"));
    render(
      <Frame>
        <ActivityPlace />
      </Frame>,
    );
    await screen.findByRole("list", { name: "Today" });
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
    const week = screen.getByRole("region", { name: "This week" });
    expect(
      within(week).getByText("Reads").nextElementSibling?.textContent,
    ).toBe("—Not recorded yet");
    fireEvent.click(screen.getByRole("radio", { name: "Reads" }));
    expect(screen.getByText("The reads didn't load.")).toBeTruthy();
    source.reads.mockResolvedValue({
      entries: [read(3, "2026-10-05T14:30:00-07:00")],
      nextCursor: null,
      week: 1,
    });
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("R-5530")).toBeTruthy();
  });
});
