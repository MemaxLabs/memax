import type { Memory } from "@/hooks/use-memories";
import {
  getAgentIdentity,
  resolveAgentIdentity,
  type AgentIdentity,
} from "@memaxlabs/ui/tokens/agents";

interface AttributionSlugMemory {
  source_agent?: string | null;
  provenance?: {
    created_by_slug?: string | null;
  } | null;
}

export interface ResolvedMemoryAttribution {
  hasAgent: boolean;
  isOwnMemory: boolean;
  authorName: string | null;
  authorAvatar: string | null;
  createdByType: "human" | "agent" | "system";
  initiationType:
    | "human_direct"
    | "human_requested_agent"
    | "agent_proactive"
    | "agent_automatic"
    | "import"
    | "unknown";
  isHumanRequestedAgent: boolean;
  isAgentCapture: boolean;
  isLegacyUnknownAgentAttribution: boolean;
  /**
   * True when the memory is system-authored (`source === "system"`).
   * Plan 23 onboarding seeds set this; future system-authored memories
   * (dream summaries, system notices, etc.) will too. Consumers should
   * render the Memax brand identity instead of a person/agent chip.
   */
  isSystem: boolean;
  agentDisplayName: string;
  agentIconEmoji: string | null;
  agentIdentity: AgentIdentity | null;
}

export function getAttributionAgentSlug(
  memory: AttributionSlugMemory | null | undefined,
): string {
  if (!memory) return "";
  return memory.provenance?.created_by_slug || memory.source_agent || "";
}

export function resolveMemoryAttribution(
  memory: Memory,
  currentUserId?: string,
): ResolvedMemoryAttribution {
  const provenance = memory.provenance;
  const isOwnMemory = !currentUserId || memory.owner_id === currentUserId;
  // Plan 23 — system-authored memories (onboarding seeds, future
  // dream summaries) short-circuit the normal human/agent resolution.
  // Provenance fields aren't reliable here because the worker-side
  // provenance shape doesn't yet have a system actor type; the
  // `source` field is the durable signal.
  //
  // Render these through the agent attribution path (`hasAgent: true`,
  // `agentIdentity: <memax>`) so memory cards show the Memax brand
  // identity instead of falling back to "you" via the personal-recent
  // attribution branch in memory-row-presentation. The Memax actor
  // pushed these on the user's behalf — attribution should reflect
  // that.
  if (memory.source === "system") {
    const memaxIdentity = getAgentIdentity("memax");
    return {
      hasAgent: true,
      isOwnMemory,
      authorName: null,
      authorAvatar: null,
      createdByType: "system",
      initiationType: "agent_automatic",
      isHumanRequestedAgent: false,
      isAgentCapture: true,
      isLegacyUnknownAgentAttribution: false,
      isSystem: true,
      agentDisplayName: memaxIdentity.displayName,
      // No emoji override — let <AgentInlineIdentity> render the
      // signature-pink Bot icon from AGENT_IDENTITIES.memax so the
      // memory-card chip matches the agents-tab tile exactly. Codex
      // pass 1 flagged that the prior "✦" override forced the
      // star-emoji branch and diverged the two surfaces.
      agentIconEmoji: null,
      agentIdentity: memaxIdentity,
    };
  }
  const authorName =
    !isOwnMemory && memory.author_name ? memory.author_name : null;
  const authorAvatar =
    !isOwnMemory && memory.author_avatar_url ? memory.author_avatar_url : null;
  const actorAgentSlug = getAttributionAgentSlug(memory);
  const createdByType =
    provenance?.created_by_type ?? (actorAgentSlug ? "agent" : "human");
  const initiationType =
    provenance?.initiation_type ??
    (actorAgentSlug ? "unknown" : "human_direct");
  const collaboratorAgentSlug =
    provenance?.assisted_by_agent ||
    (initiationType === "human_requested_agent"
      ? memory.source_agent || ""
      : "");
  const isHumanRequestedAgent =
    initiationType === "human_requested_agent" && !!collaboratorAgentSlug;
  const displayAgentSlug = isHumanRequestedAgent
    ? collaboratorAgentSlug
    : actorAgentSlug;
  const hasAgent = !!displayAgentSlug;
  const isLegacyUnknownAgentAttribution =
    !!actorAgentSlug &&
    initiationType === "unknown" &&
    provenance?.attribution_source === "legacy_source_agent";
  const isAgentCapture =
    !!actorAgentSlug &&
    !isLegacyUnknownAgentAttribution &&
    !isHumanRequestedAgent &&
    (initiationType === "agent_proactive" ||
      initiationType === "agent_automatic" ||
      initiationType === "unknown");

  if (!hasAgent) {
    return {
      hasAgent: false,
      isOwnMemory,
      authorName,
      authorAvatar,
      createdByType,
      initiationType,
      isHumanRequestedAgent,
      isAgentCapture,
      isLegacyUnknownAgentAttribution,
      isSystem: false,
      agentDisplayName: "",
      agentIconEmoji: null,
      agentIdentity: null,
    };
  }

  const agent = resolveAgentIdentity(displayAgentSlug, {
    display_name: isHumanRequestedAgent
      ? undefined
      : provenance?.created_by_display_name || memory.agent_display_name,
    icon: memory.agent_icon,
  });

  return {
    hasAgent: true,
    isOwnMemory,
    authorName,
    authorAvatar,
    createdByType,
    initiationType,
    isHumanRequestedAgent,
    isAgentCapture,
    isLegacyUnknownAgentAttribution,
    isSystem: false,
    agentDisplayName: agent.displayName,
    agentIconEmoji: agent.iconEmoji ?? null,
    agentIdentity: agent,
  };
}
