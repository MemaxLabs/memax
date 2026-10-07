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
import { DEMO_OVERVIEWS, DEMO_SPACES } from "@/lib/v2/data/demo-dataset";
import { DEMO_CONFLICTS } from "@/lib/v2/data/demo-review-data";
import { createDemoSource } from "@/lib/v2/data/demo-source";
import type { ConflictData } from "@/lib/v2/data/review";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import { KeymapProvider } from "@/lib/v2/keymap/react";
import { createUndoStack } from "@/lib/v2/undo-stack";
import { ToastProvider, ToastViewport } from "../../_components/toasts";
import { LedgerDataProvider } from "../../_lib/data";
import { OverlayProvider } from "../../_lib/overlays";
import { SpaceViewContext } from "../../_lib/space-context";
import { UndoStackProvider } from "../../_lib/undo";
import { ComparePlace } from "./index";

// ReviewConflict on the contract the server serves: the four answers
// with what each does to each side, the ones the person may not take,
// and the answer kept through resolve-conflict, undoable from its toast.

const h = vi.hoisted(() => ({
  source: null as LedgerDataSource | null,
  push: (() => {}) as (href: string) => void,
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
  useRouter: () => ({ push: (href: string) => h.push(href), replace: vi.fn() }),
  usePathname: () => "/memax-v2/review/M-0431/compare",
  useSearchParams: () => new URLSearchParams(),
}));

afterEach(() => {
  cleanup();
  h.source = null;
});

const v2 = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;

function setup(conflict?: ConflictData) {
  const demo = createDemoSource({ streamDelayMs: 0, commandDelayMs: 0 });
  const source: LedgerDataSource = {
    ...demo,
    review: {
      ...demo.review,
      ...(conflict ? { conflict: async () => conflict } : {}),
      resolveConflict: vi.fn(demo.review.resolveConflict),
    },
  };
  h.source = source;
  const push = vi.fn();
  h.push = push;
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <LocaleProvider>
        <LedgerProvider locale="en">
          <LedgerDataProvider mode="demo">
            <UndoStackProvider value={createUndoStack()}>
              <KeymapProvider>
                <ToastProvider>
                  <OverlayProvider>
                    <SpaceViewContext
                      value={{
                        space: v2,
                        overview: DEMO_OVERVIEWS["memax-v2"],
                        overviewFailed: false,
                        retryOverview: () => {},
                      }}
                    >
                      <ComparePlace memoryRef="M-0431" />
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
  return { source, push };
}

const radio = (name: RegExp) => screen.getByRole("radio", { name });

describe("ReviewConflict", () => {
  it("narrows both sides and settles through the ledger, with Undo", async () => {
    const { source, push } = setup();
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Fly.io or Railway for the v2 API?",
      }),
    ).toBeTruthy();
    // The suggested answer: both, each narrowed (not a third memory).
    expect(
      radio(/Both, each with its own scope/).getAttribute("aria-checked"),
    ).toBe("true");
    const mine = screen.getByRole("textbox", {
      name: "M-0431, as it will read",
    }) as HTMLTextAreaElement;
    const theirs = screen.getByRole("textbox", {
      name: "M-0174, as it will read",
    }) as HTMLTextAreaElement;
    expect(mine.value).toBe("The v2 API runs on Fly.io in iad and ams.");
    expect(theirs.value).toBe(
      "Preview environments for pull requests run on Railway.",
    );
    // What it does to each side, then what keeping reaches.
    expect(
      screen.getByText(
        "M-0431 is kept with the narrower words. M-0174 is kept with the narrower words. Keeping it recompiles 4 files and tells Codex, Cursor and Claude Code.",
      ),
    ).toBeTruthy();
    fireEvent.keyDown(radio(/Both/), { key: "Enter" });
    await waitFor(() => expect(push).toHaveBeenCalledWith("/memax-v2/review"));
    expect(source.review.resolveConflict).toHaveBeenCalledWith(
      expect.objectContaining({
        ref: "M-0431",
        other: "M-0174",
        version: 1,
        option: "both",
        statement: "The v2 API runs on Fly.io in iad and ams.",
        otherStatement:
          "Preview environments for pull requests run on Railway.",
      }),
    );
    expect(
      await screen.findByText("Kept M-0431 and M-0174, each narrowed"),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Undo" })).toBeTruthy();
  });

  // Rule 11: narrower words that touch another decision in force are
  // saved and wait for the judge before both are kept.
  const held = {
    ref: "M-0431",
    outcome: "proposed" as const,
    version: 2,
    recompiled: null,
    receipt: null,
    judgePending: true,
  };
  const judgePending = () =>
    new MemaxError(
      "Memax is still checking M-0431 against the decisions in force.",
      "judge_pending",
      503,
      { ref: "M-0431", retry_after: 1 },
      1,
    );
  const checkingLine =
    "Checking the narrower words against the other decisions in force. Both are kept once the check is done.";

  it("holds “both” for the judge, then keeps it with the same answer", async () => {
    const { source, push } = setup();
    const resolve = vi
      .fn(source.review.resolveConflict)
      .mockResolvedValueOnce(held)
      .mockRejectedValueOnce(judgePending())
      .mockResolvedValueOnce({
        ref: "M-0431",
        outcome: "kept",
        version: 2,
        recompiled: null,
        receipt: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
      });
    source.review.resolveConflict = resolve;
    await screen.findByRole("heading", { level: 1 });
    fireEvent.keyDown(radio(/Both/), { key: "Enter" });
    // The working mark and the checking line, never a spinner.
    expect(await screen.findByText(checkingLine)).toBeTruthy();
    expect(screen.getByText("Checking")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Stop checking/ })).toBeTruthy();
    await waitFor(() => expect(push).toHaveBeenCalledWith("/memax-v2/review"), {
      timeout: 6000,
    });
    expect(
      await screen.findByText("Kept M-0431 and M-0174, each narrowed"),
    ).toBeTruthy();
    // Saved, then the same answer on the saved version, with one key
    // across its wait.
    const calls = resolve.mock.calls.map(([input]) => input);
    expect(calls).toHaveLength(3);
    expect(calls[0]).toMatchObject({ version: 1, option: "both" });
    expect(calls[1]).toMatchObject({
      version: 2,
      option: "both",
      statement: calls[0].statement,
      otherStatement: calls[0].otherStatement,
    });
    expect(calls[2].idempotencyKey).toBe(calls[1].idempotencyKey);
    expect(calls[1].idempotencyKey).not.toBe(calls[0].idempotencyKey);
  }, 10_000);

  it("says so when the narrower words contradict another decision in force", async () => {
    const { source, push } = setup();
    source.review.resolveConflict = vi
      .fn(source.review.resolveConflict)
      .mockResolvedValueOnce(held)
      .mockRejectedValueOnce(
        new MemaxError(
          "M-0431 also conflicts with M-0102.",
          "in_conflict",
          409,
          {
            ref: "M-0102",
          },
        ),
      );
    await screen.findByRole("heading", { level: 1 });
    fireEvent.keyDown(radio(/Both/), { key: "Enter" });
    expect(
      await screen.findByText(
        "The narrower words contradict M-0102, a decision in force. Change them, or choose another answer.",
        undefined,
        { timeout: 4000 },
      ),
    ).toBeTruthy();
    // Still here, with the words as written, to change them.
    expect(push).not.toHaveBeenCalled();
    expect(screen.queryByText(checkingLine)).toBeNull();
    const mine = screen.getByRole("textbox", {
      name: "M-0431, as it will read",
    }) as HTMLTextAreaElement;
    expect(mine.value).toBe("The v2 API runs on Fly.io in iad and ams.");
    expect(mine.readOnly).toBe(false);
  }, 10_000);

  it("waits for the judge on the proposal's words before keeping it", async () => {
    const { source, push } = setup();
    const resolve = vi
      .fn(source.review.resolveConflict)
      .mockRejectedValueOnce(judgePending())
      .mockResolvedValueOnce({
        ref: "M-0431",
        outcome: "kept",
        version: 1,
        recompiled: null,
        receipt: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
      });
    source.review.resolveConflict = resolve;
    await screen.findByRole("heading", { level: 1 });
    fireEvent.keyDown(radio(/Fly\.io everywhere/), { key: "1" });
    fireEvent.keyDown(radio(/Fly\.io everywhere/), { key: "Enter" });
    expect(
      await screen.findByText(
        "Checking these words against the other decisions in force. It's settled once the check is done.",
      ),
    ).toBeTruthy();
    await waitFor(() => expect(push).toHaveBeenCalledWith("/memax-v2/review"), {
      timeout: 6000,
    });
    expect(
      await screen.findByText(/^Kept M-0431 as the decision/),
    ).toBeTruthy();
    // One answer, one key across its wait.
    const calls = resolve.mock.calls.map(([input]) => input);
    expect(calls).toHaveLength(2);
    expect(calls[0]).toMatchObject({ option: "proposal" });
    expect(calls[1].idempotencyKey).toBe(calls[0].idempotencyKey);
  }, 10_000);

  it("says so when leaving it open meets another decision in force", async () => {
    const { source, push } = setup();
    source.review.resolveConflict = vi
      .fn(source.review.resolveConflict)
      .mockRejectedValueOnce(judgePending())
      .mockRejectedValueOnce(
        new MemaxError(
          "M-0431 also conflicts with M-0102.",
          "in_conflict",
          409,
          {
            ref: "M-0102",
          },
        ),
      );
    await screen.findByRole("heading", { level: 1 });
    fireEvent.keyDown(radio(/Leave it open/), { key: "4" });
    fireEvent.keyDown(radio(/Leave it open/), { key: "Enter" });
    expect(
      await screen.findByText(
        "M-0431 contradicts M-0102, a decision in force too. Settle that first, or choose another answer.",
        undefined,
        { timeout: 4000 },
      ),
    ).toBeTruthy();
    expect(push).not.toHaveBeenCalled();
  }, 10_000);

  it("stops checking on Esc, and keeps the words", async () => {
    const { source, push } = setup();
    const resolve = vi
      .fn(source.review.resolveConflict)
      .mockResolvedValueOnce(held)
      .mockRejectedValue(judgePending());
    source.review.resolveConflict = resolve;
    await screen.findByRole("heading", { level: 1 });
    fireEvent.keyDown(radio(/Both/), { key: "Enter" });
    await screen.findByText(checkingLine);
    fireEvent.keyDown(radio(/Both/), { key: "Escape" });
    expect(
      await screen.findByText(
        "Stopped checking. Your narrower words stay as you wrote them: keep the decision again to check them.",
      ),
    ).toBeTruthy();
    expect(push).not.toHaveBeenCalled();
    const asked = resolve.mock.calls.length;
    await act(() => new Promise((resolve) => setTimeout(resolve, 1300)));
    expect(resolve.mock.calls.length).toBe(asked);
    // Keeping again checks again, on the saved version, rather than failing.
    fireEvent.keyDown(radio(/Both/), { key: "Enter" });
    expect(await screen.findByText(checkingLine)).toBeTruthy();
    expect(resolve.mock.calls.at(-1)?.[0]).toMatchObject({ version: 2 });
    fireEvent.keyDown(radio(/Both/), { key: "Escape" });
    await screen.findByText(/^Stopped checking/);
    // Esc again leaves for the queue.
    fireEvent.keyDown(radio(/Both/), { key: "Escape" });
    expect(push).toHaveBeenCalledWith("/memax-v2/review");
  }, 10_000);

  it("asks by area when nobody wrote a question, and says why an answer isn't yours", async () => {
    const board = DEMO_CONFLICTS["M-0431"];
    // The SDK's shape: no question, labels or suggestion, compile runs
    // not served, and one answer policy won't let this person take.
    const served: ConflictData = {
      ...board,
      question: null,
      suggested: null,
      recompiles: null,
      tells: [],
      options: board.options.map((o) => ({
        ...o,
        label: null,
        detail: null,
        ...(o.kind === "open"
          ? {
              allowed: false,
              refusal: { code: "decision_needs_web", message: "…" },
            }
          : {}),
      })),
    };
    const { source } = setup(served);
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Which deploy target holds?",
      }),
    ).toBeTruthy();
    // Nothing chosen yet: Keep says what it needs.
    const keep = screen.getByRole("button", { name: /Keep the decision/ });
    expect(keep.getAttribute("aria-disabled")).toBe("true");
    expect(keep.getAttribute("title")).toBe("Choose 1, 2, 3 or 4");
    // The answer policy refuses: disabled, with the reason in words.
    const open = radio(/Leave it open/);
    expect(open.getAttribute("aria-disabled")).toBe("true");
    expect(open.textContent).toContain(
      "Decisions in memax-v2 need a web sign-in to keep.",
    );
    fireEvent.keyDown(open, { key: "4" });
    expect(open.getAttribute("aria-checked")).toBe("false");
    // The catalogue words the answers the server doesn't label.
    fireEvent.keyDown(open, { key: "1" });
    expect(radio(/^Codex's proposal/).getAttribute("aria-checked")).toBe(
      "true",
    );
    const decision = screen.getByRole("textbox", {
      name: "The decision, as it will read",
    }) as HTMLTextAreaElement;
    expect(decision.value).toBe("Deploy the v2 API to Fly.io in iad and ams.");
    expect(decision.readOnly).toBe(true);
    expect(
      screen.getByText(
        "M-0431 is kept. M-0174 is superseded and stops compiling.",
      ),
    ).toBeTruthy();
    fireEvent.keyDown(radio(/M-0174, as kept/), { key: "2" });
    expect(
      screen.getByText("M-0431 is rejected. M-0174 stays as it is."),
    ).toBeTruthy();
    fireEvent.keyDown(radio(/M-0174, as kept/), { key: "Enter" });
    await waitFor(() =>
      expect(source.review.resolveConflict).toHaveBeenCalledWith(
        expect.objectContaining({ option: "kept", other: "M-0174" }),
      ),
    );
    expect(
      await screen.findByText("M-0174 stays in force. Rejected M-0431"),
    ).toBeTruthy();
  });
});
