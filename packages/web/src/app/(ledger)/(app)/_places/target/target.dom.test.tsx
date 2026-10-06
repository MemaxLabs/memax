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
import type { TargetsSource } from "@/lib/v2/data/targets";
import { renderPlace } from "../test-frame";
import { DriftPlace } from "../drift";
import { TargetPlace } from "./index";

// A compiled file's settings and a hand edit, over the demo source with
// its commands spied on: a change shows at once and rolls back when it
// fails, a retry is the same command (the same key), If-Match follows
// the version; the drift choices send pull, overwrite and stop.

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
  usePathname: () => "/memax-v2/brief/targets/agents-md",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
  push.mockReset();
});

/** The demo source with its targets' commands replaced; `over` gets the real ones. */
function withTargets(
  over: (real: TargetsSource) => Partial<TargetsSource> = () => ({}),
): LedgerDataSource {
  const demo = createDemoSource({
    streamDelayMs: 0,
    commandDelayMs: 0,
    settleMs: 5,
  });
  const source = {
    ...demo,
    targets: { ...demo.targets, ...over(demo.targets) },
  };
  h.source = source;
  return source;
}

const checked = (group: HTMLElement) =>
  within(group)
    .getAllByRole("radio")
    .find((r) => r.getAttribute("aria-checked") === "true")?.textContent;

describe("how a compiled file is written", () => {
  it("changes at once, rolls back on a failure, retries with the same key", async () => {
    const configure = vi.fn<TargetsSource["configure"]>();
    withTargets((real) => {
      configure
        .mockRejectedValueOnce(new CommandFailedError({ kind: "unreachable" }))
        .mockImplementation((input) => real.configure(input));
      return { configure };
    });
    renderPlace(<TargetPlace segment="agents-md" />);
    const stale = await screen.findByRole("radiogroup", {
      name: "Stale facts",
    });
    expect(checked(stale)).toBe("Mark them");

    fireEvent.click(within(stale).getByRole("radio", { name: "Leave out" }));
    // The new value shows while the request runs…
    expect(checked(stale)).toBe("Leave out");
    // …and goes back when it fails, saying what happened.
    await waitFor(() => expect(checked(stale)).toBe("Mark them"));
    expect(
      await screen.findByText(
        "That change didn't go through, so AGENTS.md is written as before. It didn't reach Memax, so nothing changed. Try again.",
      ),
    ).toBeTruthy();

    fireEvent.click(within(stale).getByRole("radio", { name: "Leave out" }));
    await waitFor(() => expect(configure).toHaveBeenCalledTimes(2));
    const [first, second] = configure.mock.calls.map((call) => call[0]);
    expect(first.change).toEqual({ settings: { stale: "omit" } });
    expect(first.target.version).toBe(4);
    // A dropped connection may have landed: the retry is the same command.
    expect(second.idempotencyKey).toBe(first.idempotencyKey);
    expect(
      await screen.findByText("Changed how AGENTS.md is written"),
    ).toBeTruthy();
    await waitFor(() =>
      expect(
        screen.getByRole("region", { name: "Contents of AGENTS.md" })
          .textContent,
      ).not.toContain("(Being verified)"),
    );

    // The next change starts from the new version, with a new key.
    const include = screen.getByRole("radiogroup", { name: "Include" });
    fireEvent.click(within(include).getByRole("radio", { name: "Kept only" }));
    await waitFor(() => expect(configure).toHaveBeenCalledTimes(3));
    const third = configure.mock.calls[2]![0];
    expect(third.target.version).toBe(5);
    expect(third.change).toEqual({ settings: { include: "kept_only" } });
    expect(third.idempotencyKey).not.toBe(first.idempotencyKey);
  });

  it("takes a typed size budget, and refuses one out of range", async () => {
    const configure = vi.fn<TargetsSource["configure"]>();
    withTargets((real) => {
      configure.mockImplementation((input) => real.configure(input));
      return { configure };
    });
    renderPlace(<TargetPlace segment="agents-md" />);
    const budget = await screen.findByRole("textbox", { name: "Size budget" });
    fireEvent.change(budget, { target: { value: "64" } });
    fireEvent.keyDown(budget, { key: "Enter" });
    expect(await screen.findByText("Between 1 and 32 KB.")).toBeTruthy();
    expect(configure).not.toHaveBeenCalled();
    fireEvent.change(budget, { target: { value: "20 KB" } });
    fireEvent.keyDown(budget, { key: "Enter" });
    await waitFor(() => expect(configure).toHaveBeenCalledOnce());
    expect(configure.mock.calls[0]![0].change).toEqual({
      settings: { sizeBudget: 20480 },
    });
    expect(await screen.findByText(/of a 20 KB budget/)).toBeTruthy();
  });

  it("copies ChatGPT's project instructions, neutrally", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    withTargets();
    renderPlace(<TargetPlace segment="chatgpt" />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Copy project instructions" }),
    );
    await waitFor(() => expect(writeText).toHaveBeenCalledOnce());
    expect(writeText.mock.calls[0]![0]).toContain("Memax V2 engineering brief");
    expect(
      await screen.findByText("Copied the project instructions"),
    ).toBeTruthy();
    // D3: live over its connector, never "In sync".
    expect(screen.getAllByText("Live over connector").length).toBeGreaterThan(
      0,
    );
  });
});

describe("a hand edit", () => {
  it("pulls with 1 and ↵, then offers Review", async () => {
    const resolveDrift = vi.fn<TargetsSource["resolveDrift"]>();
    withTargets((real) => {
      resolveDrift.mockImplementation((input) => real.resolveDrift(input));
      return { resolveDrift };
    });
    renderPlace(<DriftPlace segment="cursor-mdc" />);
    expect(
      await screen.findByRole("heading", {
        name: "Cursor's rules file has a hand edit",
      }),
    ).toBeTruthy();
    expect(screen.getByText("Would update M-0441")).toBeTruthy();
    await act(async () => {});
    fireEvent.keyDown(document.body, { key: "1", code: "Digit1" });
    fireEvent.keyDown(document.body, { key: "Enter", code: "Enter" });
    await waitFor(() => expect(resolveDrift).toHaveBeenCalledOnce());
    expect(resolveDrift.mock.calls[0]![0]).toMatchObject({ mode: "pull" });
    expect(await screen.findByText("Pulled into Review")).toBeTruthy();
    expect(
      screen.getByText("M-0439, M-0440 wait in Review as proposals."),
    ).toBeTruthy();
    expect(
      screen
        .getAllByRole("link", { name: "Open Review" })[0]!
        .getAttribute("href"),
    ).toBe("/memax-v2/review");
  });

  it("asks before it overwrites, and stops compiling on 3", async () => {
    const resolveDrift = vi.fn<TargetsSource["resolveDrift"]>();
    withTargets((real) => {
      resolveDrift.mockImplementation((input) => real.resolveDrift(input));
      return { resolveDrift };
    });
    renderPlace(<DriftPlace segment="cursor-mdc" />);
    await screen.findByRole("heading", {
      name: "Cursor's rules file has a hand edit",
    });
    await act(async () => {});
    fireEvent.keyDown(document.body, { key: "2", code: "Digit2" });
    fireEvent.click(screen.getByRole("button", { name: /^Overwrite/ }));
    expect(screen.getByRole("alert").textContent).toContain(
      "Overwrite the hand edit?",
    );
    expect(resolveDrift).not.toHaveBeenCalled();
    fireEvent.keyDown(screen.getByRole("button", { name: /^Cancel/ }), {
      key: "Escape",
    });
    expect(screen.queryByRole("alert")).toBeNull();

    fireEvent.keyDown(document.body, { key: "3", code: "Digit3" });
    fireEvent.click(screen.getByRole("button", { name: /^Stop compiling/ }));
    await waitFor(() => expect(resolveDrift).toHaveBeenCalledOnce());
    expect(resolveDrift.mock.calls[0]![0]).toMatchObject({ mode: "stop" });
    // The page says so, and so does the toast.
    expect(
      await screen.findAllByText(
        "Stopped compiling .cursor/rules/memax-packages-web.mdc",
      ),
    ).toHaveLength(2);
  });

  it("keeps the edit waiting when the pull fails, and says why", async () => {
    const resolveDrift = vi
      .fn<TargetsSource["resolveDrift"]>()
      .mockRejectedValue(
        new CommandFailedError({
          kind: "refused",
          code: "viewer",
          message: null,
        }),
      );
    withTargets(() => ({ resolveDrift }));
    renderPlace(<DriftPlace segment="cursor-mdc" />);
    await screen.findByRole("heading", {
      name: "Cursor's rules file has a hand edit",
    });
    await act(async () => {});
    fireEvent.keyDown(document.body, { key: "Enter", code: "Enter" });
    expect(
      await screen.findByText(
        "The hand edit is still waiting. Viewers can propose and comment. A member keeps.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("Pulled into Review")).toBeNull();
  });
});
