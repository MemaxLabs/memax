"use client";

import { useLocale } from "@/i18n";

/**
 * Coded recreation of the memax CLI in a shell — push on Monday, recall in
 * a fresh session on Thursday. Dark terminal in both themes; the ✦ is the
 * memax voice marker in signature color.
 */
export function CliScenario() {
  const { t } = useLocale();

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 font-mono text-[12px] sm:text-[13px] leading-relaxed bg-[oklch(0.18_0_0)]">
      <p>
        <span className="text-[oklch(0.55_0_0)]">$ </span>
        <span className="text-[oklch(0.93_0_0)]">memax push </span>
        <span className="text-[oklch(0.75_0_0)]">
          {"“"}
          {t.landing.termPush}
          {"”"}
        </span>
      </p>
      <p className="mt-1">
        <span style={{ color: "var(--signature)" }}>{"✦"} </span>
        <span className="text-[oklch(0.85_0_0)]">
          {t.landing.termRemembered}
        </span>
      </p>

      <p className="mt-4 text-[oklch(0.55_0_0)]">{t.landing.termComment}</p>
      <p className="mt-1">
        <span className="text-[oklch(0.55_0_0)]">$ </span>
        <span className="text-[oklch(0.93_0_0)]">memax recall </span>
        <span className="text-[oklch(0.75_0_0)]">
          {"“"}
          {t.landing.termRecall}
          {"”"}
        </span>
      </p>
      <p className="mt-1">
        <span style={{ color: "var(--signature)" }}>{"✦"} </span>
        <span className="text-[oklch(0.85_0_0)]">{t.landing.termAnswer}</span>
      </p>
    </div>
  );
}
