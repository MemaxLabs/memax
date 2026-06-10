// Accessibility — WCAG contrast ratios, focus management, motion, touch targets.
// Centralized a11y specification. Every contrast ratio is calculated from real token values.
//
// FOR LLM AGENTS: This section contains hard rules. When the contrast table says
// "FAIL for body text", that means DO NOT use that token for body text. No exceptions.
"use client";

import { Section, DemoCard, SIGNATURE } from "../_shared";

/* ── Contrast Ratio Data (calculated from real token values) ── */

interface ContrastRow {
  token: string;
  opacity: string;
  lightBg: string;
  lightCard: string;
  darkBg: string;
  aaNormal: "PASS" | "FAIL";
  aaLarge: "PASS" | "FAIL";
  rule: string;
}

// Calculated via WCAG 2.1 luminance formula against actual hex values:
// Light: bg=#fafafa, card=#ffffff, fg=#1c1c1e
// Dark: bg≈#1f1f2e (oklch 0.155 0.01 260), fg≈#fbfbfb (oklch 0.985 0 0)
const CONTRAST_TABLE: ContrastRow[] = [
  {
    token: "fg-1",
    opacity: "/90",
    lightBg: "12.2:1",
    lightCard: "12.7:1",
    darkBg: "12.9:1",
    aaNormal: "PASS",
    aaLarge: "PASS",
    rule: "Use for all body text, titles, headings, input values. No restrictions.",
  },
  {
    token: "fg-2",
    opacity: "/65",
    lightBg: "5.2:1",
    lightCard: "5.3:1",
    darkBg: "7.3:1",
    aaNormal: "PASS",
    aaLarge: "PASS",
    rule: "Safe for descriptions, placeholders, secondary labels. Passes AA at all sizes.",
  },
  {
    token: "fg-3",
    opacity: "/40",
    lightBg: "2.5:1",
    lightCard: "2.5:1",
    darkBg: "3.7:1",
    aaNormal: "FAIL",
    aaLarge: "FAIL",
    rule: "DECORATIVE ONLY in light mode. Timestamps, meta, kind labels. Never for readable body text. Dark mode passes AA large (3.7:1).",
  },
  {
    token: "fg-4",
    opacity: "/20",
    lightBg: "1.5:1",
    lightCard: "1.5:1",
    darkBg: "1.9:1",
    aaNormal: "FAIL",
    aaLarge: "FAIL",
    rule: "NON-TEXT ONLY. Hints, annotations, divider text, decorative labels. Never for any content a user needs to read.",
  },
];

/* ── Focus Ring Spec ── */

const FOCUS_SPEC = [
  {
    element: "Button (all variants)",
    ring: "focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:border-ring",
    notes: "3px ring at 50% opacity of --ring. Border also changes to --ring.",
  },
  {
    element: "Button (destructive)",
    ring: "focus-visible:border-destructive/40 focus-visible:ring-destructive/20",
    notes: "Red-tinted ring for destructive actions.",
  },
  {
    element: "Input fields",
    ring: "focus:border-ring focus:ring-2 focus:ring-ring/50",
    notes: "2px ring (smaller than button). Border transitions to --ring.",
  },
  {
    element: "Toggle switch",
    ring: "Browser default (no custom ring)",
    notes: "Relies on native focus indicator. TODO: add visible ring.",
  },
  {
    element: "Links / text buttons",
    ring: "focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:rounded",
    notes: "Subtle 2px ring with rounded corners.",
  },
];

/* ── Touch Target Rules ── */

const TOUCH_RULES = [
  {
    min: "44×44px",
    usage:
      "Primary actions: send button, mode pills, nav items, toggle switches",
  },
  {
    min: "36×36px",
    usage: "Secondary actions: icon buttons (icon-lg), close buttons",
  },
  {
    min: "32×32px",
    usage: "Tertiary: icon buttons (default), copy buttons, tag remove",
  },
  {
    min: "24×24px",
    usage:
      "Minimum: icon-xs buttons. Only for non-critical, supplementary actions",
  },
];

/* ── Color Blindness ── */

const COLOR_BLIND_RULES = [
  "Kind dots use 13 distinct hues — not distinguishable by color alone",
  "Every dot is accompanied by a text label (kind name) — color is supplementary",
  "Red (destructive) is always paired with text or icon (Trash2), never color-only",
  "Signature (violet) vs error (red) — sufficiently different hue for deuteranopia",
  "Never convey meaning through color alone — always pair with text, icon, or shape",
];

export function AccessibilitySection() {
  return (
    <Section
      title="27. Accessibility"
      description="WCAG contrast ratios (calculated from real tokens), focus management, reduced motion, touch targets, color blindness. Hard rules, not guidelines."
    >
      {/* ── Contrast Ratio Table ── */}
      <DemoCard label="Contrast ratios — WCAG 2.1 (calculated)">
        <div className="space-y-0.5">
          {/* Header */}
          <div className="grid grid-cols-[60px_40px_65px_65px_65px_50px_50px_1fr] gap-1.5 pb-2 border-b border-border/30 mb-1">
            <span className="text-[9px] text-fg-4 font-mono uppercase">
              Token
            </span>
            <span className="text-[9px] text-fg-4 font-mono uppercase">Op</span>
            <span className="text-[9px] text-fg-4 font-mono uppercase">
              Light/bg
            </span>
            <span className="text-[9px] text-fg-4 font-mono uppercase">
              Light/card
            </span>
            <span className="text-[9px] text-fg-4 font-mono uppercase">
              Dark/bg
            </span>
            <span className="text-[9px] text-fg-4 font-mono uppercase">AA</span>
            <span className="text-[9px] text-fg-4 font-mono uppercase">
              AA Lg
            </span>
            <span className="text-[9px] text-fg-4 font-mono uppercase">
              Usage rule
            </span>
          </div>
          {CONTRAST_TABLE.map((r) => (
            <div
              key={r.token}
              className="grid grid-cols-[60px_40px_65px_65px_65px_50px_50px_1fr] gap-1.5 py-2 border-b border-border/10 items-baseline"
            >
              <div className="flex items-center gap-1">
                <div
                  className={`w-3 h-3 rounded-sm bg-foreground`}
                  style={{ opacity: parseFloat(r.opacity.slice(1)) / 100 }}
                />
                <code className="text-[10px] font-mono text-fg-2">
                  {r.token}
                </code>
              </div>
              <code className="text-[10px] font-mono text-fg-3">
                {r.opacity}
              </code>
              <code className="text-[10px] font-mono text-fg-3">
                {r.lightBg}
              </code>
              <code className="text-[10px] font-mono text-fg-3">
                {r.lightCard}
              </code>
              <code className="text-[10px] font-mono text-fg-3">
                {r.darkBg}
              </code>
              <span
                className={`text-[10px] font-mono font-medium ${r.aaNormal === "PASS" ? "text-fg-2" : "text-destructive"}`}
              >
                {r.aaNormal}
              </span>
              <span
                className={`text-[10px] font-mono font-medium ${r.aaLarge === "PASS" ? "text-fg-2" : "text-destructive"}`}
              >
                {r.aaLarge}
              </span>
              <span className="text-[10px] text-fg-3">{r.rule}</span>
            </div>
          ))}
        </div>
        <div className="bg-surface-1 rounded-lg p-2.5 mt-3 space-y-1">
          <p className="text-[11px] text-fg-2 font-medium">
            WCAG 2.1 AA requirements:
          </p>
          <p className="text-[10px] text-fg-3">
            Normal text (&lt;18px): minimum 4.5:1 contrast ratio
          </p>
          <p className="text-[10px] text-fg-3">
            Large text (≥18px bold or ≥24px): minimum 3.0:1 contrast ratio
          </p>
          <p className="text-[10px] text-fg-3">
            Non-text (icons, borders): minimum 3.0:1 contrast ratio
          </p>
        </div>
      </DemoCard>

      {/* ── Signature Color Contrast ── */}
      <DemoCard label="Signature color (Dream Violet) — contrast">
        <div className="flex items-start gap-4">
          <div
            className="w-12 h-12 rounded-lg shrink-0"
            style={{ background: SIGNATURE }}
          />
          <div className="flex-1 space-y-1.5">
            <div className="flex items-center gap-3 text-[11px]">
              <span className="text-fg-3 w-20">Light bg:</span>
              <code className="font-mono text-fg-2">4.8:1</code>
              <span className="text-fg-2 font-medium">AA large PASS</span>
            </div>
            <div className="flex items-center gap-3 text-[11px]">
              <span className="text-fg-3 w-20">Dark bg:</span>
              <code className="font-mono text-fg-2">3.3:1</code>
              <span className="text-fg-2 font-medium">AA large PASS</span>
            </div>
            <p className="text-[10px] text-fg-4 pt-1">
              Signature passes AA large text (3:1) in both modes. Safe for ✦
              indicators (typically 12-20px) and button fills (white text on
              signature bg). Not safe for small body text in light mode.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── Practical Rules ── */}
      <DemoCard label="Token → text size rules (LLM reference)">
        <div className="space-y-2 text-[12px]">
          <div className="flex items-start gap-2">
            <code className="font-mono text-fg-2 w-12 shrink-0">fg-1</code>
            <p className="text-fg-2">
              Any size, any weight. No restrictions.{" "}
              <span className="text-fg-4">(12.2:1+)</span>
            </p>
          </div>
          <div className="flex items-start gap-2">
            <code className="font-mono text-fg-2 w-12 shrink-0">fg-2</code>
            <p className="text-fg-2">
              Any size, any weight. Safe everywhere.{" "}
              <span className="text-fg-4">(5.2:1+)</span>
            </p>
          </div>
          <div className="flex items-start gap-2">
            <code className="font-mono text-fg-2 w-12 shrink-0">fg-3</code>
            <p className="text-fg-3">
              Minimum 12px. Use for timestamps, meta, labels — never for content
              users must read.{" "}
              <span className="text-fg-4">(2.5:1 light, 3.7:1 dark)</span>
            </p>
          </div>
          <div className="flex items-start gap-2">
            <code className="font-mono text-fg-2 w-12 shrink-0">fg-4</code>
            <p className="text-fg-4">
              Decorative only. Annotations, DemoCard labels, divider dots. Never
              for readable text. <span className="text-fg-4">(1.5:1+)</span>
            </p>
          </div>
        </div>
        <div className="bg-surface-1 rounded-lg p-2.5 mt-3">
          <p className="text-[11px] text-fg-3">
            <span className="font-medium text-fg-2">Known trade-off:</span> fg-3
            fails WCAG AA for normal text in light mode. This is intentional —
            fg-3 is for supplementary metadata (timestamps, kind labels) where
            the information is also conveyed by position, grouping, or icon. If
            the text must be readable standalone, use fg-2.
          </p>
        </div>
      </DemoCard>

      {/* ── Focus Ring Specification ── */}
      <DemoCard label="Focus ring specification">
        <div className="space-y-0">
          {FOCUS_SPEC.map((f) => (
            <div
              key={f.element}
              className="flex items-start gap-3 py-2.5 border-b border-border/10 last:border-0"
            >
              <span className="text-[12px] font-medium text-fg-2 w-36 shrink-0">
                {f.element}
              </span>
              <div className="flex-1 min-w-0">
                <code className="text-[10px] font-mono text-fg-3 block mb-0.5">
                  {f.ring}
                </code>
                <span className="text-[10px] text-fg-4">{f.notes}</span>
              </div>
            </div>
          ))}
        </div>
        <div className="mt-3 pt-3 border-t border-border/20 space-y-1">
          <p className="text-[11px] text-fg-2 font-medium">Focus rules:</p>
          <p className="text-[10px] text-fg-3">
            1. Always use <code className="font-mono">focus-visible</code>,
            never <code className="font-mono">focus</code> — keyboard users
            only, not mouse clicks
          </p>
          <p className="text-[10px] text-fg-3">
            2. Ring color is <code className="font-mono">--ring</code> at 50%
            opacity — visible but not aggressive
          </p>
          <p className="text-[10px] text-fg-3">
            3. Tab order follows visual layout. No custom tabIndex unless
            absolutely necessary
          </p>
          <p className="text-[10px] text-fg-3">
            4. All interactive elements must be reachable via Tab key
          </p>
        </div>
      </DemoCard>

      {/* ── Live Focus Demo ── */}
      <DemoCard label="Live — focus ring demo (press Tab)">
        <div className="flex flex-wrap items-center gap-3">
          <button className="h-8 px-3 rounded-lg text-[13px] bg-primary text-primary-foreground font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:border-ring">
            Default
          </button>
          <button className="h-8 px-3 rounded-lg text-[13px] border border-border bg-background font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:border-ring">
            Outline
          </button>
          <button className="h-8 px-3 rounded-lg text-[13px] bg-destructive/10 text-destructive font-medium outline-none focus-visible:ring-3 focus-visible:ring-destructive/20 focus-visible:border-destructive/40">
            Destructive
          </button>
          <input
            type="text"
            placeholder="Input field"
            className="h-8 px-3 rounded-lg text-[13px] border border-border bg-background outline-none focus:ring-2 focus:ring-ring/50 focus:border-ring w-32"
          />
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Tab through to see focus rings. Ring is always 3px (buttons) or 2px
          (inputs) with --ring at 50%.
        </p>
      </DemoCard>

      {/* ── Reduced Motion ── */}
      <DemoCard label="Reduced motion — prefers-reduced-motion">
        <div className="space-y-2 text-[12px]">
          <p className="text-fg-2">
            When{" "}
            <code className="font-mono text-fg-3">
              prefers-reduced-motion: reduce
            </code>{" "}
            is set, these animations are disabled:
          </p>
          <div className="bg-surface-1 rounded-lg p-3 font-mono text-[11px] text-fg-3 space-y-0.5">
            <p>animate-content-ready → instant opacity (no transform)</p>
            <p>animate-fade-up → removed</p>
            <p>state-fast-pulse → static (no pulse)</p>
            <p>state-slow-breathe → static (no breathe)</p>
          </div>
          <p className="text-fg-3 text-[11px]">
            Framer Motion respects{" "}
            <code className="font-mono">useReducedMotion()</code> automatically.
            CSS-only animations use the media query in globals.css (line 1016).
          </p>
        </div>
        <div className="mt-3 pt-3 border-t border-border/20 space-y-1">
          <p className="text-[11px] text-fg-2 font-medium">What stays:</p>
          <p className="text-[10px] text-fg-3">
            Color transitions (opacity, background) — these are non-motion
          </p>
          <p className="text-[10px] text-fg-3">
            MemaxLoader dots — reduced to static display (no sequential pulse)
          </p>
          <p className="text-[10px] text-fg-3">
            Placeholder text crossfade — kept but instant (no blur transition)
          </p>
        </div>
      </DemoCard>

      {/* ── Touch Targets ── */}
      <DemoCard label="Touch targets — minimum sizes">
        <div className="space-y-0">
          {TOUCH_RULES.map((t) => (
            <div
              key={t.min}
              className="flex items-center gap-3 py-2 border-b border-border/10 last:border-0"
            >
              <div
                className="border border-border/40 rounded flex items-center justify-center shrink-0"
                style={{
                  width: parseInt(t.min),
                  height: parseInt(t.min),
                  minWidth: parseInt(t.min),
                }}
              >
                <span className="text-[9px] text-fg-4 font-mono">
                  {t.min.split("×")[0]}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <code className="text-[11px] font-mono text-fg-2">{t.min}</code>
                <p className="text-[10px] text-fg-4">{t.usage}</p>
              </div>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          WCAG 2.5.8: minimum 24×24px target. Apple HIG: 44×44pt for primary
          actions. Memax follows Apple for primary, WCAG for minimum.
        </p>

        {/* ── Implementation patterns ── */}
        <div className="mt-4 p-3 rounded-lg bg-surface-1 text-[11px] text-fg-3 font-mono space-y-1.5">
          <p className="font-semibold text-fg-2">
            Touch target patterns (pick one per element)
          </p>
          <p>
            A. <span className="text-fg-2">min-h-11</span> — element IS 44px.
            Use for list items, picker rows, toolbar buttons.
          </p>
          <p>
            B.{" "}
            <span className="text-fg-2">
              relative + after:absolute after:-inset-1.5
              after:content-[&apos;&apos;]
            </span>{" "}
            — element visually compact, invisible pseudo extends touch area. Use
            for floating pills, compact toggles, icon buttons where visual 44px
            is too large.
          </p>
          <p>
            C. <span className="text-fg-2">p-3</span> (12px all sides) — enough
            padding that content + padding ≥ 44px. Simple, no pseudo needed.
          </p>
          <p className="text-fg-4 mt-1">
            Rule: choose the pattern that keeps visual density while hitting
            44px tappable. Never mix — pick one per component.
          </p>
        </div>
      </DemoCard>

      {/* ── Color Blindness ── */}
      <DemoCard label="Color blindness — rules">
        <div className="space-y-1.5">
          {COLOR_BLIND_RULES.map((r, i) => (
            <div key={i} className="flex items-start gap-2 text-[11px]">
              <span className="text-fg-3 shrink-0 w-4">{i + 1}.</span>
              <p className="text-fg-2">{r}</p>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Keyboard Navigation ── */}
      <DemoCard label="Keyboard navigation map">
        <div className="space-y-0">
          {[
            { key: "Tab", action: "Move focus to next interactive element" },
            { key: "Shift+Tab", action: "Move focus to previous element" },
            {
              key: "Enter",
              action: "Activate button, submit form, open memory",
            },
            {
              key: "Escape",
              action: "Close modal, dismiss notification, clear bar",
            },
            { key: "↑ / ↓", action: "Navigate memory rows, dropdown items" },
            { key: "/", action: "Focus the bar (brain view)" },
          ].map((k) => (
            <div
              key={k.key}
              className="flex items-center gap-3 py-1.5 border-b border-border/10 last:border-0"
            >
              <kbd className="text-[11px] font-mono text-fg-2 bg-surface-2 px-2 py-0.5 rounded min-w-16 text-center shrink-0">
                {k.key}
              </kbd>
              <span className="text-[11px] text-fg-3">{k.action}</span>
            </div>
          ))}
        </div>
      </DemoCard>
    </Section>
  );
}
