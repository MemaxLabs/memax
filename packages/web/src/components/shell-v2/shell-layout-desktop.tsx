"use client";

/**
 * `<ShellLayoutDesktop>` — three-zone desktop chrome (LeftRail +
 * PinnedSecondaryPanel + main view + ScanRestButton + page-bg
 * aurora). Extracted from the original `<ShellLayoutV2>` so the
 * top-level shell can mount BOTH desktop + mobile and let CSS pick
 * (plan 22 §4.3).
 *
 *   ┌─────────────────────────────────────────────────────────────┐
 *   │  page background (aurora gradient via --page-aurora-gradient)│
 *   │                                                             │
 *   │  ┌─ LeftRail ─┐                                             │
 *   │  │            │   ┌─ PinnedSecondaryPanel (memories tab) ─┐ │
 *   │  │            │   │                                       │ │
 *   │  │            │   │                                       │ │
 *   │  └────────────┘   └───────────────────────────────────────┘ │
 *   │                                                             │
 *   │     main view content (route children, padded to clear      │
 *   │     the floating chrome on the left)                        │
 *   │                                                             │
 *   │                                            ⊙ ScanRestButton │
 *   └─────────────────────────────────────────────────────────────┘
 *
 * Required ancestor providers (mounted at AppShellClient root):
 *   - `<ShellStateProvider>` — owns rail collapse + secondary
 *     visibility
 *   - `<TopicDndProvider>`   — must wrap the rail + panel + main so
 *     memory→topic drag traverses them all
 *   - `<BarProvider>`        — ScanRestButton calls openBar()
 *   - existing app providers (Auth, TanStack Query, Settings, …)
 */

import { type ReactNode } from "react";
import { motion, useReducedMotion } from "framer-motion";
import { NORMAL, EASE } from "@memaxlabs/ui/tokens/motion";
import { useShellState } from "@/contexts/shell-state-context";
import { LeftRail } from "./left-rail";
import { PinnedSecondaryPanel } from "./pinned-secondary-panel";
import { ScanRestButton } from "./scan-rest-button";
import { TopicTreeMount } from "./topic-tree-mount";
import { BrainSessionsMount } from "./brain-sessions-mount";
import { SHELL_TABS, type ShellTabId } from "./shell-tabs";
import { shellLeftOffsetPx } from "@/lib/shell-geometry";

interface ShellLayoutDesktopProps {
  tab: ShellTabId | null;
  children: ReactNode;
}

export function ShellLayoutDesktop({ tab, children }: ShellLayoutDesktopProps) {
  const { secondaryHidden, isHydrated } = useShellState();
  const reduceMotion = useReducedMotion();
  const tabSpec = tab ? SHELL_TABS.find((t) => t.id === tab) : undefined;
  const tabHasSecondary = tabSpec?.hasSecondary ?? false;
  const secondaryExpanded =
    tab !== null && tabHasSecondary && !secondaryHidden[tab];

  // Main content's left padding animates with the secondary panel
  // visibility ONLY — rail's hover-expand is a visual overlay that
  // doesn't shove content (VS Code / Slack workspace switcher
  // pattern). The padding stays anchored to the collapsed rail
  // footprint regardless of hover state.
  const mainPaddingLeft = shellLeftOffsetPx({
    secondaryExpanded,
  });

  return (
    <div
      className="shell-v2 relative h-dvh w-screen overflow-hidden"
      style={{
        backgroundColor: "var(--background)",
        backgroundImage: "var(--page-aurora-gradient, none)",
      }}
    >
      <LeftRail activeTab={tab} />

      {tab !== null && tabHasSecondary && (
        <PinnedSecondaryPanel tab={tab}>
          {tab === "memories" && <TopicTreeMount />}
          {tab === "brain" && <BrainSessionsMount />}
        </PinnedSecondaryPanel>
      )}

      <motion.main
        // data-scroll-root marks this as the desktop scroll container
        // for `useScrollDirection`. Without this, the hook listens only
        // on `window` (which never scrolls on desktop because of the
        // overflow-y-auto here), so scroll-hide + ScanRestButton silently
        // never fired on desktop. Mobile stays on body scroll and is
        // detected via window.scrollY as before.
        data-scroll-root
        className="absolute inset-y-0 right-0 overflow-y-auto"
        initial={{ paddingLeft: mainPaddingLeft }}
        animate={{ paddingLeft: mainPaddingLeft }}
        transition={
          // Skip the transition until shell state has hydrated so users
          // with a hidden-panel preference don't see the main content
          // slide on every page load.
          reduceMotion || !isHydrated
            ? { duration: 0 }
            : { duration: NORMAL, ease: EASE }
        }
        style={{ left: 0 }}
      >
        {children}
      </motion.main>

      <ScanRestButton />
    </div>
  );
}
