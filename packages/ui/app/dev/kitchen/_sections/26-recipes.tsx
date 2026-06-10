// Composition Recipes — full-page assembly from tokens → components → layout.
// This section answers: "I need to build a new page. What exact markup do I use?"
//
// FOR LLM AGENTS: Each recipe is a self-contained reference. Copy the structure,
// swap the content. Every class and token is annotated with its source section.
"use client";

import { Section, DemoCard, SIGNATURE } from "../_shared";
import { Surface } from "@/components/surface";
import { Button } from "@/components/button";
import { Badge } from "@/components/badge";
import { Pill } from "@/components/pill";
import { ArrowLeft, Copy, ChevronRight } from "lucide-react";

/* ────────────────────────────────────────────
   RECIPE 1: Brain View (Memory Grid)
   Route: /(app)/page.tsx
   Layout: scrollable grid, no sidebar, bar at bottom
   ──────────────────────────────────────────── */

function BrainViewRecipe() {
  return (
    <div
      className="relative rounded-xl border border-border overflow-hidden"
      style={{ background: "var(--background)", height: 420 }}
    >
      {/* ── Page content ── */}
      <div className="px-6 pt-6 pb-20 mx-auto">
        {/* Group header */}
        <div className="mb-3">
          <span className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold">
            {/* §12 Typography: text-nano, §13 Color: text-fg-3 */}
            Recent
          </span>
        </div>

        {/* Memory rows */}
        {[
          {
            dot: "#3b82f6",
            title: "React Server Components",
            meta: "core · 2h",
            summary: "Server components render on the server...",
          },
          {
            dot: "#10b981",
            title: "Deployment strategy",
            meta: "process · 5h",
            summary: "Blue-green with canary releases...",
          },
          {
            dot: "#f59e0b",
            title: "Auth middleware decision",
            meta: "decisions · 1d",
            summary: "Moving to JWT with refresh rotation...",
          },
        ].map((m) => (
          <div
            key={m.title}
            className="w-full px-4 py-2.5 hover:bg-surface-1 transition-colors border-t border-border/30"
            /* §14 Surfaces: hover:bg-surface-1, §20 Layout: px-4 py-2.5 */
          >
            <div className="flex items-center gap-2">
              <span
                className="h-1.5 w-1.5 rounded-full shrink-0"
                /* §17 Indicators: dot sm = h-1.5 w-1.5, §13 Color: neutral dot */
                style={{ background: m.dot }}
              />
              <span className="text-[14px] font-medium text-foreground truncate flex-1">
                {/* §12 Typography: --text-body, font-medium = H3 card title */}
                {m.title}
              </span>
              <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                {/* §12: --text-secondary, §13: text-fg-3 = tertiary */}
                {m.meta}
              </span>
            </div>
            <p className="text-[13px] text-fg-3 truncate mt-0.5 pl-5">
              {/* §12: --text-secondary, pl-5 aligns with title (past dot) */}
              {m.summary}
            </p>
          </div>
        ))}

        {/* Topic cards */}
        <div className="mt-4 mb-3">
          <span className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold">
            Topics
          </span>
        </div>
        <div className="grid grid-cols-2 gap-3">
          {[
            { name: "Deployment", count: 12, dot: "#10b981" },
            { name: "Architecture", count: 8, dot: "#3b82f6" },
          ].map((t) => (
            <Surface key={t.name} variant="default" className="p-3">
              {/* §14 Surfaces: variant="default" = bar-border + bar-shadow */}
              <div className="flex items-center gap-2 mb-1">
                <span
                  className="h-2 w-2 rounded-full"
                  style={{ background: t.dot }}
                />
                <span className="text-[14px] font-semibold text-fg-1">
                  {t.name}
                </span>
              </div>
              <span className="text-[12px] text-fg-3">{t.count} memories</span>
            </Surface>
          ))}
        </div>
      </div>

      {/* ── Bar (fixed bottom) ── */}
      <div className="absolute bottom-3 left-1/2 -translate-x-1/2 w-[70%] max-w-lg">
        {/* §14 Surfaces: bar surface, §20 Layout: z-bar fixed bottom center */}
        <div
          className="h-14 rounded-2xl flex items-center px-5 gap-3"
          style={{
            background: "var(--card)",
            border: "1px solid var(--bar-border)",
            boxShadow: "var(--bar-shadow)",
          }}
        >
          <span className="text-[10px] text-fg-4 shrink-0">m</span>
          <span className="text-[15px] text-fg-3 flex-1">
            What did we agree on...
          </span>
          <div className="h-8 w-8 rounded-lg bg-foreground/10 flex items-center justify-center">
            <span className="text-[10px] text-fg-3">↑</span>
          </div>
        </div>
      </div>

      {/* Annotation */}
      <div className="absolute top-2 right-2 text-[8px] text-fg-4 font-mono space-y-0.5 text-right">
        <p>max-w-4xl · px-5 sm:px-8</p>
        <p>pb-36 md:pb-32 (bar clearance)</p>
        <p>bar: z-bar, fixed bottom center</p>
      </div>
    </div>
  );
}

/* ────────────────────────────────────────────
   RECIPE 2: Memory Detail Page
   Route: /(app)/memories/[id]/page.tsx
   Layout: borderless reading surface, provenance-first, AI hero
   ──────────────────────────────────────────── */

function MemoryDetailRecipe() {
  return (
    <div
      className="relative rounded-xl border border-border overflow-hidden"
      style={{ background: "var(--background)", height: 480 }}
    >
      <div className="px-6 sm:px-8 pt-8 pb-12 max-w-3xl mx-auto">
        {/* ── Back + Title + Actions ── */}
        <div className="flex items-start gap-3 mb-1">
          <ArrowLeft className="h-5 w-5 text-fg-3 shrink-0 mt-1" />
          <div className="flex-1 min-w-0">
            <h1
              className="text-[21px] font-bold text-foreground leading-snug"
              style={{ letterSpacing: "-0.02em" }}
            >
              React Server Components
            </h1>
          </div>
          <div className="flex items-center gap-2.5 shrink-0 mt-1.5">
            <Copy className="h-3.5 w-3.5 text-fg-4" />
            <span className="text-[13px] text-fg-3">Forget</span>
          </div>
        </div>

        {/* ── Provenance strip ── */}
        <div className="flex items-center gap-1.5 text-[13px] pl-8 mt-1.5">
          <span
            className="text-[13px]"
            style={{ color: "oklch(0.65 0.15 30)" }}
          >
            &gt;_
          </span>
          <span className="text-fg-2 font-medium">Claude Code</span>
          <span className="text-fg-3">captured</span>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3">memax</span>
          <span className="text-fg-4">·</span>
          <span className="text-fg-3">2d ago</span>
          <span className="text-fg-4">·</span>
          <span className="text-fg-2 font-medium tabular-nums">
            recalled 47×
          </span>
        </div>

        {/* ── AI Distillation (hero) ── */}
        <div className="pl-8 mt-8">
          <div className="flex items-center gap-1.5 mb-3">
            <span className="text-[12px]" style={{ color: SIGNATURE }}>
              ✦
            </span>
            <span className="text-[13px] text-fg-3">memax summary</span>
          </div>
          <p className="text-[16px] text-fg-1 leading-[1.65]">
            Server components render on the server and send HTML. Client
            components hydrate on the client. The boundary is the &quot;use
            client&quot; directive.
          </p>
        </div>

        {/* ── Raw content (collapsed) ── */}
        <div className="pl-8 mt-5 pt-5 border-t border-border/20">
          <div className="flex items-center gap-1.5 text-[13px] text-fg-3">
            <ChevronRight className="h-3 w-3" />
            <span>Original content</span>
            <span className="text-fg-4">·</span>
            <span className="text-fg-4">342 words</span>
          </div>
        </div>

        {/* ── Tags + classification ── */}
        <div className="pl-8 mt-5 pt-5 border-t border-border/20">
          <div className="flex flex-wrap gap-1.5 mb-2">
            <Pill variant="static">react</Pill>
            <Pill variant="static">architecture</Pill>
          </div>
          <div className="flex items-center gap-2 text-[13px]">
            <span
              className="h-1.5 w-1.5 rounded-full shrink-0"
              style={{ background: "#3b82f6" }}
            />
            <span className="text-fg-3">core</span>
            <span className="text-fg-4">·</span>
            <span className="text-fg-4">private</span>
          </div>
        </div>
      </div>

      {/* Dissolve into --background */}
      <div
        className="absolute bottom-0 left-0 right-0 h-16 pointer-events-none"
        style={{
          background: "linear-gradient(to top, var(--background), transparent)",
        }}
      />

      {/* Annotation */}
      <div className="absolute top-2 right-2 text-[8px] text-fg-4 font-mono space-y-0.5 text-right">
        <p>borderless — no Surface card</p>
        <p>provenance strip: agent + captured + age + recalled</p>
        <p>summary: text-fg-1 hero, ✦ signature</p>
        <p>raw content: collapsed disclosure, text-fg-2</p>
        <p>dissolve into --background</p>
      </div>
    </div>
  );
}

/* ────────────────────────────────────────────
   RECIPE 3: Settings Page
   Route: settings dialog (centered modal)
   Layout: sidebar nav (desktop) + content area
   ──────────────────────────────────────────── */

function SettingsRecipe() {
  return (
    <div
      className="relative rounded-xl border border-border overflow-hidden"
      style={{ background: "var(--background)", height: 360 }}
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0"
        style={{ background: "rgba(0,0,0,0.15)" }}
      />

      {/* Modal */}
      <div
        className="absolute inset-6 rounded-2xl overflow-hidden flex"
        style={{
          background: "var(--card)",
          border: "1px solid var(--bar-border)",
          boxShadow: "var(--bar-shadow)",
        }}
      >
        {/* Sidebar — 220px */}
        <div className="w-36 shrink-0 border-r border-border/30 p-3 space-y-0.5">
          {/* §20 Layout: sidebar 220px (scaled down here) */}
          {["General", "Integrations", "Intelligence", "Advanced"].map(
            (tab, i) => (
              <div
                key={tab}
                className={`px-3 py-1.5 rounded-lg text-[12px] ${i === 0 ? "bg-surface-2 text-fg-1 font-medium" : "text-fg-3 hover:bg-surface-1"}`}
              >
                {tab}
              </div>
            ),
          )}
        </div>

        {/* Content */}
        <div className="flex-1 p-5 overflow-y-auto">
          {/* §12: H2 = text-[16px] font-bold */}
          <h2 className="text-[16px] font-bold text-foreground mb-4">
            General
          </h2>

          {/* Settings rows */}
          <div className="divide-y divide-border/20">
            <div className="flex items-center justify-between py-3">
              <div>
                <span className="text-[14px] text-fg-1">Display name</span>
                <p className="text-[12px] text-fg-3">Visible to team members</p>
              </div>
              <Button variant="outline" size="sm">
                Edit
              </Button>
            </div>
            <div className="flex items-center justify-between py-3">
              <div>
                <span className="text-[14px] text-fg-1">Theme</span>
                <p className="text-[12px] text-fg-3">System, Light, or Dark</p>
              </div>
              <Badge variant="secondary">System</Badge>
            </div>
            <div className="flex items-center justify-between py-3">
              <div>
                <span className="text-[14px] text-fg-1">Memory Dreams</span>
                <p className="text-[12px] text-fg-3">Nightly consolidation</p>
              </div>
              {/* Toggle on */}
              <div
                className="w-10 h-6 rounded-full relative"
                style={{ background: "var(--signature)" }}
              >
                <div
                  className="absolute top-0.5 w-4.5 h-4.5 rounded-full bg-white"
                  style={{ transform: "translateX(18px)" }}
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Annotation */}
      <div className="absolute top-2 right-2 text-[8px] text-fg-4 font-mono space-y-0.5 text-right z-10">
        <p>z-modal: backdrop rgba(0,0,0,0.15)</p>
        <p>z-modal: modal 880px centered</p>
        <p>sidebar: 220px, border-r border-border/30</p>
        <p>rows: divide-y divide-border/20</p>
      </div>
    </div>
  );
}

/* ────────────────────────────────────────────
   LLM-OPTIMIZED MARKUP REFERENCE
   Structured data block for agent extraction
   ──────────────────────────────────────────── */

const MARKUP_RECIPES = [
  {
    name: "Memory Row",
    context: "Brain view grid, topic detail, search results",
    code: `<div className="w-full px-4 py-2.5 hover:bg-surface-1 transition-colors border-t border-border/30">
  <div className="flex items-center gap-2">
    <span className="h-1.5 w-1.5 rounded-full" style={{ background: dotColor }} />
    <span className="text-[14px] font-medium text-foreground truncate flex-1">{title}</span>
    <span className="text-[13px] text-fg-3 tabular-nums">{meta}</span>
  </div>
  <p className="text-[13px] text-fg-3 truncate mt-0.5 pl-5">{summary}</p>
</div>`,
  },
  {
    name: "AI Distillation (memory detail hero)",
    context: "Memory detail — summary as primary content, on --background",
    code: `<div>
  <div className="flex items-center gap-1.5 mb-3">
    <span className="text-[12px]" style={{ color: "var(--signature)" }}>✦</span>
    <span className="text-[13px] text-fg-3">memax summary</span>
  </div>
  <div className="text-[16px] text-fg-1 leading-[1.65]">{summary}</div>
</div>`,
  },
  {
    name: "Settings Row",
    context: "Settings dialog, hub management",
    code: `<div className="flex items-center justify-between py-3">
  <div>
    <span className="text-[14px] text-fg-1">{label}</span>
    <p className="text-[12px] text-fg-3">{description}</p>
  </div>
  <Button variant="outline" size="sm">{action}</Button>
</div>`,
  },
  {
    name: "Page Shell",
    context: "Any new route under /(app)/",
    code: `<div className="pb-36 md:pb-32 animate-content-ready" style={{ paddingTop: CONTENT_TOP }}>
  <div className="mx-auto max-w-4xl px-5 sm:px-8">
    {/* Page content */}
  </div>
</div>`,
  },
  {
    name: "Group Header",
    context: "Section label above memory rows or card grids",
    code: `<span className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold">
  {groupLabel}
</span>`,
  },
  {
    name: "Empty State",
    context: "No results, empty topic, first-run",
    code: `<div className="flex flex-col items-center justify-center text-center" style={{ minHeight: CENTERED_HEIGHT }}>
  <p className="text-[16px] text-fg-2 mb-2">{message}</p>
  <p className="text-[13px] text-fg-3">{hint}</p>
</div>`,
  },
];

export function RecipesSection() {
  return (
    <Section
      title="26. Composition Recipes"
      description="Full-page assembly from tokens → components → layout. Copy the structure, swap the content. Every class is annotated with its source section."
    >
      {/* ── Recipe 1: Brain View ── */}
      <DemoCard label="Recipe — Brain View (memory grid)">
        <BrainViewRecipe />
        <div className="mt-3 space-y-0.5 text-[10px] text-fg-4 font-mono">
          <p>Container: max-w-4xl mx-auto px-5 sm:px-8 pb-36 md:pb-32</p>
          <p>
            Group header: text-[10px] text-fg-3 uppercase tracking-wider
            font-semibold
          </p>
          <p>
            Memory row: px-4 py-2.5 border-t border-border/30 hover:bg-surface-1
          </p>
          <p>Topic card: Surface variant=&quot;default&quot; p-3</p>
          <p>
            Bar: fixed bottom center, z-bar, rounded-2xl, bar-border +
            bar-shadow
          </p>
        </div>
      </DemoCard>

      {/* ── Recipe 2: Memory Detail ── */}
      <DemoCard label="Recipe — Memory Detail (borderless)">
        <MemoryDetailRecipe />
        <div className="mt-3 space-y-0.5 text-[10px] text-fg-4 font-mono">
          <p>Borderless — no Surface card. All on --background.</p>
          <p>
            Provenance: agent icon + name + captured + project + age + recalled
            N×
          </p>
          <p>
            Summary: text-fg-1 hero, ✦ in signature. Raw content: collapsed
            disclosure, text-fg-2.
          </p>
          <p>
            Sections: border-t border-border/20 + mt-5 pt-5. All content pl-8.
          </p>
          <p>
            Dissolve: linear-gradient(to top, var(--background), transparent)
          </p>
        </div>
      </DemoCard>

      {/* ── Recipe 3: Settings ── */}
      <DemoCard label="Recipe — Settings Dialog">
        <SettingsRecipe />
        <div className="mt-3 space-y-0.5 text-[10px] text-fg-4 font-mono">
          <p>
            Modal: 880px centered, z-modal, rounded-2xl, bar-border + bar-shadow
          </p>
          <p>Backdrop: z-modal, rgba(0,0,0,0.15)</p>
          <p>Sidebar: 220px, border-r border-border/30</p>
          <p>Active tab: bg-surface-2 text-fg-1 font-medium</p>
          <p>Rows: divide-y divide-border/20, py-3</p>
        </div>
      </DemoCard>

      {/* ── Copy-Paste Markup ── */}
      <DemoCard label="Copy-paste markup — for LLM agents and developers">
        <p className="text-[11px] text-fg-3 mb-4">
          Structured code blocks for the most common patterns. Copy directly
          into new components.
        </p>
        <div className="space-y-4">
          {MARKUP_RECIPES.map((r) => (
            <div key={r.name}>
              <div className="flex items-baseline gap-2 mb-1.5">
                <span className="text-[13px] font-semibold text-fg-1">
                  {r.name}
                </span>
                <span className="text-[10px] text-fg-4">{r.context}</span>
              </div>
              <pre className="bg-surface-1 rounded-lg p-3 overflow-x-auto text-[11px] text-fg-2 font-mono leading-relaxed">
                {r.code}
              </pre>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Token Cheat Sheet ── */}
      <DemoCard label="Token cheat sheet — quick lookup">
        <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-[11px]">
          <div className="space-y-1.5">
            <p className="text-fg-4 uppercase tracking-wider font-semibold text-[10px]">
              Page layout
            </p>
            <p className="font-mono">
              <span className="text-fg-3">width:</span>{" "}
              <span className="text-fg-2">max-w-4xl (896px)</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">padding:</span>{" "}
              <span className="text-fg-2">px-5 sm:px-8</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">bottom:</span>{" "}
              <span className="text-fg-2">pb-36 md:pb-32</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">top:</span>{" "}
              <span className="text-fg-2">CONTENT_TOP (80px)</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">entrance:</span>{" "}
              <span className="text-fg-2">animate-content-ready</span>
            </p>
          </div>
          <div className="space-y-1.5">
            <p className="text-fg-4 uppercase tracking-wider font-semibold text-[10px]">
              Surfaces
            </p>
            <p className="font-mono">
              <span className="text-fg-3">grid card:</span>{" "}
              <span className="text-fg-2">
                Surface variant=&quot;default&quot;
              </span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">detail:</span>{" "}
              <span className="text-fg-2">borderless (no Surface)</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">section sep:</span>{" "}
              <span className="text-fg-2">border-t border-border/20</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">row hover:</span>{" "}
              <span className="text-fg-2">hover:bg-surface-1</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">row divider:</span>{" "}
              <span className="text-fg-2">border-t border-border/30</span>
            </p>
          </div>
          <div className="space-y-1.5 mt-2">
            <p className="text-fg-4 uppercase tracking-wider font-semibold text-[10px]">
              Typography
            </p>
            <p className="font-mono">
              <span className="text-fg-3">H1:</span>{" "}
              <span className="text-fg-2">text-[21px] font-bold</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">H2:</span>{" "}
              <span className="text-fg-2">text-[16px] font-bold</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">H3:</span>{" "}
              <span className="text-fg-2">text-[14px] font-semibold</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">body:</span>{" "}
              <span className="text-fg-2">text-[14px] text-fg-1</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">meta:</span>{" "}
              <span className="text-fg-2">text-[12px] text-fg-3</span>
            </p>
          </div>
          <div className="space-y-1.5 mt-2">
            <p className="text-fg-4 uppercase tracking-wider font-semibold text-[10px]">
              Motion
            </p>
            <p className="font-mono">
              <span className="text-fg-3">easing:</span>{" "}
              <span className="text-fg-2">var(--ease-spring)</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">fast:</span>{" "}
              <span className="text-fg-2">0.15s (modal, hover)</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">normal:</span>{" "}
              <span className="text-fg-2">0.2s (content, layout)</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">enter:</span>{" "}
              <span className="text-fg-2">animate-content-ready</span>
            </p>
            <p className="font-mono">
              <span className="text-fg-3">ai:</span>{" "}
              <span className="text-fg-2">state-slow-breathe</span>
            </p>
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
