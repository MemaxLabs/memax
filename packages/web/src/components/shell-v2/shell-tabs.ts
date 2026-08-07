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
 *   - `pulse`    → `/h/<active-hub-slug>/pulse` (resolved per-render the
 *                  same way memories is — the board is per-hub)
 *
 * Visible labels live in `t.nav.tabs.<id>` (i18n requirement). Each tab
 * exposes a stable `id` callers use to look up its label at render time.
 *
 * `hasSecondary: true` means the tab pairs with `<PinnedSecondaryPanel>`.
 * Today only Memories has one (the topic explorer).
 */

import type { LucideIcon } from "lucide-react";
import { Activity, Brain, Bot, Library } from "lucide-react";

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
   * Static path. Used when the tab does NOT need active-hub context
   * (brain, agents). Hub-scoped tabs (`memories`, `pulse`) set this to
   * `null` and resolve at render time via `useActiveHub()` +
   * `hubRouteSlug` so users in a team hub click the tab and land on the
   * team hub's surface, not personal.
   */
  staticPath: string | null;
}

// Order reflects daily-usage frequency + mental model (2026-08
// founder reorder): Memories first — the home, the content itself.
// Pulse second — what's new / what's waiting on you. Brain third —
// the conversational surface where you act on those memories. Agents
// last — setup, visited rarely.
//
// Pulse resolves per-hub (`/h/<slug>/pulse`), like memories. It used to
// be a static `/pulse` that read the hub from active-hub state; that
// silently showed the WRONG hub's board whenever the URL and the stored
// active hub disagreed (create a board in team hub X at
// `/h/X/memories`, open Pulse, get personal — the board looked gone).
// The two tabs stay distinguishable because they differ in the segment
// AFTER the slug, which `getShellTabForPath` keys on.
export const SHELL_TABS: readonly ShellTabSpec[] = [
  { id: "memories", icon: Library, hasSecondary: true, staticPath: null },
  // Activity — the EKG pulse line. Literal 脉搏, and it doesn't fight
  // the ✦ signature mark, which stays reserved for memax's own voice.
  { id: "pulse", icon: Activity, hasSecondary: false, staticPath: null },
  { id: "brain", icon: Brain, hasSecondary: true, staticPath: "/brain" },
  { id: "agents", icon: Bot, hasSecondary: false, staticPath: "/agents" },
] as const;

export function getShellTab(id: ShellTabId): ShellTabSpec {
  const tab = SHELL_TABS.find((t) => t.id === id);
  if (!tab) {
    throw new Error(`Unknown shell tab id: ${id}`);
  }
  return tab;
}
