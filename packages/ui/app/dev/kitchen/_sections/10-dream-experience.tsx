// Maps to: /dreams page, settings dreams section, bar notification
// ui/ components: dreamReport, dreamActionCard, dreamReviewCard
"use client";

import { useState } from "react";
import { Section, DemoCard } from "../_shared";
import {
  DREAM_PURPLE,
  DREAM_BLUE,
  DREAM_PINK,
  DREAM_GLOW,
  DREAM_BORDER,
  DREAM_BORDER_SUBTLE,
  DREAM_AURORA,
  DREAM_GRADIENT_SUBTLE,
  DREAM_PROGRESS,
  DREAM_KEYFRAMES,
  DREAM_BANNER,
  DREAM_BANNER_KEYFRAMES,
} from "@/tokens/dream";

/** Shared dream card wrapper with gradient border and soft glow */
function DreamCard({
  children,
  glow = false,
  aurora = false,
  style,
  className = "",
}: {
  children: React.ReactNode;
  glow?: boolean;
  aurora?: boolean;
  style?: React.CSSProperties;
  className?: string;
}) {
  return (
    <div className={`relative ${className}`}>
      {/* dream glow — soft radial gradient behind card */}
      {glow && (
        <div
          className="absolute -inset-3 rounded-2xl pointer-events-none"
          style={{
            background: DREAM_GLOW,
            animation: "dream-glow-pulse 4s ease-in-out infinite",
          }}
        />
      )}
      <div
        className="relative rounded-xl p-4 overflow-hidden"
        style={{
          border: DREAM_BORDER,
          ...(!style?.background && !aurora
            ? { background: "var(--card)" }
            : {}),
          ...style,
        }}
      >
        {/* Aurora animated gradient background for running state */}
        {aurora && (
          <div
            className="absolute inset-0 pointer-events-none"
            style={{
              background: DREAM_AURORA,
              backgroundSize: "300% 300%",
              animation: "dream-aurora 6s ease infinite",
            }}
          />
        )}
        <div className="relative z-10">{children}</div>
      </div>
    </div>
  );
}

function DreamSectionBanner({
  title,
  detail,
  active,
  action,
}: {
  title: string;
  detail: string;
  active?: boolean;
  action?: string;
}) {
  return (
    <div
      className="rounded-xl px-3 py-2 flex items-center gap-2 overflow-hidden"
      style={{
        border: DREAM_BORDER_SUBTLE,
        background: active ? DREAM_BANNER.dreaming.bg : DREAM_BANNER.dream.bg,
        backgroundSize: active ? "300% 300%" : undefined,
        animation: active
          ? "bar-notif-aurora 4s ease infinite, bar-dreaming-glow 3s ease-in-out infinite"
          : undefined,
      }}
    >
      <span
        className={`text-[13px] shrink-0 ${active ? "state-slow-breathe" : ""}`}
        style={{
          color: active
            ? DREAM_BANNER.dreaming.accent
            : DREAM_BANNER.dream.accent,
        }}
      >
        ✦
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-[13px] text-fg-2 font-medium truncate">{title}</p>
        <p className="text-[12px] text-fg-3 truncate">{detail}</p>
      </div>
      {action && (
        <button className="text-[12px] text-fg-2 hover:text-fg-1 transition-colors shrink-0">
          {action}
        </button>
      )}
    </div>
  );
}

function DreamQueueRow({
  label,
  count,
  detail,
}: {
  label: string;
  count: string;
  detail: string;
}) {
  return (
    <div className="flex items-center gap-2 rounded-lg px-3 py-2 border border-border/30 bg-card/80">
      <span className="text-[12px] text-fg-2 font-medium flex-1 truncate">
        {label}
      </span>
      <span className="text-[11px] rounded-full px-2 py-0.5 text-fg-2 bg-surface-1 shrink-0">
        {count}
      </span>
      <span className="text-[11px] text-fg-4 shrink-0">{detail}</span>
    </div>
  );
}

function DreamTopicDeltaCard() {
  return (
    <DreamCard style={{ background: DREAM_BANNER.dream.bg }}>
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <span style={{ color: DREAM_BANNER.dream.accent, fontSize: 13 }}>
            ✦
          </span>
          <span className="text-[13px] text-fg-2 font-medium">
            Auth & Security
          </span>
          <span className="ml-auto text-[11px] text-fg-3 rounded-full px-2 py-0.5 bg-card/70">
            4 new
          </span>
        </div>
        <p className="text-[12px] text-fg-3">
          dream grouped new auth work since your last visit. Start here before
          browsing older memories.
        </p>
        <div className="space-y-1.5">
          <div className="flex items-center gap-2 text-[12px] text-fg-2">
            <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
            <span className="flex-1 truncate">
              API key rotation policy update
            </span>
            <span className="text-fg-4">3h</span>
          </div>
          <div className="flex items-center gap-2 text-[12px] text-fg-2">
            <span className="h-1.5 w-1.5 rounded-full bg-amber-500" />
            <span className="flex-1 truncate">OAuth refresh token audit</span>
            <span className="text-fg-4">5h</span>
          </div>
        </div>
      </div>
    </DreamCard>
  );
}

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 * 10b helpers — the morning-after flow
 *   - DreamBarNotification: 3-state ephemeral bar-adjacent line
 *   - DreamRecentReviewSurface: Recent feed with row-accents
 *   - DreamTopicCard: topic card with "N new" pill
 *
 * 10c helper — ConflictInboxDemo: inline simple decision surface
 * ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */

function DreamBarNotification({
  state,
}: {
  state: "dreaming" | "complete" | "clean";
}) {
  const isActive = state === "dreaming";
  const label =
    state === "dreaming"
      ? "memax is dreaming"
      : state === "complete"
        ? "dreams updated 4 topics · view"
        : "dreams are clean — nothing to review";
  const detail =
    state === "dreaming"
      ? "scanning 247 memories"
      : state === "complete"
        ? "Auth · Deployment · Frontend · Docs"
        : "last night · 247 memories";

  return (
    <div className="space-y-2">
      {/* The ephemeral notification card — matches bar width */}
      <div
        className="rounded-xl px-3 py-2 flex items-center gap-2 overflow-hidden"
        style={{
          border: DREAM_BORDER_SUBTLE,
          background: isActive
            ? DREAM_BANNER.dreaming.bg
            : DREAM_BANNER.dream.bg,
          backgroundSize: isActive ? "300% 300%" : undefined,
          animation: isActive
            ? "bar-notif-aurora 4s ease infinite, bar-dreaming-glow 3s ease-in-out infinite"
            : undefined,
        }}
      >
        <span
          className={`text-[13px] shrink-0 ${isActive ? "state-slow-breathe" : ""}`}
          style={{
            color: isActive
              ? DREAM_BANNER.dreaming.accent
              : DREAM_BANNER.dream.accent,
          }}
        >
          ✦
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-[13px] text-fg-2 font-medium truncate">{label}</p>
          <p className="text-[12px] text-fg-3 truncate">{detail}</p>
        </div>
      </div>

      {/* Bar stub — shows the notification is above the actual bar */}
      <div
        className="rounded-xl h-10 flex items-center px-4"
        style={{
          background: "var(--card)",
          border: "1px solid var(--bar-border)",
          boxShadow: "var(--bar-shadow)",
        }}
      >
        <span className="text-[12px] text-fg-4 truncate">
          remember or ask&hellip;
        </span>
      </div>

      <p className="text-[10px] text-fg-4 text-center">
        {state === "dreaming"
          ? "while running"
          : state === "complete"
            ? "completed overnight"
            : "clean run"}
      </p>
    </div>
  );
}

/** Recent feed with row accents on dream-moved items. No header, no banner. */
function DreamRecentReviewSurface() {
  const rows = [
    {
      title: "API key rotation policy update",
      dest: "Auth & Security",
      age: "3h",
      dreamTouched: true,
    },
    {
      title: "OAuth refresh token audit",
      dest: "Auth & Security",
      age: "5h",
      dreamTouched: true,
    },
    {
      title: "SAML callback boundary fix",
      dest: "Auth & Security",
      age: "7h",
      dreamTouched: true,
    },
    {
      title: "Staging rollback script v2",
      dest: "Deployment",
      age: "9h",
      dreamTouched: true,
    },
    {
      title: "RSC caching notes",
      dest: "Frontend",
      age: "11h",
      dreamTouched: true,
    },
    {
      title: "Fly.io health check tuning",
      dest: "Deployment",
      age: "2d",
      dreamTouched: false,
    },
    {
      title: "Reranker cost analysis",
      dest: "Auth & Security",
      age: "3d",
      dreamTouched: false,
    },
  ];

  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
      }}
    >
      {/* Section header — identical to production Recent, no dream chrome */}
      <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
        <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
          Recent
        </span>
        <span className="text-[12px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded">
          12h
        </span>
        <div className="flex-1" />
        <span className="text-[12px] text-fg-4">Copy 5 &middot; Select</span>
      </div>

      {rows.map((row, i) => {
        const accentBorderColor = row.dreamTouched
          ? "oklch(0.55 0.15 290 / 0.55)"
          : "transparent";
        return (
          <div
            key={row.title}
            className={`relative flex items-center gap-2 px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors ${
              i > 0 ? "border-t border-border/30" : ""
            }`}
            style={{ boxShadow: `inset 2px 0 0 ${accentBorderColor}` }}
          >
            <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
              {row.title}
            </span>
            <span className="text-[12px] text-fg-3 shrink-0">{row.dest}</span>
            <span className="text-[12px] text-fg-4 tabular-nums shrink-0 w-8 text-right">
              {row.age}
            </span>
          </div>
        );
      })}
    </div>
  );
}

/** Topic card with optional "N new" pill. Grid-sized, matches kitchen 29a. */
function DreamTopicCard({
  name,
  emoji,
  desc,
  count,
  newCount,
}: {
  name: string;
  emoji: string;
  desc: string;
  count: number;
  newCount: number;
}) {
  const hasNew = newCount > 0;
  return (
    <div
      className="rounded-xl p-4 flex flex-col cursor-pointer transition-all hover:bg-surface-1"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
        // Shadow lift signals "this card has new items" without a colored border.
        // Default cards stay flat; active cards float a touch with a signature-tinted glow.
        boxShadow: hasNew
          ? "0 6px 20px oklch(0.55 0.15 290 / 0.1), 0 1px 3px oklch(0.55 0.15 290 / 0.08)"
          : undefined,
        transform: hasNew ? "translateY(-1px)" : undefined,
      }}
    >
      <div className="flex items-center gap-2 mb-1.5">
        <span className="text-[16px] leading-none">{emoji}</span>
        <span className="text-[14px] font-semibold text-foreground truncate flex-1">
          {name}
        </span>
        {hasNew && (
          <span
            className="text-[11px] font-medium shrink-0 tabular-nums"
            style={{ color: DREAM_BANNER.dream.accent }}
          >
            ✦ {newCount} new
          </span>
        )}
      </div>
      <p className="text-[12px] text-fg-3 truncate mb-auto">{desc}</p>
      <p className="text-[11px] text-fg-4 tabular-nums mt-2">
        {count} memories
      </p>
    </div>
  );
}

/** Inbox with a conflict row that expands inline into a simple decision
 *  surface. No card stack, no glow, no multi-step ceremony — just the
 *  two notes side by side and four buttons. Each conflict is its own
 *  inbox row; resolving removes the row. */
function ConflictInboxDemo() {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="max-w-2xl">
      <div
        className="rounded-xl overflow-hidden"
        style={{
          border: "1px solid var(--border)",
          background: "var(--card)",
        }}
      >
        {/* Inbox section header — matches production */}
        <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
          <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
            Inbox
          </span>
          <span className="text-[13px] text-fg-3 tabular-nums">3</span>
          <div className="flex-1" />
          <span className="text-[12px] text-fg-4">Organize</span>
        </div>

        {/* Conflict row — dream-attributed, tap to expand */}
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="w-full text-left flex items-center gap-2 px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30"
          style={{
            boxShadow: "inset 2px 0 0 oklch(0.55 0.15 290 / 0.55)",
          }}
        >
          <span
            className="text-[13px] shrink-0 state-slow-breathe"
            style={{ color: DREAM_BANNER.dream.accent }}
          >
            ✦
          </span>
          <span className="text-[14px] text-fg-2 font-medium truncate flex-1">
            Conflict · cache TTL values
          </span>
          <span className="text-[11px] text-fg-3 shrink-0">87% similarity</span>
          <span className="text-[12px] text-fg-4 tabular-nums shrink-0">
            2h
          </span>
        </button>

        {/* Inline expansion — simple decision surface, same container */}
        {expanded && (
          <div
            className="px-4 pt-3 pb-4 border-t border-border/30 space-y-3"
            style={{ background: "oklch(0.55 0.15 290 / 0.03)" }}
          >
            <p className="text-[12px] text-fg-3">
              Both mention Redis cache configuration — decide which to keep.
            </p>
            <div className="grid grid-cols-2 gap-3">
              <div className="rounded-lg bg-surface-1 p-3">
                <p className="text-[10px] uppercase tracking-wider text-fg-3 font-medium mb-1">
                  Note A
                </p>
                <p className="text-[13px] text-fg-1 leading-relaxed">
                  &ldquo;Default cache TTL is 3600 seconds (1 hour)&rdquo;
                </p>
              </div>
              <div className="rounded-lg bg-surface-1 p-3">
                <p className="text-[10px] uppercase tracking-wider text-fg-3 font-medium mb-1">
                  Note B
                </p>
                <p className="text-[13px] text-fg-1 leading-relaxed">
                  &ldquo;Set cache TTL to 1800s for optimal performance&rdquo;
                </p>
              </div>
            </div>
            <div className="flex items-center gap-2 pt-1">
              <button
                type="button"
                className="text-[12px] px-3 py-1.5 rounded-md bg-surface-2 text-fg-1 hover:bg-foreground/8 transition-colors cursor-pointer"
              >
                Keep A
              </button>
              <button
                type="button"
                className="text-[12px] px-3 py-1.5 rounded-md bg-surface-2 text-fg-1 hover:bg-foreground/8 transition-colors cursor-pointer"
              >
                Keep B
              </button>
              <button
                type="button"
                className="text-[12px] px-3 py-1.5 rounded-md font-medium transition-colors cursor-pointer"
                style={{
                  background: "oklch(0.55 0.15 290 / 0.12)",
                  color: "oklch(0.55 0.15 290)",
                }}
              >
                Keep both
              </button>
              <button
                type="button"
                className="ml-auto text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
              >
                Skip
              </button>
            </div>
          </div>
        )}

        {/* Other inbox rows — plain, for comparison */}
        <div className="flex items-center gap-2 px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
          <span className="text-[14px] text-fg-2 truncate flex-1">
            New captured thought about RSC caching
          </span>
          <span className="text-[12px] text-fg-4 tabular-nums shrink-0">
            4h
          </span>
        </div>
        <div className="flex items-center gap-2 px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
          <span className="text-[14px] text-fg-2 truncate flex-1">
            Meeting notes from Monday standup
          </span>
          <span className="text-[12px] text-fg-4 tabular-nums shrink-0">
            1d
          </span>
        </div>
      </div>

      <p className="text-[11px] text-fg-3 mt-3">
        {expanded
          ? "Tap the conflict row again to collapse. Resolving removes the row from the inbox. The next conflict arrives as its own inbox row."
          : "Tap the ✦ conflict row to expand inline. Two notes, four buttons — no card stack, no ceremony."}
      </p>
    </div>
  );
}

export function DreamExperienceSection() {
  return (
    <Section
      title="10. Dream Experience"
      description="Dreams show up across memax — while they are happening, after they finish, and anywhere they leave something worth your attention."
    >
      {/* Inject keyframes */}
      <style
        dangerouslySetInnerHTML={{
          __html: `${DREAM_KEYFRAMES}
${DREAM_BANNER_KEYFRAMES}`,
        }}
      />

      {/* 10a. dream Report Card */}
      <DemoCard label="10a. dream states">
        <div className="grid grid-cols-2 gap-4">
          {/* No runs */}
          <DreamCard glow>
            <div className="flex flex-col items-center justify-center text-center gap-2">
              <span style={{ color: DREAM_PURPLE, fontSize: 16 }}>✦</span>
              <p className="text-[14px] text-fg-2">
                dreams haven&apos;t run yet
              </p>
              <p className="text-[12px] text-fg-3">
                They run nightly to organize your memory
              </p>
            </div>
          </DreamCard>

          {/* Running — aurora glow background */}
          <DreamCard aurora glow>
            <div className="space-y-3">
              <div className="flex items-center gap-2">
                <span
                  className="text-[15px] leading-none state-fast-pulse"
                  style={{ color: DREAM_PINK }}
                >
                  ✦
                </span>
                <span
                  className="text-[14px] font-medium"
                  style={{ color: DREAM_PURPLE }}
                >
                  dreaming&hellip;
                </span>
              </div>
              <p className="text-[12px] text-fg-3">Scanning 247 memories</p>
              <div
                className="w-full h-1.5 rounded-full overflow-hidden"
                style={{ background: "oklch(0.55 0.15 290 / 0.1)" }}
              >
                <div
                  className="h-full rounded-full state-slow-breathe"
                  style={{
                    width: "65%",
                    background: DREAM_PROGRESS,
                  }}
                />
              </div>
            </div>
          </DreamCard>

          {/* Completed (found) — premium gradient bg */}
          <DreamCard
            glow
            style={{
              background: DREAM_GRADIENT_SUBTLE,
            }}
          >
            <div className="space-y-3">
              <div className="flex items-center gap-2">
                <span style={{ color: DREAM_BLUE, fontSize: 12 }}>✦</span>
                <span className="text-[13px] text-fg-3">
                  Last night &middot; 247 memories
                </span>
              </div>
              <div className="space-y-1.5">
                <div className="flex items-center gap-2">
                  <div className="h-1.5 w-1.5 rounded-full bg-emerald-500 shrink-0" />
                  <span className="text-[13px] text-fg-2">3 notes merged</span>
                </div>
                <div className="flex items-center gap-2">
                  <div className="h-1.5 w-1.5 rounded-full bg-amber-500 shrink-0" />
                  <span className="text-[13px] text-fg-2">
                    1 contradiction found
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <div className="h-1.5 w-1.5 rounded-full bg-foreground/40 shrink-0" />
                  <span className="text-[13px] text-fg-2">
                    2 notes archived
                  </span>
                </div>
              </div>
            </div>
          </DreamCard>

          {/* Completed (clean) */}
          <DreamCard glow>
            <div className="flex flex-col items-center justify-center text-center gap-2">
              <span style={{ color: DREAM_BLUE, fontSize: 14 }}>✦</span>
              <p className="text-[14px] text-fg-2">Your memory is clean</p>
            </div>
          </DreamCard>

          {/* Failed */}
          <DreamCard
            style={{
              border: "1px solid oklch(0.65 0.08 50 / 0.2)",
              background: "oklch(0.65 0.06 50 / 0.04)",
            }}
          >
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <div
                  className="h-1.5 w-1.5 rounded-full shrink-0"
                  style={{ backgroundColor: "oklch(0.65 0.12 50)" }}
                />
                <span className="text-[14px] text-fg-2">dream failed</span>
              </div>
              <p className="text-[12px] text-fg-3">
                Processing timed out. Your memories are safe.
              </p>
              <button
                className="text-[13px] transition-colors"
                style={{ color: "oklch(0.60 0.10 50 / 0.7)" }}
              >
                Try again
              </button>
            </div>
          </DreamCard>

          {/* Disabled */}
          <DreamCard>
            <div className="flex flex-col items-center justify-center text-center gap-2">
              <span style={{ color: DREAM_PURPLE, fontSize: 14, opacity: 0.4 }}>
                ✦
              </span>
              <p className="text-[14px] text-fg-2">dreams are turned off</p>
              <button className="text-[12px] text-fg-2 hover:text-fg-2 underline transition-colors">
                Enable in settings
              </button>
            </div>
          </DreamCard>
        </div>
      </DemoCard>

      {/* 10b. dreams around memax — the morning-after review flow */}
      <DemoCard label="10b. dreams around memax — the morning-after flow">
        <p className="text-[12px] text-fg-2 mb-1 max-w-2xl">
          dreams never live on a dedicated page. You open memax the morning
          after an overnight dream and the signal is already where it matters:
          one ephemeral line near the bar, one row-accent on the items that
          moved, one <span className="text-fg-1">N new</span> pill on the topic
          cards that grew.
        </p>
        <p className="text-[11px] text-fg-3 mb-4 max-w-2xl">
          No aggregate &ldquo;what changed&rdquo; card, no review queue, no
          dream panel. Three cooperating surfaces, each doing one job.
        </p>

        {/* Panel 1 — Bar notification morph */}
        <div className="mb-5">
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            1. Bar-adjacent notification — ephemeral, single line
          </p>
          <div className="grid gap-3 md:grid-cols-3">
            <DreamBarNotification state="dreaming" />
            <DreamBarNotification state="complete" />
            <DreamBarNotification state="clean" />
          </div>
          <p className="text-[11px] text-fg-3 mt-2">
            Auto-dismisses ~6s after complete or on first interaction. Tap
            completed state → scrolls to Recent with a 12h filter applied.
            That&apos;s the entire &ldquo;review entry point.&rdquo;
          </p>
        </div>

        {/* Panel 2 — Recent review feed */}
        <div className="mb-5">
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            2. Recent — global review feed with row accents on moved items
          </p>
          <DreamRecentReviewSurface />
          <p className="text-[11px] text-fg-3 mt-2">
            Each dream-touched row gets a 2px signature-muted left accent and an
            inline topic destination. The accent is the{" "}
            <span className="text-fg-1">only</span> dream signal — no header, no
            banner, no separate &ldquo;what changed&rdquo; card. Select any
            wrong ones → existing global batch toolbar → move.
          </p>
        </div>

        {/* Panel 3 — Topic grid with N new pills */}
        <div>
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            3. Topic grid — <span className="text-fg-1">N new</span> pills on
            cards that grew
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
            <DreamTopicCard
              name="Auth & Security"
              emoji="🛡"
              desc="JWT tokens, OAuth, API keys"
              count={23}
              newCount={3}
            />
            <DreamTopicCard
              name="Deployment"
              emoji="🚀"
              desc="Blue-green, rollback, staging"
              count={47}
              newCount={2}
            />
            <DreamTopicCard
              name="Frontend"
              emoji="📐"
              desc="RSC, Tailwind, Radix"
              count={22}
              newCount={1}
            />
            <DreamTopicCard
              name="Database"
              emoji="💾"
              desc="Connection pooling, migrations"
              count={15}
              newCount={0}
            />
            <DreamTopicCard
              name="Go Patterns"
              emoji="📦"
              desc="Error handling, context"
              count={31}
              newCount={0}
            />
            <DreamTopicCard
              name="Team Docs"
              emoji="📝"
              desc="Onboarding, code review"
              count={9}
              newCount={0}
            />
          </div>
          <p className="text-[11px] text-fg-3 mt-2">
            The <span className="text-fg-1">N new</span> pill is the spatial
            announcement — it says &ldquo;this topic is where to look.&rdquo;
            Inside the topic, delta becomes row-accent only (section 29d). The
            count never re-appears.
          </p>
        </div>
      </DemoCard>

      {/* 10c. Conflicts live in inbox — not a dedicated review queue */}
      <DemoCard label="10c. Conflicts live in inbox, not a dedicated queue">
        <p className="text-[12px] text-fg-2 mb-1 max-w-2xl">
          When the dream engine finds a contradiction it can&apos;t resolve, it
          lands in the inbox as a <span className="text-fg-1">conflict</span>{" "}
          row. Tap → expands inline into a simple decision surface (two notes,
          four buttons). No separate review queue, no card stack, no ceremony.
        </p>
        <p className="text-[11px] text-fg-3 mb-4 max-w-2xl">
          The inbox already exists. The dream engine just writes into it. Each
          conflict is its own inbox row — resolve one, it disappears, next
          conflict arrives as the next row.
        </p>
        <ConflictInboxDemo />
      </DemoCard>

      {/* i18n key reference */}
      <div className="text-[12px] text-fg-4 space-y-0.5 mt-2">
        <p>i18n keys: dreams.noRuns, dreams.running, dreams.completed,</p>
        <p>dreams.clean, dreams.failed, dreams.disabled, dreams.merge,</p>
        <p>dreams.archive, dreams.contradiction, dreams.review.*</p>
      </div>

      {/* Design rationale */}
      <div className="text-[12px] text-fg-3 mt-2">
        <p>
          dreams should feel gentle but clear. Let them glow while they are
          happening, settle when they are done, and only ask for attention when
          you really need to decide something.
        </p>
      </div>
    </Section>
  );
}
