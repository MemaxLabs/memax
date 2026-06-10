"use client";

/**
 * `<StatusDot>` — visual status indicator for an agent's recent
 * activity (just_now / today / idle / configured).
 *
 * Extracted from `agent-configs-section.tsx` (plan 24 phase 0) so
 * `<AgentList>` and any future agent surface can render it without
 * pulling in the whole settings section.
 *
 * just_now / today → emerald (live or fresh).
 * idle / configured → grey, distinguished by fill vs outline.
 *
 * just_now gets Tailwind's `animate-ping` ring — an outward-scaling
 * ripple that fades, like Apple's AirDrop / FaceTime live dot. That
 * is the same pattern kitchen §16 documents for activity status;
 * prior use of `state-slow-breathe` (opacity-only in-place) didn't
 * read as "live" at a glance, just as "loading."
 */

import type { AgentStatus } from "@/components/features/settings/shared/agent-status";

export function StatusDot({ status }: { status: AgentStatus }) {
  const colors: Record<AgentStatus, string> = {
    just_now: "oklch(0.72 0.19 145)",
    today: "oklch(0.72 0.19 145)",
    idle: "var(--fg-3)",
    configured: "var(--fg-3)",
  };
  return (
    <span className="relative flex h-2 w-2">
      {status === "just_now" && (
        <span
          className="absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping"
          style={{ backgroundColor: colors[status] }}
        />
      )}
      <span
        className="relative inline-flex h-2 w-2 rounded-full"
        style={{
          backgroundColor:
            status === "configured" ? "transparent" : colors[status],
          border: status === "configured" ? "1px solid var(--fg-3)" : "none",
        }}
      />
    </span>
  );
}

export type StatusLabelKey =
  | "statusJustNow"
  | "statusToday"
  | "statusIdle"
  | "statusConfigured";

export function statusLabelKey(status: AgentStatus): StatusLabelKey {
  switch (status) {
    case "just_now":
      return "statusJustNow";
    case "today":
      return "statusToday";
    case "idle":
      return "statusIdle";
    case "configured":
      return "statusConfigured";
  }
}
