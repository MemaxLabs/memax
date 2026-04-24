/**
 * MemaxError — the typed error thrown by the SDK on every non-2xx
 * response. Carries the HTTP status, the machine-readable `code` the
 * server sends in its error envelope, an optional `details` bag, and
 * (for 429 responses) the `Retry-After` advice in seconds.
 *
 * # How to branch on it
 *
 * Three patterns exist, in order of preference:
 *
 *   1. **Status-class getters** (`isRateLimited`, `isForbidden`, …).
 *      Covers the common HTTP semantics in one predicate. Most callers
 *      want this.
 *
 *   2. **Machine code comparison** (`err.code === "hub_frozen"`).
 *      Use when the server returns a business-logic code with no clean
 *      status-class equivalent — e.g. `hub_frozen` is a 409 but means
 *      "billing state", not "optimistic-lock conflict". The two
 *      populate the same status but call for different copy.
 *
 *   3. **Raw status** (`err.status === 418`). Escape hatch. Avoid
 *      unless you genuinely need the number.
 *
 * # Why getters, not a single `kind: "rateLimited" | ...` tag
 *
 * Errors can legitimately be in multiple classes (a 500 is a server
 * error AND, by definition, retryable). Getters compose cleanly and
 * stay open to extension without a breaking enum change. They also
 * keep the public shape narrow: the SDK doesn't need to document a
 * discriminator union that it'd have to evolve on every new status.
 */
export class MemaxError extends Error {
  /**
   * When set (always on 429 responses that carry a Retry-After header),
   * this is the server's advice for how many seconds to wait before
   * retrying. The transport parses both integer-second and HTTP-date
   * forms; undefined means the header was missing or malformed.
   *
   * Rounded up to the nearest whole second for HTTP-date parsing, so
   * callers can always display a clean countdown without needing
   * ceiling logic of their own.
   */
  public retryAfterSeconds?: number;

  constructor(
    message: string,
    public code: string,
    public status: number,
    public details?: Record<string, unknown>,
    retryAfterSeconds?: number,
  ) {
    super(message);
    this.name = "MemaxError";
    this.retryAfterSeconds = retryAfterSeconds;
  }

  /**
   * Rate-limit (HTTP 429). Server also stamps `code === "rate_limited"`.
   * Either signal is load-bearing: callers in browsers may see a 429
   * surfaced by an intermediate proxy (CDN, WAF) without the server's
   * error envelope, so the status-code branch is a genuine second path,
   * not a duplicate.
   */
  get isRateLimited(): boolean {
    return this.status === 429 || this.code === "rate_limited";
  }

  /**
   * Permission denied (HTTP 403). The user is authenticated but the
   * grant does not permit this operation — includes API-key scope
   * failures, hub-member role mismatches, and admin-only endpoint
   * refusals. Distinct from 401 (not authenticated) which should push
   * the user through the re-auth flow rather than showing a toast.
   */
  get isForbidden(): boolean {
    return this.status === 403;
  }

  /**
   * Resource not found (HTTP 404). Usually means the thing the user
   * was operating on no longer exists — deleted by another client,
   * archived by a dream cycle, revoked by an admin. Callers may prefer
   * silent success over an error toast for idempotent deletes, but the
   * default copy ("That no longer exists") is accurate.
   */
  get isNotFound(): boolean {
    return this.status === 404;
  }

  /**
   * State conflict (HTTP 409). Ambiguous on its own — could be
   * optimistic-lock (someone else updated the row mid-edit) or a
   * business-logic refusal (hub_frozen, duplicate_slug). Callers that
   * care about the distinction should check `err.code` first, then
   * fall back to this getter for the generic "refresh and try again"
   * copy.
   */
  get isConflict(): boolean {
    return this.status === 409;
  }

  /**
   * Quota exceeded (HTTP 402). The user hit their plan's limit
   * (memory count, storage bytes, recall count, etc.). Distinct from
   * rate-limited: the answer isn't "wait", it's "upgrade" or "archive
   * something". Surface a link to the plan page, not a countdown.
   */
  get isQuotaExceeded(): boolean {
    return this.status === 402 || this.code === "quota_exceeded";
  }

  /**
   * Authentication failure (HTTP 401). Means the session / token is
   * missing or expired — the user is not authenticated, not that
   * they're authenticated-but-forbidden. Callers almost always want
   * to redirect to sign-in rather than toast.
   */
  get isUnauthorized(): boolean {
    return this.status === 401;
  }

  /**
   * Bad request (HTTP 400). Usually a client bug (malformed payload,
   * invalid UUID, etc.) — shouldn't surface in production UIs. The
   * classifier's copy is deliberately vague because we'd rather the
   * error be reported than guessed-at.
   */
  get isBadRequest(): boolean {
    return this.status === 400;
  }

  /**
   * Server error (HTTP 5xx). The server failed or is momentarily
   * unavailable. Retryable — the classifier suggests "try again in
   * a moment". Distinct from network-offline (status 0) which means
   * the request didn't reach the server at all.
   */
  get isServerError(): boolean {
    return this.status >= 500 && this.status < 600;
  }

  /**
   * Network-layer failure (status 0). The request didn't reach the
   * server — fetch threw (DNS, TCP, TLS, browser offline). The SDK
   * doesn't currently manufacture this status code itself (it wraps
   * fetch throws differently), so this is a forward-compatibility
   * hook for a future transport refactor. Today, callers that want to
   * detect offline should combine `err.status === 0` with
   * `navigator.onLine === false` checks.
   */
  get isNetwork(): boolean {
    return this.status === 0;
  }
}

/**
 * parseRetryAfter — shared helper for 429 Retry-After parsing.
 *
 * The spec (RFC 7231 §7.1.3) allows two formats:
 *   - delta-seconds (a non-negative integer): "120"
 *   - HTTP-date (IMF-fixdate): "Fri, 31 Dec 1999 23:59:59 GMT"
 *
 * We handle both. Returns undefined when the header is missing or
 * unparseable, so callers can treat "don't know how long to wait" the
 * same as "server didn't say".
 *
 * Exported so the transport module and tests can share one code path —
 * otherwise two implementations drift out of sync the first time
 * anyone adds a new edge case (we've shipped that bug before).
 */
export function parseRetryAfter(
  raw: string | null | undefined,
  now: Date = new Date(),
): number | undefined {
  if (!raw) return undefined;
  const trimmed = raw.trim();
  if (!trimmed) return undefined;

  // Reject "number-ish but not delta-seconds" inputs (-5, 1.5, +3, .9)
  // before they reach Date.parse, which would interpret them as
  // ambiguous dates. The spec accepts ONLY `1*DIGIT` for delta-seconds,
  // and every HTTP-date form contains letters ("Mon", "GMT", etc.), so
  // a string that's neither pure-digits nor contains a letter is not
  // a valid Retry-After value.
  if (!/^\d+$/.test(trimmed) && !/[a-zA-Z]/.test(trimmed)) return undefined;

  // Integer seconds — the common case. Regex is deliberate over
  // Number() to reject floats and scientific notation up front.
  if (/^\d+$/.test(trimmed)) {
    const n = parseInt(trimmed, 10);
    return Number.isFinite(n) && n >= 0 ? n : undefined;
  }

  // HTTP-date form — Date.parse handles IMF-fixdate and RFC 850.
  const parsed = Date.parse(trimmed);
  if (Number.isNaN(parsed)) return undefined;
  const deltaMs = parsed - now.getTime();
  if (deltaMs <= 0) return 0;
  // Round up so "wait 0.3 seconds" becomes 1, not 0 — countdowns
  // that briefly display "0s" look broken.
  return Math.ceil(deltaMs / 1000);
}
