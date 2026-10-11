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
import { MemaxError } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import { DEMO_V1_SPACE } from "@/lib/v2/data/demo-switch-data";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { LedgerDataProvider } from "../_lib/data";
import { OverlayProvider } from "../_lib/overlays";
import { REMEMBER_CHECK_DEBOUNCE_MS } from "../_lib/use-remember";
import { CommandCenter } from "./command-center";
import { FrameKeys } from "./frame-keys";
import { ToastProvider, ToastViewport } from "./toasts";

// ⌘K in a space still on V1 (policy space_on_v1): its V2 record takes no
// new words yet, so neither Ask nor Remember is offered, Enter and ⌘↵ do
// nothing, and the one way on is its Today. Remember elsewhere never
// offers it as a target, and a refusal that says so reads as one.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
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
  usePathname: () => "/acme-web/today",
  useSearchParams: () => new URLSearchParams(),
}));

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;
const spaces: SpaceSummary[] = [...DEMO_SPACES, DEMO_V1_SPACE];

beforeEach(() => {
  h.source = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function renderFrame(space: SpaceSummary) {
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
                  <FrameKeys space={space} spaces={spaces} route="today" />
                  <CommandCenter space={space} spaces={spaces} />
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

/** ⌘K (Ctrl+K off a Mac, as jsdom is): the dialog's one field. */
async function openCommand(): Promise<HTMLInputElement> {
  fireEvent.keyDown(document.body, { key: "k", ctrlKey: true });
  return (await screen.findByRole("textbox")) as HTMLInputElement;
}

describe("⌘K in a space still on V1", () => {
  it("offers no Ask or Remember: it says so and opens the space's Today", async () => {
    const ask = vi.spyOn(h.source!, "ask");
    const remember = vi.spyOn(h.source!, "remember");
    renderFrame(DEMO_V1_SPACE);
    const field = await openCommand();
    expect(field.readOnly).toBe(true);
    expect(field.placeholder).toBe("acme-web is still on V1");
    const notice = screen.getByRole("status");
    expect(notice.textContent).toContain("acme-web is still on V1");
    expect(notice.textContent).toContain("once you switch it to V2");
    const today = within(notice).getByRole("link", { name: "Open Today" });
    expect(today.getAttribute("href")).toBe("/acme-web/today");

    // Enter, Tab and ⌘↵ do nothing: no question asked, nothing kept.
    fireEvent.keyDown(field, { key: "Enter" });
    fireEvent.keyDown(field, { key: "Tab" });
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });
    expect(ask).not.toHaveBeenCalled();
    expect(remember).not.toHaveBeenCalled();
    expect(screen.getByRole("status").textContent).toContain(
      "acme-web is still on V1",
    );
  });

  it("tells someone who isn't the owner that the owner switches it", async () => {
    renderFrame({ ...DEMO_V1_SPACE, role: "member" });
    await openCommand();
    expect(screen.getByRole("status").textContent).toContain(
      "once its owner switches it to V2",
    );
  });
});

describe("Remember in a space on V2", () => {
  it("never offers a space still on V1 as where it goes", async () => {
    renderFrame(v2);
    const field = await openCommand();
    fireEvent.keyDown(field, { key: "Tab" });
    fireEvent.change(field, { target: { value: "Use pnpm catalogs." } });
    const picker = await screen.findByRole("radiogroup", { name: "Space" });
    const names = within(picker)
      .getAllByRole("radio")
      .map((r) => r.textContent);
    expect(names).toContain("memax-v2");
    expect(names).not.toContain("acme-web");
  });

  it("says so when the server finds the space back on V1", async () => {
    vi.spyOn(h.source!, "remember").mockRejectedValue(
      new MemaxError("memax-v2 is still on V1.", "refused", 403, {
        policy: { effect: "refuse", code: "space_on_v1" },
      }),
    );
    renderFrame(v2);
    const field = await openCommand();
    fireEvent.keyDown(field, { key: "Tab" });
    fireEvent.change(field, { target: { value: "Use pnpm catalogs." } });
    await act(async () => {
      await new Promise((resolve) =>
        setTimeout(resolve, REMEMBER_CHECK_DEBOUNCE_MS + 50),
      );
    });
    fireEvent.keyDown(field, { key: "Enter" });
    expect(
      await screen.findByText(
        "memax-v2 is still on V1, so nothing was kept. Its Today says how it switches to V2.",
      ),
    ).toBeTruthy();
  });
});
