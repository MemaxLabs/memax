// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CommandFailedError } from "@/lib/v2/data/command-error";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { renderPlace } from "../test-frame";
import { BriefEditPlace } from "./index";

// Editing the Brief over the demo source: ⌥↑ regroups the focused fact,
// ↵ edits it in place and ⌘↵ keeps the words, a new fact, and Done:
// remember, then edit, then revise, each with its own key, the same
// keys again when Done is pressed after a failure.

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
const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  usePathname: () => "/memax-v2/brief/edit",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
  push.mockReset();
});

function spied() {
  const demo = createDemoSource({
    streamDelayMs: 0,
    commandDelayMs: 0,
    settleMs: 5,
  });
  const order: string[] = [];
  const remember = vi.fn<LedgerDataSource["remember"]>(async (input) => {
    order.push("remember");
    return demo.remember(input);
  });
  const edit = vi.fn<LedgerDataSource["memories"]["edit"]>(async (input) => {
    order.push("edit");
    return demo.memories.edit(input);
  });
  const revise = vi.fn<LedgerDataSource["brief"]["revise"]>();
  revise
    .mockImplementationOnce(async () => {
      order.push("revise");
      throw new CommandFailedError({ kind: "unreachable" });
    })
    .mockImplementation(async (input) => {
      order.push("revise");
      return demo.brief.revise(input);
    });
  const source: LedgerDataSource = {
    ...demo,
    remember,
    memories: { ...demo.memories, edit },
    brief: { ...demo.brief, revise },
  };
  h.source = source;
  return { remember, edit, revise, order };
}

const row = (ref: string) =>
  document.querySelector<HTMLElement>(`[data-fact="${ref}"]`)!;

describe("editing the Brief", () => {
  it("regroups, edits, adds, and Done keeps it all in order, retrying with the same keys", async () => {
    const { remember, edit, revise, order } = spied();
    renderPlace(<BriefEditPlace />);
    await screen.findByRole("heading", { name: "Editing the Brief" });
    await act(async () => {});

    // ⌥↑ twice: past the top of Conventions, into Decisions.
    row("M-0112").focus();
    fireEvent.keyDown(row("M-0112"), {
      key: "ArrowUp",
      code: "ArrowUp",
      altKey: true,
    });
    fireEvent.keyDown(row("M-0112"), {
      key: "ArrowUp",
      code: "ArrowUp",
      altKey: true,
    });
    // The moved fact keeps the focus, a frame later.
    await act(
      () => new Promise<void>((done) => requestAnimationFrame(() => done())),
    );
    expect(document.activeElement).toBe(row("M-0112"));
    expect(screen.getByText("Moved here from Conventions")).toBeTruthy();
    const changes = screen.getByRole("region", { name: /^Changes/ });
    expect(
      within(changes).getByText("Moved · M-0112 to Decisions"),
    ).toBeTruthy();

    // ↵ edits M-0102 in place; ⌘↵ keeps the words for Done.
    fireEvent.keyDown(row("M-0102"), { key: "Enter" });
    const words = await screen.findByRole("textbox", {
      name: "Edit fact M-0102",
    });
    fireEvent.change(words, {
      target: {
        value:
          "Remote MCP is stateless. Sessions are never pinned to a machine.",
      },
    });
    fireEvent.keyDown(words, { key: "Enter", ctrlKey: true });
    expect(
      screen.queryByRole("textbox", { name: "Edit fact M-0102" }),
    ).toBeNull();
    expect(within(changes).getByText("Edited · M-0102")).toBeTruthy();

    // A new fact in Conventions.
    const conventions = screen
      .getByRole("textbox", { name: "Heading of Conventions" })
      .closest("section")!;
    fireEvent.click(
      within(conventions).getByRole("button", { name: "Add a fact" }),
    );
    const fresh = await screen.findByRole("textbox", {
      name: "Add a fact to Conventions",
    });
    fireEvent.change(fresh, {
      target: {
        value: "Pin shared dependency versions with the pnpm catalog.",
      },
    });
    expect(
      screen.getByRole("button", { name: "Discard 3 changes" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Done keeps all three with your receipt and recompiles 3 files. Dream won't undo a person's edit; it can only propose.",
      ),
    ).toBeTruthy();

    // Done: the revise fails once (it may have landed), so nothing moves on.
    fireEvent.click(screen.getByRole("button", { name: /^Done/ }));
    expect(
      await screen.findByText(
        "The new facts were kept, but the Brief wasn't changed. Press Done to try again.",
      ),
    ).toBeTruthy();
    expect(order).toEqual(["remember", "edit", "revise"]);
    expect(remember.mock.calls[0]![0]).toMatchObject({
      statement: "Pin shared dependency versions with the pnpm catalog.",
      section: "conventions",
    });
    expect(edit.mock.calls[0]![0]).toMatchObject({
      ref: "M-0102",
      version: 1,
      statement:
        "Remote MCP is stateless. Sessions are never pinned to a machine.",
    });
    const sent = revise.mock.calls[0]![0];
    expect(sent.base).toBe(6);
    const decisions = sent.structure.sections.find(
      (s) => s.key === "decisions",
    )!;
    expect(decisions.items.at(-1)).toEqual({ ref: "M-0112" });
    const placed = sent.structure.sections.find(
      (s) => s.key === "conventions",
    )!;
    expect(placed.items.at(-1)).toEqual({ ref: "M-0439" });

    // Done again: the same commands, the same keys; the server replays.
    fireEvent.click(screen.getByRole("button", { name: /^Done/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/memax-v2/brief"));
    expect(order).toEqual([
      "remember",
      "edit",
      "revise",
      "remember",
      "edit",
      "revise",
    ]);
    expect(remember.mock.calls[1]![0].idempotencyKey).toBe(
      remember.mock.calls[0]![0].idempotencyKey,
    );
    expect(edit.mock.calls[1]![0].idempotencyKey).toBe(
      edit.mock.calls[0]![0].idempotencyKey,
    );
    expect(revise.mock.calls[1]![0].idempotencyKey).toBe(sent.idempotencyKey);
    expect(await screen.findByText("Kept your edits as B-0044")).toBeTruthy();
  });

  it("won't keep a line of prose that cites nothing kept", async () => {
    const { revise } = spied();
    renderPlace(<BriefEditPlace />);
    await screen.findByRole("heading", { name: "Editing the Brief" });
    await act(async () => {});
    fireEvent.keyDown(row("prose-0"), { key: "Enter" });
    fireEvent.click(
      await screen.findByRole("button", { name: "Take out M-0001" }),
    );
    fireEvent.click(screen.getByRole("button", { name: /^Done/ }));
    expect(
      await screen.findByText(
        "A line of prose must cite at least one kept memory.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("Fix the lines marked below, then press Done."),
    ).toBeTruthy();
    expect(revise).not.toHaveBeenCalled();
  });
});
