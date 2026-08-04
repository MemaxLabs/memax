"use client";

import { useLocale } from "@/i18n";

export type LandingPivot = "personal" | "team";

// Matches the focus-ring convention in hero-waitlist.tsx / pill.tsx.
const FOCUS_RING =
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50";

/**
 * Audience pivot — "For you | For teams" segmented control above the hero
 * headline. Switching is an instant content swap (design rule: intent
 * toggles never fade). Active state is carried by fill, not by a moving
 * pill — layoutId-style sliding indicators are an anti-pattern here (they
 * re-measure on unrelated renders).
 */
export function PivotToggle({
  pivot,
  onChange,
}: {
  pivot: LandingPivot;
  onChange: (pivot: LandingPivot) => void;
}) {
  const { t } = useLocale();

  const options: { key: LandingPivot; label: string }[] = [
    { key: "personal", label: t.landing.pivotPersonal },
    { key: "team", label: t.landing.pivotTeam },
  ];

  return (
    <div
      role="tablist"
      aria-label={`${t.landing.pivotPersonal} / ${t.landing.pivotTeam}`}
      className="inline-flex items-center rounded-full border border-border/60 bg-surface-1 p-0.5"
    >
      {options.map((option) => {
        const active = option.key === pivot;
        return (
          <button
            key={option.key}
            role="tab"
            aria-selected={active}
            onClick={() => onChange(option.key)}
            className={`px-4 py-1.5 rounded-full text-[13px] font-medium transition-colors cursor-pointer ${FOCUS_RING} ${
              active
                ? "bg-foreground text-background"
                : "text-fg-3 hover:text-fg-1"
            }`}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
