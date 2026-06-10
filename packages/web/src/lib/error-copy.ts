/**
 * classifyMutationError — the one place that turns a typed SDK error
 * into user-visible copy. Called by the global MutationCache handler
 * (see `query-client.ts`) and by any hook that wants to render error
 * feedback outside the toast path (e.g. inline form errors).
 *
 * # Contract
 *
 * Returns `{ message, autoDismissMs }` when the error belongs to a
 * known HTTP-status class. Returns `undefined` for anything else so
 * callers can fall through to their own copy. The caller MUST provide
 * an `action` key from `t.errors.action.*` that names what the user
 * was doing — the classifier interpolates it into the class template
 * (e.g. "move that memory").
 *
 * # Classes, in order of specificity
 *
 *   isQuotaExceeded  (402)       → upgrade CTA, not a retry
 *   isRateLimited    (429)       → "wait {seconds}s" (uses Retry-After)
 *   isForbidden      (403)       → "you don't have permission"
 *   isUnauthorized   (401)       → silently returns undefined
 *                                  (re-auth flow handles these)
 *   isNotFound       (404)       → "that's gone"
 *   isConflict       (409)       → "someone else changed this"
 *   isBadRequest     (400)       → "that request couldn't be processed"
 *   isServerError    (5xx)       → "memax had a hiccup"
 *   isNetwork / offline (status 0 OR navigator.onLine===false) → offline
 *
 * # autoDismissMs
 *
 * Rate-limit toasts show longer (8s) because the copy asks the user
 * to wait — they need time to read the countdown. Other classes use
 * the system default (5s).
 *
 * # Why not a map?
 *
 * Rule order matters (401 wins over generic "unauthorized", quota
 * wins over rate-limit when both fire somehow). A sequential
 * if-chain is easier to audit than a map that relies on insertion
 * order, and TypeScript catches missing predicates at compile time.
 */
import { MemaxError } from "memax-sdk";
import { interpolate } from "@/i18n/interpolate";
import { getTranslationsSnapshot } from "./translations-snapshot";

export interface MutationErrorClassification {
  /** User-visible toast copy. */
  readonly message: string;
  /**
   * Auto-dismiss in milliseconds. 5s for generic errors, 8s for
   * rate-limit (user needs time to read "try again in Ns"). Pass
   * through to the toast handler.
   */
  readonly autoDismissMs: number;
}

export interface ClassifyMutationErrorOptions {
  /**
   * Short noun phrase naming what the user was doing when this
   * mutation fired. Interpolated as `{action}` in the error
   * templates. Callers should source this from `t.errors.action.*`
   * so both locales get consistent phrasing — e.g.
   * `t.errors.action.moveMemory` → "move that memory".
   *
   * Optional: when missing, templates render with `{action}` left
   * in place (visible-mistake pattern, not silent fallback). Most
   * mutations SHOULD provide one; the optionality is for scaffolding
   * during the rollout.
   */
  readonly action?: string;
}

const RATE_LIMIT_TOAST_MS = 8_000;
const DEFAULT_TOAST_MS = 5_000;

export function classifyMutationError(
  error: unknown,
  opts: ClassifyMutationErrorOptions = {},
): MutationErrorClassification | undefined {
  const t = getTranslationsSnapshot();
  const tmpl = t.errors.mutation;

  // Offline detection fires before the MemaxError path because a
  // fetch that never left the browser can surface in multiple
  // shapes (DOMException, TypeError, MemaxError with status 0).
  // Checking navigator.onLine unifies them.
  if (typeof navigator !== "undefined" && navigator.onLine === false) {
    return {
      message: interpolate(tmpl.offline, {}),
      autoDismissMs: DEFAULT_TOAST_MS,
    };
  }

  if (!(error instanceof MemaxError)) {
    return undefined;
  }

  // 401 is auth-expired — the app's re-auth flow handles the UX
  // (redirect to sign-in). A toast saying "you're not authenticated"
  // would overlap with that and confuse users. Return undefined so
  // the caller's fallback copy can still run, but don't fabricate a
  // specific auth message here.
  if (error.isUnauthorized) {
    return undefined;
  }

  const actionVars = opts.action ? { action: opts.action } : undefined;

  if (error.isQuotaExceeded) {
    return {
      message: interpolate(tmpl.quotaExceeded, actionVars),
      autoDismissMs: DEFAULT_TOAST_MS,
    };
  }

  if (error.isRateLimited) {
    const seconds = error.retryAfterSeconds;
    if (typeof seconds === "number" && seconds > 0) {
      return {
        message: interpolate(tmpl.rateLimitedWithSeconds, {
          ...(actionVars ?? {}),
          seconds,
        }),
        autoDismissMs: RATE_LIMIT_TOAST_MS,
      };
    }
    return {
      message: interpolate(tmpl.rateLimited, actionVars),
      autoDismissMs: RATE_LIMIT_TOAST_MS,
    };
  }

  if (error.isForbidden) {
    return {
      message: interpolate(tmpl.forbidden, actionVars),
      autoDismissMs: DEFAULT_TOAST_MS,
    };
  }

  if (error.isNotFound) {
    return {
      message: interpolate(tmpl.missing, actionVars),
      autoDismissMs: DEFAULT_TOAST_MS,
    };
  }

  if (error.isConflict) {
    return {
      message: interpolate(tmpl.conflict, actionVars),
      autoDismissMs: DEFAULT_TOAST_MS,
    };
  }

  if (error.isBadRequest) {
    return {
      message: interpolate(tmpl.badRequest, actionVars),
      autoDismissMs: DEFAULT_TOAST_MS,
    };
  }

  if (error.isServerError) {
    return {
      message: interpolate(tmpl.server, actionVars),
      autoDismissMs: DEFAULT_TOAST_MS,
    };
  }

  if (error.isNetwork) {
    return {
      message: interpolate(tmpl.offline, actionVars),
      autoDismissMs: DEFAULT_TOAST_MS,
    };
  }

  return undefined;
}
