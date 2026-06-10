// Maps to: ui/surface.tsx (5 variants), bar, dropdowns, modals, glass edges
// Source of truth for container decisions: which variant, which border, which shadow
"use client";

import { Section, DemoCard } from "../_shared";
import { Surface } from "@/components/surface";

/* ── Elevation Hierarchy ── */

const ELEVATIONS = [
  {
    level: "0 — Page",
    token: "--background",
    tailwind: "bg-background",
    desc: "Page canvas, scroll body. No border, no shadow.",
    usage: "Layout root, brain view scroll area",
  },
  {
    level: "1 — Content",
    token: "--card",
    tailwind: 'Surface variant="subtle"',
    desc: "Content container. Subtle border (--border), no shadow.",
    usage: "Memory detail body (desktop), settings sections",
  },
  {
    level: "2 — Elevated",
    token: "--card + bar-border + bar-shadow",
    tailwind: 'Surface variant="default"',
    desc: "Primary cards. Bar-border (12% opacity) + layered shadow.",
    usage: "Memory grid cards, topic cards, bar, floating panels",
  },
  {
    level: "3 — Floating",
    token: "--popover + glass",
    tailwind: "glass / glass-shadow",
    desc: "Above-page overlays. Glass blur + frosted edge.",
    usage: "Dropdowns, popovers, command palette",
  },
  {
    level: "4 — Modal",
    token: "--card + backdrop",
    tailwind: "z-65 + backdrop",
    desc: "Full-screen overlay with backdrop dimming.",
    usage: "MemoryModal, settings dialog, confirmations",
  },
];

/* ── Surface Variants ── */

const VARIANTS: {
  variant: "default" | "subtle" | "flat" | "borderless" | "clean";
  label: string;
  desc: string;
  usage: string;
  border: string;
  shadow: string;
}[] = [
  {
    variant: "default",
    label: "Default",
    desc: "Full elevation — bar-border + bar-shadow",
    usage: "Memory grid, topic cards, bar",
    border: "var(--bar-border)",
    shadow: "var(--bar-shadow)",
  },
  {
    variant: "subtle",
    label: "Subtle",
    desc: "Light border, no shadow — recedes behind content",
    usage: "Memory detail body (desktop), inline containers",
    border: "var(--border)",
    shadow: "none",
  },
  {
    variant: "flat",
    label: "Flat",
    desc: "Same as subtle (alias) — semantic distinction for different context",
    usage: "Inline sections, nested containers",
    border: "var(--border)",
    shadow: "none",
  },
  {
    variant: "borderless",
    label: "Borderless",
    desc: "No border, no shadow, no rounding — full bleed",
    usage: "Mobile detail page, fullscreen views",
    border: "none",
    shadow: "none",
  },
  {
    variant: "clean",
    label: "Clean",
    desc: "Card background only, no border — minimal container",
    usage: "Minimal containers, collapsed sections",
    border: "none",
    shadow: "none",
  },
];

/* ── Border Radius Scale ── */

const RADII = [
  {
    name: "Surface",
    value: "20px",
    tailwind: "rounded-surface",
    usage: "Cards, dialogs, popovers, bar, form sub-cards, containers",
  },
  {
    name: "Chrome",
    value: "14px",
    tailwind: "rounded-chrome",
    usage: "Buttons, inputs, chips, role tags, code pills — chrome ≤40px",
  },
  {
    name: "Pill",
    value: "9999px",
    tailwind: "rounded-full",
    usage: "Circles (avatars, dots) + status pills ≤24px tall",
  },
];

export function SurfacesSection() {
  return (
    <Section
      title="14. Surfaces"
      description="5-level elevation hierarchy + 5 Surface variants. Every container in the product uses one of these combinations."
    >
      {/* ── Elevation Hierarchy ── */}
      <DemoCard label="Elevation hierarchy (5 levels)">
        <div className="space-y-0">
          {ELEVATIONS.map((e, i) => (
            <div
              key={e.level}
              className="flex items-start gap-4 py-3 border-b border-border/10 last:border-0"
            >
              <div className="w-6 h-6 rounded-md bg-surface-2 flex items-center justify-center shrink-0">
                <span className="text-[11px] font-mono font-bold text-fg-2">
                  {i}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-baseline gap-2 mb-0.5">
                  <span className="text-[13px] font-semibold text-fg-1">
                    {e.level}
                  </span>
                </div>
                <p className="text-[12px] text-fg-3 mb-0.5">{e.desc}</p>
                <code className="text-[10px] text-fg-4 font-mono">
                  {e.usage}
                </code>
              </div>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Rule: never skip elevation levels. Page → Content → Elevated →
          Floating → Modal.
        </p>
      </DemoCard>

      {/* ── Surface Variants (live) ── */}
      <DemoCard label="Surface component — 5 variants (live)">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {VARIANTS.map((v) => (
            <div key={v.variant}>
              <Surface
                variant={v.variant}
                className="h-24 flex items-center justify-center px-4"
              >
                <div className="text-center">
                  <span className="text-[13px] text-fg-2 font-medium block">
                    {v.label}
                  </span>
                  <span className="text-[10px] text-fg-4">{v.desc}</span>
                </div>
              </Surface>
              <div className="mt-1.5 space-y-0.5">
                <code className="text-[10px] text-fg-3 font-mono block">
                  {'<Surface variant="' + v.variant + '">'}
                </code>
                <span className="text-[10px] text-fg-4 block">{v.usage}</span>
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Bar Surface ── */}
      <DemoCard label="Bar surface">
        <div className="max-w-lg mx-auto space-y-4">
          <div>
            <div
              className="rounded-2xl h-14 flex items-center px-5"
              style={{
                background: "var(--card)",
                border: "1px solid var(--bar-border)",
                boxShadow: "var(--bar-shadow)",
              }}
            >
              <span className="text-[15px] text-fg-3">
                What did we agree on for v2?
              </span>
            </div>
            <div className="mt-1.5 space-y-0.5">
              <code className="text-[10px] text-fg-3 font-mono block">
                h-14 · rounded-2xl · bg-card · bar-border · bar-shadow
              </code>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* ── Floating Panel ── */}
      <DemoCard label="Floating panel (dropdown / popover)">
        <div className="flex gap-4 flex-wrap">
          <div
            className="w-64 rounded-2xl"
            style={{
              background: "var(--card)",
              border: "1px solid var(--bar-border)",
              boxShadow: "var(--bar-shadow)",
            }}
          >
            <div className="px-4 py-3">
              <p className="text-[14px] font-medium text-foreground">
                Item one
              </p>
              <p className="text-[12px] text-fg-3 mt-0.5">Description here</p>
            </div>
            <div className="border-t border-border/30" />
            <div className="px-4 py-3 bg-surface-1 rounded-b-2xl">
              <p className="text-[14px] text-fg-2">Item two (hover)</p>
            </div>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Same border/shadow as bar. Separator: border-t border-border/30.
          Hover: bg-surface-1.
        </p>
      </DemoCard>

      {/* ── Border Radius Scale ── */}
      <DemoCard label="Border radius scale">
        <div className="flex items-end gap-4 flex-wrap">
          {RADII.map((r) => (
            <div key={r.name} className="flex flex-col items-center gap-1.5">
              <div
                className={`w-16 h-16 border border-border bg-surface-1 ${r.tailwind}`}
              />
              <span className="text-[11px] text-fg-2 font-medium">
                {r.name}
              </span>
              <code className="text-[10px] text-fg-3 font-mono">{r.value}</code>
              <code className="text-[9px] text-fg-4 font-mono">
                {r.tailwind}
              </code>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Rule: two tiers only — rounded-surface (20px) wraps interactive
          rounded-chrome (14px). Source: packages/ui/src/surface-radius.css.
          Pick by element size — ≤40px is chrome, &gt;40px is surface.
        </p>
      </DemoCard>

      {/* ── Glass Edges ── */}
      <DemoCard label="Glass edges (functional only)">
        <p className="text-[12px] text-fg-2 mb-3">
          Glass communicates layer hierarchy: &ldquo;this floats above
          that.&rdquo; Never decorative.
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {/* Frosted edge */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold mb-2">
              Frosted edge (scroll top/bottom)
            </p>
            <div
              className="relative h-28 rounded-xl overflow-hidden border border-border/40"
              style={{ background: "var(--background)" }}
            >
              <div className="px-4 pt-10 space-y-2">
                {[1, 2, 3, 4].map((i) => (
                  <div
                    key={i}
                    className="h-4 rounded bg-surface-2"
                    style={{ width: `${70 + i * 5}%` }}
                  />
                ))}
              </div>
              <div
                className="absolute top-0 left-0 right-0 h-10 z-10"
                style={{
                  backdropFilter: "blur(20px) saturate(1.4)",
                  WebkitBackdropFilter: "blur(20px) saturate(1.4)",
                  background: "var(--glass-edge, rgba(245,245,245,0.72))",
                  maskImage:
                    "linear-gradient(to bottom, black 35%, transparent)",
                  WebkitMaskImage:
                    "linear-gradient(to bottom, black 35%, transparent)",
                }}
              />
            </div>
            <code className="text-[10px] text-fg-4 font-mono mt-1 block">
              backdrop-blur(20px) saturate(1.4) · mask-image dissolve
            </code>
          </div>
          {/* Bottom dissolve */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold mb-2">
              Dissolve gradient (infinite canvas)
            </p>
            <div
              className="relative h-28 rounded-xl overflow-hidden border border-border/40"
              style={{ background: "var(--card)" }}
            >
              <div className="px-4 py-3 space-y-2">
                {[1, 2, 3, 4].map((i) => (
                  <div
                    key={i}
                    className="h-4 rounded bg-surface-2"
                    style={{ width: `${50 + i * 12}%` }}
                  />
                ))}
              </div>
              <div
                className="absolute bottom-0 left-0 right-0 h-8 pointer-events-none"
                style={{
                  background:
                    "linear-gradient(to top, var(--card), transparent)",
                }}
              />
            </div>
            <code className="text-[10px] text-fg-4 font-mono mt-1 block">
              linear-gradient(to top, var(--card), transparent) · 120px
            </code>
          </div>
        </div>
      </DemoCard>

      {/* ── When to Use ── */}
      <DemoCard label="Decision tree — which Surface?">
        <div className="space-y-2 text-[12px]">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <div>
              <span className="text-fg-2 font-medium">
                Is it a card in a grid?
              </span>
              <span className="text-fg-3"> → default (border + shadow)</span>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <div>
              <span className="text-fg-2 font-medium">
                Is it a detail/section body?
              </span>
              <span className="text-fg-3"> → subtle (border only)</span>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <div>
              <span className="text-fg-2 font-medium">
                Is it mobile or fullscreen?
              </span>
              <span className="text-fg-3">
                {" "}
                → borderless (no border, no rounding)
              </span>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <div>
              <span className="text-fg-2 font-medium">
                Is it a minimal wrapper?
              </span>
              <span className="text-fg-3"> → clean (bg only)</span>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <div>
              <span className="text-fg-2 font-medium">
                Is it floating above content?
              </span>
              <span className="text-fg-3">
                {" "}
                → use bar-border + bar-shadow directly (not Surface)
              </span>
            </div>
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
