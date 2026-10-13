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
import type { MachinesSource } from "@/lib/v2/data/machines";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { BriefPlace } from "../_places/brief";
import { PERSONAL_SPACE, renderPlace } from "../_places/test-frame";
import { MachinesSection } from "../settings/account/machines-section";

// The machines a Personal space is written to, on its Brief and in
// Settings › Account: Rename morphs the row into its field (↵ renames,
// Esc cancels, If-Match on the version, the same key on a retry), and
// Stop compiling here asks in place, saying what happens to the files,
// with Cancel taking the focus.

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
  usePathname: () => "/personal/brief",
  useParams: () => ({ space: "personal" }),
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
});

function withMachines(
  over: (real: MachinesSource) => Partial<MachinesSource> = () => ({}),
): LedgerDataSource {
  const demo = createDemoSource({
    streamDelayMs: 0,
    commandDelayMs: 0,
    settleMs: 5,
  });
  const source = {
    ...demo,
    machines: { ...demo.machines, ...over(demo.machines) },
  };
  h.source = source;
  return source;
}

const machinesPanel = () => screen.getByRole("region", { name: "Machines" });

describe("the machines panel", () => {
  it("lists each machine: what it is, who reads its files, when it was last online", async () => {
    withMachines();
    renderPlace(<BriefPlace />, { space: PERSONAL_SPACE });
    await screen.findByRole("region", { name: "Machines" });
    const panel = machinesPanel();
    expect(within(panel).getByText("2 linked")).toBeTruthy();
    expect(
      within(panel).getByText("macOS · Claude Code and Codex · online now"),
    ).toBeTruthy();
    expect(
      within(panel).getByText(
        "Linux · Claude Code and Codex · last online 3 days ago",
      ),
    ).toBeTruthy();
  });

  it("renames in place: ↵ sends the name with If-Match, a retry reuses the key, Esc cancels", async () => {
    const rename = vi.fn<MachinesSource["rename"]>();
    withMachines((real) => {
      rename
        .mockRejectedValueOnce(new CommandFailedError({ kind: "unreachable" }))
        .mockImplementation((input) => real.rename(input));
      return { rename };
    });
    renderPlace(<BriefPlace />, { space: PERSONAL_SPACE });
    const open = await screen.findByRole("button", {
      name: "Rename ziyang-mbp",
    });
    fireEvent.click(open);
    const field = await screen.findByRole("textbox", {
      name: "Name of ziyang-mbp",
    });
    await waitFor(() => expect(document.activeElement).toBe(field));
    expect(
      screen.getByText(
        "What you call it in Memax. Receipts call it machine-1.",
      ),
    ).toBeTruthy();

    // Empty isn't a name: nothing is sent.
    fireEvent.change(field, { target: { value: "  " } });
    fireEvent.keyDown(field, { key: "Enter" });
    expect(
      await screen.findByText("Give it a name of 1 to 80 characters."),
    ).toBeTruthy();
    expect(rename).not.toHaveBeenCalled();

    fireEvent.change(field, { target: { value: "studio-mbp" } });
    fireEvent.keyDown(field, { key: "Enter" });
    expect(
      await screen.findByText(/^ziyang-mbp wasn't renamed\./),
    ).toBeTruthy();
    fireEvent.keyDown(field, { key: "Enter" });
    await waitFor(() => expect(rename).toHaveBeenCalledTimes(2));
    const [first, second] = rename.mock.calls.map((c) => c[0]);
    expect(first).toMatchObject({
      name: "studio-mbp",
      machine: { label: "machine-1", version: 1 },
    });
    expect(second.idempotencyKey).toBe(first.idempotencyKey);
    expect(
      await screen.findByText("Renamed ziyang-mbp to studio-mbp"),
    ).toBeTruthy();
    // The row is the machine again, focus back on Rename.
    const again = await screen.findByRole("button", {
      name: "Rename studio-mbp",
    });
    await waitFor(() => expect(document.activeElement).toBe(again));

    fireEvent.click(again);
    const next = await screen.findByRole("textbox", {
      name: "Name of studio-mbp",
    });
    fireEvent.change(next, { target: { value: "other" } });
    fireEvent.keyDown(next, { key: "Escape" });
    expect(
      screen.queryByRole("textbox", { name: "Name of studio-mbp" }),
    ).toBeNull();
    expect(rename).toHaveBeenCalledTimes(2);
  });

  it("says when someone renamed it first", async () => {
    withMachines(() => ({
      rename: async () => {
        throw new CommandFailedError({ kind: "clash", currentVersion: 2 });
      },
    }));
    renderPlace(<BriefPlace />, { space: PERSONAL_SPACE });
    fireEvent.click(
      await screen.findByRole("button", { name: "Rename home-linux" }),
    );
    const field = await screen.findByRole("textbox", {
      name: "Name of home-linux",
    });
    fireEvent.change(field, { target: { value: "desk" } });
    fireEvent.keyDown(field, { key: "Enter" });
    expect(
      await screen.findByText(
        "home-linux was renamed somewhere else first. Here's its name now; rename it again if you still want to.",
      ),
    ).toBeTruthy();
  });

  it("stops compiling on a machine only once confirmed, saying what happens to its files", async () => {
    const unlink = vi.fn<MachinesSource["unlink"]>();
    withMachines((real) => {
      unlink.mockImplementation((input) => real.unlink(input));
      return { unlink };
    });
    renderPlace(<BriefPlace />, { space: PERSONAL_SPACE });
    const stop = await screen.findByRole("button", {
      name: "Stop compiling on home-linux",
    });
    fireEvent.click(stop);
    const confirm = screen.getByRole("group", {
      name: "Stop compiling on home-linux",
    });
    expect(
      within(confirm).getByText(
        "Memax takes your Personal space off home-linux the next time it's online. Files you edited by hand stay as they are.",
      ),
    ).toBeTruthy();
    const cancel = within(confirm).getByRole("button", { name: /Cancel/ });
    await waitFor(() => expect(document.activeElement).toBe(cancel));
    fireEvent.keyDown(cancel, { key: "Escape" });
    expect(
      screen.queryByRole("group", { name: "Stop compiling on home-linux" }),
    ).toBeNull();
    await waitFor(() =>
      expect(document.activeElement).toBe(
        screen.getByRole("button", { name: "Stop compiling on home-linux" }),
      ),
    );
    expect(unlink).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "Stop compiling on home-linux" }),
    );
    fireEvent.click(
      within(
        screen.getByRole("group", { name: "Stop compiling on home-linux" }),
      ).getByRole("button", { name: "Stop compiling on home-linux" }),
    );
    await waitFor(() => expect(unlink).toHaveBeenCalledOnce());
    expect(unlink.mock.calls[0]![0].idempotencyKey).toBeTruthy();
    expect(
      await screen.findByText(
        "Stopped compiling on home-linux. Memax takes its files off the next time it's online.",
      ),
    ).toBeTruthy();
    // The row stays, unlinked, saying how to link it again.
    expect(
      await within(machinesPanel()).findByText("Linux · Unlinked Oct 5"),
    ).toBeTruthy();
    expect(within(machinesPanel()).getByText("1 linked")).toBeTruthy();
    expect(
      within(machinesPanel()).getByText(
        /^Run .*memax-cli link --home on it to write your Personal space there again\.$/,
      ),
    ).toBeTruthy();
  });

  it("is in Settings › Account too, with the command for the next computer", async () => {
    withMachines();
    renderPlace(<MachinesSection />, { space: PERSONAL_SPACE });
    const section = await screen.findByRole("region", {
      name: "Where Personal is written",
    });
    expect(within(section).getByText("ziyang-mbp")).toBeTruthy();
    expect(within(section).getByText("last online 3 days ago")).toBeTruthy();
    expect(
      within(section).getByText(
        "Run this on another computer to write Personal there too:",
      ),
    ).toBeTruthy();
  });
});
