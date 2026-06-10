"use client";

import { useState } from "react";
import { Section, DemoCard } from "../_shared";

// ═══════════════════════════════════════════════
// 28. Async Action Pattern
//
// Universal pattern for ALL async mutations.
// Maps to: lib/query-client.ts (MutationCache)
//          lib/mutation-toast.ts (bridge)
//          contexts/bar-context.tsx (registration)
//          hooks/use-*.ts (meta on every mutation)
//
// Every mutation in the app follows this pattern.
// No per-callsite toast wiring. No inconsistent feedback.
// ═══════════════════════════════════════════════

const SIGNATURE = "var(--signature)";

// ── State machine ──

type ActionState = "idle" | "pending" | "success" | "error";

function StateMachineDemo() {
  const [state, setState] = useState<ActionState>("idle");

  const trigger = () => {
    setState("pending");
    setTimeout(
      () => {
        setState(Math.random() > 0.3 ? "success" : "error");
        setTimeout(() => setState("idle"), 2000);
      },
      800 + Math.random() * 400,
    );
  };

  const stateConfig: Record<
    ActionState,
    { label: string; color: string; bg: string }
  > = {
    idle: { label: "Idle", color: "var(--fg-3)", bg: "transparent" },
    pending: {
      label: "Pending",
      color: SIGNATURE,
      bg: "oklch(0.62 0.16 290 / 0.08)",
    },
    success: {
      label: "Success",
      color: "oklch(0.55 0.15 155)",
      bg: "oklch(0.55 0.15 155 / 0.06)",
    },
    error: {
      label: "Error",
      color: "var(--destructive)",
      bg: "oklch(from var(--destructive) l c h / 0.06)",
    },
  };

  const current = stateConfig[state];

  return (
    <div className="space-y-4">
      {/* State machine diagram */}
      <div className="flex items-center gap-2 flex-wrap">
        {(["idle", "pending", "success", "error"] as ActionState[]).map(
          (s, i) => (
            <div key={s} className="flex items-center gap-2">
              {i > 0 && (
                <span className="text-[10px] text-fg-4">
                  {i === 1 ? "→" : i === 2 ? "→" : "→"}
                </span>
              )}
              <span
                className="text-[12px] px-2 py-0.5 rounded-md font-medium transition-all"
                style={{
                  color: state === s ? stateConfig[s].color : "var(--fg-4)",
                  background: state === s ? stateConfig[s].bg : "transparent",
                  border:
                    state === s
                      ? "1px solid currentColor"
                      : "1px solid transparent",
                }}
              >
                {stateConfig[s].label}
              </span>
            </div>
          ),
        )}
        <span className="text-[10px] text-fg-4 ml-1">→ idle</span>
      </div>

      {/* Live demo */}
      <div className="flex items-center gap-3">
        <button
          onClick={trigger}
          disabled={state === "pending"}
          className="text-[13px] px-3 py-1.5 rounded-lg font-medium transition-all cursor-pointer disabled:opacity-50"
          style={{
            background: "var(--foreground)",
            color: "var(--background)",
          }}
        >
          {state === "pending" ? "Processing..." : "Trigger Action"}
        </button>
        <div
          className="text-[13px] font-medium px-3 py-1 rounded-lg transition-all"
          style={{
            color: current.color,
            background: current.bg,
            opacity: state === "idle" ? 0.5 : 1,
          }}
        >
          {state === "idle" && "Ready"}
          {state === "pending" && (
            <span className="state-slow-breathe">Processing...</span>
          )}
          {state === "success" && "✓ Done"}
          {state === "error" && "✕ Failed"}
        </div>
      </div>
    </div>
  );
}

// ── Toast styles (matching bar-notification-card.tsx) ──

function ToastStylesDemo() {
  return (
    <div className="space-y-2">
      {[
        {
          type: "success",
          label: "Success",
          message: "3 memories forgotten",
          color: "oklch(0.55 0.15 155)",
          icon: "✓",
        },
        {
          type: "error",
          label: "Error",
          message: "Couldn't forget memories",
          color: "var(--destructive)",
          icon: "✕",
        },
        {
          type: "info",
          label: "Info / Pending",
          message: "Remembering...",
          color: SIGNATURE,
          icon: "✦",
        },
      ].map(({ type, label, message, color, icon }) => (
        <div key={type} className="flex items-center gap-3">
          <span className="text-[11px] text-fg-4 w-20 shrink-0">{label}</span>
          <div
            className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-[13px]"
            style={{
              color,
              background: `oklch(from ${color} l c h / 0.06)`,
            }}
          >
            <span className="text-[11px]">{icon}</span>
            <span>{message}</span>
          </div>
        </div>
      ))}
    </div>
  );
}

// ── Pattern reference (for LLM consumption) ──

function PatternReference() {
  return (
    <div className="space-y-3 text-[13px] text-fg-2">
      <div
        className="rounded-lg px-4 py-3 space-y-2"
        style={{ background: "oklch(from var(--foreground) l c h / 0.03)" }}
      >
        <div className="text-[11px] font-medium text-fg-3 uppercase tracking-wider">
          Architecture
        </div>
        <div className="space-y-1 text-[12px] text-fg-2 leading-relaxed">
          <p>
            <strong className="text-fg-1">MutationCache</strong> on QueryClient
            handles ALL mutation feedback globally.
          </p>
          <p>
            <strong className="text-fg-1">meta.successMessage</strong> — static
            string or{" "}
            <code className="text-[11px]">(data, vars) =&gt; string</code> for
            dynamic messages.
          </p>
          <p>
            <strong className="text-fg-1">meta.errorMessage</strong> — same
            signature. Shown for 5s with dismiss.
          </p>
          <p>
            <strong className="text-fg-1">meta.skipGlobalToast</strong> — for
            mutations with custom UX (push flow with undo).
          </p>
          <p>
            <strong className="text-fg-1">Bridge</strong>: module-level callback
            ref (mutation-toast.ts) wired by BarProvider on mount.
          </p>
        </div>
      </div>

      <div
        className="rounded-lg px-4 py-3 space-y-2"
        style={{ background: "oklch(from var(--foreground) l c h / 0.03)" }}
      >
        <div className="text-[11px] font-medium text-fg-3 uppercase tracking-wider">
          Rules for Adding New Mutations
        </div>
        <ol className="space-y-1 text-[12px] text-fg-2 leading-relaxed list-decimal list-inside">
          <li>
            Add{" "}
            <code className="text-[11px]">
              meta: &#123; errorMessage: t.toast.* &#125;
            </code>{" "}
            to every mutation — even if optimistic rollback handles the UI.
          </li>
          <li>
            Add <code className="text-[11px]">successMessage</code> only for
            irreversible or batch actions (disconnect, batch delete).
          </li>
          <li>
            Use <code className="text-[11px]">skipGlobalToast: true</code> only
            when the mutation has its own undo-capable feedback.
          </li>
          <li>
            Callsite callbacks are for UI side-effects only (selection.exit(),
            navigation) — never for toasts.
          </li>
          <li>
            All messages go through i18n (
            <code className="text-[11px]">t.*</code>). Dynamic messages use{" "}
            <code className="text-[11px]">interpolate()</code>.
          </li>
          <li>
            <code className="text-[11px]">useBarToast()</code> is ONLY for
            non-mutation feedback (clipboard copy, DnD drop). Never use it
            inside mutation callbacks — MutationCache handles that.
          </li>
        </ol>
      </div>
    </div>
  );
}

// ── Mutation inventory ──

function MutationInventory() {
  const mutations = [
    {
      hook: "useBatchDelete",
      success: "dynamic (count)",
      error: "t.batch.forgetFailed",
    },
    {
      hook: "useBatchMoveToTopic",
      success: "dynamic (count)",
      error: "t.batch.moveFailed",
    },
    {
      hook: "useBatchMoveToHub",
      success: "dynamic (count)",
      error: "t.batch.moveFailed",
    },
    { hook: "useCreateMemory", success: "—", error: "— (skipGlobalToast)" },
    {
      hook: "useUpdateMemory",
      success: "—",
      error: "t.toast.updateFailed",
    },
    {
      hook: "useDeleteMemory",
      success: "—",
      error: "t.toast.deleteFailed",
    },
    {
      hook: "useShareMemory",
      success: "t.toast.shared",
      error: "t.toast.shareFailed",
    },
    {
      hook: "useDisconnectAgent",
      success: "t.toast.disconnected",
      error: "t.toast.disconnectFailed",
    },
    {
      hook: "useUpdateAgent",
      success: "—",
      error: "t.toast.updateFailed",
    },
    {
      hook: "useDeleteAgentConfig",
      success: "—",
      error: "t.toast.deleteFailed",
    },
    {
      hook: "useRevokeApiKey",
      success: "—",
      error: "t.toast.revokeFailed",
    },
    {
      hook: "useCreateTopic",
      success: "—",
      error: "t.toast.topicFailed",
    },
    {
      hook: "useUpdateTopic",
      success: "—",
      error: "t.toast.topicFailed",
    },
    {
      hook: "useDeleteTopic",
      success: "—",
      error: "t.toast.topicFailed",
    },
    {
      hook: "useMemoryMove (authoritative user-move path)",
      success: "t.batch.moved / t.toast.moved / t.toast.movedTo",
      error: "t.batch.moveFailed / targetNotFound / noWriteAccess",
    },
    {
      hook: "useResolveReview",
      success: "—",
      error: "t.toast.reviewFailed",
    },
    {
      hook: "useUpdateSettings",
      success: "—",
      error: "t.toast.settingsFailed",
    },
    {
      hook: "useDreamTrigger",
      success: "—",
      error: "t.toast.organizeFailed",
    },
  ];

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[12px]">
        <thead>
          <tr className="text-fg-3 text-left">
            <th className="pb-1.5 pr-4 font-medium">Hook</th>
            <th className="pb-1.5 pr-4 font-medium">Success</th>
            <th className="pb-1.5 font-medium">Error</th>
          </tr>
        </thead>
        <tbody className="text-fg-2">
          {mutations.map((m) => (
            <tr key={m.hook} className="border-t border-border/20">
              <td className="py-1 pr-4 font-mono text-[11px] text-fg-1">
                {m.hook}
              </td>
              <td className="py-1 pr-4">
                {m.success === "—" ? (
                  <span className="text-fg-4">silent</span>
                ) : (
                  m.success
                )}
              </td>
              <td className="py-1">
                {m.error.includes("skip") ? (
                  <span className="text-fg-4">custom</span>
                ) : (
                  <code className="text-[10px]">{m.error}</code>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ── Export ──

export function AsyncActionsSection() {
  return (
    <Section
      title="28. Async Action Pattern"
      description="Universal mutation lifecycle: Idle → Pending → Success/Error → Idle. MutationCache global handlers with meta-driven feedback. Zero per-callsite wiring."
    >
      <DemoCard label="28a · State Machine">
        <StateMachineDemo />
      </DemoCard>

      <DemoCard label="28b · Toast Styles">
        <ToastStylesDemo />
      </DemoCard>

      <DemoCard label="28c · Pattern Reference (LLM)">
        <PatternReference />
      </DemoCard>

      <DemoCard label="28d · Mutation Inventory">
        <MutationInventory />
      </DemoCard>
    </Section>
  );
}
