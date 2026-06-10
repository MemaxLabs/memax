import type { BarScope } from "@/components/bar/scope-indicator";
import {
  getRawCommandInput,
  type BarPhase,
  type MobileComposeState,
} from "@/lib/bar-interaction";

export interface BarMachineState {
  value: string;
  scope: BarScope;
  phase: BarPhase;
  barFocused: boolean;
  barOverlayOpen: boolean;
  barPanelSuppressed: boolean;
  mobileComposeState: MobileComposeState;
  activeNavigationIndex: number | null;
  isBarExpanded: boolean;
  detectedUrl: string | null;
  memorySearch: string;
  selectedMemoryId: string | null;
  isDragging: boolean;
  /** True while the input textarea has an open IME composition session.
   *  Gates mention-mode + chip-Backspace so CJK/emoji keyboards don't flip
   *  UX mid-compose. */
  composing: boolean;
  /** True when the user explicitly dismissed the route-derived topic chip
   *  on the current topic route. Overrides the route-to-scope inference
   *  in `bar-logo-portal.tsx` and gates the topicId passed to useRecall.
   *  Reset on pathname change — except when the pathname change is
   *  restoring a snapshot (see `BarRestoreSnapshot`), which preserves it. */
  routeTopicDismissed: boolean;
}

export type BarMachineAction =
  | { type: "setValue"; value: string }
  | { type: "setScope"; scope: BarScope }
  | {
      type: "commitCommandScope";
      command: "hub";
      label: string;
      query?: string;
    }
  | { type: "clearScope" }
  | { type: "setPhase"; phase: BarPhase }
  | { type: "setBarFocused"; value: boolean }
  | { type: "setBarOverlayOpen"; value: boolean }
  | { type: "setMobileComposeState"; value: MobileComposeState }
  | { type: "setActiveNavigationIndex"; value: number | null }
  | { type: "setBarExpanded"; value: boolean }
  | { type: "setDetectedUrl"; value: string | null }
  | { type: "setMemorySearch"; value: string }
  | { type: "setSelectedMemoryId"; value: string | null }
  | { type: "setIsDragging"; value: boolean }
  | { type: "setBarPanelSuppressed"; value: boolean }
  | { type: "setComposing"; value: boolean }
  | { type: "dismissRouteTopic" }
  | { type: "clearRouteTopicDismissal" }
  | {
      type: "restoreFromSnapshot";
      snapshot: {
        value: string;
        phase: BarPhase;
        activeNavigationIndex: number | null;
        routeTopicDismissed: boolean;
        isBarExpanded: boolean;
        mobileComposeState: MobileComposeState;
      };
    }
  // Narrower counterpart to restoreFromSnapshot: only reinflates the
  // chrome state (isBarExpanded, mobileComposeState) that peelForNavigation
  // explicitly collapsed on its way out. Used by the return-to-origin
  // branch of the pathname effect, where live state for value / phase /
  // recallQuery / activeNavigationIndex / routeTopicDismissed is already
  // correct (it persisted in memory across the navigation) and must NOT
  // be clobbered by an older snapshot — only the peel-cleared chrome
  // values need to come back so the bar is visibly active on return.
  | {
      type: "restoreChromeAfterPeel";
      isBarExpanded: boolean;
      mobileComposeState: MobileComposeState;
    }
  | { type: "resetUi" };

export type PeelBackStep =
  | "restore-query"
  | "reset-phase"
  | "reset-remember"
  | "clear-selected-memory"
  | "clear-value"
  | "close-mobile-compose"
  | "collapse-expanded"
  | "clear-scope"
  | "hide-overlay"
  | "suppress-panel"
  | "blur"
  | "noop";

export interface PeelBackContext {
  phase: BarPhase;
  value: string;
  recallQuery: string;
  rememberActive: boolean;
  selectedMemory: boolean;
  mobileComposeState: MobileComposeState;
  isBarExpanded: boolean;
  scope: BarScope;
  onTopicRoute: boolean;
  barOverlayOpen: boolean;
  onBarPage: boolean;
  barHasPreservedState: boolean;
}

export function createInitialBarMachineState(): BarMachineState {
  return {
    value: "",
    scope: { kind: "recall" },
    phase: "input",
    barFocused: false,
    barOverlayOpen: false,
    barPanelSuppressed: false,
    mobileComposeState: "docked",
    activeNavigationIndex: null,
    isBarExpanded: false,
    detectedUrl: null,
    memorySearch: "",
    selectedMemoryId: null,
    isDragging: false,
    composing: false,
    routeTopicDismissed: false,
  };
}

export function clearScopeState(state: BarMachineState): BarMachineState {
  if (state.scope.kind === "command") {
    return {
      ...state,
      scope: { kind: "recall" },
      value: getRawCommandInput(state.scope.command),
      activeNavigationIndex: null,
    };
  }
  return {
    ...state,
    scope: { kind: "recall" },
    value: state.value.startsWith("/") ? "" : state.value,
    activeNavigationIndex: state.value.startsWith("/")
      ? null
      : state.activeNavigationIndex,
  };
}

export function getPeelBackStep(context: PeelBackContext): PeelBackStep {
  // Canonical order — see docs/engineering/bar-fixes-2026-04-15.md
  if (
    context.phase === "recall-result" &&
    context.value !== context.recallQuery
  ) {
    return "restore-query"; // 1
  }
  if (context.phase !== "input" || context.recallQuery) return "reset-phase"; // 2
  if (context.rememberActive) return "reset-remember"; // 3
  if (context.selectedMemory) return "clear-selected-memory"; // 4
  if (context.value) return "clear-value"; // 5
  if (context.mobileComposeState !== "docked") return "close-mobile-compose"; // 6
  if (context.isBarExpanded) return "collapse-expanded"; // 7
  if (context.scope.kind !== "recall" || context.onTopicRoute)
    return "clear-scope"; // 8
  if (context.barOverlayOpen) return "hide-overlay"; // 9
  if (context.onBarPage && context.barHasPreservedState)
    return "suppress-panel"; // 10
  return "blur"; // 11
}

// --- Navigation helpers (pure, testable) ---

export interface NavigationMoveResult {
  next: number | null;
  focusInput: boolean;
}

/**
 * Compute the next navigation index given the current state and a delta.
 *
 * Linear/Raycast convention:
 *  - null represents "cursor at input" (no row selected)
 *  - ↓ from null → 0 (first row)
 *  - ↑ from 0 → null + focusInput (return to input)
 *  - ↓ at last → last (clamp, no wrap)
 *  - ↑ at any → prev (clamp at 0, then null on next ↑)
 */
export function computeNextNavigationIndex(
  current: number | null,
  itemCount: number,
  delta: 1 | -1,
): NavigationMoveResult {
  if (itemCount === 0) return { next: null, focusInput: false };

  if (current === null) {
    // At input — ↓ enters list at 0, ↑ stays at input (no-op)
    return delta > 0
      ? { next: 0, focusInput: false }
      : { next: null, focusInput: false };
  }

  const candidate = current + delta;

  if (candidate < 0) {
    // ↑ past top → return to input
    return { next: null, focusInput: true };
  }

  if (candidate >= itemCount) {
    // ↓ past bottom → clamp at last
    return { next: itemCount - 1, focusInput: false };
  }

  return { next: candidate, focusInput: false };
}

export function barMachineReducer(
  state: BarMachineState,
  action: BarMachineAction,
): BarMachineState {
  switch (action.type) {
    case "setValue":
      return { ...state, value: action.value };
    case "setScope":
      return { ...state, scope: action.scope };
    case "commitCommandScope":
      return {
        ...state,
        scope: {
          kind: "command",
          label: action.label,
          command: action.command,
        },
        value: (action.query ?? "").trimStart(),
        activeNavigationIndex: null,
      };
    case "clearScope":
      return clearScopeState(state);
    case "setPhase":
      return { ...state, phase: action.phase };
    case "setBarFocused":
      return { ...state, barFocused: action.value };
    case "setBarOverlayOpen":
      return { ...state, barOverlayOpen: action.value };
    case "setMobileComposeState":
      return { ...state, mobileComposeState: action.value };
    case "setActiveNavigationIndex":
      return { ...state, activeNavigationIndex: action.value };
    case "setBarExpanded":
      return { ...state, isBarExpanded: action.value };
    case "setDetectedUrl":
      return { ...state, detectedUrl: action.value };
    case "setMemorySearch":
      return { ...state, memorySearch: action.value };
    case "setSelectedMemoryId":
      return { ...state, selectedMemoryId: action.value };
    case "setIsDragging":
      return { ...state, isDragging: action.value };
    case "setBarPanelSuppressed":
      if (state.barPanelSuppressed === action.value) return state;
      return { ...state, barPanelSuppressed: action.value };
    case "setComposing":
      if (state.composing === action.value) return state;
      return { ...state, composing: action.value };
    case "dismissRouteTopic":
      if (state.routeTopicDismissed) return state;
      return { ...state, routeTopicDismissed: true };
    case "clearRouteTopicDismissal":
      if (!state.routeTopicDismissed) return state;
      return { ...state, routeTopicDismissed: false };
    case "restoreFromSnapshot":
      return {
        ...state,
        value: action.snapshot.value,
        phase: action.snapshot.phase,
        activeNavigationIndex: action.snapshot.activeNavigationIndex,
        routeTopicDismissed: action.snapshot.routeTopicDismissed,
        isBarExpanded: action.snapshot.isBarExpanded,
        mobileComposeState: action.snapshot.mobileComposeState,
        // A restored snapshot is the source of truth — clear any
        // "peeled" suppression so the expand surface re-emerges.
        barPanelSuppressed: false,
      };
    case "restoreChromeAfterPeel":
      // Idempotent: if the chrome already matches (live state never
      // got peeled or user already reopened the bar manually) return
      // the same state so React can skip the render.
      if (
        state.isBarExpanded === action.isBarExpanded &&
        state.mobileComposeState === action.mobileComposeState &&
        !state.barPanelSuppressed
      ) {
        return state;
      }
      return {
        ...state,
        isBarExpanded: action.isBarExpanded,
        mobileComposeState: action.mobileComposeState,
        barPanelSuppressed: false,
      };
    case "resetUi":
      return {
        ...createInitialBarMachineState(),
        barFocused: state.barFocused,
      };
    default:
      return state;
  }
}
