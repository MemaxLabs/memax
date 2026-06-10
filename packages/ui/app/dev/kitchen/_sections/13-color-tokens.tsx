// Maps to: globals.css theme tokens, --fg-*, --surface-*, --signature
// Source of truth: :root and .dark blocks in globals.css
// Usage: every color decision in the product references this section
"use client";

import { Section, DemoCard, SIGNATURE } from "../_shared";

/* ── Opacity Architecture ── */

const OPACITY_TEXT = [
  {
    token: "--op-primary",
    value: "0.9",
    class: "text-fg-1",
    label: "Primary",
    desc: "Titles, body, input text",
  },
  {
    token: "--op-secondary",
    value: "0.65",
    class: "text-fg-2",
    label: "Secondary",
    desc: "Descriptions, placeholders, secondary labels",
  },
  {
    token: "--op-tertiary",
    value: "0.4",
    class: "text-fg-3",
    label: "Tertiary",
    desc: "Timestamps, meta, muted labels",
  },
  {
    token: "--op-muted",
    value: "0.2",
    class: "text-fg-4",
    label: "Decorative",
    desc: "Hints, annotations, divider text",
  },
];

const OPACITY_SURFACE = [
  {
    token: "--op-bg-subtle",
    value: "0.03",
    class: "bg-surface-1",
    label: "Subtle",
    desc: "Slight tint, chip background, hover hint",
  },
  {
    token: "--op-bg-hover",
    value: "0.05",
    class: "bg-surface-2",
    label: "Hover",
    desc: "Hover state, selected row, active tab",
  },
  {
    token: "--op-bg-active",
    value: "0.08",
    class: "bg-surface-3",
    label: "Active",
    desc: "Active/pressed state, strong selection",
  },
];

/* ── Theme Primitives ── */

const PRIMITIVES = [
  {
    token: "--background",
    light: "#fafafa",
    dark: "oklch(0.155 0.01 260)",
    desc: "Page background",
  },
  {
    token: "--foreground",
    light: "#1c1c1e",
    dark: "oklch(0.985 0 0)",
    desc: "Primary text base",
  },
  {
    token: "--card",
    light: "#ffffff",
    dark: "oklch(0.2 0.008 260)",
    desc: "Card, bar, modal surfaces",
  },
  {
    token: "--border",
    light: "#e5e5e5",
    dark: "oklch(0.275 0 0)",
    desc: "Card borders, separators",
  },
  {
    token: "--muted",
    light: "#f2f2f2",
    dark: "oklch(0.269 0 0)",
    desc: "Summary callouts, subtle bg",
  },
  {
    token: "--destructive",
    light: "#ff3b30",
    dark: "oklch(0.704 0.191 22)",
    desc: "Error, delete states",
  },
  {
    token: "--primary",
    light: "#000000",
    dark: "oklch(0.922 0 0)",
    desc: "Primary button, CTA fill",
  },
  {
    token: "--secondary",
    light: "#e8e8e8",
    dark: "oklch(0.269 0 0)",
    desc: "Secondary button, modal header",
  },
];

/* ── Signature ── */

const SIGNATURE_RULES = [
  "✦ star indicator (AI intelligence marker)",
  "Loading pulse dots (MemaxLoader)",
  "Active nav dot in tree panel",
  "Toggle switch on-state fill",
  "Drag-drop border hint",
  "Recall mode send button fill",
];

export function ColorTokensSection() {
  return (
    <Section
      title="13. Color Tokens"
      description="Semantic color system built on oklch opacity architecture. All derived from --foreground, adapts to any theme or dark mode automatically."
    >
      {/* ── Opacity Architecture ── */}
      <DemoCard label="Architecture — how colors are computed">
        <div className="bg-surface-1 rounded-lg p-3 mb-3">
          <code className="text-[11px] text-fg-2 font-mono leading-relaxed block">
            --fg-1: oklch(from var(--foreground) l c h / var(--op-primary));
            <br />
            --surface-1: oklch(from var(--foreground) l c h /
            var(--op-bg-subtle));
          </code>
        </div>
        <p className="text-[11px] text-fg-3">
          All semantic colors are computed from{" "}
          <code className="font-mono text-fg-2">--foreground</code> with opacity
          multipliers. Change one variable, entire palette adapts. No hardcoded
          color values in components.
        </p>
      </DemoCard>

      {/* ── Foreground Scale ── */}
      <DemoCard label="Foreground scale — text colors">
        <div className="space-y-0">
          {OPACITY_TEXT.map((t) => (
            <div
              key={t.token}
              className="flex items-center gap-3 py-2.5 border-b border-border/10 last:border-0"
            >
              {/* Live swatch */}
              <div className="w-8 h-8 rounded-lg border border-border/30 flex items-center justify-center shrink-0">
                <span className={`text-[16px] font-bold ${t.class}`}>Aa</span>
              </div>
              {/* Details */}
              <div className="flex-1 min-w-0">
                <div className="flex items-baseline gap-2">
                  <span className={`text-[14px] font-medium ${t.class}`}>
                    {t.label}
                  </span>
                  <code className="text-[10px] text-fg-4 font-mono">
                    {t.class}
                  </code>
                </div>
                <p className="text-[11px] text-fg-4">{t.desc}</p>
              </div>
              {/* Opacity value */}
              <code className="text-[11px] font-mono text-fg-3 shrink-0">
                /{(parseFloat(t.value) * 100).toFixed(0)}
              </code>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Surface Scale ── */}
      <DemoCard label="Surface scale — background fills">
        <div className="space-y-3">
          {OPACITY_SURFACE.map((s) => (
            <div key={s.token} className="flex items-center gap-3">
              {/* Live swatch */}
              <div
                className={`w-16 h-10 rounded-lg border border-border/20 ${s.class}`}
              />
              {/* Details */}
              <div className="flex-1 min-w-0">
                <div className="flex items-baseline gap-2">
                  <span className="text-[13px] font-medium text-fg-2">
                    {s.label}
                  </span>
                  <code className="text-[10px] text-fg-4 font-mono">
                    {s.class}
                  </code>
                </div>
                <p className="text-[11px] text-fg-4">{s.desc}</p>
              </div>
              <code className="text-[11px] font-mono text-fg-3 shrink-0">
                /{(parseFloat(s.value) * 100).toFixed(0)}
              </code>
            </div>
          ))}
        </div>
        {/* Stacking demo */}
        <div className="mt-4 pt-3 border-t border-border/20">
          <p className="text-[10px] text-fg-4 uppercase tracking-wider font-semibold mb-2">
            Stacking demo
          </p>
          <div
            className="rounded-xl p-4"
            style={{ background: "var(--background)" }}
          >
            <div className="bg-surface-1 rounded-lg p-3">
              <span className="text-[12px] text-fg-3 block mb-2">
                surface-1 (subtle tint)
              </span>
              <div className="bg-surface-2 rounded-md p-3">
                <span className="text-[12px] text-fg-3 block mb-2">
                  surface-2 (hover)
                </span>
                <div className="bg-surface-3 rounded-md p-3">
                  <span className="text-[12px] text-fg-2">
                    surface-3 (active)
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* ── Theme Primitives ── */}
      <DemoCard label="Theme primitives — light / dark values">
        <div className="space-y-0.5">
          {/* Header */}
          <div className="grid grid-cols-[120px_1fr_1fr_1fr] gap-2 pb-2 border-b border-border/30 mb-1">
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Token
            </span>
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Light
            </span>
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Dark
            </span>
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Usage
            </span>
          </div>
          {PRIMITIVES.map((p) => (
            <div
              key={p.token}
              className="grid grid-cols-[120px_1fr_1fr_1fr] gap-2 py-1.5 border-b border-border/10 items-center"
            >
              <code className="text-[11px] text-fg-2 font-mono">{p.token}</code>
              <div className="flex items-center gap-2">
                <div
                  className="w-4 h-4 rounded border border-border/40 shrink-0"
                  style={{ background: p.light }}
                />
                <code className="text-[10px] text-fg-3 font-mono truncate">
                  {p.light}
                </code>
              </div>
              <code className="text-[10px] text-fg-3 font-mono truncate">
                {p.dark}
              </code>
              <span className="text-[10px] text-fg-4">{p.desc}</span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Live Theme Swatches ── */}
      <DemoCard label="Live swatches (adapts to current theme)">
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          {[
            { var: "--background", label: "Background" },
            { var: "--card", label: "Card" },
            { var: "--foreground", label: "Foreground" },
            { var: "--border", label: "Border" },
            { var: "--muted", label: "Muted" },
            { var: "--destructive", label: "Destructive" },
            { var: "--primary", label: "Primary" },
            { var: "--secondary", label: "Secondary" },
          ].map((s) => (
            <div key={s.var} className="flex items-center gap-2.5">
              <div
                className="w-8 h-8 rounded-lg border border-border/40 shrink-0"
                style={{ background: `var(${s.var})` }}
              />
              <div>
                <p className="text-[12px] text-fg-2 font-medium">{s.label}</p>
                <code className="text-[9px] text-fg-4 font-mono">{s.var}</code>
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Signature Color ── */}
      <DemoCard label="Signature color — Dream Violet">
        <div className="flex items-start gap-4 mb-3">
          <div
            className="w-16 h-16 rounded-xl shrink-0"
            style={{ background: SIGNATURE }}
          />
          <div className="flex-1">
            <div className="flex items-baseline gap-2 mb-1">
              <span className="text-[14px] font-semibold text-fg-1">
                Dream Violet
              </span>
              <code className="text-[11px] font-mono text-fg-3">
                oklch(0.62 0.16 290)
              </code>
            </div>
            <p className="text-[12px] text-fg-3">
              The memax intelligence marker. Appears only where memax is
              actively working — never decorative, never structural.
            </p>
          </div>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <div className="bg-surface-1 rounded-lg p-2.5">
            <p className="text-[10px] text-fg-4 uppercase tracking-wider font-semibold mb-1.5">
              Allowed usage
            </p>
            <div className="space-y-1">
              {SIGNATURE_RULES.map((r) => (
                <div key={r} className="flex items-center gap-2">
                  <span
                    className="text-[10px] shrink-0"
                    style={{ color: SIGNATURE }}
                  >
                    ✦
                  </span>
                  <span className="text-[11px] text-fg-2">{r}</span>
                </div>
              ))}
            </div>
          </div>
          <div className="bg-surface-1 rounded-lg p-2.5">
            <p className="text-[10px] text-fg-4 uppercase tracking-wider font-semibold mb-1.5">
              Never use for
            </p>
            <div className="space-y-1 text-[11px] text-fg-3">
              <p>Decorative borders or backgrounds</p>
              <p>Regular button fills or highlights</p>
              <p>Text color for non-AI content</p>
              <p>Branding outside the app</p>
            </div>
          </div>
        </div>
        <div className="flex items-center gap-3 mt-3 pt-3 border-t border-border/20">
          <div
            className="w-8 h-8 rounded-lg shrink-0"
            style={{ background: "var(--signature-muted)" }}
          />
          <div>
            <code className="text-[11px] font-mono text-fg-3">
              --signature-muted
            </code>
            <p className="text-[10px] text-fg-4">
              oklch(signature / 0.12) — subtle tint for backgrounds
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── Bar-Specific Tokens ── */}
      <DemoCard label="Bar-specific tokens">
        <div className="space-y-2">
          {[
            {
              var: "--bar-border",
              desc: "Bar border color (12% opacity)",
              class: "border",
            },
            {
              var: "--bar-shadow",
              desc: "Bar shadow (layered: outline + medium + large)",
              class: "shadow",
            },
            {
              var: "--bar-bg",
              desc: "Bar background (falls back to --card)",
              class: "bg",
            },
          ].map((t) => (
            <div key={t.var} className="flex items-center gap-3 py-1.5">
              <code className="text-[11px] text-fg-2 font-mono w-28 shrink-0">
                {t.var}
              </code>
              <span className="text-[11px] text-fg-3">{t.desc}</span>
            </div>
          ))}
        </div>
      </DemoCard>
    </Section>
  );
}
