// Four meaningful states, not three. The prior "recent" bucket ran for
// 7 days, which read as "active recently" even when the user hadn't
// touched the agent in 5+ days. Tightened to match how people actually
// think about developer tool usage:
//
//   just_now  — <5m          green + pulse       "you're live"
//   today     — <24h         green solid         "used today"
//   idle      — has observed green, but >24h     grey solid
//                                                "has been used, not recently"
//   configured — never observed                  grey outlined
//                                                "never used at all"
//
// The idle / configured split matters because the remediation differs:
// idle agents are fine (the user just hasn't used them recently);
// configured ones might indicate a broken setup worth investigating.
export type AgentStatus = "configured" | "idle" | "today" | "just_now";

const MINUTE = 60 * 1000;
const JUST_NOW = 5 * MINUTE;
const TODAY = 24 * 60 * MINUTE;

export function getAgentStatus(agent: {
  memory_count: number;
  last_active_at?: string | null;
  last_observed_at?: string | null;
}): AgentStatus {
  const observedAt = agent.last_observed_at ?? agent.last_active_at;
  if (!observedAt) return "configured";
  const elapsed = Date.now() - new Date(observedAt).getTime();
  if (elapsed < JUST_NOW) return "just_now";
  if (elapsed < TODAY) return "today";
  return "idle";
}

export function getAgentStatusColor(status: AgentStatus): string {
  switch (status) {
    case "just_now":
    case "today":
      return "text-emerald-500";
    case "idle":
    case "configured":
      return "text-fg-3";
  }
}

// Note on just_now: this helper only returns classes for the SOLID
// dot. The outward-scaling ripple ring (Tailwind `animate-ping`) is a
// separate rendering concern — kitchen §16 and `agent-configs-section`
// StatusDot compose it as a sibling absolute span so it can scale past
// the dot's bounds without expanding the parent. Callers that want the
// live AirDrop-style pulse should follow that two-span pattern rather
// than trying to bake it into this single class string.
export function getAgentStatusDotClass(status: AgentStatus): string {
  switch (status) {
    case "just_now":
      return "bg-emerald-500";
    case "today":
      return "bg-emerald-500";
    case "idle":
      // Solid grey fill, no border — distinguishes "has been used but
      // not recently" from "never used" (which renders outlined).
      return "bg-fg-3";
    case "configured":
      return "border border-fg-3 bg-transparent";
  }
}

/**
 * Sort priority:
 *   0 just_now   → top of the list (live right now)
 *   1 today      → used today
 *   2 idle       → used before, just not recently
 *   3 configured → never used — sinks to the bottom so it doesn't
 *                  clutter the active set
 */
export function getAgentStatusSortOrder(status: AgentStatus): number {
  switch (status) {
    case "just_now":
      return 0;
    case "today":
      return 1;
    case "idle":
      return 2;
    case "configured":
      return 3;
  }
}
