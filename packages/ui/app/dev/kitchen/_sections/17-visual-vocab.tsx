// Maps to: ui/state-indicator.tsx (● dot, ✦ star)
// ui/ components: StateIndicator (future)
"use client";

import {
  Section,
  DemoCard,
  DotDemo,
  StarDemo,
  SIGNATURE,
  SIGNATURE_MUTED,
  DOT_SIZE,
  STAR_SIZE,
} from "../_shared";

export function VisualVocabSection() {
  return (
    <Section
      title="17. Visual Vocabulary"
      description="Two symbols only: ● dot (content) and ✦ star (memax intelligence). Behavior modifiers signal state."
    >
      <div className="grid grid-cols-2 gap-4">
        <DemoCard label="● Dot — content indicator">
          <div className="space-y-4">
            <DotDemo
              color="var(--foreground)"
              behavior="static"
              label="Static — complete/resting"
            />
            <DotDemo
              color="#3b82f6"
              behavior="fast-pulse"
              label="Fast pulse 0.8s — processing"
            />
            <DotDemo
              color="var(--foreground)"
              behavior="slow-breathe"
              label="Slow breathe 2.5s — idle"
            />
            <DotDemo
              color="var(--foreground)"
              behavior="fade"
              label="Fade out — deleting"
            />
            <DotDemo
              color="var(--destructive)"
              behavior="flash"
              label="Flash — error occurred"
            />
          </div>
        </DemoCard>

        <DemoCard label="✦ Star — memax intelligence">
          <div className="space-y-4">
            <StarDemo behavior="static" label="Static — complete/at rest" />
            <StarDemo
              behavior="fast-pulse"
              label="Fast pulse 0.8s — actively working"
            />
            <StarDemo
              behavior="slow-breathe"
              label="Slow breathe 2.5s — waiting"
            />
            <div className="pt-2 border-t border-border/30">
              <p className="text-[12px] text-fg-3">
                Star uses signature color:{" "}
                <code
                  className="px-1 py-0.5 rounded text-[10px]"
                  style={{ background: SIGNATURE_MUTED, color: SIGNATURE }}
                >
                  oklch(0.62 0.16 290)
                </code>
              </p>
            </div>
          </div>
        </DemoCard>
      </div>

      {/* Size scale */}
      <DemoCard label="Size scale — optical balance" className="mt-4">
        <div className="space-y-3">
          <p className="text-[12px] text-fg-3">
            Star font-size ≈ 1.5× dot diameter for equal visual weight (star
            glyphs have more whitespace than filled circles).
          </p>
          <div className="grid grid-cols-4 gap-4">
            {(["xs", "sm", "md", "lg"] as const).map((s) => (
              <div key={s} className="flex flex-col items-center gap-2">
                <span className="text-[10px] text-fg-3 uppercase font-semibold">
                  {s}
                </span>
                <div className="flex items-center gap-2">
                  <div
                    className={`rounded-full shrink-0 ${DOT_SIZE[s]}`}
                    style={{ backgroundColor: "var(--foreground)" }}
                  />
                  <span
                    className={`leading-none ${STAR_SIZE[s]}`}
                    style={{ color: SIGNATURE }}
                  >
                    ✦
                  </span>
                </div>
                <span className="text-[9px] text-fg-4">
                  {s === "xs"
                    ? "4 / 8px"
                    : s === "sm"
                      ? "6 / 10px"
                      : s === "md"
                        ? "8 / 12px"
                        : "12 / 20px"}
                </span>
                <span className="text-[9px] text-fg-4">
                  {s === "xs"
                    ? "dense lists"
                    : s === "sm"
                      ? "cards (default)"
                      : s === "md"
                        ? "section headers"
                        : "page loading"}
                </span>
              </div>
            ))}
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
