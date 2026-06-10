// Maps to: globals.css keyframes + motion tokens, lib/motion.ts
// Source of truth for all animation timing, easing, and behavior
"use client";

import { useState } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";

/* ── Duration Tokens ── */

const DURATIONS = [
  {
    token: "--duration-instant",
    value: "0.1s",
    js: "INSTANT",
    desc: "UI state changes — intent label swap, right slot toggle, hover feedback",
    example: "opacity: 0 → 1 on hover",
  },
  {
    token: "--duration-fast",
    value: "0.15s",
    js: "FAST",
    desc: "Modal open/close, expand slot, view switch, dropdown appear",
    example: "scale: 0.96 → 1 on mount",
  },
  {
    token: "--duration-normal",
    value: "0.2s",
    js: "NORMAL",
    desc: "Content dissolve, list reorder, layout shift, page transition",
    example: "translateY: 6px → 0 on enter",
  },
  {
    token: "--duration-loading",
    value: "0.5s",
    js: "LOADING",
    desc: "Breathing dots, loading pulses, skeleton shimmer",
    example: "opacity: 0.3 → 1 loop",
  },
  {
    token: "--duration-signature",
    value: "3.5s",
    js: "SIGNATURE",
    desc: "Ghost breathing, slow atmospheric loops",
    example: "breathing ✦ states, idle animations",
  },
];

/* ── Easing Curves ── */

const EASINGS = [
  {
    name: "Spring",
    token: "--ease-spring",
    value: "cubic-bezier(0.16, 1, 0.3, 1)",
    desc: "Default for all UI transitions. Fast start, gentle settle. Apple-like feel.",
    useFor: "Everything unless specified otherwise",
  },
  {
    name: "Bounce",
    token: "--ease-bounce",
    value: "cubic-bezier(0.34, 1.56, 0.64, 1)",
    desc: "Slight overshoot then settle. Playful, attention-drawing.",
    useFor: "Toast enter, success indicator, notification badge",
  },
];

/* ── Named Animations ── */

const ANIMATIONS = [
  {
    name: "content-ready",
    tailwind: "animate-content-ready",
    desc: "Skeleton → content transition. Opacity 0→1 + translateY 6px→0.",
    duration: "0.15s",
    usage: "All loading → loaded state changes globally",
  },
  {
    name: "slow-breathe",
    tailwind: "state-slow-breathe",
    desc: "Gentle opacity pulse 0.3→1. For AI processing indicators.",
    duration: "2.5s infinite",
    usage: '✦ star during AI streaming, "organizing..." text',
  },
  {
    name: "fast-pulse",
    tailwind: "state-fast-pulse",
    desc: "Quick opacity pulse. For active processing feedback.",
    duration: "1s infinite",
    usage: "Kind dot during processing, inline loading",
  },
  {
    name: "fade-up",
    tailwind: "animate-fade-up",
    desc: "Entrance: opacity 0→1 + translateY 8px→0.",
    duration: "0.3s",
    usage: "Section entrance, staggered list items",
  },
  {
    name: "memax-loader",
    tailwind: "MemaxLoader",
    desc: "Sequential pulse dots in signature color. Brand loading.",
    duration: "per-dot stagger",
    usage: "Full-page cold start, route transition",
  },
];

/* ── Status Messages ── */

const STATUS_MESSAGES = [
  "listening",
  "organizing",
  "dreaming",
  "connecting dots",
  "thinking",
];

/* ── Interactive Demo ── */

function TimingDemo() {
  const [playing, setPlaying] = useState(false);
  const [key, setKey] = useState(0);

  const play = () => {
    setPlaying(false);
    setTimeout(() => {
      setKey((k) => k + 1);
      setPlaying(true);
    }, 50);
  };

  return (
    <div className="space-y-3">
      <button
        onClick={play}
        className="text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors"
      >
        {playing ? "↻ Replay" : "▶ Play all durations"}
      </button>
      <div className="space-y-2">
        {DURATIONS.map((d, i) => (
          <div key={d.token} className="flex items-center gap-3">
            <code className="text-[10px] text-fg-3 font-mono w-16 shrink-0 text-right">
              {d.value}
            </code>
            <div className="flex-1 h-6 bg-surface-1 rounded-md overflow-hidden relative">
              {playing && (
                <div
                  key={key + "-" + i}
                  className="absolute inset-y-0 left-0 rounded-md"
                  style={{
                    background: SIGNATURE,
                    opacity: 0.6,
                    animation: `timing-bar ${d.value} var(--ease-spring) forwards`,
                  }}
                />
              )}
            </div>
          </div>
        ))}
      </div>
      <style>{`
        @keyframes timing-bar {
          from { width: 0%; }
          to { width: 100%; }
        }
      `}</style>
    </div>
  );
}

/* ── Easing Comparison Demo ── */

function EasingDemo() {
  const [playing, setPlaying] = useState(false);
  const [key, setKey] = useState(0);

  const play = () => {
    setPlaying(false);
    setTimeout(() => {
      setKey((k) => k + 1);
      setPlaying(true);
    }, 50);
  };

  return (
    <div className="space-y-3">
      <button
        onClick={play}
        className="text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors"
      >
        {playing ? "↻ Replay" : "▶ Compare easings"}
      </button>
      <div className="space-y-2">
        {[
          {
            label: "Spring (default)",
            easing: "cubic-bezier(0.16, 1, 0.3, 1)",
          },
          { label: "Bounce", easing: "cubic-bezier(0.34, 1.56, 0.64, 1)" },
          { label: "Linear (never use)", easing: "linear" },
          { label: "ease-in-out (never use)", easing: "ease-in-out" },
        ].map((e, i) => (
          <div key={e.label} className="flex items-center gap-3">
            <span className="text-[10px] text-fg-3 w-32 shrink-0 text-right">
              {e.label}
            </span>
            <div className="flex-1 h-6 bg-surface-1 rounded-md relative overflow-hidden">
              {playing && (
                <div
                  key={key + "-e-" + i}
                  className="absolute top-1 bottom-1 left-1 w-4 rounded-sm"
                  style={{
                    background: i < 2 ? SIGNATURE : "var(--destructive)",
                    opacity: i < 2 ? 0.8 : 0.4,
                    animation: `easing-slide 0.6s ${e.easing} forwards`,
                  }}
                />
              )}
            </div>
          </div>
        ))}
      </div>
      <style>{`
        @keyframes easing-slide {
          from { transform: translateX(0); }
          to { transform: translateX(calc(100% * 12)); }
        }
      `}</style>
    </div>
  );
}

export function MotionTokensSection() {
  return (
    <Section
      title="18. Motion Tokens"
      description="Named timing constants + easing curves. Every animation references a token — never raw durations. Spring easing is the default."
    >
      {/* ── Duration Tokens ── */}
      <DemoCard label="Duration tokens">
        <div className="space-y-0">
          {DURATIONS.map((d) => (
            <div
              key={d.token}
              className="flex items-start gap-3 py-2.5 border-b border-border/10 last:border-0"
            >
              <div className="w-20 shrink-0">
                <code className="text-[11px] font-mono text-fg-2 font-medium">
                  {d.js}
                </code>
                <p className="text-[10px] font-mono text-fg-4">{d.value}</p>
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-[12px] text-fg-2">{d.desc}</p>
                <code className="text-[10px] text-fg-4 font-mono">
                  {d.example}
                </code>
              </div>
            </div>
          ))}
        </div>
        <div className="mt-3 pt-3 border-t border-border/20">
          <p className="text-[10px] text-fg-4 font-mono">
            CSS: var(--duration-*) · JS: import{" "}
            {"{ INSTANT, FAST, NORMAL, LOADING, SIGNATURE }"} from
            &quot;@/lib/motion&quot;
          </p>
        </div>
      </DemoCard>

      {/* ── Interactive Timing Demo ── */}
      <DemoCard label="Duration comparison — interactive">
        <TimingDemo />
      </DemoCard>

      {/* ── Easing Curves ── */}
      <DemoCard label="Easing curves">
        <div className="space-y-4">
          {EASINGS.map((e) => (
            <div key={e.name} className="flex items-start gap-3">
              <div className="w-20 shrink-0">
                <span className="text-[13px] font-medium text-fg-2">
                  {e.name}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <code className="text-[10px] font-mono text-fg-3 block mb-0.5">
                  {e.value}
                </code>
                <p className="text-[12px] text-fg-2">{e.desc}</p>
                <p className="text-[10px] text-fg-4">Use for: {e.useFor}</p>
              </div>
            </div>
          ))}
        </div>
        <div className="bg-surface-1 rounded-lg p-2.5 mt-3">
          <p className="text-[11px] text-fg-3">
            <span className="font-medium text-fg-2">Rule:</span> never use{" "}
            <code className="font-mono">linear</code> or{" "}
            <code className="font-mono">ease-in-out</code>. Spring easing is the
            memax feel. Set it once in Framer Motion{" "}
            <code className="font-mono">transition</code> or CSS{" "}
            <code className="font-mono">transition-timing-function</code>.
          </p>
        </div>
      </DemoCard>

      {/* ── Interactive Easing Demo ── */}
      <DemoCard label="Easing comparison — interactive">
        <EasingDemo />
        <p className="text-[10px] text-fg-4 mt-2">
          Red = forbidden easings. Notice how linear feels robotic and
          ease-in-out feels sluggish compared to spring.
        </p>
      </DemoCard>

      {/* ── Named Animations ── */}
      <DemoCard label="Named animations (globals.css keyframes)">
        <div className="space-y-0">
          {ANIMATIONS.map((a) => (
            <div
              key={a.name}
              className="flex items-start gap-3 py-2.5 border-b border-border/10 last:border-0"
            >
              <code className="text-[11px] font-mono text-fg-2 w-28 shrink-0">
                {a.tailwind}
              </code>
              <div className="flex-1 min-w-0">
                <p className="text-[12px] text-fg-2">{a.desc}</p>
                <div className="flex items-center gap-2 mt-0.5">
                  <code className="text-[10px] text-fg-4 font-mono">
                    {a.duration}
                  </code>
                  <span className="text-[10px] text-fg-4">— {a.usage}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Live Animation Demos ── */}
      <DemoCard label="Live — breathing indicators">
        <div className="flex items-center gap-8">
          <div className="flex flex-col items-center gap-1.5">
            <span
              className="text-[20px] leading-none state-slow-breathe"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            <span className="text-[10px] text-fg-4">slow-breathe</span>
          </div>
          <div className="flex flex-col items-center gap-1.5">
            <span
              className="text-[20px] leading-none state-fast-pulse"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            <span className="text-[10px] text-fg-4">fast-pulse</span>
          </div>
          <div className="flex flex-col items-center gap-1.5">
            <span
              className="text-[20px] leading-none"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            <span className="text-[10px] text-fg-4">static</span>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          slow-breathe: AI streaming/processing. fast-pulse: active card
          processing. static: complete/idle.
        </p>
      </DemoCard>

      {/* ── Status Messages ── */}
      <DemoCard label="Status messages (RecallingText)">
        <div className="flex flex-wrap gap-2">
          {STATUS_MESSAGES.map((msg) => (
            <div
              key={msg}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-surface-1 border border-border/40"
            >
              <span className="text-[13px] text-fg-3">memax </span>
              <span
                className="text-[13px] font-medium state-slow-breathe"
                style={{ color: SIGNATURE }}
              >
                {msg}
              </span>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-fg-4 mt-2">
          Crossfade cycle during AI loading. Source: RecallingText component.
          verb breathes in signature color.
        </p>
      </DemoCard>

      {/* ── Rules ── */}
      <DemoCard label="Motion rules">
        <div className="space-y-2 text-[12px]">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0">1.</span>
            <p className="text-fg-2">
              <span className="font-medium">Never use raw durations</span> —
              always reference a token (CSS var or JS constant)
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0">2.</span>
            <p className="text-fg-2">
              <span className="font-medium">Spring easing everywhere</span> —
              never ease-in-out, never linear
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0">3.</span>
            <p className="text-fg-2">
              <span className="font-medium">Signature color only breathes</span>{" "}
              — never flashes, never blinks
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0">4.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                No animation &gt; bad animation
              </span>{" "}
              — if unsure, use a simple opacity fade
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0">5.</span>
            <p className="text-fg-2">
              <span className="font-medium">prefers-reduced-motion</span> —
              respect system setting, disable non-essential motion
            </p>
          </div>
        </div>
      </DemoCard>
    </Section>
  );
}
