// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { DEMO_OVERVIEWS, DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { LedgerDataProvider } from "../../_lib/data";
import { OverlayProvider } from "../../_lib/overlays";
import { SpaceViewContext } from "../../_lib/space-context";
import { TodayPlace } from "./index";

// Today's "Waiting on you" with decision gates: the questions agents
// asked come first, each with its agent, its age and when it expires,
// and each opens its card in Review. A space with none reads as drawn.

const h = vi.hoisted(() => ({ source: null as LedgerDataSource | null }));

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
  useAuth: () => ({ user: null, loading: false }),
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/memax-team/today",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
});

const team = DEMO_SPACES.find((s) => s.slug === "memax-team")!;
const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

function renderToday(space: SpaceSummary) {
  h.source = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <LedgerProvider locale="en">
          <LedgerDataProvider mode="demo">
            <KeymapProvider>
              <ToastProvider>
                <OverlayProvider>
                  <SpaceViewContext
                    value={{
                      space,
                      overview:
                        h.source.peek?.overview(space.slug) ??
                        DEMO_OVERVIEWS[space.slug],
                      overviewFailed: false,
                      retryOverview: () => {},
                    }}
                  >
                    <TodayPlace />
                  </SpaceViewContext>
                  <ToastViewport />
                </OverlayProvider>
              </ToastProvider>
            </KeymapProvider>
          </LedgerDataProvider>
        </LedgerProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

const panel = () => screen.getByRole("region", { name: /^Waiting on you/ });

describe("Today's Waiting on you", () => {
  it("lists the questions agents asked first, each opening its card in Review", () => {
    renderToday(team);
    const waiting = panel();
    // Two proposals and two questions; the count says all four.
    expect(within(waiting).getByLabelText("4 waiting on you")).toBeTruthy();
    expect(within(waiting).getByText("2 questions · 2 proposals")).toBeTruthy();
    const rows = within(waiting).getAllByRole("listitem");
    // Two questions, then one of Review's items, then "1 more in Review".
    expect(rows).toHaveLength(3);
    const first = within(rows[0]!).getByRole("link", {
      name: "Keep the ChatGPT tool names, or align them with the core set?",
    });
    expect(first.getAttribute("href")).toBe("/memax-team/review?gate=G-0011");
    // The agent, how long ago it asked, and when it stops waiting.
    expect(rows[0]!.textContent).toContain("asked");
    expect(rows[0]!.textContent).toContain("1 h ago");
    expect(rows[0]!.textContent).toContain("G-0011");
    expect(rows[0]!.textContent).toContain("Expires tomorrow at 13:40");
    expect(rows[0]!.querySelector(".mx-stamp")?.textContent).toBe("CC");
    expect(
      within(rows[1]!)
        .getByRole("link", {
          name: "Which deploy target should the v2 API use?",
        })
        .getAttribute("href"),
    ).toBe("/memax-team/review?gate=G-0012");
    expect(rows[1]!.textContent).toContain("2 min ago");
    expect(rows[1]!.textContent).toContain("Expires Oct 12");
    // A question, not a proposal: roman, with the waiting mark.
    expect(rows[1]!.className).not.toContain("is-unconfirmed");
    expect(rows[1]!.querySelector(".mx-state")?.className).toContain(
      "mx-state--proposed",
    );
    expect(
      within(waiting).getByRole("link", { name: /1 more in Review/ }),
    ).toBeTruthy();
    // The lede names who's waiting.
    expect(
      screen.getByText(
        "Two proposals and two questions from Claude Code and Codex are waiting on you.",
      ),
    ).toBeTruthy();
  });

  it("reads as drawn where nothing asked (Main.png)", () => {
    renderToday(v2);
    const waiting = panel();
    expect(within(waiting).getByText("4 proposals · 1 to verify")).toBeTruthy();
    expect(waiting.textContent).not.toContain("G-0");
    expect(within(waiting).getAllByRole("listitem")).toHaveLength(3);
  });
});
