"use client";

/**
 * `<ShellLayoutMobile>` — plan 22 phase 1 mobile chrome.
 *
 * Composition:
 *   - `<MobileTopBar>` sticks to the top, hosts the hamburger,
 *     hub-identity row, and the 5 bar portal slots (§5.4).
 *   - `<MobileDrawer>` slides in from the left when the hamburger is
 *     tapped. Renders the full nav surface — brand mark, tab list,
 *     user-avatar SettingsPanel trigger.
 *   - `<main>` fills the viewport between the top bar and the bottom
 *     bar surface; scrolls with `overscroll-contain` so the bar
 *     surface doesn't bounce-overlap.
 *   - `<ScanRestButton>` floats bottom-right (legacy mobile placement).
 *   - `<MobileBarSurface>` and `<MobileComposeOverlay>` are mounted
 *     by `<AppShellClient>` already (above the layout dispatch) — they
 *     position via `fixed` and don't need a slot here.
 *
 * The bar portal slots inside `<MobileTopBar>` mean the GLOBAL bar
 * (provider mounted higher up) renders into the top bar when the bar
 * is in the active phase. When the bar is at rest, the slots stay
 * empty placeholder divs and the bar's own dock surface paints at
 * the bottom via MobileBarSurface.
 */

import { useState, type ReactNode } from "react";
import { useRouter, usePathname } from "next/navigation";
import { useAuth, useActiveHub } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { useSettingsPanel } from "@/contexts/settings-panel-context";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { MemaxLogo, MemaxTextLogo } from "@memaxlabs/ui";
import { MobileTopBar } from "./mobile-top-bar";
import { MobileDrawer } from "./mobile-drawer";
import { HubSwitcherBottomSheet } from "./hub-switcher-bottom-sheet";
import { PushTransitionWrapper } from "./push-transition-wrapper";
import { ScanRestButton } from "./scan-rest-button";
import { SHELL_TABS, type ShellTabId } from "./shell-tabs";

interface ShellLayoutMobileProps {
  tab: ShellTabId | null;
  children: ReactNode;
}

export function ShellLayoutMobile({ tab, children }: ShellLayoutMobileProps) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [hubSwitcherOpen, setHubSwitcherOpen] = useState(false);
  return (
    <div
      // `overflow-x: clip` (NOT hidden) — `hidden` on one axis forces
      // the other axis to compute as `auto` (CSS Overflow Module L3
      // §3.3), turning this outer into a scroll container and
      // breaking body scroll on iOS Safari. `clip` confines paint
      // without making this a scroll container, so body scroll
      // (the contract every mobile surface relies on — see <main>
      // comment below — useScrollDirection / useKeyboardOpen /
      // memories scroll restoration all read window.scrollY) keeps
      // working. Plan 26 follow-up: "mobile memory page is not
      // scrollable" root cause.
      className="shell-v2 shell-v2-mobile relative min-h-dvh w-screen overflow-x-clip"
      style={{
        backgroundColor: "var(--background)",
        backgroundImage: "var(--page-aurora-gradient, none)",
      }}
    >
      <MobileTopBar
        tab={tab}
        onMenuClick={() => setDrawerOpen(true)}
        onHubSwitchClick={() => setHubSwitcherOpen(true)}
      />
      {/* Body-scroll is preserved here: every existing surface
          (memories/layout scroll restoration, useScrollDirection bar
          hide, useKeyboardOpen position save) reads from window.scrollY,
          and pivoting to inner-main scroll on mobile would silently
          break all of them. PushTransitionWrapper is now a pass-through
          (plan 26 follow-up — user spec "tab switch should be instant,
          no animation"); routes that need a loading state should ship
          loading.tsx segment files. */}
      <main
        className="relative pt-14 pb-32"
        style={{ paddingTop: "calc(3.5rem + env(safe-area-inset-top))" }}
      >
        <PushTransitionWrapper>{children}</PushTransitionWrapper>
      </main>
      <MobileDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)}>
        <DrawerContent tab={tab} onNavigate={() => setDrawerOpen(false)} />
      </MobileDrawer>
      <HubSwitcherBottomSheet
        open={hubSwitcherOpen}
        onClose={() => setHubSwitcherOpen(false)}
      />
      <ScanRestButton />
    </div>
  );
}

interface DrawerContentProps {
  tab: ShellTabId | null;
  /** Called after a tab is tapped so the parent can close the drawer. */
  onNavigate: () => void;
}

function DrawerContent({ tab: activeTab, onNavigate }: DrawerContentProps) {
  const router = useRouter();
  const pathname = usePathname();
  const { user } = useAuth();
  const { activeHub } = useActiveHub();
  const { t } = useLocale();
  const settingsPanel = useSettingsPanel();

  // Same active-hub-aware path logic as the desktop LeftRail / mobile
  // dock. Single source of truth would mean lifting this; for now
  // the snippet is small enough that copying is preferable to a
  // shared hook with no other consumers.
  const memoriesPath = (() => {
    if (!activeHub?.hub || !user) return "/h/personal/memories";
    const slug = hubRouteSlug(activeHub.hub, user.id);
    return `/h/${slug}/memories`;
  })();

  const tabLabel: Record<ShellTabId, string> = {
    brain: t.nav.tabs.brain,
    memories: t.nav.tabs.memories,
    pulse: t.nav.tabs.pulse,
    agents: t.nav.tabs.agents,
  };

  const handleTabTap = (id: ShellTabId) => {
    const spec = SHELL_TABS.find((s) => s.id === id);
    if (!spec) return;
    const target = spec.staticPath ?? (id === "memories" ? memoriesPath : null);
    if (!target) return;
    if (pathname !== target) router.push(target);
    onNavigate();
  };

  return (
    <div className="flex h-full flex-col">
      {/* Brand mark — visual anchor at the top of the drawer. Aligned
          to the same left rhythm as the nav rows below: the brand's
          icon column matches the tab icon column (each tab's icon
          sits at nav-px-2 + tab-px-3 + 0 = 20px from the drawer's
          left edge; the brand's logo also sits at 20px). Tab icons
          are bumped to h-5 w-5 so the visual weight pairs with the
          24px MemaxLogo above them — Notion 2026 / Linear pattern
          where brand and nav share one indent grid. Plan 26
          follow-up: "how would 2026 pair the mobile logo icon and
          the tabs". */}
      <div className="flex h-12 items-center gap-3 px-5 pt-2 shrink-0">
        <span className="flex h-6 w-6 shrink-0 items-center justify-center text-foreground">
          <MemaxLogo size={24} />
        </span>
        <MemaxTextLogo height={18} className="text-foreground" />
      </div>

      <nav
        aria-label={t.nav.primary}
        className="flex-1 px-2 pt-3 flex flex-col gap-0.5"
      >
        {SHELL_TABS.map((spec) => {
          const Icon = spec.icon;
          const isActive = spec.id === activeTab;
          return (
            <button
              key={spec.id}
              type="button"
              onClick={() => handleTabTap(spec.id)}
              aria-current={isActive ? "page" : undefined}
              className={`flex items-center gap-3 rounded-lg px-3 py-2.5 text-left text-[15px] transition-colors cursor-pointer ${
                isActive
                  ? "bg-foreground/8 text-foreground font-medium"
                  : "text-fg-2 hover:bg-foreground/4"
              }`}
            >
              <span className="flex h-6 w-6 shrink-0 items-center justify-center">
                <Icon className="h-5 w-5" strokeWidth={isActive ? 2.5 : 2} />
              </span>
              <span>{tabLabel[spec.id]}</span>
            </button>
          );
        })}
      </nav>

      <div className="px-2 pb-2 shrink-0">
        <button
          type="button"
          onClick={() => {
            // Toggle the settings panel WITHOUT closing the drawer —
            // the settings panel is overlay chrome, not a navigation
            // destination, so dismissing the drawer underneath it
            // makes the user "lose" their nav context. The drawer
            // stays open so closing the settings panel returns the
            // user to where they were. Plan 26 follow-up.
            settingsPanel.toggle();
          }}
          aria-label={t.nav.openSettings}
          aria-expanded={settingsPanel.open}
          className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 text-left transition-colors cursor-pointer hover:bg-foreground/6"
        >
          <span className="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded-full bg-surface-2">
            {user?.avatar_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={user.avatar_url}
                alt=""
                className="h-8 w-8 rounded-full"
              />
            ) : (
              <span className="text-[13px] font-medium text-fg-2">
                {user?.name?.[0]?.toUpperCase() ?? ""}
              </span>
            )}
          </span>
          <span className="text-[14px] font-medium text-fg-2 truncate">
            {user?.display_name || user?.name || t.nav.openSettings}
          </span>
        </button>
      </div>
    </div>
  );
}
