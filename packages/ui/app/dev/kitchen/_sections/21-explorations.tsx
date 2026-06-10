// Maps to: future features (not yet implemented)
// ui/ components: explorations and proposals
"use client";

import { Section, DemoCard, Swatch, SIGNATURE } from "../_shared";

export function ExplorationsSection() {
  return (
    <Section
      title="21. Explorations"
      description="Proposed / Not Yet Implemented"
    >
      <div className="border-t border-border/30 pt-1 mb-2">
        <p className="text-[10px] text-fg-3 uppercase tracking-widest">
          Proposed / Not Yet Implemented
        </p>
      </div>

      {/* 1. Signature Color proposal */}
      <DemoCard label="Signature color proposals">
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
          <Swatch color="#e8956a" label="Option A" token="#e8956a warm peach" />
          <Swatch color="#d4845e" label="Option B" token="#d4845e terra" />
          <Swatch color="#c9916e" label="Option C" token="#c9916e sand" />
          <Swatch
            color="#10b981"
            label="Current (emerald)"
            token="#10b981 emerald"
          />
        </div>
      </DemoCard>

      {/* 2. Nav Dots */}
      <DemoCard label="Navigation dots">
        <div className="flex items-center gap-4">
          {(
            [
              { name: "home", active: true },
              { name: "memories", active: false },
              { name: "dreams", active: false },
              { name: "settings", active: false },
            ] as const
          ).map((dot) => (
            <div key={dot.name} className="flex flex-col items-center gap-1.5">
              <div
                className="rounded-full"
                style={{
                  width: 6,
                  height: 6,
                  ...(dot.active
                    ? { backgroundColor: SIGNATURE }
                    : {
                        border:
                          "1.5px solid oklch(from var(--foreground) l c h / 0.2)",
                      }),
                }}
              />
              <span className="text-[9px] text-fg-3">{dot.name}</span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* 3. Ghost Card Breathing */}
      <DemoCard label="Ghost card breathing">
        <div className="flex items-center gap-3">
          {[0, 1, 2, 3].map((i) => (
            <div
              key={i}
              className={`w-24 h-16 rounded-xl bg-surface-2 ghost-breathe-${i}`}
            />
          ))}
        </div>
      </DemoCard>
    </Section>
  );
}
