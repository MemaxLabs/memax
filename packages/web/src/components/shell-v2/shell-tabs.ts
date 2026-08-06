/**
 * SHELL_TABS — canonical list of v2 left-rail tabs.
 *
 * Single source of truth shared between desktop `<LeftRail>` and mobile
 * `<MobileDrawer>` (plan 22). Adding a new tab = adding one entry here.
 *
 * Tab routing:
 *   - `brain`    → `/brain`
 *   - `memories` → `/h/<active-hub-slug>/memories` (resolved per-render
 *                  by the rail using `useActiveHub()` + `hubRouteSlug` —
 *                  this constant intentionally does NOT bake an active-
 *                  hub assumption; the rail is the only consumer of the
 *                  routing decision and pulls the active hub there)
 *   - `agents`   → `/agents`     (built in plan 24)
 *   - `pulse`    → `/pulse`      (plan 25 P4 — the pulse board surface;
 *                  replaced the retired `/inbox` tab, which now only
 *                  survives as a redirect for old deep links)
 *
 * Visible labels live in `t.nav.tabs.<id>` (i18n requirement). Each tab
 * exposes a stable `id` callers use to look up its label at render time.
 *
 * `hasSecondary: true` means the tab pairs with `<PinnedSecondaryPanel>`.
 * Today only Memories has one (the topic explorer).
 */

import type { LucideIcon } from "lucide-react";
import { Brain, Bot, Library, Sparkles } from "lucide-react";

export type ShellTabId = "brain" | "memories" | "agents" | "pulse";

export interface ShellTabSpec {
  id: ShellTabId;
  /**
   * Lucide icon component rendered in the rail. Stroke width and color
   * are set by the rail (active vs inactive variants); the icon stays
   * neutral here.
   */
  icon: LucideIcon;
  /**
   * Whether this tab pairs with a `<PinnedSecondaryPanel>`. Today only
   * `memories` does. Drives the click-active-tab toggle behavior.
   */
  hasSecondary: boolean;
  /**
   * Static fallback path. Used when the tab does NOT need active-hub
   * context (brain, agents, pulse). The memories tab ignores this and
   * resolves at render time via `useActiveHub()` + `hubRouteSlug` so
   * users in a team hub click "Memories" and land on the team hub's
   * memories grid, not personal.
   */
  staticPath: string | null;
}

// Order reflects product hierarchy (2026-05-20 reorder per user
// direction): Memories is the foundation — what the user has
// dumped. Agents is the surface that wrote / will write into it.
// Brain is the conversational surface ON TOP of those memories.
// Pulse is what memax noticed + what's waiting on you, last.
//
// Pulse resolves to the STATIC `/pulse` route rather than mirroring
// the memories tab's active-hub path: the board is embedded in the
// memories page too (BoardSection), and if both tabs resolved to
// `/h/<slug>/memories` the pathname-derived active-tab lookup would
// be ambiguous. `/pulse` renders the same board full-page for the
// active hub, and gives the retired `/inbox` route a redirect target.
export const SHELL_TABS: readonly ShellTabSpec[] = [
  { id: "memories", icon: Library, hasSecondary: true, staticPath: null },
  { id: "agents", icon: Bot, hasSecondary: false, staticPath: "/agents" },
  { id: "brain", icon: Brain, hasSecondary: true, staticPath: "/brain" },
  { id: "pulse", icon: Sparkles, hasSecondary: false, staticPath: "/pulse" },
] as const;

export function getShellTab(id: ShellTabId): ShellTabSpec {
  const tab = SHELL_TABS.find((t) => t.id === id);
  if (!tab) {
    throw new Error(`Unknown shell tab id: ${id}`);
  }
  return tab;
}
