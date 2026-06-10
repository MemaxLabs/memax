import { describe, expect, it } from "vitest";
import { deriveBarSurfaceState } from "@/lib/bar-surface";

function baseInput() {
  return {
    view: "memory" as const,
    isMobile: false,
    barFocused: false,
    barOverlayOpen: false,
    barPanelSuppressed: false,
    mobileComposeState: "docked" as const,
    phase: "input" as const,
    value: "",
    recallQuery: "",
    rememberState: "idle" as const,
    pushing: false,
    selectedMemory: false,
    hasText: false,
    hasFiles: false,
    isBarExpanded: false,
    isCommandMode: false,
    showRecallSurfaces: true,
    hasLiftSignal: false,
    hasExpandContent: false,
  };
}

describe("deriveBarSurfaceState", () => {
  it("keeps remember surface engaged without draft text", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      rememberState: "sending",
    });

    expect(state.visibilityState).toBe("engaged");
    expect(state.expandSurfaceKind).toBe("remember");
    expect(state.showRememberRow).toBe(true);
  });

  it("prefers command surface over recall input", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      hasText: true,
      isCommandMode: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.expandSurfaceKind).toBe("command");
    expect(state.showSynthesis).toBe(false);
    expect(state.showRememberRow).toBe(true);
  });

  it("uses recall-result surface for persisted recall query", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      recallQuery: "foo",
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.expandSurfaceKind).toBe("recall-result");
    expect(state.showRecallResults).toBe(true);
  });

  it("keeps remember row visible during recall typing", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      hasText: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.expandSurfaceKind).toBe("recall-input");
    expect(state.showRememberRow).toBe(true);
  });

  it("lifts to engaged when memory-view bar is focused", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      barFocused: true,
    });

    expect(state.visibilityState).toBe("engaged");
  });

  // --- Option C: remember row gating during recall ---

  it("hides remember row during recall-loading", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      phase: "loading",
      value: "test query",
      recallQuery: "test query",
      hasText: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.expandSurfaceKind).toBe("recall-loading");
    expect(state.showRememberRow).toBe(false);
  });

  it("hides remember row during recall-result while value === recallQuery", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      phase: "recall-result",
      value: "test query",
      recallQuery: "test query",
      hasText: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.expandSurfaceKind).toBe("recall-result");
    expect(state.showRememberRow).toBe(false);
  });

  it("shows remember row during recall-result when user edits value", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      phase: "recall-result",
      value: "test query edited",
      recallQuery: "test query",
      hasText: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.expandSurfaceKind).toBe("recall-result");
    expect(state.showRememberRow).toBe(true);
  });

  it("shows remember row during recall-loading when files are staged", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      phase: "loading",
      value: "test",
      recallQuery: "test",
      hasText: true,
      hasFiles: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.showRememberRow).toBe(true);
  });

  it("shows remember row during recall-result when bar is expanded", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      phase: "recall-result",
      value: "test",
      recallQuery: "test",
      hasText: true,
      isBarExpanded: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.showRememberRow).toBe(true);
  });

  it("shows remember row during recall when remember is sending (feedback invariant)", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      phase: "recall-result",
      value: "test",
      recallQuery: "test",
      rememberState: "sending",
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.showRememberRow).toBe(true);
  });

  // --- barPanelSuppressed ---

  it("hides expand surface when barPanelSuppressed is true", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      barPanelSuppressed: true,
      hasText: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });

    expect(state.showExpandSurface).toBe(false);
  });

  // --- barPanelSuppressed precedence (regression guards for 2026-04-21
  //     peel-not-clear refactor) ---

  it("suppressed bar rests even when remember is active", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      barPanelSuppressed: true,
      rememberState: "sending",
      pushing: true,
    });
    expect(state.visibilityState).toBe("rest");
  });

  it("suppressed bar rests even when memory-view bar is focused", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      barPanelSuppressed: true,
      view: "memory",
      barFocused: true,
    });
    expect(state.visibilityState).toBe("rest");
  });

  it("suppressed bar rests even when recall is active", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      barPanelSuppressed: true,
      phase: "recall-result",
      recallQuery: "auth",
      hasText: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });
    expect(state.visibilityState).toBe("rest");
  });

  it("suppressed bar rests even when all lift signals are set", () => {
    const state = deriveBarSurfaceState({
      ...baseInput(),
      barPanelSuppressed: true,
      view: "memory",
      barFocused: true,
      phase: "recall-result",
      value: "auth",
      recallQuery: "auth",
      rememberState: "sending",
      pushing: true,
      hasText: true,
      hasFiles: true,
      isBarExpanded: true,
      isCommandMode: true,
      hasLiftSignal: true,
      hasExpandContent: true,
    });
    expect(state.visibilityState).toBe("rest");
    expect(state.showExpandSurface).toBe(false);
  });
});
