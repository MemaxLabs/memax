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
import type { SpaceSummary } from "@/lib/v2/data/types";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { createUndoStack } from "@/lib/v2/undo-stack";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { LedgerDataProvider } from "../../_lib/data";
import { OverlayProvider } from "../../_lib/overlays";
import { SpaceViewContext } from "../../_lib/space-context";
import { UndoStackProvider } from "../../_lib/undo";
import { ReviewPlace } from "./index";

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
  usePathname: () => "/memax-v2/review",
  useSearchParams: () => new URLSearchParams(),
}));

// jsdom has no layout: the queue scrolls its selected row into view.
Element.prototype.scrollIntoView = () => {};

afterEach(() => {
  cleanup();
  h.source = null;
});

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

/** The demo, with the commands a test wants to watch or break. */
function sourceWith(
  change: (demo: LedgerDataSource) => {
    review?: Partial<LedgerDataSource["review"]>;
    memories?: Partial<LedgerDataSource["memories"]>;
    undo?: LedgerDataSource["undo"];
  },
  options: Parameters<typeof createDemoSource>[0] = {},
) {
  const demo = createDemoSource({
    streamDelayMs: 0,
    commandDelayMs: 0,
    ...options,
  });
  const parts = change(demo);
  const source: LedgerDataSource = {
    ...demo,
    review: { ...demo.review, ...parts.review },
    memories: { ...demo.memories, ...parts.memories },
    undo: parts.undo ?? demo.undo,
  };
  h.source = source;
  return source;
}

function renderReview(space: SpaceSummary = v2) {
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
                      <ReviewPlace />
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

const press = (key: string, init: Partial<KeyboardEventInit> = {}) =>
  fireEvent.keyDown(document.activeElement ?? document.body, {
    key,
    code: /^[a-z]$/i.test(key) ? `Key${key.toUpperCase()}` : undefined,
    ...init,
  });
const seal = () => screen.queryByRole("img", { name: /^Kept, Oct 5, M-0430/ });

/** Waits for the queue and for the page's key scope to listen. */
async function ready() {
  await screen.findByText("memax-v2 · 1 of 5");
  await act(async () => {});
}

describe("Review", () => {
  it("keeps optimistically: the seal stamps on K, then the next card", async () => {
    let settle!: () => void;
    const source = sourceWith((demo) => ({
      review: {
        keep: vi.fn(async (input) => {
          await new Promise<void>((resolve) => (settle = resolve));
          return demo.review.keep(input);
        }),
      },
    }));
    renderReview();
    await ready();
    press("k");
    // Stamped before the server answers.
    await waitFor(() => expect(seal()).not.toBeNull());
    expect(source.review.keep).toHaveBeenCalledWith(
      expect.objectContaining({ idempotencyKey: expect.any(String) }),
    );
    await act(async () => settle());
    expect(
      await screen.findByText("Kept M-0430 · 3 files recompiled"),
    ).toBeTruthy();
    await screen.findByText("memax-v2 · 1 of 4", undefined, { timeout: 2000 });
    expect(seal()).toBeNull();
    // The Keep has a receipt, so its toast offers Undo.
    expect(screen.getByRole("button", { name: "Undo" })).toBeTruthy();
  });

  it("rolls back on a dropped connection and retries with the same key", async () => {
    const source = sourceWith((demo) => ({
      review: {
        keep: vi
          .fn(demo.review.keep)
          .mockRejectedValueOnce(new MemaxError("down", "network_error", 0)),
      },
    }));
    renderReview();
    await ready();
    press("k");
    expect(
      await screen.findByText(
        "M-0430 wasn't kept. It didn't reach Memax, so nothing changed. Try again.",
      ),
    ).toBeTruthy();
    expect(seal()).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(source.review.keep).toHaveBeenCalledTimes(2));
    const [first, second] = vi.mocked(source.review.keep).mock.calls;
    expect(second[0].idempotencyKey).toBe(first[0].idempotencyKey);
    expect(
      await screen.findByText("Kept M-0430 · 3 files recompiled"),
    ).toBeTruthy();
  });

  it("says why a quarantined proposal can't be kept from this session", async () => {
    const source = sourceWith((demo) => ({
      review: {
        keep: vi.fn(demo.review.keep).mockRejectedValueOnce(
          new MemaxError("refused", "refused", 403, {
            policy: {
              effect: "refuse",
              code: "external_needs_review",
              message: "M-0430 comes from an outside source.",
            },
          }),
        ),
      },
    }));
    renderReview();
    await ready();
    press("k");
    expect(
      await screen.findByText(
        "M-0430 wasn't kept. It quotes an outside source, so only a web sign-in can keep it. Sign out, sign in again on this page, then keep it.",
      ),
    ).toBeTruthy();
    // A refusal isn't retried as the same command.
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    press("k");
    await waitFor(() => expect(source.review.keep).toHaveBeenCalledTimes(2));
    const [first, second] = vi.mocked(source.review.keep).mock.calls;
    expect(second[0].idempotencyKey).not.toBe(first[0].idempotencyKey);
  });

  it("shows a viewer why Keep is unavailable, and K does nothing", async () => {
    const source = sourceWith((demo) => ({
      review: { keep: vi.fn(demo.review.keep) },
    }));
    renderReview({ ...v2, role: "viewer" });
    await ready();
    const keep = screen.getByRole("button", { name: "Keep" });
    expect(keep.getAttribute("aria-disabled")).toBe("true");
    expect(keep.getAttribute("title")).toBe(
      "Viewers can propose and comment. A member keeps.",
    );
    press("k");
    press("e");
    await act(async () => {});
    expect(source.review.keep).not.toHaveBeenCalled();
    expect(screen.queryByRole("textbox", { name: "Statement" })).toBeNull();
  });

  it("edits then keeps, and settles an edit clash with Keep mine", async () => {
    const theirs = {
      version: 2,
      statement: "MCP write tools ask for confirmation with input_required.",
      by: {
        kind: "person" as const,
        self: false,
        initials: "JY",
        name: "Jiahao",
      },
      at: new Date(new Date(DEMO_NOW).getTime() - 60_000).toISOString(),
    };
    const source = sourceWith((demo) => ({
      review: { keep: vi.fn(demo.review.keep) },
      memories: {
        edit: vi
          .fn()
          .mockRejectedValueOnce(
            new MemaxError("changed", "edit_clash", 412, {
              current_version: 2,
            }),
          )
          .mockResolvedValue({
            ref: "M-0430",
            outcome: "kept",
            version: 3,
            recompiled: 3,
          }),
        latest: vi.fn().mockResolvedValue(theirs),
      },
    }));
    renderReview();
    await ready();
    press("e");
    const field = await screen.findByRole("textbox", { name: "Statement" });
    expect(document.activeElement).toBe(field);
    // Single keys are text while typing: K doesn't keep.
    press("k");
    expect(source.review.keep).not.toHaveBeenCalled();
    fireEvent.change(field, {
      target: {
        value:
          "MCP write tools ask with input_required when a person is present.",
      },
    });
    fireEvent.change(
      screen.getByRole("textbox", { name: "Why you changed it" }),
      {
        target: { value: "Cloud agents run with nobody watching." },
      },
    );
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true });
    expect(
      await screen.findByText(
        "Jiahao kept a change to this fact 1 minute ago.",
      ),
    ).toBeTruthy();
    const firstEdit = vi.mocked(source.memories.edit).mock.calls[0][0];
    expect(firstEdit).toMatchObject({
      version: 1,
      keep: true,
      reason: "Cloud agents run with nobody watching.",
    });
    fireEvent.click(screen.getByRole("button", { name: "Keep mine" }));
    await waitFor(() => expect(source.memories.edit).toHaveBeenCalledTimes(2));
    const secondEdit = vi.mocked(source.memories.edit).mock.calls[1][0];
    expect(secondEdit).toMatchObject({
      version: 2,
      statement:
        "MCP write tools ask with input_required when a person is present.",
      keep: true,
    });
    expect(secondEdit.idempotencyKey).not.toBe(firstEdit.idempotencyKey);
    expect(
      await screen.findByText("Kept M-0430 · 3 files recompiled"),
    ).toBeTruthy();
  });

  it("rejects with an optional reason: X, then ↵", async () => {
    const source = sourceWith((demo) => ({
      review: { reject: vi.fn(demo.review.reject) },
    }));
    renderReview();
    await ready();
    press("x");
    const why = await screen.findByRole("textbox", {
      name: "Why, if you'd like to say",
    });
    expect(document.activeElement).toBe(why);
    fireEvent.change(why, { target: { value: "Not true for cloud agents." } });
    fireEvent.keyDown(why, { key: "Enter" });
    expect(await screen.findByText("Rejected M-0430")).toBeTruthy();
    expect(source.review.reject).toHaveBeenCalledWith(
      expect.objectContaining({ reason: "Not true for cloud agents." }),
    );
    await screen.findByText("memax-v2 · 1 of 4");
  });

  it("ignores keys an IME is composing", async () => {
    const source = sourceWith((demo) => ({
      review: { keep: vi.fn(demo.review.keep) },
    }));
    renderReview();
    await ready();
    press("k", { isComposing: true, keyCode: 229 });
    await act(async () => {});
    expect(source.review.keep).not.toHaveBeenCalled();
    press("ArrowDown");
    await screen.findByText("memax-v2 · 2 of 5");
  });
});

const team = DEMO_SPACES.find((s) => s.slug === "memax-team")!;
const busy = () =>
  new MemaxError(
    "Memax is still checking M-0430 against the decisions in force.",
    "judge_pending",
    503,
    { retry_after: 1, ref: "M-0430" },
    1,
  );
/** The card's head mark: what state the card says it's in. */
const headMark = () =>
  document.querySelector(".mx-review .mx-review-head .mx-state");

describe("Review and the judge", () => {
  it("shows the working mark while the judge checks, and stops refetching once it's done", async () => {
    const source = sourceWith(
      (demo) => ({ review: { queue: vi.fn(demo.review.queue) } }),
      { judging: { "M-0445": { afterMs: 600, touchesDecision: true } } },
    );
    renderReview(team);
    await screen.findByText(/· 1 of 2$/);
    // M-0444's check couldn't run: an ordinary proposal, with a quiet line.
    expect(
      screen.getByText(
        "Memax couldn't check it against what's kept. Review it as usual; Dream looks again tonight.",
      ),
    ).toBeTruthy();
    expect(headMark()?.className).toContain("mx-state--proposed");
    // M-0445 is still being checked: the row and the card wear the working mark.
    const row = screen.getByRole("img", { name: "Checking" });
    expect(row.className).toContain("mx-state--working");
    expect(document.querySelector(".mx-glyph-arc")).not.toBeNull();
    press("ArrowDown");
    await screen.findByText(/· 2 of 2$/);
    expect(headMark()?.className).toContain("mx-state--working");
    expect(headMark()?.textContent).toBe("Checking");
    // A second later Review asks again, and the verdict has landed.
    await waitFor(
      () => expect(screen.queryByRole("img", { name: "Checking" })).toBeNull(),
      { timeout: 3000 },
    );
    expect(headMark()?.className).toContain("mx-state--proposed");
    const calls = vi.mocked(source.review.queue).mock.calls.length;
    expect(calls).toBeGreaterThanOrEqual(1);
    // Nothing is working any more, so it doesn't ask again.
    await act(() => new Promise((resolve) => setTimeout(resolve, 2300)));
    expect(vi.mocked(source.review.queue).mock.calls.length).toBe(calls);
  }, 10_000);

  it("waits for the judge when Keep is busy, then keeps with the same key", async () => {
    const source = sourceWith((demo) => ({
      review: {
        keep: vi.fn(demo.review.keep).mockRejectedValueOnce(busy()),
      },
    }));
    renderReview();
    await ready();
    press("k");
    // No rollback and no error: the working mark, and why it waits.
    expect(
      await screen.findByText(
        "Checking it against the decision in force. It's kept once the check is done.",
      ),
    ).toBeTruthy();
    expect(headMark()?.className).toContain("mx-state--working");
    expect(seal()).toBeNull();
    expect(screen.queryByText(/wasn't kept/)).toBeNull();
    expect(screen.getByText("stop waiting")).toBeTruthy();
    // After Retry-After it asks again, with the same key, and keeps.
    expect(
      await screen.findByText("Kept M-0430 · 3 files recompiled", undefined, {
        timeout: 3000,
      }),
    ).toBeTruthy();
    const [first, second] = vi.mocked(source.review.keep).mock.calls;
    expect(second[0].idempotencyKey).toBe(first[0].idempotencyKey);
    await screen.findByText("memax-v2 · 1 of 4", undefined, { timeout: 2000 });
  }, 10_000);

  it("settles as a conflict when the judge flags it while Keep waits", async () => {
    // The judge flags M-0430 while Keep waits: from then on the server
    // lists it as a conflict with M-0102.
    let flagged = false;
    const asConflict = <T extends { ref: string } | null>(item: T): T =>
      item && flagged && item.ref === "M-0430"
        ? { ...item, state: "conflict", conflictsWith: "M-0102" }
        : item;
    const source = sourceWith((demo) => ({
      review: {
        queue: vi.fn(async (input) => {
          const queue = await demo.review.queue(input);
          return { ...queue, items: queue.items.map(asConflict) };
        }),
        keep: vi
          .fn(demo.review.keep)
          .mockRejectedValueOnce(busy())
          .mockImplementationOnce(async () => {
            flagged = true;
            throw new MemaxError(
              "M-0430 contradicts M-0102, a decision in force.",
              "in_conflict",
              409,
              { ref: "M-0102" },
            );
          }),
        item: vi.fn(async (input) => asConflict(await demo.review.item(input))),
      },
    }));
    renderReview();
    await ready();
    press("k");
    await screen.findByText(
      "Checking it against the decision in force. It's kept once the check is done.",
    );
    expect(
      await screen.findByText(
        "M-0430 contradicts M-0102, a decision in force. Compare both sides to settle it.",
        undefined,
        { timeout: 3000 },
      ),
    ).toBeTruthy();
    // Still waiting on the person, now as a conflict with two sides. The
    // 409 names the decision, so Review doesn't ask for the memory again.
    expect(source.review.item).not.toHaveBeenCalled();
    expect(screen.getByText("memax-v2 · 1 of 5")).toBeTruthy();
    const row = document.querySelector(".mx-row.is-selected .mx-state");
    expect(row?.className).toContain("mx-state--conflict");
    expect(
      screen.getByRole("link", { name: /Compare both sides/ }),
    ).toBeTruthy();
  }, 10_000);

  it("saves an edit, then keep, for the judge, then keeps the new version", async () => {
    const words =
      "MCP write tools ask for confirmation with input_required when a person is present.";
    const source = sourceWith((demo) => ({
      review: {
        keep: vi
          .fn(demo.review.keep)
          .mockRejectedValueOnce(busy())
          .mockResolvedValueOnce({
            ref: "M-0430",
            outcome: "kept",
            version: 3,
            recompiled: 3,
            receipt: "r-keep",
          }),
      },
      memories: {
        edit: vi.fn().mockResolvedValue({
          ref: "M-0430",
          outcome: "edited",
          version: 2,
          recompiled: null,
          receipt: "r-edit",
          judgePending: true,
        }),
      },
    }));
    renderReview();
    await ready();
    press("e");
    const field = await screen.findByRole("textbox", { name: "Statement" });
    fireEvent.change(field, { target: { value: words } });
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true });
    // Saved, not kept: the card waits for the judge with the new words.
    expect(
      await screen.findByText(
        "Checking it against the decision in force. It's kept once the check is done.",
      ),
    ).toBeTruthy();
    expect(headMark()?.className).toContain("mx-state--working");
    expect(seal()).toBeNull();
    // Then a plain Keep of the saved version, with its own key, retried
    // after Retry-After until the judge has looked.
    expect(
      await screen.findByText("Kept M-0430 · 3 files recompiled", undefined, {
        timeout: 3000,
      }),
    ).toBeTruthy();
    const edit = vi.mocked(source.memories.edit).mock.calls[0][0];
    const keeps = vi.mocked(source.review.keep).mock.calls;
    expect(keeps).toHaveLength(2);
    expect(keeps[0][0].item).toMatchObject({ version: 2, statement: words });
    expect(keeps[0][0].idempotencyKey).not.toBe(edit.idempotencyKey);
    expect(keeps[1][0].idempotencyKey).toBe(keeps[0][0].idempotencyKey);
    await screen.findByText("memax-v2 · 1 of 4", undefined, { timeout: 2000 });
  }, 10_000);

  it("leaves the saved edit when Esc stops waiting for the judge", async () => {
    const words = "MCP write tools ask for confirmation with input_required.";
    const source = sourceWith((demo) => ({
      review: { keep: vi.fn(demo.review.keep).mockRejectedValue(busy()) },
      memories: {
        edit: vi.fn().mockResolvedValue({
          ref: "M-0430",
          outcome: "edited",
          version: 2,
          recompiled: null,
          receipt: "r-edit",
          judgePending: true,
        }),
      },
    }));
    renderReview();
    await ready();
    press("e");
    const field = await screen.findByRole("textbox", { name: "Statement" });
    fireEvent.change(field, { target: { value: words } });
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true });
    await screen.findByText(/kept once the check is done/);
    press("Escape");
    await waitFor(() =>
      expect(screen.queryByText(/kept once the check is done/)).toBeNull(),
    );
    // Still in Review, with the saved words, and no error.
    expect(screen.getByText("memax-v2 · 1 of 5")).toBeTruthy();
    expect(screen.getAllByText(words).length).toBeGreaterThan(0);
    expect(screen.queryByText(/wasn't kept/)).toBeNull();
    await act(() => new Promise((resolve) => setTimeout(resolve, 1300)));
    expect(source.review.keep).toHaveBeenCalledTimes(1);
  }, 10_000);

  it("stops waiting on Esc, and when you move to another card", async () => {
    const source = sourceWith((demo) => ({
      review: { keep: vi.fn(demo.review.keep).mockRejectedValue(busy()) },
    }));
    renderReview();
    await ready();
    press("k");
    await screen.findByText(
      "Checking it against the decision in force. It's kept once the check is done.",
    );
    press("Escape");
    await waitFor(() =>
      expect(headMark()?.className).toContain("mx-state--proposed"),
    );
    expect(screen.queryByText(/kept once the check is done/)).toBeNull();
    // Esc stopped the retry: nothing asks again.
    await act(() => new Promise((resolve) => setTimeout(resolve, 1300)));
    expect(source.review.keep).toHaveBeenCalledTimes(1);
    // Waiting again, ↓ moves on and stops it too.
    press("k");
    await screen.findByText(/kept once the check is done/);
    press("ArrowDown");
    await screen.findByText("memax-v2 · 2 of 5");
    await act(() => new Promise((resolve) => setTimeout(resolve, 1300)));
    expect(source.review.keep).toHaveBeenCalledTimes(2);
  }, 10_000);

  it("keeps a flagged proposal in place of the decision it contradicts", async () => {
    const source = sourceWith((demo) => ({
      review: {
        keep: vi.fn(demo.review.keep),
        resolveConflict: vi.fn(demo.review.resolveConflict),
      },
    }));
    renderReview();
    await ready();
    press("ArrowDown");
    await screen.findByText("memax-v2 · 2 of 5");
    expect(
      screen.getByRole("button", { name: /Keep, replace old/ }),
    ).toBeTruthy();
    press("k");
    expect(
      await screen.findByText("Kept M-0431 in place of M-0174"),
    ).toBeTruthy();
    expect(source.review.keep).not.toHaveBeenCalled();
    expect(source.review.resolveConflict).toHaveBeenCalledWith(
      expect.objectContaining({
        ref: "M-0431",
        other: "M-0174",
        option: "proposal",
        version: 1,
      }),
    );
  });
});

describe("Undo in Review", () => {
  it("⌘Z undoes the last decision: the card comes back as it was", async () => {
    const source = sourceWith((demo) => ({
      undo: vi.fn(demo.undo),
    }));
    renderReview();
    await ready();
    press("x");
    const why = await screen.findByRole("textbox", {
      name: "Why, if you'd like to say",
    });
    fireEvent.keyDown(why, { key: "Enter" });
    await screen.findByText("Rejected M-0430");
    await screen.findByText("memax-v2 · 1 of 4");
    (document.activeElement as HTMLElement | null)?.blur();
    press("z", { ctrlKey: true });
    // At once, before the server answers: back in the queue, selected.
    await screen.findByText("memax-v2 · 1 of 5");
    const undone = await screen.findByText(
      "Undid the rejection. M-0430 is back in Review.",
    );
    expect(source.undo).toHaveBeenCalledWith(
      expect.objectContaining({
        receipt: expect.stringMatching(/^demo-receipt-/),
      }),
    );
    // An undo's toast carries no state mark.
    expect(undone.parentElement?.querySelector(".mx-state")).toBeNull();
    // Nothing left to undo: ⌘Z does nothing more.
    press("z", { ctrlKey: true });
    await act(async () => {});
    expect(source.undo).toHaveBeenCalledTimes(1);
  });

  it.each([
    [
      new MemaxError("too late", "undo_refused", 409, {
        reason: "window_passed",
        ref: "M-0430",
      }),
      "M-0430 was decided more than 10 minutes ago, so it can't be undone. Change it on its page instead.",
    ],
    [
      new MemaxError("again", "undo_refused", 409, {
        reason: "already_undone",
        ref: "M-0430",
      }),
      "That was already undone. M-0430 is as it was before.",
    ],
    [
      new MemaxError("no", "undo_refused", 409, {
        reason: "not_undoable",
        ref: "M-0430",
      }),
      "That change to M-0430 can't be undone. Change it on its page instead.",
    ],
    [
      new MemaxError("later", "undo_refused", 409, {
        reason: "later_changes",
        ref: "B-0043",
      }),
      "The Brief cites M-0430 now. Take it out of the Brief first, then undo.",
    ],
    [
      new MemaxError("not yours", "refused", 403, {
        policy: {
          effect: "refuse",
          code: "undo_by_decider",
          message: "Only the person who decided M-0430 can undo it.",
        },
      }),
      "Only the person who decided M-0430 can undo it. Change it instead, or ask them.",
    ],
  ])("words a refusal and takes the card out again: %s", async (err, text) => {
    sourceWith(() => ({ undo: vi.fn().mockRejectedValue(err) }));
    renderReview();
    await ready();
    press("k");
    await screen.findByText("memax-v2 · 1 of 4", undefined, { timeout: 2000 });
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    const refusal = await screen.findByText(text);
    expect(refusal.parentElement?.querySelector(".mx-state")).toBeNull();
    // Rolled back: the kept card is out of the queue again.
    await screen.findByText("memax-v2 · 1 of 4");
    // A refusal is final: no Try again, and ⌘Z has nothing left.
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });
});
