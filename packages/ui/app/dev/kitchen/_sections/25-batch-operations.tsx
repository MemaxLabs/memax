// Kitchen 25 — Batch Memory Operations
// Maps to: topic-grid.tsx, topic-detail.tsx, memory-row.tsx
// Production files: selection-context.tsx, batch-toolbar.tsx, batch-active.ts
//
// ═══ LLM AGENT RULES — BATCH OPERATIONS ═══
//
// TOUCH TARGETS: ALL interactive elements MUST be min-h-11 (44px).
//   WCAG 2.5.5 + Apple HIG + Material 3 all require 44px minimum.
//   Visual content stays compact — min-height provides the touch area.
//   Icons: h-4 w-4 (16px). Text: text-[13px]. Buttons: min-h-11 px-3.
//   Move picker items: min-h-11. Forget/Keep confirmation: min-h-11 px-4.
//
// ARCHITECTURE:
//   - SelectionProvider is SECTION-SCOPED (not layout-level).
//   - BatchToolbar portals to document.body via createPortal.
//   - Bar/toolbar SWAP: batch-active.ts bridge signals GlobalBar.
//   - Toasts via MutationCache meta (kitchen 28).
//
// CLICK-OUTSIDE: Radix pattern (pointerdown + contains), not overlay div.
// TOOLBAR: glass surface matching bar tokens, z-60 (above bar z-50).
// ═══════════════════════════════════════════════
"use client";

import { useState, useCallback } from "react";
import { Section, DemoCard, SIGNATURE } from "../_shared";
import {
  Check,
  FolderInput,
  Trash2,
  Download,
  Copy,
  Share2,
  X,
  ArrowRight,
  Clock,
  Inbox,
  CheckCircle2,
} from "lucide-react";

const NEUTRAL_DOT = "oklch(from var(--foreground) l c h / 0.2)";

/* ── Mock data ── */

const MOCKS = [
  {
    id: "1",
    title: "Blue-green deploy rollback procedure",
    age: "2h",
    dot: NEUTRAL_DOT,
  },
  {
    id: "2",
    title: "OAuth2 token rotation — refresh flow",
    age: "4h",
    dot: NEUTRAL_DOT,
  },
  {
    id: "3",
    title: "pgvector index tuning — probes=10",
    age: "1d",
    dot: NEUTRAL_DOT,
  },
  {
    id: "4",
    title: "Tailwind v4 migration notes",
    age: "1d",
    dot: NEUTRAL_DOT,
  },
  {
    id: "5",
    title: "River queue retry backoff config",
    age: "2d",
    dot: NEUTRAL_DOT,
  },
  {
    id: "6",
    title: "Cohere rerank threshold tuning",
    age: "3d",
    dot: NEUTRAL_DOT,
  },
];

const TOPICS = [
  { id: "t1", name: "Deployment", icon: "🚀" },
  { id: "t2", name: "Authentication", icon: "🔒" },
  { id: "t3", name: "Database", icon: "💾" },
];

/* ═══════════════════════════════════════════════════════════════════
 * INDICATOR SLOT MORPH
 *
 * 3 states in same h-3 w-3 container:
 *   default  → row-native indicator (dot / type icon / avatar)
 *   ring     → hollow circle (selection active, not selected)
 *   check    → signature ✓ (selected)
 * ═══════════════════════════════════════════════════════════════════ */

function Indicator({
  dot,
  selected,
  active,
}: {
  dot: string;
  selected: boolean;
  active: boolean;
}) {
  const spring =
    "all 0.25s var(--ease-spring, cubic-bezier(0.34, 1.56, 0.64, 1))";
  return (
    <span
      className="h-3 w-3 flex items-center justify-center shrink-0"
      style={{ transition: spring }}
    >
      {selected ? (
        <Check
          className="h-3 w-3"
          style={{ color: SIGNATURE }}
          strokeWidth={2.5}
        />
      ) : active ? (
        <span
          className="h-2.5 w-2.5 rounded-full"
          style={{ border: "1.5px solid var(--fg-3)", transition: spring }}
        />
      ) : (
        <span
          className="h-1.5 w-1.5 rounded-full"
          style={{ backgroundColor: dot, transition: spring }}
        />
      )}
    </span>
  );
}

/* ═══════════════════════════════════════════════════════════════════
 * SECTION HEADER — matches production SectionCard header exactly
 *
 * Left:  Icon + Label (uppercase, tracking-wide)
 * Right: trailing slot (filter dropdown, copy, count)
 *        + "Select" text button (batch entry point)
 *
 * "Select" appears as subtle fg-3 text in the trailing slot.
 * When selection is active, it changes to "Done" (exits selection).
 * ═══════════════════════════════════════════════════════════════════ */

function SectionHeader({
  icon: Icon,
  label,
  trailing,
  selectionActive,
  onToggleSelect,
}: {
  icon: typeof Clock;
  label: string;
  trailing?: React.ReactNode;
  selectionActive: boolean;
  onToggleSelect: () => void;
}) {
  return (
    <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
      <Icon className="h-3.5 w-3.5 text-fg-3 shrink-0" />
      <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
        {label}
      </span>
      <div className="flex-1" />
      {trailing}
      <button
        onClick={onToggleSelect}
        className="text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors ml-2"
      >
        {selectionActive ? "Done" : "Select"}
      </button>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════════
 * MEMORY ROW — selection-aware
 * Matches production: px-4 py-2.5, border-t border-border/30
 * ═══════════════════════════════════════════════════════════════════ */

function Row({
  m,
  selected,
  active,
  onToggle,
  divider,
}: {
  m: (typeof MOCKS)[number];
  selected: boolean;
  active: boolean;
  onToggle: () => void;
  divider: boolean;
}) {
  return (
    <div
      className={`flex items-center gap-2 px-4 py-2.5 cursor-pointer transition-colors ${
        divider ? "border-t border-border/30" : ""
      } ${selected ? "bg-surface-2" : "hover:bg-surface-1"}`}
      onClick={onToggle}
    >
      <Indicator dot={m.dot} selected={selected} active={active} />
      <span
        className={`text-[14px] flex-1 truncate ${selected ? "text-fg-1 font-medium" : "text-fg-2"}`}
      >
        {m.title}
      </span>
      <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
        {m.age}
      </span>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════════
 * BATCH TOOLBAR — glass surface, bar-native
 *
 * Uses var(--bar-bg), var(--bar-border), bar-shadow tokens.
 * Glass backdrop blur. Signature accent on count.
 * Destructive confirmation morphs IN the toolbar (container morphing).
 * ═══════════════════════════════════════════════════════════════════ */

function Toolbar({
  count,
  onClear,
  compact,
  confirmForget,
  onConfirmForget,
  onCancelForget,
  onForget,
}: {
  count: number;
  onClear: () => void;
  compact?: boolean;
  confirmForget?: boolean;
  onConfirmForget?: () => void;
  onCancelForget?: () => void;
  onForget?: () => void;
}) {
  if (confirmForget) {
    // Destructive morph — same container, glassmorphism destructive
    return (
      <div
        className="flex items-center gap-2 rounded-2xl px-4 py-2.5"
        style={{
          background: "oklch(from var(--destructive) l c h / 0.08)",
          border: "1px solid oklch(from var(--destructive) l c h / 0.15)",
          backdropFilter: "blur(24px)",
          WebkitBackdropFilter: "blur(24px)",
          boxShadow: "0 4px 24px oklch(0 0 0 / 0.08)",
        }}
      >
        <span className="text-[13px] text-fg-2 flex-1">
          Forget {count} {count === 1 ? "memory" : "memories"}?
        </span>
        <button
          onClick={onConfirmForget}
          className="text-[13px] min-h-11 px-4 py-2 rounded-xl cursor-pointer transition-colors"
          style={{
            background: "oklch(from var(--destructive) l c h / 0.12)",
            color: "oklch(from var(--destructive) l c h / 0.8)",
          }}
        >
          Forget
        </button>
        <button
          onClick={onCancelForget}
          className="text-[13px] min-h-11 px-4 py-2 rounded-xl cursor-pointer text-fg-2 hover:text-fg-1 transition-colors"
        >
          Keep
        </button>
      </div>
    );
  }

  const actions = [
    { icon: FolderInput, label: "Move", key: "move" },
    { icon: Copy, label: "Copy", key: "copy" },
    { icon: Download, label: "Export", key: "export" },
    { icon: Share2, label: "Share", key: "share" },
    { icon: Trash2, label: "Forget", key: "forget", danger: true },
  ];

  return (
    <div
      className="flex items-center gap-0.5 rounded-2xl px-1.5 py-1"
      style={{
        background: "var(--bar-bg, var(--card))",
        border: "1px solid var(--bar-border)",
        boxShadow:
          "0 4px 24px oklch(0 0 0 / 0.08), 0 1px 4px oklch(0 0 0 / 0.04)",
        backdropFilter: "blur(24px)",
        WebkitBackdropFilter: "blur(24px)",
      }}
    >
      {/* All buttons min-h-11 (44px) per WCAG 2.5.5 */}
      <button
        onClick={onClear}
        className="flex items-center gap-1.5 min-h-11 pl-3 pr-2.5 rounded-xl cursor-pointer transition-colors hover:bg-surface-2"
      >
        <span
          className="text-[13px] font-semibold tabular-nums"
          style={{ color: SIGNATURE }}
        >
          {count}
        </span>
        <X className="h-3.5 w-3.5 text-fg-3" />
      </button>
      <div className="w-px h-5 bg-border/40 mx-0.5" />
      {actions.map((a) => (
        <button
          key={a.key}
          onClick={a.key === "forget" ? onForget : undefined}
          className={`flex items-center gap-1.5 min-h-11 px-3 rounded-xl cursor-pointer transition-colors ${
            a.danger
              ? "text-fg-2 hover:text-destructive/70 hover:bg-destructive/5"
              : "text-fg-2 hover:text-fg-1 hover:bg-surface-2"
          }`}
        >
          <a.icon className="h-4 w-4" />
          {!compact && <span className="text-[13px]">{a.label}</span>}
        </button>
      ))}
    </div>
  );
}

/* ── Move Picker ── */

function MovePicker({
  items,
}: {
  items: { id: string; name: string; icon?: string }[];
}) {
  return (
    <div
      className="rounded-xl overflow-hidden w-52"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
        boxShadow: "0 8px 32px oklch(0 0 0 / 0.08)",
        backdropFilter: "blur(16px)",
        WebkitBackdropFilter: "blur(16px)",
      }}
    >
      <p className="text-[11px] text-fg-3 uppercase tracking-wider font-medium px-3 pt-2.5 pb-1">
        Move to
      </p>
      {items.map((t) => (
        <button
          key={t.id}
          className="w-full flex items-center gap-2.5 min-h-11 px-3 text-[13px] text-fg-2 hover:bg-surface-1 cursor-pointer transition-colors"
        >
          {t.icon && <span className="text-[14px]">{t.icon}</span>}
          <span className="flex-1 text-left">{t.name}</span>
          <ArrowRight className="h-3.5 w-3.5 text-fg-4" />
        </button>
      ))}
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════════
 * MAIN SECTION
 * ═══════════════════════════════════════════════════════════════════ */

export function BatchOperationsSection() {
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [selectionActive, setSelectionActive] = useState(false);
  const [forgetConfirm, setForgetConfirm] = useState(false);
  const [postAction, setPostAction] = useState<string | null>(null);

  const toggle = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const enterSelect = () => {
    setSelectionActive(true);
    setPostAction(null);
    setForgetConfirm(false);
  };
  const exitSelect = () => {
    setSelectionActive(false);
    setSelected(new Set());
    setForgetConfirm(false);
  };

  return (
    <Section
      title="25. Batch Operations"
      description="Indicator-slot morph + glass toolbar. Entry via &ldquo;Select&rdquo; in section header trailing slot. Every action is end-to-end."
    >
      {/* ── 25a. Entry Point — "Select" in section header ── */}
      <DemoCard label="25a. Entry &mdash; where batch starts on each surface">
        <p className="text-[13px] text-fg-2 mb-4">
          Each section header has a trailing slot (right side, after spacer).
          &ldquo;Select&rdquo; lives there as subtle text. Tapping it enters
          selection mode &mdash; indicators morph to rings, toolbar appears.
          &ldquo;Done&rdquo; exits. Same pattern on every surface. In
          production, the current row indicator stays in the same slot and
          morphs there; it should never hard-swap to a disconnected checkbox.
        </p>

        <div className="space-y-3">
          {/* Recent header — existing filter + Select (group copy is batch-only) */}
          <div
            className="rounded-xl overflow-hidden"
            style={{
              border: "1px solid var(--border)",
              background: "var(--card)",
            }}
          >
            <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
              <Clock className="h-3.5 w-3.5 text-fg-3 shrink-0" />
              <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
                Recent
              </span>
              <span className="text-[12px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded cursor-pointer">
                Past 24h
              </span>
              <div className="flex-1" />
              <button className="text-fg-3 hover:text-fg-2 transition-colors cursor-pointer flex items-center gap-1">
                <Copy className="h-3 w-3" />
                <span className="text-[11px] tabular-nums">6</span>
              </button>
              <button className="text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors ml-2">
                Select
              </button>
            </div>
            <div className="px-4 py-2 text-[12px] text-fg-3">
              &hellip; memory rows &hellip;
            </div>
          </div>

          {/* Inbox header — with existing organize + copy */}
          <div
            className="rounded-xl overflow-hidden"
            style={{
              border: "1px solid var(--border)",
              background: "var(--card)",
            }}
          >
            <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
              <Inbox className="h-3.5 w-3.5 text-fg-3 shrink-0" />
              <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
                Unassigned
              </span>
              <span className="text-[13px] text-fg-3 tabular-nums">12</span>
              <div className="flex-1" />
              <button
                className="flex items-center gap-1 text-[12px] cursor-pointer transition-colors"
                style={{ color: "oklch(0.62 0.16 290)" }}
              >
                <span className="text-[12px] leading-none">✦</span>
                Organize
              </button>
              <button className="text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors ml-2">
                Select
              </button>
            </div>
            <div className="px-4 py-2 text-[12px] text-fg-3">
              &hellip; inbox rows &hellip;
            </div>
          </div>

          {/* Topic detail header */}
          <div
            className="rounded-xl overflow-hidden"
            style={{
              border: "1px solid var(--border)",
              background: "var(--card)",
            }}
          >
            <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
              <span className="text-[14px]">🚀</span>
              <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
                Deployment
              </span>
              <span className="text-[13px] text-fg-3 tabular-nums">23</span>
              <div className="flex-1" />
              <button className="text-fg-3 hover:text-fg-2 transition-colors cursor-pointer flex items-center gap-1">
                <Copy className="h-3 w-3" />
                <span className="text-[11px] tabular-nums">23</span>
              </button>
              <button className="text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors ml-2">
                Select
              </button>
            </div>
            <div className="px-4 py-2 text-[12px] text-fg-3">
              &hellip; topic rows &hellip;
            </div>
          </div>
        </div>

        <p className="text-[11px] text-fg-3 mt-3">
          Alternative entries: <span className="text-fg-2">long-press</span>{" "}
          (mobile, 500ms), <span className="text-fg-2">shift+click</span>{" "}
          (desktop range select). Both auto-enter selection mode.
        </p>
      </DemoCard>

      {/* ── 25b. The Morph — indicator states close-up ── */}
      <DemoCard label="25b. Indicator Morph &mdash; same container, 3 states">
        <p className="text-[13px] text-fg-2 mb-4">
          The h-3 w-3 indicator container morphs with spring easing. No checkbox
          slides in. The row&apos;s own indicator slot becomes the selection
          affordance, then returns to its native icon on exit.
        </p>
        <div className="flex items-center gap-10">
          {[
            { label: "Default", sel: false, act: false },
            { label: "Ring", sel: false, act: true },
            { label: "Check", sel: true, act: true },
          ].map((s) => (
            <div key={s.label} className="flex flex-col items-center gap-2">
              <div
                className="h-12 w-12 rounded-xl flex items-center justify-center"
                style={{
                  background: s.sel ? "var(--surface-2)" : "var(--surface-1)",
                }}
              >
                <Indicator dot={NEUTRAL_DOT} selected={s.sel} active={s.act} />
              </div>
              <span className="text-[11px] text-fg-3">{s.label}</span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── 25c. Full interactive flow ── */}
      <DemoCard label="25c. Full Flow &mdash; tap Select, pick rows, act">
        <p className="text-[13px] text-fg-2 mb-4">
          End-to-end: &ldquo;Select&rdquo; enters mode &rarr; indicators morph
          to rings &rarr; tap rows to select &rarr; checks appear &rarr;
          &ldquo;Done&rdquo; exits back to the native row indicator.
        </p>

        <div
          className="rounded-xl overflow-hidden relative"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          {/* Section header with Select/Done toggle */}
          <SectionHeader
            icon={Clock}
            label="Recent"
            trailing={
              <span className="text-[12px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded">
                Past 24h
              </span>
            }
            selectionActive={selectionActive}
            onToggleSelect={selectionActive ? exitSelect : enterSelect}
          />

          {/* Rows */}
          {MOCKS.map((m, i) => (
            <Row
              key={m.id}
              m={m}
              selected={selected.has(m.id)}
              active={selectionActive}
              onToggle={() => selectionActive && toggle(m.id)}
              divider={i > 0}
            />
          ))}

          {/* Post-action feedback toast */}
          {postAction && (
            <div className="flex items-center gap-2 px-4 py-2.5 bg-surface-1">
              <CheckCircle2
                className="h-3.5 w-3.5"
                style={{ color: "oklch(0.72 0.19 145)" }}
              />
              <span className="text-[13px] text-fg-2">{postAction}</span>
            </div>
          )}

          {/* Toolbar — glass, anchored bottom */}
          {selectionActive && selected.size > 0 && (
            <div className="sticky bottom-0 flex justify-center py-3 px-4">
              <Toolbar
                count={selected.size}
                onClear={() => setSelected(new Set())}
                confirmForget={forgetConfirm}
                onForget={() => setForgetConfirm(true)}
                onConfirmForget={() => {
                  setPostAction(`Forgot ${selected.size} memories`);
                  exitSelect();
                  setTimeout(() => setPostAction(null), 3000);
                }}
                onCancelForget={() => setForgetConfirm(false)}
              />
            </div>
          )}
        </div>
      </DemoCard>

      {/* ── 25d. Action detail — Move picker ── */}
      <DemoCard label="25d. Move &mdash; glass picker anchored to toolbar">
        <p className="text-[13px] text-fg-2 mb-4">
          &ldquo;Move&rdquo; opens a glass picker above the toolbar. Topics with
          emoji icons. Tap topic &rarr; memories move &rarr; rows animate out
          &rarr; toast &ldquo;3 &rarr; Deployment&rdquo; &rarr; selection
          clears.
        </p>
        <div className="flex flex-col items-center gap-3">
          <MovePicker items={TOPICS} />
          <Toolbar count={3} onClear={() => {}} />
        </div>
        <p className="text-[11px] text-fg-3 mt-3">
          Picker anchored to Move button, opens upward. Click outside or Esc
          closes. Same glass treatment as bar expand slot.
        </p>
      </DemoCard>

      {/* ── 25e. Forget confirmation — toolbar morphs ── */}
      <DemoCard label="25e. Forget &mdash; toolbar morphs to confirmation">
        <p className="text-[13px] text-fg-2 mb-4">
          Same toolbar container. Actions collapse, destructive confirmation
          appears. Glassmorphism destructive treatment (kitchen 03). Forget
          LEFT, Keep RIGHT (cursor safety &mdash; cursor was on Forget, Keep
          appears where cursor is).
        </p>
        <div className="flex flex-col items-center gap-3">
          {/* Before */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2 text-center">
              Before
            </p>
            <Toolbar count={3} onClear={() => {}} />
          </div>
          {/* After morph */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2 text-center">
              After &ldquo;Forget&rdquo; tap
            </p>
            <Toolbar
              count={3}
              onClear={() => {}}
              confirmForget
              onConfirmForget={() => {}}
              onCancelForget={() => {}}
            />
          </div>
        </div>
      </DemoCard>

      {/* ── 25f. Mobile ── */}
      <DemoCard label="25f. Mobile &mdash; long-press entry, compact toolbar">
        <p className="text-[13px] text-fg-2 mb-4">
          Long-press (500ms, haptic feedback) enters selection on mobile.
          &ldquo;Select&rdquo; text still available in header. Compact toolbar:
          icons only, same glass treatment. Fixed above safe area.
        </p>
        <div className="max-w-xs mx-auto">
          <div
            className="rounded-2xl overflow-hidden relative"
            style={{
              border: "1px solid var(--border)",
              background: "var(--card)",
              height: 300,
            }}
          >
            {/* Header */}
            <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
              <Clock className="h-3.5 w-3.5 text-fg-3" />
              <span className="text-[13px] font-semibold text-fg-2 uppercase tracking-wide">
                Recent
              </span>
              <div className="flex-1" />
              <span className="text-[12px]" style={{ color: SIGNATURE }}>
                Done
              </span>
            </div>
            {/* Rows */}
            {MOCKS.slice(0, 4).map((m, i) => (
              <div
                key={m.id}
                className={`flex items-center gap-2 px-4 py-2.5 ${i > 0 ? "border-t border-border/30" : ""} ${i < 2 ? "bg-surface-2" : ""}`}
              >
                <Indicator dot={m.dot} selected={i < 2} active={true} />
                <span
                  className={`text-[13px] truncate flex-1 ${i < 2 ? "text-fg-1 font-medium" : "text-fg-2"}`}
                >
                  {m.title}
                </span>
              </div>
            ))}
            {/* Compact toolbar */}
            <div className="absolute bottom-0 inset-x-0 flex justify-center py-3">
              <Toolbar count={2} onClear={() => {}} compact />
            </div>
          </div>
        </div>
      </DemoCard>

      {/* ── 25g. Scenarios — when batch matters ── */}
      <DemoCard label="25g. When batch matters &mdash; real user scenarios">
        <div className="space-y-2">
          {[
            {
              scenario: "Inbox triage",
              freq: "Weekly",
              action: "Move to topics",
              desc: "20 unassigned memories. Select by theme, batch move to topics. Primary batch use case.",
            },
            {
              scenario: "Context handoff",
              freq: "Per project",
              action: "Copy context",
              desc: "Starting new project. Select all auth memories, copy as <memax-context> for agent.",
            },
            {
              scenario: "Dream cleanup",
              freq: "Monthly",
              action: "Batch forget",
              desc: "After dreams merge duplicates. Forget stale memories the dream engine didn't catch.",
            },
            {
              scenario: "Knowledge backup",
              freq: "On demand",
              action: "Export .md",
              desc: "Leaving project. Export topic memories as .md file for handoff.",
            },
          ].map((s) => (
            <div
              key={s.scenario}
              className="flex items-start gap-3 px-3 py-2.5 rounded-xl hover:bg-surface-1 transition-colors"
            >
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-[13px] text-fg-1 font-medium">
                    {s.scenario}
                  </span>
                  <span className="text-[10px] text-fg-4 bg-surface-2 px-1.5 py-0.5 rounded flex items-center gap-1">
                    <Clock className="h-2.5 w-2.5" />
                    {s.freq}
                  </span>
                </div>
                <p className="text-[12px] text-fg-3 mt-0.5">{s.desc}</p>
              </div>
              <span className="text-[10px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded shrink-0 mt-0.5">
                {s.action}
              </span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── 25h. State machine — all states ── */}
      <DemoCard label="25h. State Machine &mdash; complete lifecycle">
        <p className="text-[13px] text-fg-2 mb-4">
          Every batch action follows the same state machine. No dead states, no
          silent failures.
        </p>
        <div className="space-y-3">
          {[
            {
              state: "Idle",
              desc: "Normal rows. Native row indicators. No selection UI visible.",
              visual: "text-fg-3",
            },
            {
              state: "Selection active",
              desc: '"Select" → "Done" in header. Indicators morph to rings. Click toggles.',
              visual: "text-fg-2",
            },
            {
              state: "Toolbar visible",
              desc: "≥1 selected. Glass toolbar at bottom: count + Move/Copy/Export/Forget.",
              visual: "text-fg-1",
            },
            {
              state: "Confirm (forget only)",
              desc: 'Toolbar morphs in place: "Forget N memories? [Forget] [Keep]".',
              visual: "text-fg-1",
            },
            {
              state: "Pending",
              desc: "Toolbar shows spinner + count. Actions disabled. No user interaction.",
              visual: "text-fg-2",
            },
            {
              state: "Success",
              desc: 'Selection exits. Bar toast: "3 memories forgotten" / "3 memories moved" / "Copied".',
              visual: "text-fg-1",
            },
            {
              state: "Error",
              desc: 'Selection stays. Bar toast (error): "Failed to forget/move". User can retry.',
              visual: "text-fg-2",
            },
          ].map((s, i) => (
            <div
              key={s.state}
              className="flex items-start gap-3 px-3 py-2 rounded-xl bg-surface-1"
            >
              <span className="text-[12px] text-fg-4 font-mono tabular-nums shrink-0 mt-0.5 w-4">
                {i + 1}
              </span>
              <div className="flex-1 min-w-0">
                <span className={`text-[13px] font-medium ${s.visual}`}>
                  {s.state}
                </span>
                <p className="text-[12px] text-fg-3 mt-0.5">{s.desc}</p>
              </div>
            </div>
          ))}
        </div>

        {/* Loading toolbar mock */}
        <div className="mt-4">
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Pending state (toolbar morphs to spinner)
          </p>
          <div className="flex justify-center">
            <div
              className="flex items-center gap-2 rounded-2xl px-4 py-2"
              style={{
                background: "var(--bar-bg, var(--card))",
                border: "1px solid var(--bar-border)",
                boxShadow: "0 4px 24px oklch(0 0 0 / 0.08)",
                backdropFilter: "blur(24px)",
                WebkitBackdropFilter: "blur(24px)",
              }}
            >
              <span className="h-3.5 w-3.5 border-2 border-fg-3 border-t-transparent rounded-full animate-spin" />
              <span className="text-[13px] text-fg-2">3 memories...</span>
            </div>
          </div>
        </div>

        {/* Success toast mock */}
        <div className="mt-3">
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            Success toast (bar notification)
          </p>
          <div className="flex justify-center">
            <div className="flex items-center gap-2 rounded-xl bg-surface-1 px-4 py-2.5">
              <Check
                className="h-3.5 w-3.5"
                style={{ color: "oklch(0.72 0.19 145)" }}
              />
              <span className="text-[13px] text-fg-2">
                3 memories forgotten
              </span>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* Implementation notes (LLM reference) */}
      <div
        className="rounded-lg px-4 py-3 space-y-1.5 mt-2"
        style={{ background: "oklch(from var(--foreground) l c h / 0.03)" }}
      >
        <p className="text-[11px] font-medium text-fg-3 uppercase tracking-wider">
          Architecture (for LLM agents)
        </p>
        <p className="text-[12px] text-fg-2">
          <span className="font-mono text-fg-1">SelectionProvider</span> &mdash;
          section-scoped (not layout-level). Each section wraps its own provider
          from the parent (TopicGrid, TopicDetailPage). useSelection() inside
          the section reads the correct context. Esc exits globally.
        </p>
        <p className="text-[12px] text-fg-2">
          <span className="font-mono text-fg-1">BatchToolbar</span> &mdash;
          portals to document.body via createPortal (escapes overflow-hidden +
          transform ancestors). z-60 (above bar z-50). Signals GlobalBar to hide
          via batch-active.ts bridge. Slides up (y 24&rarr;0, opacity, FAST
          spring). Uses bar tokens (--bar-bg, --bar-border).
        </p>
        <p className="text-[12px] text-fg-2">
          <span className="font-mono text-fg-1">batch-active.ts</span> &mdash;
          module-level signal (same pattern as mutation-toast.ts). BatchToolbar
          signals active/inactive on mount/unmount. GlobalBar listens: bar
          slides down + fades (marginTop 0&rarr;24, avoids transform conflict
          with translateX(-50%)). Bar/toolbar share same bottom position &mdash;
          container morphing with animated swap.
        </p>
        <p className="text-[12px] text-fg-2">
          <span className="font-mono text-fg-1">MemoryRow</span> &mdash;
          indicator slot morphs native indicator &rarr; ring &rarr; check &rarr;
          native indicator (spring easing, same slot). Click: selectionActive ?
          toggle : navigate. Long-press 500ms on mobile enters selection.
        </p>
        <p className="text-[12px] text-fg-2">
          <span className="font-mono text-fg-1">FilterDropdown</span> &mdash;
          portalled to body (escapes card overflow-hidden). Positioned via
          anchorRef getBoundingClientRect.
        </p>
        <p className="text-[12px] text-fg-2">
          <span className="font-mono text-fg-1">Toasts</span> &mdash; via
          MutationCache meta (universal async action pattern, kitchen 28). No
          per-callsite toast wiring. Callsite callbacks for UI side-effects only
          (selection.exit()).
        </p>
        <p className="text-[12px] text-fg-2">
          <span className="font-mono text-fg-1">Server</span>: POST
          /v1/memories/batch-delete (single SQL), POST /v1/memories/batch-move
          (topic or hub). Atomic &mdash; zero partial state.
        </p>
      </div>
    </Section>
  );
}
