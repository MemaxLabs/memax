// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { AppFrame } from "./app-frame";

// The app frame around a place, on the demo: the rail's status line says
// what is true of the space (a space still on V1 says so, without reading
// a V2 record it hasn't got), "Loading" while the overview is read and
// that it didn't load when the read failed, never loading forever.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  path: "/memax-v2/today",
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

/** The frame at `path`, around `page`. */
function renderFrame(path: string, page: React.ReactNode = null) {
  h.path = path;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <LedgerProvider locale="en">
          <AppFrame mode="demo">{page}</AppFrame>
        </LedgerProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

function railStatus(): HTMLElement {
  const rail = screen.getByRole("navigation", { name: "Main" });
  return within(rail).getByRole("status");
}

describe("the rail's status line", () => {
  it("says a space still on V1 is on V1, without reading its overview", async () => {
    const overview = vi.spyOn(h.source!, "overview");
    renderFrame("/acme-web/today");
    expect(railStatus().textContent).toBe("Still on V1");
    expect(overview).not.toHaveBeenCalled();
  });

  it("says what the overview says for a space on V2", () => {
    renderFrame("/memax-v2/today");
    expect(railStatus().textContent).toBe("Cursor file drifted");
  });

  it("says Loading while the overview is read, and when it didn't load", async () => {
    const source = h.source!;
    let fail: (e: Error) => void = () => {};
    h.source = {
      ...source,
      // No first-render data: the SDK source's case.
      peek: undefined,
      overview: () =>
        new Promise((_, reject) => {
          fail = reject;
        }),
    };
    renderFrame("/memax-v2/today");
    expect((await screen.findByText("Loading")).closest("[role=status]")).toBe(
      railStatus(),
    );
    fail(new Error("down"));
    expect(await screen.findByText("Status didn't load")).toBeTruthy();
    expect(railStatus().textContent).toBe("Status didn't load");
  });
});
