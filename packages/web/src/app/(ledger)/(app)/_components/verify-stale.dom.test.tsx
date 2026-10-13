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
import { LedgerProvider } from "@memaxlabs/ledger";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n";
import { DEMO_OVERVIEWS, DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { StaleWhy } from "@/lib/v2/data/memories";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { createUndoStack } from "@/lib/v2/undo-stack";
import { LedgerDataProvider } from "../_lib/data";
import { OverlayProvider, useOverlays } from "../_lib/overlays";
import { SpaceViewContext } from "../_lib/space-context";
import { UndoStackProvider } from "../_lib/undo";
import { ToastProvider, ToastViewport } from "./toasts";
import { VerifyStale } from "./verify-stale";

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
  usePathname: () => "/memax-v2/memories",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
});

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

function Opener({ memoryRef }: { memoryRef: string }) {
  const { openVerify } = useOverlays();
  return (
    <button type="button" onClick={() => openVerify(memoryRef)}>
      open
    </button>
  );
}

/** The demo source with Verify spied on, and the layer opened on a memory. */
async function open(
  memoryRef: string,
  patch?: Partial<LedgerDataSource["memories"]>,
) {
  const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  const verify = vi.fn(demo.memories.verify);
  const source: LedgerDataSource = {
    ...demo,
    memories: { ...demo.memories, verify, ...patch },
  };
  h.source = source;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <LedgerProvider locale="en">
          <LedgerDataProvider mode="demo">
            <UndoStackProvider value={createUndoStack()}>
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
                      <Opener memoryRef={memoryRef} />
                      <VerifyStale />
                    </SpaceViewContext>
                    <ToastViewport />
                  </OverlayProvider>
                </ToastProvider>
              </KeymapProvider>
            </UndoStackProvider>
          </LedgerDataProvider>
        </LedgerProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "open" }));
  await screen.findByRole("dialog");
  await act(async () => {});
  return { verify };
}

const key = (k: string, extra: Partial<KeyboardEventInit> = {}) =>
  fireEvent.keyDown(document.activeElement ?? document.body, {
    key: k,
    ...extra,
  });

describe("VerifyStale", () => {
  it("draws the board: what changed, three answers, the suggestion", async () => {
    await open("M-0187");
    expect(screen.getByRole("heading", { name: "Verify M-0187" })).toBeTruthy();
    expect(screen.getByText("Stale since Sep 11")).toBeTruthy();
    expect(screen.getByText("read 88 times since")).toBeTruthy();
    expect(screen.getByText("PR #198")).toBeTruthy();
    expect(screen.getByText(/TierSonnet/)).toBeTruthy();
    const radios = screen.getAllByRole("radio");
    expect(radios.map((r) => r.getAttribute("aria-checked"))).toEqual([
      "false",
      "true",
      "false",
    ]);
    expect(
      screen.getByText("Dream suggested the update from PR #198"),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /Keep the update/ }),
    ).toBeTruthy();
    expect(
      await screen.findByText(
        /Removed from .* and every agent\. A tombstone stays\./,
      ),
    ).toBeTruthy();
  });

  it("keeps the suggested update with ↵", async () => {
    const { verify } = await open("M-0187");
    key("Enter");
    await waitFor(() => expect(verify).toHaveBeenCalled());
    expect(verify.mock.calls[0]![0]).toMatchObject({
      ref: "M-0187",
      version: 1,
      answer: "update",
      statement: "Ask memax answers with the Sonnet tier.",
    });
    expect(
      await screen.findByText("Updated M-0187 and verified it."),
    ).toBeTruthy();
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Verify M-0187" }),
      ).toBeNull(),
    );
  });

  it("answers still true on 1, and forget on 3 with what goes with it", async () => {
    const { verify } = await open("M-0187");
    key("1");
    expect(screen.getAllByRole("radio")[0]!.getAttribute("aria-checked")).toBe(
      "true",
    );
    expect(screen.getByRole("button", { name: /Keep it/ })).toBeTruthy();
    key("3");
    const forget = screen.getByRole("button", { name: /Forget it/ });
    expect(forget.className).toContain("danger");
    key("1");
    key("Enter");
    await waitFor(() => expect(verify).toHaveBeenCalled());
    expect(verify.mock.calls[0]![0]).toMatchObject({ answer: "still_true" });
    expect(
      await screen.findByText("Verified M-0187. It's kept as it is."),
    ).toBeTruthy();
  });

  it("asks for an answer, and new words, when a date passed", async () => {
    const dated: StaleWhy = {
      ref: "M-0187",
      version: 1,
      statement: "Ask memax answers with the Haiku tier.",
      kind: "fact",
      stale: true,
      since: "2026-10-05T03:12:00Z",
      flaggedBy: { kind: "dream" },
      kept: {
        by: { kind: "person", self: true },
        at: "2026-07-14T11:20:00Z",
        action: "kept",
      },
      readsSince: 0,
      causes: [{ kind: "date", staleAfter: "2026-10-01T12:00:00Z" }],
      suggestion: null,
    };
    const { verify } = await open("M-0187", {
      stale: vi.fn().mockResolvedValue(dated),
      peekStale: () => dated,
    });
    expect(
      screen.getByText("Its date to check again, Oct 1, has passed."),
    ).toBeTruthy();
    expect(screen.getByText("not read since")).toBeTruthy();
    expect(
      screen
        .getAllByRole("radio")
        .every((r) => r.getAttribute("aria-checked") === "false"),
    ).toBe(true);
    const keep = screen.getByRole("button", { name: /Keep it/ });
    expect(keep.getAttribute("aria-disabled")).toBe("true");
    key("Enter");
    expect(verify).not.toHaveBeenCalled();
    key("2");
    const field = await screen.findByRole("textbox", {
      name: "What's true now, in your words",
    });
    expect((field as HTMLTextAreaElement).value).toBe(dated.statement);
    fireEvent.keyDown(field, { key: "Enter", metaKey: true });
    expect(
      await screen.findByText("Change the words first, or answer still true."),
    ).toBeTruthy();
    fireEvent.change(field, {
      target: { value: "Ask memax answers with the Sonnet tier." },
    });
    fireEvent.keyDown(field, { key: "Enter", metaKey: true });
    await waitFor(() => expect(verify).toHaveBeenCalled());
    expect(verify.mock.calls[0]![0]).toMatchObject({
      answer: "update",
      statement: "Ask memax answers with the Sonnet tier.",
    });
  });

  it("says what a check found: the path, where it went, the commit", async () => {
    const checked: StaleWhy = {
      ref: "M-0187",
      version: 1,
      statement:
        "Ask's tiers are set in packages/server/internal/ask/config.go.",
      kind: "fact",
      stale: true,
      since: "2026-10-05T09:01:00Z",
      flaggedBy: { kind: "memax" },
      kept: {
        by: { kind: "person", self: true },
        at: "2026-07-14T11:20:00Z",
        action: "kept",
      },
      readsSince: 3,
      causes: [
        {
          kind: "check",
          condition: {
            key: "c1",
            kind: "path_exists",
            path: "packages/server/internal/ask/config.go",
            glob: null,
          },
          change: "renamed",
          path: "packages/server/internal/ask/config.go",
          renamedTo: "packages/server/internal/ask/tiers.go",
          constraint: null,
          branch: "main",
          commit: "a1b2c3d".padEnd(40, "0"),
          checkedAt: "2026-10-05T09:00:00Z",
        },
      ],
      suggestion: null,
    };
    await open("M-0187", {
      stale: vi.fn().mockResolvedValue(checked),
      peekStale: () => checked,
    });
    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain(
      "packages/server/internal/ask/config.go was renamed to packages/server/internal/ask/tiers.go on main.",
    );
    expect(dialog.textContent).toContain("in a1b2c3d · checked Oct 5");
    expect(
      screen.getByText("It's checked again when the source next changes."),
    ).toBeTruthy();
  });
});
