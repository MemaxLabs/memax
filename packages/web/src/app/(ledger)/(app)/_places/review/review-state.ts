import type { LatestVersion } from "@/lib/v2/data/memories";

/**
 * Review's state machine, pure so its transitions are unit-tested:
 * which card is selected, what this visit has decided, the optimistic
 * seal, the command in flight, and the card's mode (reading, editing,
 * an edit clash, or a reject waiting for its optional reason).
 */

/** The version an edit started from (its If-Match) and its words. */
export interface EditBase {
  version: number;
  statement: string;
}

export type ReviewMode =
  | { kind: "browse" }
  | {
      kind: "editing";
      ref: string;
      base: EditBase;
      draft: string;
      reason: string;
      error: "empty" | null;
    }
  | {
      kind: "clash";
      ref: string;
      base: EditBase;
      mine: string;
      reason: string;
      theirs: LatestVersion;
    }
  | { kind: "rejecting"; ref: string; reason: string };

export interface ReviewState {
  selected: string | null;
  /** Decided during this visit: out of the queue at once, before any refetch. */
  done: Readonly<Record<string, "kept" | "rejected" | "gone">>;
  /**
   * Kept optimistically: the card wears the seal while the command is in
   * flight (with the edited words after an edit). `restore` is the mode
   * a rollback returns to.
   */
  sealed: { ref: string; statement: string | null; restore: ReviewMode } | null;
  /** The memory with a command in flight. */
  busy: string | null;
  mode: ReviewMode;
}

export type ReviewAction =
  | { type: "select"; ref: string }
  | { type: "seal"; ref: string; statement?: string }
  | { type: "unseal"; ref: string }
  | { type: "busy"; ref: string }
  | { type: "settled"; ref: string }
  | {
      type: "decided";
      ref: string;
      // `gone`: decided elsewhere, or no longer there.
      outcome: "kept" | "rejected" | "gone";
      /** The visible queue, in order, before this decision. */
      order: readonly string[];
    }
  | { type: "edit"; ref: string; base: EditBase }
  | { type: "draft"; draft?: string; reason?: string }
  | { type: "editError"; error: "empty" }
  | { type: "clash"; ref: string; theirs: LatestVersion }
  | { type: "combine" }
  | { type: "keepTheirs" }
  | { type: "reject"; ref: string }
  | { type: "browse" };

export const INITIAL_REVIEW: ReviewState = {
  selected: null,
  done: {},
  sealed: null,
  busy: null,
  mode: { kind: "browse" },
};

const BROWSE: ReviewMode = { kind: "browse" };

/** Where the selection goes when `ref` leaves the queue: the next one, else the one before. */
export function nextAfter(
  order: readonly string[],
  ref: string,
): string | null {
  const i = order.indexOf(ref);
  if (i < 0) return order[0] ?? null;
  const rest = order.filter((r) => r !== ref);
  return rest[i] ?? rest[i - 1] ?? null;
}

export function reviewReducer(
  state: ReviewState,
  action: ReviewAction,
): ReviewState {
  switch (action.type) {
    case "select":
      // Never while a command is in flight: the seal stays on its card.
      if (state.busy || state.sealed) return state;
      return { ...state, selected: action.ref, mode: BROWSE };
    case "seal":
      return {
        ...state,
        selected: action.ref,
        sealed: {
          ref: action.ref,
          statement: action.statement ?? null,
          restore: state.mode,
        },
        busy: action.ref,
        mode: BROWSE,
      };
    case "unseal":
      if (state.sealed?.ref !== action.ref) return state;
      return {
        ...state,
        sealed: null,
        busy: null,
        mode: state.sealed.restore,
      };
    case "busy":
      return { ...state, busy: action.ref };
    case "settled":
      return state.busy === action.ref ? { ...state, busy: null } : state;
    case "decided": {
      const wasSelected =
        state.selected === action.ref || state.selected === null;
      return {
        ...state,
        done: { ...state.done, [action.ref]: action.outcome },
        selected: wasSelected
          ? nextAfter(action.order, action.ref)
          : state.selected,
        sealed: state.sealed?.ref === action.ref ? null : state.sealed,
        busy: state.busy === action.ref ? null : state.busy,
        mode: BROWSE,
      };
    }
    case "edit":
      if (state.busy) return state;
      return {
        ...state,
        selected: action.ref,
        mode: {
          kind: "editing",
          ref: action.ref,
          base: action.base,
          draft: action.base.statement,
          reason: "",
          error: null,
        },
      };
    case "draft": {
      const mode = state.mode;
      if (mode.kind === "editing") {
        return {
          ...state,
          mode: {
            ...mode,
            draft: action.draft ?? mode.draft,
            reason: action.reason ?? mode.reason,
            error: null,
          },
        };
      }
      if (mode.kind === "rejecting" && action.reason !== undefined) {
        return { ...state, mode: { ...mode, reason: action.reason } };
      }
      return state;
    }
    case "editError":
      return state.mode.kind === "editing"
        ? { ...state, mode: { ...state.mode, error: action.error } }
        : state;
    case "clash": {
      // A clash answers an edit: it comes back from the optimistic seal.
      const from =
        state.sealed?.ref === action.ref ? state.sealed.restore : state.mode;
      if (from.kind !== "editing") return state;
      return {
        ...state,
        sealed: null,
        busy: null,
        mode: {
          kind: "clash",
          ref: from.ref,
          base: from.base,
          mine: from.draft,
          reason: from.reason,
          theirs: action.theirs,
        },
      };
    }
    case "combine": {
      const mode = state.mode;
      if (mode.kind !== "clash") return state;
      return {
        ...state,
        mode: {
          kind: "editing",
          ref: mode.ref,
          base: {
            version: mode.theirs.version,
            statement: mode.theirs.statement,
          },
          draft: mode.mine,
          reason: mode.reason,
          error: null,
        },
      };
    }
    case "keepTheirs":
      return state.mode.kind === "clash" ? { ...state, mode: BROWSE } : state;
    case "reject":
      if (state.busy) return state;
      return {
        ...state,
        selected: action.ref,
        mode: { kind: "rejecting", ref: action.ref, reason: "" },
      };
    case "browse":
      return { ...state, mode: BROWSE };
  }
}
