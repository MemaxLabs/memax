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
import { DEMO_OVERVIEWS, DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import { DEMO_IMPORT_ID } from "@/lib/v2/data/demo-imports-data";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { LedgerDataProvider } from "../../_lib/data";
import { OverlayProvider } from "../../_lib/overlays";
import { SpaceViewContext } from "../../_lib/space-context";
import { EmptySpace } from "../today/empty-space";
import { useRecordsView } from "../records-view";
import { ReviewImportPlace } from "./index";

// ReviewImport (ReviewImport.png) on the demo's import: what can be kept
// in bulk, why the rest waits, and keeping the selection with K. And the
// empty space's way to follow init along.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  params: new URLSearchParams("filter=import"),
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
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/memax-v2/review",
  useSearchParams: () => h.params,
}));

beforeEach(() => {
  h.source = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
});
afterEach(cleanup);

function renderIn(slug: string, node: React.ReactNode) {
  const space = DEMO_SPACES.find((s) => s.slug === slug)!;
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
                      overview: DEMO_OVERVIEWS[slug],
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

const press = (key: string) =>
  act(() => {
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key, bubbles: true }),
    );
  });

describe("ReviewImport", () => {
  it("lists the import, the most files agreeing first, and why the rest waits", () => {
    renderIn("memax-v2", <ReviewImportPlace />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "33 imports from 6 files",
    );
    expect(
      screen.getByText(
        "Review · imported by npx memax-cli init · Oct 5 at 09:02",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("3 conflicts left · 1 held for review"),
    ).toBeTruthy();
    const filter = screen.getByRole("radiogroup", { name: "Filter by source" });
    expect(
      within(filter)
        .getAllByRole("radio")
        .map((r) => r.textContent),
    ).toEqual([
      "All 33",
      "CLAUDE.md 18",
      "AGENTS.md 9",
      "Cursor rules 6",
      "Codex 7",
    ]);
    expect(
      screen.getByText("Select the 25 that can be kept in bulk"),
    ).toBeTruthy();
    const rows = screen.getAllByRole("checkbox", { name: "Select it" });
    expect(rows).toHaveLength(12);
    expect(
      screen.getAllByText("3 files agree, so it's one proposal"),
    ).toHaveLength(3);
    fireEvent.click(screen.getByRole("button", { name: "Show 21 more" }));
    expect(
      screen.getByText(
        "Conflicts with AGENTS.md:8, testing.mdc:2. Settle it in the cleanup first.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByText(
          "Conflicts with AGENTS.md:8, testing.mdc:2. Settle it in the cleanup first.",
        )
        .getAttribute("href"),
    ).toBe(`/setup/cleanup?space=memax-v2&import=${DEMO_IMPORT_ID}`);
    expect(
      screen.getByText(
        "Codex read this on a web page. Check it against the source on its own.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("from modelcontextprotocol.io")).toBeTruthy();
    expect(
      screen.getAllByRole("checkbox", { name: "Can't be kept in bulk" }),
    ).toHaveLength(8);
  });

  it("keeps the selection with K, and says what happened", async () => {
    renderIn("memax-v2", <ReviewImportPlace />);
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Select the 25 that can be kept in bulk",
      }),
    );
    expect(screen.getByText("25 selected")).toBeTruthy();
    expect(
      screen.getByText(
        /from CLAUDE.md, AGENTS.md, Codex's memory and Cursor rules/,
      ),
    ).toBeTruthy();
    press("k");
    expect(
      await screen.findByText("Kept 25. Every file recompiles."),
    ).toBeTruthy();
    // What agreed is kept; the disagreements and the web statement wait.
    expect(
      await screen.findByText("Select the 0 that can be kept in bulk"),
    ).toBeTruthy();
    expect(
      screen.getAllByRole("checkbox", { name: "Can't be kept in bulk" }),
    ).toHaveLength(8);
  });

  it("rejects one with X, and filters by file", async () => {
    renderIn("memax-v2", <ReviewImportPlace />);
    fireEvent.click(
      within(
        screen.getByRole("radiogroup", { name: "Filter by source" }),
      ).getByRole("radio", { name: "Codex 7" }),
    );
    expect(screen.getAllByRole("checkbox", { name: "Select it" })).toHaveLength(
      5,
    );
    fireEvent.click(screen.getAllByRole("checkbox", { name: "Select it" })[0]!);
    expect(screen.getByText("1 selected")).toBeTruthy();
    press("x");
    expect(await screen.findByText("Rejected 1.")).toBeTruthy();
  });

  it("says when the space has no import", () => {
    renderIn("memax-web", <ReviewImportPlace />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Nothing imported here yet.",
    );
  });
});

function EmptyToday() {
  const view = useRecordsView();
  return <EmptySpace view={view} dreamAt={null} />;
}

describe("EmptySpace", () => {
  it("offers to follow init along on FirstRun", () => {
    renderIn("memax-web", <EmptyToday />);
    expect(
      screen
        .getByRole("link", { name: "Follow along as it runs" })
        .getAttribute("href"),
    ).toBe("/setup/import?space=memax-web");
    expect(
      screen.getByText("npx memax-cli init --space memax-web"),
    ).toBeTruthy();
  });
});
