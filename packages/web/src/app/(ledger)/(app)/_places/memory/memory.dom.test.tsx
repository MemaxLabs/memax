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
import { DEMO_FOLD } from "@/lib/v2/data/demo-review-data";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { createUndoStack } from "@/lib/v2/undo-stack";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { LedgerDataProvider } from "../../_lib/data";
import { OverlayProvider } from "../../_lib/overlays";
import { SpaceViewContext } from "../../_lib/space-context";
import { UndoStackProvider } from "../../_lib/undo";
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

function renderMemory(
  ref: string,
  source: LedgerDataSource,
  space: SpaceSummary = v2,
) {
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
                        space,
                        overview: DEMO_OVERVIEWS[space.slug],
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
            </UndoStackProvider>
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

  it("offers Undo on an edit, which puts the words back", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    const source: LedgerDataSource = { ...demo, undo: vi.fn(demo.undo) };
    renderMemory("M-0098", source);
    await screen.findByRole("heading", { level: 1 });
    await act(async () => {});
    fireEvent.keyDown(document.body, { key: "e", code: "KeyE" });
    const field = await screen.findByRole("textbox", { name: "Statement" });
    fireEvent.change(field, {
      target: { value: "API errors are RFC 9457 problem+json, always." },
    });
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true });
    expect(await screen.findByText("Edited M-0098")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(
      await screen.findByText(
        "Undid your edit. M-0098 reads as it did before.",
      ),
    ).toBeTruthy();
    expect(source.undo).toHaveBeenCalledOnce();
  });
});

describe("a memory's reads and reach", () => {
  it("shows Memory.png's line: reads, files and agents", async () => {
    renderMemory(
      "M-0219",
      createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 }),
    );
    expect(
      await screen.findByText("Read 214 times · reaches 4 files and 5 agents"),
    ).toBeTruthy();
    expect(screen.getByText("4 files · 5 agents")).toBeTruthy();
  });

  it("says a count is a floor, and leaves out the files it doesn't know", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    const base = demo.memories.peekRecord!("memax-v2", "M-0098")!;
    // As the SDK source reads one whose targets didn't load.
    const record = {
      ...base,
      reads: 41,
      readsUnobserved: true,
      reach: { files: null, agents: 3 },
    };
    const source: LedgerDataSource = {
      ...demo,
      memories: {
        ...demo.memories,
        peekRecord: () => record,
        get: vi.fn().mockResolvedValue(record),
      },
    };
    renderMemory("M-0098", source);
    const meta = await screen.findByText(
      "Read at least 41 times · reaches 3 agents",
    );
    expect(meta.getAttribute("title")).toBe(
      "It's in a compiled file agents load without telling Memax, so it may be read more than this.",
    );
    expect(screen.getByText("3 agents")).toBeTruthy();
  });
});

describe("one of the judge's folds", () => {
  const team = DEMO_SPACES.find((s) => s.slug === "memax-team")!;

  it("says what it was folded into, and Undo puts it back in Review", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    const source: LedgerDataSource = { ...demo, undo: vi.fn(demo.undo) };
    renderMemory("M-0446", source, team);
    expect(await screen.findByText("Memax folded it into M-0310")).toBeTruthy();
    expect(screen.getByText("Undo until Oct 19")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Undo the fold of M-0446" }),
    );
    expect(
      await screen.findByText("Unfolded M-0446. It's back in Review."),
    ).toBeTruthy();
    expect(source.undo).toHaveBeenCalledWith(
      expect.objectContaining({ receipt: DEMO_FOLD.receipt }),
    );
    // Refetched: a proposal again, with no fold left to undo.
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Undo the fold of M-0446" }),
      ).toBeNull(),
    );
  });

  it("lists the fold on the memory it went into, with its Undo", async () => {
    renderMemory(
      "M-0310",
      createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 }),
      team,
    );
    expect(
      await screen.findByText("Reviews need a member who isn't the author."),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Undo the fold of M-0446" }),
    ).toBeTruthy();
  });

  it("offers a viewer no Undo", async () => {
    renderMemory(
      "M-0446",
      createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 }),
      { ...team, role: "viewer" },
    );
    expect(await screen.findByText("Memax folded it into M-0310")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Undo the fold/ })).toBeNull();
  });
});
