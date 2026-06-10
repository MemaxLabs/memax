"use client";

import { useEffect } from "react";
import Link from "next/link";
import { useLocale } from "@/i18n";
import { reportRenderError } from "@/lib/report-render-error";

/**
 * ErrorFallback — shared render for every segment-level error.tsx.
 *
 * Visual aligns with the existing ContentError vernacular (small
 * destructive dot, not a large warning icon) so error states feel
 * like the rest of the app's failure messaging rather than a scary
 * cross-app overlay. No signature ✦ — that color is reserved for
 * AI work.
 *
 * The primary CTA prefers `unstable_retry` (Next 16.2+) which
 * triggers a router refresh in a transition — it re-fetches data
 * for the segment and then re-renders. `reset` (the legacy prop)
 * only clears error state without re-fetching; it's used as a
 * fallback when `unstable_retry` isn't passed down. A deterministic
 * bug will simply show the fallback again after clicking — there's
 * no auto-retry loop.
 *
 * Telemetry fires once on mount. Tracking itself can't throw; see
 * reportRenderError for the try/catch.
 */
export interface ErrorFallbackProps {
  error: Error & { digest?: string };
  /** Legacy Next App Router reset. Always present. */
  reset: () => void;
  /** Next 16.2+ retry that also refreshes the segment. Preferred
   *  when present — re-fetches data instead of re-rendering stale
   *  state. */
  unstable_retry?: () => void;
  /** Which boundary fired, for telemetry. */
  boundary: string;
  /** Destination of the secondary CTA. */
  homeHref?: string;
  /** Label override for the secondary CTA. Defaults to
   *  t.errors.backToHome. */
  homeLabelKey?: "backToHome" | "backToSignIn";
  /** Extra hint rendered below the description (admin uses this
   *  to tell operators to share the digest with engineering). */
  hint?: string;
}

export function ErrorFallback({
  error,
  reset,
  unstable_retry,
  boundary,
  homeHref = "/home",
  homeLabelKey = "backToHome",
  hint,
}: ErrorFallbackProps) {
  const { t } = useLocale();
  const copy = t.errors;

  useEffect(() => {
    reportRenderError(error, {
      digest: error.digest,
      boundary,
      pathname:
        typeof window !== "undefined" ? window.location.pathname : undefined,
    });
  }, [error, boundary]);

  const primary = unstable_retry ?? reset;
  const isDev = process.env.NODE_ENV !== "production";

  return (
    <div
      role="alert"
      aria-live="assertive"
      className="min-h-[60dvh] flex flex-col items-center justify-center text-center px-6"
    >
      <span
        className="mb-4 inline-block h-2.5 w-2.5 shrink-0 rounded-full state-flash"
        style={{
          backgroundColor: "oklch(from var(--destructive) l c h / 0.75)",
        }}
        aria-hidden
      />
      <h1 className="text-display-lg text-fg-1 mb-2">{copy.title}</h1>
      <p className="text-[14px] text-fg-3 max-w-md">{copy.description}</p>
      {hint ? (
        <p className="mt-2 text-[13px] text-fg-4 max-w-md">{hint}</p>
      ) : null}
      {error.digest ? (
        <p className="mt-3 text-[11px] text-fg-4 font-mono tabular-nums">
          {copy.digestLabel}: {error.digest}
        </p>
      ) : null}

      <div className="mt-6 flex items-center gap-2">
        <button
          type="button"
          onClick={primary}
          className="rounded-lg bg-fg-1 text-background px-4 py-2 text-[14px] font-medium hover:opacity-90 transition-opacity cursor-pointer"
        >
          {copy.tryAgain}
        </button>
        <Link
          href={homeHref}
          className="rounded-lg border border-border/60 px-4 py-2 text-[14px] text-fg-2 hover:bg-surface-1 hover:text-fg-1 transition-colors"
        >
          {copy[homeLabelKey]}
        </Link>
      </div>

      {isDev ? (
        <details className="mt-8 w-full max-w-2xl text-left">
          <summary className="cursor-pointer text-[12px] text-fg-4 hover:text-fg-2">
            {copy.showDetails}
          </summary>
          <pre className="mt-2 p-3 rounded-lg bg-surface-1 text-[11px] text-fg-3 overflow-auto whitespace-pre-wrap break-words">
            {error.message}
            {error.stack ? "\n\n" + error.stack : ""}
          </pre>
        </details>
      ) : null}
    </div>
  );
}
