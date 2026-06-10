"use client";

import { useMemo } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Inbox, Network } from "lucide-react";
import { useLocale } from "@/i18n";
import { useAuth, useActiveHub } from "@/lib/auth";
import { useKeyboardOpen } from "@/hooks/use-keyboard-open";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { getShellTabForPath, isChatSessionRoute } from "@/lib/route-helpers";
import { MemaxStar } from "./memax-star";

/**
 * MobileDock — bottom tab bar for mobile.
 *
 * Kitchen 30. Always visible on ALL surfaces (industry standard: iOS tab bar,
 * Instagram, Twitter/X). Users can switch tabs from any route, including
 * detail pages. Only hides when virtual keyboard opens.
 *
 * Active tab inferred from pathname via boundary-safe prefix matching
 * (exact match OR startsWith + "/"). No tab highlighted on unrelated routes.
 *
 * Keyboard handling: When the virtual keyboard opens, the dock stays
 * pinned to the physical screen bottom (hidden behind keyboard) instead
 * of riding up with the visual viewport.
 *
 * Adding a tab: add entry to TABS + create route + add i18n key.
 */

type DockTab = "brain" | "topics" | "inbox";

interface DockTabSpec {
  id: DockTab;
  icon?: React.ElementType;
  star?: boolean;
  labelKey: "brain" | "topics" | "inbox";
  /** Resolved navigation target for this tab. */
  resolveRoute: (memoriesPath: string) => string;
  signature?: boolean;
}

const TABS: DockTabSpec[] = [
  {
    id: "brain",
    labelKey: "brain",
    // `/brain` is the canonical Brain landing.
    resolveRoute: () => "/brain",
    star: true,
    signature: true,
  },
  {
    id: "topics",
    icon: Network,
    labelKey: "topics",
    // Memories tab targets the active-hub-aware path (matches the v2
    // LeftRail's memoriesPath logic). Without this, tapping Memories
    // bounces team-hub users to /h/personal/memories via the legacy
    // /memories middleware redirect — wrong hub.
    resolveRoute: (memoriesPath) => memoriesPath,
  },
  {
    id: "inbox",
    icon: Inbox,
    labelKey: "inbox",
    resolveRoute: () => "/inbox",
    signature: true,
  },
];

/** Map a `ShellTabId` from the URL-based tab resolver onto our local
 *  `DockTab` set. The shell's "memories" maps to the dock's "topics"
 *  (v2 renamed the tab; we kept the older internal id here). The
 *  shell's "agents" tab has no dock counterpart — returns null so the
 *  dock renders unhighlighted on /agents (consistent with /settings). */
function shellTabToDockTab(
  tab: ReturnType<typeof getShellTabForPath>,
): DockTab | null {
  switch (tab) {
    case "brain":
      return "brain";
    case "memories":
      return "topics";
    case "inbox":
      return "inbox";
    case "agents":
    case null:
      return null;
  }
}

export function MobileDock() {
  const pathname = usePathname();
  const router = useRouter();
  const { t } = useLocale();
  const { user } = useAuth();
  const { activeHub } = useActiveHub();
  const keyboardOpen = useKeyboardOpen();

  // Active-hub-aware Memories path. Mirrors the v2 LeftRail's
  // memoriesPath logic so a team-hub user tapping Memories goes to
  // their team hub, not the personal-hub fallback.
  const memoriesPath = useMemo(() => {
    if (!activeHub?.hub || !user) return "/h/personal/memories";
    const slug = hubRouteSlug(activeHub.hub, user.id);
    return `/h/${slug}/memories`;
  }, [activeHub, user]);

  // Active-tab matching delegates to the shared `getShellTabForPath`
  // so /h/<slug>/memories highlights the Memories tab the same way
  // the LeftRail does. Without this, the dock would only match the
  // legacy /memories pattern and lose highlighting after the
  // middleware redirect to /h/<slug>/memories. Codex 5.5 caught this.
  const activeTab = shellTabToDockTab(getShellTabForPath(pathname));

  // Inside an active chat session (/brain/sessions/<id>), hide the
  // dock entirely so the conversation owns the viewport. Industry
  // pattern: ChatGPT and Claude.ai mobile both hide global nav
  // inside threads. The sessions LIST at /brain keeps the dock —
  // it's a list page like any other. Plan F (2026-05-04).
  if (isChatSessionRoute(pathname)) return null;

  return (
    <nav
      aria-label="Main tabs"
      className="fixed left-0 right-0 bottom-0 z-bar"
      style={{
        // Same glass material as the thumb bar so the two bottom surfaces
        // read as one coherent layer instead of "pill on top of a flat
        // rectangle". Matches iOS 26 Liquid Glass / Apple Music direction.
        // Dropped the hairline border-top — the thumb bar above already
        // carries its own border + shadow; stacking another 1px stroke
        // here read as a seam (worse in dark mode where --card/--border
        // sit close). The soft upward shadow does the separation.
        background: "oklch(from var(--card) l c h / 0.92)",
        // Belt-and-suspenders gate: when the keyboard is open we fade
        // the dock out (opacity 0 below), but a live backdrop-filter on
        // a hidden-by-opacity surface still composites on iOS Safari
        // and can leave a faint blurred strip near the home indicator
        // (same class of bug the mobile bar guards against — see
        // app/(app)/layout.tsx:771). Null the filter when hidden.
        backdropFilter: keyboardOpen ? "none" : "blur(40px) saturate(180%)",
        WebkitBackdropFilter: keyboardOpen
          ? "none"
          : "blur(40px) saturate(180%)",
        borderTopLeftRadius: 20,
        borderTopRightRadius: 20,
        boxShadow: "0 -4px 16px oklch(0 0 0 / 0.04)",
        paddingBottom: "max(6px, env(safe-area-inset-bottom))",
        // Hide dock when keyboard is open (native iOS/Android pattern).
        // No translateY math — just fade out and disable pointer events.
        opacity: keyboardOpen ? 0 : 1,
        pointerEvents: keyboardOpen ? "none" : "auto",
        transition: "opacity 0.15s var(--ease-spring)",
      }}
      aria-hidden={keyboardOpen}
    >
      {/* Vertical icon-over-label layout — Apple HIG / Apple Music / Linear iOS.
          ~50px content height + safe-area inset (HIG: 49pt minimum tab bar).
          Active state = signature color tint + icon scale + weight 600. No
          box fill, no underline — pure tint, preserves the flush tab-bar feel. */}
      <div className="flex items-stretch px-2 pt-1.5">
        {TABS.map((tab) => {
          const TabIcon = tab.icon ?? Network;
          const isActive = tab.id === activeTab;
          const activeColor = tab.signature
            ? "var(--signature)"
            : "var(--foreground)";
          const color = isActive ? activeColor : "var(--fg-4)";

          // Color lives on the parent button so lucide icons (currentColor)
          // and the label span inherit together. Instant on tab swap — no
          // color fade, no surface transition overlay. Cross-tab dock swap
          // is snap-instant per explicit user preference.
          const route = tab.resolveRoute(memoriesPath);
          return (
            <button
              key={tab.id}
              onClick={() => {
                // Different tab → navigate. Same tab on detail page → go to tab root.
                // Same tab already on root → no-op (iOS: would scroll to top).
                if (pathname !== route) {
                  router.push(route);
                }
              }}
              aria-current={isActive ? "page" : undefined}
              className="flex flex-1 cursor-pointer flex-col items-center justify-center gap-0.5 py-1.5"
              style={{ color }}
            >
              <span
                className="inline-flex items-center justify-center transition-transform duration-150"
                style={{
                  transitionTimingFunction: "var(--ease-spring)",
                  transform: isActive ? "scale(1.04)" : "scale(1)",
                }}
              >
                {tab.star ? (
                  <MemaxStar className="text-[20px]" />
                ) : (
                  <TabIcon className="h-5 w-5" />
                )}
              </span>
              <span
                className="text-[10px] leading-none tracking-wide"
                style={{ fontWeight: isActive ? 600 : 400 }}
              >
                {t.dock[tab.labelKey]}
              </span>
            </button>
          );
        })}
      </div>
    </nav>
  );
}

export const MOBILE_DOCK_HEIGHT = 56;
