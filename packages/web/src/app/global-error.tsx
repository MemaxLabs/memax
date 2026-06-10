"use client";

// global-error.tsx — ultimate fallback, replaces the root layout.
//
// Next mounts this ABOVE the root layout, so there is no i18n
// Provider, no font variables, no design tokens from the normal
// tree. We import ./globals.css directly so the destructive color
// tokens + font stack still resolve, and we ship our own <html>
// + <body> per Next's requirement.
//
// Keep this file dependency-free on the render path: it must
// render even if an import in the providers tree throws. The
// reporter is dynamic-imported inside an effect so a broken
// posthog chunk can't take down the fallback itself.

import { useEffect, useState } from "react";
import "./globals.css";

type Locale = "en" | "zh";

// Local dictionary. Only the strings this fallback needs; nothing
// else. Deliberately not reusing t.errors so a corrupted i18n
// bundle still renders readable copy.
const COPY: Record<
  Locale,
  {
    title: string;
    description: string;
    tryAgain: string;
    backToHome: string;
    digestLabel: string;
  }
> = {
  en: {
    title: "Something went wrong",
    description:
      "An unexpected error prevented the page from rendering. The error has been logged.",
    tryAgain: "Try again",
    backToHome: "Back to home",
    digestLabel: "Digest",
  },
  zh: {
    title: "出了点问题",
    description: "页面渲染时遇到意外错误。该错误已记录。",
    tryAgain: "重试",
    backToHome: "返回首页",
    digestLabel: "Digest",
  },
};

function resolveClientLocale(): Locale {
  try {
    const stored = window.localStorage.getItem("memax-locale");
    if (stored === "zh" || stored === "en") return stored;
  } catch {
    // localStorage can throw in private mode; fall through.
  }
  const lang =
    typeof navigator !== "undefined" ? navigator.language.toLowerCase() : "";
  return lang.startsWith("zh") ? "zh" : "en";
}

export default function GlobalError({
  error,
  reset,
  unstable_retry,
}: {
  error: Error & { digest?: string };
  reset: () => void;
  unstable_retry?: () => void;
}) {
  // SSR renders "en"; we swap to the user's preference after mount
  // so the initial client markup matches SSR and avoids a
  // hydration warning on zh users.
  const [locale, setLocale] = useState<Locale>("en");
  useEffect(() => {
    setLocale(resolveClientLocale());
  }, []);

  useEffect(() => {
    // Dynamic import keeps the telemetry dependency off the render
    // path — if posthog-js fails to load, the fallback still shows.
    void import("@/lib/report-render-error")
      .then(({ reportRenderError }) => {
        reportRenderError(error, {
          digest: error.digest,
          boundary: "global",
          pathname:
            typeof window !== "undefined"
              ? window.location.pathname
              : undefined,
        });
      })
      .catch(() => {
        // Swallow — telemetry must never break the fallback.
      });
  }, [error]);

  const copy = COPY[locale];
  const primary = unstable_retry ?? reset;

  return (
    <html lang={locale}>
      <head>
        <title>{copy.title}</title>
      </head>
      <body>
        <div
          role="alert"
          aria-live="assertive"
          style={{
            minHeight: "100dvh",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            textAlign: "center",
            padding: "24px",
            fontFamily:
              "var(--font-sans, system-ui, -apple-system, Segoe UI, Roboto, sans-serif)",
            backgroundColor: "var(--background, #fafafa)",
            color: "var(--foreground, #111)",
          }}
        >
          <span
            aria-hidden
            style={{
              display: "inline-block",
              width: 10,
              height: 10,
              borderRadius: 9999,
              marginBottom: 16,
              backgroundColor: "oklch(0.65 0.18 25 / 0.75)",
            }}
          />
          <h1
            style={{
              fontSize: 24,
              fontWeight: 600,
              letterSpacing: "-0.02em",
              margin: 0,
              marginBottom: 8,
            }}
          >
            {copy.title}
          </h1>
          <p
            style={{
              fontSize: 14,
              opacity: 0.65,
              maxWidth: 420,
              margin: 0,
              lineHeight: 1.6,
            }}
          >
            {copy.description}
          </p>
          {error.digest ? (
            <p
              style={{
                marginTop: 12,
                fontSize: 11,
                fontFamily: "var(--font-mono, ui-monospace, monospace)",
                opacity: 0.5,
              }}
            >
              {copy.digestLabel}: {error.digest}
            </p>
          ) : null}
          <div style={{ marginTop: 24, display: "flex", gap: 8 }}>
            <button
              type="button"
              onClick={primary}
              style={{
                padding: "8px 16px",
                fontSize: 14,
                fontWeight: 500,
                borderRadius: 8,
                border: 0,
                cursor: "pointer",
                background: "var(--foreground, #111)",
                color: "var(--background, #fafafa)",
              }}
            >
              {copy.tryAgain}
            </button>
            <a
              href="/"
              style={{
                padding: "8px 16px",
                fontSize: 14,
                borderRadius: 8,
                border: "1px solid rgba(0,0,0,0.12)",
                textDecoration: "none",
                color: "inherit",
              }}
            >
              {copy.backToHome}
            </a>
          </div>
        </div>
      </body>
    </html>
  );
}
