// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { AppFrame } from "../_components/app-frame";
import { SpacePlaces, type PlaceOnV1 } from "./space-on-v1";

// Every place of a space still on V1 but Today (the switch itself) says
// so, on the demo's acme-web: the place's header, the space on V1 and
// what the place holds once it's on V2, the switch for its owner (a link
// to Today) and V1 for anyone. Spaces on V2, Today and places a space
// doesn't have are left to their pages.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  path: "/acme-web/review",
}));

vi.mock("@/lib/v2/data/demo-source", async (load) => {
  const actual = await load<typeof import("@/lib/v2/data/demo-source")>();
  return {
    ...actual,
    get demoSource() {
      return h.source ?? actual.demoSource;
    },
  };
});
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: null, loading: false, session: null }),
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), prefetch: vi.fn() }),
  usePathname: () => h.path,
  useParams: () => ({ space: h.path.split("/")[1] }),
  useSearchParams: () => new URLSearchParams(),
}));

beforeEach(() => {
  h.source = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
});
afterEach(cleanup);

/** The demo's spaces, with acme-web (on V1) changed by `change`. */
function withV1Space(change: Partial<SpaceSummary>) {
  const demo = h.source!;
  const spaces = () =>
    demo
      .peek!.spaces()
      .map((s) => (s.slug === "acme-web" ? { ...s, ...change } : s));
  h.source = {
    ...demo,
    peek: { ...demo.peek!, spaces },
    spaces: async () => spaces(),
  };
}

/** The frame at `path`, around a page that says it rendered. */
function renderAt(path: string) {
  h.path = path;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <LedgerProvider locale="en">
          <AppFrame mode="demo">
            <SpacePlaces>
              <p>The place itself</p>
            </SpacePlaces>
          </AppFrame>
        </LedgerProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

const PLACES: Array<[PlaceOnV1, string, string]> = [
  [
    "review",
    "Review",
    "Once it's on V2, what agents propose waits here for a person to keep or reject.",
  ],
  [
    "brief",
    "Brief",
    "Once it's on V2, its Brief is what every agent reads, compiled into each one's file.",
  ],
  [
    "memories",
    "Memories",
    "Once it's on V2, what's kept in it is listed here with its receipts, and its V1 memories come along as notes.",
  ],
  [
    "handoffs",
    "Handoffs",
    "Once it's on V2, work handed from one agent to the next arrives here.",
  ],
  [
    "agents",
    "Agents",
    "Once it's on V2, its agents connect here, each at Read, Propose or Write.",
  ],
  [
    "decisions",
    "Decisions",
    "Once it's on V2, the decisions it keeps are recorded here, with why and what they rest on.",
  ],
  [
    "dream",
    "Dream",
    "Once it's on V2, Dream reads its notes overnight and publishes an edition here.",
  ],
  [
    "activity",
    "Activity",
    "Once it's on V2, every change to it leaves a receipt here.",
  ],
];

describe("a place of a space still on V1", () => {
  it.each(PLACES)(
    "%s says the space is on V1, and offers its owner the switch",
    (route, title, detail) => {
      renderAt(`/acme-web/${route}`);
      const main = screen.getByRole("main");
      expect(screen.queryByText("The place itself")).toBeNull();
      expect(within(main).getByRole("heading", { level: 1 }).textContent).toBe(
        title,
      );
      expect(within(main).getByText("acme-web · Team space")).toBeTruthy();
      const state = within(main).getByRole("heading", { level: 2 });
      expect(state.textContent).toBe("acme-web is still on V1.");
      expect(within(main).getByText(detail)).toBeTruthy();
      const links = within(main).getAllByRole("link");
      expect(links.map((a) => [a.textContent, a.getAttribute("href")])).toEqual(
        [
          ["Switch to V2", "/acme-web/today"],
          ["Open in V1", "/h/acme-web/memories"],
        ],
      );
      expect(
        within(main).getByText(
          "Switching changes nothing in V1, and you can switch back.",
        ),
      ).toBeTruthy();
      // None of the place's own commands: there is no record to act on.
      expect(within(main).queryAllByRole("button")).toEqual([]);
    },
  );

  it("covers a place's inner pages too", () => {
    for (const path of [
      "/acme-web/memories/M-0012",
      "/acme-web/brief/edit",
      "/acme-web/dream/3",
      "/acme-web/agents/claude-code",
    ]) {
      renderAt(path);
      expect(screen.getByText("acme-web is still on V1.")).toBeTruthy();
      expect(screen.queryByText("The place itself")).toBeNull();
      cleanup();
    }
  });

  it("tells a member or viewer that only its owner switches it", () => {
    for (const role of ["member", "viewer"] as const) {
      withV1Space({ role });
      renderAt("/acme-web/memories");
      const main = screen.getByRole("main");
      expect(within(main).getByText("acme-web is still on V1.")).toBeTruthy();
      expect(
        within(main).getByText("Only its owner can switch it to V2."),
      ).toBeTruthy();
      expect(
        within(main)
          .getAllByRole("link")
          .map((a) => [a.textContent, a.getAttribute("href")]),
      ).toEqual([["Open in V1", "/h/acme-web/memories"]]);
      cleanup();
    }
  });

  it("opens a personal space at V1's own name for it", () => {
    withV1Space({ kind: "personal", name: "Personal" });
    renderAt("/acme-web/review");
    expect(screen.getByText("Personal is still on V1.")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Open in V1" }).getAttribute("href"),
    ).toBe("/h/personal/memories");
  });

  it("leaves Today, spaces on V2 and places a space hasn't got to their pages", () => {
    // Today of a space on V1 is its switch (today/switch-to-v2.tsx).
    renderAt("/acme-web/today");
    expect(screen.getByText("The place itself")).toBeTruthy();
    cleanup();
    renderAt("/memax-v2/review");
    expect(screen.getByText("The place itself")).toBeTruthy();
    expect(screen.queryByText(/is still on V1/)).toBeNull();
    cleanup();
    // Decisions is a team space's; the page answers 404 elsewhere.
    withV1Space({ kind: "personal" });
    renderAt("/acme-web/decisions");
    expect(screen.getByText("The place itself")).toBeTruthy();
  });
});
