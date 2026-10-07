// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CommandFailedError } from "@/lib/v2/data/command-error";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { DreamSource } from "@/lib/v2/data/dream";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { DreamSettingsPanel } from "../../settings/account/dream-settings";
import { renderPlace } from "../test-frame";
import { TodayPlace } from "../today";
import { DreamPlace } from "./index";

// Edition No. 214 (DreamEdition.png) over the demo source, its Dream
// commands spied on: every section of the board, Undo on a fold, Restore
// all on what faded, a refusal said in words, and an empty space.

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
  usePathname: () => "/memax-v2/dream/214",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
  push.mockReset();
});

function withDream(
  over: (real: DreamSource) => Partial<DreamSource> = () => ({}),
): LedgerDataSource {
  const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  const source = { ...demo, dream: { ...demo.dream, ...over(demo.dream) } };
  h.source = source;
  return source;
}

const section = (name: string) =>
  screen.getByRole("region", { name: new RegExp(`^${name}`) });

describe("a Dream edition", () => {
  it("draws the board: header, sections, last night and earlier editions", () => {
    withDream();
    renderPlace(<DreamPlace editionRef="214" />);
    expect(screen.getByText("memax-v2 · Dream edition No. 214")).toBeTruthy();
    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Monday, October 5, overnight",
      }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Dream read 34 notes from three agents and two chats between 18:00 and 03:00. Everything it changed is listed here, and every change can be undone.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /Resolve the conflict/ }),
    ).toHaveProperty(
      "href",
      expect.stringContaining("/memax-v2/review/M-0431/compare"),
    );

    const folded = section("Folded into what you kept");
    expect(within(folded).getByText("13 notes")).toBeTruthy();
    expect(
      within(folded).getByRole("button", { name: "Undo both" }),
    ).toBeTruthy();
    expect(
      within(folded).getByText(
        "Dream folded 9 notes into it: N-1180 to N-1188",
      ),
    ).toBeTruthy();
    expect(
      within(folded).getByText("Dream folded 4 notes into it"),
    ).toBeTruthy();

    const facts = section("New facts");
    expect(within(facts).getByText("4 from 18 notes")).toBeTruthy();
    expect(within(facts).getByRole("link", { name: /Review 3/ })).toBeTruthy();
    expect(
      within(facts).getByText(
        "From 5 notes by Codex and Cursor · waiting in Review",
      ),
    ).toBeTruthy();
    expect(
      within(facts).getByText("From 4 notes in two chats · waiting in Review"),
    ).toBeTruthy();
    expect(
      within(facts).getByText(
        "From 6 notes by Claude Code, which may write here",
      ),
    ).toBeTruthy();

    const needs = section("Needs you");
    expect(
      within(needs).getByText(
        "Contradicts M-0174, kept by Jiahao. Compare them side by side.",
      ),
    ).toBeTruthy();
    expect(
      within(needs).getByText(
        "Its source changed on Sep 11 in PR #198. Verify it.",
      ),
    ).toBeTruthy();

    const faded = section("Faded");
    expect(within(faded).getByText("11 unread for 60 days")).toBeTruthy();
    expect(within(faded).getByRole("button", { name: "9 more" })).toBeTruthy();

    const last = section("Last night");
    expect(within(last).getByText("Two chats")).toBeTruthy();
    expect(within(last).getByText("03:12 · 41 s")).toBeTruthy();
    const earlier = section("Earlier editions");
    expect(within(earlier).getAllByRole("link")).toHaveLength(3);
    expect(within(earlier).getByText("12 notes became 3 facts.")).toBeTruthy();
    expect(
      screen.getByText(
        "Next edition Tuesday at 03:00. Dream never keeps anything an agent could not have kept itself.",
      ),
    ).toBeTruthy();
  });

  it(
    "undoes a fold, and restores everything that faded",
    { timeout: 20_000 },
    async () => {
      const undo = vi.fn();
      const undoAll = vi.fn();
      withDream((real) => ({
        undo: async (input) => {
          undo(input);
          return real.undo(input);
        },
        undoAll: async (input) => {
          undoAll(input);
          return real.undoAll(input);
        },
      }));
      renderPlace(<DreamPlace editionRef="latest" />);
      const folded = section("Folded into what you kept");
      const rows = within(folded).getAllByRole("listitem");
      fireEvent.click(within(rows[0]!).getByRole("button", { name: "Undo" }));
      await screen.findByText("Undid it. M-0219 is as it was before Dream.");
      expect(undo).toHaveBeenCalledTimes(1);
      expect(undo.mock.calls[0]![0].action.kind).toBe("fold");
      // One fold left: its section says Undo, not Undo both.
      await waitFor(() => {
        const head = section("Folded into what you kept").querySelector(
          "header",
        )!;
        expect(within(head).getByText("Undo")).toBeTruthy();
      });

      fireEvent.click(
        within(section("Faded")).getByRole("button", { name: "Restore all" }),
      );
      await screen.findByText("Restored 11 memories. They compile again.");
      expect(undoAll.mock.calls[0]![0]).toMatchObject({
        edition: "D-0214",
        kind: "fade",
      });
      await waitFor(() =>
        expect(screen.queryByRole("region", { name: /^Faded/ })).toBeNull(),
      );
    },
  );

  it("says why an undo was refused, in words", async () => {
    withDream(() => ({
      undo: async () => {
        throw new CommandFailedError({
          kind: "undo-refused",
          reason: "later_changes",
          ref: "M-0219",
        });
      },
    }));
    renderPlace(<DreamPlace editionRef="214" />);
    const rows = within(section("Folded into what you kept")).getAllByRole(
      "listitem",
    );
    fireEvent.click(within(rows[0]!).getByRole("button", { name: "Undo" }));
    await screen.findByText(
      "M-0219 changed after Dream acted, so undoing it would lose that change. Change it on its page instead.",
    );
  });

  it("says when a space has no edition yet", async () => {
    withDream(() => ({
      edition: async () => null,
      peekEdition: () => null,
    }));
    renderPlace(<DreamPlace editionRef="latest" />);
    expect(await screen.findByText("No edition yet")).toBeTruthy();
  });

  it("opens from Today's card", async () => {
    withDream();
    renderPlace(<TodayPlace />);
    const open = await screen.findByRole("link", {
      name: "Read edition No. 214",
    });
    expect(open.getAttribute("href")).toBe("/memax-v2/dream/214");
  });

  it("changes the morning email and the zone in Settings", async () => {
    const update = vi.fn();
    withDream((real) => ({
      updateSettings: async (input) => {
        update(input);
        return real.updateSettings(input);
      },
    }));
    renderPlace(<DreamSettingsPanel />);
    const panel = screen.getByRole("region", { name: "Dream" });
    fireEvent.click(within(panel).getByRole("radio", { name: "Off" }));
    await screen.findByText("Dream settings changed.");
    expect(update.mock.calls[0]![0]).toMatchObject({ morningEmail: false });
    fireEvent.change(
      within(panel).getByRole("combobox", { name: "Time zone" }),
      {
        target: { value: "Europe/Paris" },
      },
    );
    await waitFor(() =>
      expect(update.mock.calls[1]?.[0]).toMatchObject({
        timeZone: "Europe/Paris",
      }),
    );
  });
});
