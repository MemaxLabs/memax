import type { RememberState } from "@/components/bar/remember-row";
import type { BarPhase, MobileComposeState } from "@/lib/bar-interaction";

export type BarSurfaceView = "brain" | "memory" | "none";

export type BarVisibilityState = "hidden" | "rest" | "engaged";

export type BarExpandSurfaceKind =
  | "hidden"
  | "command"
  | "remember"
  | "recall-input"
  | "recall-loading"
  | "recall-result";

export interface DeriveBarSurfaceInput {
  view: BarSurfaceView;
  isMobile: boolean;
  barFocused: boolean;
  barOverlayOpen: boolean;
  barPanelSuppressed: boolean;
  mobileComposeState: MobileComposeState;
  phase: BarPhase;
  value: string;
  recallQuery: string;
  rememberState: RememberState;
  pushing: boolean;
  selectedMemory: boolean;
  hasText: boolean;
  hasFiles: boolean;
  isBarExpanded: boolean;
  isCommandMode: boolean;
  showRecallSurfaces: boolean;
  hasLiftSignal: boolean;
  hasExpandContent: boolean;
}

export interface DerivedBarSurfaceState {
  visibilityState: BarVisibilityState;
  expandSurfaceKind: BarExpandSurfaceKind;
  showExpandSurface: boolean;
  showRememberRow: boolean;
  showSynthesis: boolean;
  showRecallResults: boolean;
  showHintBar: boolean;
}

export function deriveBarSurfaceState(
  input: DeriveBarSurfaceInput,
): DerivedBarSurfaceState {
  const rememberActive = input.rememberState !== "idle" || input.pushing;
  const recallActive =
    input.phase === "loading" ||
    input.phase === "recall-result" ||
    input.recallQuery.length > 0;

  const mountedOnRoute =
    input.view === "brain" || input.view === "memory" || input.barOverlayOpen;
  const visibilityState = !mountedOnRoute
    ? "hidden"
    : input.barPanelSuppressed
      ? "rest"
      : input.hasLiftSignal ||
          rememberActive ||
          recallActive ||
          (input.view === "memory" && input.barFocused)
        ? "engaged"
        : "rest";

  let expandSurfaceKind: BarExpandSurfaceKind = "hidden";
  if (input.isCommandMode) {
    expandSurfaceKind = "command";
  } else if (rememberActive || input.hasFiles) {
    expandSurfaceKind = "remember";
  } else if (input.showRecallSurfaces) {
    if (input.phase === "loading") {
      expandSurfaceKind = "recall-loading";
    } else if (
      input.phase === "recall-result" ||
      input.recallQuery.length > 0
    ) {
      expandSurfaceKind = "recall-result";
    } else if (input.hasText) {
      expandSurfaceKind = "recall-input";
    }
  }

  const showExpandSurface =
    !input.barPanelSuppressed &&
    input.mobileComposeState !== "fullscreen" &&
    (expandSurfaceKind !== "hidden" || input.hasExpandContent);

  // Option C: hide Remember during active recall (loading, or result while the
  // user hasn't edited the query). Show it again when the user starts drafting
  // new content, or when files/expanded/remember-lifecycle override.
  const isActivelyRecalling =
    expandSurfaceKind === "recall-loading" ||
    (expandSurfaceKind === "recall-result" &&
      input.value === input.recallQuery);
  const showRememberRow =
    showExpandSurface &&
    (!isActivelyRecalling ||
      input.hasFiles ||
      input.isBarExpanded ||
      rememberActive);

  return {
    visibilityState,
    expandSurfaceKind,
    showExpandSurface,
    showRememberRow,
    showSynthesis:
      input.showRecallSurfaces &&
      (expandSurfaceKind === "recall-input" ||
        expandSurfaceKind === "recall-loading" ||
        expandSurfaceKind === "recall-result"),
    showRecallResults:
      input.showRecallSurfaces &&
      (expandSurfaceKind === "recall-input" ||
        expandSurfaceKind === "recall-loading" ||
        expandSurfaceKind === "recall-result"),
    showHintBar: !input.isMobile && !input.selectedMemory && !input.hasFiles,
  };
}
