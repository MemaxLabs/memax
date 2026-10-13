// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useOverlays } from "../../_lib/overlays";
import { renderPlace } from "../test-frame";
import { SearchPlace } from ".";

// Search (Search.png) on the demo's search: the board's matches for
// "deploy" by kind, the facets and their counts, raw notes off until
// asked for, Ask instead (⌘↵), and what it says when there's nothing.

const h = vi.hoisted(() => ({
  query: "deploy",
  replace: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: h.replace, prefetch: vi.fn() }),
  usePathname: () => "/memax-v2/search",
  useParams: () => ({ space: "memax-v2" }),
  useSearchParams: () => new URLSearchParams(h.query ? { q: h.query } : {}),
}));

/** Shows what ⌘K was opened with. */
function CommandProbe() {
  const { command } = useOverlays();
  return command.open ? (
    <output aria-label="command">{`${command.mode}: ${command.query}`}</output>
  ) : null;
}

function renderSearch(query: string) {
  h.query = query;
  renderPlace(
    <>
      <SearchPlace />
      <CommandProbe />
    </>,
  );
}

const text = (el: Element | null | undefined) => el?.textContent ?? "";
const checked = (el: HTMLElement) => (el as HTMLInputElement).checked;
const meta = () => text(screen.getByRole("status"));
const panel = (name: string) => screen.getByRole("region", { name });
const facets = () => screen.getByRole("complementary", { name: "Filters" });
const facet = (name: RegExp) =>
  within(facets()).getByRole("checkbox", { name });
const command = () => text(screen.getByRole("status", { name: "command" }));

beforeEach(() => {
  h.replace.mockReset();
});
afterEach(cleanup);

describe("Search", () => {
  it("lists the board's matches by kind, with their counts", async () => {
    renderSearch("deploy");
    await waitFor(() =>
      expect(meta()).toBe(
        "8 results for “deploy” in three spaces · 9 raw notes hidden",
      ),
    );
    const memories = panel("Memories");
    expect(text(within(memories).getByRole("heading"))).toBe("Memories 6");
    // Four, then "2 more".
    expect(within(memories).getAllByRole("listitem")).toHaveLength(4);
    fireEvent.click(within(memories).getByRole("button", { name: "2 more" }));
    expect(within(memories).getAllByRole("listitem")).toHaveLength(6);
    // The words that matched wear the highlighter.
    const first = within(memories).getAllByRole("listitem")[0];
    expect(text(first.querySelector("mark"))).toBe("Deploy");
    expect(text(first)).toContain("M-0431 · memax-v2");

    const decisions = panel("Decisions");
    expect(text(within(decisions).getByRole("heading"))).toBe(
      "Decisions 2 in Memax team",
    );
    expect(within(decisions).getByText("Open")).toBeTruthy();
    expect(within(decisions).getByText("In force")).toBeTruthy();

    expect(checked(facet(/Raw notes/))).toBe(false);
    expect(checked(facet(/Memories/))).toBe(true);
    // The spaces, the most found first; memax-web has nothing in it.
    const spaces = within(facets()).getByRole("group", { name: "Space" });
    expect(
      within(spaces)
        .getAllByRole("checkbox")
        .map((c) => text(c.closest("label"))),
    ).toEqual(["memax-v25", "Memax team3", "Personal0"]);
  });

  it("filters what it found, without asking again", async () => {
    renderSearch("deploy");
    await screen.findByRole("complementary", { name: "Filters" });
    // Raw notes on: the person's notes, and nothing hidden.
    fireEvent.click(facet(/Raw notes/));
    expect(text(panel("Raw notes"))).toContain("N-0871");
    expect(meta()).toBe("17 results for “deploy” in three spaces");

    // Memax team off: its decisions go, and two spaces are left.
    fireEvent.click(facet(/Memax team/));
    expect(screen.queryByRole("region", { name: "Decisions" })).toBeNull();
    expect(meta()).toContain("in two spaces");

    // What you wrote, off.
    const you = within(facets()).getByRole("button", {
      name: "What You wrote",
    });
    expect(you.getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(you);
    expect(you.getAttribute("aria-pressed")).toBe("false");
    expect(text(panel("Memories"))).not.toContain("Workers deploy");

    // Everything off: nothing left, and a way back.
    fireEvent.click(facet(/Memories/));
    fireEvent.click(facet(/Raw notes/));
    expect(
      screen.getByRole("heading", { name: "Nothing here with these filters." }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show everything" }));
    expect(text(panel("Memories"))).toContain("Workers deploy");
  });

  it("asks the words instead, from the bar", async () => {
    renderSearch("deploy");
    await screen.findByRole("region", { name: "Memories" });
    fireEvent.click(
      screen.getByRole("button", { name: "Ask memax-v2 instead: deploy" }),
    );
    expect(command()).toBe("ask: deploy");
  });

  it("asks with ⌘↵ from the field", async () => {
    renderSearch("deploy");
    await screen.findByRole("region", { name: "Memories" });
    const field = screen.getByRole("searchbox", { name: "Search everything" });
    fireEvent.change(field, { target: { value: "fly regions" } });
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true });
    expect(command()).toBe("ask: fly regions");
  });

  it("moves the address once typing settles, at once on Enter", () => {
    vi.useFakeTimers();
    try {
      renderSearch("deploy");
      const field = screen.getByRole("searchbox", {
        name: "Search everything",
      });
      fireEvent.change(field, { target: { value: "railway" } });
      expect(h.replace).not.toHaveBeenCalled();
      act(() => {
        vi.advanceTimersByTime(300);
      });
      expect(h.replace).toHaveBeenCalledWith("/memax-v2/search?q=railway", {
        scroll: false,
      });
      h.replace.mockReset();
      fireEvent.change(field, { target: { value: "fly" } });
      fireEvent.keyDown(field, { key: "Enter" });
      expect(h.replace).toHaveBeenCalledWith("/memax-v2/search?q=fly", {
        scroll: false,
      });
    } finally {
      vi.useRealTimers();
    }
  });

  it("says what it searches before anything is typed", () => {
    renderSearch("");
    expect(
      screen.getByRole("heading", { name: "Search your spaces." }),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: /instead/ })).toBeNull();
  });

  it("says when nothing matches, and offers Ask", async () => {
    renderSearch("kubernetes");
    expect(
      await screen.findByRole("heading", {
        name: "Nothing matches “kubernetes”.",
      }),
    ).toBeTruthy();
    expect(screen.queryByRole("complementary")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Ask memax-v2 instead: kubernetes" }),
    ).toBeTruthy();
  });
});
