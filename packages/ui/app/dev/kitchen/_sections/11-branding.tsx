// Maps to: memax-loader.tsx, memax-logo.tsx (packages/ui/)
"use client";

import { ChevronDown } from "lucide-react";
import { Section, DemoCard, LOGO_PATH } from "../_shared";
import { MemaxLoader } from "@/components/memax-loader";
import { MemaxWordmark } from "@/memax-logo";

/* ── Wordmark ink is NEUTRAL, not signature ──
 *
 * Early draft of this card recommended painting the wordmark with
 * var(--signature). That was wrong: signature is already the accent
 * language (hub avatar fill, dreams, active states), so using it on the
 * wordmark too collapses hierarchy — everything reads as "the violet
 * thing". Linear / Notion / Vercel / Apple all keep their wordmarks
 * neutral for exactly this reason; the brand anchor stays stable while
 * accents move around it.
 *
 * The wordmark tracks var(--foreground) — which already shifts per
 * theme (near-black in light mode, near-white in dark, with each
 * palette's subtle gray tint baked in via the `grays` table in
 * _kitchen-context.tsx). That IS the theme-aware behavior we want, at
 * the only layer it belongs: lightness, not hue.
 *
 * The "feels small" problem is 100% size + opacity, not color.
 */

function MiniCapsule() {
  // Faithful mini-recreation of the top-bar right capsule so the wordmark
  // sits next to the same visual weight it actually competes with in-app.
  return (
    <div className="flex items-center gap-2 rounded-2xl border border-white/10 bg-background/60 px-2 py-1.5 backdrop-blur-md">
      <div className="flex h-9 items-center gap-2 rounded-xl border border-border/60 bg-card px-3 text-[14px] text-fg-2">
        <span
          className="flex h-6 w-6 items-center justify-center rounded-full text-[11px] font-medium"
          style={{
            background: "oklch(from var(--signature) l c h / 0.15)",
            color: "var(--signature)",
          }}
        >
          小
        </span>
        <span className="truncate">小宝记忆空间</span>
        <ChevronDown className="size-4 shrink-0 text-fg-4" />
      </div>
      <div className="h-8 w-8 shrink-0 rounded-full border border-border/50 bg-card" />
    </div>
  );
}

export function BrandingSection() {
  return (
    <Section
      title="11. Branding"
      description="Logo wordmark, icon at multiple sizes, and loader variants."
    >
      {/* Wordmark */}
      <DemoCard label="Wordmark (icon + text)">
        <div className="flex items-center gap-10 flex-wrap">
          <div className="flex flex-col items-center gap-2">
            <MemaxWordmark height={32} className="text-foreground" />
            <p className="text-[10px] text-fg-3">Light</p>
          </div>
          <div className="flex flex-col items-center gap-2">
            <div className="bg-neutral-900 rounded-xl px-5 py-3">
              <MemaxWordmark height={32} className="text-white" />
            </div>
            <p className="text-[10px] text-fg-3">Dark</p>
          </div>
        </div>
      </DemoCard>

      {/* ══════════════════════════════════════════════════════════════════
          Top-bar wordmark — canonical spec for BrandLogo in layout.tsx
          ══════════════════════════════════════════════════════════════════ */}
      <DemoCard label="Top-bar wordmark — size, opacity, alignment (mobile + desktop)">
        <p className="text-[12px] text-fg-2 mb-3">
          <span className="text-fg-1 font-medium">
            The wordmark is the stable brand anchor.
          </span>{" "}
          Next to the right-side hub capsule it was reading like a watermark —
          too small, too muted, hard to notice. The fix is{" "}
          <span className="font-medium">size + opacity</span>, not color. The
          ink stays neutral <span className="font-mono">var(--foreground)</span>{" "}
          so signature (violet) can keep its job as accent for hub avatars /
          dreams / active states without the wordmark stealing that vocabulary.
        </p>

        {/* ── Problem → Fix comparison ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Before → after (rendered against the real top-bar right capsule)
          </p>
          <div className="space-y-4">
            {/* BEFORE — current production */}
            <div>
              <div className="flex items-center justify-between gap-6 rounded-xl border border-border/40 bg-card px-4 py-3">
                <div className="flex h-12 items-center">
                  <MemaxWordmark
                    height={20}
                    className="text-foreground"
                    style={{ opacity: 0.45 }}
                  />
                </div>
                <MiniCapsule />
              </div>
              <p className="text-[10px] text-fg-4 font-mono mt-1.5">
                ✗ current — h-5 (20px mobile) / h-6 (24px desktop), opacity
                0.45, neutral foreground. Reads as a faded watermark next to a
                48px-tall capsule. Weight ratio ≈ 1:2.
              </p>
            </div>

            {/* AFTER — proposed */}
            <div>
              <div className="flex items-center justify-between gap-6 rounded-xl border border-border/40 bg-card px-4 py-3">
                <div className="flex h-12 items-center">
                  <MemaxWordmark
                    height={24}
                    className="text-foreground"
                    style={{ opacity: 0.9 }}
                  />
                </div>
                <MiniCapsule />
              </div>
              <p className="text-[10px] text-fg-4 font-mono mt-1.5">
                ✓ proposed — h-6 (24px, same mobile + desktop), opacity 0.9,
                var(--foreground). Bumping opacity 0.45 → 0.9 does the heavy
                lifting (2× readable weight); the mobile h-5 → h-6 nudge is a
                small follow-up so mobile matches desktop. Container h-12 so the
                vertical center lines up with the capsule.
              </p>
            </div>
          </div>
        </div>

        {/* ── Why neutral, not signature ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Why neutral, not signature
          </p>
          <div className="text-[11px] text-fg-2 space-y-1.5">
            <p>
              Signature (violet today) is memax&apos;s{" "}
              <span className="text-fg-1 font-medium">accent language</span>:
              hub avatar fill, dreams state, active tab indicator, recall star.
              Painting the wordmark with signature too would flatten that
              hierarchy — everything in the frame would be &ldquo;the violet
              thing&rdquo; and the accent stops carrying meaning.
            </p>
            <p>
              Linear, Notion, Vercel, Apple all keep their wordmarks neutral for
              exactly this reason. The brand anchor stays stable; accents move
              around it.
            </p>
            <p>
              <span className="font-mono">var(--foreground)</span> is already
              theme-aware at the only layer that matters:{" "}
              <span className="text-fg-1 font-medium">lightness</span>.
              Near-black in light mode, near-white in dark, with a subtle
              palette-specific gray tint (see the{" "}
              <span className="font-mono">grays</span> table in{" "}
              <span className="font-mono">_kitchen-context.tsx</span> — each
              palette adds ~0.005 chroma so the neutral drifts warm / cool /
              earth / dream without ever becoming chromatic). That is the
              theme-awareness the wordmark needs — no hue swap required.
            </p>
          </div>
        </div>

        {/* ── Wordmark vs hub ambience ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Wordmark vs hub ambience — two different layers
          </p>
          <div className="text-[11px] text-fg-2 space-y-1">
            <p>
              The top-bar <span className="font-mono">memax</span> wordmark is{" "}
              <span className="text-fg-1 font-medium">global product</span>{" "}
              brand. The hub header behind it (aurora / signature fill /
              time-of-day drift) is{" "}
              <span className="text-fg-1 font-medium">local hub ambience</span>.
              They are two different layers and neither should recolor the
              other.
            </p>
            <p>
              Personal hubs can drift through time-based aurora, team hubs can
              stay on signature, dreams mode can pulse purple — none of that
              touches the wordmark. The logo stays stable while the header mood
              changes behind it.
            </p>
            <p>
              Rule: if <span className="text-fg-1">product</span> branding
              changes, the wordmark can change. If{" "}
              <span className="text-fg-1">hub</span> ambience changes, the
              wordmark stays put.
            </p>
          </div>
        </div>

        {/* ── Size math ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Size — why h-6 (24px), not h-7 or h-5
          </p>
          <div className="text-[11px] text-fg-2 space-y-1.5">
            <p>
              <span className="font-mono">h-5</span> (20px, original mobile) was
              a genuine &ldquo;too small&rdquo; problem — at opacity 0.45 it
              read as a watermark, not a brand.
            </p>
            <p>
              <span className="font-mono">h-7</span> (28px) is{" "}
              <span className="text-fg-1 font-medium">too wide on mobile</span>.
              SVG wordmark aspect ratio is{" "}
              <span className="font-mono">4.65:1</span>, so h-7 → w-33 (132px).
              On a 375px viewport that&apos;s 35% of screen width competing with
              the capsule at the right edge — the top bar feels overstuffed.
            </p>
            <p>
              <span className="font-mono">h-6</span> (24px, w-30 = 120px) is the
              sweet spot. On mobile it&apos;s 32% of a 375px screen instead of
              35%, still leaves breathing room for the right capsule. On desktop
              it matches the current production value, so we ship one size for
              both — no <span className="font-mono">md:</span> breakpoint split.
              The wordmark is the stable brand anchor; making it different
              between breakpoints is inconsistency for no reason.
            </p>
            <p>
              The real &ldquo;feels present&rdquo; fix isn&apos;t size anyway —
              it&apos;s{" "}
              <span className="text-fg-1 font-medium">opacity 0.45 → 0.9</span>.
              That alone doubles the perceived weight; the size nudge is just
              making mobile match desktop.
            </p>
          </div>
        </div>

        {/* ── Vertical alignment ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Alignment — both containers at h-12, same top
          </p>
          <p className="text-[11px] text-fg-2">
            Today the wordmark container is{" "}
            <span className="font-mono">h-11</span> (44px) and the right capsule
            derives a ~46px height from its flex children. Close but not
            identical — and near-misses at 1–2px read as misalignment. Fix: give
            both containers{" "}
            <span className="font-mono">flex h-12 items-center</span> so they
            lock to the same vertical center at the same{" "}
            <span className="font-mono">top</span> value.
          </p>
        </div>

        {/* ── Implementation spec codex follows ── */}
        <div className="pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Implementation spec — use the @memaxlabs/ui component
          </p>
          <pre className="text-[10px] font-mono text-fg-2 bg-surface-1 rounded-md p-2.5 overflow-x-auto whitespace-pre">
            {`// packages/web/src/app/(app)/layout.tsx
import { MemaxWordmark } from "@memaxlabs/ui";

function BrandLogo({ isMobile }: { isMobile: boolean }) {
  return (
    <div
      className="fixed z-50 flex h-12 items-center"   // was h-11
      style={{
        left: isMobile ? 16 : 32,
        top: isMobile
          ? "max(20px, calc(8px + var(--safe-top, 0px)))"
          : "32px",
      }}
    >
      <a
        href="/home"
        className="cursor-pointer text-foreground opacity-90 transition-opacity hover:opacity-100"
      >
        <MemaxWordmark height={24} />
      </a>
    </div>
  );
}

// Right-capsule container on line ~143 — lock height to match:
<div
  className="fixed right-4 md:right-8 z-50 flex h-12 items-center gap-2 ..."
  //                                             ^^^^^ add h-12
  ...
>`}
          </pre>
          <p className="text-[10px] text-fg-4 mt-2">
            Switches from the mask-based{" "}
            <span className="font-mono">/images/memax-wordmark.svg</span> URL to
            the <span className="font-mono">MemaxWordmark</span> React component
            exported from <span className="font-mono">@memaxlabs/ui</span>. The
            component uses{" "}
            <span className="font-mono">fill=&quot;currentColor&quot;</span>, so{" "}
            <span className="font-mono">text-foreground opacity-90</span> on the
            parent controls both ink and strength. No static asset to maintain,
            no CSS mask plumbing, and the kitchen demo above renders the exact
            same primitive production will use.
          </p>
          <p className="text-[10px] text-fg-4 mt-2">
            Four edits total: container{" "}
            <span className="font-mono">h-11 → h-12</span>, wordmark rendering
            swap to <span className="font-mono">&lt;MemaxWordmark /&gt;</span>,{" "}
            <span className="font-mono">text-foreground opacity-90</span>{" "}
            (replaces the hardcoded 0.45 foreground mask), and{" "}
            <span className="font-mono">h-12</span> added to the right capsule
            so both sides share one vertical anchor.
          </p>
        </div>
      </DemoCard>

      {/* ══════════════════════════════════════════════════════════════════
          Wordmark over aura — legibility rule
          ══════════════════════════════════════════════════════════════════ */}
      <DemoCard label="Wordmark over aura — legibility rule (industry best practice)">
        <p className="text-[12px] text-fg-2 mb-3">
          Hub headers drift through <span className="font-mono">aurora</span>{" "}
          gradients (time-of-day, signature, dreams). When an aura sits behind
          the wordmark, the fixed{" "}
          <span className="font-mono">var(--foreground)</span> ink can fall into
          the gradient and become hard to read. This card answers the question:{" "}
          <span className="text-fg-1 font-medium">
            should the wordmark auto-adapt to the aura behind it?
          </span>
        </p>

        {/* ── Failure scenario ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            The failure — dark ink on a darker aura
          </p>
          <div
            className="flex h-16 items-center px-4 rounded-xl"
            style={{
              background:
                "radial-gradient(ellipse at 30% 50%, oklch(0.45 0.15 290), oklch(0.32 0.12 290))",
            }}
          >
            <MemaxWordmark
              height={24}
              className="text-foreground"
              style={{ opacity: 0.9 }}
            />
          </div>
          <p className="text-[10px] text-fg-4 mt-1.5">
            Light-theme foreground (near-black) at 0.9 on a saturated deep
            violet aura — legibility drops off a cliff. Dark-theme foreground
            (near-white) on a light peach aura has the same problem in reverse.
            We need a mechanism that doesn&apos;t require the wordmark to care
            what&apos;s behind it.
          </p>
        </div>

        {/* ── Four industry approaches ranked ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Four approaches the industry uses
          </p>
          <div className="space-y-3 text-[11px] text-fg-2">
            <div>
              <p className="text-fg-1 font-medium">
                ✓ A. Scrim backdrop (Apple, Notion, Vercel, macOS Control
                Center)
              </p>
              <p className="text-fg-3 mt-0.5">
                The nav / logo sits on its own translucent glass backdrop (
                <span className="font-mono">backdrop-blur-md</span> +{" "}
                <span className="font-mono">bg-background/52</span>), which
                absorbs whatever is behind it. The wordmark itself never changes
                color — legibility comes from the scrim, not the ink. Brand
                identity is preserved (memax is always foreground-neutral), and
                the scrim is visually consistent regardless of aura mood.
                Industry default.
              </p>
            </div>
            <div>
              <p className="text-fg-1 font-medium">
                ⚠️ B. mix-blend-mode: difference / luminosity (editorial, video
                heroes)
              </p>
              <p className="text-fg-3 mt-0.5">
                One CSS line auto-inverts the wordmark against whatever is
                behind it. Works well over grayscale or muted backgrounds.
                Against vivid gradients it goes psychedelic — the wordmark turns
                green/orange/cyan depending on which gradient stop is under each
                letter. Also kills brand intent: you lose &ldquo;memax is this
                color&rdquo;, you get &ldquo;memax is whatever difference-blend
                says&rdquo;. Wrong for a product brand that ships a distinctive
                signature feature elsewhere.
              </p>
            </div>
            <div>
              <p className="text-fg-1 font-medium">
                ⚠️ C. IntersectionObserver + section-aware class swap (Linear
                docs, Stripe)
              </p>
              <p className="text-fg-3 mt-0.5">
                Each section declares its{" "}
                <span className="font-mono">theme=&quot;light&quot;</span> or{" "}
                <span className="font-mono">theme=&quot;dark&quot;</span>, a
                scroll observer watches which section intersects the nav, and
                the wordmark container swaps a class. Full control,
                brand-preserving. But: stateful, JS-coupled, easy to break on
                dynamic content (e.g. dream aurora that drifts during a single
                session view). Overkill for a top-bar wordmark that lives above
                one kind of container.
              </p>
            </div>
            <div>
              <p className="text-fg-1 font-medium">
                ❌ D. Auto-sample background color at runtime
              </p>
              <p className="text-fg-3 mt-0.5">
                Canvas-sample the pixels behind the wordmark, compute luminance,
                flip ink. Never do this. Expensive, flaky on animated
                backgrounds, impossible to SSR, impossible to snapshot test.
                Listed only so we name it and reject it.
              </p>
            </div>
          </div>
        </div>

        {/* ── Recommended: scrim backdrop (reuse showBannerChrome) ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Recommended — extend showBannerChrome to the wordmark side
          </p>
          <p className="text-[11px] text-fg-2 mb-3">
            memax <span className="text-fg-1 font-medium">already has</span> the
            scrim mechanism for the right capsule:{" "}
            <span className="font-mono">showBannerChrome</span> is the boolean
            state that switches the right container between glass chrome and
            transparent. We apply the same treatment to the wordmark container —
            one source of truth, zero new state, both sides light up together
            when the banner is visible.
          </p>

          <div className="space-y-3">
            {/* Demo: chrome OFF — transparent, neutral bg */}
            <div>
              <div className="flex items-center justify-between rounded-xl border border-dashed border-border/40 bg-background px-4 py-3">
                <div className="flex h-12 items-center text-foreground opacity-90">
                  <MemaxWordmark height={24} />
                </div>
                <MiniCapsule />
              </div>
              <p className="text-[10px] text-fg-4 font-mono mt-1.5">
                showBannerChrome = false — plain background, both sides
                transparent. Wordmark reads directly against{" "}
                <span className="font-mono">var(--background)</span>.
              </p>
            </div>

            {/* Demo: chrome ON — scrim over aura, both sides chromed */}
            <div>
              <div
                className="flex items-center justify-between rounded-xl border border-border/40 px-4 py-3"
                style={{
                  background:
                    "radial-gradient(ellipse at 20% 30%, oklch(0.55 0.15 290), oklch(0.68 0.10 60) 60%, oklch(0.78 0.06 30))",
                }}
              >
                <div className="flex h-12 items-center rounded-2xl border border-white/10 bg-background/52 px-3 py-1.5 backdrop-blur-md ring-1 ring-black/5">
                  <div className="text-foreground opacity-90">
                    <MemaxWordmark height={24} />
                  </div>
                </div>
                <MiniCapsule />
              </div>
              <p className="text-[10px] text-fg-4 font-mono mt-1.5">
                showBannerChrome = true — both sides wear the same glass scrim (
                <span className="font-mono">bg-background/52</span> +{" "}
                <span className="font-mono">backdrop-blur-md</span> +{" "}
                <span className="font-mono">ring-1 ring-black/5</span>).
                Wordmark ink stays neutral; legibility comes from the scrim.
                Brand anchor never wiggles.
              </p>
            </div>
          </div>
        </div>

        {/* ── Why this beats auto-adaptive schemes ── */}
        <div className="mb-4 pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Why scrim beats auto-adaptive color
          </p>
          <div className="text-[11px] text-fg-2 space-y-1.5">
            <p>
              <span className="text-fg-1 font-medium">
                Legibility without identity loss.
              </span>{" "}
              The wordmark never changes color, so the brand stays recognizable.
              Users learn &ldquo;memax looks like this&rdquo; once and it holds
              in every surface.
            </p>
            <p>
              <span className="text-fg-1 font-medium">Stateless.</span> No
              canvas sampling, no scroll listeners, no viewport math. CSS alone.
              SSR-friendly, snapshot-testable, cacheable.
            </p>
            <p>
              <span className="text-fg-1 font-medium">Symmetry.</span> The
              right-side hub capsule already flips on{" "}
              <span className="font-mono">showBannerChrome</span>. Making the
              wordmark do the same thing ties the two sides into one visual beat
              instead of two independent rules.
            </p>
            <p>
              <span className="text-fg-1 font-medium">
                Future-proof for dreams mode.
              </span>{" "}
              If dreams ever ships an aurora that saturates more deeply, the
              scrim absorbs it automatically — the darker the aura, the more the
              scrim carries. No per-aura color tuning.
            </p>
          </div>
        </div>

        {/* ── Implementation spec — scrim edit ── */}
        <div className="pt-3 border-t border-border/20">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
            Implementation — wordmark container mirrors the right capsule
          </p>
          <pre className="text-[10px] font-mono text-fg-2 bg-surface-1 rounded-md p-2.5 overflow-x-auto whitespace-pre">
            {`// packages/web/src/app/(app)/layout.tsx

function BrandLogo({
  isMobile,
  showBannerChrome,              // NEW — accept the same flag the right capsule uses
}: {
  isMobile: boolean;
  showBannerChrome: boolean;
}) {
  return (
    <div
      className={\`fixed z-50 flex h-12 items-center rounded-2xl px-3 py-1.5 transition-all \${
        showBannerChrome
          ? "border border-white/10 bg-background/52 backdrop-blur-md ring-1 ring-black/5"
          : "border border-transparent bg-transparent backdrop-blur-0 ring-0"
      }\`}
      style={{
        left: isMobile ? 16 : 32,
        top: isMobile
          ? "max(20px, calc(8px + var(--safe-top, 0px)))"
          : "32px",
      }}
    >
      <a
        href="/home"
        className="cursor-pointer text-foreground opacity-90 transition-opacity hover:opacity-100"
      >
        <MemaxWordmark height={24} />
      </a>
    </div>
  );
}

// Layout already computes showBannerChrome for the right capsule — pass the
// same value into <BrandLogo /> so both sides light up together.`}
          </pre>
          <p className="text-[10px] text-fg-4 mt-2">
            Single state, two consumers. The wordmark container now wears the
            identical glass chrome as the right capsule, gated on the same{" "}
            <span className="font-mono">showBannerChrome</span> flag. When the
            banner is absent both sides are invisible chrome; when it is present
            both sides wear a matching scrim. The wordmark ink (
            <span className="font-mono">text-foreground opacity-90</span>) never
            changes.
          </p>
        </div>
      </DemoCard>

      {/* Icon only */}
      <DemoCard label="Icon only">
        <div className="flex items-center gap-10 flex-wrap">
          {/* 48px light */}
          <div className="flex flex-col items-center gap-2">
            <svg
              width={48}
              height={48}
              viewBox="0 0 128 128"
              fill="currentColor"
              stroke="currentColor"
              className="text-foreground"
              style={{
                fillRule: "evenodd",
                strokeLinecap: "round",
                strokeLinejoin: "round",
              }}
            >
              <path
                d={LOGO_PATH}
                style={{ strokeWidth: 0.97 }}
                transform="translate(-691.755 -1503.419) scale(1.03547)"
              />
              <path
                d={LOGO_PATH}
                style={{ strokeWidth: 0.97 }}
                transform="translate(819.755 1631.418) scale(-1.03547)"
              />
            </svg>
            <p className="text-[10px] text-fg-3">48px</p>
          </div>

          {/* 32px */}
          <div className="flex flex-col items-center gap-2">
            <svg
              width={32}
              height={32}
              viewBox="0 0 128 128"
              fill="currentColor"
              stroke="currentColor"
              className="text-foreground"
              style={{
                fillRule: "evenodd",
                strokeLinecap: "round",
                strokeLinejoin: "round",
              }}
            >
              <path
                d={LOGO_PATH}
                style={{ strokeWidth: 0.97 }}
                transform="translate(-691.755 -1503.419) scale(1.03547)"
              />
              <path
                d={LOGO_PATH}
                style={{ strokeWidth: 0.97 }}
                transform="translate(819.755 1631.418) scale(-1.03547)"
              />
            </svg>
            <p className="text-[10px] text-fg-3">32px (favicon)</p>
          </div>

          {/* 20px */}
          <div className="flex flex-col items-center gap-2">
            <svg
              width={20}
              height={20}
              viewBox="0 0 128 128"
              fill="currentColor"
              stroke="currentColor"
              className="text-foreground"
              style={{
                fillRule: "evenodd",
                strokeLinecap: "round",
                strokeLinejoin: "round",
              }}
            >
              <path
                d={LOGO_PATH}
                style={{ strokeWidth: 0.97 }}
                transform="translate(-691.755 -1503.419) scale(1.03547)"
              />
              <path
                d={LOGO_PATH}
                style={{ strokeWidth: 0.97 }}
                transform="translate(819.755 1631.418) scale(-1.03547)"
              />
            </svg>
            <p className="text-[10px] text-fg-3">20px (nav)</p>
          </div>

          {/* 48px dark */}
          <div className="flex flex-col items-center gap-2">
            <div className="bg-neutral-900 rounded-xl p-3">
              <svg
                width={48}
                height={48}
                viewBox="0 0 128 128"
                fill="white"
                stroke="white"
                style={{
                  fillRule: "evenodd",
                  strokeLinecap: "round",
                  strokeLinejoin: "round",
                }}
              >
                <path
                  d={LOGO_PATH}
                  style={{ strokeWidth: 0.97 }}
                  transform="translate(-691.755 -1503.419) scale(1.03547)"
                />
                <path
                  d={LOGO_PATH}
                  style={{ strokeWidth: 0.97 }}
                  transform="translate(819.755 1631.418) scale(-1.03547)"
                />
              </svg>
            </div>
            <p className="text-[10px] text-fg-3">48px dark</p>
          </div>
        </div>
      </DemoCard>

      {/* MemaxLoader */}
      <DemoCard label="MemaxLoader">
        <div className="flex items-end gap-8 flex-wrap">
          <div className="flex flex-col items-center gap-2">
            <MemaxLoader inline />
            <p className="text-[10px] text-fg-3">inline</p>
          </div>
          <div className="flex flex-col items-center gap-2">
            <MemaxLoader inline compact />
            <p className="text-[10px] text-fg-3">compact</p>
          </div>
          <div className="flex flex-col items-center gap-2">
            <MemaxLoader inline label="Loading..." />
            <p className="text-[10px] text-fg-3">with label</p>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          3 dots in signature color, sequential pulse. Pure CSS — no
          requestAnimationFrame.
        </p>
      </DemoCard>
    </Section>
  );
}
