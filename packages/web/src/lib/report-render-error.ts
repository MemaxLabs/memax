"use client";

import { initPostHog, trackEvent } from "./posthog";

/**
 * Context passed to reportRenderError by Next.js error boundaries.
 * `digest` comes from Next when the error surfaced from a server
 * component — it ties the client row to the server log without
 * sending the full message/stack through PostHog in production.
 */
export interface RenderErrorContext {
  digest?: string;
  /** Route segment the boundary lives in — "app" | "admin" | "auth"
   *  | "global" | "root" | "not-found". Helps PostHog breakdown. */
  boundary: string;
  /** Current pathname at the time of the error, if available. */
  pathname?: string;
}

// UUID v1-v5 with hyphens — matches any object ID we hand out.
const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
// Opaque invite / API-key / nanoid-ish token. Conservative — only
// redacts segments that are long AND look random. Route slugs like
// "admin" or "settings" stay readable; "Jf8a2KcQpR9v...Ub3" does not.
const TOKEN_RE = /^[A-Za-z0-9_-]{24,}$/;
// Digit-only IDs (legacy numeric keys). 6+ digits so pages like
// "/v1" keep their version.
const NUMERIC_ID_RE = /^\d{6,}$/;

/**
 * Redact dynamic / high-cardinality segments out of a pathname
 * before it ships to PostHog. We keep the route *shape* so the
 * breakdown is useful ("/admin/users/:id"), but we strip the
 * identifier itself — UUIDs, invite tokens, API keys, long
 * numeric IDs.
 *
 * Called on the production path only; dev builds see the raw
 * pathname so you can click through from the PostHog row to the
 * failing page.
 */
export function sanitizePathname(path: string): string {
  if (!path) return path;
  // Drop the query string entirely — query params carry their own
  // PII risk (search queries, tokens) and the breakdown doesn't
  // need them.
  const [p] = path.split("?");
  return p
    .split("/")
    .map((seg) => {
      if (!seg) return seg;
      if (UUID_RE.test(seg)) return ":id";
      if (NUMERIC_ID_RE.test(seg)) return ":id";
      if (TOKEN_RE.test(seg)) return ":token";
      return seg;
    })
    .join("/");
}

/**
 * reportRenderError — the single channel for "React render blew up
 * and the user sees a fallback" events.
 *
 * Production payload is intentionally thin:
 *   - error.name, error.digest (server correlation)
 *   - boundary, sanitized pathname
 *   - NO message body and NO stack — user content can end up in
 *     thrown errors (e.g. a memory title that tripped a parser),
 *     and we don't want PostHog to inherit our PII surface area.
 *   - Pathname is scrubbed of UUIDs / tokens / long numeric IDs so
 *     we can't leak invite tokens or resource IDs into analytics.
 *
 * Development payload adds message + stack + raw pathname so local
 * dev still gets the full picture in the PostHog UI without needing
 * to peek at the browser console.
 *
 * initPostHog() is idempotent so calling it here covers the
 * global-error case where the Providers tree never mounted.
 */
export function reportRenderError(
  error: unknown,
  context: RenderErrorContext,
): void {
  if (typeof window === "undefined") return;
  try {
    initPostHog();
    const err = error instanceof Error ? error : new Error(String(error));
    const isProd = process.env.NODE_ENV === "production";
    const pathname = context.pathname
      ? isProd
        ? sanitizePathname(context.pathname)
        : context.pathname
      : undefined;
    const base: Record<string, unknown> = {
      name: err.name,
      digest: context.digest,
      boundary: context.boundary,
      pathname,
    };
    if (!isProd) {
      base.message = err.message;
      base.stack = err.stack;
    }
    trackEvent("web.render_error", base);
  } catch {
    // Telemetry must never throw on the error-boundary path.
    // Swallowing here is the safe choice — if PostHog itself fails
    // we still want the fallback UI to render for the user.
  }
}
