// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CommandFailedError } from "@/lib/v2/data/command-error";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { useSpaces } from "../_lib/data";
import { useOverlays } from "../_lib/overlays";
import { renderPlace } from "../_places/test-frame";
import { NewSpaceDialog } from "./new-space";

// New space (NewSpace.png) on the demo: a project space from its name
// and repository, landing on its Today; a second personal space and team
// spaces said before anything is sent, and the API's refusals worded.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  push: vi.fn(),
  params: "",
  replace: vi.fn(),
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
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: h.push, replace: h.replace, prefetch: vi.fn() }),
  usePathname: () => "/memax-v2/today",
  useParams: () => ({ space: "memax-v2" }),
  useSearchParams: () => new URLSearchParams(h.params),
}));

/** Opens the layer, and lists the spaces the frame knows. */
function Host() {
  const { openNewSpace } = useOverlays();
  const spaces = useSpaces().data ?? [];
  return (
    <>
      <button type="button" onClick={openNewSpace}>
        Open
      </button>
      <output aria-label="spaces">{spaces.map((s) => s.slug).join(" ")}</output>
      <NewSpaceDialog />
    </>
  );
}

function open() {
  renderPlace(<Host />);
  fireEvent.click(screen.getByRole("button", { name: "Open" }));
  return screen.getByRole("dialog", { name: "New space" });
}

const text = (el: Element | null | undefined) => el?.textContent ?? "";
const radio = (name: RegExp) => screen.getByRole("radio", { name });
const create = () => screen.getByRole("button", { name: /Create space/ });
const field = (name: string) =>
  screen.getByRole("textbox", { name }) as HTMLInputElement;

beforeEach(() => {
  h.source = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  h.push.mockReset();
  h.replace.mockReset();
  h.params = "";
});
afterEach(cleanup);

describe("New space", () => {
  it("makes a project space from its repository and lands on its Today", async () => {
    open();
    expect(radio(/^Project/).getAttribute("aria-checked")).toBe("true");
    fireEvent.change(field("Repository"), {
      target: { value: "MemaxLabs/acme-api" },
    });
    // The name follows the repository until it's named.
    expect(field("Name").value).toBe("acme-api");
    expect(
      text(
        screen
          .getByRole("radio", {
            name: /What agents already know in this repository/,
          })
          .closest("label"),
      ),
    ).toContain("CLAUDE.md, AGENTS.md and rules");
    fireEvent.click(create());
    await waitFor(() => expect(h.push).toHaveBeenCalledWith("/acme-api/today"));
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "New space" })).toBeNull(),
    );
    expect(
      text(screen.getByRole("region", { name: "Notifications" })),
    ).toContain("acme-api is ready. Connect an agent to fill it.");
    // The switcher lists it at once.
    expect(text(screen.getByRole("status", { name: "spaces" }))).toContain(
      "acme-api",
    );
  });

  it("takes a name of its own, and asks for one", async () => {
    open();
    fireEvent.click(create());
    expect(screen.getByText("Name the space.")).toBeTruthy();
    expect(h.push).not.toHaveBeenCalled();
    fireEvent.change(field("Name"), { target: { value: "Acme web" } });
    fireEvent.change(field("Repository"), {
      target: { value: "acme/web-app" },
    });
    // Named by the person: the repository leaves the name alone.
    expect(field("Name").value).toBe("Acme web");
    fireEvent.click(create());
    await waitFor(() =>
      expect(h.push).toHaveBeenCalledWith("/acme-web-2/today"),
    );
  });

  it("checks the repository's shape before it asks", () => {
    open();
    fireEvent.change(field("Name"), { target: { value: "acme" } });
    fireEvent.change(field("Repository"), {
      target: { value: "not a repository" },
    });
    expect(
      screen.getByText("Write it as owner/name, such as MemaxLabs/memax-web."),
    ).toBeTruthy();
    fireEvent.click(create());
    expect(h.push).not.toHaveBeenCalled();
  });

  it("says a person has one personal space, and opens it", () => {
    open();
    fireEvent.click(radio(/^Personal/));
    expect(
      screen.getByText("You have one already: Personal. Each person has one."),
    ).toBeTruthy();
    expect(create().getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Open Personal" }));
    expect(h.push).toHaveBeenCalledWith("/personal/today");
  });

  it("says team spaces come with Team; the arrows choose", () => {
    open();
    const project = radio(/^Project/);
    project.focus();
    fireEvent.keyDown(project, { key: "ArrowRight" });
    expect(radio(/^Team/).getAttribute("aria-checked")).toBe("true");
    expect(document.activeElement).toBe(radio(/^Team/));
    expect(
      screen.getByText(
        "Team spaces come with Team. Make a project space for now.",
      ),
    ).toBeTruthy();
    expect(create().getAttribute("aria-disabled")).toBe("true");
    fireEvent.keyDown(radio(/^Team/), { key: "ArrowRight" });
    expect(radio(/^Personal/).getAttribute("aria-checked")).toBe("true");
  });

  it("words the API's refusal, and keeps the form", async () => {
    const demo = h.source!;
    h.source = {
      ...demo,
      createSpace: async () => {
        throw new CommandFailedError({
          kind: "refused",
          code: "space_limit",
          message: null,
        });
      },
    };
    open();
    fireEvent.change(field("Name"), { target: { value: "acme" } });
    fireEvent.click(create());
    expect(text(await screen.findByRole("alert"))).toBe(
      "acme wasn't made. You own 50 project spaces, the most one person can during the alpha.",
    );
    expect(screen.getByRole("dialog", { name: "New space" })).toBeTruthy();
    expect(h.push).not.toHaveBeenCalled();
  });

  it("opens from ?overlay=new-space, then leaves the address", async () => {
    h.params = "overlay=new-space";
    renderPlace(<Host />);
    expect(
      await screen.findByRole("dialog", { name: "New space" }),
    ).toBeTruthy();
    expect(h.replace).toHaveBeenCalledWith("/memax-v2/today", {
      scroll: false,
    });
  });
});
