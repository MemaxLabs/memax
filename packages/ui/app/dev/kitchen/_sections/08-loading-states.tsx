// Maps to: ui/content-skeleton.tsx, ui/loading-page.tsx, all routes
// ui/ components: MemaxLoader, Skeleton
// Transition: animate-content-ready (globals.css) — 0.15s opacity+lift
"use client";

import { useEffect, useState } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import { MemaxLoader } from "@/components/memax-loader";
/* ── Approved page loading redesign ──
 *
 * Single breathing orb — signature dot that pulses with glow halo.
 * Arrival: orb flares outward then fades, content enters.
 */

const LOADER_KEYFRAMES = `
@keyframes memax-orb-breathe {
  0%, 100% {
    transform: scale(0.85);
    opacity: 0.4;
    box-shadow: 0 0 0px 0px oklch(from var(--signature) l c h / 0);
  }
  50% {
    transform: scale(1);
    opacity: 1;
    box-shadow: 0 0 28px 8px oklch(from var(--signature) l c h / 0.15);
  }
}
@keyframes memax-orb-flare {
  0% {
    transform: scale(1);
    opacity: 1;
    box-shadow: 0 0 28px 8px oklch(from var(--signature) l c h / 0.15);
  }
  30% {
    transform: scale(2.8);
    opacity: 1;
    box-shadow: 0 0 48px 16px oklch(from var(--signature) l c h / 0.3);
  }
  100% {
    transform: scale(0.5);
    opacity: 0;
    box-shadow: 0 0 0px 0px oklch(from var(--signature) l c h / 0);
  }
}`;

/** Breathing orb — the approved loader */
function BreathingOrb({ size = 10, label }: { size?: number; label?: string }) {
  return (
    <div className="flex flex-col items-center gap-4">
      <style dangerouslySetInnerHTML={{ __html: LOADER_KEYFRAMES }} />
      <div
        className="rounded-full"
        style={{
          width: size,
          height: size,
          background: "var(--signature)",
          animation: "memax-orb-breathe 2.4s ease-in-out infinite",
          willChange: "transform, opacity, box-shadow",
        }}
      />
      {label && <span className="text-[14px] text-fg-3">{label}</span>}
    </div>
  );
}

/** Orb breathes → flares → content */
function OrbArrivalDemo() {
  const [phase, setPhase] = useState<"breathing" | "flare" | "content">(
    "breathing",
  );
  const [key, setKey] = useState(0);

  const replay = () => {
    setPhase("breathing");
    setKey((k) => k + 1);
  };

  useEffect(() => {
    if (phase !== "breathing") return;
    const t = setTimeout(() => setPhase("flare"), 2200);
    return () => clearTimeout(t);
  }, [phase, key]);

  useEffect(() => {
    if (phase !== "flare") return;
    const t = setTimeout(() => setPhase("content"), 500);
    return () => clearTimeout(t);
  }, [phase]);

  return (
    <div className="space-y-3">
      <button
        onClick={replay}
        className="cursor-pointer text-[12px] text-fg-3 transition-colors hover:text-fg-2"
      >
        ↻ Replay transition
      </button>
      <div
        className="relative flex items-center justify-center overflow-hidden rounded-lg py-16"
        style={{ background: "var(--background)" }}
      >
        <style dangerouslySetInnerHTML={{ __html: LOADER_KEYFRAMES }} />

        {phase === "content" ? (
          <div
            key={`content-${key}`}
            className="animate-content-ready space-y-2 text-center"
          >
            <p className="text-[14px] font-medium text-fg-1">
              Your memories are ready
            </p>
            <p className="text-[12px] text-fg-3">
              3 topics &middot; 48 memories &middot; last synced 2m ago
            </p>
          </div>
        ) : phase === "flare" ? (
          <div
            key={`flare-${key}`}
            className="rounded-full"
            style={{
              width: 10,
              height: 10,
              background: "var(--signature)",
              animation:
                "memax-orb-flare 0.5s cubic-bezier(0.16, 1, 0.3, 1) forwards",
              willChange: "transform, opacity, box-shadow",
            }}
          />
        ) : (
          <div key={`orb-${key}`}>
            <BreathingOrb size={10} />
          </div>
        )}
      </div>
    </div>
  );
}

function Shimmer({ className = "" }: { className?: string }) {
  return (
    <div
      className={`rounded-md animate-pulse bg-foreground/[0.07] ${className}`}
    />
  );
}

function TransitionDemo() {
  const [showContent, setShowContent] = useState(false);
  const [key, setKey] = useState(0);

  return (
    <div className="space-y-3">
      <button
        onClick={() => {
          if (showContent) {
            setShowContent(false);
            setTimeout(() => {
              setKey((k) => k + 1);
              setShowContent(true);
            }, 300);
          } else {
            setShowContent(true);
          }
        }}
        className="text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors"
      >
        {showContent ? "↻ Replay transition" : "▶ Show content"}
      </button>
      <div className="rounded-xl border border-border/60 overflow-hidden">
        {!showContent ? (
          <div className="p-4 space-y-3">
            <div className="flex items-center gap-2">
              <Shimmer className="h-1.5 w-1.5 rounded-full! shrink-0" />
              <Shimmer className="h-3 w-3/5" />
            </div>
            <Shimmer className="h-2.5 w-full" />
            <Shimmer className="h-2.5 w-3/4" />
          </div>
        ) : (
          <div key={key} className="p-4 space-y-2 animate-content-ready">
            <div className="flex items-center gap-2">
              <div
                className="h-1.5 w-1.5 rounded-full shrink-0"
                style={{ backgroundColor: "#3b82f6" }}
              />
              <span className="text-[14px] text-fg-1 font-semibold">
                React Server Components
              </span>
            </div>
            <p className="text-[13px] text-fg-2">
              Server components render on the server and send HTML to the
              client. Client components hydrate and become interactive.
            </p>
            <p className="text-[12px] text-fg-3">code &middot; 2h ago</p>
          </div>
        )}
      </div>
    </div>
  );
}

function SilentRefetchDemo() {
  return (
    <div className="space-y-3">
      <div
        className="rounded-xl overflow-hidden"
        style={{
          border: "1px solid var(--border)",
          background: "var(--card)",
        }}
      >
        <div className="flex items-center gap-2 px-4 pt-3 pb-2">
          <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
            Recent
          </span>
          <div className="flex-1" />
          <span className="text-[12px] text-fg-3">Past 12h</span>
        </div>
        <div className="px-4 py-2.5 border-t border-border/30">
          <div className="mb-0.5 flex items-center gap-1.5 text-[12px]">
            <span className="text-foreground/40">claude-code captured</span>
            <span className="ml-auto text-foreground/40 tabular-nums">14m</span>
          </div>
          <div className="text-[14px] font-medium text-foreground truncate">
            Fly.io blue-green with health checks
          </div>
        </div>
        <div className="px-4 py-2.5 border-t border-border/30">
          <div className="mb-0.5 flex items-center gap-1.5 text-[12px]">
            <span className="text-foreground/40">cursor captured</span>
            <span className="ml-auto text-foreground/40 tabular-nums">1h</span>
          </div>
          <div className="text-[14px] font-medium text-foreground truncate">
            OAuth refresh token rotation strategy
          </div>
        </div>
      </div>
      <p className="text-[10px] text-fg-4">
        A mounted card that just got invalidated looks <em>exactly</em> like
        this. No pulse, no star, no spinner, no dim. Data on screen is already
        accurate — background re-sync should not interrupt the user. For the
        case where data IS about to change (hub / filter pivot), see section
        35d. For the case where a NEW row arrives via SSE, see section 35e — the
        row animates in, the card chrome stays still.
      </p>
    </div>
  );
}

export function LoadingStatesSection() {
  return (
    <Section
      title="8. Loading States"
      description="Shape before content. Signature color only when memax is actively working. For composed card-level state machines see section 35 (Data Section Card)."
    >
      {/* ── APPROVED: Breathing orb + flare arrival ── */}

      <DemoCard label="08-new. Breathing Orb — page loader">
        <div className="grid grid-cols-3 gap-4">
          <div
            className="flex flex-col items-center justify-center gap-3 rounded-lg py-14"
            style={{ background: "var(--background)" }}
          >
            <BreathingOrb size={10} />
            <span className="text-[10px] text-fg-4">10px full-screen</span>
          </div>
          <div
            className="flex flex-col items-center justify-center gap-3 rounded-lg py-14"
            style={{ background: "var(--background)" }}
          >
            <BreathingOrb size={6} />
            <span className="text-[10px] text-fg-4">6px compact</span>
          </div>
          <div
            className="flex flex-col items-center justify-center gap-3 rounded-lg py-14"
            style={{ background: "#0f0f12" }}
          >
            <BreathingOrb size={10} />
            <span
              className="text-[10px]"
              style={{ color: "oklch(from #e8e8ec l c h / 0.2)" }}
            >
              dark (glow shines)
            </span>
          </div>
        </div>
        <p className="mt-2 text-[10px] text-fg-4">
          Single signature dot that breathes — scale 0.85→1.0, opacity 0.4→1.0,
          soft glow halo (2.4s ease-in-out). Replaces the three-dot pulse.
        </p>
      </DemoCard>

      <DemoCard label="08-new-b. With label — auth / invite pages">
        <div
          className="flex items-center justify-center rounded-lg py-12"
          style={{ background: "var(--background)" }}
        >
          <BreathingOrb size={10} label="Signing you in..." />
        </div>
      </DemoCard>

      <DemoCard label="08-new-c. Arrival — breathe → flare → content">
        <OrbArrivalDemo />
        <p className="mt-2 text-[10px] text-fg-4">
          Orb breathes → data arrives → dot flares outward (scale 2.8×, glow
          burst, 0.5s spring) → fades → content enters. Strong sense of
          &quot;arriving.&quot;
        </p>
      </DemoCard>

      {/* ── CURRENT: Original dots (kept for reference) ── */}

      {/* ── Full page: app cold start ── */}
      <DemoCard label="Full page — cold start (current)">
        <div
          className="flex items-center justify-center py-16 rounded-lg"
          style={{ background: "var(--background)" }}
        >
          <div className="flex flex-col items-center gap-4">
            <MemaxLoader inline />
            <p className="text-[14px] text-fg-3">Loading your memory...</p>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          {/* t.loading.fullPage */}
          MemaxLoader (signature-colored sequential pulse dots) is the brand.
          Centered on background, not card. Text at /40. Used on brain view
          during initial load.
        </p>
      </DemoCard>

      {/* ── Full page: route transition ── */}
      <DemoCard label="Full page — route transition">
        <div
          className="flex items-center justify-center py-8 rounded-lg"
          style={{ background: "var(--background)" }}
        >
          <MemaxLoader inline compact />
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Brief transition. Compact loader, no text — user already knows where
          they&apos;re going.
        </p>
      </DemoCard>

      {/* ── AI streaming: breathing star + summary callout ── */}
      <DemoCard label="AI streaming — breathing ✦ + summary callout">
        <div className="space-y-3">
          <div className="bg-surface-1 rounded-xl p-4 flex gap-2.5 animate-content-ready">
            <span
              className="text-[12px] shrink-0 mt-0.5 state-slow-breathe"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            <div className="text-[15px] text-fg-2 leading-[1.65]">
              Your deployment strategy combines blue-green deployment with
              canary releases [1]. For risk mitigation [2], you use gradual
              rollouts...
            </div>
          </div>
          <div className="bg-surface-1 rounded-xl p-4 flex gap-2.5">
            <span
              className="text-[12px] shrink-0 mt-0.5"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            <div className="text-[15px] text-fg-2 leading-[1.65]">
              Complete answer with static star — streaming done.
            </div>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          AI answer uses summary callout pattern (bg-surface-1 rounded-xl).
          Entry: animate-content-ready. ✦ breathes (state-slow-breathe) while
          streaming, static when complete. Text at /75.
        </p>
      </DemoCard>

      {/* ── Content area: skeleton grid ── */}
      <DemoCard label="Content area — skeleton grid">
        <div className="grid grid-cols-2 gap-4">
          {[0, 1, 2, 3].map((i) => (
            <div
              key={i}
              className="rounded-xl border border-border p-4 space-y-3"
              style={{
                background: "var(--card)",
                animationDelay: `${i * 120}ms`,
              }}
            >
              <div className="flex items-center gap-2">
                <Shimmer className="h-1.5 w-1.5 !rounded-full shrink-0" />
                <Shimmer className="h-3 w-3/5" />
              </div>
              <Shimmer className="h-2.5 w-full" />
              <Shimmer className="h-2.5 w-3/4" />
              <div className="flex items-center gap-2 pt-1">
                <Shimmer className="h-2 w-1/4" />
                <Shimmer className="h-2 w-1/6" />
              </div>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          {/* t.loading.skeleton */}
          Skeleton mirrors loaded card: dot+title, body, meta. Shimmer uses
          foreground opacity, not gray — adapts to dark mode.
        </p>
      </DemoCard>

      {/* ── Recall loading — send button spinner ── */}
      <DemoCard label="Recall loading — send button spinner + AI cycling text">
        <p className="text-[10px] text-fg-4 mb-3">
          Recall loading: send button shows spinner (no notification banner). AI
          synthesis loading: ✦ breathing + RecallingText variant=&quot;ai&quot;
          crossfade below results (&quot;memax is thinking&quot; → &quot;reading
          your memories&quot; → &quot;connecting the dots&quot;...).
        </p>
        <div className="flex items-center gap-2 px-5 py-2">
          <span
            className="text-[13px] leading-none state-slow-breathe shrink-0"
            style={{ color: SIGNATURE }}
          >
            ✦
          </span>
          <span className="text-[13px] text-fg-3">memax is thinking</span>
        </div>
      </DemoCard>

      {/* ── Transition: skeleton → content ── */}
      <DemoCard label="Transition — skeleton → content (animate-content-ready)">
        <p className="text-[12px] text-fg-2 mb-3">
          0.15s ease-out, opacity 0→1 + translateY 6px→0. Applied globally to
          all loading→loaded transitions. Click to replay.
        </p>
        <TransitionDemo />
      </DemoCard>

      {/* ── Silent refetch — the invisible state ── */}
      <DemoCard label="08h. Background refetch — silent by design">
        <p className="text-[12px] text-fg-2 mb-3">
          When a mounted data card refetches (stale-while-revalidate,
          post-mutation invalidation, focus refetch), memax shows{" "}
          <strong className="text-fg-1">no visual signal at all</strong>. The
          rows the user sees are already accurate, and a pulsing indicator would
          only distract. This is a deliberate rule. For the full composed state
          machine and the cache pivot / memory arrival cases see section 35.
        </p>
        <SilentRefetchDemo />
      </DemoCard>

      {/* ── Inline: processing card ── */}
      <DemoCard label="Inline — card processing indicator">
        <div className="border border-border rounded-xl p-4 space-y-2">
          <div className="flex items-center gap-2">
            <span
              className="text-[10px] leading-none state-fast-pulse shrink-0"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            <span className="text-[14px] text-fg-1 font-medium">
              Docker multi-stage builds
            </span>
          </div>
          <p className="text-[13px] text-fg-2 line-clamp-2">
            Use multi-stage builds to keep production images small. Separate
            build dependencies from runtime...
          </p>
          <div className="flex items-center gap-2">
            <span className="text-[12px] text-fg-3">process</span>
            <span className="text-[12px] text-fg-4">&middot;</span>
            <span
              className="text-[12px] state-slow-breathe"
              style={{ color: SIGNATURE, opacity: 0.6 }}
            >
              organizing...
            </span>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          {/* t.memories.processing */}
          Card appears immediately with content. ✦ replaces the neutral
          indicator during processing. &quot;organizing...&quot; breathes in
          signature color.
        </p>
      </DemoCard>
    </Section>
  );
}
