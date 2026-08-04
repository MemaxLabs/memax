/**
 * Unified agent identity system.
 *
 * Single source of truth for agent display names, icons, and accent colors.
 * Used across memory-row attribution, Settings > Agents, and kitchen.
 *
 * Icon is fixed per agent type (not user-configurable) for scan-ability —
 * users can instantly recognize which agent pushed a memory by color + shape.
 */

import type { LucideIcon } from "lucide-react";
import {
  Terminal,
  MousePointer2,
  Code2,
  Sparkles,
  CircleDot,
  Wind,
  Bot,
  Feather,
} from "lucide-react";

export interface AgentIdentity {
  /** Immutable slug from api_keys.agent_name (e.g. "claude-code") */
  slug: string;
  /** Default display name (user can override via rename) */
  displayName: string;
  /** Lucide icon component */
  icon: LucideIcon;
  /** oklch accent color for icon background + tint */
  color: string;
}

/**
 * Known agent identities. Keyed by agent_name slug.
 * Fallback for unknown agents: Bot icon + neutral color.
 */
export const AGENT_IDENTITIES: Record<string, AgentIdentity> = {
  "claude-code": {
    slug: "claude-code",
    displayName: "Claude Code",
    icon: Terminal,
    color: "oklch(0.65 0.15 30)", // warm coral
  },
  "claude-ai": {
    slug: "claude-ai",
    displayName: "Claude",
    icon: Bot,
    color: "oklch(0.65 0.15 30)", // warm coral (same family as claude-code)
  },
  cursor: {
    slug: "cursor",
    displayName: "Cursor",
    icon: MousePointer2,
    color: "oklch(0.65 0.15 145)", // teal
  },
  codex: {
    slug: "codex",
    displayName: "Codex",
    icon: Code2,
    color: "oklch(0.65 0.15 250)", // blue
  },
  gemini: {
    slug: "gemini",
    displayName: "Gemini CLI",
    icon: Sparkles,
    color: "oklch(0.65 0.15 60)", // amber
  },
  copilot: {
    slug: "copilot",
    displayName: "GitHub Copilot",
    icon: CircleDot,
    color: "oklch(0.65 0.12 170)", // seafoam
  },
  windsurf: {
    slug: "windsurf",
    displayName: "Windsurf",
    icon: Wind,
    color: "oklch(0.65 0.15 210)", // sky
  },
  openclaw: {
    slug: "openclaw",
    displayName: "OpenClaw",
    icon: Bot,
    color: "oklch(0.65 0.12 300)", // purple
  },
  hermes: {
    slug: "hermes",
    displayName: "Hermes",
    icon: Feather,
    color: "oklch(0.70 0.13 85)", // gold
  },
  opencode: {
    slug: "opencode",
    displayName: "OpenCode",
    icon: Code2,
    color: "oklch(0.65 0.12 120)", // lime
  },
  generic: {
    slug: "generic",
    displayName: "Other",
    icon: Bot,
    color: "oklch(0.65 0.10 260)", // soft indigo
  },
  memax: {
    slug: "memax",
    displayName: "Memax Agent",
    icon: Bot,
    color: "oklch(0.65 0.18 330)", // signature pink — coming soon
  },
};

/** Fallback identity for unknown agent slugs — uses the memax Bot icon */
const FALLBACK_IDENTITY: Omit<AgentIdentity, "slug" | "displayName"> = {
  icon: Bot,
  color: "oklch(0.65 0.10 260)", // soft indigo
};

/**
 * Get agent identity by slug. Returns known identity or a fallback
 * with the raw slug as display name.
 */
export function getAgentIdentity(slug: string): AgentIdentity {
  const known = AGENT_IDENTITIES[slug];
  if (known) return known;

  return {
    slug,
    displayName: slug,
    ...FALLBACK_IDENTITY,
  };
}

/**
 * Get the display name for an agent slug.
 * Shorthand for getAgentIdentity(slug).displayName.
 */
export function getAgentDisplayName(slug: string): string {
  return getAgentIdentity(slug).displayName;
}

/**
 * Resolve agent identity with server-side overrides (display_name, icon).
 * Priority: server display_name → AGENT_IDENTITIES displayName → raw slug
 * Icon: server icon emoji (rendered as text) → AGENT_IDENTITIES icon (Lucide component)
 */
export function resolveAgentIdentity(
  slug: string,
  serverAgent?: { display_name?: string; icon?: string },
): AgentIdentity & { iconEmoji?: string } {
  const base = getAgentIdentity(slug);
  if (!serverAgent) return base;
  return {
    ...base,
    displayName: serverAgent.display_name || base.displayName,
    iconEmoji: serverAgent.icon || undefined,
  };
}
