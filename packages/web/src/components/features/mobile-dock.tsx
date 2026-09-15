"use client";

import { useMemo } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useLocale } from "@/i18n";
import { useAuth, useActiveHub } from "@/lib/auth";
import { useKeyboardOpen } from "@/hooks/use-keyboard-open";
import { useBar } from "@/contexts/bar-context";
import { useNotificationSummary } from "@/hooks/use-notifications";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import {
  buildMemoriesPath,
  buildPulsePath,
  getShellTabForPath,
  isChatSessionRoute,
} from "@/lib/route-helpers";
import { SHELL_TABS, type ShellTabId } from "@/components/shell-v2/shell-tabs";

/**
 * Mobile tab bar — the same four tabs as the desktop rail, in the same
 * order (memories / pulse / brain / agents). Sits flush at the bottom
 * under the floating ✦ bar; the bar's rest gap (MOBILE_DOCK_BOTTOM_GAP_PX)
 * is sized so the two never overlap. Hides only while the keyboard is
 * up (the bar takes the bottom band then) and inside a chat session
 * (the session owns the whole screen; the top bar's back arrow is the
 * way out).
 *
 * Badge grammar mirrors the rail: pulse carries a dot only for pending
 * decisions (content channel); unseen updates live on the top-bar bell.
 */
export function MobileDock() {
  const pathname = usePathname();
  const router = useRouter();
  const { t } = useLocale();
  const { user } = useAuth();
  const { activeHub } = useActiveHub();
  const keyboardOpen = useKeyboardOpen();
  const { interaction } = useBar();
  const { data: notificationSummary } = useNotificationSummary();
  // Hidden while the keyboard is up AND while the ✦ bar is in any
  // non-docked compose state: in "mirror" the bar drops to 12px above
  // the viewport bottom (over the dock band) and MobileBarSurface sits
  // BELOW z-bar, so a visible dock would paint over the input row and
  // stay tappable mid-compose. Keyboard alone is not a safe signal —
  // hardware keyboards never trip useKeyboardOpen.
  const hidden = keyboardOpen || interaction.mobileComposeState !== "docked";

  const hubSlug = useMemo(
    () =>
      activeHub?.hub && user
        ? hubRouteSlug(activeHub.hub, user.id)
        : "personal",
    [activeHub, user],
  );

  const activeTab = getShellTabForPath(pathname);
  const pulseNeedsAction = (notificationSummary?.needs_action_pending ?? 0) > 0;

  if (isChatSessionRoute(pathname)) return null;

  const label: Record<ShellTabId, string> = {
    memories: t.dock.memories,
    pulse: t.dock.pulse,
    brain: t.dock.brain,
    agents: t.dock.agents,
  };

  const routeFor = (id: ShellTabId): string => {
    const spec = SHELL_TABS.find((s) => s.id === id);
    if (spec?.staticPath) return spec.staticPath;
    return id === "pulse"
      ? buildPulsePath(hubSlug)
      : buildMemoriesPath(hubSlug);
  };

  return (
    <nav
      aria-label={t.nav.primary}
      className="fixed left-0 right-0 bottom-0 z-bar"
      style={{
        background: "oklch(from var(--card) l c h / 0.92)",
        backdropFilter: hidden ? "none" : "blur(40px) saturate(180%)",
        WebkitBackdropFilter: hidden ? "none" : "blur(40px) saturate(180%)",
        borderTopLeftRadius: 20,
        borderTopRightRadius: 20,
        boxShadow: "0 -4px 16px oklch(0 0 0 / 0.04)",
        paddingBottom: "max(6px, env(safe-area-inset-bottom))",
        opacity: hidden ? 0 : 1,
        pointerEvents: hidden ? "none" : "auto",
        transition: "opacity 0.15s var(--ease-spring)",
      }}
      aria-hidden={hidden}
    >
      {/* Icon-over-label, 49pt-minimum band (Apple HIG). Active = tint +
          slight icon scale + weight; no fill, no underline. */}
      <div className="flex items-stretch px-2 pt-1.5">
        {SHELL_TABS.map((spec) => {
          const Icon = spec.icon;
          const isActive = spec.id === activeTab;
          const route = routeFor(spec.id);
          return (
            <button
              key={spec.id}
              type="button"
              onClick={() => {
                if (pathname !== route) router.push(route);
              }}
              aria-current={isActive ? "page" : undefined}
              className="flex flex-1 cursor-pointer flex-col items-center justify-center gap-0.5 py-1.5"
              style={{ color: isActive ? "var(--foreground)" : "var(--fg-4)" }}
            >
              <span
                className="relative inline-flex items-center justify-center transition-transform duration-150"
                style={{
                  transitionTimingFunction: "var(--ease-spring)",
                  transform: isActive ? "scale(1.04)" : "scale(1)",
                }}
              >
                <Icon className="h-5 w-5" strokeWidth={isActive ? 2.25 : 2} />
                {spec.id === "pulse" && pulseNeedsAction ? (
                  <span
                    aria-hidden
                    className="absolute -right-1 -top-0.5 h-1.5 w-1.5 rounded-full"
                    style={{ background: "var(--signature)" }}
                  />
                ) : null}
              </span>
              <span
                className="text-[10px] leading-none tracking-wide"
                style={{ fontWeight: isActive ? 600 : 400 }}
              >
                {label[spec.id]}
              </span>
            </button>
          );
        })}
      </div>
    </nav>
  );
}
