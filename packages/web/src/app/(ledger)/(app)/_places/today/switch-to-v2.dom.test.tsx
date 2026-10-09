// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { DEMO_OVERVIEWS } from "@/lib/v2/data/demo-dataset";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import {
  DEMO_V1_IMPORT_ID,
  DEMO_V1_SPACE,
} from "@/lib/v2/data/demo-switch-data";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { LedgerDataProvider } from "../../_lib/data";
import { OverlayProvider } from "../../_lib/overlays";
import { SpaceViewContext } from "../../_lib/space-context";
import { ReviewImportPlace } from "../review-import";
import { TodayPlace } from "./index";

// Switch to V2 on the demo's space still on V1 (acme-web): Today says
// what moves and switches it for the owner, follows the switch running
// in the background, then opens Review's "From V1", where the person's
// own V1 memories are kept in one go.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  params: new URLSearchParams(),
  push: vi.fn(),
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
  useAuth: () => ({ user: null, loading: false }),
  getAccessToken: () => null,
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: h.push, replace: vi.fn() }),
  usePathname: () => "/acme-web/today",
  useSearchParams: () => h.params,
}));

beforeEach(() => {
  h.source = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  h.params = new URLSearchParams();
  h.push.mockReset();
});
afterEach(cleanup);

function renderIn(space: SpaceSummary, node: React.ReactNode) {
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
                      overview: DEMO_OVERVIEWS[space.slug],
                      overviewFailed: false,
                      retryOverview: () => {},
                    }}
                  >
                    {node}
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

describe("Switch to V2", () => {
  it("says what moves, and switches for the owner", async () => {
    renderIn(DEMO_V1_SPACE, <TodayPlace />);
    expect((await screen.findByRole("heading", { level: 1 })).textContent).toBe(
      "acme-web is still on V1",
    );
    const moves = screen.getByRole("region", { name: "What moves" });
    const terms = within(moves)
      .getAllByRole("term")
      .map((t) => t.textContent);
    expect(terms).toEqual([
      "Notes",
      "Review",
      "Dream",
      "Notes only",
      "People",
      "Agents",
      "Files",
      "Decisions",
      "History",
      "Plan",
    ]);
    expect(
      within(moves).getByText(
        "7 you wrote, one statement each, wait in Review to keep in one go.",
      ),
    ).toBeTruthy();
    // A team hub may switch as a project space.
    const as = screen.getByRole("radiogroup", { name: "Switch as" });
    fireEvent.click(within(as).getByRole("radio", { name: "Project" }));

    fireEvent.click(screen.getByRole("button", { name: "Switch to V2" }));
    expect(
      await screen.findByText(
        "Switching acme-web. Numbering its notes and proposing what you wrote. It goes on if you leave this page.",
      ),
    ).toBeTruthy();
    // It lands on the next read, and opens Review's "From V1".
    expect(
      await screen.findByText("acme-web is on V2.", {}, { timeout: 3000 }),
    ).toBeTruthy();
    expect(h.push).toHaveBeenCalledWith(
      `/acme-web/review?filter=import&import=${DEMO_V1_IMPORT_ID}`,
    );
  });

  it("tells someone who isn't the owner who can switch", async () => {
    renderIn({ ...DEMO_V1_SPACE, role: "member" }, <TodayPlace />);
    expect(
      await screen.findByText("Only the space's owner can switch it."),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Switch to V2" })).toBeNull();
    expect(screen.queryByRole("radiogroup", { name: "Switch as" })).toBeNull();
  });
});

describe("ReviewImport, from V1", () => {
  it("offers the person's own V1 memories to keep in one go", async () => {
    const source = h.source!;
    await source.switch.toV2({
      space: DEMO_V1_SPACE,
      idempotencyKey: "switch",
    });
    await source.switch.status({ space: DEMO_V1_SPACE });
    h.params = new URLSearchParams(`filter=import&import=${DEMO_V1_IMPORT_ID}`);
    renderIn({ ...DEMO_V1_SPACE, onV2: true }, <ReviewImportPlace />);
    expect((await screen.findByRole("heading", { level: 1 })).textContent).toBe(
      "You wrote these 7 in V1. Keep them in one go?",
    );
    expect(
      screen.getByText("Review · from V1 · switched Oct 6 at 08:40"),
    ).toBeTruthy();
    expect(screen.getByText("In the order you wrote them")).toBeTruthy();
    // Each cites its note; no file groups.
    expect(screen.getByText("N-0001")).toBeTruthy();
    expect(
      screen.queryByRole("radiogroup", { name: "Filter by source" }),
    ).toBeNull();
    expect(
      screen.getByText("Memax is still checking it against the rest."),
    ).toBeTruthy();

    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Select the 6 that can be kept in bulk",
      }),
    );
    expect(screen.getByText("6 selected")).toBeTruthy();
    expect(screen.queryByText(/^from /)).toBeNull();
    act(() => {
      document.dispatchEvent(
        new KeyboardEvent("keydown", { key: "k", bubbles: true }),
      );
    });
    expect(
      await screen.findByText("Kept 6. Every file recompiles."),
    ).toBeTruthy();
  });
});
