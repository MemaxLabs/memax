import { MutationCache, QueryClient } from "@tanstack/react-query";
import { classifyMutationError } from "./error-copy";
import { fireMutationToast } from "./mutation-toast";

// ── Type augmentation ────────────────────────────────────
// Gives every useMutation() a typed `meta` field.
declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: MutationToastMeta;
  }
}

/**
 * Meta payload read by the global MutationCache toast pipeline.
 *
 * # Precedence on error
 *
 * The toast copy comes from (first match wins):
 *
 *   1. `errorMessage(err)` when it's a **function** and returns a
 *      string. Use this for business-logic codes the classifier
 *      can't see — e.g. `hub_frozen` (a 409 that needs different
 *      copy than a generic conflict). Return `undefined` from the
 *      function to fall through to step 2.
 *
 *   2. `classifyMutationError` keyed by `errorAction`. Handles HTTP
 *      status classes (429, 403, 404, 409, 5xx, offline, 402). Also
 *      surfaces `Retry-After` seconds on rate-limit responses.
 *
 *   3. `errorMessage` when it's a **string**. Final fallback for
 *      errors the classifier didn't recognize.
 *
 *   4. `errorFallback` when `errorMessage` is a **function**. Same
 *      final-fallback role as step 3, but applies when a hook uses
 *      the function form (step 1) and both step 1 and step 2 returned
 *      undefined. Without this, function-errorMessage hooks have a
 *      silent-no-toast hole for truly unclassifiable errors.
 *
 * The ordering is deliberate: specific business codes beat status
 * classes (step 1 wins when it returns a string), status classes
 * beat a generic per-callsite fallback (steps 2 > 3/4).
 *
 * # Success copy
 *
 * `successMessage` is simpler — no classification, just the string
 * (or function-returning-string) the mutation wants to show.
 */
export interface MutationToastMeta {
  /** Toast shown on success. String or (data, variables) → string for dynamic messages. */
  successMessage?:
    | string
    | ((data: unknown, variables: unknown) => string | undefined);
  /**
   * Toast shown on error. See precedence doc on MutationToastMeta.
   *
   * - String: used as the final fallback when the classifier didn't
   *   match (step 3).
   * - Function: runs first; may return a string (wins) or undefined
   *   (falls through to the classifier). Pair with `errorFallback`
   *   to provide the step-4 static fallback for unclassifiable
   *   errors.
   */
  errorMessage?: string | ((error: Error) => string | undefined);
  /**
   * Static last-resort copy when `errorMessage` is a function that
   * returned undefined AND the classifier also returned undefined.
   * Without this, unclassifiable errors through the function-form
   * path produce no toast at all — a silent-failure hole.
   *
   * Redundant (and ignored) when `errorMessage` is a string: step 3
   * already provides the static fallback in that case.
   */
  errorFallback?: string;
  /**
   * Short noun phrase from `t.errors.action.*` describing what the
   * user was doing. Used by the classifier to render specific copy
   * for HTTP error classes. Strongly recommended on every mutation —
   * without it, rate-limit errors fall through to the generic
   * `errorMessage` and the user loses the specific "too fast — wait
   * a moment" signal.
   *
   * Example: `errorAction: t.errors.action.moveMemory`
   */
  errorAction?: string;
  /** Set true to suppress the global toast (e.g., push flow with undo handles its own). */
  skipGlobalToast?: boolean;
}

// ── MutationCache — global success/error handler ──────────
// Every mutation with meta.successMessage/errorMessage/errorAction
// gets automatic bar notification feedback. Zero per-callsite wiring.
const mutationCache = new MutationCache({
  onSuccess: (data, variables, _context, mutation) => {
    if (mutation.meta?.skipGlobalToast) return;
    const raw = mutation.meta?.successMessage;
    if (!raw) return;
    const msg = typeof raw === "function" ? raw(data, variables) : raw;
    if (msg) {
      fireMutationToast({
        type: "success",
        message: msg,
        autoDismissMs: 4000,
      });
    }
  },
  onError: (error, _variables, _context, mutation) => {
    if (mutation.meta?.skipGlobalToast) return;

    const rawMessage = mutation.meta?.errorMessage;
    const action: string | undefined =
      typeof mutation.meta?.errorAction === "string"
        ? mutation.meta.errorAction
        : undefined;

    // 1. Function fallback runs first — business-logic codes like
    //    `hub_frozen` need copy the classifier can't generate from
    //    status alone. Functions that want the classifier to take
    //    over return `undefined`.
    if (typeof rawMessage === "function") {
      const fromFn = rawMessage(error as Error);
      if (fromFn) {
        fireMutationToast({
          type: "error",
          message: fromFn,
          autoDismissMs: 5000,
        });
        return;
      }
    }

    // 2. Classifier — HTTP status class → warm, specific copy with
    //    Retry-After countdown on 429. Returns undefined for errors
    //    it doesn't recognize.
    const classified = classifyMutationError(error, { action });
    if (classified) {
      fireMutationToast({
        type: "error",
        message: classified.message,
        autoDismissMs: classified.autoDismissMs,
      });
      return;
    }

    // 3. Static fallback for unclassified errors.
    if (typeof rawMessage === "string") {
      fireMutationToast({
        type: "error",
        message: rawMessage,
        autoDismissMs: 5000,
      });
      return;
    }

    // 4. Function-form fallback — errorMessage was a function that
    //    returned undefined AND the classifier didn't match. Without
    //    this, unclassifiable errors through the function-form path
    //    produce no toast at all, which looks to the user like the
    //    click did nothing.
    const fallback = mutation.meta?.errorFallback;
    if (typeof fallback === "string") {
      fireMutationToast({
        type: "error",
        message: fallback,
        autoDismissMs: 5000,
      });
    }
  },
});

// ── QueryClient ───────────────────────────────────────────
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30 * 1000, // 30 seconds
      gcTime: 5 * 60 * 1000, // 5 minutes
      retry: 1,
      refetchOnWindowFocus: false,
      refetchOnReconnect: true,
    },
  },
  mutationCache,
});
