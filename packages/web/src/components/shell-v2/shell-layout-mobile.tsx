"use client";

/**
 * Mobile shell (plan 22 v2 chrome, 2026-09 M1 revision).
 *
 *   top    — <MobileTopBar>: hub pill · bell · avatar
 *   body   — the route, body-scrolled (see note on <main>)
 *   bottom — <MobileDock>: the four shell tabs, isomorphic with the
 *            desktop rail; the floating ✦ bar rests above it
 *
 * Sheets hosted here: hub switcher and the notification drawer. The
 * SettingsPanel (avatar) and 入门与机制 are hosted by <AppShell>.
 */

import { useState, type ReactNode } from "react";
import { MobileNotificationSheet } from "@/components/features/notification-drawer";
import { MobileDock } from "@/components/features/mobile-dock";
import { MobileTopBar } from "./mobile-top-bar";
import { HubSwitcherBottomSheet } from "./hub-switcher-bottom-sheet";
import { PushTransitionWrapper } from "./push-transition-wrapper";

interface ShellLayoutMobileProps {
  children: ReactNode;
}

export function ShellLayoutMobile({ children }: ShellLayoutMobileProps) {
  const [hubSwitcherOpen, setHubSwitcherOpen] = useState(false);
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  return (
    <div
      className="shell-v2 shell-v2-mobile relative min-h-dvh w-screen overflow-x-clip"
      style={{
        backgroundColor: "var(--background)",
        backgroundImage: "var(--page-aurora-gradient, none)",
      }}
    >
      <MobileTopBar
        onHubSwitchClick={() => setHubSwitcherOpen(true)}
        onNotificationsClick={() => setNotificationsOpen(true)}
      />
      {/* Body-scroll is preserved here: every existing surface
          (memories/layout scroll restoration, useScrollDirection bar
          hide, useKeyboardOpen position save) reads from window.scrollY,
          and pivoting to inner-main scroll on mobile would silently
          break all of them. PushTransitionWrapper is now a pass-through
          (plan 26 follow-up — user spec "tab switch should be instant,
          no animation"); routes that need a loading state should ship
          loading.tsx segment files.
          Bottom padding clears the 56px dock plus breathing room; the
          ✦ bar is hidden at rest on mobile (summoned from the top-bar
          search button), so its band is not reserved. */}
      <main
        className="relative"
        style={{
          paddingTop: "calc(3.5rem + env(safe-area-inset-top))",
          paddingBottom: "calc(5rem + env(safe-area-inset-bottom))",
        }}
      >
        <PushTransitionWrapper>{children}</PushTransitionWrapper>
      </main>
      <MobileDock />
      <MobileNotificationSheet
        open={notificationsOpen}
        onClose={() => setNotificationsOpen(false)}
      />
      <HubSwitcherBottomSheet
        open={hubSwitcherOpen}
        onClose={() => setHubSwitcherOpen(false)}
      />
    </div>
  );
}
