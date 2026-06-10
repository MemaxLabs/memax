"use client";

import { Hourglass, Waves, Infinity as InfinityIcon } from "lucide-react";
import { useLocale } from "@/i18n";

/**
 * StabilityIcon — trailing indicator for the classification sentence on
 * the memory detail page and memory modal. Encodes the memory's decay
 * behavior without leaking schema terms (episodic/rationale/etc.).
 *
 * volatile   → Hourglass  (fades over ~2 weeks)
 * evolving   → Waves      (updates over time)
 * stable     → Infinity   (kept forever)
 *
 * The sentence itself already carries the meaning in plain language;
 * this icon is an ambient reinforcement, not a replacement for copy.
 * Tooltip exposes the per-stability i18n hint.
 */
export function StabilityIcon({
  stability,
  className,
}: {
  stability: string | undefined;
  className?: string;
}) {
  const { t } = useLocale();
  const hint = t.note.stability;
  const size = className ?? "h-3.5 w-3.5 text-fg-3 shrink-0";

  // Icon is visual reinforcement only — the classification sentence
  // already carries the decay behavior in plain language. Mark
  // decorative so screen readers don't double-announce; keep a native
  // `title` tooltip for sighted hover discovery of the concise hint.
  if (stability === "volatile") {
    return (
      <span title={hint.volatile} aria-hidden className="inline-flex">
        <Hourglass className={size} aria-hidden />
      </span>
    );
  }
  if (stability === "stable") {
    return (
      <span title={hint.stable} aria-hidden className="inline-flex">
        <InfinityIcon className={size} aria-hidden />
      </span>
    );
  }
  // evolving is the default fallback (NormalizeMemoryStability → "evolving")
  return (
    <span title={hint.evolving} aria-hidden className="inline-flex">
      <Waves className={size} aria-hidden />
    </span>
  );
}
