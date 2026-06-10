"use client";

import type { LucideIcon } from "lucide-react";

export type ComingSoonCard = {
  icon: LucideIcon;
  title: string;
  description: string;
};

export function ComingSoonSurface({
  eyebrow,
  title,
  description,
  comingSoonLabel,
  cards,
  footnote,
}: {
  eyebrow: string;
  title: string;
  description: string;
  comingSoonLabel: string;
  cards: ComingSoonCard[];
  footnote?: string;
}) {
  return (
    <div className="space-y-6">
      <div>
        <p className="text-[11px] font-semibold uppercase tracking-[0.08em] text-fg-3">
          {eyebrow}
        </p>
        <h2 className="mt-1 text-[18px] font-semibold tracking-tight text-fg-1">
          {title}
        </h2>
        <p className="mt-1 max-w-3xl text-[13.5px] leading-relaxed text-fg-2">
          {description}
        </p>
      </div>

      <div className="grid gap-3 md:grid-cols-2">
        {cards.map((card) => {
          const Icon = card.icon;
          return (
            <div
              key={card.title}
              className="relative flex items-start gap-3 rounded-surface border border-border/50 bg-card p-5"
            >
              <span className="absolute right-4 top-4 rounded-full border border-border/60 bg-surface-1 px-2 py-0.5 text-[10.5px] font-medium uppercase tracking-wide text-fg-3">
                {comingSoonLabel}
              </span>
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-surface-2 text-fg-2">
                <Icon className="h-4 w-4" />
              </div>
              <div className="min-w-0 pr-16">
                <h3 className="text-[14px] font-semibold text-fg-1">
                  {card.title}
                </h3>
                <p className="mt-1 text-[13px] leading-relaxed text-fg-2">
                  {card.description}
                </p>
              </div>
            </div>
          );
        })}
      </div>

      {footnote ? (
        <p className="max-w-3xl text-[12.5px] leading-relaxed text-fg-3">
          {footnote}
        </p>
      ) : null}
    </div>
  );
}
