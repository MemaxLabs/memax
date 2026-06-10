// @vitest-environment jsdom
import { describe, expect, it, beforeEach, afterEach } from "vitest";
import { act, render } from "@testing-library/react";
import { useEffect } from "react";
import { SelectionProvider, useSelection } from "@/contexts/selection-context";

/**
 * Thin harness: a test-only component that exposes every selection
 * primitive back to the test via a shared ref. Keeps the test free to
 * drive the state machine directly without going through DOM click
 * plumbing.
 */
function Harness({
  onReady,
  initialVisibleIds,
}: {
  onReady: (api: ReturnType<typeof useSelection>) => void;
  initialVisibleIds?: string[];
}) {
  const api = useSelection();
  useEffect(() => {
    onReady(api);
  }, [api, onReady]);
  // Fire setVisibleIds ONCE on mount. Re-running on every `api` identity
  // change (which happens with every selected-set update) would cause
  // reconciliation to fire after every toggle/shiftToggle and drop ids
  // the test just added.
  const setVisibleIds = api.setVisibleIds;
  useEffect(() => {
    if (initialVisibleIds) setVisibleIds(initialVisibleIds);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return null;
}

function mount(initialVisibleIds?: string[]) {
  let latest: ReturnType<typeof useSelection> | null = null;
  render(
    <SelectionProvider>
      <Harness
        initialVisibleIds={initialVisibleIds}
        onReady={(api) => {
          latest = api;
        }}
      />
    </SelectionProvider>,
  );
  return () => {
    if (!latest) throw new Error("selection API not ready");
    return latest;
  };
}

describe("SelectionProvider", () => {
  beforeEach(() => {
    // jsdom keeps window/document across tests; nothing to clear.
  });
  afterEach(() => {
    document.body.innerHTML = "";
  });

  describe("basic lifecycle", () => {
    it("enter + toggle tracks selection", () => {
      const api = mount();
      act(() => api().enter("a"));
      expect(api().active).toBe(true);
      expect([...api().selected]).toEqual(["a"]);
      act(() => api().toggle("b"));
      expect([...api().selected].sort()).toEqual(["a", "b"]);
      act(() => api().toggle("a"));
      expect([...api().selected]).toEqual(["b"]);
    });

    it("exit clears selection + deactivates", () => {
      const api = mount();
      act(() => api().enter("a"));
      act(() => api().toggle("b"));
      act(() => api().exit());
      expect(api().active).toBe(false);
      expect(api().selected.size).toBe(0);
    });

    it("clear empties selection but keeps mode active", () => {
      const api = mount();
      act(() => api().enter("a"));
      act(() => api().clear());
      expect(api().active).toBe(true);
      expect(api().selected.size).toBe(0);
    });
  });

  describe("shiftToggle range", () => {
    it("selects inclusive range from anchor to target when both are visible", () => {
      const api = mount(["a", "b", "c", "d", "e"]);
      act(() => api().enter("a")); // anchor = a
      act(() => api().shiftToggle("c"));
      expect([...api().selected].sort()).toEqual(["a", "b", "c"]);
    });

    it("works in reverse order (anchor below target)", () => {
      const api = mount(["a", "b", "c", "d", "e"]);
      act(() => api().enter("d"));
      act(() => api().shiftToggle("b"));
      expect([...api().selected].sort()).toEqual(["b", "c", "d"]);
    });

    it("consecutive shift-clicks advance the anchor to the last target", () => {
      const api = mount(["a", "b", "c", "d", "e"]);
      act(() => api().enter("a")); // anchor = a
      act(() => api().shiftToggle("c")); // range a..c, anchor = c
      act(() => api().shiftToggle("e")); // range c..e (extends), anchor = e
      expect([...api().selected].sort()).toEqual(["a", "b", "c", "d", "e"]);
    });

    it("falls back to plain toggle when no anchor exists", () => {
      const api = mount(["a", "b", "c"]);
      act(() => {
        // directly call shiftToggle without prior enter/toggle
        api().enter();
      });
      // enter() without id sets active=true but no anchor
      act(() => api().shiftToggle("b"));
      expect([...api().selected]).toEqual(["b"]);
    });

    it("falls back to plain toggle when target is not in visibleIds", () => {
      const api = mount(["a", "b", "c"]);
      act(() => api().enter("a")); // anchor = a
      act(() => api().shiftToggle("z")); // z missing from visibleIds
      expect(api().selected.has("z")).toBe(true); // fell back to plain toggle
    });
  });

  describe("selectAll", () => {
    it("selects every registered visibleId when active", () => {
      const api = mount(["a", "b", "c"]);
      act(() => api().enter());
      act(() => api().selectAll());
      expect([...api().selected].sort()).toEqual(["a", "b", "c"]);
    });

    it("is a no-op when not active (guard lives in provider)", () => {
      const api = mount(["a", "b"]);
      act(() => api().selectAll());
      expect(api().active).toBe(false);
      expect(api().selected.size).toBe(0);
    });

    it("is a no-op when visibleIds is empty", () => {
      const api = mount([]);
      act(() => api().enter());
      act(() => api().selectAll());
      expect(api().selected.size).toBe(0);
    });
  });

  describe("setVisibleIds reconciliation", () => {
    it("drops hidden ids from selection when visibility narrows", () => {
      const api = mount(["a", "b", "c", "d"]);
      act(() => api().enter("a"));
      act(() => api().toggle("b"));
      act(() => api().toggle("c"));
      act(() => api().setVisibleIds(["a", "c"])); // drops b, d never selected
      expect([...api().selected].sort()).toEqual(["a", "c"]);
    });

    it("auto-exits batch mode when reconciliation empties the selection", async () => {
      const api = mount(["a", "b"]);
      act(() => api().enter("a"));
      act(() => api().toggle("b"));
      // Replace visibleIds with a disjoint set — selection should reconcile
      // to empty and auto-exit batch mode (queueMicrotask).
      act(() => api().setVisibleIds(["x", "y"]));
      await act(async () => {
        // flush the queueMicrotask from setVisibleIds
        await Promise.resolve();
      });
      expect(api().active).toBe(false);
      expect(api().selected.size).toBe(0);
    });

    it("no-op reconciliation preserves the same set reference (cheap update)", () => {
      const api = mount(["a", "b", "c"]);
      act(() => api().enter("a"));
      act(() => api().toggle("b"));
      const before = api().selected;
      act(() => api().setVisibleIds(["a", "b", "c", "d"])); // expands universe, all selected still visible
      expect(api().selected).toBe(before);
    });
  });
});
