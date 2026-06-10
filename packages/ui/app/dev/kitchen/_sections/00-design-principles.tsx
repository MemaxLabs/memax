// The "spirit" of memax design — read this first, before anything else.
// Every section, component, and token decision flows from these principles.
"use client";

import { Section, DemoCard, SIGNATURE } from "../_shared";

/* ── Principles ── */

const PRINCIPLES: {
  number: string;
  title: string;
  rule: string;
  why: string;
  example: string;
}[] = [
  {
    number: "01",
    title: "Opacity, not colors",
    rule: "All text and surface colors derive from --foreground via opacity. Never hardcode color values in components.",
    why: "One variable change updates the entire palette. Dark mode, themes, and accessibility come free.",
    example: "text-fg-2 (65% opacity) instead of text-gray-500",
  },
  {
    number: "02",
    title: "Container morphing",
    rule: "The same container adapts to different states. Never overlay when the surface can transform.",
    why: "Overlays break spatial continuity. Users lose context when content jumps between surfaces.",
    example:
      "Memory card morphs into detail view — no modal popup, no new page.",
  },
  {
    number: "03",
    title: "Signature = intelligence",
    rule: "Dream Violet (--signature) appears only where memax AI is actively working. Never decorative.",
    why: "If signature color is everywhere, it means nothing. Scarcity creates meaning.",
    example:
      "✦ star breathes during AI streaming, static when complete. Never on borders or backgrounds.",
  },
  {
    number: "04",
    title: "No sidebar, no top bar",
    rule: "Floating bottom bar, centered content. Tree navigation is peek-on-demand, not always visible.",
    why: "Maximum content density. The bar is the only persistent UI element.",
    example:
      "Bar: fixed bottom center (z-50). Content: scrolls freely with max-w-4xl.",
  },
  {
    number: "05",
    title: "Spring easing always",
    rule: "Use var(--ease-spring) for all transitions. Never linear, never ease-in-out.",
    why: "Spring easing (fast start, gentle settle) creates the memax feel — responsive, alive, never sluggish.",
    example: "cubic-bezier(0.16, 1, 0.3, 1) for all UI motion.",
  },
  {
    number: "06",
    title: "Shape before content",
    rule: "Skeletons mirror the final layout exactly. Loading states show shape, not spinners.",
    why: "The brain anchors to layout. When content loads into the same shape, it feels instant.",
    example: "Shimmer skeleton with dot+title+body matches loaded MemoryRow.",
  },
  {
    number: "07",
    title: "Spacing creates hierarchy",
    rule: "Use spacing and opacity to separate sections. Never add visible dividers or accent borders.",
    why: "Dividers add visual noise. Whitespace is a stronger (and quieter) grouping signal.",
    example:
      "divide-border/20 (barely visible) or gap spacing, never border-l-4 colored accent.",
  },
  {
    number: "08",
    title: "Mobile-first, keyboard-ready",
    rule: "Default styles are mobile. Add sm:/md:/lg: for larger screens. Never block keyboard handlers behind isMobile.",
    why: "Touch targets and keyboard shortcuts coexist. isMobile is for visual hints only, never for disabling functionality.",
    example:
      "Mobile: pill chips (44px touch). Desktop: text toggle. Both work with keyboard.",
  },
  {
    number: "09",
    title: "Single blur layer on mobile",
    rule: "At most ONE backdrop-filter element composited at a time on mobile. Never stack scrim + edge + bar. Never blur a solid bg.",
    why: "Mobile GPUs choke on stacked backdrop-filter. Invisible blur (over opaque bg-card) still costs a full compositing pass — pure waste.",
    example:
      "Mobile compose = solid var(--background) sheet, zero backdrop-filter. Popover over bg-card = no blur.",
  },
  {
    number: "10",
    title: "Mobile navigation is instant",
    rule: "Cross-tab dock swap, memory detail back, and in-app route changes run with ZERO fade or slide on mobile. Entrance animations are opacity-only when present.",
    why: "Any motion on cross-tab navigation reads as lag. Apple Notes, Linear, Instagram all snap-switch tabs. Motion is reserved for the compose/drill flows that need it (kitchen 29n topic drill only).",
    example:
      "mobile-dock onClick → router.push(). No requestSurfaceTransitionForNavigation. No color transition on tab button. router.back() has no setTimeout.",
  },
];

/* ── Anti-patterns ── */

const ANTI_PATTERNS = [
  {
    bad: "text-gray-500",
    good: "text-fg-3",
    why: "Gray doesn't adapt to dark mode or themes",
  },
  {
    bad: "ease-in-out",
    good: "var(--ease-spring)",
    why: "Feels sluggish and generic",
  },
  {
    bad: "border-l-4 border-blue-500",
    good: "Surface variant + shadow lift",
    why: "Colored accent lines are not memax",
  },
  {
    bad: "bg-gray-100",
    good: "bg-surface-1",
    why: "Surface tokens adapt to any theme",
  },
  {
    bad: "shadow-md",
    good: "var(--bar-shadow)",
    why: "Bar shadow is a composed, branded shadow",
  },
  {
    bad: "rounded-md (6px) / rounded-lg (8px) on cards or buttons",
    good: "rounded-surface (20px) on cards/dialogs; rounded-chrome (14px) on buttons/inputs/chips",
    why: "Two-tier system from surface-radius.css: --app-radius-surface (20px) for large containers, --app-radius-chrome (14px) for interactive chrome ≤40px. Everything else is stale.",
  },
  {
    bad: "Modal overlay for editing",
    good: "Inline transform / morph",
    why: "Container morphing principle",
  },
  {
    bad: "Spinner for loading",
    good: "Skeleton shimmer",
    why: "Shape before content principle",
  },
  {
    bad: "text-foreground/40",
    good: "text-fg-3",
    why: "Use semantic tokens, not raw opacity",
  },
  {
    bad: "font-light (300)",
    good: "font-normal (400) minimum",
    why: "Product uses 400-700 weight range only",
  },
  {
    bad: "backdrop-filter: blur() over bg-card",
    good: "no blur — bg-card is solid, blur is invisible",
    why: "Full-viewport GPU compositing pass per frame for zero visual benefit",
  },
  {
    bad: "Stacked blur layers on mobile (scrim + edge + bar)",
    good: "One blur surface OR solid sheet",
    why: "Mobile GPUs drop frames on multi-backdrop-filter compositing",
  },
  {
    bad: 'AnimatePresence mode="wait" on routes',
    good: "Default parallel AnimatePresence",
    why: "Serial mode doubles perceived duration (exit + enter)",
  },
  {
    bad: "animate-slide-in-right / out-right on mobile routes",
    good: "CSS fade-in + instant router.back()",
    why: "Slide wrappers add 250-350ms lag to every mobile route swap",
  },
  {
    bad: "setTimeout(() => router.back(), 250)",
    good: "router.back() instant",
    why: "Users perceive the delay as lag, not animation polish",
  },
  {
    bad: "Multiple motion.div nested for one fade",
    good: "Single motion.div OR plain render",
    why: "Framer overhead eats the budget at sub-150ms durations",
  },
  {
    bad: "requestSurfaceTransitionForNavigation on mobile",
    good: "router.push() only",
    why: "Overlay + content scale/translate is desktop polish; mobile snaps",
  },
  {
    bad: "Translucent scrim over page for mobile compose",
    good: "Solid var(--background) sheet (container takeover)",
    why: "Kitchen 38e3 — compose TAKES OVER, doesn't LAYER. Apple Notes pattern",
  },
];

export function DesignPrinciplesSection() {
  return (
    <Section
      title="0. Design Principles"
      description="The memax design philosophy. Read this first — every token, component, and pattern decision flows from these 8 rules."
    >
      {/* ── LLM Agent Quick Reference ── */}
      <DemoCard label="LLM Agent Reference — structured rules for code generation">
        <p className="text-[11px] text-fg-3 mb-3">
          If you are an AI agent generating memax UI code, these are the hard
          rules. Each rule maps to a section. Violating any rule produces
          non-memax output.
        </p>
        <pre className="bg-surface-1 rounded-lg p-4 overflow-x-auto text-[10px] text-fg-2 font-mono leading-relaxed whitespace-pre">{`MEMAX DESIGN SYSTEM — HARD RULES FOR CODE GENERATION
═══════════════════════════════════════════════════════

COLORS (§13)
  text:       text-fg-1 (titles, body) | text-fg-2 (secondary) | text-fg-3 (meta) | text-fg-4 (decorative)
  surfaces:   bg-surface-1 (subtle) | bg-surface-2 (hover) | bg-surface-3 (active)
  accent:     var(--signature) — ONLY for AI indicators (✦) AND Intelligence tab controls, never decorative
  NEVER:      text-gray-*, bg-gray-*, hardcoded colors, text-foreground/XX

CONTROL COLOR SEMANTICS (§19)
  rule:       purple = Intelligence tab ONLY. Everywhere else uses NEUTRAL_INK.
  NEUTRAL_INK:       var(--fg-1) — active toggles, radio rings, radio dots
  NEUTRAL_INK_INVERSE: var(--background) — toggle thumb when filled
  off states:        NEUTRAL_TRACK_OFF / NEUTRAL_BORDER_OFF / NEUTRAL_THUMB_OFF
  import from:       @memaxlabs/ui/tokens/controls
  toggle:     iOS-solid. Track fills with active color, thumb inverts.
  radio:      border-2 ring + NEUTRAL_INK dot. NEVER fill the outer ring.
  pills vs radio rows: pills = self-explanatory labels (roles, tiers). radio rows = needs descriptions (Plain/Signature/Time).
  See §19 "Control color semantics" card for the canonical rule block.

TYPOGRAPHY (§12)
  H1:         text-[21px] font-bold                    (one per view, memory detail title)
  H2:         text-[16px] font-bold                    (section headers)
  H3:         text-[14px] font-semibold                (card titles)
  body:       text-[14px] text-fg-1                    (default)
  secondary:  text-[13px] text-fg-2 or text-fg-3       (descriptions, meta)
  caption:    text-[12px] text-fg-3                    (timestamps, kind)
  micro:      text-[10px] text-fg-4 uppercase tracking-wider font-semibold (group headers)
  weights:    400-700 only. NEVER font-light (300) or font-black (900)
  prose:      leading-[1.65] for AI answers and readable paragraphs

SURFACES (§14)
  card grid:  <Surface variant="default">     (bar-border + bar-shadow)
  detail:     <Surface variant="subtle">      (border only)
  mobile:     <Surface variant="borderless">  (full bleed)
  minimal:    <Surface variant="clean">       (bg only)
  note:       Surface's 'rounded' prop ("xl"|"2xl"|"lg") is a component API
              that resolves to the unified tokens — "xl"/"2xl" resolve to
              rounded-surface (20px); "lg" is legacy — don't use it. Surface
              defaults to rounded-surface when the prop is omitted.
  radius:     TWO tiers only — rounded-surface (20px) | rounded-chrome (14px)
              surface: cards, dialogs, popovers, bar, form sub-cards, containers
              chrome:  buttons, inputs, chips, role tags, code pills, any interactive
                       element ≤40px tall (even role tags become pill-ish at that size)
              source:  packages/ui/src/surface-radius.css (root-scoped, theme-safe)
              NEVER use rounded-lg/md/xl/2xl/3xl on product UI — those are stale.
              Exceptions:
                rounded-full  — true circles (avatars, status dots) OR status pills
                                ≤24px tall where the half-height clamps to the same
                                curve as rounded-chrome anyway
                rounded-sm    — 2px focus outlines on inline text links only

LAYOUT (§20)
  page:       max-w-4xl mx-auto px-5 sm:px-8 pb-36 md:pb-32
  top:        paddingTop: CONTENT_TOP (80px desktop)
  entrance:   animate-content-ready (on every page load)
  bar:        fixed bottom center, z-bar, max-w-[640px]
  modal:      z-modal (backdrop + content — DOM order stacks)

Z-INDEX SCALE — semantic tokens only (§20)
  NEVER use hardcoded z-50, z-[60], etc. Pick from this scale:
  z-page         (0)   regular page content
  z-bar-notif    (30)  bar notification (below bar)
  z-bar          (40)  command bar, mobile dock, top-right chrome row
  z-topic-tree   (50)  Topic Explorer floating panel + mobile fullscreen
  z-modal        (60)  Settings panel, Settings dialog, memory modal,
                       admin drawer, batch-toolbar backdrop
  z-popover      (70)  ALL popovers/dropdowns/menus/InfoPopover (above
                       modal so popovers triggered inside a dialog work)
  z-takeover     (80)  full-screen immersive: lightbox, hub-create,
                       mobile compose, surface-transition-overlay
  z-toast        (90)  top-most ephemeral feedback, impersonation-bar

  Invariant: popover > modal > topic-tree > bar > bar-notif > page.
  Invariant: takeover > popover (full-screen covers dropdowns).
  Tokens are defined as CSS vars (--z-*) in :root + @utility blocks
  in globals.css. Do NOT invent new tiers unless the token scale
  genuinely can't express the layer — add a tier with a rationale.

GLASS + BACKDROP-BLUR — unified surface material (§14)
  ALL translucent floating surfaces use ONE of three sibling classes,
  each paired with backdrop-blur-sm (8px) so the standard
  backdrop-filter property emits (Lightning CSS strips the standard
  form from raw custom-CSS declarations in some cases).
  .glass-bar       command bar                           → paired in layout.tsx
  .glass-panel     Topic Explorer tree panel             → paired in topic-tree-panel.tsx
  .glass-dropdown  ALL popovers (via PopoverContent)     → paired in packages/ui/src/components/popover.tsx
  Recipe: 65% fill + saturate(180%) contrast(1.05) + rim inset + ambient shadow.
  NEVER hand-roll a fourth glass variant. If a new surface needs glass,
  reuse one of the three. The popover primitive OWNS the material for
  every dropdown — consumers pass NO variant/override.
  Popovers: PopoverContent is always glass — no variant prop.
  Menu rows: <MenuItem> from @memaxlabs/ui — never hand-roll a button
  with px/py/hover/radius classes for menu items.

MOTION (§18)
  easing:     var(--ease-spring) — cubic-bezier(0.16, 1, 0.3, 1)
  NEVER:      linear, ease-in-out, ease
  fast:       0.15s (hover, modal, dropdown)
  normal:     0.2s (content transitions)
  AI breathe: state-slow-breathe (2.5s loop)
  entrance:   animate-content-ready (0.15s opacity + translateY)

MOBILE MOTION — HARD RULES (§38e3 mobile lifecycle)
  ░░░ THE SINGLE-BLUR INVARIANT ░░░
  At most ONE backdrop-filter: blur() element composited at a time on mobile.
  Stacking (scrim + glass edge + bar) is the #1 frame-drop cause on mobile GPUs.
  Never blur over a solid background — if bg-card / bg-background is opaque,
  backdrop-filter is INVISIBLE but still costs a full compositing pass per
  frame. Check --card / --background opacity BEFORE adding blur.

  CROSS-TAB DOCK SWITCH (mobile)
  Instant. No fade, no surface-transition overlay, no color-interpolation
  on tab buttons. router.push() only. Desktop keeps subtle fade-in; mobile
  snaps. NEVER call requestSurfaceTransitionForNavigation on mobile tabs.

  MEMORY DETAIL NAVIGATION
  Entry:  opacity fade via template's animate-fade-in (150ms). No slide-in.
  Exit:   router.back() INSTANT. No setTimeout delay, no slide-out-right.
  NEVER:  animate-slide-in-right / animate-slide-out-right on mobile routes.

  TOPIC DRILL (kitchen 29n spec)
  Duration:  FAST (150ms) on mobile, NORMAL (200ms) on desktop.
  Translate: ±16px on x-axis.
  Easing:    var(--ease-spring).
  Parallel:  AnimatePresence WITHOUT mode="wait". Serial mode doubles
             perceived duration (exit 200ms + enter 200ms = 400ms).

  MOBILE COMPOSE SHEET (kitchen 38e3)
  Surface:   ONE solid var(--background) sheet. NEVER translucent + blur.
             The compose state TAKES OVER, it doesn't LAYER on top.
  Motion:    opacity fade only (FAST 150ms). NO y-slide, NO delayed content
             fade, NO framer layout animations.
  Gesture:   drag-to-dismiss is user-initiated — keep it. Rubber-band
             resist 0.55, dismiss >96px OR velocity >720px/s, settle 0.25s.

  BAR ON MOBILE
  - No 100ms setTimeout stagger before barVisible flips.
  - No CSS transition on outer positioning div (top / transform constants).
  - No framer opacity+y fade-in on mount/tab-switch.
  - Position: calc(100dvh - 96px - var(--safe-bottom, 0px)) above dock.
  - Rest dock has zero backdrop-filter (see single-blur invariant).

  CHROME ROW (logo + hub chip + avatar)
  - No transition-all on mobile — tab switches must not animate bg/border.
  - Always transparent on mobile; banner-mode tint transition is desktop-only.
  - BrandLogo: h-5 w-24 (mobile) / h-6 w-30 (desktop).

  MEMORY ROW COMPACT (mobile recent surface)
  - showSummary = false (no description preview).
  - useStackedRecent = false (single-line layout).
  - showCopy = false (saves horizontal space).
  - leadingIdentity = "none" (title leads the scan).
  - trailingActor = author avatar | agent icon | none.
  - showTrailingContentMeta = true for pdf/image/link (NOT for docs/notes).
  - Flag: isMobileCompactRecent in memory-row-presentation.ts.

  POPOVERS / DROPDOWNS
  - No backdrop-filter over solid bg-card. Remove it — the card is opaque.
  - data-open:duration-100 (not default 150) for snap-open feel.
  - For mobile SettingsPanel-style: plain conditional render, no framer
    AnimatePresence. Framer overhead eats the budget at sub-150ms durations.

  FRAMER MOTION BUDGET
  - Never nest motion.div with opacity fades 3 levels deep for one appearance.
  - duration < 0.15s → prefer CSS transition or plain render (framer tax).
  - AnimatePresence mode="wait" → NEVER on routes; use default parallel.

CONTROLS (§19)
  buttons:    <Button variant="default|outline|secondary|ghost|destructive">
  sizes:      xs (h-6) | sm (h-7) | default (h-8) | lg (h-9)
  icon:       icon-xs (24px) | icon-sm (28px) | icon (32px) | icon-lg (36px)
  send btn:   h-8 w-8 rounded-lg. Push=foreground fill, Recall=signature fill
  toggle:     w-10 h-6 rounded-full. See CONTROL COLOR SEMANTICS above for on/off colors.

ACCESSIBILITY (§27)
  fg-1:       12.2:1 — safe everywhere
  fg-2:       5.2:1  — safe everywhere
  fg-3:       2.5:1  — meta/labels only, FAILS AA normal text in light mode
  fg-4:       1.5:1  — decorative only, NEVER readable text
  focus:      focus-visible:ring-3 focus-visible:ring-ring/50
  touch:      44px primary, 32px secondary, 24px minimum`}</pre>
      </DemoCard>

      {/* ── Principles ── */}
      <DemoCard label="8 principles">
        <div className="space-y-0">
          {PRINCIPLES.map((p) => (
            <div
              key={p.number}
              className="flex items-start gap-3 py-3 border-b border-border/10 last:border-0"
            >
              <div className="w-7 h-7 rounded-lg bg-surface-2 flex items-center justify-center shrink-0">
                <span className="text-[11px] font-mono font-bold text-fg-2">
                  {p.number}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <span className="text-[14px] font-semibold text-fg-1 block mb-0.5">
                  {p.title}
                </span>
                <p className="text-[12px] text-fg-2 mb-1">{p.rule}</p>
                <p className="text-[11px] text-fg-3 mb-1">
                  <span className="font-medium">Why:</span> {p.why}
                </p>
                <code className="text-[10px] text-fg-4 font-mono">
                  {p.example}
                </code>
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── What Makes Memax Feel Like Memax ── */}
      <DemoCard label="The memax feel">
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div className="bg-surface-1 rounded-lg p-3">
            <span className="text-[13px] font-semibold text-fg-1 block mb-1">
              Quiet
            </span>
            <p className="text-[11px] text-fg-3">
              No visual noise. Opacity hierarchy instead of color variety.
              Whitespace instead of dividers. The content breathes.
            </p>
          </div>
          <div className="bg-surface-1 rounded-lg p-3">
            <span className="text-[13px] font-semibold text-fg-1 block mb-1">
              Alive
            </span>
            <p className="text-[11px] text-fg-3">
              Spring easing on everything. ✦ breathes when AI works. Containers
              morph instead of swapping. Nothing feels static or dead.
            </p>
          </div>
          <div className="bg-surface-1 rounded-lg p-3">
            <span className="text-[13px] font-semibold text-fg-1 block mb-1">
              Confident
            </span>
            <p className="text-[11px] text-fg-3">
              One accent color, used sparingly. No gradients, no decorative
              elements. The product gets out of the way. Content is the hero.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── Anti-patterns ── */}
      <DemoCard label="Anti-patterns — never do this">
        <div className="space-y-0.5">
          {/* Header */}
          <div className="grid grid-cols-[1fr_1fr_1fr] gap-2 pb-2 border-b border-border/30 mb-1">
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Bad
            </span>
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Good
            </span>
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Why
            </span>
          </div>
          {ANTI_PATTERNS.map((ap) => (
            <div
              key={ap.bad}
              className="grid grid-cols-[1fr_1fr_1fr] gap-2 py-1.5 border-b border-border/10 items-baseline"
            >
              <code className="text-[10px] font-mono text-destructive/70 line-through">
                {ap.bad}
              </code>
              <code className="text-[10px] font-mono text-fg-2">{ap.good}</code>
              <span className="text-[10px] text-fg-4">{ap.why}</span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Quick Reference ── */}
      <DemoCard label="Quick reference — where to find what">
        <div className="grid grid-cols-2 gap-3 text-[11px]">
          <div className="space-y-1.5">
            <p className="text-fg-4 uppercase tracking-wider font-semibold text-[10px]">
              Foundations
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Colors →</span> Section 13
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Typography →</span> Section 12
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Surfaces →</span> Section 14
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Spacing →</span> Section 20
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Motion →</span> Section 18
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Branding →</span> Section 11
            </p>
          </div>
          <div className="space-y-1.5">
            <p className="text-fg-4 uppercase tracking-wider font-semibold text-[10px]">
              Components
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Buttons/Badges →</span> Section 19
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Indicators →</span> Section 17
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">State Machine →</span> Section 16
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Loading →</span> Section 08
            </p>
            <p className="text-fg-2">
              <span className="text-fg-3">Empty States →</span> Section 09
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── Design System File Map ── */}
      <DemoCard label="File map — source of truth">
        <div className="space-y-1.5 text-[11px] font-mono">
          <div className="flex items-center gap-2">
            <code className="text-fg-2">globals.css</code>
            <span className="text-fg-4">
              — all CSS custom properties (tokens)
            </span>
          </div>
          <div className="flex items-center gap-2">
            <code className="text-fg-2">ui/button.tsx</code>
            <span className="text-fg-4">— Button (CVA variants + sizes)</span>
          </div>
          <div className="flex items-center gap-2">
            <code className="text-fg-2">ui/badge.tsx</code>
            <span className="text-fg-4">— Badge (CVA variants)</span>
          </div>
          <div className="flex items-center gap-2">
            <code className="text-fg-2">ui/surface.tsx</code>
            <span className="text-fg-4">— Surface (5 container variants)</span>
          </div>
          <div className="flex items-center gap-2">
            <code className="text-fg-2">ui/pill.tsx</code>
            <span className="text-fg-4">
              — Pill (select / remove / add / static)
            </span>
          </div>
          <div className="flex items-center gap-2">
            <code className="text-fg-2">ui/skeleton.tsx</code>
            <span className="text-fg-4">— Skeleton (loading placeholder)</span>
          </div>
          <div className="flex items-center gap-2">
            <code className="text-fg-2">ui/memax-loader.tsx</code>
            <span className="text-fg-4">— MemaxLoader (signature dots)</span>
          </div>
          <div className="flex items-center gap-2">
            <code className="text-fg-2">lib/motion.ts</code>
            <span className="text-fg-4">— JS timing constants</span>
          </div>
          <div className="flex items-center gap-2">
            <code className="text-fg-2">lib/kind.ts</code>
            <span className="text-fg-4">— Kind dot colors</span>
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
