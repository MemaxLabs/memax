"use client";

/**
 * `<MobileTopBar>` — top-of-viewport chrome for mobile shell-v2 (plan
 * 22 P0-1).
 *
 * Two responsibilities:
 *   1. Hamburger that opens the `<MobileDrawer>` (caller-owned via
 *      `onMenuClick`).
 *   2. Tab-aware identity row in the middle:
 *      - Memories tab: `HubBadge + hub name + chevron` (chevron taps
 *        the hub-switcher BottomSheet — wired by Phase 3; for now the
 *        chevron is decorative).
 *      - Other tabs: tab label only.
 *
 * Glass material: `--background`-based translucent fill + 12px blur,
 * applied via inline style so the backdrop-filter actually samples
 * page content (when applied via class, some browser/CSS pipelines
 * silently dropped the filter and the background read as a flat dark
 * strip). This is the SAME recipe shape as the rail/panel — fill +
 * blur — but tuned to a shallow chrome strip (12px blur, lighter
 * --background fill) instead of the deep rail/panel material (36px
 * blur, --card fill).
 *
 * Transparent at top: when the page is at scroll origin, the glass
 * fades out so the top row blends seamlessly into the page background
 * (no visible chrome strip). As soon as content scrolls under the
 * bar, the glass fades back in to maintain legibility. Drives the
 * "no background strip when nothing's beneath" user spec.
 *
 * Bar portal slots: NOT mounted here. `<GlobalBar>` already mounts
 * the 5 slot IDs (`#bar-{logo,input,right,chip,expand}-slot`) inside
 * the bar's own fixed-position pill — desktop branch mounts all five
 * inline; mobile branch routes through `<MobileThumbBarRow>` which
 * mounts `#bar-logo-slot` itself. Mounting them again here would
 * produce duplicate IDs and break `document.getElementById` lookups
 * the portal helpers use. Plan 22 §5.4's "slots live in MobileTopBar"
 * direction is a future restructure that requires moving ownership
 * out of GlobalBar in the same commit.
 */

import { Menu, ChevronDown, ArrowLeft } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useAuth, useActiveHub } from "@/lib/auth";
import { HubBadge } from "@/components/features/hub/hub-badge";
import { useLocale } from "@/i18n";
import { useScrollDirection } from "@/hooks/use-scroll-direction";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";
import { isChatSessionRoute } from "@/lib/route-helpers";
import type { ShellTabId } from "./shell-tabs";

interface MobileTopBarProps {
  tab: ShellTabId | null;
  onMenuClick: () => void;
  /**
   * Click handler for the hub-identity chevron. Defaults to a no-op
   * (chevron renders decorative). Phase 3 wires it to open a hub-
   * switcher `<BottomSheet>` per plan 22 §5.2.
   */
  onHubSwitchClick?: () => void;
}

export function MobileTopBar({
  tab,
  onMenuClick,
  onHubSwitchClick,
}: MobileTopBarProps) {
  const { user, hubs } = useAuth();
  const { activeHub, isTeamHub } = useActiveHub();
  const { t } = useLocale();
  const { atTop } = useScrollDirection();
  const pathname = usePathname();
  const router = useRouter();
  // Inside an active chat session, the leading icon swaps to a Back
  // arrow that returns to the sessions list at /brain. Industry
  // pattern (ChatGPT, Telegram, Linear mobile): when a thread owns
  // the viewport, the global nav menu is replaced by a thread-
  // local back. The dock is also hidden on this route via
  // mobile-dock's isChatSessionRoute guard. Plan F (2026-05-04).
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
  const showHubChip = tab === "memories" && !!activeHub;
  const canSwitchHub = hubs.length >= 2 && !!onHubSwitchClick;

  // Tab labels reuse the shared `nav.tabs.*` strings the rail
  // consumes — single source of truth across desktop + mobile chrome.
  const tabLabel: Record<ShellTabId, string> = {
    brain: t.nav.tabs.brain,
    memories: t.nav.tabs.memories,
    inbox: t.nav.tabs.inbox,
    agents: t.nav.tabs.agents,
  };

  return (
    <header
      // 56px (h-14) bar + 20px icons + 44px touch targets + 17px
      // title — splits the difference between iOS HIG (44pt nav bar
      // / 17pt title) and Material 3 small top app bar (64dp).
      // Earlier 48px (h-12) was below Apple's 44pt+status-bar minimum
      // and the 13–14px title felt cramped against a 16-17px content
      // body.
      className={`fixed top-0 inset-x-0 z-bar h-14 flex items-center gap-2 px-3 transition-[background-color,border-color] duration-200 ${
        atTop ? "border-b border-transparent" : "border-b border-border/40"
      }`}
      style={{
        paddingTop: "env(safe-area-inset-top)",
        // Inline `background` so the backdrop-filter consistently applies
        // (class-based material was silently dropped in some pipelines).
        // `transparent` at scroll origin so the bar blends into the page
        // background; ~85% --background fill + blur once content scrolls
        // beneath for legibility.
        background: atTop
          ? "transparent"
          : "color-mix(in srgb, var(--background) 85%, transparent)",
        // Always-on backdrop blur — even when fill is transparent the
        // blur is harmless (nothing visible to blur), and keeping it
        // mounted avoids backdrop-filter "popping in" on first scroll.
        backdropFilter: "blur(12px)",
        WebkitBackdropFilter: "blur(12px)",
      }}
    >
      {inChatSession ? (
        <button
          type="button"
          onClick={() => router.push("/brain")}
          aria-label={t.chat.sidebar.heading}
          // 44x44 touch target — iOS HIG minimum. 20px icon for
          // industry-standard back/menu glyph weight.
          className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-fg-2 cursor-pointer transition-colors hover:bg-foreground/6"
        >
          <ArrowLeft className="h-5 w-5" strokeWidth={2} />
        </button>
      ) : (
        <button
          type="button"
          onClick={onMenuClick}
          aria-label={t.nav.primary}
          className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-fg-2 cursor-pointer transition-colors hover:bg-foreground/6"
        >
          <Menu className="h-5 w-5" strokeWidth={2} />
        </button>
      )}

      {showHubChip ? (
        <button
          type="button"
          onClick={canSwitchHub ? onHubSwitchClick : undefined}
          disabled={!canSwitchHub}
          aria-label={
            canSwitchHub
              ? t.composeModal.targetHubAria.replace("{hub}", hubName)
              : hubName
          }
          // 17px font + semibold matches iOS large-title scale for
          // mobile nav identity. py-2 / 44px-ish height keeps the
          // chip in the bar's vertical center without crowding.
          className={`min-w-0 inline-flex items-center gap-2 rounded-full px-3 py-2 text-[17px] text-fg-1 ${
            canSwitchHub
              ? "transition-colors hover:bg-foreground/6 cursor-pointer"
              : "cursor-default"
          }`}
        >
          <HubBadge
            kind={isTeamHub ? "team" : "personal"}
            label={hubInitial}
            accent={activeHub?.hub.accent}
            size="md"
          />
          <span className="truncate max-w-44 font-semibold">{hubName}</span>
          {canSwitchHub ? (
            <ChevronDown
              className="h-4 w-4 text-fg-3 shrink-0"
              strokeWidth={2}
            />
          ) : null}
        </button>
      ) : tab !== null ? (
        // 17px font + semibold = iOS HIG nav bar title. Earlier
        // 13px read as a caption, not a title.
        <span className="min-w-0 truncate text-[17px] font-semibold text-fg-1">
          {tabLabel[tab]}
        </span>
      ) : null}
    </header>
  );
}
