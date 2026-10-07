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
import type { LineageFlag } from "@/lib/v2/data/memories";
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

describe("flags in a memory's lineage", () => {
  it("words a conflict flag as a conflict and a stale one as stale", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    const flag = (key: string, by: "memax" | "dream", flag: LineageFlag) => ({
      key,
      action: "flagged" as const,
      by: { kind: by },
      at: "2026-10-05T03:12:00Z",
      detail: null,
      count: null,
      to: null,
      flag,
    });
    const source: LedgerDataSource = {
      ...demo,
      memories: {
        ...demo.memories,
        // No first render from the demo's record: the page reads this one.
        peekRecord: undefined,
        get: async (input) => {
          const base = await demo.memories.get(input);
          return (
            base && {
              ...base,
              lineage: [
                flag("c", "memax", { kind: "conflict", with: "M-0174" }),
                flag("s", "dream", { kind: "stale" }),
              ],
            }
          );
        },
      },
    };
    renderMemory("M-0098", source);
    expect(
      await screen.findByText("Flagged by Memax as contradicting M-0174"),
    ).toBeTruthy();
    expect(screen.getByText("Flagged stale by Dream")).toBeTruthy();
    expect(screen.queryByText("Flagged stale by Memax")).toBeNull();
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

describe("Forget on a memory's page", () => {
  it("confirms inline with what it removes, then leaves the tombstone", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    const forget = vi.fn(demo.memories.forget);
    renderMemory("M-0219", {
      ...demo,
      memories: { ...demo.memories, forget },
    });
    await screen.findByRole("heading", { level: 1 });
    // Forget has no key, and it is never the default.
    fireEvent.click(screen.getByRole("button", { name: "Forget" }));
    expect(await screen.findByText("Forget this everywhere?")).toBeTruthy();
    expect(
      await screen.findByText(
        /^Removes the words from Memax, \d+ compiled files?, 1 copy-out and 5 agents\. A tombstone stays so you can see it happened\. This can't be undone\.$/,
      ),
    ).toBeTruthy();
    // Esc takes it back.
    fireEvent.keyDown(screen.getByRole("button", { name: /Cancel/ }), {
      key: "Escape",
    });
    await waitFor(() =>
      expect(screen.queryByText("Forget this everywhere?")).toBeNull(),
    );
    expect(forget).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Forget" }));
    const note = await screen.findByRole("textbox", {
      name: "A note for the tombstone",
    });
    fireEvent.change(note, { target: { value: "superseded by the ADR" } });
    fireEvent.click(
      await screen.findByRole("button", { name: "Forget M-0219" }),
    );
    await waitFor(() => expect(forget).toHaveBeenCalledOnce());
    expect(forget.mock.calls[0]![0]).toMatchObject({
      ref: "M-0219",
      version: 1,
      carries: [],
      note: "superseded by the ADR",
    });
    expect(
      await screen.findByText("Forgot M-0219. The tombstone stays."),
    ).toBeTruthy();
    // The page is the tombstone now: never the words.
    expect(await screen.findByText("How it was forgotten")).toBeTruthy();
    expect(screen.getByText("You asked to forget it")).toBeTruthy();
    expect(
      screen.queryByText(
        "Background jobs run on River, Postgres‑backed. We do not use Temporal.",
      ),
    ).toBeNull();
  });

  it("says why a person can't forget, before they try", async () => {
    const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
    renderMemory("M-0219", {
      ...demo,
      memories: {
        ...demo.memories,
        previewForget: vi.fn().mockResolvedValue({
          version: 1,
          carries: [{ ref: "M-0220", reason: "cites" }],
          files: 2,
          copies: 0,
          agents: 1,
          refusal: { code: "forget_not_allowed", message: "Only owners." },
        }),
      },
    });
    await screen.findByRole("heading", { level: 1 });
    fireEvent.click(screen.getByRole("button", { name: "Forget" }));
    expect(
      await screen.findByText(
        "M-0219 wasn't forgotten. Only owners forget in memax-v2. Ask an owner to forget it.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Removes the words from Memax, 2 compiled files and 1 agent. It takes M-0220 (cites it) with it. A tombstone stays so you can see it happened. This can't be undone.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Forget M-0219" })
        .getAttribute("aria-disabled"),
    ).toBe("true");
  });

  it("shows an agent's request to forget it, and keeps it", async () => {
    renderMemory(
      "M-0096",
      createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 }),
    );
    expect(
      await screen.findByText(
        /Codex asked you to forget it: “It repeats the API style guide word for word\.”/,
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep it" }));
    expect(
      await screen.findByText(
        "Kept M-0096. The request to forget it is closed.",
      ),
    ).toBeTruthy();
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Keep it" })).toBeNull(),
    );
  });

  it("reads a forgotten memory's page as its tombstone (Tombstone.png)", async () => {
    renderMemory(
      "M-0201",
      createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 }),
    );
    expect(await screen.findByText("How it was forgotten")).toBeTruthy();
    expect(
      screen.getByText(/^Forgotten Oct 3, \d\d:12, at your request$/),
    ).toBeTruthy();
    expect(
      screen.getByText("Kept Sep 21 · read 23 times before it was forgotten"),
    ).toBeTruthy();
    expect(
      screen.getByText("CLAUDE.md, AGENTS.md and Cursor rules rewritten"),
    ).toBeTruthy();
    expect(screen.getByText("Gemini CLI is paused")).toBeTruthy();
    expect(screen.getByText("The statement and its two sources")).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Out of Memax's reach" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Database backups, for 7 days. Memax never restores one without forgetting it again.",
      ),
    ).toBeTruthy();
    // Nothing to edit or forget on a tombstone.
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Forget" })).toBeNull();
  });
});
