// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  render,
  cleanup,
  act,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { useEffect } from "react";
import { SelectionProvider, useSelection } from "@/contexts/selection-context";

// ── Mocks ─────────────────────────────────────────────────────────────────

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      batch: {
        move: "Move",
        copy: "Copy",
        copied: "Copied",
        forget: "Forget",
        forgetConfirm: "Forget {n}?",
        forgetting: "Forgetting {n}…",
        moving: "Moving {n}…",
        export: "Export",
        cancel: "Cancel",
      },
      forget: {
        button: "Forget",
        keep: "Keep",
      },
      bar: {
        dismiss: "Dismiss",
      },
      topics: {
        movePickerHint: "Select a topic",
      },
    },
  }),
  useInterpolate: () => (template: string, vars: Record<string, unknown>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? "")),
  pluralize: (one: string, other: string, n: number) =>
    n === 1
      ? one
      : other.replace(/\{(\w+)\}/g, (_, key) => (key === "n" ? String(n) : "")),
}));

vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("@/lib/batch-active", () => ({
  signalBatchToolbarActive: () => Symbol("id"),
  signalBatchToolbarInactive: () => {},
}));

vi.mock("@/lib/scroll-lock", () => ({
  acquireBodyScrollLock: () => () => {},
}));

// useDestructiveAction is the real 95-line state hook — no mock. Running the
// actual state machine is the only way to drive confirm → running transitions
// in the toolbar (the previous stub kept both flags false forever, making the
// forget-running case untestable).

// DestinationPicker pulls in auth + hub + topic hooks that aren't relevant
// to the toolbar's shell morph. Stub it with a focusable, closeable body
// so the in-shell picker path can be exercised end-to-end: render test-id
// for locator, expose a focusable first row, and call props.onClose() from
// an explicit button so tests can drive picker → actions return.
vi.mock("@/components/features/destination-picker", () => ({
  DestinationPicker: ({ onClose }: { onClose: () => void }) => (
    <div data-testid="batch-picker">
      <button type="button" data-testid="batch-picker-first-row">
        First row
      </button>
      <button type="button" data-testid="batch-picker-close" onClick={onClose}>
        Close
      </button>
    </div>
  ),
}));

// eslint-disable-next-line import/first
import { BatchToolbar } from "./batch-toolbar";

// ── Harness ───────────────────────────────────────────────────────────────

function Trigger({ count }: { count: number }) {
  const selection = useSelection();
  useEffect(() => {
    // Sync selection state to the count prop each render so the parent
    // can transition 0 → 1 → 0 without juggling nested hooks.
    if (count > 0) {
      const ids = Array.from({ length: count }, (_, i) => `id-${i}`);
      selection.enter();
      ids.forEach((id) => selection.toggle(id));
    } else {
      selection.exit();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [count]);
  return null;
}

function Harness({
  count,
  onBatchForget,
  pending,
  pendingLabel,
}: {
  count: number;
  onBatchForget?: (ids: string[]) => Promise<boolean>;
  pending?: boolean;
  pendingLabel?: string;
}) {
  return (
    <SelectionProvider>
      <Trigger count={count} />
      <BatchToolbar
        onBatchForget={onBatchForget ?? (async () => true)}
        onBatchMove={() => {}}
        onCopyContext={() => {}}
        pending={pending}
        pendingLabel={pendingLabel}
      />
    </SelectionProvider>
  );
}

beforeEach(() => {
  document.body.innerHTML = "";
});

afterEach(() => {
  cleanup();
});

// ── Tests ─────────────────────────────────────────────────────────────────

describe("BatchToolbar render regression", () => {
  it("does not throw when selection count transitions 0 → 1 → 0", () => {
    // Regression for commit 7b5fba51's hooks-rule violation. A new useEffect
    // was placed after the `if (!isVisible) return null` early return, so the
    // first selection tick raised "Rendered more hooks than during the
    // previous render" and crashed the toolbar entirely. Hooks now always
    // run above the early return — this test locks that in.
    const onError = vi.fn();
    const originalError = console.error;
    console.error = (...args: unknown[]) => {
      onError(...args);
      // Still log for debugging if the test fails:
      originalError(...args);
    };

    try {
      const { rerender } = render(<Harness count={0} />);
      act(() => {
        rerender(<Harness count={1} />);
      });
      act(() => {
        rerender(<Harness count={3} />);
      });
      act(() => {
        rerender(<Harness count={0} />);
      });
      act(() => {
        rerender(<Harness count={2} />);
      });

      const errorMessages = onError.mock.calls
        .map((call) => String(call[0] ?? ""))
        .join("\n");
      expect(errorMessages).not.toContain("Rendered more hooks");
      expect(errorMessages).not.toContain("Rendered fewer hooks");
    } finally {
      console.error = originalError;
    }
  });

  it("morphs to clean spinner card when forget is running (no nested card, no Moving flash)", async () => {
    // Regression for the nested-card bug: useDestructiveAction keeps
    // confirmingKey set throughout run(), so the confirmation branch
    // would stay mounted while the destructive mutation was in flight,
    // leaving a destructive-tinted card with a destructive-tinted button
    // inside it (visual "card within card"). Fix: gate the confirm branch
    // on !isRunning so it falls through to the clean spinner morph.
    //
    // Second regression guarded here: the label must resolve to
    // "Forgetting" locally, not flash "Moving" during the transient
    // window before the parent's batchDelete.isPending propagates.
    let resolveForget!: (value: boolean) => void;
    const forgetPromise = new Promise<boolean>((resolve) => {
      resolveForget = resolve;
    });
    const onBatchForget = vi.fn(() => forgetPromise);

    const { rerender } = render(
      <Harness count={2} onBatchForget={onBatchForget} />,
    );

    // The toolbar portals into document.body, so scope queries there.
    const toolbarRoot = document.body;

    // Click the main toolbar's Forget button to enter confirming state.
    // (In the default render this is the only button with name "Forget".)
    act(() => {
      fireEvent.click(
        within(toolbarRoot).getByRole("button", { name: /^forget$/i }),
      );
    });

    // Confirmation card is visible with the mocked confirm string, and
    // the Keep button is present while awaiting user decision.
    // (No jest-dom in this project — use plain truthy assertions.)
    expect(within(toolbarRoot).getByText("Forget 2?")).toBeTruthy();
    expect(
      within(toolbarRoot).getByRole("button", { name: /^keep$/i }),
    ).toBeTruthy();

    // In the confirm state the toolbar renders ONLY the destructive card
    // (Forget + Keep buttons) — the main toolbar's buttons are gone.
    // Click the confirm card's Forget button to fire run(). setRunningKey
    // flips synchronously while `onBatchForget` awaits forgetPromise.
    act(() => {
      fireEvent.click(
        within(toolbarRoot).getByRole("button", { name: /^forget$/i }),
      );
    });

    // While forgetPromise is unresolved the toolbar must be in its clean
    // spinner state — no Keep button, no confirm card, no Moving label,
    // and "Forgetting 2…" must be visible.
    await waitFor(() => {
      expect(within(toolbarRoot).queryByText("Forget 2?")).toBeNull();
      expect(
        within(toolbarRoot).queryByRole("button", { name: /^keep$/i }),
      ).toBeNull();
      expect(within(toolbarRoot).getByText("Forgetting 2…")).toBeTruthy();
      // Transient-flash regression guard: must NEVER render the Moving
      // fallback during forget-running, even if parent prop lags.
      expect(within(toolbarRoot).queryByText("Moving 2…")).toBeNull();
    });

    // Resolve and flush.
    await act(async () => {
      resolveForget(true);
      await forgetPromise;
    });

    // After success, selection clears (via run()'s closeOnSuccess). Ensure
    // no error was raised during the morph by rerendering the same props.
    rerender(<Harness count={0} onBatchForget={onBatchForget} />);
  });

  describe("Move picker — in-shell morph (slice 1)", () => {
    it("Move opens the picker inside the same shell (not as a sibling overlay)", async () => {
      render(<Harness count={3} />);
      const root = document.body;

      // Default: action row visible, picker absent.
      expect(
        within(root).getByRole("button", { name: /^move$/i }),
      ).toBeTruthy();
      expect(within(root).queryByTestId("batch-picker")).toBeNull();

      act(() => {
        fireEvent.click(within(root).getByRole("button", { name: /^move$/i }));
      });

      // Picker is now inside the DOM. Action cluster (Copy/Forget) is unmounted
      // because the shell swapped the body region rather than stacking an
      // overlay above the actions.
      await waitFor(() => {
        expect(within(root).getByTestId("batch-picker")).toBeTruthy();
      });
      expect(
        within(root).queryByRole("button", { name: /^copy$/i }),
      ).toBeNull();
      expect(
        within(root).queryByRole("button", { name: /^forget$/i }),
      ).toBeNull();

      // Count + close anchor row still visible (stable across modes).
      expect(within(root).getByText("3")).toBeTruthy();
    });

    it("closing the picker returns to the action row", async () => {
      render(<Harness count={2} />);
      const root = document.body;

      act(() => {
        fireEvent.click(within(root).getByRole("button", { name: /^move$/i }));
      });
      await waitFor(() =>
        expect(within(root).getByTestId("batch-picker")).toBeTruthy(),
      );

      act(() => {
        fireEvent.click(within(root).getByTestId("batch-picker-close"));
      });

      await waitFor(() => {
        expect(within(root).queryByTestId("batch-picker")).toBeNull();
        expect(
          within(root).getByRole("button", { name: /^move$/i }),
        ).toBeTruthy();
        expect(
          within(root).getByRole("button", { name: /^copy$/i }),
        ).toBeTruthy();
      });
    });

    it("Escape closes the picker and returns to actions", async () => {
      render(<Harness count={1} />);
      const root = document.body;

      act(() => {
        fireEvent.click(within(root).getByRole("button", { name: /^move$/i }));
      });
      await waitFor(() =>
        expect(within(root).getByTestId("batch-picker")).toBeTruthy(),
      );

      // Listener is attached on the shell element (bubble phase) so
      // DrillDownTree's nested Esc can go-back before ours closes
      // the picker. Fire Escape on a descendant of the shell — in
      // real usage Esc fires on the focused picker row and bubbles
      // up. Using `picker` as the target ensures we exercise the
      // bubble path that codex asked us to protect.
      act(() => {
        fireEvent.keyDown(within(root).getByTestId("batch-picker"), {
          key: "Escape",
        });
      });

      await waitFor(() => {
        expect(within(root).queryByTestId("batch-picker")).toBeNull();
        expect(
          within(root).getByRole("button", { name: /^move$/i }),
        ).toBeTruthy();
      });
    });

    it("outside pointerdown closes the picker", async () => {
      render(<Harness count={1} />);
      const root = document.body;

      act(() => {
        fireEvent.click(within(root).getByRole("button", { name: /^move$/i }));
      });
      await waitFor(() =>
        expect(within(root).getByTestId("batch-picker")).toBeTruthy(),
      );

      // Clicking somewhere outside the shell closes the picker. The handler
      // is attached at document level; dispatch directly on body.
      act(() => {
        fireEvent.pointerDown(document.body);
      });

      await waitFor(() => {
        expect(within(root).queryByTestId("batch-picker")).toBeNull();
      });
    });
  });

  it("renders the toolbar when count > 0 and returns null when count = 0", () => {
    const { container, rerender } = render(<Harness count={0} />);
    // The toolbar portals to document.body, so the container stays empty.
    // When count = 0, the portal also returns null.
    expect(
      document.body.querySelectorAll("[class*='min-h-[52px]']"),
    ).toHaveLength(0);

    act(() => {
      rerender(<Harness count={2} />);
    });
    // Portal should now have mounted the toolbar into the body.
    expect(
      document.body.querySelectorAll("[class*='min-h-[52px]']").length,
    ).toBeGreaterThan(0);

    act(() => {
      rerender(<Harness count={0} />);
    });
    expect(
      document.body.querySelectorAll("[class*='min-h-[52px]']"),
    ).toHaveLength(0);
    // Suppress the unused-container lint.
    void container;
  });
});
