"use client";

// Top-level 404. Fires when notFound() is called or an unmatched
// route is hit. Visually mirrors the ErrorFallback vernacular
// (centered cluster, ContentError-style destructive dot) without
// a retry CTA — there's nothing to retry for a missing page.

import Link from "next/link";
import { useLocale } from "@/i18n";

export default function NotFound() {
  const { t } = useLocale();
  const copy = t.errors.notFound;
  return (
    <div
      role="alert"
      aria-live="polite"
      className="min-h-[60dvh] flex flex-col items-center justify-center text-center px-6"
    >
      <span
        className="mb-4 inline-block h-2.5 w-2.5 shrink-0 rounded-full"
        style={{
          backgroundColor: "oklch(from var(--destructive) l c h / 0.45)",
        }}
        aria-hidden
      />
      <h1 className="text-display-lg text-fg-1 mb-2">{copy.title}</h1>
      <p className="text-[14px] text-fg-3 max-w-md">{copy.description}</p>
      <div className="mt-6 flex items-center gap-2">
        <Link
          href="/"
          className="rounded-lg bg-fg-1 text-background px-4 py-2 text-[14px] font-medium hover:opacity-90 transition-opacity"
        >
          {copy.backToHome}
        </Link>
        <Link
          href="/login"
          className="rounded-lg border border-border/60 px-4 py-2 text-[14px] text-fg-2 hover:bg-surface-1 hover:text-fg-1 transition-colors"
        >
          {copy.signIn}
        </Link>
      </div>
    </div>
  );
}
