"use client";

import { useLocale } from "@/i18n";
import { DOCS_URL } from "@/lib/urls";

// Headline numbers from docs.memax.app/quickstart/benchmarks — keep in sync
// with packages/docs-site/content/docs/quickstart/benchmarks.mdx when a new
// eval run changes the published results.
const STATS = [
  { value: "96.9%", labelKey: "benchRecallLabel" as const },
  { value: "91.2%", labelKey: "benchQaLabel" as const },
  { value: "$0.046", labelKey: "benchCostLabel" as const },
];

/**
 * Benchmark proof strip — closes the page after the scenario showcase:
 * the showcase shows what memax looks like, this shows how well it works.
 * Quiet typographic stat row (no cards, no color) matching the overview
 * strip's register, linking to the full published methodology.
 */
export function BenchmarkStrip() {
  const { t } = useLocale();

  return (
    <div className="w-full flex flex-col items-center gap-5">
      <p className="text-fg-4 text-[11px] tracking-[0.08em] uppercase font-medium">
        {t.landing.benchLabel}
      </p>

      <div className="flex flex-wrap items-start justify-center gap-x-10 sm:gap-x-14 gap-y-5">
        {STATS.map((stat) => (
          <div key={stat.labelKey} className="text-center">
            <p
              className="text-[26px] sm:text-[30px] font-semibold text-fg-1 tabular-nums"
              style={{ letterSpacing: "-0.02em" }}
            >
              {stat.value}
            </p>
            <p className="mt-1 text-[12px] sm:text-[13px] text-fg-4">
              {t.landing[stat.labelKey]}
            </p>
          </div>
        ))}
      </div>

      <a
        href={`${DOCS_URL}/quickstart/benchmarks`}
        className="text-[13px] text-fg-3 hover:text-fg-1 underline underline-offset-2 transition-colors"
      >
        {t.landing.benchLink} {"→"}
      </a>
    </div>
  );
}
