"use client";

/**
 * LeftRail — shell-v2 primary navigation.
 *
 * Visual surface: floating glass rail at top:12 left:12 bottom:12.
 * Width is CONSTANT (RAIL_WIDTH, labels always visible).
 *
 * It used to derive its width from secondary-panel state under an
 * "exactly one panel expanded at a time" budget: opening the knowledge
 * tree collapsed the rail to an icon column, and closing it expanded
 * the rail back. That made primary navigation rearrange itself as a
 * side effect of looking at something else — the reader's anchor moved
 * whenever they used the app. Primary navigation is the one surface
 * that must not move underfoot, so the budget was dropped instead of
 * tuned (2026-08). Only the secondary panel opens and closes now, and
 * the rail is a real layout footprint rather than an overlay.
 *
 * Active-tab click on a `hasSecondary: true` tab toggles the secondary
 * panel for that tab (Notion / Linear convention). The rail no longer
 * moves as part of that transition.
 *
 * A Search action row sits above the tabs: it toggles the global bar
 * via the same code path as Cmd+K (see BarContext.toggleBar). It
 * replaced the bottom-right ScanRestButton FAB (2026-08) so search
 * has one fixed home in the chrome instead of a floating button that
 * appeared and disappeared with scroll state.
 *
 * Brand-mark click navigates to `/brain` (the home/default landing).
 *
 * Accessibility:
 *   - role="navigation", aria-label="Primary"
 *   - Each tab is a button with aria-current="page" when active
 *   - Brand-mark button has aria-label="Memax (home)"
 *   - Keyboard: tab cycles brand → tabs → user; Enter/Space activate
 *   - Labels are always visible inline; tooltips remain as the
 *     accessible name for icon-only affordances.
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Search } from "lucide-react";
import { MemaxLogo, MemaxTextLogo } from "@memaxlabs/ui";
import { useAuth, useActiveHub } from "@/lib/auth";
import { HubIdentityChip } from "@/components/features/hub/hub-identity-chip";
import { useBar } from "@/contexts/bar-context";
import { useShellState } from "@/contexts/shell-state-context";
import { useNotificationSummary } from "@/hooks/use-notifications";
import { useSettingsPanel } from "@/contexts/settings-panel-context";
import { useLocale } from "@/i18n";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { buildMemoriesPath, buildPulsePath } from "@/lib/route-helpers";
import { SHELL_TABS, type ShellTabId } from "./shell-tabs";
import { PANEL_INSET as RAIL_INSET, RAIL_WIDTH } from "@/lib/shell-geometry";

interface LeftRailProps {
  /**
   * The currently active tab — caller derives this from route or context.
   * `null` for routes that don't map to any shell tab; the rail renders
   * no active state in that case.
   */
  activeTab: ShellTabId | null;
}

export function LeftRail({ activeTab }: LeftRailProps) {
  const {
    secondaryHidden,
    toggleSecondary,
    setSecondaryHidden,
    setBarScrollHidden,
  } = useShellState();
  const { toggleBar, barOverlayOpen } = useBar();
  const router = useRouter();
  const pathname = usePathname();
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

  // The rail is ALWAYS expanded at RAIL_WIDTH (2026-08). It used to
  // derive its width from secondary-panel state under an "always
  // exactly one panel expanded" budget, which meant opening the
  // knowledge tree silently collapsed the rail to a column of icons —
  // navigation rearranging itself as a side effect of looking at
  // something. Primary navigation is the one surface that must not
  // move under the reader; only the secondary panel opens and closes
  // now. This also retired the `/home` entry-resolver special case,
  // which existed purely to pre-empt the collapse animation on cold
  // entry, and the width animation itself (a constant can't tween).

  // Platform-appropriate shortcut hint for the Search row. Computed
  // after mount (null during SSR + first paint) so server and client
  // markup match — the hint is a detail, not worth a hydration
  // mismatch.
  const [kbdHint, setKbdHint] = useState<string | null>(null);
  useEffect(() => {
    const ua = navigator.userAgent;
    setKbdHint(/Mac|iPhone|iPad|iPod/.test(ua) ? "⌘K" : "Ctrl K");
  }, []);

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

  // Brand mark click → go home, which is the active hub's memories
  // overview (founder call 2026-08: memories is the home surface;
  // Ask memax is a tab you choose, never a place the brand mark
  // teleports you to). A clickable-looking brand surface must do
  // something meaningful, and "logo goes home" is the universal
  // convention (web logos, IDE app icons, etc.).
  const onBrandClick = useCallback(() => {
    router.push(buildMemoriesPath(hubTabSlug));
  }, [router, hubTabSlug]);

  return (
    <aside
      role="navigation"
      aria-label={t.nav.primary}
      // Glass material matches the secondary panel
      // (`glass-dropdown backdrop-blur-sm`) so both surfaces read
      // as the same chrome family.
      className="glass-dropdown backdrop-blur-sm z-shell-rail fixed flex flex-col rounded-card"
      style={{
        top: RAIL_INSET,
        left: RAIL_INSET,
        bottom: RAIL_INSET,
        width: RAIL_WIDTH,
      }}
    >
      {/* Brand-mark band — clicks navigate to /brain. Hover swaps the
          glyph to the wordmark on expansion. Industry convention:
          brand glyph in nav rails = "go home". */}
      <button
        type="button"
        onClick={onBrandClick}
        aria-label={`memax (${t.nav.tabs.brain})`}
        className="group flex h-12 items-center gap-2 cursor-pointer px-3"
      >
        <span className="relative flex h-9 w-9 items-center justify-center rounded-card text-foreground shrink-0 transition-colors group-hover:bg-surface-2">
          <MemaxLogo size={24} />
        </span>
        <MemaxTextLogo height={18} className="text-foreground shrink-0" />
      </button>

      {/* Hub anchor — the global hub identity + switcher, present on
          every surface (Slack/Linear pattern: workspace identity lives
          in the nav rail, not inside one tab's panel). Gated on 2+
          hubs like every other switcher trigger; single-hub users have
          nothing to switch. */}
      {hubs.length >= 2 && activeHub && (
        <div className="mt-1 flex shrink-0 px-3">
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

      {/* Search — an action row, not a navigation tab: it toggles the
          global bar (the exact Cmd+K semantic, same code path) instead
          of routing anywhere. Lives above the tabs per the sidebar
          convention (Slack / Linear / Notion put search at the top of
          the nav stack) and replaces the old bottom-right FAB, which
          was a second floating chrome language competing with the
          rail. Never gets aria-current — aria-expanded reflects the
          overlay it controls. */}
      <div className="flex flex-col px-2 mt-2 shrink-0">
        <button
          type="button"
          onClick={() => {
            // Clear the sticky scroll-hide flag so a bar hidden by
            // scrolling reveals on the same click that summons it —
            // matches the old FAB's tap path.
            setBarScrollHidden(false);
            toggleBar();
          }}
          aria-label={t.nav.openBar}
          aria-expanded={barOverlayOpen}
          className="relative flex h-9 items-center gap-2.5 rounded-lg px-2 text-left transition-[background-color] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)] cursor-pointer hover:bg-surface-2"
          style={{
            background: barOverlayOpen ? "var(--surface-3)" : undefined,
            color: barOverlayOpen ? "var(--foreground)" : "var(--fg-2)",
          }}
        >
          <span className="relative flex h-6 w-6 shrink-0 items-center justify-center">
            <Search
              className="h-5 w-5"
              strokeWidth={barOverlayOpen ? 2.2 : 1.8}
            />
          </span>
          <span className="text-[13px] font-medium truncate flex-1">
            {t.nav.search}
          </span>
          {kbdHint && (
            <kbd
              aria-hidden
              className="shrink-0 rounded-md border border-border px-1.5 py-0.5 text-[10px] font-medium tracking-wide text-fg-3"
              style={{ fontFamily: "inherit" }}
            >
              {kbdHint}
            </kbd>
          )}
        </button>
      </div>

      {/* Tabs */}
      <nav className="flex flex-col gap-0.5 px-2 mt-2 flex-1">
        {SHELL_TABS.map((tab) => {
          const Icon = tab.icon;
          const isActive = tab.id === displayedActiveTab;
          const isSecondaryHidden = secondaryHidden[tab.id];
          const label = t.nav.tabs[tab.id];
          const badgeTone = tab.id === "pulse" ? pulseBadgeTone : null;
          return (
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
              className="relative flex h-9 items-center gap-2.5 rounded-lg px-2 text-left transition-[background-color] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)] cursor-pointer hover:bg-surface-2"
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
              <span className="text-[13px] font-medium truncate">{label}</span>
            </button>
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
          className="flex h-9 w-full items-center gap-2.5 rounded-lg px-2 text-left transition-[background-color] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)] cursor-pointer hover:bg-surface-2"
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
          <span className="text-[13px] font-medium truncate">
            {user?.name ?? t.nav.openSettings}
          </span>
        </button>
      </div>
    </aside>
  );
}
