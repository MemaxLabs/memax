// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
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

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  params: null as URLSearchParams | null,
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
vi.mock("@/lib/auth", () => ({
  useAuth: () => ({ user: null, loading: false }),
}));
vi.mock("@/lib/memax-client", () => ({ getMemaxClient: () => ({}) }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/memax-v2/review",
  useSearchParams: () => h.params ?? new URLSearchParams(),
}));

// jsdom has no layout: the queue scrolls its selected row into view.
Element.prototype.scrollIntoView = () => {};

afterEach(() => {
  cleanup();
  h.source = null;
  h.params = null;
});

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

/** The demo, with the commands a test wants to watch or break. */
function sourceWith(
  change: (demo: LedgerDataSource) => {
    review?: Partial<LedgerDataSource["review"]>;
    memories?: Partial<LedgerDataSource["memories"]>;
    gates?: Partial<LedgerDataSource["gates"]>;
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
    gates: { ...demo.gates, ...parts.gates },
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
  return client;
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
    // The team space's two questions head the queue; M-0444 comes next.
    await screen.findByText(/· 1 of 4$/);
    press("ArrowDown");
    press("ArrowDown");
    await screen.findByText(/· 3 of 4$/);
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
    await screen.findByText(/· 4 of 4$/);
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

  it("shows a write the judge returned to Review as a conflict, with its receipt", async () => {
    // Rule 11: Codex kept M-0431 at once; the judge found it contradicts
    // M-0174, a decision in force, and returned it to Review.
    const returned = <T extends { ref: string } | null>(item: T): T =>
      item && item.ref === "M-0431"
        ? { ...item, action: "returned", returned: { decision: "M-0174" } }
        : item;
    const source = sourceWith((demo) => ({
      review: {
        peekQueue: (slug) => {
          const queue = demo.review.peekQueue?.(slug);
          return queue && { ...queue, items: queue.items.map(returned) };
        },
        queue: vi.fn(async (input) => {
          const queue = await demo.review.queue(input);
          return { ...queue, items: queue.items.map(returned) };
        }),
        item: vi.fn(async (input) => returned(await demo.review.item(input))),
        resolveConflict: vi.fn(demo.review.resolveConflict),
      },
    }));
    renderReview();
    await ready();
    press("ArrowDown");
    await screen.findByText("memax-v2 · 2 of 5");
    // The card: the conflict, who kept it at once, and what the judge did.
    expect(
      await screen.findByText(
        "Codex kept this at once. Memax returned it to Review: it contradicts M-0174, a decision in force.",
      ),
    ).toBeTruthy();
    const card = document.querySelector(".mx-review") as HTMLElement;
    expect(within(card).getByText("Conflicts with a kept memory")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /Keep, replace old/ }),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /Compare both sides/ }),
    ).toBeTruthy();
    // The queue row says it's back in Review, in conflict.
    const row = document.querySelector(".mx-row.is-selected") as HTMLElement;
    expect(row.querySelector(".mx-state")?.className).toContain(
      "mx-state--conflict",
    );
    expect(within(row).getByText("back in Review")).toBeTruthy();
    // Keeping it settles the conflict in its favour: a person's Keep.
    press("k");
    expect(
      await screen.findByText("Kept M-0431 in place of M-0174"),
    ).toBeTruthy();
    expect(source.review.resolveConflict).toHaveBeenCalledWith(
      expect.objectContaining({ ref: "M-0431", option: "proposal" }),
    );
  });

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

describe("Decision gates in Review", () => {
  const card = () => document.querySelector(".mx-gate") as HTMLElement;
  /** Waits for the team space's queue, a question first. */
  async function teamReady() {
    await screen.findByText("Memax team · 1 of 4");
    await act(async () => {});
  }
  const confirmation = () =>
    within(card()).queryByRole("status")?.textContent ?? null;

  it("lists the questions agents asked first, with the agent, the session and the options", async () => {
    sourceWith(() => ({}));
    renderReview(team);
    await teamReady();
    const queue = screen.getByRole("complementary", { name: "Waiting on you" });
    const rows = within(queue).getAllByRole("listitem");
    expect(
      rows.map((r) => r.querySelector(".mx-row-rail-id")?.textContent),
    ).toEqual(["G-0011", "G-0012", "M-0444", "M-0445"]);
    expect(rows[0]!.textContent).toContain("asked");
    expect(within(queue).getByText("4 waiting · oldest 1 h")).toBeTruthy();
    expect(
      screen.getByText("Asked in session 3e1a · Expires tomorrow at 13:40"),
    ).toBeTruthy();
    const gate = card();
    expect(
      within(gate).getByText("Claude Code is waiting on you"),
    ).toBeTruthy();
    expect(
      within(gate).getByRole("radiogroup", {
        name: "Keep the ChatGPT tool names, or align them with the core set?",
      }),
    ).toBeTruthy();
    expect(within(gate).getAllByRole("radio")).toHaveLength(3);
    expect(
      within(gate).getByText(
        "The seven aliases stay; nothing changes for ChatGPT.",
      ),
    ).toBeTruthy();
    expect(
      within(gate).getByText(
        "Your answer is kept in Memax team, authored by you.",
      ),
    ).toBeTruthy();
    // The legend follows the card: digits choose, ↵ answers.
    const legend = screen.getByRole("group", { name: "Review keys" });
    expect(legend.textContent).toContain("1–3 choose");
    expect(legend.textContent).toContain("↵ answer");
    expect(legend.textContent).not.toContain("keep");
  });

  it("answers by keyboard: a digit, ↵ to confirm, ↵ again, and the seal with the decision", async () => {
    const source = sourceWith((demo) => ({
      gates: { answer: vi.fn(demo.gates.answer) },
    }));
    renderReview(team);
    await teamReady();
    press("2");
    const radios = within(card()).getAllByRole("radio");
    expect(radios[1]?.getAttribute("aria-checked")).toBe("true");
    press("Enter");
    expect(confirmation()).toBe(
      "Answer “Align with the core set”? This becomes a kept decision by you, and compiles into every file.",
    );
    expect(source.gates.answer).not.toHaveBeenCalled();
    press("Enter");
    await waitFor(() =>
      expect(
        screen.queryByRole("img", { name: /^Kept, Oct 5, M-0439/ }),
      ).not.toBeNull(),
    );
    expect(within(card()).getByText("You answered")).toBeTruthy();
    expect(source.gates.answer).toHaveBeenCalledTimes(1);
    expect(vi.mocked(source.gates.answer).mock.calls[0]![0]).toMatchObject({
      option: 1,
      gate: expect.objectContaining({ ref: "G-0011", version: 1 }),
      idempotencyKey: expect.any(String),
    });
    expect(
      await screen.findByText(/^Kept M-0439 as your answer to G-0011/),
    ).toBeTruthy();
    // A link to the decision, and no Undo: answers can't be undone yet.
    expect(screen.getByRole("button", { name: "Open M-0439" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Undo" })).toBeNull();
    // Then the next card: Codex's question.
    await screen.findByText("Memax team · 1 of 3", undefined, {
      timeout: 2000,
    });
    expect(within(card()).getByText("Codex is waiting on you")).toBeTruthy();
  });

  it("Esc goes back from the confirmation, and a digit chooses again", async () => {
    sourceWith(() => ({}));
    renderReview(team);
    await teamReady();
    press("1");
    press("Enter");
    expect(confirmation()).toContain("Keep the ChatGPT names");
    press("Escape");
    expect(confirmation()).toBeNull();
    // Back to choosing, the focus is on the option chosen.
    expect(document.activeElement?.getAttribute("role")).toBe("radio");
    press("3");
    press("Enter");
    expect(confirmation()).toContain("Decide later");
  });

  it("says before you try when answering needs the web and this session isn't (D15)", async () => {
    const source = sourceWith(
      (demo) => ({ gates: { answer: vi.fn(demo.gates.answer) } }),
      { webSession: false },
    );
    renderReview(team);
    await teamReady();
    const note = within(card()).getByRole("note");
    expect(note.textContent).toContain(
      "Decisions in Memax team are answered only on memax.app, and Memax can't confirm this session is. Sign in again here, then answer.",
    );
    expect(
      within(note).getByRole("button", { name: "Sign in again" }),
    ).toBeTruthy();
    press("1");
    const answer = within(card()).getByRole("button", { name: "Answer" });
    expect(answer.getAttribute("aria-disabled")).toBe("true");
    expect(answer.title).toBe("Sign in again on memax.app to answer.");
    press("Enter");
    expect(confirmation()).toBeNull();
    expect(source.gates.answer).not.toHaveBeenCalled();
  });

  it("words D15's refusal with Sign in again, and says so on the card from then on", async () => {
    const source = sourceWith(() => ({
      gates: {
        answer: vi.fn().mockRejectedValue(
          new MemaxError("needs the web", "refused", 403, {
            policy: {
              effect: "refuse",
              code: "decision_needs_web",
              message: "Decisions in Memax team need a person on the web.",
            },
          }),
        ),
      },
    }));
    renderReview(team);
    await teamReady();
    press("1");
    press("Enter");
    press("Enter");
    expect(
      await screen.findByText(
        /^G-0011 wasn't answered\. Decisions in Memax team are answered only on memax\.app, and Memax couldn't confirm this came from there\. Sign in again here, then answer\./,
      ),
    ).toBeTruthy();
    const toasts = screen.getByRole("region", { name: "Notifications" });
    expect(
      within(toasts).getByRole("button", { name: "Sign in again" }),
    ).toBeTruthy();
    expect(
      within(toasts).queryByRole("button", { name: "Try again" }),
    ).toBeNull();
    // The gate keeps waiting, and the card now says why it can't be answered here.
    expect(within(card()).getByRole("note").textContent).toContain(
      "answered only on memax.app",
    );
    expect(source.gates.answer).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Memax team · 1 of 4")).toBeTruthy();
  });

  it("withdraws quietly, confirmed inline", async () => {
    const source = sourceWith((demo) => ({
      gates: { withdraw: vi.fn(demo.gates.withdraw) },
    }));
    renderReview(team);
    await teamReady();
    fireEvent.click(within(card()).getByRole("button", { name: "Withdraw" }));
    expect(confirmation()).toBe(
      "Withdraw G-0011? Claude Code hears that you took the question back on its next read. Nothing is kept.",
    );
    // Cancel is the safe default, and Esc backs out.
    expect(document.activeElement?.textContent).toContain("Cancel");
    press("Escape");
    expect(confirmation()).toBeNull();
    expect(source.gates.withdraw).not.toHaveBeenCalled();
    fireEvent.click(within(card()).getByRole("button", { name: "Withdraw" }));
    fireEvent.click(within(card()).getByRole("button", { name: "Withdraw" }));
    expect(
      await screen.findByText(
        "Withdrew G-0011. Claude Code hears it on its next read.",
      ),
    ).toBeTruthy();
    expect(source.gates.withdraw).toHaveBeenCalledWith(
      expect.objectContaining({
        gate: expect.objectContaining({ ref: "G-0011", version: 1 }),
        idempotencyKey: expect.any(String),
      }),
    );
    await screen.findByText("Memax team · 1 of 3");
  });

  it("says how a gate ended when a 409 meets it, and lets it leave the queue", async () => {
    sourceWith((demo) => ({
      gates: {
        answer: vi.fn().mockRejectedValue(
          new MemaxError("withdrawn", "invalid_transition", 409, {
            ref: "G-0011",
            status: "withdrawn",
          }),
        ),
        get: vi.fn(async (input) => {
          const gate = await demo.gates.get(input);
          return (
            gate && {
              ...gate,
              status: "withdrawn" as const,
              version: 2,
              withdrawn: {
                by: { kind: "agent" as const, agent: "claude-code" },
                at: DEMO_NOW,
              },
            }
          );
        }),
      },
    }));
    renderReview(team);
    await teamReady();
    press("1");
    press("Enter");
    press("Enter");
    expect(
      await screen.findByText(
        "G-0011 wasn't answered. Claude Code took the question back.",
      ),
    ).toBeTruthy();
    await waitFor(() =>
      expect(within(card()).getByText("Withdrawn")).toBeTruthy(),
    );
    expect(
      within(card()).getByText("Claude Code took the question back."),
    ).toBeTruthy();
    expect(within(card()).queryByRole("button", { name: "Answer" })).toBeNull();
    // It stays on screen until you move on, then leaves the queue.
    expect(screen.getByText("Memax team · 1 of 4")).toBeTruthy();
    press("ArrowDown");
    await screen.findByText("Memax team · 1 of 3");
    expect(within(card()).getByText("Codex is waiting on you")).toBeTruthy();
  });

  it("says so when a gate on screen is answered elsewhere, and lets it go when you move on", async () => {
    let answeredElsewhere = false;
    sourceWith((demo) => ({
      gates: {
        waiting: vi.fn(async (input) => {
          const list = await demo.gates.waiting(input);
          return answeredElsewhere
            ? list.filter((g) => g.ref !== "G-0011")
            : list;
        }),
        get: vi.fn(async (input) => {
          const gate = await demo.gates.get(input);
          return (
            gate && {
              ...gate,
              status: "answered" as const,
              version: 2,
              answer: {
                option: 0,
                label: "Keep the ChatGPT names",
                memory: "M-0450",
                by: { kind: "person" as const, self: false },
                at: DEMO_NOW,
              },
            }
          );
        }),
      },
    }));
    const client = renderReview(team);
    await teamReady();
    answeredElsewhere = true;
    await act(() => client.invalidateQueries());
    await waitFor(() =>
      expect(within(card()).getByText("Answered")).toBeTruthy(),
    );
    expect(card().textContent).toContain(
      "A teammate answered it already: “Keep the ChatGPT names”, kept as M-0450.",
    );
    expect(
      within(card()).getByRole("link", { name: "M-0450" }).getAttribute("href"),
    ).toBe("/memax-team/memories/M-0450");
    press("ArrowDown");
    await screen.findByText("Memax team · 1 of 3");
  });

  it("retries a dropped answer with the same idempotency key", async () => {
    const source = sourceWith((demo) => ({
      gates: {
        answer: vi
          .fn(demo.gates.answer)
          .mockRejectedValueOnce(new MemaxError("down", "network_error", 0)),
      },
    }));
    renderReview(team);
    await teamReady();
    press("2");
    press("Enter");
    press("Enter");
    expect(
      await screen.findByText(
        "G-0011 wasn't answered. It didn't reach Memax, so nothing changed. Try again.",
      ),
    ).toBeTruthy();
    // Still asking to confirm the same answer.
    expect(confirmation()).toContain("Align with the core set");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(source.gates.answer).toHaveBeenCalledTimes(2));
    const [first, second] = vi.mocked(source.gates.answer).mock.calls;
    expect(second![0].idempotencyKey).toBe(first![0].idempotencyKey);
    expect(second![0].option).toBe(1);
    expect(
      await screen.findByText(/^Kept M-0439 as your answer to G-0011/),
    ).toBeTruthy();
  });

  it("opens a gate from a link, and says so when the space has none by that ref", async () => {
    h.params = new URLSearchParams("gate=G-0012");
    sourceWith(() => ({}));
    renderReview(team);
    await screen.findByText("Memax team · 2 of 4");
    expect(within(card()).getByText("Codex is waiting on you")).toBeTruthy();
    cleanup();
    h.params = new URLSearchParams("gate=G-0099");
    sourceWith(() => ({}));
    renderReview(team);
    expect(
      await screen.findByText("There's no question G-0099 in Memax team."),
    ).toBeTruthy();
    expect(screen.getByText("Memax team · 1 of 4")).toBeTruthy();
  });

  it("waits for the questions before the first card, and says so when they don't load", async () => {
    let fail!: (err: unknown) => void;
    sourceWith(() => ({
      gates: {
        peekWaiting: undefined,
        waiting: vi.fn(
          () =>
            new Promise<never>((_, reject) => {
              fail = reject;
            }),
        ),
      },
    }));
    renderReview(team);
    // Loading: the queue's skeleton at its rows' heights, and the card's.
    expect(
      await screen.findByRole("status", { name: "Loading the queue" }),
    ).toBeTruthy();
    expect(screen.queryByText(/^Memax team · 1 of/)).toBeNull();
    await act(async () => fail(new MemaxError("down", "network_error", 0)));
    // The memories still review; one quiet line says the questions didn't load.
    expect(
      await screen.findByText("The questions agents asked didn't load."),
    ).toBeTruthy();
    expect(screen.getByText("Memax team · 1 of 2")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });

  it("keeps a viewer to reading: Answer says why, and there's no Withdraw", async () => {
    sourceWith(() => ({}));
    renderReview({ ...team, role: "viewer" });
    await teamReady();
    const answer = within(card()).getByRole("button", { name: "Answer" });
    expect(answer.getAttribute("aria-disabled")).toBe("true");
    expect(answer.title).toBe(
      "Viewers can read the question. A member answers it.",
    );
    expect(
      within(card()).queryByRole("button", { name: "Withdraw" }),
    ).toBeNull();
  });
});
