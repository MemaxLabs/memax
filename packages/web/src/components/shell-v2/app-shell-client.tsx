"use client";

/**
 * AppShellClient — host for the app's providers, portal anchors, and
 * direct-mount surfaces (BarProvider, MobileBarSurface,
 * MobileComposeOverlay, MemaxEventBridge, RouteHistoryProvider,
 * IsMobileProvider, SettingsDialogProvider, TopicTreePanelProvider,
 * TopicDndProvider, …).
 *
 * Plan 24 phase 4b removed the v1/v2 cookie dispatch — pre-launch,
 * no external users — so v2 chrome is the only path. Mobile chrome
 * (floating BrandMark, top-right hub chip, MobileDock, tree drawer
 * overlay host) still mounts on `isMobile` until plan 22 ships v2
 * mobile chrome.
 */

import { useEffect, useRef, useState } from "react";
import { useRouter, usePathname, useSearchParams } from "next/navigation";
import { useAuth, useActiveHub } from "@/lib/auth";
import { Suspense } from "react";
import { MemaxLoader } from "@memaxlabs/ui";
import { SettingsPanel } from "@/components/features/settings-panel";
import { SettingsDialog } from "@/components/features/settings/settings-dialog";
import { MemaxDebugger } from "@/components/features/memax-debugger";
import { ImpersonationBar } from "@/components/features/impersonation-bar";
import { MemaxEventBridge } from "@/components/features/memax-event-bridge";
import {
  SettingsDialogProvider,
  useSettingsDialog,
} from "@/contexts/settings-dialog-context";
import {
  SettingsPanelProvider,
  useSettingsPanel,
} from "@/contexts/settings-panel-context";
import { useIsMobile, IsMobileProvider } from "@/hooks/use-is-mobile";
import { useKeyboardOpen } from "@/hooks/use-keyboard-open";
import { useScrollDirection } from "@/hooks/use-scroll-direction";
import { useShellState } from "@/contexts/shell-state-context";
import { getBarClickOutsideAction } from "@/lib/bar-interaction";
import { MobileDock } from "@/components/features/mobile-dock";
import { motion, AnimatePresence, useReducedMotion } from "framer-motion";
import { FAST, NORMAL, EASE } from "@memaxlabs/ui/tokens/motion";
import { BarProvider, useBar } from "@/contexts/bar-context";
// SelectionProvider is section-scoped (not layout-scoped) — each section
// (Recent, Inbox, TopicDetail) wraps its own SelectionProvider so batch
// selection doesn't leak across conceptually distinct groups.
import { BarLogoPortal } from "@/components/bar/bar-logo-portal";
import { BarInputPortal } from "@/components/bar/bar-input-portal";
import { BarRightPortal } from "@/components/bar/bar-right-portal";
import { BarExpandPortal } from "@/components/bar/bar-expand-portal";
import { BarChipPortal } from "@/components/bar/bar-chip-portal";
import { MobileComposeOverlay } from "@/components/bar/mobile-compose-overlay";
import { MobileBarSurface } from "@/components/bar/mobile-bar-surface";
import { MobileThumbBarShell } from "@/components/bar/mobile-thumb-bar-shell";
import { MobileThumbBarRow } from "@/components/bar/mobile-thumb-bar-row";

import {
  TopicTreePanelProvider,
  TopicTreePanelOverlayHost,
  useTreePanel,
} from "@/components/features/topic/topic-tree-panel";
import dynamic from "next/dynamic";
import { BarNotificationCard } from "@/components/bar/bar-notification-card";
import { useLocale } from "@/i18n";
import { useSettings } from "@/hooks/use-settings";
import { resolveHubHeaderMode } from "@/lib/hub-header-mode";
import { SurfaceTransitionOverlay } from "@/components/features/surface-transition-overlay";
import { InboxSurfaceProvider } from "@/contexts/inbox-surface-context";
import { ComposeProvider, useCompose } from "@/contexts/compose-context";
import { ComposeModal } from "@/components/features/compose/compose-modal";
import { RouteHistoryProvider } from "@/contexts/route-history-context";
import { NavigationDirectionProvider } from "@/contexts/navigation-direction";
import {
  BRAIN_BAR_REST_TOP,
  BAR_ENGAGED_TOP,
  TREE_SIDEBAR_W,
  DESKTOP_DOCK_BOTTOM_GAP_PX,
  MOBILE_DOCK_BOTTOM_GAP_PX,
  BAR_HEIGHT,
  MOBILE_BAR_HEIGHT,
  MOBILE_DOCK_HEIGHT,
} from "@/lib/layout";

import {
  registerBatchActiveListener,
  unregisterBatchActiveListener,
} from "@/lib/batch-active";
import {
  clearLiveSurfaceTransition,
  subscribeLiveSurfaceTransition,
  type SurfaceTransitionRequest,
} from "@/lib/recent-navigation";
import { ShellStateProvider } from "@/contexts/shell-state-context";
import { ShellLayoutV2 } from "@/components/shell-v2/shell-layout";
import { registerSuperProperties } from "@/lib/posthog";
import {
  getShellTabForPath,
  isChatSurfaceRoute,
  isMemoriesOverviewRoute,
} from "@/lib/route-helpers";

const TopicDndProvider = dynamic(
  () =>
    import("@/components/features/topic/topic-dnd-provider").then(
      (m) => m.TopicDndProvider,
    ),
  { ssr: false },
);

interface AppShellClientProps {
  children: React.ReactNode;
}

export function AppShellClient({ children }: AppShellClientProps) {
  // Tag every PostHog event with `shellVersion: "v2"` for telemetry
  // continuity with the soak gate (plan 24 §5.5). Plan 24 phase 4b
  // removed the v1 dispatch path, so this is constant — kept so the
  // rollout-gate query keeps reading the same property name.
  useEffect(() => {
    registerSuperProperties({ shellVersion: "v2" });
  }, []);

  // SettingsPanelProvider lives ABOVE <AppShell> so AppShell can call
  // useSettingsPanel() at its top level. Multiple trigger surfaces
  // (the floating mobile-fallback avatar, the v2 LeftRail footer)
  // share the same open state via this provider.
  return (
    <IsMobileProvider>
      <NavigationDirectionProvider>
        <SettingsPanelProvider>
          <Suspense fallback={<MemaxLoader />}>
            <AppShell>{children}</AppShell>
          </Suspense>
        </SettingsPanelProvider>
      </NavigationDirectionProvider>
    </IsMobileProvider>
  );
}

function AppShell({ children }: { children: React.ReactNode }) {
  const { user, loading, hubs } = useAuth();
  const { activeHub, isTeamHub } = useActiveHub();
  const router = useRouter();
  const pathname = usePathname();
  const isMobile = useIsMobile();
  const { t } = useLocale();
  const { data: settings } = useSettings();
  const {
    open: settingsOpen,
    toggle: toggleSettings,
    close: closeSettings,
  } = useSettingsPanel();
  const [liveTransition, setLiveTransition] =
    useState<SurfaceTransitionRequest | null>(null);
  const [liveTransitionVisible, setLiveTransitionVisible] = useState(false);
  // Banner chrome predicate. Consumed inside the mobile-only
  // floating top-right chrome block below (controls the glass-card
  // tint on /memories overview). Desktop routes its own banner via
  // <ShellLayoutV2> aurora + <TopicGrid>'s hub header.
  //
  // Topic / memory detail nested routes get their own header chrome,
  // so we keep this scoped to the OVERVIEW surface only — fires on
  // `/memories[/]` AND `/h/<slug>/memories[/]`, not on `/memories/<id>`
  // or `/h/<slug>/memories/<id>`.
  const showBannerChrome =
    isMemoriesOverviewRoute(pathname) &&
    resolveHubHeaderMode(activeHub?.hub ?? null, settings) !== "none";
  const previousPathnameRef = useRef<string | null>(null);

  useEffect(() => {
    previousPathnameRef.current = pathname;
  }, [pathname]);

  useEffect(() => {
    return subscribeLiveSurfaceTransition((request) => {
      setLiveTransition(request);
      setLiveTransitionVisible(Boolean(request));
    });
  }, []);

  useEffect(() => {
    if (!liveTransition) return;
    // Hub-switch animation only fires on the memories OVERVIEW under
    // either shell. v1 lands on `/memories`; v2 on `/h/<slug>/memories`.
    if (
      liveTransition.kind !== "hub-switch" ||
      !isMemoriesOverviewRoute(pathname)
    ) {
      return;
    }
    const holdMs = liveTransition.minDurationMs ?? 420;
    const fadeMs = 200;
    const maxMs = liveTransition.maxDurationMs ?? 2000;
    // When the destination data settles almost immediately (cache-warm
    // hub switch), the overlay would otherwise sit dimmed for the full
    // min-hold announcing a wait that isn't happening — perceived
    // latency added by the loading UI itself. Under this threshold we
    // drop the hold and let the 200ms opacity fade be the entire
    // transition. The min-hold only applies when there is a real wait,
    // where it keeps the pill legible instead of flash-blinking.
    const instantThresholdMs = 120;
    let settled = false;

    const finish = () => {
      if (settled) return;
      settled = true;
      const elapsed = performance.now() - startedAt;
      const remaining =
        elapsed < instantThresholdMs ? 0 : Math.max(0, holdMs - elapsed);
      const hideTimeout = window.setTimeout(() => {
        setLiveTransitionVisible(false);
      }, remaining);
      const clearTimeoutId = window.setTimeout(() => {
        clearLiveSurfaceTransition();
      }, remaining + fadeMs);
      timeouts.push(hideTimeout, clearTimeoutId);
    };

    const startedAt = performance.now();
    const timeouts: number[] = [];
    const maxTimeout = window.setTimeout(() => {
      finish();
    }, maxMs);
    timeouts.push(maxTimeout);

    if (liveTransition.waitFor) {
      Promise.resolve(liveTransition.waitFor)
        .catch(() => {})
        .finally(() => finish());
    } else {
      finish();
    }
    return () => {
      timeouts.forEach((id) => window.clearTimeout(id));
    };
  }, [liveTransition, pathname]);

  // ── Page loader with flare arrival ──
  //
  // The flare is a transition animation that bridges from "loader
  // showing" to "app shell showing" so the visual swap doesn't feel
  // abrupt. It is meaningful ONLY when the user actually had to wait
  // for auth to load. On a cached / pre-warmed first render (the
  // common case — already logged in, navigating between routes,
  // tab-switching back) auth is already resolved synchronously, no
  // loader was ever shown, and the flare would just be a 500ms delay
  // between mount and content with no narrative payoff.
  //
  // Initialize `arrived` from the synchronous auth state on mount:
  // if user already there → arrived=true → no loader, no flare, app
  // renders on the first frame. If auth is still loading → arrived
  // stays false → loader shows → flare plays on transition → arrived
  // flips → content reveals.
  const [arrived, setArrived] = useState(() => !loading && !!user);
  const authReady = !loading && !!user;

  useEffect(() => {
    if (!loading && !user) router.replace("/login");
  }, [user, loading, router]);

  if (!arrived && (loading || !user || authReady)) {
    if (loading || !user) {
      return <MemaxLoader />;
    }
    return <MemaxLoader ready onArrived={() => setArrived(true)} />;
  }
  if (!user) return null;

  return (
    <InboxSurfaceProvider>
      <ComposeProvider>
        <BarProvider>
          <SettingsDialogProvider>
            <ComposeTriggers />
            {/* ShellStateProvider lifted above <GlobalBar> + <V2ChromeWrap>
                (plan 26 phase 4). The bar reads `barScrollHidden` to drive
                its sticky scroll-hide state, and V2ChromeWrap's rail +
                ScanRestButton read the same flag. Single source for both.

                ComposeProvider wraps BarProvider as of 2026-05-20 so the
                bar can call `useCompose().openModal()` from its
                `⌘⇧↵`-in-bar handler, carrying the bar's in-progress
                text into the compose draft. Order matters: providers
                that DEPEND on ancestors live below them. */}
            <ShellStateProvider>
              <TopicTreePanelProvider>
                <div className="theme-warm relative bg-background">
                  {liveTransition && (
                    <SurfaceTransitionOverlay
                      request={liveTransition}
                      visible={liveTransitionVisible}
                      label={
                        liveTransition.kind === "hub-switch" &&
                        liveTransition.hubName
                          ? t.hubs.switchingTo.replace(
                              "{name}",
                              liveTransition.hubName,
                            )
                          : null
                      }
                    />
                  )}
                  <ImpersonationBar />

                  <SettingsPanel
                    open={settingsOpen}
                    onClose={closeSettings}
                    anchor="bottom-left"
                  />
                  <SettingsDialog />
                  <MemaxDebugger />
                  <MemaxEventBridge />

                  {/* Global bar + portals + backdrop. */}
                  <GlobalBar />

                  <TopicDndProvider>
                    <div className="relative flex">
                      {/*
                    No inner <ErrorBoundary> here — Next's segment
                    error.tsx ((app)/error.tsx) catches render
                    errors for this subtree now. Keeping a custom
                    React boundary at this level would pre-empt the
                    Next boundary and the telemetry/retry it wires
                    up via unstable_retry.
                  */}
                      <main className="relative flex-1 min-w-0">
                        <RouteHistoryProvider
                          previousPathname={previousPathnameRef.current}
                        >
                          <V2ChromeWrap pathname={pathname}>
                            {children}
                          </V2ChromeWrap>
                        </RouteHistoryProvider>
                      </main>
                    </div>
                    {/* Mobile bottom-sheet host for the topic tree —
                    mounted inside DndContext so its tree nodes register
                    as drop targets. Plan 22 mobile drawer renders the
                    tree inline; this host is for legacy modal-tree
                    surfaces still mounted on `isMobile` (will be
                    pruned as those surfaces migrate). */}
                    {isMobile && <TopicTreePanelOverlayHost />}
                  </TopicDndProvider>
                </div>
              </TopicTreePanelProvider>
            </ShellStateProvider>
            {/* Compose modal lives at the same shell-root level as
              <SettingsDialog> — single global instance, opened via
              `useCompose().openModal()` from anywhere in the tree. */}
            <ComposeModal />
          </SettingsDialogProvider>
        </BarProvider>
      </ComposeProvider>
    </InboxSurfaceProvider>
  );
}

/**
 * Global compose triggers — both the `⌘⇧↵` hotkey and the
 * `?compose=1` URL deep link route through here.
 *
 * Lives inside `<ComposeProvider>` so it can call `openModal()` via
 * context. Bar's textarea handler matches `⌘↵` for quick capture and
 * explicitly ignores `e.shiftKey` so the shift-modified form falls
 * through to this listener exclusively.
 *
 * `?compose=1` lets share-links, email CTAs, and agent-generated URLs
 * land users in the compose flow on any route. The query param is
 * stripped after firing so a browser back/forward doesn't re-open the
 * modal in a stale state.
 *
 * No-op on `e.isComposing` — Asian IME `Enter` press shouldn't open
 * the modal mid-character.
 */
function ComposeTriggers() {
  const { openModal } = useCompose();
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.isComposing) return;
      if (e.key !== "Enter") return;
      if (!(e.metaKey || e.ctrlKey)) return;
      if (!e.shiftKey) return;
      e.preventDefault();
      openModal();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [openModal]);

  useEffect(() => {
    if (searchParams.get("compose") !== "1") return;
    openModal();
    // Strip the param so back/forward doesn't re-trigger and so the
    // canonical URL (which the user might copy) doesn't carry the
    // one-shot modal trigger.
    const next = new URLSearchParams(searchParams);
    next.delete("compose");
    const qs = next.toString();
    router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
  }, [searchParams, openModal, router, pathname]);

  return null;
}

/**
 * GlobalBar — the single bar instance for the entire app.
 * Always mounted. Visibility controlled by view + overlay state.
 * All portals and backdrop live here — pages don't render bar logic.
 */
function GlobalBar() {
  const pathname = usePathname() ?? "";
  const {
    view,
    phase,
    isDragging,
    isMobile,
    barFocused,
    setBarFocused,
    barOverlayOpen,
    hideBar,
    reset,
    askAnswer,
    aiLoading,
    cancelAI,
    peelBack,
    isBarExpanded,
    value,
    recallQuery,
    mobileComposeOpen,
    mobileComposeActive,
    mobileComposeState,
    stagedFiles,
    handleChange,
    handleKeyDown,
    handleSubmit,
    inputRef,
    sendKind,
    scope,
    addStagedFiles,
    openMobileComposeActive,
    openMobileCompose,
    interaction,
    surface,
    barHasState,
    activeNotification,
    setIsBarExpanded,
    closeMobileComposeActive,
    clearNavigationSelection,
    barPanelSuppressed,
    setBarPanelSuppressed,
  } = useBar();
  const { t } = useLocale();
  // Glass-edge offset must track BOTH the user's explicit toggle state
  // AND the transient drag-session auto-open, otherwise the bottom blur
  // strip runs under the temporarily opened tree during a drag. Computed
  // via `desktopLeftWidth()` which returns 0 when the tree is closed
  // and the panel's visual footprint (TREE_SIDEBAR_W) when it's open
  // — the panel floats via position: fixed and does not push content.
  const { isPinned: isTreePinned, isDragSessionOpen } = useTreePanel();

  const barRef = useRef<HTMLDivElement>(null);
  // Notification now lives OUTSIDE the bar's fixed DOM (see the
  // z-bar-notif sibling in the return) so the bar's z-bar glass can
  // paint over its tuck region.
  // The click-outside handler must still treat the notification as "inside
  // bar" — otherwise clicking the notification's dismiss/action would fire
  // the bar's outside-dismiss path (codex review finding).
  const barNotifRef = useRef<HTMLDivElement>(null);
  const [barVisible, setBarVisible] = useState(false);
  const [batchActive, setBatchActive] = useState(false);
  const keyboardOpen = useKeyboardOpen();
  const { direction: scrollDirection, atTop } = useScrollDirection();
  const reduceMotion = useReducedMotion();
  // Phase 4 — sticky scroll-hide. Once the user scrolls down enough to
  // hide the bar, the bar STAYS hidden (scroll-up no longer reveals it)
  // until the user reaches the top of the page. The re-entry is the
  // ScanRestButton (FAB-style search button at bottom-right) which
  // becomes visible only while `barScrollHidden` is true; clicking it
  // calls openBar() AND clears the flag so the bar reveals again.
  // Lives in ShellStateContext so both this component (drives bar
  // hide rendering) and ScanRestButton (drives FAB visibility) read
  // from one source — duplicating local state would be racy.
  // `barShown` mirrors `shouldShow` so ScanRestButton can surface as
  // the persistent re-entry on routes where the bar is route-hidden
  // (view==="none" without overlay).
  const { barScrollHidden, setBarScrollHidden, setBarShown } = useShellState();

  // Listen for batch toolbar visibility — bar hides when batch toolbar shows
  useEffect(() => {
    registerBatchActiveListener(setBatchActive);
    return () => unregisterBatchActiveListener();
  }, []);

  // Mobile with dock: bar is only inline on Brain tab. Topics tab uses dock.
  // Desktop: bar is inline ONLY on Brain (the centered capture surface
  // that IS the brain page). Memory + inbox + agents + everywhere else
  // default to FAB-as-entry (ScanRestButton); tapping the FAB opens the
  // bar as an overlay. User-driven shift away from a permanently-docked
  // bar to a single FAB language across all destination routes.
  //
  // Phase 3.7c chat surface — when the new ChatBrainView owns the page
  // (`isChatSurfaceRoute`), the chat composer is the user's input.
  // Suppressing the inline bar (and the FAB below) avoids two
  // competing text affordances on the same surface; option A of the
  // chat-integration plan.
  const onChatSurface = isChatSurfaceRoute(pathname);
  const isInline = !onChatSurface && view === "brain";
  // Overlay path now applies on ANY non-brain desktop route (memory,
  // inbox, agents, etc.) so opening the bar via the FAB consistently
  // surfaces it as a centered overlay regardless of which destination
  // the user was on.
  const isOverlay = !isInline && barOverlayOpen;
  const isMemory = view === "memory";
  // Bar is always mounted. Visible when on a bar page, or overlay is open.
  // Batch no longer hides the bar — it mutes it (see barMuted below) so
  // users keep their spatial anchor for where the bar lives.
  const shouldShow = isInline || isOverlay;
  // Mirror shouldShow into shared shell state so ScanRestButton can
  // become the persistent re-entry on routes where the bar isn't
  // visible (e.g., /agents, /inbox without overlay). Plan 26 follow-up.
  useEffect(() => {
    setBarShown(shouldShow);
  }, [shouldShow, setBarShown]);
  // Muted state: bar stays mounted but is visually secondary and
  // non-interactive while the batch toolbar is active. 0.45 opacity +
  // pointer-events-none + inert attribute mirrors the disabled-button
  // grammar the user already recognises.
  const barMuted = batchActive && shouldShow;
  // Whether a click outside the bar should dismiss something. Idle-focused
  // and empty-expanded cases are covered so tap-outside collapses the bar
  // the same way empty-bar ⌘K does (item 5 symmetry). Any preservable
  // state (remember "sent" chrome, detectedUrl chip, committed scope,
  // etc.) also mounts the listener so tap-outside can drop the bar to rest
  // — without it, post-remember "Sent to memax" stayed engaged forever.
  const shouldDismissOnClickOutside =
    !isMobile &&
    (isOverlay ||
      (isInline && phase !== "input") ||
      (isInline && (barFocused || isBarExpanded || barHasState())));

  // Click-outside dismiss (Radix DismissableLayer pattern).
  // Zero DOM overlay — listens on document, checks containment.
  useEffect(() => {
    if (!shouldDismissOnClickOutside) return;

    const isInBar = (target: Node | null) => {
      if (!target) return false;
      if (barRef.current?.contains(target)) return true;
      // Notification is a z-bar-notif sibling of barRef — clicking its dismiss /
      // action must count as inside-bar so the bar's outside-dismiss path
      // doesn't fire.
      if (barNotifRef.current?.contains(target)) return true;
      const expand = document.getElementById("bar-expand-slot");
      if (expand?.contains(target)) return true;
      const input = document.getElementById("bar-input-slot");
      if (input?.contains(target)) return true;
      const right = document.getElementById("bar-right-slot");
      if (right?.contains(target)) return true;
      const logo = document.getElementById("bar-logo-slot");
      if (logo?.contains(target)) return true;
      return false;
    };

    const handler = (e: MouseEvent) => {
      if (isInBar(e.target as Node)) return;
      const action = getBarClickOutsideAction({
        isOverlay,
        aiLoading,
        phase,
        hasState: barHasState(),
      });
      switch (action) {
        case "hide-overlay":
          hideBar();
          return;
        case "clear-ai":
          // Cancel the stream — abort controller, bump request id,
          // clear answer + loading + streaming. Prevents late SSE
          // deltas from rehydrating the answer after dismiss
          // (codex-review 2026-04-21).
          cancelAI();
          return;
        case "suppress-with-draft":
          setBarPanelSuppressed(true);
          inputRef.current?.blur();
          closeMobileComposeActive();
          return;
        case "collapse-empty":
          inputRef.current?.blur();
          setIsBarExpanded(false);
          clearNavigationSelection();
          closeMobileComposeActive();
          return;
      }
    };

    document.addEventListener("pointerdown", handler);
    return () => document.removeEventListener("pointerdown", handler);
  }, [
    shouldDismissOnClickOutside,
    isOverlay,
    aiLoading,
    hideBar,
    cancelAI,
    phase,
    barHasState,
    inputRef,
    setIsBarExpanded,
    clearNavigationSelection,
    closeMobileComposeActive,
    setBarPanelSuppressed,
  ]);

  useEffect(() => {
    // Mobile: no 100ms stagger — the bar is always at its resting dock
    // position with no slide-in. Desktop keeps the short delay so the bar
    // settles after route transitions.
    if (shouldShow) {
      if (isMobile) {
        setBarVisible(true);
        return;
      }
      const t = setTimeout(() => setBarVisible(true), 100);
      return () => clearTimeout(t);
    }
    setBarVisible(false);
  }, [shouldShow, isMobile]);

  // Track focus on bar slots
  useEffect(() => {
    const inBar = (el: Element | null) =>
      el?.closest("#bar-input-slot") ||
      el?.closest("#bar-right-slot") ||
      el?.closest("#bar-logo-slot") ||
      el?.closest("#bar-expand-slot");
    const onFocus = (e: Event) => {
      if (inBar(e.target as HTMLElement)) {
        setBarFocused(true);
        // Focus on the bar always brings the expand surface back —
        // tap-outside / ⌘K suppress is user-dismissed chrome, not a
        // persistent preference. The setter is a stable useCallback and
        // no-ops in the reducer when the state is already false.
        setBarPanelSuppressed(false);
      }
    };
    const onBlur = () => {
      setTimeout(() => {
        if (!inBar(document.activeElement)) setBarFocused(false);
      }, 10);
    };
    document.addEventListener("focusin", onFocus);
    document.addEventListener("focusout", onBlur);
    return () => {
      document.removeEventListener("focusin", onFocus);
      document.removeEventListener("focusout", onBlur);
    };
  }, [setBarFocused, setBarPanelSuppressed]);

  // Bar position — route first, then interaction.
  //
  // Brain view idle: centered slightly below midline so the memory-count
  // anchor and quiet prompt have room underneath. This is the quiet
  // "dump or ask" state.
  // Brain/memory/overlay engaged: lifts to 24vh once the user has actual
  // content/results/files, so the expand surface can grow downward under a
  // stable input row. Plain focus only changes chrome; it does not lift.
  // Memory view idle: bottom-docked at 32px.
  // Mobile: above dock, keyboard-aware.
  const isBrainView = !isMobile && view === "brain";
  const isDockedDesktopRoute = !isMobile && (isMemory || view === "none");
  const shouldUseLiftedPosition =
    !isMobile && surface.visibilityState === "engaged";
  const easing = `cubic-bezier(${EASE.join(", ")})`;
  // Dedicated rest→engaged curve for the bar. The shared EASE is easeOutExpo
  // [0.16, 1, 0.3, 1]; over 200ms NORMAL on a long `top` travel (brain
  // 42vh→24vh, memory bottom→24vh) that reads as "floating into place."
  // The iOS 18 sheet curve below lands faster (perceived arrival in the
  // first ~60ms) with a touch of decel-overshoot — bar reads as *arrived*,
  // not *flown*. Scoped locally to avoid churning every other surface.
  const barEnterEase = "cubic-bezier(0.22, 1, 0.36, 1)";
  // Plan 26 follow-up: re-aligned to NORMAL (0.2s) so the bar's `top`
  // transition runs in lockstep with `transform` (which composes
  // `barTranslateY` + scroll-hide + notif-outer offsets, all NORMAL).
  // Earlier the top was tightened to 0.12s to feel snappy on the
  // FAB↔bar morph, but FAB visibility is opacity-driven (not top-
  // driven), so the snappy duration here only created a desync: top
  // settled in 120ms while translateY (rest -50% → engaged 0%) kept
  // animating to 200ms — the residual translateY moved the bar DOWN
  // by half-its-height during the last 80ms, which user reported as
  // a "下沉" sink. Aligning durations keeps the engaged motion a clean
  // upward lift. iOS-sheet curve preserved for the deceleration feel.
  const barEnterDuration = `${NORMAL}s`;
  const hasExpandSurface = surface.showExpandSurface;
  const hasText = interaction.hasText;
  const hasFiles = interaction.hasFiles;
  const sendState = interaction.sendState;
  const desktopRestDockTop = `calc(100vh - ${DESKTOP_DOCK_BOTTOM_GAP_PX}px)`;
  const mobileRestTop = keyboardOpen
    ? "calc(100dvh - 8px)"
    : `calc(100dvh - ${MOBILE_DOCK_BOTTOM_GAP_PX}px - var(--safe-bottom, 0px))`;
  const barTop = isMobile
    ? mobileRestTop
    : isBrainView && !shouldUseLiftedPosition
      ? // Brain view at REST: bar centered on 42vh (translateY=-50%
        //  composes with this anchor to render the bar visually centered).
        BRAIN_BAR_REST_TOP
      : isOverlay || shouldUseLiftedPosition
        ? // ENGAGED on any view (brain typing, FAB tap on memory/inbox/
          //  agents, Cmd+K overlay): bar surfaces at the engaged anchor
          //  (24vh) so results have ample room below the input. Earlier
          //  this branch was unreachable on brain view because the
          //  isBrainView short-circuit fired regardless of engagement —
          //  meaning brain view never moved the bar's top anchor on
          //  rest→engaged, and translateY's -50%→0% transition produced
          //  a visible "下沉" sink (bar's effective top dropped by half-
          //  its-height). User-reported plan 26 follow-up.
          BAR_ENGAGED_TOP
        : isDockedDesktopRoute
          ? desktopRestDockTop
          : undefined;
  const barTranslateY = isMobile
    ? "-100%"
    : shouldUseLiftedPosition
      ? "0%"
      : isBrainView
        ? "-50%"
        : isDockedDesktopRoute
          ? "-100%"
          : "0%";

  // Apple-style scroll reveal: bar hides on scroll-down, reveals on
  // scroll-up. Applies to the desktop bottom-docked state AND to the
  // mobile dock-aligned bar when the keyboard is closed. Any interaction
  // signal pins the bar in place — `barHasState()` covers remember /
  // scope / detectedUrl / mobileComposeState; `activeNotification`
  // covers transient/dream/status banners. Reduced-motion users never
  // get the hide animation.
  // Scroll-hide only applies where it makes UX sense:
  //   · Desktop docked /memories — bar is bottom chrome, scroll-hide
  //     mimics Safari tab-bar behavior so content can read clean.
  //   · Mobile NON-brain routes (memory view, detail, topic) —
  //     scroll-hide helps focus on content. Brain tab is empty chrome
  //     with no scrollable content; auto-hiding a floating bar there
  //     just disorients (user explicitly asked to exclude it).
  const scrollHideableRoute =
    (isDockedDesktopRoute && !shouldUseLiftedPosition) ||
    (isMobile && !keyboardOpen && view !== "brain");
  const scrollHideable =
    scrollHideableRoute &&
    !barFocused &&
    !barOverlayOpen &&
    !barHasState() &&
    !activeNotification &&
    !isDragging &&
    // Batch mode keeps the bar mounted and muted as a spatial anchor; a
    // scroll-hide slide that removes it mid-selection defeats that whole
    // premise AND strands the batch toolbar with a --bar-dock-offset
    // reserving space for a bar that's no longer visible.
    !batchActive &&
    !reduceMotion;
  // Phase 4 — Once-hidden-stays-hidden. Effect A flips
  // `barScrollHidden=true` the first time the user scrolls down on a
  // scrollable surface; Effect B resets it when they reach the top. The
  // `!atTop` gate on Effect A ensures pages without scrollable content
  // (atTop stays true permanently) can't get into a state where the bar
  // is hidden but never resets — Effect B would otherwise race with a
  // touchmove-driven "down" signal. Bar hide is driven by the sticky
  // `barScrollHidden` rather than the live scrollDirection, so scroll-
  // up doesn't restore the bar mid-scroll.
  useEffect(() => {
    if (scrollDirection === "down" && scrollHideable && !atTop) {
      setBarScrollHidden(true);
    }
  }, [scrollDirection, scrollHideable, atTop, setBarScrollHidden]);
  useEffect(() => {
    if (atTop) setBarScrollHidden(false);
  }, [atTop, setBarScrollHidden]);
  const isScrollHidden = scrollHideable && barScrollHidden;
  // ScanRestButton owns the FAB tap path: it calls openBar() AND
  // clears barScrollHidden itself when clicked (so the bar reveals
  // AND focuses simultaneously). No openBar reference needed here.

  // Compose the scroll-hide translate so the bar's top edge lands
  // exactly at 100vh (fully below the viewport) regardless of bar
  // height. In the docked rest state, `top` + `translateY(-100%)`
  // places the bar's bottom edge at (viewport-bottom − dockGap), so
  // translating by (100% of own height + dockGap) pushes the bar
  // entirely below the viewport. A fixed `120%` — the previous
  // constant — assumed a specific bar height and left ~10–60px of
  // the bar still on-screen on both mobile (68px gap) and desktop
  // (32px gap); on mobile Safari the backdrop-filter blur pass
  // then painted that remaining strip as a rectangular blur blob
  // over page content. Safe-bottom is added on mobile so the
  // translate tracks device-safe-area insets baked into `top`.
  const dockBottomGapPx = isMobile
    ? MOBILE_DOCK_BOTTOM_GAP_PX
    : DESKTOP_DOCK_BOTTOM_GAP_PX;
  const scrollHideSafeBottom = isMobile ? " + var(--safe-bottom, 0px)" : "";
  const scrollHideTranslate = isScrollHidden
    ? `calc(100% + ${dockBottomGapPx}px${scrollHideSafeBottom})`
    : "0px";

  // Dock-offset signal for surfaces that stack above bottom chrome
  // (batch toolbar, toasts anchored to the bottom, etc). Computed as
  // the total bottom-chrome height the stacked surface must clear:
  //
  //   Desktop docked bar (memories rest, overlay) → BAR_HEIGHT
  //   Mobile bar present (brain tab)              → MOBILE_BAR_HEIGHT
  //   Mobile dock only (non-brain routes)         → MOBILE_DOCK_HEIGHT
  //   Desktop lifted / brain centered / no chrome → 0
  //
  // Mobile dock is always visible on ALL mobile surfaces (per
  // mobile-dock.tsx comment — only hides with the keyboard), so
  // mobile batch mode ALWAYS needs to clear MOBILE_DOCK_HEIGHT.
  // When the mobile bar is also showing (brain tab), it stacks
  // ABOVE the dock, so the bar's taller total is the one we clear.
  // `isOverlay` is intentionally NOT in the desktop path — plan 26's
  // FAB-as-rest-state shift positions desktop overlays at
  // BAR_ENGAGED_TOP (24vh center), NOT at the bottom. Including
  // `isOverlay` here would publish BAR_HEIGHT as bottom-chrome offset
  // for an overlay that's centered at top, lifting bottom-stacked UI
  // (batch toolbar, toasts) for no real bar at the bottom. Codex
  // critical-change review.
  const isBarDockedBottom =
    shouldShow &&
    (isMobile || (isDockedDesktopRoute && !shouldUseLiftedPosition));
  const barDockOffset = isMobile
    ? // Mobile: always clear the dock; clear the bar instead when the
      // bar is inline at bottom (it stacks over the dock).
      isBarDockedBottom
      ? MOBILE_BAR_HEIGHT
      : MOBILE_DOCK_HEIGHT
    : // Desktop: clear the bar only when it's docked at the bottom.
      isBarDockedBottom
      ? BAR_HEIGHT
      : "0px";

  return (
    <>
      {/* Global CSS var — surfaces that stack above the bar (batch
          toolbar, future toasts anchored to the bottom) read this. */}
      <style>{`:root { --bar-dock-offset: ${barDockOffset}; }`}</style>
      {/*
       * Bar notification — global toast surface, decoupled from the bar's
       * position. Earlier this lived as a satellite of the bar (`top:
       * barTop` + same translateY chain), which meant on FAB-only routes
       * (memory detail, agents, inbox — anywhere the bar is hidden until
       * tapped) notifications were silently swallowed: the notif's
       * visibility was gated on `barVisible`, so when the bar was off-
       * screen the notif was too.
       *
       * Now: bottom-anchored on every surface. Desktop sits 24px from
       * the viewport bottom; mobile sits above the mobile dock-bar's
       * rest position (MOBILE_DOCK_BOTTOM_GAP_PX + MOBILE_BAR_HEIGHT +
       * 16 + safe-area), so the notif clears the bar's vertical band
       * with a 16px breathing gap. Visibility is purely
       * `activeNotification` truthy + a single mobile-fullscreen-compose
       * escape hatch (compose overlay covers everything; suppressing
       * the notif there avoids stacking two surfaces on a small screen).
       * No more bar-coupled gates.
       *
       * Z-index: --z-bar-notif (51) sits below --z-modal (60) and below
       * --z-bar (52). Mobile bar at rest occupies bottom band [68 …
       * 68 + 72] + safe-area, and the notif's offset above clears that
       * band, so the layering order doesn't matter for visibility —
       * they no longer overlap geographically. Modal/popover/
       * takeover/toast still win above when those surfaces open.
       *
       * The `--notif-outer` CSS var still drives the card's outer
       * height (44px). The legacy `--notif-content` / `--notif-tuck`
       * tokens are no longer applied in this component (the inner
       * row uses `h-full items-center` so content centers in the full
       * outer); they remain defined in globals.css + notification-
       * glass.css because the kitchen demo and design-system docs
       * still reference them, but no production code reads them.
       */}
      {(() => {
        // Single visibility boolean drives aria-hidden + opacity +
        // pointerEvents in lockstep. Earlier the aria-hidden gate only
        // checked `!activeNotification`, so during mobile fullscreen
        // compose the notif was visually suppressed (opacity 0,
        // pointer-events none) but still announced to screen readers
        // — codex P2 finding.
        const notifVisible = Boolean(
          activeNotification &&
          !(isMobile && mobileComposeState === "fullscreen"),
        );
        // Mobile bottom placement: bottom-edge toast pattern (like
        // Material snackbar). The mobile bar floats with a
        // MOBILE_DOCK_BOTTOM_GAP_PX (68px) bottom gap, so the band
        // BELOW the bar [0 … 68px from viewport bottom] is empty.
        // A 44px notif at bottom:16+safe-area leaves an 8px gap to
        // the bar's bottom edge and a 16px gap to the viewport edge
        // (cleared via safe-area-inset on iOS home-indicator phones).
        //
        // Prior to this, the notif sat ABOVE the bar at bottom ≈ 156px
        // — well into the middle of a 700px phone, visually mid-screen
        // instead of mid-bottom (codex P1 fix overcorrected from an
        // earlier "inside the bar band" bug to "above the bar band"
        // and skipped the cleaner option of "below the bar band").
        return (
          <div
            ref={barNotifRef}
            aria-hidden={!notifVisible}
            className="fixed left-1/2 z-bar-notif w-[calc(100%-32px)] sm:w-[calc(100%-96px)] md:w-[calc(100%-128px)] max-w-[640px]"
            style={{
              // Bottom-anchored on every surface. Desktop: 24px from
              // the viewport bottom. Mobile: 16px + safe-area, sitting
              // below the floating bar (which is 68px above edge), so
              // notif and bar visually stack near the bottom of the
              // viewport without overlap.
              bottom: isMobile ? `calc(16px + var(--safe-bottom, 0px))` : 24,
              transform: "translateX(-50%)",
              transition: `opacity ${FAST}s ${easing}`,
              opacity: notifVisible ? 1 : 0,
              pointerEvents: notifVisible ? "auto" : "none",
            }}
          >
            <BarNotificationCard />
          </div>
        );
      })()}
      {/*
       * Bar container — ALWAYS MOUNTED.
       *
       * Architecture (industry standard: Spotlight / Raycast / Linear):
       * - Outer div: positioning layer (CSS only — left-1/2, bottom/top, -translate-x-1/2)
       * - Inner motion.div: animation layer (opacity + translateY for show/hide)
       *
       * Centering and animation live on separate elements so framer motion's
       * transform never conflicts with the CSS centering transform.
       * When hidden: pointer-events-none, opacity 0, shifted down 24px.
       */}
      <div
        ref={barRef}
        inert={barMuted || undefined}
        aria-hidden={barMuted || undefined}
        className={`fixed left-1/2 z-bar w-[calc(100%-32px)] sm:w-[calc(100%-96px)] md:w-[calc(100%-128px)] max-w-[640px]${
          // Mobile intentionally forgoes glass material on the bar
          // (mobile-bar-surface.tsx perf rule — full-viewport
          // backdrop-filter passes are expensive on iOS Safari —
          // and the mobile bar design is a flat MobileThumbBarRow,
          // not a glass pill). Gating these classes is what prevents
          // a rectangular blur blob from rendering around the mobile
          // bar: on desktop the glass fill + 20px radius mask the
          // backdrop-filter rectangle as a rounded pill; on mobile,
          // with `background: transparent`, `border: none`, and
          // `borderRadius: 0` set via inline style, an active
          // backdrop-filter on this element becomes a visible
          // rectangular blur over page content. Belt-and-suspenders
          // `backdropFilter: "none"` is also applied below so future
          // cascade changes can't reintroduce the bug silently.
          // Skip the glass-bar material when the bar isn't supposed to
          // be visible on this route — otherwise the floating fixed
          // div paints a blurry blank strip at the bottom on routes
          // like /agents (view==="none" with no overlay open). Plan
          // 26 follow-up.
          isMobile || !shouldShow ? "" : " glass-bar backdrop-blur-sm"
        }`}
        style={{
          top: barTop,
          // All bar transforms composed onto THIS element — horizontal
          // centering, vertical anchor, and scroll-hide offset — so no
          // ancestor transform creates a compositing layer that would
          // trap this element's own `backdrop-filter`. The glass only
          // samples page content when backdrop-filter lives on the
          // outermost fixed element with no transformed ancestors.
          transform: `translateX(-50%) translateY(${barTranslateY}) translateY(${scrollHideTranslate})`,
          // Mobile keeps `top` instant (route/tab-switch stability for
          // mirror/fullscreen) but still animates `transform` so
          // scroll-hide slides smoothly. Desktop animates both.
          transition: isMobile
            ? `transform ${NORMAL}s ${easing}`
            : `top ${barEnterDuration} ${barEnterEase}, transform ${NORMAL}s ${easing}, box-shadow ${FAST}s ${easing}, border ${FAST}s ${easing}, border-radius ${NORMAL}s ${easing}`,
          // Muted bar still captures pointer events so taps don't fall
          // through to rows beneath (which would mutate selection on the
          // surface hosting the batch toolbar). `inert` disables focus /
          // activation inside the bar; the outer shell just consumes
          // hits quietly. Fully hidden (not muted) = pointer-events none.
          pointerEvents:
            barVisible &&
            !(isMobile && mobileComposeState === "fullscreen") &&
            !isScrollHidden
              ? "auto"
              : "none",
          // Glass chrome: desktop only. The `.glass-bar` class owns
          // the material + inner rim; inline overrides apply
          // focus/drag state shadows on top. Mobile forgoes glass
          // entirely (see className gating above) and explicitly
          // zeroes out backdrop-filter as defense-in-depth so the
          // rectangular blur-blob bug can't reappear via a future
          // class addition, Tailwind @layer reshuffle, or cascade
          // specificity change.
          background: isMobile ? "transparent" : undefined,
          backdropFilter: isMobile ? "none" : undefined,
          WebkitBackdropFilter: isMobile ? "none" : undefined,
          // Drag-target border (`2px dashed signature`) only when the
          // bar is actually visible — otherwise the dashed purple
          // ring paints on the invisible outer div on FAB-state
          // routes (memory, agents, etc.) creating a "ghost" rectangle
          // mid-air during memory-to-bar drag. Plan 26 follow-up.
          border:
            isDragging && shouldShow
              ? "2px dashed var(--signature)"
              : isMobile
                ? "none"
                : undefined,
          // boxShadow + borderRadius only when the bar is actually
          // showing (or in mobile flat mode) — otherwise the outer div
          // would paint a rounded rim + ambient shadow on routes where
          // the bar isn't supposed to be visible (e.g., /agents),
          // creating a phantom strip even with the glass material
          // gated. Plan 26 follow-up.
          boxShadow: !shouldShow
            ? "none"
            : isDragging
              ? "inset 0 1px 0 var(--glass-inset-highlight), 0 0 0 6px oklch(from var(--signature) l c h / 0.06)"
              : isMobile
                ? "none"
                : barFocused
                  ? "inset 0 1px 0 var(--glass-inset-highlight), 0 16px 40px oklch(0 0 0 / 0.12), 0 4px 12px oklch(0 0 0 / 0.08), 0 0 0 1px oklch(from var(--foreground) l c h / 0.08)"
                  : "inset 0 1px 0 var(--glass-inset-highlight), 0 8px 24px oklch(0 0 0 / 0.07), 0 2px 6px oklch(0 0 0 / 0.04)",
          borderRadius:
            isMobile || !shouldShow ? 0 : "var(--app-radius-surface)",
        }}
      >
        {/* Inner wrapper — no transform, no will-change, so the outer
            div's backdrop-filter samples page content unobstructed.
            Opacity-only animation on the show/hide motion.div preserves
            that. */}
        <div>
          <motion.div
            className="flex flex-col items-stretch"
            initial={false}
            // Opacity-only (no `y`): any `transform` on a backdrop-filter
            // ancestor — even at a rest value like translateY(0px) —
            // promotes the element to its own compositing layer in
            // Chromium, which causes `.glass-bar`'s backdrop-filter to
            // sample only within that layer (empty) instead of the page
            // below. The visible effect is a bar with no blur. Keeping
            // this fade opacity-only lets the glass actually sample
            // page content.
            animate={{
              opacity:
                shouldShow &&
                barVisible &&
                !(isMobile && mobileComposeState === "fullscreen") &&
                !isScrollHidden
                  ? barMuted
                    ? 0.45
                    : 1
                  : 0,
            }}
            // Mobile: no fade. Desktop: snappy spring matching the
            // ScanRestButton FAB so the two surfaces feel like one
            // morph (~120ms settle, no overshoot). Plan 26 critical
            // change — earlier 200ms iOS-curve fade read as sluggish
            // for a chrome morph.
            //
            // Chat-surface carry-over: when route flips to /brain and
            // shouldShow goes false, the prior-route opacity was 1.
            // A tween from 1 → 0 paints the bar over the chat composer
            // for the FAST duration. Force duration 0 here so the bar
            // disappears instantly. The escape-hatch path (⌘K opens
            // the overlay on /brain → shouldShow becomes true) still
            // animates normally because shouldShow is true in that
            // branch (codex review pass 1).
            transition={
              onChatSurface && !shouldShow
                ? { duration: 0 }
                : isMobile
                  ? { duration: 0 }
                  : {
                      type: "spring",
                      stiffness: 700,
                      damping: 38,
                      mass: 0.4,
                    }
            }
          >
            {/* Notification card moved OUT of the bar's fixed container — see
              the new sibling element above this bar's wrapper. Rendering it
              as a child painted its tuck region ON TOP of the bar's glass
              instead of behind it. The sibling at z-bar-notif sits below the
              bar's z-bar so the glass overlaps the 14px tuck correctly. */}

            {/* Mode toggle now lives inside the bar's logo slot as ModeCapsule
              (kitchen 31). Floating pills removed — see bar-logo-portal.tsx. */}

            {/* The bar */}
            <motion.div
              initial={isMobile ? false : { opacity: 0 }}
              animate={{ opacity: shouldShow && barVisible ? 1 : 0 }}
              // Same carry-over guard as the outer motion.div: when the
              // route flips to /brain and shouldShow goes false, kill
              // the FAST tween so the inner bar doesn't paint briefly
              // over the chat composer. ⌘K-opened overlay path still
              // animates normally (shouldShow=true in that branch).
              transition={
                onChatSurface && !shouldShow
                  ? { duration: 0 }
                  : isMobile
                    ? { duration: 0 }
                    : { duration: FAST }
              }
            >
              {/* Inner chrome — transparent wrapper whose only job is
                  to clip the expand surface to the bar's rounded
                  shape. Glass, border, shadow, and radius all live on
                  the outer fixed div (so backdrop-filter works); this
                  div inherits radius via border-radius: inherit and
                  applies overflow-hidden on desktop. */}
              <div
                className={`${
                  isMobile
                    ? "overflow-visible"
                    : shouldUseLiftedPosition
                      ? "overflow-hidden max-h-[calc(100vh-24vh-16px)]"
                      : "overflow-hidden max-h-[80vh]"
                }`}
                style={{
                  borderRadius: "inherit",
                }}
              >
                <div className="flex flex-col">
                  {isMobile ? (
                    <MobileThumbBarShell
                      chips={
                        <div id="bar-chip-slot" className="flex flex-col" />
                      }
                      row={
                        <MobileThumbBarRow
                          value={value}
                          placeholder={
                            stagedFiles.length > 0
                              ? t.remember.hintPlaceholder
                              : t.bar.placeholder.default
                          }
                          inputRef={inputRef}
                          onFocus={openMobileComposeActive}
                          onChange={handleChange}
                          onKeyDown={handleKeyDown}
                          hasText={hasText}
                          hasFiles={hasFiles}
                          isExpanded={isBarExpanded}
                          showClose={mobileComposeActive}
                          onClose={peelBack}
                          onExpand={openMobileCompose}
                          onAddFiles={addStagedFiles}
                          sendKind={sendKind}
                          sendState={sendState}
                          onSend={handleSubmit}
                        />
                      }
                      input={null}
                      actions={null}
                    />
                  ) : (
                    <>
                      <div id="bar-chip-slot" className="flex flex-col" />
                      <div
                        className="relative min-h-14 px-4 py-2 sm:px-6"
                        style={{
                          display: "grid",
                          gridTemplateAreas: isBarExpanded
                            ? '"input input input" "logo . send"'
                            : '"logo input send"',
                          gridTemplateColumns: "auto 1fr auto",
                          alignItems: "center",
                          gap: isBarExpanded ? "4px 8px" : "0 8px",
                          transition: "gap 0.2s cubic-bezier(0.32, 0.72, 0, 1)",
                        }}
                      >
                        <div
                          id="bar-logo-slot"
                          className="shrink-0"
                          style={{ gridArea: "logo" }}
                        />
                        <div
                          className="min-w-0 py-1"
                          id="bar-input-slot"
                          style={{ gridArea: "input" }}
                        />
                        <div
                          id="bar-right-slot"
                          className="shrink-0"
                          style={{ gridArea: "send" }}
                        />
                      </div>

                      <motion.div
                        initial={false}
                        animate={{
                          opacity: hasExpandSurface ? 1 : 0,
                          y: hasExpandSurface ? 0 : -6,
                          height: hasExpandSurface ? "auto" : 0,
                        }}
                        transition={{ duration: FAST, ease: EASE }}
                        className="overflow-hidden"
                      >
                        <div
                          id="bar-expand-slot"
                          className="flex flex-col overflow-hidden"
                        />
                      </motion.div>
                    </>
                  )}
                </div>
              </div>
            </motion.div>
          </motion.div>
        </div>
      </div>

      {/* Bar portals — always rendered by layout */}
      <BarLogoPortal />
      <BarChipPortal />
      <BarInputPortal />
      <BarRightPortal />
      <BarExpandPortal />
      {isMobile && <MobileBarSurface />}
      {isMobile && <MobileComposeOverlay />}
    </>
  );
}

/**
 * V2ChromeWrap — wraps route children in shell-v2 chrome.
 *
 * Mounts <ShellStateProvider> (rail collapsed + secondaryHidden state)
 * and <ShellLayoutV2> (LeftRail + PinnedSecondaryPanel + ScanRestButton +
 * page-bg aurora). The active tab is resolved from the URL via
 * getShellTabForPath; routes that don't map to any tab return null and
 * the rail renders no active state.
 *
 * Mobile-only legacy floating chrome (BrandMark, top-right hub chip,
 * MobileDock, TopicTreePanelOverlayHost) is mounted directly by
 * <AppShell> on `isMobile` until plan 22 ships v2 mobile chrome.
 */
function V2ChromeWrap({
  pathname,
  children,
}: {
  pathname: string;
  children: React.ReactNode;
}) {
  // tab is `ShellTabId | null` — null on routes that don't map to a
  // shell tab (e.g., /, /login, future /settings). The rail renders
  // no active state in that case rather than incorrectly highlighting
  // Memories. ShellLayoutV2 + LeftRail accept null directly.
  const tab = getShellTabForPath(pathname);
  // ShellStateProvider is mounted higher up (above GlobalBar) so the bar
  // can consume `barScrollHidden` from the same source as the rail and
  // ScanRestButton. Plan 26 phase 4.
  return <ShellLayoutV2 tab={tab}>{children}</ShellLayoutV2>;
}
