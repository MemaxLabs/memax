"use client";

import type { ResolvedMemoryAttribution } from "@/lib/memory-attribution";

export function AgentInlineIdentity({
  attribution,
  className = "",
  iconOnly = false,
}: {
  attribution: ResolvedMemoryAttribution;
  className?: string;
  /**
   * Render only the agent icon (no name). Used in compact contexts
   * like the mobile card-footer where the team-hub attribution
   * collapses to `[author icon] via [agent icon]` with no labels.
   */
  iconOnly?: boolean;
}) {
  if (!attribution.hasAgent) return null;

  const AgentIcon = attribution.agentIdentity?.icon;

  return (
    <span
      className={`inline-flex min-w-0 items-center gap-1 ${className}`}
      title={iconOnly ? attribution.agentDisplayName : undefined}
    >
      {attribution.agentIconEmoji ? (
        <span className="flex h-3.5 w-3.5 shrink-0 items-center justify-center text-[11px] leading-none">
          {attribution.agentIconEmoji}
        </span>
      ) : AgentIcon ? (
        <span className="flex h-3.5 w-3.5 shrink-0 items-center justify-center">
          <AgentIcon
            className="h-3.5 w-3.5"
            style={{ color: attribution.agentIdentity?.color }}
            strokeWidth={1.8}
          />
        </span>
      ) : (
        <span
          className="flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-md text-[9px] font-medium text-fg-2"
          style={{ background: "oklch(from var(--foreground) l c h / 0.08)" }}
        >
          {attribution.agentDisplayName[0]?.toUpperCase() ?? "A"}
        </span>
      )}
      {!iconOnly ? (
        <span className="truncate">{attribution.agentDisplayName}</span>
      ) : null}
    </span>
  );
}
