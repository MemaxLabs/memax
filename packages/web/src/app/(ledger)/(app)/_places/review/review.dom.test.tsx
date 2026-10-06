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
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { LedgerDataProvider } from "../../_lib/data";
import { OverlayProvider } from "../../_lib/overlays";
import { SpaceViewContext } from "../../_lib/space-context";
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
  },
) {
  const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  const parts = change(demo);
  const source: LedgerDataSource = {
    ...demo,
    review: { ...demo.review, ...parts.review },
    memories: { ...demo.memories, ...parts.memories },
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
            <KeymapProvider>
              <ToastProvider>
                <OverlayProvider>
                  <SpaceViewContext
                    value={{
                      space,
                      overview: DEMO_OVERVIEWS["memax-v2"],
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
    // Undo only where there is an inverse command, and there is none.
    expect(screen.queryByRole("button", { name: "Undo" })).toBeNull();
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
