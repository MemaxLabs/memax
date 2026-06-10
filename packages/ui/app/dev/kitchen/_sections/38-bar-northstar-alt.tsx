// ═══════════════════════════════════════════════════════════════════
// 38. Bar — North Star (alt take)
//
// A second take on the bar redesign, sibling to section 37. Both are
// valid targets — kept side-by-side so the team can A/B read them
// before committing. The deltas card at the bottom (38q) calls out
// where this version disagrees with 37 and why.
//
// Full written spec for this version: docs/design/bar-northstar.md.
//
// ── Ten rules (read before editing) ───────────────────────────────
//
//   1. 24m GLASS EVERYWHERE. The chrome uses exactly 24m's treatment:
//      oklch(card / 0.92), backdrop-filter blur(24px) saturate(140%),
//      1px border oklch(border / 0.5), layered soft shadow. No flat
//      card. No heavy edge. The whole bar feels lifted off the page.
//
//   2. RECALL IS DEFAULT. In the rest state, zone ① shows a plain
//      Search icon on the left — no capsule, no label. The capsule
//      only appears when scope is non-default (topic / command).
//      Recall-by-default means the most common state has the least UI.
//
//   3. DOWNWARD CASCADE. Results flow below the input. [1] at top,
//      [N] at bottom, natural order. Linear / Raycast / Spotlight.
//
//   4. AI SLOT IS ALWAYS RESERVED. Zone ② (synthesis) is present from
//      the first keystroke — dim placeholder during typing, thinking /
//      streaming / complete after ↵. Free users see an upgrade stub
//      in the exact same slot. The layout never jumps between tiers,
//      and the user never wonders where the AI went.
//
//   5. REMEMBER ROW IS INLINE + TRANSIENT. Zone ③ is the Remember
//      action row. It stays visible in every phase (so the user can
//      always save), and it also hosts inline feedback for the save
//      action itself: idle → Remembering… → Sent · Undo → idle.
//      BarNotificationCard is deleted. The bar stays open after remember
//      completion; it never auto-docks or auto-clears the inline state.
//      No separate toast.
//
//   6. HUB BADGE BELONGS ON THE REMEMBER ROW. When the user is in a
//      multi-hub context AND the active hub is a team hub, the Remember
//      row carries a → {hub} badge. It persists through every transient
//      state so the user always knows *where* the save is going. Single
//      hub / personal: no badge.
//
//   7. ROUTE-AWARE REST, MIDDLE-UPPER ENGAGE. Shipped production uses
//      rest top 42vh on /home, bottom-docked rest on /memories/overlay,
//      and engaged top 24vh once real interaction state exists. Result
//      surface has max-h-[60vh] and a "+N more" / Load more affordance
//      at the bottom of zone ④.
//
//   8. MOBILE IS A NOTION-STYLE MIRROR. The bar stays above the
//      keyboard (thumb zone). On focus, a mirror input slides down
//      from the top of the screen showing the same query in large
//      type — eyes at top, thumbs at bottom. Result surface sits
//      between them. Edge-to-edge, no card chrome. Three exits: X,
//      swipe-down, tap scrim. This is the Notion mobile search
//      pattern, adapted to memax.
//
//   9. SLASH IS MINIMAL. Shipped command scope is /topic + /hub.
//      Route-scoped /forget remains a future addition, not a half-wired
//      current command. No /import (drag-drop already covers it), no
//      /share, no /remember (⌘↵ covers it), no /settings. Less is more.
//
//  10. REAL KEYBOARD GLYPHS, 24m HINT BAR. Hint chips are <kbd> with
//      literal Unicode — ↵ ⇧ ⌘ ⌥ ⇥ esc ↑ ↓. Hint bar at the bottom
//      of the expanded surface uses 24m's style: plain text spans
//      with single-glyph <kbd>, not chains of boxy pills. No lucide
//      icons anywhere in the hint system.
//
//  11. MOBILE TOP INSET USES LAYOUT TOKENS (SOT). Never hardcode
//      "safe-top + magic number" in mobile bar overlays or page roots.
//      Use the shared tokens from packages/web/src/lib/layout.ts:
//      MOBILE_CHROME_TOP, MOBILE_CONTENT_TOP, MOBILE_OVERLAY_CONTENT_TOP.
//      App chrome (logo + hub chip), page content, and mirror/fullscreen
//      compose must all reference those tokens so spacing stays aligned.
//
// ── Anatomy (5 stacked zones — always in this order) ─────────────
//
//   ┌──────────────────────────────────────────────────────┐
//   │ ① Input row      🔍  textarea                 send  │
//   ├──────────────────────────────────────────────────────┤
//   │ ② Synthesis slot ✦  AI answer (or upgrade stub)     │   ← reserved
//   ├──────────────────────────────────────────────────────┤
//   │ ③ Remember row   +  Remember "…"  →hub        ⌘↵    │   ← inline feedback
//   ├──────────────────────────────────────────────────────┤
//   │ ④ Source list    [1] Title · topic · hub     92%    │
//   │                  [2] Title · topic           88%    │
//   │                  + 3 more                           │
//   ├──────────────────────────────────────────────────────┤
//   │ ⑤ Hint bar       ↵ recall   ⌘↵ remember   esc close │   ← 24m style
//   └──────────────────────────────────────────────────────┘
//
// ── i18n notes ────────────────────────────────────────────────────
//
// Every user-facing string is a kitchen mock. On production port, wire
// through t.* (see i18n/SKILL.md):
//   t.bar.placeholder.*   — input placeholders
//   t.bar.remember.*      — zone ③ states (idle / sending / sent / error)
//   t.bar.ai.*            — zone ② stub, thinking, error
//   t.bar.hint.*          — hint bar labels
//   t.bar.command.*       — slash command labels + descriptions
//   t.bar.noResults       — zone ④ empty state
//   t.bar.recallError     — zone ④ error state
// ═══════════════════════════════════════════════════════════════════
"use client";

import { useState } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import {
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Camera,
  Loader2,
  Maximize2,
  ShieldCheck,
  Wrench,
  Hash,
  Folder,
  Trash2,
  Plus,
  Check,
  X,
  Image as ImageIcon,
  FileText,
  Paperclip,
  Link2,
  Command,
  Undo2,
  type LucideIcon,
} from "lucide-react";

// ═══════════════════════════════════════════════════════════════════
// Primitives
// ═══════════════════════════════════════════════════════════════════

/** Kbd — literal Unicode glyph in a <kbd>. Never lucide. */
function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd
      className="inline-flex items-center justify-center h-[18px] min-w-[18px] px-1 rounded text-[10px] font-mono text-fg-3 bg-surface-2 leading-none"
      style={{ fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" }}
    >
      {children}
    </kbd>
  );
}

/**
 * StarGlyph — the one place ✦ is rendered. Every usage in this section
 * goes through this helper so baseline alignment is consistent.
 *
 * The ✦ (U+2726) character has a taller-than-wide glyph that sits a hair
 * above the optical center of its line box. Fix: fixed-size square
 * container with flex centering + a tiny transform nudge. Size controls
 * both font-size and container — no more eyeballing mt-0.5 per usage.
 */
function StarGlyph({
  size = 13,
  color = SIGNATURE,
  breathe,
  className = "",
}: {
  size?: number;
  color?: string;
  breathe?: boolean;
  className?: string;
}) {
  return (
    <span
      className={`inline-flex items-center justify-center shrink-0 leading-none ${breathe ? "state-slow-breathe" : ""} ${className}`}
      style={{
        width: size,
        height: size,
        fontSize: size,
        color,
        transform: "translateY(0.5px)",
      }}
      aria-hidden
    >
      ✦
    </span>
  );
}

/**
 * GlassChrome — 24m glass treatment, the entire premise of this take.
 *   - bg:     oklch(from var(--card) l c h / 0.92)
 *   - blur:   backdrop-filter: blur(24px) saturate(140%)
 *   - border: 1px solid oklch(from var(--border) l c h / 0.5)
 *   - shadow: layered soft drop — 24m "shadow-2xl" vibe
 *   - radius: 20px
 */
function GlassChrome({
  children,
  caption,
  mobile,
  maxWidth,
  dragActive,
}: {
  children: React.ReactNode;
  caption?: string;
  mobile?: boolean;
  maxWidth?: number;
  dragActive?: boolean;
}) {
  const mw = maxWidth ?? (mobile ? 375 : 560);
  return (
    <div className="flex flex-col items-center gap-2">
      <div
        className="w-full overflow-hidden"
        style={{
          maxWidth: `${mw}px`,
          borderRadius: 20,
          background: "oklch(from var(--card) l c h / 0.92)",
          backdropFilter: "blur(24px) saturate(140%)",
          WebkitBackdropFilter: "blur(24px) saturate(140%)",
          border: dragActive
            ? "2px dashed var(--signature)"
            : "1px solid oklch(from var(--border) l c h / 0.5)",
          boxShadow: dragActive
            ? "0 0 0 6px oklch(from var(--signature) l c h / 0.06)"
            : "0 12px 40px oklch(0 0 0 / 0.08), 0 2px 8px oklch(0 0 0 / 0.04)",
        }}
      >
        <div className="flex flex-col">{children}</div>
      </div>
      {caption && (
        <p className="text-[10px] text-fg-3 text-center">{caption}</p>
      )}
    </div>
  );
}

/** Subtle hairline between zones — a whisper, not a divider. */
function ZoneRule() {
  return <div className="h-px bg-border/30 mx-4" />;
}

// ───────────────────────────────────────────────
// Zone ① — Input row
// ───────────────────────────────────────────────

type Scope =
  | { kind: "recall" }
  | { kind: "topic"; label: string; Icon?: LucideIcon }
  | { kind: "command"; label: string };

/**
 * ScopeIndicator — leftmost element of zone ① when scope ≠ recall.
 *
 * Default (recall): renders NOTHING. The textarea starts at the bar
 * padding. Placeholder text carries the affordance. Same as Spotlight,
 * Notion, ChatGPT, Claude, Linear ⌘K.
 *
 * Non-default (topic, command): a labeled pill so the active scope is
 * unmissable. Click to clear → returns to recall.
 */
function ScopeIndicator({ scope }: { scope: Scope }) {
  if (scope.kind === "recall") return null;
  const Icon = scope.kind === "topic" ? (scope.Icon ?? Folder) : Command;
  return (
    <div
      className="relative flex items-center gap-1.5 h-9 px-3 rounded-full shrink-0 text-[13px] font-medium select-none cursor-pointer"
      style={{
        background: "oklch(from var(--foreground) l c h / 0.06)",
        color: "var(--fg-2)",
      }}
      title="Click to clear scope"
    >
      <Icon className="h-3.5 w-3.5" />
      <span>{scope.label}</span>
      <X className="h-3 w-3 text-fg-3 ml-0.5" />
      <span className="absolute -inset-1" aria-hidden />
    </div>
  );
}

/**
 * Send button — ALWAYS ↑. Color communicates intent, not the icon.
 *
 * Rule (the one source of truth for send action):
 *   - files staged           → remember (↑ in dark foreground fill)
 *   - text contains '\n'     → remember (↑ in dark foreground fill)
 *   - otherwise              → recall   (↑ in signature violet fill)
 *   - empty                  → disabled (↑ ghost, 40% opacity)
 *   - in-flight              → Loader2 spinner
 *
 * The icon is always ArrowUp (except Loader2). The BACKGROUND COLOR
 * tells the user what tapping will fire:
 *   - violet → ask memax (recall + AI)
 *   - dark   → send to memax (dump + save)
 *
 * Why not ✦ for recall? The memax design system reserves ✦ (signature
 * violet) for "memax AI is working" — the AI thinking indicator in
 * zone ②. Using it as a CTA icon conflates two meanings and violates
 * the "signature = intelligence, never decorative" rule. The
 * ChatGPT / Claude / Perplexity / Gemini convention is unified ↑ for
 * send. We follow that. ✦ stays sacred to the synthesis slot.
 *
 * Override (desktop only): ⌘↵ forces dump from any state (edge case:
 * short single-line note the user wants to save). No recall override —
 * if you're composing multi-line and want to search, delete the
 * newlines. Edge case, acceptable friction.
 *
 * Deprecated patterns (do not reintroduce):
 *   - packages/web/src/lib/detect-intent.ts — EN/ZH regex intent sniff
 *   - `?` prefix recall trigger (kitchen 24 proposal, never shipped)
 *   - mode capsule tap-to-toggle (kitchen 31 — ScopeIndicator replaces)
 *   - ✦ as send button CTA (kitchen 38 pre-2026-04-13 — conflated
 *     meanings with AI-thinking glyph; replaced by unified ↑)
 */
function SendButton({
  kind = "recall",
  state,
}: {
  kind?: "recall" | "remember";
  state: "disabled" | "ready" | "loading";
}) {
  const bg =
    state === "disabled"
      ? undefined
      : kind === "recall"
        ? "var(--signature)"
        : "var(--foreground)";
  const fg =
    state === "disabled"
      ? "var(--fg-4)"
      : kind === "recall"
        ? "#fff"
        : "var(--background)";
  return (
    <div
      className="flex items-center justify-center h-8 w-8 rounded-lg shrink-0"
      style={{
        background: state === "disabled" ? undefined : bg,
        color: fg,
        opacity: state === "disabled" ? 0.4 : 1,
      }}
    >
      {state === "loading" ? (
        <Loader2 className="h-4 w-4 animate-spin" />
      ) : (
        <ArrowUp className="h-4 w-4" />
      )}
    </div>
  );
}

/**
 * InputRow — zone ①, collapsed (single-line) variant.
 *
 * Grid: [scope?] [textarea] [camera?] [cancel?] [send]
 *
 * - scope: only renders for topic / command scopes (recall = nothing)
 * - camera: mobile-only, shown when input is empty AND no files staged —
 *   opens native photo/camera picker. Hides as soon as user types or
 *   stages a file (same gate as production bar-right-portal.tsx).
 * - cancel: X icon left of send during searching / recall-results phase,
 *   layered with esc semantics.
 */
function InputRow({
  scope = { kind: "recall" },
  value,
  placeholder,
  sendKind = "recall",
  sendState = "disabled",
  mobile,
  showCamera,
  showCancel,
}: {
  scope?: Scope;
  value?: string;
  placeholder?: string;
  sendKind?: "recall" | "remember";
  sendState?: "disabled" | "ready" | "loading";
  mobile?: boolean;
  /** mobile photo picker — shown when empty + no files staged */
  showCamera?: boolean;
  showCancel?: boolean;
}) {
  const ph =
    placeholder ??
    // i18n: t.bar.placeholder.default — casual, dual-intent voice
    "dump or ask…";
  const hasScope = scope.kind !== "recall";
  return (
    <div
      className="min-h-14 px-4 sm:px-5 py-2 grid items-center"
      style={{
        gridTemplateColumns: hasScope ? "auto 1fr auto" : "1fr auto",
        columnGap: 10,
      }}
    >
      {hasScope && <ScopeIndicator scope={scope} />}
      <div className="min-w-0 py-1">
        {value ? (
          <p className="text-[16px] text-fg-1 truncate leading-none">{value}</p>
        ) : (
          <p className="text-[16px] text-fg-2 truncate leading-none">{ph}</p>
        )}
      </div>
      <div className="flex items-center gap-1.5">
        {showCamera && mobile && (
          <div
            className="flex items-center justify-center h-8 w-8 text-fg-3 cursor-pointer"
            aria-label="Add photo"
          >
            <Camera className="h-4 w-4" />
          </div>
        )}
        {showCancel && (
          <div className="flex items-center justify-center h-7 w-7 text-fg-3 cursor-pointer">
            <X className="h-3.5 w-3.5" />
          </div>
        )}
        <SendButton kind={sendKind} state={sendState} />
      </div>
    </div>
  );
}

/**
 * ComposeInputRow — zone ①, stacked (multiline) variant. Kitchen 24f
 * pattern: CSS Grid with two templates, instant switch, only gap
 * animates. Shown here as a static expanded state.
 *
 * sendKind defaults to "remember" here (not "recall") because multi-line
 * content is a dump 99% of the time — see the SendButton rule. Callers
 * who somehow want recall from a multi-line compose can pass it
 * explicitly, but there's no kitchen mock that does.
 */
function ComposeInputRow({
  value,
  sendKind = "remember",
  sendState = "ready",
}: {
  value: string;
  sendKind?: "recall" | "remember";
  sendState?: "disabled" | "ready" | "loading";
}) {
  return (
    <div
      className="min-h-14 px-4 sm:px-5 py-2"
      style={{
        display: "grid",
        gridTemplateAreas: '"input input" ".  send"',
        gridTemplateColumns: "1fr auto",
        alignItems: "center",
        gap: "6px 10px",
      }}
    >
      <div className="min-w-0 py-1" style={{ gridArea: "input" }}>
        <p className="text-[16px] text-fg-1 leading-[24px] whitespace-pre-line">
          {value}
        </p>
      </div>
      <div style={{ gridArea: "send" }}>
        <SendButton kind={sendKind} state={sendState} />
      </div>
    </div>
  );
}

// ───────────────────────────────────────────────
// Zone ② — Synthesis slot (reserved through every phase)
// ───────────────────────────────────────────────

type SynthesisState =
  | "placeholder"
  | "thinking"
  | "streaming"
  | "complete"
  | "free"
  | "error";

function SynthesisSlot({
  state,
  text,
  query,
}: {
  state: SynthesisState;
  text?: string;
  query?: string;
}) {
  if (state === "placeholder") {
    return (
      <div className="px-5 sm:px-6 py-3.5 flex items-center gap-2 text-[13px] text-fg-3">
        <StarGlyph />
        {/* i18n: t.bar.ai.placeholder */}
        <span>
          AI answer will appear here after <Kbd>↵</Kbd>
        </span>
      </div>
    );
  }

  if (state === "free") {
    return (
      <div
        className="px-5 sm:px-6 py-3.5 flex items-center gap-2 text-[13px] text-fg-3"
        style={{ background: "oklch(from var(--signature) l c h / 0.03)" }}
      >
        <StarGlyph />
        {/* i18n: t.bar.ai.freeStub */}
        <span className="flex-1">
          AI answers are a Pro feature — recall stays free.
        </span>
        <button className="text-fg-1 font-medium underline underline-offset-2 cursor-pointer">
          {/* i18n: t.billing.upgrade */}
          Upgrade
        </button>
      </div>
    );
  }

  if (state === "error") {
    return (
      <div className="px-5 sm:px-6 py-3.5 flex items-center gap-2 text-[13px] text-fg-3">
        <span
          className="inline-flex items-center justify-center shrink-0 text-fg-3"
          style={{ width: 13, height: 13, fontSize: 13, lineHeight: 1 }}
          aria-hidden
        >
          ⚠
        </span>
        {/* i18n: t.bar.ai.error */}
        <span className="flex-1">
          AI answer failed — sources below are still available.
        </span>
        <button className="text-fg-2 hover:text-fg-1 underline underline-offset-2 cursor-pointer text-[12px]">
          {/* i18n: t.common.retry */}
          Retry
        </button>
      </div>
    );
  }

  if (state === "thinking") {
    return (
      <div
        className="px-5 sm:px-6 py-3.5"
        style={{ background: "oklch(from var(--signature) l c h / 0.03)" }}
      >
        <div className="flex items-start gap-2">
          <StarGlyph breathe className="mt-0.5" />
          {/* i18n: t.bar.ai.thinking + query interpolation */}
          <div className="flex-1 text-[14px] text-fg-3 leading-relaxed">
            Thinking about{" "}
            <span className="text-fg-2">“{query ?? "your question"}”</span>…
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      className="px-5 sm:px-6 py-3.5"
      style={{ background: "oklch(from var(--signature) l c h / 0.03)" }}
    >
      <div className="flex items-start gap-2">
        <StarGlyph breathe={state === "streaming"} className="mt-0.5" />
        <div className="flex-1 text-[14px] text-fg-1 leading-relaxed">
          <CitedText text={text ?? ""} />
        </div>
      </div>
      {state === "complete" && (
        <div className="flex items-center gap-2 mt-3 ml-5">
          <button className="text-[12px] text-fg-3 hover:text-fg-2 px-2 py-1 rounded bg-surface-2 cursor-pointer">
            {/* i18n: t.recall.copy */}
            Copy
          </button>
          <button className="text-[12px] text-fg-3 hover:text-fg-2 px-2 py-1 rounded bg-surface-2 cursor-pointer">
            {/* i18n: t.recall.copyForAI */}
            Copy for AI
          </button>
        </div>
      )}
    </div>
  );
}

/** Inline [N] citation chip — same visual as SourceRow index badge. */
function CitedText({ text }: { text: string }) {
  const parts = text.split(/(\[\d+\])/g);
  return (
    <>
      {parts.map((part, i) => {
        const m = part.match(/^\[(\d+)\]$/);
        if (m) {
          return (
            <span
              key={i}
              className="inline-flex items-center justify-center text-[12px] font-mono text-fg-3 hover:text-fg-2 bg-surface-2 hover:bg-surface-3 rounded px-1.5 py-px mx-0.5 cursor-pointer transition-colors align-baseline"
            >
              {m[1]}
            </span>
          );
        }
        return <span key={i}>{part}</span>;
      })}
    </>
  );
}

// ───────────────────────────────────────────────
// Zone ③ — Remember row (inline feedback replaces BarNotificationCard)
// ───────────────────────────────────────────────

type RememberState = "idle" | "sending" | "sent" | "error";

/**
 * RememberRow — hosts both the idle CTA and the transient save feedback.
 *
 * Always visible while a result surface is open. Morphs in place through
 * sending → sent (with Undo, 4s) → idle, or sending → error (with Retry,
 * 5s auto-dismiss, content restored to input). Hub badge persists through
 * every state when the user is in a multi-hub + team-hub context.
 */
function RememberRow({
  state = "idle",
  query,
  title,
  fileCount,
  hub,
  highlighted,
}: {
  state?: RememberState;
  query?: string;
  title?: string;
  fileCount?: number;
  hub?: string;
  highlighted?: boolean;
}) {
  const baseBg = highlighted ? "bg-surface-1" : "hover:bg-surface-1";

  if (state === "sending") {
    return (
      <div className="flex items-center gap-3 px-4 min-h-11 text-[13px]">
        <StarGlyph breathe />
        {/* i18n: t.bar.remember.sending */}
        <span className="flex-1 text-fg-2 truncate">Sending…</span>
        {hub && <HubBadge name={hub} />}
      </div>
    );
  }

  if (state === "sent") {
    return (
      <div
        className="flex items-center gap-3 px-4 min-h-11 text-[13px]"
        style={{ background: "oklch(0.55 0.15 155 / 0.05)" }}
      >
        <Check
          className="h-3.5 w-3.5 shrink-0"
          style={{ color: "oklch(0.55 0.15 155)" }}
        />
        <span className="flex-1 text-fg-2 truncate">
          {/* i18n: t.bar.remember.sent */}
          <span
            className="font-medium"
            style={{ color: "oklch(0.55 0.15 155)" }}
          >
            Sent to memax.
          </span>
          {title && (
            <span className="text-fg-3"> · “{title.slice(0, 40)}”</span>
          )}
        </span>
        {hub && <HubBadge name={hub} />}
        <button className="flex items-center gap-1 text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer">
          <Undo2 className="h-3 w-3" />
          {/* i18n: t.import.undo */}
          Undo
        </button>
      </div>
    );
  }

  if (state === "error") {
    return (
      <div
        className="flex items-center gap-3 px-4 min-h-11 text-[13px]"
        style={{ background: "oklch(from var(--destructive) l c h / 0.05)" }}
      >
        <X
          className="h-3.5 w-3.5 shrink-0"
          style={{ color: "var(--destructive)" }}
        />
        <span className="flex-1 text-fg-2 truncate">
          {/* i18n: t.bar.remember.error */}
          <span className="font-medium" style={{ color: "var(--destructive)" }}>
            Couldn’t send.
          </span>
          <span className="text-fg-3"> · Network error</span>
        </span>
        <button className="text-[12px] text-fg-3 hover:text-fg-2 underline underline-offset-2 cursor-pointer">
          {/* i18n: t.common.retry */}
          Retry
        </button>
      </div>
    );
  }

  // ── Idle / highlighted: primary action ──
  //
  // Copy decision (see 38a header): "Dump to memax" for invitation voice
  // (matches placeholder "dump or ask…"). The query is visible in the
  // input one line up, so we don't echo it here — cleaner row, action-first.
  // Query prop is accepted for future interpolation variants but unused now.
  void query;
  const label = fileCount
    ? // i18n: t.bar.remember.actionFiles — pluralized
      `Dump ${fileCount} ${fileCount === 1 ? "file" : "files"} to memax`
    : // i18n: t.bar.remember.action — no query interpolation
      "Dump to memax";

  return (
    <div
      className={`flex items-center gap-3 px-4 min-h-11 text-[13px] cursor-pointer transition-colors ${baseBg}`}
    >
      <span className="inline-flex items-center justify-center h-5 w-5 rounded text-fg-2 bg-surface-2 shrink-0">
        <Plus className="h-3 w-3" />
      </span>
      <span className="flex-1 text-fg-1 font-medium truncate">{label}</span>
      {hub && <HubBadge name={hub} />}
      <Kbd>⌘</Kbd>
      <Kbd>↵</Kbd>
    </div>
  );
}

/** Hub target badge — team hub name with → prefix, surface-2 pill. */
function HubBadge({ name }: { name: string }) {
  return (
    <span className="inline-flex items-center gap-1 text-[11px] text-fg-2 bg-surface-2 px-1.5 py-0.5 rounded shrink-0 whitespace-nowrap">
      <ArrowRight className="h-2.5 w-2.5" />
      {name}
    </span>
  );
}

// ───────────────────────────────────────────────
// Zone ④ — Source list
// ───────────────────────────────────────────────

function SourceRow({
  index,
  title,
  snippet,
  relevance,
  topic,
  topicIcon: TopicIconCmp,
  hub,
  highlighted,
}: {
  index?: number;
  title: string;
  snippet?: string;
  relevance?: number;
  topic?: string;
  topicIcon?: LucideIcon;
  hub?: string;
  highlighted?: boolean;
}) {
  return (
    <div
      className={`group flex flex-col px-4 cursor-pointer transition-colors ${
        highlighted ? "bg-surface-1" : "hover:bg-surface-1"
      }`}
    >
      <div className="flex items-center gap-2 min-h-9">
        {index !== undefined && (
          <span className="inline-flex items-center justify-center text-[12px] font-mono text-fg-3 bg-surface-2 rounded px-1.5 py-px shrink-0">
            {index}
          </span>
        )}
        <p className="text-[14px] text-fg-1 font-medium truncate flex-1 min-w-0">
          {title}
        </p>
        {topic && (
          <span className="inline-flex items-center gap-1 text-[11px] text-fg-4 shrink-0">
            {TopicIconCmp && <TopicIconCmp className="h-3 w-3" />}
            {topic}
          </span>
        )}
        {topic && hub && (
          <span className="text-fg-4 text-[10px] shrink-0">·</span>
        )}
        {hub && (
          <span className="text-[11px] px-1.5 py-px rounded bg-surface-1 text-fg-3 shrink-0">
            {hub}
          </span>
        )}
        {relevance !== undefined && (
          <span className="text-[11px] text-fg-3 tabular-nums shrink-0">
            {relevance}%
          </span>
        )}
      </div>
      {snippet && (
        <p
          className="text-[13px] text-fg-3 truncate pb-1.5"
          style={index !== undefined ? { paddingLeft: "26px" } : undefined}
        >
          {snippet}
        </p>
      )}
    </div>
  );
}

/** "+ N more" / Load more affordance at the bottom of zone ④. */
function LoadMoreRow({ count }: { count: number }) {
  return (
    <div className="flex items-center justify-center px-4 py-2 text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer border-t border-border/20">
      {/* i18n: t.bar.loadMore */}
      <span>+ {count} more</span>
    </div>
  );
}

// ───────────────────────────────────────────────
// Zone ⑤ — Hint bar (24m style)
// ───────────────────────────────────────────────

/**
 * HintBar — 24m-style: plain text spans with a single <kbd> glyph per
 * hint, NOT boxy pill chains. Left-aligned primary actions; close on
 * the right. Desktop only (mobile has no keyboard hints).
 */
function HintBar({
  hints,
  trailing,
}: {
  hints: { label: string; key: string }[];
  trailing?: { label: string; key: string };
}) {
  return (
    <div className="px-5 py-2 border-t border-border/30 flex items-center gap-4 text-[11px] text-fg-3">
      {hints.map((h) => (
        <span key={h.label} className="flex items-center gap-1.5">
          <Kbd>{h.key}</Kbd>
          <span>{h.label}</span>
        </span>
      ))}
      {trailing && (
        <span className="flex items-center gap-1.5 ml-auto">
          <Kbd>{trailing.key}</Kbd>
          <span>{trailing.label}</span>
        </span>
      )}
    </div>
  );
}

// ───────────────────────────────────────────────
// Files / URL / drag capture (kitchen 24g pattern)
// ───────────────────────────────────────────────

function StagedChipBar({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-4 sm:px-5 py-2 border-b border-border/30">
      <div className="flex items-center gap-2 flex-wrap">{children}</div>
    </div>
  );
}

function FileChip({
  icon: Icon,
  name,
  uploading,
}: {
  icon: LucideIcon;
  name: string;
  uploading?: boolean;
}) {
  return (
    <span className="flex items-center gap-1.5 px-2 py-1 rounded-lg bg-surface-1 text-[12px] text-fg-2">
      {uploading ? (
        <Loader2 className="h-3 w-3 text-fg-3 animate-spin" />
      ) : (
        <Icon className="h-3 w-3 text-fg-3" />
      )}
      <span className="truncate max-w-32">{name}</span>
      <button className="text-fg-4 hover:text-fg-2 ml-0.5" aria-label="Remove">
        ×
      </button>
    </span>
  );
}

function UrlChip({ url }: { url: string }) {
  return (
    <>
      <span className="flex items-center gap-1.5 px-2 py-1 rounded-lg bg-surface-1 text-[12px] text-fg-2">
        <Link2 className="h-3 w-3 text-fg-3" />
        <span className="truncate max-w-48">{url}</span>
        <button className="text-fg-4 hover:text-fg-2 ml-0.5">×</button>
      </span>
      <span className="text-[11px] text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded cursor-pointer hover:text-fg-2">
        {/* i18n: t.bar.captureUrl */}
        Capture page
      </span>
    </>
  );
}

// ───────────────────────────────────────────────
// Command row — zone ④ variant for slash mode
// ───────────────────────────────────────────────

function CommandRow({
  icon: Icon,
  name,
  desc,
  highlighted,
}: {
  icon: LucideIcon;
  name: string;
  desc: string;
  highlighted?: boolean;
}) {
  return (
    <div
      className={`flex items-center gap-3 px-4 min-h-10 text-[13px] cursor-pointer transition-colors ${
        highlighted ? "bg-surface-1" : "hover:bg-surface-1"
      }`}
    >
      <Icon className="h-3.5 w-3.5 text-fg-3 shrink-0" />
      <span className="text-fg-1 font-medium w-32 shrink-0 truncate">
        {name}
      </span>
      <span className="text-fg-3 truncate flex-1">{desc}</span>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════
// Section entry point
// ═══════════════════════════════════════════════════════════════════

export function BarNorthStarAltSection() {
  return (
    <Section
      title="38. Bar — North Star (alt)"
      description="Second take on the bar redesign. 24m glass · recall default · AI slot always reserved · inline Remember feedback · hub badge · mobile mirror · slash minimal. Sibling to section 37 — read 38q for deltas."
    >
      <DemoCard label="38.0 Sync note — 2026-04-14 production state">
        <div className="space-y-3 text-[13px] text-fg-2">
          <p>
            Production moved to a cleaner engineering model after the initial
            port: one derived interaction state in{" "}
            <span className="font-mono">bar-context.tsx</span>, one explicit
            mobile compose state (
            <span className="font-mono">docked | mirror | fullscreen</span>),
            and thinner render portals that follow that state instead of
            re-deriving behavior locally.
          </p>
          <ul className="list-disc space-y-1.5 pl-5">
            <li>
              Plain <span className="font-mono">/</span> opens command discovery
              only. No chip yet.
            </li>
            <li>
              A command chip appears only after explicit commit: exact{" "}
              <span className="font-mono">/topic</span> or{" "}
              <span className="font-mono">/hub</span>, tapping a command row, or
              pressing <span className="font-mono">↵</span> on a highlighted
              command.
            </li>
            <li>
              Committed command + empty query shows the full target list. Typing
              filters that list. Backspace on empty query degrades the chip back
              to raw slash text.
            </li>
            <li>
              Staged files force remember mode and suppress recall / AI surfaces
              on both desktop and mobile.
            </li>
            <li>
              Remember feedback is owned inline by{" "}
              <span className="font-mono">RememberRow</span>. Global bar
              notifications are ambient-only, not remember lifecycle.
            </li>
            <li>
              Mobile keeps one persistent bottom thumb bar. Mirror/results fade
              in above it. Fullscreen compose is the higher tier, not a second
              bottom bar or a desktop-style expand card.
            </li>
          </ul>
        </div>
      </DemoCard>

      {/* ─── 38a. Principles ─── */}
      <DemoCard label="38a. Principles — ten rules">
        <div className="text-[13px] text-fg-2 space-y-3">
          <p>
            This take optimizes for <em>continuity</em>: the user should see the
            same surface evolve through every phase, not watch zones appear and
            disappear. The bar is memax’s one command palette — glass, downward,
            recall-default, layout-reserved AI, inline save.
          </p>
          <ol className="list-decimal pl-5 space-y-1.5 marker:text-fg-4">
            <li>
              <span className="text-fg-1 font-medium">24m glass chrome.</span>{" "}
              <span className="font-mono text-fg-3">
                oklch(card / 0.92) · blur(24) saturate(140)
              </span>{" "}
              · layered shadow · 1px border{" "}
              <span className="font-mono text-fg-3">/0.5</span>. The whole bar
              feels lifted off the page.
            </li>
            <li>
              <span className="text-fg-1 font-medium">Recall is default.</span>{" "}
              <em>Nothing</em> on the left in zone ① — no icon, no capsule. The
              placeholder (<span className="font-mono">dump or ask…</span>) is
              the affordance. A scope pill appears only for topic / command
              scope. Same lead-with-nothing pattern as Spotlight, ChatGPT,
              Claude, Notion, Linear ⌘K.
            </li>
            <li>
              <span className="text-fg-1 font-medium">Downward cascade.</span>{" "}
              [1] at top, [N] at bottom. Natural ranked-list order.
            </li>
            <li>
              <span className="text-fg-1 font-medium">
                AI slot always reserved.
              </span>{" "}
              Zone ② is present from the first keystroke (dim placeholder),
              through thinking / streaming / complete. Free users see an upgrade
              stub in the <em>same</em> slot — layout never jumps.
            </li>
            <li>
              <span className="text-fg-1 font-medium">
                Remember row is inline + transient.
              </span>{" "}
              Zone ③ is always visible and hosts inline feedback for the save
              itself. Idle CTA reads{" "}
              <span className="font-mono">Dump to memax</span> (matches the
              placeholder voice). Transients use the action verb:{" "}
              <em>Sending… → Sent · Undo → idle</em>, or{" "}
              <em>Couldn&rsquo;t send · Retry</em>. Two voices on purpose —{" "}
              <em>dump</em> invites, <em>send</em> commits. Remember-specific
              notification banners are deprecated; passive notifications remain
              for ambient events only.
            </li>
            <li>
              <span className="text-fg-1 font-medium">
                Hub badge on Remember row.
              </span>{" "}
              Multi-hub + team hub →{" "}
              <span className="font-mono">→{"{hub}"}</span> badge persists
              through every state. Single hub / personal: no badge.
            </li>
            <li>
              <span className="text-fg-1 font-medium">
                Stay put, rise on engage.
              </span>{" "}
              Bar REST position is route-aware:{" "}
              <span className="font-mono">42vh</span> on{" "}
              <span className="font-mono">/home</span>, docked at
              32px-from-bottom on <span className="font-mono">/memories</span>{" "}
              and overlay routes, above-dock on mobile. Real interaction lifts
              it to <span className="font-mono">top 24vh</span> with a{" "}
              <span className="font-mono">0.45s</span> spring. Result surface
              caps at <span className="font-mono">max-h-[60vh]</span> + &ldquo;+
              N more&rdquo; in zone ④.
            </li>
            <li>
              <span className="text-fg-1 font-medium">Mobile is a mirror.</span>{" "}
              Notion pattern: bar stays above keyboard (thumb zone), mirror
              input at top of screen (eye zone), results between. Edge-to-edge,
              no card. A 📷 camera icon in zone ① opens the native photo / file
              picker when the input is empty + no files staged. Three exits: X,
              swipe-down, tap scrim.
            </li>
            <li>
              <span className="text-fg-1 font-medium">
                Slash is minimal + route-aware.
              </span>{" "}
              Three commands — <span className="font-mono">/topic</span>,{" "}
              <span className="font-mono">/hub</span>,{" "}
              <span className="font-mono">/forget</span>. Plain{" "}
              <span className="font-mono">/</span> is discovery only; partial
              prefixes like <span className="font-mono">/h</span> rank the
              closest command but do not commit the chip yet.{" "}
              <span className="font-mono">/forget</span> remains{" "}
              <em>route-scoped</em>: only appears in the command list when the
              user is on <span className="font-mono">/memories/{"{id}"}</span>{" "}
              and forgets <em>that</em> mounted memory — never an abstract
              &ldquo;highlighted row.&rdquo; Hidden everywhere else.
            </li>
            <li>
              <span className="text-fg-1 font-medium">
                Real glyphs, 24m hint bar.
              </span>{" "}
              <Kbd>↵</Kbd> <Kbd>⇧</Kbd> <Kbd>⌘</Kbd> <Kbd>⌥</Kbd> <Kbd>esc</Kbd>{" "}
              <Kbd>↑</Kbd> <Kbd>↓</Kbd> — literal Unicode in{" "}
              <span className="font-mono">&lt;kbd&gt;</span>. Hint bar is 24m
              style: plain text + single-glyph chips, not pill chains.
            </li>
          </ol>
          <div className="mt-3 rounded-lg border border-destructive/20 bg-surface-1 px-4 py-3 text-[12px] text-fg-3 space-y-1.5">
            <p className="text-fg-2 font-medium">
              Deprecated patterns — do not reintroduce
            </p>
            <p>
              Two old intent-routing mechanisms are gone and must not come back
              in the production port:
            </p>
            <ul className="list-disc pl-5 space-y-0.5">
              <li>
                <span className="font-mono">
                  packages/web/src/lib/detect-intent.ts
                </span>{" "}
                — the EN/ZH regex sniffer (question words,
                什么/怎么/为什么/吗/呢). Replaced by the content-shape rule:
                newline or files ⇒ dump, else ⇒ recall. Delete the file on port.
              </li>
              <li>
                <span className="font-mono">?</span> prefix recall trigger
                (kitchen 24 proposal) — never shipped to production, and
                won&rsquo;t. The user asks in natural language; the bar infers
                action from content shape, not magic characters.
              </li>
              <li>
                Mode capsule tap-to-toggle (kitchen 31 ModeExpandedCard).
                Already covered by §2 above, but worth repeating: the capsule in
                38 is a read-only scope indicator, not a mode switch.
              </li>
              <li>
                <Kbd>✦</Kbd> as a send-button CTA icon (kitchen 38 pre
                2026-04-13). Conflated the &ldquo;AI is working&rdquo; signal
                with an action affordance and violated the design-system rule{" "}
                <em>signature = intelligence, never decorative</em>. The send
                button is now unified <Kbd>↑</Kbd> (color communicates mode),
                matching ChatGPT / Claude / Perplexity. <Kbd>✦</Kbd> stays
                sacred to zone ②.
              </li>
            </ul>
          </div>
          <div className="mt-3 rounded-lg bg-surface-1 px-4 py-3 text-[12px] text-fg-3 space-y-1">
            <p className="text-fg-2 font-medium">
              Open question — dream notifications
            </p>
            <p>
              Deleting <span className="font-mono">BarNotificationCard</span>{" "}
              also deletes today’s dream event surface (dream · dreaming ·
              dream_complete). Three options to decide before porting:
            </p>
            <ul className="list-disc pl-5 space-y-0.5">
              <li>Dock badge on mobile + header chip on desktop (ambient).</li>
              <li>
                Reuse <span className="font-mono">MemaxStatusStrip</span>{" "}
                (already exists for section-level progress).
              </li>
              <li>
                Toast top-center — deviates from &ldquo;no overlay when the
                surface can morph&rdquo; but is the simplest port.
              </li>
            </ul>
            <p>
              Flag for product decision. Dream is memax’s differentiator — the
              surface matters.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ─── 38b. Anatomy ─── */}
      <DemoCard label="38b. Anatomy — five stacked zones, always in this order">
        <p className="text-[12px] text-fg-3 mb-4">
          Glass container, five zones stacked top-to-bottom. Zones ②③ can be
          empty but never reorder. Zone ⑤ (hint bar) only appears when the
          result surface is open and on desktop.
        </p>
        <GlassChrome caption="anatomy reference — not a real state">
          <ZoneLabel index="①" label="Input row — scope · textarea · send" />
          <ZoneRule />
          <ZoneLabel
            index="②"
            label="Synthesis slot — reserved for both tiers"
          />
          <ZoneRule />
          <ZoneLabel
            index="③"
            label="Remember row — conversion CTA + inline feedback"
          />
          <ZoneRule />
          <ZoneLabel
            index="④"
            label="Source list — ranked [1] at top, load more at bottom"
          />
          <ZoneRule />
          <ZoneLabel index="⑤" label="Hint bar — contextual keyboard hints" />
        </GlassChrome>
      </DemoCard>

      {/* ─── 38c. Position ─── */}
      <DemoCard label="38c. Position — stay put, rise to 24vh on engage">
        <p className="text-[12px] text-fg-3 mb-4">
          The bar&rsquo;s <em>resting</em> position is unchanged from
          production: <span className="font-mono">42vh</span> centered on{" "}
          <span className="font-mono">/home</span>, 32px-from-bottom on{" "}
          <span className="font-mono">/memories</span> and overlay routes,
          above-dock on mobile.{" "}
          <em>Same fade-in / fade-out animation as today.</em> What changes is
          the <em>engaged</em> position — once real interaction exists, the bar
          animates to <span className="font-mono">top 24vh</span> (center-upper)
          with the existing <span className="font-mono">0.45s</span> spring.
          Result surface caps at <span className="font-mono">max-h-[60vh]</span>{" "}
          with a &ldquo;+ N more&rdquo; affordance at the bottom of zone ④. On{" "}
          <Kbd>esc</Kbd> the bar returns to its rest position.
        </p>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col items-center gap-2">
            <ViewportFrame>
              <div className="absolute left-0 right-0 top-[42%] -translate-y-1/2 px-4">
                <GlassChrome>
                  <InputRow />
                </GlassChrome>
              </div>
            </ViewportFrame>
            <p className="text-[11px] text-fg-3">
              rest · <span className="font-mono">top 42vh</span> · /home (hero
              moment, unchanged)
            </p>
          </div>
          <div className="flex flex-col items-center gap-2">
            <ViewportFrame>
              <div className="absolute left-0 right-0 top-[24%] px-4">
                <GlassChrome>
                  <InputRow value="auth architecture" sendState="ready" />
                  <ZoneRule />
                  <SynthesisSlot state="placeholder" />
                  <ZoneRule />
                  <RememberRow query="auth architecture" hub="memax-team" />
                  <ZoneRule />
                  <SourceRow
                    title="Auth Middleware"
                    snippet="JWT + API key dual path…"
                    topic="Auth"
                    topicIcon={ShieldCheck}
                  />
                  <SourceRow
                    title="Security Rules"
                    snippet="Owner isolation…"
                    topic="Security"
                    topicIcon={ShieldCheck}
                  />
                  <LoadMoreRow count={12} />
                </GlassChrome>
              </div>
            </ViewportFrame>
            <p className="text-[11px] text-fg-3">
              engaged · <span className="font-mono">top 24vh</span> ·{" "}
              <span className="font-mono">max-h-[60vh]</span> · + Load more
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ─── 38d. Typing ─── */}
      <DemoCard label="38d. Typing — all zones live, AI placeholder reserved">
        <p className="text-[12px] text-fg-3 mb-4">
          First keystroke opens the result surface. Zone ② holds a dim AI
          placeholder (same for Pro/free — layout doesn’t jump). Zone ③ is the
          explicit save affordance. Zone ④ live-updates as keyword + FTS results
          arrive. No citation numbers yet.
        </p>
        <div className="space-y-5">
          <GlassChrome caption="engaged · empty · focused (no query)">
            <InputRow placeholder="ask memax or / for commands" />
            <HintBar
              hints={[
                { key: "/", label: "commands" },
                { key: "↵", label: "recall" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>

          <GlassChrome caption="typing · t = 0–200ms · keyword + FTS">
            <InputRow value="auth architecture" sendState="ready" />
            <ZoneRule />
            <SynthesisSlot state="placeholder" />
            <ZoneRule />
            <RememberRow query="auth architecture" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              title="Auth Middleware Design"
              snippet="JWT + API key dual path, agent_name from key…"
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
            <SourceRow
              title="Security Rules"
              snippet="Owner isolation at data layer, every query filters by owner_id…"
              topic="Security"
              topicIcon={ShieldCheck}
              hub="memax-team"
            />
            <SourceRow
              title="MCP OAuth JWT Fix"
              snippet="Added AgentName claim to JWT, MCP OAuth tokens carry identity…"
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
            <HintBar
              hints={[
                { key: "↵", label: "recall" },
                { key: "⌘↵", label: "remember" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38e. Compose morph ─── */}
      <DemoCard label="38e. Compose — ChatGPT-style auto-grow, CSS Grid stacked">
        <p className="text-[12px] text-fg-3 mb-4">
          Zone ① morphs when the user pastes or writes multiline text. CSS Grid
          two templates, instant switch, only{" "}
          <span className="font-mono">gap</span> animates (0.2s spring).
          Auto-grow via <span className="font-mono">useLayoutEffect</span> +{" "}
          <span className="font-mono">scrollHeight</span>, bounded at 200px
          before a thin scrollbar appears.
        </p>
        <div className="space-y-4">
          <GlassChrome caption="collapsed · single-line · [scope input send]">
            <InputRow
              value="Our auth uses JWT + API key dual path"
              sendState="ready"
            />
          </GlassChrome>
          <GlassChrome caption="expanded · multiline · [input full] / [scope . send]">
            <ComposeInputRow
              value={
                "Meeting notes — bar redesign review:\n- 24m glass adopted, search icon default\n- Remember row replaces notification card\n- Mobile = Notion mirror"
              }
              sendState="ready"
            />
          </GlassChrome>
          <GlassChrome caption="expanded + result surface visible">
            <ComposeInputRow
              value={
                "how do we handle JWT refresh when the token\nis close to expiry and the user is offline"
              }
              sendState="ready"
            />
            <ZoneRule />
            <SynthesisSlot state="placeholder" />
            <ZoneRule />
            <RememberRow query="JWT refresh when offline" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              title="Auth Middleware Design"
              snippet="Refresh rotation, offline grace window…"
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38e2. Mobile compose tiers ─── */}
      <DemoCard label="38e2. Mobile compose — two tiers, in-place then full-screen">
        <p className="text-[12px] text-fg-3 mb-4">
          Compose on mobile is a two-tier experience. Short content stays in the
          thumb-zone bar (Tier 1, in-place growth). Long content escapes to a
          full-screen takeover (Tier 2, Apple Notes pattern) where the only
          thing on screen is the textarea, a back chevron, and a save button.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          <span className="text-fg-1 font-medium">Tier 1</span> — bar textarea
          grows vertically from one line up to ~3 lines (~150px). The mirror at
          the top echoes the multi-line content in sync — they stay visually
          consistent. Source list shrinks to give the bar room but stays
          scrollable. Auto-grow via{" "}
          <span className="font-mono">useLayoutEffect</span> +{" "}
          <span className="font-mono">scrollHeight</span>, same JS pattern as
          desktop.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          <span className="text-fg-1 font-medium">Tier 2</span> — full-screen
          compose takeover. Triggered automatically when the textarea would
          exceed 3 lines, OR manually by tapping the{" "}
          <Maximize2 className="inline h-3 w-3 align-middle mx-0.5" /> expand
          icon that appears next to the send button as soon as there&rsquo;s
          content. In Tier 2 the mirror, source list, and remember row all hide
          — the bar <em>is</em> the screen. Back chevron (top-left) returns to
          the bar in Tier 1 state, preserving the content. Save button
          (top-right) commits and returns to the resting dock.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Inspiration: Apple Notes, Gmail compose, Notion&rsquo;s mobile block
          editor. The user is either browsing (Tier 0 resting dock / Tier 1
          recall-then-type) OR writing (Tier 2 fullscreen). The two modes feel
          distinct.
        </p>
        <div className="my-4 rounded-lg border border-border/40 bg-surface-1 px-4 py-3 text-[12px] text-fg-2 space-y-2">
          <p className="text-fg-1 font-medium">
            Send button — always <Kbd>↑</Kbd>. Color says what fires.
          </p>
          <p className="text-fg-3">
            The icon is always <Kbd>↑</Kbd> — same as ChatGPT, Claude,
            Perplexity, Gemini, Arc. Universal 2026 chat-input convention, zero
            learning curve. The{" "}
            <span className="text-fg-1 font-medium">background color</span>{" "}
            tells the user what tapping will fire. One rule, four cases:
          </p>
          <ul className="text-[12px] space-y-1 list-disc pl-5 text-fg-2">
            <li>
              <span
                className="inline-flex items-center justify-center h-4 w-4 rounded-sm align-middle mx-0.5 text-[9px]"
                style={{
                  background: "oklch(from var(--fg-4) l c h / 0.2)",
                  color: "var(--fg-4)",
                }}
              >
                ↑
              </span>{" "}
              empty → disabled, ghost outline
            </li>
            <li>
              <span
                className="inline-flex items-center justify-center h-4 w-4 rounded-sm align-middle mx-0.5 text-[9px]"
                style={{ background: "var(--signature)", color: "#fff" }}
              >
                ↑
              </span>{" "}
              single-line, no files → signature violet fill → tap fires{" "}
              <span className="text-fg-1">recall + AI</span>
            </li>
            <li>
              <span
                className="inline-flex items-center justify-center h-4 w-4 rounded-sm align-middle mx-0.5 text-[9px]"
                style={{
                  background: "var(--foreground)",
                  color: "var(--background)",
                }}
              >
                ↑
              </span>{" "}
              text contains <span className="font-mono">\n</span> (Return
              pressed) → dark foreground fill → tap fires{" "}
              <span className="text-fg-1">dump</span>
            </li>
            <li>
              <span
                className="inline-flex items-center justify-center h-4 w-4 rounded-sm align-middle mx-0.5 text-[9px]"
                style={{
                  background: "var(--foreground)",
                  color: "var(--background)",
                }}
              >
                ↑
              </span>{" "}
              files staged → dark foreground fill → tap fires{" "}
              <span className="text-fg-1">dump</span> (files lock the bar to
              remember)
            </li>
          </ul>
          <p className="text-fg-3">
            Why explicit <span className="font-mono">\n</span> and not visual
            wrap: a long single line that just wraps isn&rsquo;t multi-paragraph
            — it&rsquo;s still a recall query. Only a deliberate Return signals
            composition. Robust and predictable.
          </p>
          <p className="text-fg-3">
            Why unified <Kbd>↑</Kbd> instead of <Kbd>✦</Kbd> for recall:{" "}
            <Kbd>✦</Kbd> is the memax design system&rsquo;s &ldquo;intelligence
            is working&rdquo; glyph — reserved for zone ②&rsquo;s AI thinking
            state. Using it as a CTA icon conflates two meanings and violates
            the &ldquo;signature = intelligence, never decorative&rdquo; rule.
            Unified <Kbd>↑</Kbd> lets <Kbd>✦</Kbd> stay sacred to the synthesis
            slot.
          </p>
          <p className="text-fg-3">
            Desktop override: <Kbd>⌘</Kbd>
            <Kbd>↵</Kbd> forces dump from any state (edge case: short
            single-line note worth saving). No recall-forcer — if you&rsquo;re
            composing and want to search, delete the newlines. Edge case,
            acceptable friction. The Remember row in zone ③ never disappears, so
            users always see a second dump affordance.
          </p>
        </div>
        <div className="grid grid-cols-2 gap-4">
          <MobileComposeFrame tier={1} />
          <MobileComposeFrame tier={2} />
        </div>
        <div className="mt-5 rounded-lg bg-surface-1 px-4 py-3 text-[12px] text-fg-3 space-y-1.5">
          <p className="text-fg-2 font-medium">Transition rules:</p>
          <ul className="list-disc pl-5 space-y-0.5">
            <li>
              resting dock → tap bar → Tier 1 compose (single-line, mirror
              echoes query)
            </li>
            <li>
              Tier 1 → user keeps typing → textarea grows 2 → 3 lines, mirror
              grows with it
            </li>
            <li>
              Tier 1 → tap <Maximize2 className="inline h-3 w-3 align-middle" />{" "}
              OR textarea reaches 4 lines → Tier 2 fullscreen
            </li>
            <li>
              Tier 2 → tap <ArrowLeft className="inline h-3 w-3 align-middle" />{" "}
              back → return to Tier 1 (content preserved)
            </li>
            <li>
              Tier 2 → tap Save → Remember flow fires → return to resting dock
            </li>
            <li>
              Tier 2 → tap <Kbd>esc</Kbd> (hardware keyboard if attached) →
              cancel (discard guard)
            </li>
            <li>
              In Tier 2, drag-drop doesn&rsquo;t apply (mobile). Photo
              attachment via the{" "}
              <Camera className="inline h-3 w-3 align-middle" /> icon in the
              toolbar above the keyboard.
            </li>
          </ul>
        </div>
        <p className="text-[12px] text-fg-3 mt-4">
          <span className="text-fg-1 font-medium">Web drag-drop</span> is
          orthogonal to compose state — the dashed signature chrome (see 38f)
          fires from the container regardless of whether the textarea is
          collapsed or expanded. Dropping files onto a compose-expanded bar
          stages chips in the usual place above the input row and locks the bar
          to remember. One less thing to worry about on desktop.
        </p>
      </DemoCard>

      {/* ─── 38e3. Mobile lifecycle ─── */}
      <DemoCard label="38e3. Mobile lifecycle — 6 states, every transition explicit">
        <p className="text-[12px] text-fg-3 mb-4">
          The full mobile journey as a filmstrip. This card exists because 38n +
          38e2 show the <em>component</em>-level details but leave the
          end-to-end flow implicit. If you&rsquo;re porting the mobile branch
          and piecing states together from two cards, read THIS card first. It
          is the single source of truth for the mobile state machine.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Production wiring:
          [(app)/layout.tsx](../../packages/web/src/app/(app)/layout.tsx){" "}
          already branches on <span className="font-mono">useIsMobile()</span>.
          The current north-star branch is cleaner than the earliest port:
          mobile uses one explicit compose state (
          <span className="font-mono">docked | mirror | fullscreen</span>) and
          keeps one persistent thumb-zone bar at the bottom. Scrim + mirror +
          results render above it in mirror mode; fullscreen is the higher tier.
          Portal slots (<span className="font-mono">#bar-input-slot</span>,{" "}
          <span className="font-mono">#bar-right-slot</span>,{" "}
          <span className="font-mono">#bar-logo-slot</span>) still work — they
          just target the shared mobile row instead of a separate desktop card.
        </p>

        {/* Filmstrip: 2 rows × 3 frames */}
        <div className="space-y-5">
          <div className="grid grid-cols-3 gap-3">
            <MobileLifecycleFrame
              state="dock"
              label="state 0 · resting dock"
              sublabel="above dock, single-line, no scrim"
            />
            <MobileLifecycleFrame
              state="mirror"
              label="state 2 · mirror compose"
              sublabel="tap bar → scrim + mirror slide down"
            />
            <MobileLifecycleFrame
              state="tier1"
              label="state 3 · mirror grow"
              sublabel="multi-line write still keeps one persistent bottom bar"
            />
          </div>
          <div className="grid grid-cols-3 gap-3">
            <MobileLifecycleFrame
              state="tier2"
              label="state 4 · tier 2 fullscreen"
              sublabel="explicit higher writing tier → Apple Notes takeover"
            />
            <MobileLifecycleFrame
              state="result"
              label="state 5 · post-↑ result"
              sublabel="AI streams into mirror (recall)"
            />
            <MobileLifecycleFrame
              state="exit"
              label="state 6 · exit paths"
              sublabel="X · swipe-down · tap scrim → state 0"
            />
          </div>
        </div>

        <p className="text-[12px] text-fg-3 mt-5 mb-2">
          Note:{" "}
          <span className="text-fg-2 font-medium">
            state 1 (compose entry transition)
          </span>{" "}
          is not mocked as its own frame — it&rsquo;s the animation between
          state 0 and state 2. See the transition table below for timing and
          mechanics.
        </p>

        {/* Transition table */}
        <div className="mt-5 overflow-x-auto">
          <table className="w-full text-[12px] text-fg-2">
            <thead>
              <tr className="text-left text-fg-3 border-b border-border/30">
                <th className="py-1.5 pr-3">From</th>
                <th className="py-1.5 pr-3">To</th>
                <th className="py-1.5 pr-3">Trigger</th>
                <th className="py-1.5 pr-3">Duration</th>
                <th className="py-1.5">What animates</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/20">
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">dock</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">mirror</td>
                <td className="py-1.5 pr-3">tap bar / focus</td>
                <td className="py-1.5 pr-3">0.35s spring</td>
                <td className="py-1.5 text-fg-3">
                  scrim fade-in · mirror slide down from top · dock stays
                  mounted as the single editable bar · keyboard opens (native)
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">mirror</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">tier1</td>
                <td className="py-1.5 pr-3">newline / multi-line growth</td>
                <td className="py-1.5 pr-3">
                  instant template switch · gap 0.2s
                </td>
                <td className="py-1.5 text-fg-3">
                  same bottom bar grows in place · mirror tracks content ·
                  source list shrinks · send color flips violet → dark
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">tier1</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">tier2</td>
                <td className="py-1.5 pr-3">
                  explicit expand / compose takeover{" "}
                  <Maximize2 className="inline h-3 w-3 align-middle" />
                </td>
                <td className="py-1.5 pr-3">0.3s spring</td>
                <td className="py-1.5 text-fg-3">
                  mirror collapse to 0 · source list hide · remember row hide ·
                  top bar slide-in · textarea expand to 60vh
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">tier2</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">tier1</td>
                <td className="py-1.5 pr-3">
                  tap <ArrowLeft className="inline h-3 w-3 align-middle" /> back
                </td>
                <td className="py-1.5 pr-3">0.3s spring</td>
                <td className="py-1.5 text-fg-3">
                  reverse of above · content preserved (no discard)
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">mirror</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">result</td>
                <td className="py-1.5 pr-3">tap send (↑ violet)</td>
                <td className="py-1.5 pr-3">0.2s FAST</td>
                <td className="py-1.5 text-fg-3">
                  mirror content swaps from query echo → thinking → streaming AI
                  · send becomes spinner · source list citations hydrate
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">tier1/2</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">dock</td>
                <td className="py-1.5 pr-3">tap send (↑ dark) OR Save</td>
                <td className="py-1.5 pr-3">optimistic instant · 0.35s exit</td>
                <td className="py-1.5 text-fg-3">
                  fireRemember fires · scrim fade-out · mirror slide up · dock
                  remains the same bar · Sent transient stays inline
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">any</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">dock</td>
                <td className="py-1.5 pr-3">
                  <Kbd>X</Kbd> button (layered)
                </td>
                <td className="py-1.5 pr-3">0.35s spring</td>
                <td className="py-1.5 text-fg-3">
                  peels back one layer per tap (edit → revert → clear → exit)
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">any</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">dock</td>
                <td className="py-1.5 pr-3">swipe-down on source list</td>
                <td className="py-1.5 pr-3">follows finger · 0.25s settle</td>
                <td className="py-1.5 text-fg-3">
                  iOS modal dismiss · content preserved · rubber-band if pulled
                  past threshold
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-mono text-[11px]">any</td>
                <td className="py-1.5 pr-3 font-mono text-[11px]">dock</td>
                <td className="py-1.5 pr-3">tap scrim above mirror</td>
                <td className="py-1.5 pr-3">0.35s spring</td>
                <td className="py-1.5 text-fg-3">
                  reverse of entry · content preserved · scrim fade + mirror
                  slide + dock fade
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        {/* Production wiring notes */}
        <div className="mt-6 rounded-lg bg-surface-1 px-4 py-3 text-[12px] text-fg-3 space-y-2">
          <p className="text-fg-2 font-medium">
            Production wiring (what codex actually touches)
          </p>
          <ul className="list-disc pl-5 space-y-1">
            <li>
              <span className="font-mono">useIsMobile()</span> already exists.{" "}
              <span className="font-mono">bar-context.tsx</span> reads it and
              exposes <span className="font-mono">isMobile</span> on the
              context. The mobile branch of{" "}
              <span className="font-mono">(app)/layout.tsx</span> now also owns
              a single mobile compose enum state instead of competing booleans.
            </li>
            <li>
              <span className="text-fg-2 font-medium">Scrim</span> — new. Mount
              a fixed-position overlay div when mobile enters mirror or
              fullscreen compose. Style:{" "}
              <span className="font-mono">
                bg: oklch(from var(--background) l c h / 0.88); backdrop-filter:
                blur(8px); z-index: 40
              </span>
              . Click handler on the scrim = tap-scrim exit (State 6).
            </li>
            <li>
              <span className="text-fg-2 font-medium">Mirror</span> — new. Mount
              above the scrim using{" "}
              <span className="font-mono">MOBILE_OVERLAY_CONTENT_TOP</span>
              (defined in <span className="font-mono">lib/layout.ts</span>).
              Bind to the same <span className="font-mono">value</span> from bar
              context. It&rsquo;s a read-only{" "}
              <span className="font-mono">&lt;p&gt;</span>, NOT a second
              textarea. Line count echoes the real textarea below so they grow
              in sync during Tier 1.
            </li>
            <li>
              <span className="text-fg-2 font-medium">Tier 2 takeover</span> —
              new. Mount a full-screen absolute layer when{" "}
              <span className="font-mono">isMobile && tier === 2</span>.
              Contains its own top bar, textarea, and bottom toolbar. The
              existing bar portal content is hidden (not unmounted — state
              preservation).
            </li>
            <li>
              <span className="text-fg-2 font-medium">
                Resting bar and dock
              </span>{" "}
              — already exist. Don&rsquo;t touch{" "}
              <span className="font-mono">mobile-dock.tsx</span> (kitchen 30).
              It&rsquo;s orthogonal to the bar lifecycle.
            </li>
            <li>
              <span className="text-fg-2 font-medium">Keyboard avoidance</span>{" "}
              — the viewport already has{" "}
              <span className="font-mono">
                interactiveWidget: resizes-content
              </span>{" "}
              set in <span className="font-mono">app/layout.tsx</span>. When
              keyboard opens, the viewport shrinks and the bar stays anchored to
              the bottom automatically. No manual offset math needed.
            </li>
            <li>
              <span className="text-fg-2 font-medium">
                Exit handler precedence
              </span>{" "}
              — three paths (X tap, swipe-down, scrim tap) all route through the
              same <span className="font-mono">exitCompose()</span> function in
              bar-context. Don&rsquo;t duplicate the logic in each handler — one
              source of truth.
            </li>
          </ul>
        </div>

        {/* State machine summary */}
        <div className="mt-4 rounded-lg border border-border/40 bg-surface-1 px-4 py-3 text-[12px] text-fg-2 space-y-2">
          <p className="text-fg-1 font-medium">
            State machine — the 7 names you need to know
          </p>
          <ul className="text-[12px] space-y-0.5 list-disc pl-5 text-fg-2">
            <li>
              <span className="font-mono">state 0 · dock</span> — resting,
              single-line, above mobile dock
            </li>
            <li>
              <span className="font-mono">state 1 · entry</span> — 0.35s
              transition (not a standing state)
            </li>
            <li>
              <span className="font-mono">state 2 · mirror</span> — active
              compose, single-line, mirror echoes query
            </li>
            <li>
              <span className="font-mono">state 3 · mirror grow</span> — same
              bottom bar + mirror growing in sync, multi-line
            </li>
            <li>
              <span className="font-mono">state 4 · tier2</span> — fullscreen
              takeover, Apple Notes
            </li>
            <li>
              <span className="font-mono">state 5 · result</span> — post-send,
              AI streaming (recall) OR sent transient (dump)
            </li>
            <li>
              <span className="font-mono">state 6 · exit</span> — 0.35s
              transition back to state 0
            </li>
          </ul>
        </div>
      </DemoCard>

      <BarEngineeringStateMachine />

      {/* ─── 38f. File & URL ─── */}
      <DemoCard label="38f. File & URL — drag-drop, paste, eager upload">
        <p className="text-[12px] text-fg-3 mb-4">
          Files stage as chips above the input. Drag-over flips the chrome to a
          dashed signature border. URL paste auto-detects and offers capture.
          Eager upload (ChatGPT pattern) — files start uploading on drop. When
          files are staged the bar locks to remember (send = ↑).
        </p>
        <div className="space-y-4">
          <GlassChrome caption="single image pasted">
            <StagedChipBar>
              <FileChip icon={ImageIcon} name="screenshot.png" />
            </StagedChipBar>
            <InputRow
              value="Auth flow diagram"
              sendKind="remember"
              sendState="ready"
            />
            <ZoneRule />
            <RememberRow
              query="Auth flow diagram"
              fileCount={1}
              hub="memax-team"
            />
          </GlassChrome>

          <GlassChrome caption="batch · 3 files · one still uploading">
            <StagedChipBar>
              <FileChip icon={FileText} name="design-spec.md" />
              <FileChip icon={ImageIcon} name="mockup.png" />
              <FileChip icon={Paperclip} name="notes.pdf" uploading />
            </StagedChipBar>
            <InputRow
              value="Design review materials"
              sendKind="remember"
              sendState="loading"
            />
            <ZoneRule />
            <RememberRow
              query="Design review materials"
              fileCount={3}
              hub="memax-team"
            />
          </GlassChrome>

          <GlassChrome caption="URL pasted · Capture page offered">
            <StagedChipBar>
              <UrlChip url="github.com/MemaxLabs/memax-internal/pull/42" />
            </StagedChipBar>
            <InputRow sendKind="remember" sendState="ready" />
          </GlassChrome>

          <GlassChrome
            dragActive
            caption="drag active · dashed signature border"
          >
            <InputRow placeholder="Drop files here…" />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38g. Searching → streaming → complete ─── */}
      <DemoCard label="38g. Searching → streaming → complete — Pro, AI top-of-surface">
        <p className="text-[12px] text-fg-3 mb-4">
          On <Kbd>↵</Kbd>: query locks, send spins, semantic recall + AI
          synthesis fire in parallel. Zone ② flips placeholder → thinking →
          streaming → complete. Citations in zone ④ — [1] at top.
        </p>
        <div className="space-y-5">
          <GlassChrome caption="searching · results hydrated, AI thinking">
            <InputRow
              value="auth architecture"
              sendState="loading"
              showCancel
            />
            <ZoneRule />
            <SynthesisSlot state="thinking" query="auth architecture" />
            <ZoneRule />
            <RememberRow query="auth architecture" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              index={1}
              title="Agent Identity Architecture"
              snippet="Auth = identity. API key → agent_name, OAuth JWT → claim…"
              relevance={98}
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
            <SourceRow
              index={2}
              title="MCP OAuth JWT Fix"
              snippet="Added AgentName claim to JWT…"
              relevance={91}
              topic="Authentication"
              topicIcon={ShieldCheck}
              hub="memax-team"
            />
            <SourceRow
              index={3}
              title="Security Rules"
              snippet="Owner isolation at data layer…"
              relevance={74}
              topic="Security"
              topicIcon={ShieldCheck}
            />
          </GlassChrome>

          <GlassChrome caption="streaming · tokens appending live">
            <InputRow
              value="auth architecture"
              sendState="loading"
              showCancel
            />
            <ZoneRule />
            <SynthesisSlot
              state="streaming"
              text="Memax uses a dual auth architecture [1]: API keys carry agent_name for CLI/MCP, while OAuth JWTs embed agent_name as a claim [2]."
            />
            <ZoneRule />
            <RememberRow query="auth architecture" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              index={1}
              title="Agent Identity Architecture"
              snippet="Auth = identity. API key → agent_name, OAuth JWT → claim…"
              relevance={98}
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
            <SourceRow
              index={2}
              title="MCP OAuth JWT Fix"
              snippet="Added AgentName claim to JWT…"
              relevance={91}
              topic="Authentication"
              topicIcon={ShieldCheck}
              hub="memax-team"
            />
            <SourceRow
              index={3}
              title="Security Rules"
              snippet="Owner isolation at data layer…"
              relevance={74}
              topic="Security"
              topicIcon={ShieldCheck}
            />
          </GlassChrome>

          <GlassChrome caption="complete · ✦ static · copy toolbar · query preserved">
            <InputRow value="auth architecture" sendState="ready" />
            <ZoneRule />
            <SynthesisSlot
              state="complete"
              text="Memax uses a dual auth architecture [1]: API keys carry agent_name for CLI/MCP, while OAuth JWTs embed agent_name as a claim [2]. Owner isolation [3] is enforced at the data layer."
            />
            <ZoneRule />
            <RememberRow query="auth architecture" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              index={1}
              title="Agent Identity Architecture"
              snippet="Auth = identity. API key → agent_name, OAuth JWT → claim…"
              relevance={98}
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
            <SourceRow
              index={2}
              title="MCP OAuth JWT Fix"
              snippet="Added AgentName claim to JWT…"
              relevance={91}
              topic="Authentication"
              topicIcon={ShieldCheck}
              hub="memax-team"
            />
            <SourceRow
              index={3}
              title="Security Rules"
              snippet="Owner isolation at data layer…"
              relevance={74}
              topic="Security"
              topicIcon={ShieldCheck}
            />
            <HintBar
              hints={[
                { key: "↵", label: "open" },
                { key: "⌘⌫", label: "forget" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38h. Free vs Pro ─── */}
      <DemoCard label="38h. Free vs Pro — zone ② reserved for both tiers">
        <p className="text-[12px] text-fg-3 mb-4">
          Layout identical. Only zone ② content differs: Pro streams the answer,
          free shows a dim upgrade stub in the exact same slot. Zones ③④ sit at
          the same vertical position on both.
        </p>
        <div className="grid grid-cols-2 gap-4">
          <GlassChrome caption="free · upgrade stub in zone ②">
            <InputRow value="deployment" sendState="ready" />
            <ZoneRule />
            <SynthesisSlot state="free" />
            <ZoneRule />
            <RememberRow query="deployment" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              index={1}
              title="Deployment Process"
              snippet="Fly.io two-process deploy…"
              relevance={95}
              topic="Infrastructure"
              topicIcon={Wrench}
            />
            <SourceRow
              index={2}
              title="CI/CD Pipeline"
              snippet="GitHub Actions workflow…"
              relevance={78}
              topic="Infrastructure"
              topicIcon={Wrench}
            />
          </GlassChrome>
          <GlassChrome caption="pro · streamed answer in zone ②">
            <InputRow value="deployment" sendState="ready" />
            <ZoneRule />
            <SynthesisSlot
              state="complete"
              text="Deploy uses Fly.io with two processes [1]: API server and worker. CI/CD [2] runs via GitHub Actions on push to main."
            />
            <ZoneRule />
            <RememberRow query="deployment" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              index={1}
              title="Deployment Process"
              snippet="Fly.io two-process deploy…"
              relevance={95}
              topic="Infrastructure"
              topicIcon={Wrench}
            />
            <SourceRow
              index={2}
              title="CI/CD Pipeline"
              snippet="GitHub Actions workflow…"
              relevance={78}
              topic="Infrastructure"
              topicIcon={Wrench}
            />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38i. Keyboard navigation ─── */}
      <DemoCard label="38i. Keyboard navigation — highlight + contextual hints">
        <p className="text-[12px] text-fg-3 mb-4">
          <Kbd>↑</Kbd>
          <Kbd>↓</Kbd> move across zones ③ and ④. <Kbd>↵</Kbd> opens a
          highlighted source row, or remembers if the Remember row is
          highlighted. Highlight tint ={" "}
          <span className="font-mono">bg-surface-1</span>, no border, no ring.
        </p>
        <div className="space-y-5">
          <GlassChrome caption="remember row highlighted · ↵ = remember">
            <InputRow value="auth architecture" sendState="ready" />
            <ZoneRule />
            <SynthesisSlot state="placeholder" />
            <ZoneRule />
            <RememberRow
              query="auth architecture"
              hub="memax-team"
              highlighted
            />
            <ZoneRule />
            <SourceRow
              title="Auth Middleware Design"
              snippet="JWT + API key dual path…"
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
            <SourceRow
              title="Security Rules"
              snippet="Owner isolation…"
              topic="Security"
              topicIcon={ShieldCheck}
            />
            <HintBar
              hints={[
                { key: "↵", label: "remember" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>

          <GlassChrome caption="source row highlighted · ↵ = open · ⌘⌫ = forget">
            <InputRow value="auth architecture" sendState="ready" />
            <ZoneRule />
            <SynthesisSlot
              state="complete"
              text="Memax uses a dual auth architecture [1]…"
            />
            <ZoneRule />
            <RememberRow query="auth architecture" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              index={1}
              title="Agent Identity Architecture"
              snippet="Auth = identity. API key → agent_name…"
              relevance={98}
              topic="Authentication"
              topicIcon={ShieldCheck}
              highlighted
            />
            <SourceRow
              index={2}
              title="MCP OAuth JWT Fix"
              snippet="Added AgentName claim to JWT…"
              relevance={91}
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
            <HintBar
              hints={[
                { key: "↵", label: "open" },
                { key: "⌘⌫", label: "forget" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38j. Remember feedback ─── */}
      <DemoCard label="38j. Remember feedback — inline, replaces BarNotificationCard">
        <p className="text-[12px] text-fg-3 mb-4">
          Zone ③ morphs through its transient states in place. No separate
          notification card, no push-down of the bar, no flicker. Hub badge
          persists through every state so the user always knows <em>where</em>{" "}
          the memory is going. Undo window = 4s; auto-dismiss after that reverts
          the row to idle.
        </p>
        <div className="space-y-3 max-w-[560px] mx-auto">
          <StandaloneRemember>
            <RememberRow query="auth architecture" hub="memax-team" />
          </StandaloneRemember>
          <StandaloneRemember>
            <RememberRow
              query="auth architecture"
              hub="memax-team"
              highlighted
            />
          </StandaloneRemember>
          <StandaloneRemember>
            <RememberRow state="sending" hub="memax-team" />
          </StandaloneRemember>
          <StandaloneRemember>
            <RememberRow
              state="sent"
              title="Auth Architecture"
              hub="memax-team"
            />
          </StandaloneRemember>
          <StandaloneRemember>
            <RememberRow state="error" />
          </StandaloneRemember>
        </div>
        <div className="mt-5 text-[12px] text-fg-3 space-y-1">
          <p className="text-fg-2 font-medium">
            Transition table (4s total, success path):
          </p>
          <ol className="list-decimal pl-5 space-y-0.5">
            <li>
              <span className="font-mono">idle</span> → <Kbd>⌘</Kbd>
              <Kbd>↵</Kbd> or click → <span className="font-mono">sending</span>{" "}
              (value cleared optimistically)
            </li>
            <li>
              <span className="font-mono">sending</span> → API accepts →{" "}
              <span className="font-mono">sent</span> (✓ + Undo, 4s window)
            </li>
            <li>
              <span className="font-mono">sent</span> + 4s →{" "}
              <span className="font-mono">idle</span> (row returns to CTA for
              next query)
            </li>
            <li>
              <span className="font-mono">sending</span> → API fails →{" "}
              <span className="font-mono">error</span> (content restored, 5s
              auto-dismiss, retry available)
            </li>
            <li>
              Undo → row dismisses, created memory deleted, content restored
            </li>
          </ol>
        </div>
      </DemoCard>

      {/* ─── 38k. Slash commands ─── */}
      <DemoCard label="38k. Slash commands — /topic + /hub shipped, /forget deferred">
        <p className="text-[12px] text-fg-3 mb-4">
          Typing <Kbd>/</Kbd> as the first character opens command discovery.
          Plain slash stays plain slash; no chip yet. After explicit command
          commit, zone ④ becomes the target list for that command and empty
          query shows the full set before the user narrows it.
        </p>
        <ul className="text-[12px] text-fg-2 mb-4 space-y-1 list-disc pl-5">
          <li>
            <span className="font-mono">/topic {"{name}"}</span> — scope recall
            to a topic. Always available.
          </li>
          <li>
            <span className="font-mono">/hub {"{name}"}</span> — switch active
            workspace. Always available.
          </li>
          <li>
            <span className="font-mono">/forget</span> remains deferred until
            the forget lifecycle is fully reworked inside the bar. It stays a
            route-scoped future command, not a half-wired shipped action.
          </li>
        </ul>
        <p className="text-[12px] text-fg-3 mb-4">
          Dropped: <span className="font-mono">/import</span> (drag-drop already
          imports), <span className="font-mono">/share</span> (no share, only
          Copy / Copy-for-AI), <span className="font-mono">/remember</span> (
          <Kbd>⌘</Kbd>
          <Kbd>↵</Kbd> covers it), <span className="font-mono">/settings</span>{" "}
          (gear icon covers it).
        </p>
        <div className="space-y-5">
          <GlassChrome caption="/ · on /home · command discovery only">
            <InputRow
              value="/"
              scope={{ kind: "command", label: "command" }}
              sendState="ready"
            />
            <ZoneRule />
            <CommandRow
              icon={Hash}
              name="topic"
              desc="Jump to a topic (scopes recall)"
            />
            <CommandRow
              icon={Folder}
              name="hub"
              desc="Switch active workspace"
            />
            <HintBar
              hints={[
                { key: "↵", label: "run" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>

          <GlassChrome caption="future reference · route-scoped /forget stays deferred">
            <InputRow
              value="/"
              scope={{ kind: "command", label: "command" }}
              sendState="ready"
            />
            <ZoneRule />
            <CommandRow
              icon={Hash}
              name="topic"
              desc="Jump to a topic (scopes recall)"
            />
            <CommandRow
              icon={Folder}
              name="hub"
              desc="Switch active workspace"
            />
            <CommandRow
              icon={Trash2}
              name="forget"
              desc="Forget “Auth Architecture”"
              highlighted
            />
            <HintBar
              hints={[
                { key: "↵", label: "run" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>

          <GlassChrome caption="/topic pkce · fuzzy-filtered topic jump">
            <InputRow
              value="/topic pkce"
              scope={{ kind: "command", label: "topic" }}
              sendState="ready"
            />
            <ZoneRule />
            <CommandRow
              icon={Hash}
              name="PKCE"
              desc="Security › OAuth"
              highlighted
            />
            <CommandRow icon={Hash} name="OAuth flows" desc="Security" />
            <CommandRow
              icon={Hash}
              name="Token storage"
              desc="Security › Sessions"
            />
            <HintBar
              hints={[
                { key: "↵", label: "drill" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38l. Topic scope (absorbs 24m) ─── */}
      <DemoCard label="38l. Topic scope — capsule reflects route context (absorbs 24m)">
        <p className="text-[12px] text-fg-3 mb-4">
          On <span className="font-mono">/memories/topics/{"{id}"}</span> the
          bar picks up an implicit topic scope. Scope indicator flips from a
          Search icon to a labeled pill. Queries bias to the active topic.
          Absorbs kitchen 24m — the bar IS the picker.
        </p>
        <GlassChrome caption="inside a topic · bar scoped to Engineering">
          <InputRow
            value="pkce"
            scope={{
              kind: "topic",
              label: "Engineering",
              Icon: Folder,
            }}
            sendState="ready"
          />
          <ZoneRule />
          <SynthesisSlot state="placeholder" />
          <ZoneRule />
          <RememberRow query="pkce" hub="memax-team" />
          <ZoneRule />
          <SourceRow
            title="PKCE flow — native apps"
            snippet="Proof Key for Code Exchange, required for mobile OAuth…"
            topic="Security › OAuth"
            topicIcon={ShieldCheck}
          />
          <SourceRow
            title="OAuth flows reference"
            snippet="Authorization code, client credentials, device flow…"
            topic="Security"
            topicIcon={ShieldCheck}
          />
        </GlassChrome>
      </DemoCard>

      {/* ─── 38m. Errors & empty ─── */}
      <DemoCard label="38m. Errors & empty — inline, never a modal">
        <p className="text-[12px] text-fg-3 mb-4">
          Recall errors render inline in zone ④. AI errors render inline in zone
          ② — sources below still show. No results = empty state pointing at the
          Remember row.
        </p>
        <div className="space-y-5">
          <GlassChrome caption="no results · Remember row is the primary affordance">
            <InputRow value="zimbabwe onboarding" sendState="ready" />
            <ZoneRule />
            <SynthesisSlot state="placeholder" />
            <ZoneRule />
            <RememberRow query="zimbabwe onboarding" />
            <ZoneRule />
            <div className="px-5 py-6 text-center">
              {/* i18n: t.bar.noResults + t.bar.noResultsHint */}
              <p className="text-[14px] text-fg-2">
                No memories match this query yet.
              </p>
              <p className="text-[12px] text-fg-3 mt-1">
                Save it above to start remembering.
              </p>
            </div>
          </GlassChrome>

          <GlassChrome caption="recall failed · inline error · retry available">
            <InputRow value="auth architecture" sendState="ready" showCancel />
            <ZoneRule />
            <SynthesisSlot state="placeholder" />
            <ZoneRule />
            <RememberRow query="auth architecture" hub="memax-team" />
            <ZoneRule />
            <div
              className="mx-4 my-3 rounded-lg border p-3 flex items-start gap-2"
              style={{
                background: "oklch(from var(--destructive) l c h / 0.05)",
                borderColor: "oklch(from var(--destructive) l c h / 0.15)",
              }}
            >
              <span
                className="text-[13px] leading-none mt-0.5"
                style={{ color: "var(--destructive)" }}
              >
                ⚠
              </span>
              <div className="flex-1 text-[13px] text-fg-2">
                {/* i18n: t.bar.recallError */}
                <p className="font-medium">Recall failed.</p>
                <p className="text-fg-3">
                  Network error — your query is still here.
                </p>
              </div>
              <button className="text-[12px] text-fg-2 hover:text-fg-1 underline underline-offset-2 cursor-pointer">
                {/* i18n: t.common.retry */}
                Retry
              </button>
            </div>
          </GlassChrome>

          <GlassChrome caption="ai failed · sources still usable · quiet fallback">
            <InputRow value="auth architecture" sendState="ready" />
            <ZoneRule />
            <SynthesisSlot state="error" />
            <ZoneRule />
            <RememberRow query="auth architecture" hub="memax-team" />
            <ZoneRule />
            <SourceRow
              index={1}
              title="Agent Identity Architecture"
              snippet="Auth = identity…"
              relevance={98}
              topic="Authentication"
              topicIcon={ShieldCheck}
            />
            <SourceRow
              index={2}
              title="Security Rules"
              snippet="Owner isolation at data layer…"
              relevance={74}
              topic="Security"
              topicIcon={ShieldCheck}
            />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38n. Mobile mirror ─── */}
      <DemoCard label="38n. Mobile — Notion-style mirror, photo capture, three exits">
        <p className="text-[12px] text-fg-3 mb-4">
          Notion mobile pattern. Bar stays above the keyboard as the thumb zone.
          On focus, a mirror input slides down from the top of the screen (eye
          zone) — the user sees what they’re typing in large type while their
          thumbs stay comfortable. Result surface sits between them, scrollable.
          Edge-to-edge, no card chrome. One persistent bottom bar only — the top
          mirror is read-only context, not a second editable bar.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          <span className="text-fg-1 font-medium">Photo capture</span> — a 📷
          icon lives in zone ① between the textarea and the send button. It
          appears whenever the input is empty AND no files are staged (matching
          production <span className="font-mono">bar-right-portal</span>{" "}
          behavior). Tapping opens the native iOS / Android picker → photo
          library, files, or live camera. Selected media stages as a chip and
          the bar locks to dump (recall hidden, send becomes ↑). Same
          eager-upload flow as desktop drag-drop.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          <span className="text-fg-1 font-medium">Three exits</span>:
        </p>
        <ul className="text-[12px] text-fg-2 mb-4 space-y-1 list-disc pl-5">
          <li>
            <span className="text-fg-1 font-medium">X button</span> in the
            right-side action cluster of zone ① — layered (peels back one
            state), same role as desktop <Kbd>esc</Kbd>
          </li>
          <li>
            <span className="text-fg-1 font-medium">Swipe down</span> on the
            source list when scrolled to top — native iOS modal dismiss gesture
          </li>
          <li>
            <span className="text-fg-1 font-medium">Tap the scrim</span> above
            the mirror — returns to resting dock, query preserved
          </li>
        </ul>
        <div className="grid grid-cols-3 gap-3">
          <MobileMirrorFrame variant="mirror" />
          <MobileMirrorFrame variant="answer" />
          <MobileMirrorFrame variant="exiting" />
        </div>
      </DemoCard>

      {/* ─── 38o. Hint bar reference ─── */}
      <DemoCard label="38o. Hint bar — 24m style, &lt;kbd&gt; for glyphs only">
        <p className="text-[12px] text-fg-3 mb-4">
          24m pattern: thin row, plain text + single-glyph <Kbd>↵</Kbd> chips,
          never chains of boxy pills. Left primary, right close. Desktop only —
          mobile has no keyboard hints.
        </p>
        <div className="space-y-3">
          <GlassChrome caption="typing">
            <InputRow value="deploy" sendState="ready" />
            <HintBar
              hints={[
                { key: "↵", label: "recall" },
                { key: "⌘↵", label: "remember" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>
          <GlassChrome caption="row highlighted">
            <InputRow value="deploy" sendState="ready" />
            <HintBar
              hints={[
                { key: "↵", label: "open" },
                { key: "⌘⌫", label: "forget" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>
          <GlassChrome caption="command mode">
            <InputRow
              value="/"
              scope={{ kind: "command", label: "command" }}
              sendState="ready"
            />
            <HintBar
              hints={[
                { key: "↵", label: "run" },
                { key: "↑↓", label: "navigate" },
              ]}
              trailing={{ key: "esc", label: "close" }}
            />
          </GlassChrome>
        </div>
      </DemoCard>

      {/* ─── 38p. State machine ─── */}
      <DemoCard label="38p. State machine — single flow, layered esc">
        <div className="overflow-x-auto">
          <table className="w-full text-[12px] text-fg-2">
            <thead>
              <tr className="text-left text-fg-3 border-b border-border/30">
                <th className="py-1.5 pr-3">State</th>
                <th className="py-1.5 pr-3">Zone ②</th>
                <th className="py-1.5 pr-3">Zone ③</th>
                <th className="py-1.5 pr-3">Zone ④</th>
                <th className="py-1.5">Transition</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/20">
              <tr>
                <td className="py-1.5 pr-3 font-medium">rest</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5">→ engaged on focus/keystroke</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">engaged (empty)</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5">→ typing on first char</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">typing</td>
                <td className="py-1.5 pr-3">placeholder</td>
                <td className="py-1.5 pr-3">
                  Remember <span className="text-fg-4">“{"{query}"}”</span>
                </td>
                <td className="py-1.5 pr-3">keyword + FTS live</td>
                <td className="py-1.5">
                  → searching on <Kbd>↵</Kbd>
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">compose</td>
                <td className="py-1.5 pr-3">placeholder</td>
                <td className="py-1.5 pr-3">Remember row</td>
                <td className="py-1.5 pr-3">keyword + FTS live</td>
                <td className="py-1.5">zone ① morphs to stacked layout</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">staged</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3">
                  Remember <span className="text-fg-4">N files</span>
                </td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5">locks to remember (recall hidden)</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">searching</td>
                <td className="py-1.5 pr-3">thinking (✦ breathe)</td>
                <td className="py-1.5 pr-3">Remember row</td>
                <td className="py-1.5 pr-3">semantic fades in over keyword</td>
                <td className="py-1.5">→ streaming / complete / error</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">
                  streaming <span className="text-fg-4">(Pro)</span>
                </td>
                <td className="py-1.5 pr-3">AI tokens + ✦ breathe</td>
                <td className="py-1.5 pr-3">Remember row</td>
                <td className="py-1.5 pr-3">sources with [N] citations</td>
                <td className="py-1.5">→ complete on stream end</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">complete</td>
                <td className="py-1.5 pr-3">AI + copy toolbar</td>
                <td className="py-1.5 pr-3">Remember row</td>
                <td className="py-1.5 pr-3">sources + load more</td>
                <td className="py-1.5">→ typing on new keystroke</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">remembering</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3">✦ Remembering…</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5">→ sent / error</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">sent</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3">✓ Sent · Undo (4s)</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5">→ idle (autodismiss) / undone</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">error</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3">✕ Couldn&rsquo;t save · Retry</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5">content restored, user retries</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">command</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3 text-fg-4">—</td>
                <td className="py-1.5 pr-3">
                  command discovery / topic+hub target list
                </td>
                <td className="py-1.5">
                  → typing on backspace past <Kbd>/</Kbd>
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">topic scope</td>
                <td className="py-1.5 pr-3">placeholder</td>
                <td className="py-1.5 pr-3">Remember (in-topic)</td>
                <td className="py-1.5 pr-3">topic-biased results</td>
                <td className="py-1.5">set by route, cleared by chip X</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div className="mt-4 text-[12px] text-fg-3 space-y-1">
          <p className="text-fg-2 font-medium">
            Layered <Kbd>esc</Kbd> — peel back one layer per press:
          </p>
          <ol className="list-decimal pl-5 space-y-0.5">
            <li>edited value ≠ recallQuery → restore value</li>
            <li>result surface open → dismiss results + clear recall</li>
            <li>textarea has text → clear it</li>
            <li>
              scope chip active → degrade chip back to raw slash or clear scope
            </li>
            <li>bar engaged on /home → return to rest</li>
            <li>otherwise → blur</li>
          </ol>
        </div>
      </DemoCard>

      {/* ─── 38q. Deltas from section 37 ─── */}
      <DemoCard label="38q. Deltas — what differs from section 37">
        <p className="text-[12px] text-fg-3 mb-4">
          Both sections are valid targets. This card is for the team reviewer —
          scan the deltas, pick a direction, then one of the two gets deleted
          during the production port.
        </p>
        <div className="overflow-x-auto">
          <table className="w-full text-[12px] text-fg-2">
            <thead>
              <tr className="text-left text-fg-3 border-b border-border/30">
                <th className="py-1.5 pr-3">Aspect</th>
                <th className="py-1.5 pr-3">Section 37</th>
                <th className="py-1.5 pr-3">Section 38 (this)</th>
                <th className="py-1.5">Why 38</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/20">
              <tr>
                <td className="py-1.5 pr-3 font-medium">Chrome</td>
                <td className="py-1.5 pr-3">flat bar-bg + bar-border</td>
                <td className="py-1.5 pr-3">24m glass · blur(24) · /0.92</td>
                <td className="py-1.5 text-fg-3">
                  user explicitly said &ldquo;I like 24m
                  shadow/color/style&rdquo;
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Position</td>
                <td className="py-1.5 pr-3">top 16vh everywhere</td>
                <td className="py-1.5 pr-3">
                  rest 42vh on /home · docked elsewhere · engaged 24vh
                </td>
                <td className="py-1.5 text-fg-3">
                  hero moment + middle-upper breathing room
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Zones ②③ presence</td>
                <td className="py-1.5 pr-3">
                  mutually exclusive · ② typing, ③ after ↵
                </td>
                <td className="py-1.5 pr-3">
                  ② always reserved · ③ always visible
                </td>
                <td className="py-1.5 text-fg-3">
                  continuity — user tracks the same surface evolving
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">
                  Remember row lifecycle
                </td>
                <td className="py-1.5 pr-3">disappears after Enter</td>
                <td className="py-1.5 pr-3">
                  stays visible, morphs through transient states
                </td>
                <td className="py-1.5 text-fg-3">
                  user can always save, feedback is inline
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Save feedback</td>
                <td className="py-1.5 pr-3">implied toast (not shown)</td>
                <td className="py-1.5 pr-3">
                  inline zone ③ transients (replaces notification card)
                </td>
                <td className="py-1.5 text-fg-3">
                  one fewer surface; container morphing
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Hub badge</td>
                <td className="py-1.5 pr-3">absent</td>
                <td className="py-1.5 pr-3">on Remember row, all states</td>
                <td className="py-1.5 text-fg-3">
                  multi-hub users need the target visible at save time
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Mobile</td>
                <td className="py-1.5 pr-3">
                  single surface, results cascade upward
                </td>
                <td className="py-1.5 pr-3">
                  Notion mirror — eye zone top, thumb zone bottom
                </td>
                <td className="py-1.5 text-fg-3">
                  user&rsquo;s original ask; separates where you look from where
                  you type
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Slash commands</td>
                <td className="py-1.5 pr-3">
                  7 (includes /import, /share, /remember, /settings)
                </td>
                <td className="py-1.5 pr-3">
                  2 shipped (/topic, /hub) + /forget deferred
                </td>
                <td className="py-1.5 text-fg-3">
                  /import ≡ drag-drop, /share doesn&rsquo;t exist, /remember ≡
                  ⌘↵
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Compose morph (24f)</td>
                <td className="py-1.5 pr-3">absent</td>
                <td className="py-1.5 pr-3">
                  present · CSS Grid stacked layout
                </td>
                <td className="py-1.5 text-fg-3">
                  ChatGPT auto-grow is load-bearing for long pastes
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">
                  File/URL capture (24g)
                </td>
                <td className="py-1.5 pr-3">absent</td>
                <td className="py-1.5 pr-3">chips, drag-active, URL capture</td>
                <td className="py-1.5 text-fg-3">
                  the only non-text push path — can&rsquo;t drop it
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">
                  Topic scope indicator
                </td>
                <td className="py-1.5 pr-3">inline #chip in input row</td>
                <td className="py-1.5 pr-3">labeled capsule on the left</td>
                <td className="py-1.5 text-fg-3">
                  matches the command-scope visual (same shape, different label)
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">
                  Load more / max height
                </td>
                <td className="py-1.5 pr-3">unspecified</td>
                <td className="py-1.5 pr-3">
                  max-h-[60vh] + &ldquo;+ N more&rdquo; row
                </td>
                <td className="py-1.5 text-fg-3">
                  user explicitly asked for a fixed max with pagination
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Send button icon</td>
                <td className="py-1.5 pr-3">
                  Sparkle (lucide ✦) for recall, ↑ for command
                </td>
                <td className="py-1.5 pr-3">
                  unified ↑ · color changes by mode (violet ↔ dark)
                </td>
                <td className="py-1.5 text-fg-3">
                  ChatGPT / Claude / Perplexity convention · frees ✦ back to
                  AI-only (design system rule)
                </td>
              </tr>
              <tr>
                <td className="py-1.5 pr-3 font-medium">Deprecated inputs</td>
                <td className="py-1.5 pr-3">detect-intent.ts deleted</td>
                <td className="py-1.5 pr-3">
                  detect-intent.ts deleted ·{" "}
                  <span className="font-mono">?</span> prefix deleted · mode
                  capsule tap deleted
                </td>
                <td className="py-1.5 text-fg-3">
                  38 is explicit about all three — see callout in 38a
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p className="text-[12px] text-fg-3 mt-4">
          Things the two share: downward cascade, recall-default Enter, AI above
          sources, <Kbd>⌘</Kbd>
          <Kbd>↵</Kbd> for remember, slash as a command palette trigger, literal
          Unicode glyphs in <span className="font-mono">&lt;kbd&gt;</span>,
          layered <Kbd>esc</Kbd>, intent detector deleted, kitchen 24m absorbed.
          The disagreements are all about <em>presence vs absence</em>: whether
          zones stay visible across phases, whether mobile duplicates the input,
          whether the save target is visible.
        </p>
      </DemoCard>
    </Section>
  );
}

// ═══════════════════════════════════════════════════════════════════
// Local helpers (kitchen-only mocks)
// ═══════════════════════════════════════════════════════════════════

function ZoneLabel({ index, label }: { index: string; label: string }) {
  return (
    <div className="px-4 min-h-12 flex items-center gap-3 text-[12px] text-fg-3">
      <span className="inline-flex items-center justify-center h-5 w-5 rounded bg-surface-2 text-fg-2 shrink-0 text-[10px] font-mono">
        {index}
      </span>
      <span>{label}</span>
    </div>
  );
}

function ViewportFrame({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="relative w-full h-56 rounded-xl overflow-hidden"
      style={{
        background: "var(--surface-1)",
        border: "1px solid var(--border)",
      }}
    >
      <div className="absolute top-0 left-0 right-0 h-5 flex items-center px-2 gap-1">
        <div className="h-1.5 w-1.5 rounded-full bg-fg-4/60" />
        <div className="h-1.5 w-1.5 rounded-full bg-fg-4/60" />
        <div className="h-1.5 w-1.5 rounded-full bg-fg-4/60" />
      </div>
      {children}
    </div>
  );
}

function StandaloneRemember({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="w-full overflow-hidden"
      style={{
        borderRadius: 14,
        background: "oklch(from var(--card) l c h / 0.92)",
        backdropFilter: "blur(24px) saturate(140%)",
        WebkitBackdropFilter: "blur(24px) saturate(140%)",
        border: "1px solid oklch(from var(--border) l c h / 0.5)",
        boxShadow: "0 6px 24px oklch(0 0 0 / 0.06)",
      }}
    >
      {children}
    </div>
  );
}

// ───────────────────────────────────────────────
// Mobile mirror mock — 3 exit variants
// ───────────────────────────────────────────────

function MobileMirrorFrame({
  variant,
}: {
  variant: "mirror" | "answer" | "exiting";
}) {
  const [showHint, setShowHint] = useState(false);
  const isExiting = variant === "exiting";

  return (
    <div className="flex flex-col items-center gap-2">
      <div
        className="relative w-full overflow-hidden"
        style={{
          maxWidth: 260,
          aspectRatio: "9 / 19",
          borderRadius: 32,
          border: "1px solid var(--border)",
          background: "var(--background)",
          boxShadow: "0 6px 32px oklch(0 0 0 / 0.08)",
        }}
        onClick={() => isExiting && setShowHint(true)}
      >
        {isExiting && (
          <div
            className="absolute inset-0 z-10 pointer-events-none"
            style={{
              background:
                "linear-gradient(to bottom, oklch(from var(--foreground) l c h / 0.08) 0%, transparent 40%)",
            }}
          />
        )}

        <div className="absolute top-0 left-0 right-0 h-7 flex items-center justify-center">
          <div className="h-1 w-10 rounded-full bg-fg-4/60" />
        </div>

        {/* Mirror / answer — top (eye zone) */}
        <div
          className="absolute left-0 right-0 px-4 pt-9 pb-3"
          style={{
            top: 0,
            height: "40%",
            background: "oklch(from var(--signature) l c h / 0.04)",
            borderBottom: "1px solid var(--border)",
          }}
        >
          {variant === "mirror" || variant === "exiting" ? (
            <>
              <p className="text-[9px] uppercase tracking-wider text-fg-3 mb-1">
                asking
              </p>
              <p className="text-[16px] text-fg-1 font-medium leading-snug line-clamp-4">
                what do I know about auth architecture
              </p>
            </>
          ) : (
            <div className="flex items-start gap-1.5">
              <span
                className="text-[11px] leading-none mt-0.5 state-slow-breathe"
                style={{ color: SIGNATURE }}
              >
                ✦
              </span>
              <p className="text-[12px] text-fg-1 leading-relaxed line-clamp-6">
                Memax uses a dual auth architecture <em>[1]</em>: API keys carry
                agent_name for CLI/MCP, while OAuth JWTs embed it as a claim{" "}
                <em>[2]</em>. Owner isolation <em>[3]</em> at the data layer.
              </p>
            </div>
          )}
        </div>

        {/* Remember row (inline feedback — same as desktop) */}
        <div
          className="absolute left-0 right-0 flex items-center gap-2 px-4 py-2 text-[10px] text-fg-2 border-b border-border/30"
          style={{ top: "40%", background: "var(--background)" }}
        >
          <Plus className="h-3 w-3 text-fg-3 shrink-0" />
          <span className="truncate flex-1">
            {/* i18n: t.bar.remember.actionText */}
            Remember <span className="text-fg-1 font-medium">“…”</span>
          </span>
          <span className="inline-flex items-center gap-0.5 text-[9px] text-fg-2 bg-surface-2 px-1 py-px rounded">
            <ArrowRight className="h-2 w-2" />
            memax-team
          </span>
        </div>

        {/* Source list */}
        <div
          className="absolute left-0 right-0 overflow-hidden"
          style={{
            top: "calc(40% + 28px)",
            bottom: 84,
            background: "var(--background)",
          }}
        >
          <div className="px-4 py-1.5 flex items-center gap-2">
            <span className="inline-flex items-center justify-center text-[9px] font-mono text-fg-3 bg-surface-2 rounded px-1 py-px shrink-0">
              1
            </span>
            <span className="text-[11px] text-fg-1 font-medium truncate flex-1">
              Agent Identity
            </span>
            <span className="text-[9px] text-fg-3 tabular-nums">98%</span>
          </div>
          <div className="px-4 py-1.5 flex items-center gap-2">
            <span className="inline-flex items-center justify-center text-[9px] font-mono text-fg-3 bg-surface-2 rounded px-1 py-px shrink-0">
              2
            </span>
            <span className="text-[11px] text-fg-1 font-medium truncate flex-1">
              MCP OAuth JWT
            </span>
            <span className="text-[9px] text-fg-3 tabular-nums">91%</span>
          </div>
          <div className="px-4 py-1.5 flex items-center gap-2">
            <span className="inline-flex items-center justify-center text-[9px] font-mono text-fg-3 bg-surface-2 rounded px-1 py-px shrink-0">
              3
            </span>
            <span className="text-[11px] text-fg-1 font-medium truncate flex-1">
              Security Rules
            </span>
            <span className="text-[9px] text-fg-3 tabular-nums">74%</span>
          </div>
          {isExiting && (
            <div className="mt-4 flex flex-col items-center gap-0.5 text-fg-4">
              <span className="text-[14px] leading-none">↓</span>
              <span className="text-[9px]">swipe down</span>
            </div>
          )}
        </div>

        {/* Input row — thumb zone, above keyboard.
            Layout: [X close]  [textarea ………………… ]  [📷 camera]  [send]
            Camera icon shows when input is empty + no files staged.
            Tapping it opens the native photo / camera picker. Input row is
            intentionally action-light; hub target ownership lives on the
            Remember row, not beside send. */}
        <div
          className="absolute left-2 right-2 flex items-center gap-2 px-2 min-h-10"
          style={{
            bottom: 36,
            borderRadius: 16,
            background: "oklch(from var(--card) l c h / 0.92)",
            backdropFilter: "blur(24px) saturate(140%)",
            WebkitBackdropFilter: "blur(24px) saturate(140%)",
            border: "1px solid oklch(from var(--border) l c h / 0.5)",
            boxShadow: "0 4px 16px oklch(0 0 0 / 0.06)",
          }}
        >
          <div
            className="flex items-center justify-center h-6 w-6 text-fg-3 shrink-0"
            aria-label="Close"
          >
            <X className="h-3 w-3" />
          </div>
          <span className="text-[11px] text-fg-1 flex-1 truncate">
            auth architecture
          </span>
          <div
            className="flex items-center justify-center h-6 w-6 shrink-0 text-fg-3"
            aria-label="Add photo"
          >
            <Camera className="h-3 w-3" />
          </div>
          <div
            className="flex items-center justify-center h-6 w-6 rounded-md shrink-0"
            style={{ background: "var(--signature)", color: "#fff" }}
          >
            <ArrowUp className="h-3 w-3" />
          </div>
        </div>

        {/* Keyboard */}
        <div
          className="absolute left-0 right-0 bottom-0 h-9 border-t border-border/30 flex items-center justify-center text-[8px] text-fg-4 tracking-wider uppercase"
          style={{ background: "var(--surface-1)" }}
        >
          keyboard
        </div>

        {isExiting && showHint && (
          <div className="absolute top-1/3 left-1/2 -translate-x-1/2 -translate-y-1/2 z-20 text-[10px] text-fg-2 bg-surface-3 px-2 py-1 rounded">
            tap scrim → close
          </div>
        )}
      </div>
      <p className="text-[10px] text-fg-3 text-center">
        {variant === "mirror"
          ? "compose · mirror echoes query (top = eye zone)"
          : variant === "answer"
            ? "compose · AI streams into eye zone"
            : "exit · X · swipe-down · tap scrim"}
      </p>
    </div>
  );
}

// ───────────────────────────────────────────────
// Mobile compose tiers — 38e2
//
// Tier 1: in-place growth. Bar textarea grows vertically up to ~3 lines
//         (~150px). Mirror at top echoes the multi-line content in sync.
//         Source list shrinks but stays scrollable. Expand icon appears
//         next to send as a hint that tier 2 is available.
//
// Tier 2: fullscreen takeover. Apple Notes pattern — textarea fills the
//         viewport, back chevron + save button in the top bar, mirror
//         and source list hidden, toolbar above keyboard for attachments.
//         Triggered automatically at 4+ lines OR manually via the expand
//         icon in tier 1.
// ───────────────────────────────────────────────

function MobileComposeFrame({ tier }: { tier: 1 | 2 }) {
  return (
    <div className="flex flex-col items-center gap-2">
      <div
        className="relative w-full overflow-hidden"
        style={{
          maxWidth: 260,
          aspectRatio: "9 / 19",
          borderRadius: 32,
          border: "1px solid var(--border)",
          background: "var(--background)",
          boxShadow: "0 6px 32px oklch(0 0 0 / 0.08)",
        }}
      >
        {/* Status bar / notch */}
        <div className="absolute top-0 left-0 right-0 h-7 flex items-center justify-center">
          <div className="h-1 w-10 rounded-full bg-fg-4/60" />
        </div>

        {tier === 1 ? (
          <>
            {/* Tier 1 — bar grows in place, mirror syncs ───────────── */}

            {/* Eye zone — mirror echoes multi-line content */}
            <div
              className="absolute left-0 right-0 px-4 pt-9 pb-3"
              style={{
                top: 0,
                height: "34%",
                background: "oklch(from var(--signature) l c h / 0.04)",
                borderBottom: "1px solid var(--border)",
              }}
            >
              <p className="text-[9px] uppercase tracking-wider text-fg-3 mb-1">
                dumping
              </p>
              <p className="text-[13px] text-fg-1 leading-snug line-clamp-4">
                Meeting notes from product review
                <br />
                bar redesign approved
                <br />
                mobile tier 2 takeover
              </p>
            </div>

            {/* Remember row */}
            <div
              className="absolute left-0 right-0 flex items-center gap-2 px-4 py-2 text-[10px] text-fg-2 border-b border-border/30"
              style={{ top: "34%", background: "var(--background)" }}
            >
              <Plus className="h-3 w-3 text-fg-3 shrink-0" />
              <span className="truncate flex-1 text-fg-1 font-medium">
                Dump to memax
              </span>
              <span className="inline-flex items-center gap-0.5 text-[9px] text-fg-2 bg-surface-2 px-1 py-px rounded">
                <ArrowRight className="h-2 w-2" />
                memax-team
              </span>
            </div>

            {/* Source list — squished to make room for the grown bar */}
            <div
              className="absolute left-0 right-0 overflow-hidden"
              style={{
                top: "calc(34% + 28px)",
                bottom: 144,
                background: "var(--background)",
              }}
            >
              <div className="px-4 py-1.5 flex items-center gap-2">
                <span className="text-[11px] text-fg-1 font-medium truncate flex-1">
                  Bar Redesign Kitchen 24
                </span>
              </div>
              <div className="px-4 py-1.5 flex items-center gap-2">
                <span className="text-[11px] text-fg-1 font-medium truncate flex-1">
                  Mobile Takeover Pattern
                </span>
              </div>
              <div className="px-4 py-1 text-[9px] text-fg-4 text-center">
                + 3 more
              </div>
            </div>

            {/* Input row — grown to 3 lines, expand icon visible */}
            <div
              className="absolute left-2 right-2 flex flex-col px-2 py-1.5"
              style={{
                bottom: 36,
                minHeight: 96,
                borderRadius: 16,
                background: "oklch(from var(--card) l c h / 0.92)",
                backdropFilter: "blur(24px) saturate(140%)",
                WebkitBackdropFilter: "blur(24px) saturate(140%)",
                border: "1px solid oklch(from var(--border) l c h / 0.5)",
                boxShadow: "0 4px 16px oklch(0 0 0 / 0.06)",
              }}
            >
              <div className="flex-1 min-h-0 px-1 pt-1">
                <p className="text-[11px] text-fg-1 leading-[14px] whitespace-pre-line">
                  Meeting notes from product review{"\n"}bar redesign approved
                  {"\n"}mobile tier 2 takeover
                </p>
              </div>
              <div className="flex items-center gap-1.5 justify-end pt-1">
                <div
                  className="flex items-center justify-center h-5 w-5 text-fg-3"
                  aria-label="Close"
                >
                  <X className="h-2.5 w-2.5" />
                </div>
                <div
                  className="flex items-center justify-center h-5 w-5 text-fg-2"
                  aria-label="Expand to fullscreen"
                  title="Expand to fullscreen"
                >
                  <Maximize2 className="h-2.5 w-2.5" />
                </div>
                <div
                  className="flex items-center justify-center h-5 w-5 rounded-md shrink-0"
                  style={{
                    background: "var(--foreground)",
                    color: "var(--background)",
                  }}
                >
                  <ArrowUp className="h-2.5 w-2.5" />
                </div>
              </div>
            </div>

            {/* Keyboard */}
            <div
              className="absolute left-0 right-0 bottom-0 h-9 border-t border-border/30 flex items-center justify-center text-[8px] text-fg-4 tracking-wider uppercase"
              style={{ background: "var(--surface-1)" }}
            >
              keyboard
            </div>
          </>
        ) : (
          <>
            {/* Tier 2 — fullscreen takeover ───────────────────────── */}

            {/* Top bar — back · title · save */}
            <div
              className="absolute left-0 right-0 flex items-center justify-between px-3 border-b border-border/30"
              style={{ top: 28, height: 36, background: "var(--background)" }}
            >
              <div
                className="flex items-center justify-center h-6 w-6 text-fg-2"
                aria-label="Back"
              >
                <ArrowLeft className="h-3.5 w-3.5" />
              </div>
              <span className="text-[11px] text-fg-1 font-medium">
                Dump to memax
              </span>
              <div
                className="flex items-center gap-1 px-2 h-6 rounded-md text-[10px] font-medium"
                style={{
                  background: "var(--foreground)",
                  color: "var(--background)",
                }}
              >
                Save
              </div>
            </div>

            {/* Giant textarea — fills viewport */}
            <div
              className="absolute left-0 right-0 px-4 py-3 overflow-hidden"
              style={{
                top: 72,
                bottom: 80,
                background: "var(--background)",
              }}
            >
              <p className="text-[12px] text-fg-1 leading-[16px] whitespace-pre-line">
                Meeting notes from product review:{"\n"}
                {"\n"}- Bar redesign kitchen 38 approved{"\n"}- Mobile tier 2 =
                fullscreen takeover{"\n"}- Apple Notes pattern, back + save in
                top bar{"\n"}- Mirror + source list hidden in tier 2{"\n"}-
                Auto-trigger at 4+ lines, manual via expand{"\n"}
                {"\n"}Follow-ups:{"\n"}- wire production ComposeInputRow{"\n"}-
                handle discard guard on back
              </p>
              <span
                className="inline-block w-px h-3 bg-fg-1 align-middle ml-0.5"
                style={{ animation: "blink 1s steps(2) infinite" }}
              />
            </div>

            {/* Hub target — bottom-left, persistent above toolbar */}
            <div
              className="absolute left-3 flex items-center gap-1 text-[9px] text-fg-3"
              style={{ bottom: 78 }}
            >
              <ArrowRight className="h-2 w-2" />
              <span>memax-team</span>
            </div>

            {/* Toolbar above keyboard — attach · camera · char count */}
            <div
              className="absolute left-0 right-0 flex items-center gap-2 px-3 border-t border-border/30"
              style={{ bottom: 36, height: 36, background: "var(--surface-1)" }}
            >
              <div
                className="flex items-center justify-center h-6 w-6 text-fg-3"
                aria-label="Attach file"
              >
                <Paperclip className="h-3 w-3" />
              </div>
              <div
                className="flex items-center justify-center h-6 w-6 text-fg-3"
                aria-label="Add photo"
              >
                <Camera className="h-3 w-3" />
              </div>
              <div className="flex-1" />
              <div className="text-[9px] text-fg-4">9 lines · 312 chars</div>
            </div>

            {/* Keyboard */}
            <div
              className="absolute left-0 right-0 bottom-0 h-9 border-t border-border/30 flex items-center justify-center text-[8px] text-fg-4 tracking-wider uppercase"
              style={{ background: "var(--surface-1)" }}
            >
              keyboard
            </div>
          </>
        )}
      </div>
      <p className="text-[10px] text-fg-3 text-center">
        {tier === 1
          ? "tier 1 · bar grows in place, mirror syncs"
          : "tier 2 · fullscreen takeover, Apple Notes pattern"}
      </p>
    </div>
  );
}

// ───────────────────────────────────────────────
// MobileLifecycleFrame — 38e3
//
// Filmstrip-sized phone mock. Each frame shows one state of the mobile
// bar lifecycle. Smaller than MobileMirrorFrame/MobileComposeFrame
// (which are component-level detail mocks) — this is the state machine
// overview card, so frames are compact.
//
// States rendered:
//   dock   — resting, no scrim, mobile dock visible, single-line bar
//   mirror — active compose, mirror + source list + bar (single-line)
//   tier1  — bar + mirror growing, source list shrunk
//   tier2  — fullscreen takeover (Apple Notes)
//   result — post-↑ recall, AI streaming in mirror
//   exit   — compose active but with exit affordances annotated
// ───────────────────────────────────────────────

type LifecycleState = "dock" | "mirror" | "tier1" | "tier2" | "result" | "exit";

function MobileLifecycleFrame({
  state,
  label,
  sublabel,
}: {
  state: LifecycleState;
  label: string;
  sublabel: string;
}) {
  return (
    <div className="flex flex-col items-center gap-2">
      <div
        className="relative w-full overflow-hidden"
        style={{
          maxWidth: 220,
          aspectRatio: "9 / 19",
          borderRadius: 28,
          border: "1px solid var(--border)",
          background: "var(--background)",
          boxShadow: "0 4px 24px oklch(0 0 0 / 0.06)",
        }}
      >
        {/* Notch */}
        <div className="absolute top-0 left-0 right-0 h-6 flex items-center justify-center">
          <div className="h-[3px] w-8 rounded-full bg-fg-4/60" />
        </div>

        {state === "dock" && <LifecycleDock />}
        {state === "mirror" && <LifecycleMirror variant="active" />}
        {state === "tier1" && <LifecycleTier1 />}
        {state === "tier2" && <LifecycleTier2 />}
        {state === "result" && <LifecycleMirror variant="streaming" />}
        {state === "exit" && <LifecycleExit />}
      </div>
      <div className="flex flex-col items-center gap-0.5">
        <p className="text-[11px] text-fg-1 font-medium text-center">{label}</p>
        <p className="text-[9px] text-fg-3 text-center leading-tight">
          {sublabel}
        </p>
      </div>
    </div>
  );
}

// ───────────────────────────────────────────────
// 38e4 — Engineering State Machine (production)
//
// This card documents the explicit bar reducers + derived surface model
// now used in production (packages/web/src/lib/bar-machine.ts +
// bar-domain.ts + bar-surface.ts). It exists so future agents can reason
// about state transitions without spelunking.
// ───────────────────────────────────────────────

function BarEngineeringStateMachine() {
  return (
    <DemoCard label="38e4. Engineering state machine — production">
      <p className="text-[12px] text-fg-3 mb-4">
        Explicit reducers + derived surface ownership. This is the actual source
        of truth for state flow.
      </p>
      <div className="space-y-4">
        <div className="rounded-2xl border border-border/40 bg-surface-1 px-4 py-3">
          <p className="text-[12px] text-fg-2">
            The bar now has two reducers plus one derived surface model: a UI
            shell reducer (bar-machine), a domain reducer (bar-domain), and a
            pure surface derivation layer (bar-surface). Layout positions from
            the surface model; visual components read that model instead of
            inventing their own booleans.
          </p>
        </div>

        <div className="grid gap-3 md:grid-cols-3">
          <div className="rounded-2xl border border-border/40 bg-surface-1 px-4 py-3">
            <p className="text-[11px] text-fg-2 uppercase tracking-wide">
              BarMachine (UI shell)
            </p>
            <pre className="mt-2 text-[11px] text-fg-2 whitespace-pre-wrap">
              {`state:
  value
  scope: recall | topic | command
  phase: input | loading | recall-result
  barFocused
  barOverlayOpen
  mobileComposeState: docked | mirror | fullscreen
  activeNavigationIndex
  isBarExpanded
  detectedUrl
  memorySearch
  selectedMemoryId
  isDragging

peelBack order:
  restore-query
  reset-phase
  reset-remember
  clear-value
  close-mobile-compose
  collapse-expanded
  clear-scope
  hide-overlay
  blur`}
            </pre>
          </div>
          <div className="rounded-2xl border border-border/40 bg-surface-1 px-4 py-3">
            <p className="text-[11px] text-fg-2 uppercase tracking-wide">
              BarDomain (async + feedback)
            </p>
            <pre className="mt-2 text-[11px] text-fg-2 whitespace-pre-wrap">
              {`state:
  recallQuery
  askAnswer
  aiLoading / aiStreaming
  rememberState / rememberedTitle
  stagedFiles
  barNotification / dreamNotification / statusNotification
  selectedMemory
  pushing

resets:
  resetRecall
  resetRemember
  resetDomain`}
            </pre>
          </div>
          <div className="rounded-2xl border border-border/40 bg-surface-1 px-4 py-3">
            <p className="text-[11px] text-fg-2 uppercase tracking-wide">
              BarSurface (derived presentation)
            </p>
            <pre className="mt-2 text-[11px] text-fg-2 whitespace-pre-wrap">
              {`derived:
  visibilityState:
    hidden | rest | engaged

  expandSurfaceKind:
    hidden
    command
    remember
    recall-input
    recall-loading
    recall-result

rules:
  layout positions only
  child renderers read explicit surface kinds
  remember is first-class; never inferred from raw text
  recall UI never survives as a stale side effect
  command / remember / recall are mutually ordered surface owners`}
            </pre>
          </div>
        </div>
      </div>
    </DemoCard>
  );
}

// ─── State 0 · resting dock ───

function LifecycleDock() {
  return (
    <>
      {/* Page content area — empty for now, just shows it's not focused */}
      <div
        className="absolute left-0 right-0 px-3 pt-8"
        style={{ top: 0, bottom: 60 }}
      >
        <div className="flex flex-col items-center justify-center h-full gap-2">
          <div
            className="h-3 w-3 rounded"
            style={{
              background: "var(--signature)",
              opacity: 0.4,
            }}
          />
          <p className="text-[8px] text-fg-4">memax · /home</p>
        </div>
      </div>

      {/* Resting bar — single-line, above dock */}
      <div
        className="absolute left-2 right-2 flex items-center gap-2 px-2"
        style={{
          bottom: 32,
          height: 32,
          borderRadius: 12,
          background: "oklch(from var(--card) l c h / 0.92)",
          backdropFilter: "blur(24px) saturate(140%)",
          WebkitBackdropFilter: "blur(24px) saturate(140%)",
          border: "1px solid oklch(from var(--border) l c h / 0.5)",
          boxShadow: "0 2px 8px oklch(0 0 0 / 0.06)",
        }}
      >
        <span className="text-[10px] text-fg-3 flex-1 truncate">
          dump or ask…
        </span>
        <div
          className="flex items-center justify-center h-5 w-5 rounded-md shrink-0 opacity-40"
          style={{ background: "transparent", color: "var(--fg-4)" }}
        >
          <ArrowUp className="h-2.5 w-2.5" />
        </div>
      </div>

      {/* Mobile dock (kitchen 30) — 3 tab icons */}
      <div
        className="absolute left-0 right-0 flex items-center justify-around px-4 border-t border-border/30"
        style={{ bottom: 0, height: 32, background: "var(--background)" }}
      >
        <div className="h-3 w-3 rounded bg-fg-3" />
        <div className="h-3 w-3 rounded bg-fg-4" />
        <div className="h-3 w-3 rounded bg-fg-4" />
      </div>
    </>
  );
}

// ─── State 2 · mirror compose · or State 5 · streaming ───

function LifecycleMirror({ variant }: { variant: "active" | "streaming" }) {
  return (
    <>
      {/* Eye zone — mirror */}
      <div
        className="absolute left-0 right-0 px-3 pt-8 pb-2"
        style={{
          top: 0,
          height: "30%",
          background: "oklch(from var(--signature) l c h / 0.04)",
          borderBottom: "1px solid var(--border)",
        }}
      >
        {variant === "active" ? (
          <>
            <p className="text-[7px] uppercase tracking-wider text-fg-3 mb-0.5">
              asking
            </p>
            <p className="text-[12px] text-fg-1 font-medium leading-snug line-clamp-3">
              what do I know about auth architecture
            </p>
          </>
        ) : (
          <div className="flex items-start gap-1">
            <span
              className="text-[9px] leading-none mt-px state-slow-breathe"
              style={{ color: SIGNATURE }}
            >
              ✦
            </span>
            <p className="text-[9px] text-fg-1 leading-snug line-clamp-5">
              Memax uses a dual auth architecture: API keys carry agent_name for
              CLI/MCP, OAuth JWTs embed it as a claim.
            </p>
          </div>
        )}
      </div>

      {/* Remember row */}
      <div
        className="absolute left-0 right-0 flex items-center gap-1.5 px-3 text-[8px] text-fg-2 border-b border-border/30"
        style={{
          top: "30%",
          height: 20,
          background: "var(--background)",
        }}
      >
        <Plus className="h-2 w-2 text-fg-3 shrink-0" />
        <span className="truncate flex-1 text-fg-1 font-medium">
          Dump to memax
        </span>
        <span className="inline-flex items-center gap-0.5 text-[7px] text-fg-2 bg-surface-2 px-0.5 rounded">
          <ArrowRight className="h-[6px] w-[6px]" />
          team
        </span>
      </div>

      {/* Source list */}
      <div
        className="absolute left-0 right-0 overflow-hidden"
        style={{
          top: "calc(30% + 20px)",
          bottom: 66,
          background: "var(--background)",
        }}
      >
        <SourceMini index={1} title="Agent Identity" pct={98} />
        <SourceMini index={2} title="MCP OAuth JWT" pct={91} />
        <SourceMini index={3} title="Security Rules" pct={74} />
      </div>

      {/* Bar — thumb zone */}
      <div
        className="absolute left-2 right-2 flex items-center gap-1 px-1.5"
        style={{
          bottom: 32,
          height: 26,
          borderRadius: 12,
          background: "oklch(from var(--card) l c h / 0.92)",
          backdropFilter: "blur(24px) saturate(140%)",
          WebkitBackdropFilter: "blur(24px) saturate(140%)",
          border: "1px solid oklch(from var(--border) l c h / 0.5)",
          boxShadow: "0 2px 8px oklch(0 0 0 / 0.06)",
        }}
      >
        <div className="flex items-center justify-center h-4 w-4 text-fg-3">
          <X className="h-2 w-2" />
        </div>
        <span className="text-[9px] text-fg-1 flex-1 truncate">
          auth architecture
        </span>
        <div
          className="flex items-center justify-center h-4 w-4 rounded shrink-0"
          style={{
            background:
              variant === "active" ? "var(--signature)" : "var(--signature)",
            color: "#fff",
          }}
        >
          {variant === "active" ? (
            <ArrowUp className="h-2 w-2" />
          ) : (
            <Loader2 className="h-2 w-2 animate-spin" />
          )}
        </div>
      </div>

      {/* Keyboard */}
      <div
        className="absolute left-0 right-0 bottom-0 h-8 border-t border-border/30 flex items-center justify-center text-[7px] text-fg-4 tracking-wider uppercase"
        style={{ background: "var(--surface-1)" }}
      >
        keyboard
      </div>
    </>
  );
}

// ─── State 3 · tier 1 ───

function LifecycleTier1() {
  return (
    <>
      {/* Eye zone — grown mirror with 3 lines */}
      <div
        className="absolute left-0 right-0 px-3 pt-8 pb-2"
        style={{
          top: 0,
          height: "32%",
          background: "oklch(from var(--signature) l c h / 0.04)",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <p className="text-[7px] uppercase tracking-wider text-fg-3 mb-0.5">
          dumping
        </p>
        <p className="text-[10px] text-fg-1 font-medium leading-[12px] line-clamp-4">
          Meeting notes
          <br />
          bar redesign approved
          <br />
          mobile tier 2 takeover
        </p>
      </div>

      {/* Remember row */}
      <div
        className="absolute left-0 right-0 flex items-center gap-1.5 px-3 text-[8px] text-fg-2 border-b border-border/30"
        style={{
          top: "32%",
          height: 20,
          background: "var(--background)",
        }}
      >
        <Plus className="h-2 w-2 text-fg-3 shrink-0" />
        <span className="truncate flex-1 text-fg-1 font-medium">
          Dump 3 lines to memax
        </span>
      </div>

      {/* Source list — shrunk */}
      <div
        className="absolute left-0 right-0 overflow-hidden"
        style={{
          top: "calc(32% + 20px)",
          bottom: 120,
          background: "var(--background)",
        }}
      >
        <SourceMini index={1} title="Bar Redesign" pct={87} />
      </div>

      {/* Bar — grown to 3 lines */}
      <div
        className="absolute left-2 right-2 flex flex-col px-1.5 py-1"
        style={{
          bottom: 32,
          minHeight: 82,
          borderRadius: 12,
          background: "oklch(from var(--card) l c h / 0.92)",
          backdropFilter: "blur(24px) saturate(140%)",
          WebkitBackdropFilter: "blur(24px) saturate(140%)",
          border: "1px solid oklch(from var(--border) l c h / 0.5)",
          boxShadow: "0 2px 8px oklch(0 0 0 / 0.06)",
        }}
      >
        <div className="flex-1 min-h-0 px-0.5 pt-0.5">
          <p className="text-[9px] text-fg-1 leading-[11px] whitespace-pre-line">
            Meeting notes{"\n"}bar redesign approved{"\n"}mobile tier 2 takeover
          </p>
        </div>
        <div className="flex items-center gap-1 justify-end pt-0.5">
          <div className="flex items-center justify-center h-4 w-4 text-fg-3">
            <X className="h-2 w-2" />
          </div>
          <div className="flex items-center justify-center h-4 w-4 text-fg-2">
            <Maximize2 className="h-2 w-2" />
          </div>
          <div
            className="flex items-center justify-center h-4 w-4 rounded shrink-0"
            style={{
              background: "var(--foreground)",
              color: "var(--background)",
            }}
          >
            <ArrowUp className="h-2 w-2" />
          </div>
        </div>
      </div>

      {/* Keyboard */}
      <div
        className="absolute left-0 right-0 bottom-0 h-8 border-t border-border/30 flex items-center justify-center text-[7px] text-fg-4 tracking-wider uppercase"
        style={{ background: "var(--surface-1)" }}
      >
        keyboard
      </div>
    </>
  );
}

// ─── State 4 · tier 2 ───

function LifecycleTier2() {
  return (
    <>
      {/* Top bar */}
      <div
        className="absolute left-0 right-0 flex items-center justify-between px-2 border-b border-border/30"
        style={{ top: 24, height: 28, background: "var(--background)" }}
      >
        <div className="flex items-center justify-center h-5 w-5 text-fg-2">
          <ArrowLeft className="h-3 w-3" />
        </div>
        <span className="text-[9px] text-fg-1 font-medium">Dump to memax</span>
        <div
          className="flex items-center px-1.5 h-5 rounded text-[8px] font-medium"
          style={{
            background: "var(--foreground)",
            color: "var(--background)",
          }}
        >
          Save
        </div>
      </div>

      {/* Giant textarea */}
      <div
        className="absolute left-0 right-0 px-3 py-2"
        style={{
          top: 56,
          bottom: 64,
          background: "var(--background)",
        }}
      >
        <p className="text-[9px] text-fg-1 leading-[12px] whitespace-pre-line">
          Meeting notes from product review:{"\n"}
          {"\n"}- Kitchen 38 approved{"\n"}- Mobile tier 2 fullscreen{"\n"}-
          Back preserves content{"\n"}
          {"\n"}Follow-ups:{"\n"}- wire production bar-context
        </p>
        <span
          className="inline-block w-px h-2.5 bg-fg-1 align-middle ml-px"
          style={{ animation: "blink 1s steps(2) infinite" }}
        />
      </div>

      {/* Hub target + toolbar */}
      <div
        className="absolute left-2 flex items-center gap-0.5 text-[7px] text-fg-3"
        style={{ bottom: 66 }}
      >
        <ArrowRight className="h-[6px] w-[6px]" />
        <span>memax-team</span>
      </div>
      <div
        className="absolute left-0 right-0 flex items-center gap-2 px-3 border-t border-border/30"
        style={{ bottom: 32, height: 28, background: "var(--surface-1)" }}
      >
        <Paperclip className="h-2 w-2 text-fg-3" />
        <Camera className="h-2 w-2 text-fg-3" />
        <div className="flex-1" />
        <span className="text-[7px] text-fg-4">7 lines</span>
      </div>

      {/* Keyboard */}
      <div
        className="absolute left-0 right-0 bottom-0 h-8 border-t border-border/30 flex items-center justify-center text-[7px] text-fg-4 tracking-wider uppercase"
        style={{ background: "var(--surface-1)" }}
      >
        keyboard
      </div>
    </>
  );
}

// ─── State 6 · exit paths annotated ───

function LifecycleExit() {
  return (
    <>
      {/* Scrim overlay (showing exit-via-scrim affordance) */}
      <div
        className="absolute inset-0 z-10 pointer-events-none"
        style={{
          background:
            "linear-gradient(to bottom, oklch(from var(--foreground) l c h / 0.18) 0%, transparent 28%)",
        }}
      />

      {/* Same mirror compose layout as state 2 */}
      <div
        className="absolute left-0 right-0 px-3 pt-8 pb-2"
        style={{
          top: 0,
          height: "30%",
          background: "oklch(from var(--signature) l c h / 0.04)",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <p className="text-[7px] uppercase tracking-wider text-fg-3 mb-0.5">
          asking
        </p>
        <p className="text-[12px] text-fg-1 font-medium leading-snug line-clamp-3">
          what do I know about auth architecture
        </p>
      </div>

      {/* Scrim-tap annotation */}
      <div
        className="absolute left-1/2 -translate-x-1/2 z-20 text-[7px] text-fg-2 bg-surface-3 px-1 py-px rounded whitespace-nowrap"
        style={{ top: 4 }}
      >
        tap scrim ↑
      </div>

      {/* Remember row */}
      <div
        className="absolute left-0 right-0 flex items-center gap-1.5 px-3 text-[8px] text-fg-2 border-b border-border/30"
        style={{ top: "30%", height: 20, background: "var(--background)" }}
      >
        <Plus className="h-2 w-2 text-fg-3 shrink-0" />
        <span className="truncate flex-1 text-fg-1 font-medium">
          Dump to memax
        </span>
      </div>

      {/* Source list with swipe-down annotation */}
      <div
        className="absolute left-0 right-0 overflow-hidden"
        style={{
          top: "calc(30% + 20px)",
          bottom: 66,
          background: "var(--background)",
        }}
      >
        <SourceMini index={1} title="Agent Identity" pct={98} />
        <SourceMini index={2} title="MCP OAuth JWT" pct={91} />
        <div className="mt-1 flex flex-col items-center gap-0 text-fg-4">
          <span className="text-[9px] leading-none">↓</span>
          <span className="text-[6px] uppercase tracking-wider">
            swipe down
          </span>
        </div>
      </div>

      {/* Bar with X annotation */}
      <div
        className="absolute left-2 right-2 flex items-center gap-1 px-1.5"
        style={{
          bottom: 32,
          height: 26,
          borderRadius: 12,
          background: "oklch(from var(--card) l c h / 0.92)",
          backdropFilter: "blur(24px) saturate(140%)",
          WebkitBackdropFilter: "blur(24px) saturate(140%)",
          border: "1px solid oklch(from var(--border) l c h / 0.5)",
          boxShadow: "0 2px 8px oklch(0 0 0 / 0.06)",
        }}
      >
        <div
          className="flex items-center justify-center h-4 w-4 rounded"
          style={{
            background: "oklch(from var(--signature) l c h / 0.15)",
            color: "var(--signature)",
          }}
        >
          <X className="h-2 w-2" />
        </div>
        <span className="text-[9px] text-fg-1 flex-1 truncate">
          auth architecture
        </span>
        <div
          className="flex items-center justify-center h-4 w-4 rounded shrink-0"
          style={{ background: "var(--signature)", color: "#fff" }}
        >
          <ArrowUp className="h-2 w-2" />
        </div>
      </div>

      {/* Keyboard */}
      <div
        className="absolute left-0 right-0 bottom-0 h-8 border-t border-border/30 flex items-center justify-center text-[7px] text-fg-4 tracking-wider uppercase"
        style={{ background: "var(--surface-1)" }}
      >
        keyboard
      </div>
    </>
  );
}

/** Shared compact source row for the filmstrip frames. */
function SourceMini({
  index,
  title,
  pct,
}: {
  index: number;
  title: string;
  pct: number;
}) {
  return (
    <div className="px-3 py-1 flex items-center gap-1">
      <span className="inline-flex items-center justify-center text-[7px] font-mono text-fg-3 bg-surface-2 rounded px-[3px] py-px shrink-0">
        {index}
      </span>
      <span className="text-[9px] text-fg-1 font-medium truncate flex-1">
        {title}
      </span>
      <span className="text-[7px] text-fg-3 tabular-nums">{pct}%</span>
    </div>
  );
}
