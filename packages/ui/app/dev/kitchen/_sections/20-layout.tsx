// Maps to: all route layouts, bar positioning, spacing grid, responsive breakpoints
// Source of truth for page widths, z-index hierarchy, and spacing decisions
"use client";

import { Section, DemoCard } from "../_shared";

/* ── Page Widths ── */

const PAGES = [
  {
    name: "Bar",
    width: "max-w-[640px]",
    responsive:
      "w-[calc(100%-32px)] sm:w-[calc(100%-96px)] md:w-[calc(100%-128px)]",
    desc: "Global input bar. Centered, responsive width shrinks on larger screens.",
  },
  {
    name: "Content (brain view)",
    width: "max-w-4xl (896px)",
    responsive: "px-4 sm:px-8",
    desc: "Memory grid, topic detail. Outer padding + inner max-width.",
  },
  {
    name: "Memory detail",
    width: "max-w-4xl (896px)",
    responsive: "px-5 sm:px-8 md:px-10",
    desc: "Same max-w as list — container morphs, no new surface.",
  },
  {
    name: "Settings modal",
    width: "880px",
    responsive: "sidebar 220px + content px-8",
    desc: "Centered dialog. Sidebar nav (desktop), tabs (mobile).",
  },
  {
    name: "Share target",
    width: "max-w-sm (384px)",
    responsive: "p-5",
    desc: "Single card, centered. PWA share sheet capture.",
  },
];

/* ── Spacing Scale ── */

const SPACING = [
  { px: 4, tw: "1", usage: "Micro gaps (icon-to-icon, badge internal)" },
  { px: 8, tw: "2", usage: "Inline gaps (icon + text, dots)" },
  { px: 12, tw: "3", usage: "Tight element spacing (badge group)" },
  { px: 16, tw: "4", usage: "Card padding, grid gap, section gaps" },
  { px: 24, tw: "6", usage: "Section spacing within a page" },
  { px: 32, tw: "8", usage: "Section margin, page top padding" },
  { px: 48, tw: "12", usage: "Large section breaks, page bottom" },
  { px: 64, tw: "16", usage: "Page-level spacing (rarely used)" },
];

/* ── Breakpoints ── */

const BREAKPOINTS = [
  {
    name: "Mobile",
    prefix: "(default)",
    min: "0px",
    max: "639px",
    notes: "Single column, pill chips, bottom bar, borderless surfaces",
  },
  {
    name: "Tablet",
    prefix: "sm:",
    min: "640px",
    max: "767px",
    notes: "2-column grid, text mode toggle, larger type",
  },
  {
    name: "Desktop",
    prefix: "md:",
    min: "768px",
    max: "1023px",
    notes: "3+ column grid, sidebar peek, full bar width",
  },
  {
    name: "Wide",
    prefix: "lg:",
    min: "1024px",
    max: "—",
    notes: "Pinned sidebar, max content width, settings modal",
  },
];

/* ── Z-Index ── */

const Z_LAYERS = [
  { z: "z-page", value: "0", usage: "Regular page content" },
  {
    z: "z-bar-notif",
    value: "30",
    usage: "Bar notification + memory sticky header",
  },
  {
    z: "z-bar",
    value: "40",
    usage: "Command bar, mobile dock, top-right chrome row",
  },
  {
    z: "z-topic-tree",
    value: "50",
    usage: "Topic Explorer floating panel + mobile fullscreen",
  },
  {
    z: "z-modal",
    value: "60",
    usage: "Settings panel/dialog, memory modal, admin drawer",
  },
  {
    z: "z-popover",
    value: "70",
    usage:
      "ALL popovers/dropdowns (above modal so popovers inside dialogs work)",
  },
  {
    z: "z-takeover",
    value: "80",
    usage: "Lightbox, hub-create, mobile compose, surface transition",
  },
  { z: "z-toast", value: "90", usage: "Impersonation bar, future toasts" },
];

/* ── Safe Areas ── */

const SAFE_AREAS = [
  { token: "--safe-top", usage: "Status bar inset (iOS notch)" },
  { token: "--safe-bottom", usage: "Home indicator (iPhone), bar clearance" },
  { token: "--safe-left", usage: "Landscape notch (left)" },
  { token: "--safe-right", usage: "Landscape notch (right)" },
];

export function LayoutSection() {
  return (
    <Section
      title="20. Layout"
      description="Page widths, 8px spacing grid, responsive breakpoints, z-index hierarchy, and safe area insets. Every layout decision references this."
    >
      {/* ── Page Widths ── */}
      <DemoCard label="Page widths">
        <div className="space-y-3">
          {PAGES.map((p) => (
            <div key={p.name} className="flex items-start gap-3">
              <span className="text-[13px] font-medium text-fg-2 w-28 shrink-0">
                {p.name}
              </span>
              <div className="flex-1 min-w-0">
                <code className="text-[11px] text-fg-2 font-mono">
                  {p.width}
                </code>
                <p className="text-[10px] text-fg-3 mt-0.5">{p.desc}</p>
                <code className="text-[9px] text-fg-4 font-mono">
                  {p.responsive}
                </code>
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Width Comparison (visual) ── */}
      <DemoCard label="Width comparison (visual)">
        <div className="space-y-2.5">
          {[
            { label: "Share (384px)", w: "max-w-sm", pct: "43%" },
            { label: "Bar (640px)", w: "max-w-[640px]", pct: "71%" },
            { label: "Settings (880px)", w: "max-w-[880px]", pct: "98%" },
            { label: "Content (896px)", w: "max-w-4xl", pct: "100%" },
          ].map((item) => (
            <div key={item.label}>
              <div className="flex items-center gap-2 mb-0.5">
                <span className="text-[10px] text-fg-3">{item.label}</span>
                <span className="text-[9px] text-fg-4 font-mono">
                  {item.pct}
                </span>
              </div>
              <div
                className="h-5 rounded-md bg-surface-2 border border-border/30"
                style={{ width: item.pct }}
              />
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Spacing Scale ── */}
      <DemoCard label="Spacing grid (4px base, 8px primary)">
        <div className="flex items-end gap-2 flex-wrap">
          {SPACING.map((s) => (
            <div key={s.px} className="flex flex-col items-center gap-1">
              <div
                className="w-8 rounded-sm bg-surface-3 border border-border/20"
                style={{ height: s.px }}
              />
              <span className="text-[11px] text-fg-2 font-mono font-medium">
                {s.px}
              </span>
              <span className="text-[9px] text-fg-4 font-mono">gap-{s.tw}</span>
            </div>
          ))}
        </div>
        <div className="mt-3 pt-3 border-t border-border/20 space-y-0.5">
          {SPACING.map((s) => (
            <div
              key={s.px + "-row"}
              className="flex items-center gap-3 text-[10px]"
            >
              <code className="font-mono text-fg-3 w-10 text-right">
                {s.px}px
              </code>
              <code className="font-mono text-fg-4 w-10">gap-{s.tw}</code>
              <span className="text-fg-4">{s.usage}</span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Breakpoints ── */}
      <DemoCard label="Responsive breakpoints">
        <div className="space-y-0">
          {BREAKPOINTS.map((b) => (
            <div
              key={b.name}
              className="flex items-start gap-3 py-2.5 border-b border-border/10 last:border-0"
            >
              <div className="w-20 shrink-0">
                <span className="text-[13px] font-medium text-fg-2">
                  {b.name}
                </span>
                <code className="text-[10px] font-mono text-fg-3 block">
                  {b.prefix}
                </code>
              </div>
              <div className="flex-1 min-w-0">
                <code className="text-[10px] font-mono text-fg-3">
                  {b.min} — {b.max}
                </code>
                <p className="text-[11px] text-fg-4 mt-0.5">{b.notes}</p>
              </div>
            </div>
          ))}
        </div>
        <div className="bg-surface-1 rounded-lg p-2.5 mt-3">
          <p className="text-[11px] text-fg-3">
            <span className="font-medium text-fg-2">Rule:</span> mobile-first.
            Default styles = mobile. Add <code className="font-mono">sm:</code>{" "}
            / <code className="font-mono">md:</code> /{" "}
            <code className="font-mono">lg:</code> for larger screens. Never
            hide critical functionality behind a breakpoint.
          </p>
        </div>
      </DemoCard>

      {/* ── Z-Index Hierarchy ── */}
      <DemoCard label="Z-index hierarchy">
        <div className="space-y-1">
          {[...Z_LAYERS].reverse().map((layer) => (
            <div key={layer.z} className="flex items-center gap-3">
              <code className="text-[11px] font-mono text-fg-2 w-12 shrink-0 text-right">
                {layer.z}
              </code>
              <div
                className="flex-1 h-7 rounded-md flex items-center px-3 border border-border/20"
                style={{
                  background: `oklch(from var(--foreground) l c h / ${0.02 + parseInt(layer.value) * 0.002})`,
                }}
              >
                <span className="text-[11px] text-fg-2">{layer.usage}</span>
              </div>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Rule: semantic tokens only. NEVER use hardcoded{" "}
          <code className="font-mono">z-50</code>,{" "}
          <code className="font-mono">z-[65]</code>, etc. Pick from this list.
          Invariants: popover &gt; modal (popovers must work inside a Settings
          dialog) and takeover &gt; popover (full-screen surfaces cover stray
          popovers). Tokens defined as CSS vars{" "}
          <code className="font-mono">--z-*</code> on{" "}
          <code className="font-mono">:root</code> plus{" "}
          <code className="font-mono">@utility</code> blocks in globals.css.
        </p>
      </DemoCard>

      {/* ── Safe Area Insets ── */}
      <DemoCard label="Safe area insets (CSS env())">
        <div className="space-y-1.5">
          {SAFE_AREAS.map((sa) => (
            <div key={sa.token} className="flex items-center gap-3">
              <code className="text-[11px] font-mono text-fg-2 w-28 shrink-0">
                {sa.token}
              </code>
              <span className="text-[11px] text-fg-3">{sa.usage}</span>
            </div>
          ))}
        </div>
        <div className="bg-surface-1 rounded-lg p-2.5 mt-3">
          <code className="text-[10px] font-mono text-fg-3 block">
            padding-bottom: calc(var(--safe-bottom) + 16px);
          </code>
          <p className="text-[10px] text-fg-4 mt-1">
            Bar uses safe-bottom for iPhone home indicator clearance. Always add
            safe area to fixed bottom elements.
          </p>
        </div>
      </DemoCard>

      {/* ── Composition: Full Page Layout ── */}
      <DemoCard label="Composition — page layout">
        <div
          className="relative rounded-xl border border-border overflow-hidden"
          style={{ background: "var(--background)", height: 200 }}
        >
          {/* Content area */}
          <div className="absolute inset-0 flex flex-col items-center pt-8 px-4">
            <div className="w-full max-w-50 space-y-2">
              <div className="h-3 rounded bg-surface-2 w-3/4" />
              <div className="h-3 rounded bg-surface-2 w-full" />
              <div className="h-3 rounded bg-surface-2 w-2/3" />
            </div>
            <span className="text-[9px] text-fg-4 mt-1">max-w-4xl content</span>
          </div>
          {/* Bar */}
          <div className="absolute bottom-4 left-1/2 -translate-x-1/2 w-3/4">
            <div
              className="h-8 rounded-xl flex items-center px-3"
              style={{
                background: "var(--card)",
                border: "1px solid var(--bar-border)",
                boxShadow: "var(--bar-shadow)",
              }}
            >
              <span className="text-[9px] text-fg-3">
                bar (z-bar, fixed bottom)
              </span>
            </div>
          </div>
          {/* Z-labels */}
          <div className="absolute top-2 right-2 space-y-0.5 text-[8px] text-fg-4 font-mono">
            <p>z-page: content</p>
            <p>z-bar: bar</p>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Content scrolls behind bar. Bar is fixed bottom center. No sidebar, no
          top bar. Bottom padding: safe-bottom + bar height + 16px clearance.
        </p>
      </DemoCard>
    </Section>
  );
}
