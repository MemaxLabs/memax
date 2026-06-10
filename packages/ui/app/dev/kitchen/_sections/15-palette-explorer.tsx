// Maps to: globals.css tokens, candidate swatches for review
"use client";

import { Section, DemoCard, StarDemo, SIGNATURE } from "../_shared";
import {
  useKitchen,
  PALETTES,
  ACCENTS,
  type PaletteKey,
} from "../_kitchen-context";

// ─── Text level config for preview ───

const TEXT_LEVELS = [
  { key: "primary", label: "Primary", example: "Memory card title" },
  {
    key: "secondary",
    label: "Secondary",
    example: "Last recalled 3 hours ago from CLI",
  },
  {
    key: "tertiary",
    label: "Tertiary",
    example: "core / api-design / 2024-03-15",
  },
  { key: "muted", label: "Muted", example: "Content hash: 8f3a..." },
] as const;

export function PaletteExplorerSection() {
  const { paletteKey, palette, setPaletteKey, grayOklch } = useKitchen();

  return (
    <Section
      title="15. Palette Explorer"
      description="Interactive theme preview. Switch presets to compare text hierarchy, surfaces, and accents live."
    >
      {/* ── Theme switcher ── */}
      <div className="flex gap-2 mb-4">
        {(Object.keys(PALETTES) as PaletteKey[]).map((key) => (
          <button
            key={key}
            onClick={() => setPaletteKey(key)}
            className={`px-4 py-2 rounded-lg text-[13px] transition-all cursor-pointer ${
              paletteKey === key
                ? "bg-foreground text-background font-medium"
                : "bg-surface-2 text-fg-2 hover:bg-surface-3"
            }`}
          >
            <div className="flex items-center gap-2">
              <div
                className="w-3 h-3 rounded-full border border-border/30"
                style={{ background: PALETTES[key].signature }}
              />
              {PALETTES[key].name}
            </div>
          </button>
        ))}
      </div>

      <p className="text-[12px] text-fg-2 mb-4">{palette.desc}</p>

      {/* ── 1. Text hierarchy preview ── */}
      <DemoCard label="Text hierarchy">
        <div className="space-y-4">
          {/* Side-by-side comparison of all text levels */}
          <div className="grid grid-cols-4 gap-3">
            {TEXT_LEVELS.map(({ key, label, example }) => {
              const opacity = palette.text[key as keyof typeof palette.text];
              return (
                <div key={key} className="space-y-1.5">
                  <div className="flex items-baseline gap-1.5">
                    <span
                      className="text-[12px] font-semibold"
                      style={{
                        color: `oklch(from var(--foreground) l c h / ${opacity})`,
                      }}
                    >
                      {label}
                    </span>
                    <code className="text-[9px] text-fg-3">
                      /{Math.round(opacity * 100)}
                    </code>
                  </div>
                  <p
                    className="text-[13px] leading-relaxed"
                    style={{
                      color: `oklch(from var(--foreground) l c h / ${opacity})`,
                    }}
                  >
                    {example}
                  </p>
                  {/* Opacity bar */}
                  <div className="h-1 rounded-full bg-surface-2 overflow-hidden">
                    <div
                      className="h-full rounded-full"
                      style={{
                        width: `${opacity * 100}%`,
                        background: palette.signature,
                      }}
                    />
                  </div>
                </div>
              );
            })}
          </div>

          {/* Paragraph readability test */}
          <div
            className="rounded-lg p-3 border border-border/50"
            style={{ background: "var(--card)" }}
          >
            <p
              className="text-[14px] font-medium mb-1"
              style={{
                color: `oklch(from var(--foreground) l c h / ${palette.text.primary})`,
              }}
            >
              Readability test on card surface
            </p>
            <p
              className="text-[13px] mb-1"
              style={{
                color: `oklch(from var(--foreground) l c h / ${palette.text.secondary})`,
              }}
            >
              Secondary text should be clearly readable without straining. If
              you squint to read this, the opacity is too low.
            </p>
            <p
              className="text-[12px] mb-1"
              style={{
                color: `oklch(from var(--foreground) l c h / ${palette.text.tertiary})`,
              }}
            >
              Tertiary text recedes but stays legible — timestamps, kinds,
              metadata.
            </p>
            <p
              className="text-[10px]"
              style={{
                color: `oklch(from var(--foreground) l c h / ${palette.text.muted})`,
              }}
            >
              Muted text: placeholders, disabled labels, hashes.
            </p>
          </div>

          {/* Cross-palette comparison table */}
          <div className="overflow-hidden rounded-lg border border-border/20">
            <table className="w-full text-[10px]">
              <thead>
                <tr className="bg-surface-1">
                  <th className="text-left px-2 py-1.5 text-fg-3 font-medium">
                    Level
                  </th>
                  {(Object.keys(PALETTES) as PaletteKey[]).map((key) => (
                    <th
                      key={key}
                      className={`text-center px-2 py-1.5 font-medium ${
                        key === paletteKey ? "text-fg-2" : "text-fg-3"
                      }`}
                    >
                      {PALETTES[key].name}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {TEXT_LEVELS.map(({ key, label }) => (
                  <tr key={key} className="border-t border-border/10">
                    <td className="px-2 py-1.5 text-fg-3">{label}</td>
                    {(Object.keys(PALETTES) as PaletteKey[]).map((pKey) => {
                      const val =
                        PALETTES[pKey].text[
                          key as keyof (typeof PALETTES)[typeof pKey]["text"]
                        ];
                      return (
                        <td
                          key={pKey}
                          className={`text-center px-2 py-1.5 font-mono ${
                            pKey === paletteKey
                              ? "text-fg-2 font-semibold"
                              : "text-fg-3"
                          }`}
                        >
                          {val}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </DemoCard>

      {/* ── 2. Signature color comparison ── */}
      <DemoCard label="Signature color comparison" className="mt-4">
        <div className="flex items-start gap-6">
          {(Object.keys(PALETTES) as PaletteKey[]).map((key) => (
            <div key={key} className="flex flex-col items-center gap-2">
              <div
                className="w-14 h-14 rounded-xl border border-border/30"
                style={{ background: PALETTES[key].signature }}
              />
              <span className="text-[12px] text-fg-2 font-medium">
                {PALETTES[key].name}
              </span>
              <span
                className="text-[15px] leading-none state-slow-breathe"
                style={{ color: PALETTES[key].signature }}
              >
                ✦
              </span>
              <code className="text-[9px] text-fg-4">
                {PALETTES[key].signature}
              </code>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── 3. Gray ramp comparison ── */}
      <DemoCard label="Gray ramp comparison" className="mt-4">
        <div className="space-y-3">
          {/* Safe (pure neutral) baseline */}
          <div>
            <p className="text-[9px] text-fg-3 mb-1">
              Safe Neutral (pure achromatic)
            </p>
            <div className="flex rounded-lg overflow-hidden border border-border/20">
              {PALETTES.safe.grays.map((step) => (
                <div key={step.label} className="flex-1 text-center">
                  <div
                    className="h-10"
                    style={{
                      background: `oklch(${step.l} ${step.c} ${step.h})`,
                    }}
                  />
                  <span className="text-[7px] text-fg-4 leading-tight block mt-0.5">
                    {step.label}
                  </span>
                </div>
              ))}
            </div>
          </div>
          {/* Selected palette tint */}
          {paletteKey !== "safe" && (
            <div>
              <p className="text-[9px] text-fg-3 mb-1">{palette.name} tint</p>
              <div className="flex rounded-lg overflow-hidden border border-border/20">
                {palette.grays.map((step) => (
                  <div key={step.label} className="flex-1 text-center">
                    <div
                      className="h-10"
                      style={{ background: grayOklch(step) }}
                    />
                    <span className="text-[7px] text-fg-4 leading-tight block mt-0.5">
                      {step.label}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </DemoCard>

      {/* ── 4. Full card preview ── */}
      <DemoCard label="Full card preview" className="mt-4">
        <div className="grid grid-cols-2 gap-3">
          {/* Memory card mock */}
          <div
            className="rounded-xl p-4 border border-border/50"
            style={{ background: "var(--card)" }}
          >
            <div className="flex items-center gap-2 mb-2">
              <div className="w-1.5 h-1.5 rounded-full bg-blue-400" />
              <span
                className="text-[13px] font-semibold"
                style={{
                  color: `oklch(from var(--foreground) l c h / ${palette.text.primary})`,
                }}
              >
                API Design Patterns
              </span>
            </div>
            <p
              className="text-[12px] leading-relaxed mb-2"
              style={{
                color: `oklch(from var(--foreground) l c h / ${palette.text.secondary})`,
              }}
            >
              REST endpoints should use plural nouns. Pagination via cursor, not
              offset.
            </p>
            <div className="flex items-center gap-2">
              <span
                className="text-[9px]"
                style={{
                  color: `oklch(from var(--foreground) l c h / ${palette.text.tertiary})`,
                }}
              >
                core / api-design
              </span>
              <span
                className="text-[8px]"
                style={{
                  color: `oklch(from var(--foreground) l c h / ${palette.text.muted})`,
                }}
              >
                3h ago
              </span>
            </div>
          </div>

          {/* Search bar mock */}
          <div
            className="rounded-2xl p-3 flex items-center border border-border/50"
            style={{ background: "var(--card)" }}
          >
            <span
              className="text-[13px] state-slow-breathe mr-2"
              style={{ color: palette.signature }}
            >
              ✦
            </span>
            <span
              className="text-[13px]"
              style={{
                color: `oklch(from var(--foreground) l c h / ${palette.text.muted})`,
              }}
            >
              search your memory...
            </span>
          </div>

          {/* Detail card mock */}
          <div
            className="rounded-xl p-4 border border-border/50 col-span-2"
            style={{ background: "var(--card)" }}
          >
            <div className="flex items-center justify-between mb-2">
              <span
                className="text-[14px] font-semibold"
                style={{
                  color: `oklch(from var(--foreground) l c h / ${palette.text.primary})`,
                }}
              >
                Architecture Decision Records
              </span>
              <span className="px-2 py-0.5 rounded-md text-[9px] font-medium bg-foreground text-background">
                PRO
              </span>
            </div>
            <p
              className="text-[12px] leading-relaxed mb-1.5"
              style={{
                color: `oklch(from var(--foreground) l c h / ${palette.text.secondary})`,
              }}
            >
              We chose River over SQS because Postgres-backed queues simplify
              our infra. Trade-off: no cross-region fan-out, but we don&apos;t
              need it at current scale.
            </p>
            <div className="flex items-center gap-3">
              <span
                className="text-[9px]"
                style={{
                  color: `oklch(from var(--foreground) l c h / ${palette.text.tertiary})`,
                }}
              >
                decisions / infrastructure
              </span>
              <span
                className="text-[8px]"
                style={{
                  color: `oklch(from var(--foreground) l c h / ${palette.text.muted})`,
                }}
              >
                recalled 12 times
              </span>
              <span
                className="text-[8px]"
                style={{
                  color: `oklch(from var(--foreground) l c h / ${palette.text.muted})`,
                }}
              >
                sha:4f2c...
              </span>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* ── 5. Functional accent palette ── */}
      <DemoCard label="Functional accent palette" className="mt-4">
        <div className="grid grid-cols-2 gap-3">
          {ACCENTS.map((color) => (
            <div
              key={color.name}
              className="flex items-start gap-3 p-2.5 rounded-lg bg-surface-1 border border-border/20"
            >
              <div className="flex gap-1 shrink-0">
                <div
                  className="w-8 h-8 rounded-lg border border-border/20"
                  style={{ background: color.oklch }}
                />
                <div
                  className="w-8 h-8 rounded-lg border border-border/20"
                  style={{ background: color.dark }}
                />
              </div>
              <div className="min-w-0">
                <div className="flex items-baseline gap-2">
                  <span className="text-[13px] font-semibold text-fg-1">
                    {color.name}
                  </span>
                  <span className="text-[9px] text-fg-3">{color.use}</span>
                </div>
                <code className="text-[9px] text-fg-3 block mt-0.5">
                  {color.oklch}
                </code>
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── 6. Accents in context ── */}
      <DemoCard label="Accents in context" className="mt-4">
        <div className="space-y-3">
          <div className="flex flex-wrap gap-2">
            <button className="px-4 py-1.5 rounded-lg text-[13px] font-medium bg-foreground text-background">
              Recall
            </button>
            <button className="px-4 py-1.5 rounded-lg text-[13px] font-medium text-fg-2 border border-foreground/20">
              Remember
            </button>
            <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-foreground text-background">
              PRO
            </span>
            <span
              className="px-2 py-0.5 rounded-md text-[10px] font-medium"
              style={{
                background: "oklch(0.72 0.10 155 / 0.15)",
                color: "oklch(0.55 0.12 155)",
              }}
            >
              ● Saved
            </span>
            <span
              className="px-2 py-0.5 rounded-md text-[10px] font-medium"
              style={{
                background: "oklch(0.62 0.22 25 / 0.12)",
                color: "oklch(0.55 0.22 25)",
              }}
            >
              ● Error
            </span>
          </div>
          <div className="text-[13px] text-fg-2">
            Regular text with a{" "}
            <span
              className="underline underline-offset-2"
              style={{ color: "oklch(0.55 0.08 260)" }}
            >
              Dusk link color
            </span>{" "}
            and{" "}
            <span
              className="px-0.5 rounded-sm"
              style={{ background: "oklch(0.82 0.16 85 / 0.3)" }}
            >
              Honeycomb highlight
            </span>
          </div>
        </div>
      </DemoCard>

      {/* ── 7. Design rationale ── */}
      <div className="text-[12px] text-fg-3 mt-2">
        <p>
          2026 trend: warm-tinted grays (Linear, Notion) + bold single accent
          (Superhuman 0.18, Vercel 0.21). Kind dots unchanged — data layer, not
          UI chrome.
        </p>
      </div>
    </Section>
  );
}
