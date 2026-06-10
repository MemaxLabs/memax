import { describe, expect, it } from "vitest";
import {
  barMachineReducer,
  clearScopeState,
  computeNextNavigationIndex,
  createInitialBarMachineState,
  getPeelBackStep,
} from "@/lib/bar-machine";

function basePeelBackContext(): import("@/lib/bar-machine").PeelBackContext {
  return {
    phase: "input",
    value: "",
    recallQuery: "",
    rememberActive: false,
    selectedMemory: false,
    mobileComposeState: "docked",
    isBarExpanded: false,
    scope: { kind: "recall" },
    onTopicRoute: false,
    barOverlayOpen: false,
    onBarPage: true,
    barHasPreservedState: false,
  };
}

describe("bar machine", () => {
  it("commits command scope into chip state", () => {
    const next = barMachineReducer(createInitialBarMachineState(), {
      type: "commitCommandScope",
      command: "hub",
      label: "Hub",
      query: "memax",
    });
    expect(next.scope).toEqual({
      kind: "command",
      label: "Hub",
      command: "hub",
    });
    expect(next.value).toBe("memax");
  });

  it("degrades command scope back to raw slash text", () => {
    const next = clearScopeState({
      ...createInitialBarMachineState(),
      scope: { kind: "command", label: "Hub", command: "hub" },
      value: "",
    });
    expect(next.scope).toEqual({ kind: "recall" });
    expect(next.value).toBe("/hub");
  });

  it("tracks routeTopicDismissed with dismiss + clear actions", () => {
    const initial = createInitialBarMachineState();
    expect(initial.routeTopicDismissed).toBe(false);
    const dismissed = barMachineReducer(initial, { type: "dismissRouteTopic" });
    expect(dismissed.routeTopicDismissed).toBe(true);
    const cleared = barMachineReducer(dismissed, {
      type: "clearRouteTopicDismissal",
    });
    expect(cleared.routeTopicDismissed).toBe(false);
  });

  it("tracks composing via setComposing", () => {
    const initial = createInitialBarMachineState();
    expect(initial.composing).toBe(false);
    const composing = barMachineReducer(initial, {
      type: "setComposing",
      value: true,
    });
    expect(composing.composing).toBe(true);
    const done = barMachineReducer(composing, {
      type: "setComposing",
      value: false,
    });
    expect(done.composing).toBe(false);
  });

  it("restoreFromSnapshot hydrates preserved fields + clears barPanelSuppressed", () => {
    const initial = {
      ...createInitialBarMachineState(),
      barPanelSuppressed: true,
      value: "stale",
      phase: "input" as const,
    };
    const next = barMachineReducer(initial, {
      type: "restoreFromSnapshot",
      snapshot: {
        value: "auth flow",
        phase: "recall-result",
        activeNavigationIndex: 2,
        routeTopicDismissed: true,
        isBarExpanded: true,
        mobileComposeState: "mirror",
      },
    });
    expect(next.value).toBe("auth flow");
    expect(next.phase).toBe("recall-result");
    expect(next.activeNavigationIndex).toBe(2);
    expect(next.routeTopicDismissed).toBe(true);
    expect(next.isBarExpanded).toBe(true);
    expect(next.mobileComposeState).toBe("mirror");
    expect(next.barPanelSuppressed).toBe(false);
  });

  it("clears plain slash discovery entirely", () => {
    const next = clearScopeState({
      ...createInitialBarMachineState(),
      value: "/h",
    });
    expect(next.scope).toEqual({ kind: "recall" });
    expect(next.value).toBe("");
  });

  it("peels back in the right order", () => {
    expect(
      getPeelBackStep({
        ...basePeelBackContext(),
        phase: "recall-result",
        value: "edited",
        recallQuery: "original",
        mobileComposeState: "mirror",
        isBarExpanded: true,
        scope: { kind: "command", label: "Hub", command: "hub" },
        barOverlayOpen: true,
      }),
    ).toBe("restore-query");

    expect(
      getPeelBackStep({
        ...basePeelBackContext(),
        mobileComposeState: "mirror",
      }),
    ).toBe("close-mobile-compose");

    expect(
      getPeelBackStep({
        ...basePeelBackContext(),
        scope: { kind: "command", label: "Hub", command: "hub" },
      }),
    ).toBe("clear-scope");
  });

  it("resets stale recall layer before blur when phase already returned to input", () => {
    expect(
      getPeelBackStep({
        ...basePeelBackContext(),
        recallQuery: "keep",
      }),
    ).toBe("reset-phase");
  });

  it("clears remember feedback as its own peel-back layer", () => {
    expect(
      getPeelBackStep({
        ...basePeelBackContext(),
        rememberActive: true,
      }),
    ).toBe("reset-remember");
  });

  it("clears selected memory before clearing value", () => {
    expect(
      getPeelBackStep({
        ...basePeelBackContext(),
        selectedMemory: true,
        value: "draft text",
      }),
    ).toBe("clear-selected-memory");
  });

  it("clears scope on topic route even when scope is recall", () => {
    expect(
      getPeelBackStep({
        ...basePeelBackContext(),
        onTopicRoute: true,
      }),
    ).toBe("clear-scope");
  });

  it("suppresses panel on bar page with preserved state", () => {
    expect(
      getPeelBackStep({
        ...basePeelBackContext(),
        onBarPage: true,
        barHasPreservedState: true,
      }),
    ).toBe("suppress-panel");
  });

  it("blurs when nothing to peel on bar page without state", () => {
    expect(getPeelBackStep(basePeelBackContext())).toBe("blur");
  });
});

describe("computeNextNavigationIndex", () => {
  it("↓ from null (input) enters list at 0", () => {
    expect(computeNextNavigationIndex(null, 5, 1)).toEqual({
      next: 0,
      focusInput: false,
    });
  });

  it("↑ from null (input) stays at input", () => {
    expect(computeNextNavigationIndex(null, 5, -1)).toEqual({
      next: null,
      focusInput: false,
    });
  });

  it("↑ from 0 returns to input with focusInput=true", () => {
    expect(computeNextNavigationIndex(0, 5, -1)).toEqual({
      next: null,
      focusInput: true,
    });
  });

  it("↓ from 0 moves to 1", () => {
    expect(computeNextNavigationIndex(0, 5, 1)).toEqual({
      next: 1,
      focusInput: false,
    });
  });

  it("↓ at last index clamps (no wrap)", () => {
    expect(computeNextNavigationIndex(4, 5, 1)).toEqual({
      next: 4,
      focusInput: false,
    });
  });

  it("↑ at last index moves to prev", () => {
    expect(computeNextNavigationIndex(4, 5, -1)).toEqual({
      next: 3,
      focusInput: false,
    });
  });

  it("↓ from middle moves down", () => {
    expect(computeNextNavigationIndex(2, 5, 1)).toEqual({
      next: 3,
      focusInput: false,
    });
  });

  it("↑ from middle moves up", () => {
    expect(computeNextNavigationIndex(2, 5, -1)).toEqual({
      next: 1,
      focusInput: false,
    });
  });

  it("empty list always returns null, focusInput=false", () => {
    expect(computeNextNavigationIndex(null, 0, 1)).toEqual({
      next: null,
      focusInput: false,
    });
    expect(computeNextNavigationIndex(null, 0, -1)).toEqual({
      next: null,
      focusInput: false,
    });
  });

  it("single item: ↓ from null enters 0, ↓ from 0 clamps, ↑ from 0 returns to input", () => {
    expect(computeNextNavigationIndex(null, 1, 1)).toEqual({
      next: 0,
      focusInput: false,
    });
    expect(computeNextNavigationIndex(0, 1, 1)).toEqual({
      next: 0,
      focusInput: false,
    });
    expect(computeNextNavigationIndex(0, 1, -1)).toEqual({
      next: null,
      focusInput: true,
    });
  });
});
