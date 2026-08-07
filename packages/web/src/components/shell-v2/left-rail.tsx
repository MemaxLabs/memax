"use client";

/**
 * LeftRail — shell-v2 primary navigation.
 *
 * Visual surface: floating glass rail at top:12 left:12 bottom:12.
 * Width is **derived from secondary-panel state**, not hover:
 *
 *   - Active tab has an OPEN secondary panel → rail COLLAPSED (56px,
 *     icons only). The secondary panel owns the visual budget.
 *   - Active tab has a HIDDEN secondary panel → rail EXPANDED
 *     (196px, labels visible). The rail owns the visual budget.
 *   - Active tab has NO secondary panel (agents, pulse) → rail
 *     EXPANDED. Nothing competes for space.
 *
 * Principle: exactly one panel is expanded at a time. This replaces
 * the earlier hover-to-expand model (removed 2026-05-18) which felt
 * jittery and made the rail's state mouse-position-dependent. Now
 * state transitions are user-driven (tab click / secondary toggle)
 * and predictable.
 *
 * Active-tab click on a `hasSecondary: true` tab toggles the secondary
 * panel for that tab (Notion / Linear convention). The rail then
 * inverts width as a deliberate part of the same transition — clicking
 * "Memories" while on /memories collapses the rail and shows the
 * topic panel; clicking again expands the rail back with labels.
 *
 * Brand-mark click navigates to `/brain` (the home/default landing).
 *
 * Accessibility:
 *   - role="navigation", aria-label="Primary"
 *   - Each tab is a button with aria-current="page" when active
 *   - Brand-mark button has aria-label="Memax (home)"
 *   - Keyboard: tab cycles brand → tabs → user; Enter/Space activate
 *   - Tooltips render labels when the rail is collapsed; when
 *     expanded, labels are visible inline so tooltips redundant.
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { motion, useReducedMotion } from "framer-motion";
import {
  MemaxLogo,
  MemaxTextLogo,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@memaxlabs/ui";
import { NORMAL, EASE } from "@memaxlabs/ui/tokens/motion";
import { useAuth, useActiveHub } from "@/lib/auth";
import { HubIdentityChip } from "@/components/features/hub/hub-identity-chip";
import { useShellState } from "@/contexts/shell-state-context";
import { useNotificationSummary } from "@/hooks/use-notifications";
import { useSettingsPanel } from "@/contexts/settings-panel-context";
import { useLocale } from "@/i18n";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { buildMemoriesPath, buildPulsePath } from "@/lib/route-helpers";
import { SHELL_TABS, type ShellTabId } from "./shell-tabs";
import {
  PANEL_INSET as RAIL_INSET,
  RAIL_WIDTH_COLLAPSED,
  RAIL_WIDTH_EXPANDED,
} from "@/lib/shell-geometry";

interface LeftRailProps {
  /**
   * The currently active tab — caller derives this from route or context.
   * `null` for routes that don't map to any shell tab; the rail renders
   * no active state in that case.
   */
  activeTab: ShellTabId | null;
}

export function LeftRail({ activeTab }: LeftRailProps) {
  const { secondaryHidden, toggleSecondary, setSecondaryHidden, isHydrated } =
    useShellState();
  const router = useRouter();
  const pathname = usePathname();
  const reduceMotion = useReducedMotion();
  const { t } = useLocale();
  const { data: notificationSummary } = useNotificationSummary();
  const { user, hubs } = useAuth();
  const { activeHub } = useActiveHub();
  const settingsPanel = useSettingsPanel();
  const pulseBadgeTone =
    (notificationSummary?.needs_action_pending ?? 0) > 0
      ? ("needs-action" as const)
      : (notificationSummary?.updates_unseen ?? 0) > 0
        ? ("updates" as const)
        : null;

  // Rail expansion is now derived from secondary-panel state, NOT
  // hover. The rail collapses ONLY when the active tab has a
  // secondary panel and that panel is currently open — the panel
  // owns the visual budget in that case. Otherwise the rail is
  // expanded so labels are visible. See the file-level docstring
  // for the full "always one panel expanded" principle.
  const activeTabSpec = activeTab
    ? SHELL_TABS.find((tab) => tab.id === activeTab)
    : null;
  const secondaryOpenOnActive = Boolean(
    activeTabSpec?.hasSecondary &&
    activeTab !== null &&
    !secondaryHidden[activeTab],
  );
  // `/home` is the neutral entry resolver (see app/(app)/home/page.tsx):
  // it exists for a few frames while the landing surface resolves, and
  // every destination it routes to opens a secondary panel — i.e. the
  // rail lands collapsed. Starting expanded there would play a
  // full-width rail that immediately collapses on arrival: a layout
  // shift on every cold entry with no information payoff.
  const isEntryResolver = pathname === "/home";
  const expanded = !secondaryOpenOnActive && !isEntryResolver;
  const visualWidth = expanded ? RAIL_WIDTH_EXPANDED : RAIL_WIDTH_COLLAPSED;

  // Resolve the hub-scoped tab paths against the user's active hub.
  // Static-path tabs (brain, agents) ignore active-hub context; the
  // memories and pulse tabs both live under `/h/<slug>/`, so a team-hub
  // user clicking either lands in their team hub, not personal.
  const hubTabSlug = useMemo(() => {
    if (!activeHub?.hub || !user) return "personal";
    return hubRouteSlug(activeHub.hub, user.id);
  }, [activeHub, user]);
  const resolveTabPath = useCallback(
    (id: ShellTabId, staticPath: string | null): string | null => {
      if (staticPath) return staticPath;
      if (id === "memories") return buildMemoriesPath(hubTabSlug);
      if (id === "pulse") return buildPulsePath(hubTabSlug);
      return null;
    },
    [hubTabSlug],
  );

  // Prefetch tab routes so a click feels instant. Without this each
  // tab change pays the route's RSC payload + JS chunk download
  // cost on first visit, which surfaces as a 100-400ms delay
  // between click and active-state flip (the tab's active state is
  // derived from pathname, which only updates after the route
  // resolves). Industry pattern: Linear / Vercel / Notion all
  // prefetch shell-nav destinations on mount.
  useEffect(() => {
    for (const tab of SHELL_TABS) {
      const path = resolveTabPath(tab.id, tab.staticPath);
      if (path) router.prefetch(path);
    }
  }, [router, resolveTabPath]);

  // Optimistic active-tab state. The pathname-derived `activeTab`
  // prop only flips after the new route resolves; on cache miss
  // that's perceived as "I clicked but nothing happened" for the
  // length of the navigation. Tracking a local pending state lets
  // the rail flip the active styling on the click itself, then
  // reconcile when the route lands. Clears whenever the actual
  // activeTab catches up.
  const [pendingTab, setPendingTab] = useState<ShellTabId | null>(null);
  useEffect(() => {
    if (pendingTab && pendingTab === activeTab) {
      setPendingTab(null);
    }
  }, [activeTab, pendingTab]);
  const displayedActiveTab = pendingTab ?? activeTab;

  const onTabClick = useCallback(
    (id: ShellTabId, hasSecondary: boolean, staticPath: string | null) => {
      if (id === activeTab) {
        if (hasSecondary) toggleSecondary(id);
        return;
      }
      if (hasSecondary) setSecondaryHidden(id, false);
      const path = resolveTabPath(id, staticPath);
      if (path) {
        setPendingTab(id);
        router.push(path);
      }
    },
    [activeTab, router, toggleSecondary, setSecondaryHidden, resolveTabPath],
  );

  // Brand mark click → navigate to /brain (home). Per codex review:
  // a clickable-looking brand surface must do something meaningful.
  // Toggling pin was removed (no-pin model) so we re-purpose the
  // gesture to "go home", which is a universal brand-mark
  // convention (web logos, IDE app icons, etc.).
  const onBrandClick = useCallback(() => {
    router.push("/brain");
  }, [router]);

  return (
    <motion.aside
      role="navigation"
      aria-label={t.nav.primary}
      // Glass material matches the secondary panel
      // (`glass-dropdown backdrop-blur-sm`) so both surfaces read
      // as the same chrome family.
      className="glass-dropdown backdrop-blur-sm z-shell-rail fixed flex flex-col rounded-card"
      initial={{ width: visualWidth }}
      animate={{ width: visualWidth }}
      transition={
        // Skip the width transition until persisted state has
        // hydrated; also skip on reduced-motion. Width animates only
        // on secondary-panel toggle / tab navigation — both
        // user-driven, never mouse-position-driven.
        reduceMotion || !isHydrated
          ? { duration: 0 }
          : { duration: NORMAL, ease: EASE }
      }
      style={{
        top: RAIL_INSET,
        left: RAIL_INSET,
        bottom: RAIL_INSET,
      }}
    >
      {/* Brand-mark band — clicks navigate to /brain. Hover swaps the
          glyph to the wordmark on expansion. Industry convention:
          brand glyph in nav rails = "go home". */}
      <button
        type="button"
        onClick={onBrandClick}
        aria-label={`memax (${t.nav.tabs.brain})`}
        className={`group flex h-12 items-center gap-2 cursor-pointer transition-[padding] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)] ${
          expanded ? "px-3" : "justify-center px-0"
        }`}
      >
        <span className="relative flex h-9 w-9 items-center justify-center rounded-card text-foreground shrink-0 transition-colors group-hover:bg-surface-2">
          <MemaxLogo size={24} />
        </span>
        {expanded && (
          <MemaxTextLogo height={18} className="text-foreground shrink-0" />
        )}
      </button>

      {/* Hub anchor — the global hub identity + switcher, present on
          every surface (Slack/Linear pattern: workspace identity lives
          in the nav rail, not inside one tab's panel). Gated on 2+
          hubs like every other switcher trigger; single-hub users have
          nothing to switch. */}
      {hubs.length >= 2 && activeHub && (
        <div
          className={`mt-1 flex shrink-0 ${expanded ? "px-3" : "justify-center px-0"}`}
        >
          <HubIdentityChip
            variant="rail"
            kind={activeHub.hub.hub_type === "team" ? "team" : "personal"}
            name={activeHub.hub.name}
            icon={activeHub.hub.icon}
            accent={activeHub.hub.accent}
            viewerName={user?.name}
            viewerDisplayName={user?.display_name}
          />
        </div>
      )}

      {/* Tabs */}
      <nav className="flex flex-col gap-0.5 px-2 mt-2 flex-1">
        {SHELL_TABS.map((tab) => {
          const Icon = tab.icon;
          const isActive = tab.id === displayedActiveTab;
          const isSecondaryHidden = secondaryHidden[tab.id];
          const label = t.nav.tabs[tab.id];
          const badgeTone = tab.id === "pulse" ? pulseBadgeTone : null;
          const button = (
            <button
              key={tab.id}
              type="button"
              onClick={() =>
                onTabClick(tab.id, tab.hasSecondary, tab.staticPath)
              }
              aria-current={isActive ? "page" : undefined}
              aria-label={
                isActive && tab.hasSecondary
                  ? isSecondaryHidden
                    ? t.nav.showSecondaryPanel
                    : t.nav.hideSecondaryPanel
                  : label
              }
              className={`relative flex h-9 items-center gap-2.5 rounded-lg text-left transition-[padding,background-color] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)] cursor-pointer hover:bg-surface-2 ${
                expanded ? "px-2" : "justify-center px-0"
              }`}
              style={{
                background: isActive ? "var(--surface-3)" : undefined,
                color: isActive ? "var(--foreground)" : "var(--fg-2)",
              }}
            >
              <span className="relative flex h-6 w-6 shrink-0 items-center justify-center">
                <Icon className="h-5 w-5" strokeWidth={isActive ? 2.2 : 1.8} />
                {badgeTone && (
                  <span
                    className="absolute -right-0.5 -top-0.5 h-[7px] w-[7px] rounded-full"
                    style={{
                      background:
                        badgeTone === "needs-action"
                          ? "var(--signature)"
                          : "oklch(from var(--foreground) l c h / 0.42)",
                      boxShadow: "0 0 0 1.5px var(--card)",
                    }}
                    aria-hidden
                  />
                )}
              </span>
              {expanded && (
                <span className="text-[13px] font-medium truncate">
                  {label}
                </span>
              )}
            </button>
          );
          // Tooltip surfaces the label only in the collapsed (resting)
          // visual state. When hover-expanded, the label is already
          // inline. On touch devices (no hover), the tooltip is the
          // only label-surfacing path.
          return !expanded ? (
            <Tooltip key={tab.id}>
              <TooltipTrigger render={button} />
              <TooltipContent side="right" sideOffset={12}>
                {label}
              </TooltipContent>
            </Tooltip>
          ) : (
            button
          );
        })}
      </nav>

      {/* Footer — user avatar opens the SettingsPanel. */}
      <div className="px-2 pb-2 shrink-0">
        <button
          type="button"
          onClick={settingsPanel.toggle}
          aria-label={t.nav.openSettings}
          aria-expanded={settingsPanel.open}
          className={`flex h-9 w-full items-center gap-2.5 rounded-lg text-left transition-[padding,background-color] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)] cursor-pointer hover:bg-surface-2 ${
            expanded ? "px-2" : "justify-center px-0"
          }`}
          style={{ color: "var(--fg-2)" }}
        >
          <span className="flex h-7 w-7 shrink-0 items-center justify-center overflow-hidden rounded-full bg-surface-2">
            {user?.avatar_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={user.avatar_url}
                alt=""
                className="h-7 w-7 rounded-full"
              />
            ) : (
              <span className="text-[12px] font-medium text-fg-2">
                {user?.name?.[0]?.toUpperCase() ?? ""}
              </span>
            )}
          </span>
          {expanded && (
            <span className="text-[13px] font-medium truncate">
              {user?.name ?? t.nav.openSettings}
            </span>
          )}
        </button>
      </div>
    </motion.aside>
  );
}
