"use client";

/**
 * Mobile top bar (plan 22 v2 chrome, 2026-09 M1 revision).
 *
 * Three fixed elements, left → right:
 *   1. Hub pill — the ONE hub entry on mobile (hub context is not a
 *      memories-only concern: brain chat scope, pulse, agents all read
 *      the active hub). 2+ hubs → opens the switcher sheet; one hub →
 *      inert identity anchor, no chevron.
 *   2. Bell — notification drawer (mobile form: bottom sheet). Badge
 *      = drawer unseen count, same classifier as the desktop bell.
 *   3. Avatar — opens the SettingsPanel (account domain; 入门与机制
 *      lives inside it on mobile).
 *
 * Navigation itself lives in the bottom <MobileDock>; the hamburger
 * drawer is retired. Inside a chat session the bar collapses to a back
 * arrow only — the session owns the screen.
 */

import { ArrowLeft, Bell, ChevronDown, Search } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useAuth, useActiveHub } from "@/lib/auth";
import { useBar } from "@/contexts/bar-context";
import { useShellState } from "@/contexts/shell-state-context";
import { HubBadge } from "@/components/features/hub/hub-badge";
import { useDrawerData } from "@/components/features/notification-drawer";
import { useLocale } from "@/i18n";
import { useScrollDirection } from "@/hooks/use-scroll-direction";
import { useSettingsPanel } from "@/contexts/settings-panel-context";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";
import { isChatSessionRoute } from "@/lib/route-helpers";

interface MobileTopBarProps {
  onHubSwitchClick?: () => void;
  onNotificationsClick: () => void;
}

const iconButtonClass =
  "relative flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-fg-2 cursor-pointer transition-colors hover:bg-foreground/6";

export function MobileTopBar({
  onHubSwitchClick,
  onNotificationsClick,
}: MobileTopBarProps) {
  const { user, hubs } = useAuth();
  const { activeHub, isTeamHub } = useActiveHub();
  const { t } = useLocale();
  const { atTop } = useScrollDirection();
  const pathname = usePathname();
  const router = useRouter();
  const settingsPanel = useSettingsPanel();
  const { unseen } = useDrawerData();
  const { openBar } = useBar();
  const { setBarScrollHidden } = useShellState();
  const inChatSession = pathname ? isChatSessionRoute(pathname) : false;
  const viewerIdentity = user
    ? { name: user.name, displayName: user.display_name }
    : undefined;
  const hubName = activeHub
    ? getHubDisplayName(activeHub.hub, t, viewerIdentity)
    : "";
  const hubInitial = activeHub
    ? getHubDisplayInitial(activeHub.hub, t, viewerIdentity)
    : "";
  const canSwitchHub = hubs.length >= 2 && !!onHubSwitchClick;

  return (
    <header
      className={`fixed top-0 inset-x-0 z-bar flex items-center gap-1 px-2 transition-[background-color,border-color] duration-200 ${
        atTop ? "border-b border-transparent" : "border-b border-border/40"
      }`}
      style={{
        // border-box: the 56px content band must sit BELOW the notch
        // inset, so the height carries the inset too (matches <main>'s
        // top padding in ShellLayoutMobile).
        height: "calc(3.5rem + env(safe-area-inset-top))",
        paddingTop: "env(safe-area-inset-top)",
        background: atTop
          ? "transparent"
          : "color-mix(in srgb, var(--background) 85%, transparent)",
        backdropFilter: "blur(12px)",
        WebkitBackdropFilter: "blur(12px)",
      }}
    >
      {inChatSession ? (
        <button
          type="button"
          onClick={() => router.push("/brain")}
          aria-label={t.chat.sidebar.heading}
          className={iconButtonClass}
        >
          <ArrowLeft className="h-5 w-5" strokeWidth={2} />
        </button>
      ) : (
        <>
          {activeHub ? (
            <button
              type="button"
              onClick={canSwitchHub ? onHubSwitchClick : undefined}
              disabled={!canSwitchHub}
              aria-label={
                canSwitchHub
                  ? t.composeModal.targetHubAria.replace("{hub}", hubName)
                  : hubName
              }
              className={`glass-subtle ml-1 flex min-h-11 min-w-0 items-center gap-1.5 rounded-full py-1.5 pl-1.5 pr-2.5 ${
                canSwitchHub ? "cursor-pointer" : "cursor-default"
              }`}
            >
              <HubBadge
                kind={isTeamHub ? "team" : "personal"}
                label={hubInitial}
                accent={activeHub.hub.accent}
                size="xl"
              />
              <span className="max-w-32 truncate text-[13px] font-medium text-fg-1">
                {hubName}
              </span>
              {canSwitchHub ? (
                <ChevronDown
                  className="h-4 w-4 shrink-0 text-fg-3"
                  strokeWidth={2}
                />
              ) : null}
            </button>
          ) : null}

          <div className="ml-auto flex items-center">
            {/* 快速搜索 — the ✦ bar is hidden at rest on mobile, so this
                is the always-available way to summon it (recall +
                capture). openBar() promotes the mobile compose state to
                "mirror" so the input is immediately editable. */}
            <button
              type="button"
              onClick={() => {
                setBarScrollHidden(false);
                openBar();
              }}
              aria-label={t.nav.openBar}
              className={iconButtonClass}
            >
              <Search className="h-5 w-5" strokeWidth={2} />
            </button>
            <button
              type="button"
              onClick={onNotificationsClick}
              aria-label={t.notificationDrawer.openAria}
              aria-haspopup="dialog"
              className={iconButtonClass}
            >
              <Bell className="h-5 w-5" strokeWidth={2} />
              {unseen > 0 ? (
                <span
                  className="absolute right-1.5 top-1.5 flex h-[15px] min-w-[15px] items-center justify-center rounded-full px-[3px] text-[9.5px] font-semibold leading-none text-white"
                  style={{ background: "var(--signature)" }}
                >
                  {unseen > 9 ? "9+" : unseen}
                </span>
              ) : null}
            </button>
            <button
              type="button"
              onClick={settingsPanel.toggle}
              aria-label={t.nav.openSettings}
              aria-expanded={settingsPanel.open}
              className={iconButtonClass}
            >
              <span className="flex h-8 w-8 items-center justify-center overflow-hidden rounded-full bg-surface-2">
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
            </button>
          </div>
        </>
      )}
    </header>
  );
}
