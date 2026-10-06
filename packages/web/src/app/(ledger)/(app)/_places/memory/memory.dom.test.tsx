// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemaxError } from "memax-sdk";
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import {
  DEMO_NOW,
  DEMO_OVERVIEWS,
  DEMO_SPACES,
} from "@/lib/v2/data/demo-dataset";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { LedgerDataProvider } from "../../_lib/data";
import { OverlayProvider } from "../../_lib/overlays";
import { SpaceViewContext } from "../../_lib/space-context";
import { MemoryPlace } from "./index";

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
  usePathname: () => "/memax-v2/memories/M-0098",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
});

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

function renderMemory(ref: string, source: LedgerDataSource) {
  h.source = source;
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
                      space: v2,
                      overview: DEMO_OVERVIEWS["memax-v2"],
                      overviewFailed: false,
                      retryOverview: () => {},
                    }}
                  >
                    <MemoryPlace memoryRef={ref} />
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

describe("a memory's page", () => {
  it("settles an edit clash with Keep mine, from their version", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    const edit = vi
      .fn()
      .mockRejectedValueOnce(
        new MemaxError("changed", "edit_clash", 412, { current_version: 2 }),
      )
      .mockResolvedValue({
        ref: "M-0098",
        outcome: "edited",
        version: 3,
        recompiled: null,
      });
    const source: LedgerDataSource = {
      ...demo,
      memories: {
        ...demo.memories,
        edit,
        latest: vi.fn().mockResolvedValue({
          version: 2,
          statement:
            "API errors are RFC 9457 problem+json, never bare strings.",
          by: { kind: "person", self: false, initials: "JY", name: "Jiahao" },
          at: new Date(new Date(DEMO_NOW).getTime() - 60_000).toISOString(),
        }),
      },
    };
    renderMemory("M-0098", source);
    await screen.findByRole("heading", { level: 1 });
    await act(async () => {});
    fireEvent.keyDown(document.body, { key: "e", code: "KeyE" });
    const field = await screen.findByRole("textbox", { name: "Statement" });
    fireEvent.change(field, {
      target: {
        value:
          "API errors are RFC 9457 problem+json, including from MCP tools.",
      },
    });
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true });
    expect(
      await screen.findByText(
        "Jiahao kept a change to this fact 1 minute ago.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Yours: “API errors are RFC 9457 problem+json, including from MCP tools.”",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep mine" }));
    await waitFor(() => expect(edit).toHaveBeenCalledTimes(2));
    const [first, second] = edit.mock.calls.map((call) => call[0]);
    expect(first).toMatchObject({ version: 1 });
    expect(second).toMatchObject({
      version: 2,
      statement:
        "API errors are RFC 9457 problem+json, including from MCP tools.",
    });
    expect(second.keep).toBeUndefined();
    expect(second.idempotencyKey).not.toBe(first.idempotencyKey);
    expect(await screen.findByText("Edited M-0098")).toBeTruthy();
  });

  it("copies the citation with ⌘⇧C: the ID and its link", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderMemory(
      "M-0219",
      createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 }),
    );
    await screen.findByRole("heading", { level: 1 });
    await act(async () => {});
    fireEvent.keyDown(document.body, {
      key: "C",
      code: "KeyC",
      ctrlKey: true,
      shiftKey: true,
    });
    await waitFor(() => expect(writeText).toHaveBeenCalledOnce());
    expect(writeText.mock.calls[0][0]).toMatch(
      /^\[M-0219\] https?:\/\/[^/]+\/memax-v2\/memories\/M-0219$/,
    );
    expect(
      await screen.findByText("Copied [M-0219] and its link"),
    ).toBeTruthy();
  });

  it("says when there's no memory with this ID", async () => {
    renderMemory(
      "M-9999",
      createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 }),
    );
    expect(
      await screen.findByText(
        "There's no memory with this ID that you can open.",
      ),
    ).toBeTruthy();
  });
});
