"use client";

import { useLocale } from "@/i18n";

// Same fixed dark terminal palette as the Claude Code scenario.
const FG = "text-[oklch(0.85_0_0)]";
const DIM = "text-[oklch(0.55_0_0)]";
const BRIGHT = "text-[oklch(0.93_0_0)]";
const GREEN = { color: "oklch(0.72 0.17 150)" };
const CYAN = { color: "oklch(0.75 0.11 210)" };

/**
 * Coded recreation of the memax CLI — output mirrors packages/cli exactly:
 * `push` prints green "Saved" + bold title + gray id/classification meta;
 * `recall` prints the bold title header with dim classification, cyan
 * relevance score, and the indented summary. Push on Monday, recall in a
 * fresh session on Thursday.
 */
export function CliScenario() {
  const { t } = useLocale();

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 font-mono text-[12px] sm:text-[13px] leading-relaxed bg-[oklch(0.18_0_0)]">
      <p>
        <span className={DIM}>$ </span>
        <span className={BRIGHT}>memax push </span>
        <span className={FG}>
          {'"'}
          {t.landing.termPush}
          {'"'}
        </span>
      </p>
      <p className="mt-1">
        <span style={GREEN}>{t.landing.cliSaved}</span>{" "}
        <span className={`${BRIGHT} font-semibold`}>
          {t.landing.cliSavedTitle}
        </span>
      </p>
      <p className={`pl-2 ${DIM}`}>{t.landing.cliSavedMeta}</p>

      <p className={`mt-4 ${DIM}`}>{t.landing.termComment}</p>
      <p className="mt-1">
        <span className={DIM}>$ </span>
        <span className={BRIGHT}>memax recall </span>
        <span className={FG}>
          {'"'}
          {t.landing.termRecall}
          {'"'}
        </span>
      </p>
      <p className="mt-1">
        <span className={`${BRIGHT} font-semibold`}>
          {t.landing.cliSavedTitle}
        </span>{" "}
        <span className={DIM}>{t.landing.cliResultClass}</span>{" "}
        <span style={CYAN}>{t.landing.cliResultScore}</span>{" "}
        <span className={DIM}>{t.landing.cliResultAge}</span>
      </p>
      <p className={`pl-2 ${FG}`}>{t.landing.termAnswer}</p>
    </div>
  );
}
