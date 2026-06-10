"use client";

/**
 * PinnedSecondaryPanel — secondary panel anchored flush against the
 * LeftRail. Today it serves both Memories (topic tree) and Brain
 * (chat sessions list); future tabs can opt in by setting
 * `hasSecondary: true` in shell-tabs.ts and mounting their own
 * children inside this primitive.
 *
 * Visual model:
 *   - position: fixed; `left = PANEL_INSET + RAIL_WIDTH_COLLAPSED`
 *     so the panel meets the rail's right edge with NO gap. Top +
 *     bottom page-edge insets remain.
 *   - `rounded-r-[var(--app-radius-surface)]` ONLY — the left edge
 *     is straight and tucks behind the rail visually (the rail
 *     z-stacks above the panel).
 *   - `glass-dropdown` material matches v1 PinnedTreeSidebar /
 *     plan 26 chrome.
 *   - z-index BELOW the rail (z-shell-secondary < z-shell-rail) so
 *     the rail z-stacks above the panel's straight left edge.
 *
 * Animation on hidden ↔ visible:
 *   - Children stay MOUNTED at all times — load-bearing for dnd-kit
 *     drop-target measurement (memory→topic drag) and stable
 *     react-query subscriptions.
 *   - Reveal uses transform-only translateX(-100% → 0) so children
 *     don't relayout per frame (codex 2026-05-04 fix-required:
 *     framer width-grow re-runs flex layout every frame and broke
 *     dnd rect measurements).
 *   - duration NORMAL (200ms) + EASE token; reduced-motion: opacity-only.
 *   - First paint waits for shell hydration to avoid the panel
 *     sliding in from off-screen on every page load when the user
 *     has `secondaryHidden[tab] = false` persisted.
 *
 * Header is tab-gated:
 *   - memories: hub-identity chip + close chevron (the panel is
 *     hub-scoped)
 *   - brain: close chevron only (hub-scoped sessions render their
 *     own grouping)
 *
 * Visibility is driven by `useShellState().secondaryHidden[tab]`.
 * The close chevron sets `secondaryHidden[tab] = true`. Re-open by
 * clicking the active rail tab again — when the panel is hidden the
 * rail is expanded with labels visible (see left-rail docstring), so
 * the active label IS the affordance.
 */

import { type ReactNode } from "react";
import { motion, useReducedMotion } from "framer-motion";
import { ChevronsLeft } from "lucide-react";
import { NORMAL, EASE } from "@memaxlabs/ui/tokens/motion";
import { useShellState } from "@/contexts/shell-state-context";
import { useTreePanel } from "@/components/features/topic/topic-tree-panel";
import { useAuth, useActiveHub } from "@/lib/auth";
import { HubIdentityChip } from "@/components/features/hub/hub-identity-chip";
import { useLocale } from "@/i18n";
import type { ShellTabId } from "./shell-tabs";
import {
  PANEL_INSET,
  PANEL_WIDTH,
  RAIL_WIDTH_COLLAPSED,
} from "@/lib/shell-geometry";

interface PinnedSecondaryPanelProps {
  tab: ShellTabId;
  children: ReactNode;
}

export function PinnedSecondaryPanel({
  tab,
  children,
}: PinnedSecondaryPanelProps) {
  const { secondaryHidden, toggleSecondary, isHydrated } = useShellState();
  const reduceMotion = useReducedMotion();
  const { t } = useLocale();
  const { user, hubs } = useAuth();
  const { activeHub, isTeamHub } = useActiveHub();
  // Drag-session bridge: while a memory→topic drag is in flight, the
  // panel must be visually visible (and its drop targets reachable)
  // even if the user has it hidden. Mirrors v1 PinnedTreeSidebar's
  // `expanded = isPinned || isDragSessionOpen` contract.
  // Scoped to memories ONLY — brain has no drag-target bridge.
  const { isDragSessionOpen } = useTreePanel();
  const hidden =
    secondaryHidden[tab] && !(tab === "memories" && isDragSessionOpen);

  // Reveal via transform translateX (GPU-only, no flex relayout).
  // x = -PANEL_WIDTH when hidden, 0 when visible. The rail no longer
  // hover-grows (left-rail.tsx 2026-05-19), so there's no rail-tracking
  // shift to compose anymore.
  const target = reduceMotion
    ? { opacity: hidden ? 0 : 1, x: 0 }
    : { opacity: hidden ? 0 : 1, x: hidden ? -PANEL_WIDTH : 0 };

  const showHubChip = tab === "memories" && hubs.length >= 2 && !!activeHub;

  return (
    <motion.aside
      role="region"
      aria-label={tab === "memories" ? t.nav.topicTreeRegion : t.nav.primary}
      aria-hidden={hidden}
      inert={hidden ? true : undefined}
      // Left edge is straight — the rail's right edge is flush
      // against this panel and z-stacks above to clip the slide.
      className="glass-dropdown backdrop-blur-sm z-shell-secondary fixed flex flex-col rounded-r-[var(--app-radius-surface)] overflow-hidden"
      initial={target}
      animate={target}
      transition={
        reduceMotion || !isHydrated
          ? { duration: 0 }
          : { duration: NORMAL, ease: EASE }
      }
      style={{
        top: PANEL_INSET,
        // Flush against the rail's right edge — no gap. The rail
        // sits at `left: PANEL_INSET, width: RAIL_WIDTH_COLLAPSED`,
        // so this panel starts at PANEL_INSET + RAIL_WIDTH_COLLAPSED.
        left: PANEL_INSET + RAIL_WIDTH_COLLAPSED,
        bottom: PANEL_INSET,
        width: PANEL_WIDTH,
        pointerEvents: hidden ? "none" : "auto",
      }}
    >
      {/* Header is tab-gated. Memories: hub chip + close chevron.
          Brain: close chevron only. The chevron sits on the right
          per the open↔close affordance pair (active rail tab opens;
          chevron closes). */}
      <div className="flex h-12 items-center justify-between gap-2 px-3 shrink-0">
        <div className="min-w-0 flex-1">
          {showHubChip ? (
            <HubIdentityChip
              kind={isTeamHub ? "team" : "personal"}
              name={activeHub.hub.name}
              icon={activeHub.hub.icon}
              accent={activeHub.hub.accent}
              viewerName={user?.name}
              viewerDisplayName={user?.display_name}
              role={
                isTeamHub &&
                (activeHub.role === "owner" || activeHub.role === "admin")
                  ? activeHub.role
                  : undefined
              }
            />
          ) : null}
        </div>
        <button
          type="button"
          onClick={() => toggleSecondary(tab)}
          aria-label={t.nav.hideSecondaryPanel}
          tabIndex={hidden ? -1 : 0}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-fg-3 cursor-pointer transition-colors hover:bg-foreground/6 hover:text-fg-2"
        >
          <ChevronsLeft className="h-4 w-4" strokeWidth={2} />
        </button>
      </div>
      <div className="flex-1 overflow-y-auto overscroll-contain">
        {children}
      </div>
    </motion.aside>
  );
}
