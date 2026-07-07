"use client";

/**
 * `<HubSwitcherBottomSheet>` — plan 22 §5.2 (P3-2).
 *
 * Bottom-sheet wrapping `<HubSwitcherMenu>` for the mobile top bar's
 * hub-identity chevron tap. The menu owns the actual switch logic
 * (hub list, switchHub, navigation, surface-transition, bar
 * notification) — this component is glue + the "Create hub" portal
 * trigger that desktop's `<HubIdentityChip>` also wires.
 *
 * Open state is owned by the caller (ShellLayoutMobile holds it
 * alongside the drawer state). Selecting a hub closes the sheet via
 * `onSelect`; tapping "Create hub" opens HubCreateDialog as a
 * portal-mounted modal layered above the sheet so the sheet can stay
 * out of the body-scroll lock contention.
 */

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useRouter, usePathname } from "next/navigation";
import { BottomSheet } from "@memaxlabs/ui";
import { HubSwitcherMenu } from "@/components/features/hub/hub-switcher-menu";
import { HubCreateDialog } from "@/components/features/settings/hub-create-dialog";
import { useAuth } from "@/lib/auth";
import { useBar } from "@/contexts/bar-context";
import { useLocale } from "@/i18n";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";
import { startLiveSurfaceTransition } from "@/lib/recent-navigation";
import { warmHubLandingRoute } from "@/lib/hub-route-readiness";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { buildMemoriesPath } from "@/lib/route-helpers";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";

interface HubSwitcherBottomSheetProps {
  open: boolean;
  onClose: () => void;
}

export function HubSwitcherBottomSheet({
  open,
  onClose,
}: HubSwitcherBottomSheetProps) {
  const { t } = useLocale();
  const router = useRouter();
  const pathname = usePathname();
  const { user, switchHub } = useAuth();
  const { setBarNotification } = useBar();
  const [showCreate, setShowCreate] = useState(false);
  const [mounted, setMounted] = useState(false);
  const createBackdropRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setMounted(true);
  }, []);

  useEffect(() => {
    if (!showCreate) return;
    const release = acquireBodyScrollLock();
    const el = createBackdropRef.current;
    const onWheel = (e: WheelEvent) => e.preventDefault();
    if (el) el.addEventListener("wheel", onWheel, { passive: false });
    return () => {
      release();
      if (el) el.removeEventListener("wheel", onWheel);
    };
  }, [showCreate]);

  return (
    <>
      <BottomSheet
        open={open}
        onClose={onClose}
        ariaLabel={t.hubs.switchTitle}
        title={t.hubs.switchTitle}
      >
        <HubSwitcherMenu
          onSelect={onClose}
          onCreateHub={() => setShowCreate(true)}
        />
      </BottomSheet>

      {showCreate &&
        mounted &&
        createPortal(
          <>
            <div
              ref={createBackdropRef}
              className="fixed inset-0 z-takeover"
              style={{
                background: "rgba(0,0,0,0.4)",
                touchAction: "none",
                overscrollBehavior: "contain",
              }}
              onClick={() => setShowCreate(false)}
              onTouchMove={(e) => e.preventDefault()}
            />
            <div className="fixed inset-0 z-takeover flex items-center justify-center pointer-events-none p-6">
              <div className="pointer-events-auto w-full max-w-md">
                <HubCreateDialog
                  onCreated={(createdHub) => {
                    // Mirror the desktop HubIdentityChip create flow so
                    // mobile users land on the new hub instead of the
                    // create modal closing into the previous hub. Uses
                    // the SDK response directly (the hubs array is
                    // stale until refreshProfile + next render).
                    setShowCreate(false);
                    onClose();
                    const viewerIdentity = user
                      ? { name: user.name, displayName: user.display_name }
                      : undefined;
                    const createdLabel = getHubDisplayName(
                      createdHub,
                      t,
                      viewerIdentity,
                    );
                    const createdBadge = getHubDisplayInitial(
                      createdHub,
                      t,
                      viewerIdentity,
                    );
                    const createdKind =
                      createdHub.hub_type === "team" ? "team" : "personal";
                    const warmPromise = warmHubLandingRoute(
                      "/memories",
                      createdHub.id,
                    );
                    startLiveSurfaceTransition({
                      kind: "hub-switch",
                      hubName: createdLabel,
                      hubBadgeLabel: createdBadge,
                      hubAccent: createdHub.accent,
                      hubKind: createdKind,
                      minDurationMs: 480,
                      maxDurationMs: 2500,
                      waitFor: warmPromise,
                    });
                    switchHub(createdHub.id);
                    // Always navigate to the created hub's v2 slug —
                    // the "/memories" fallback bounced through the
                    // middleware rewrite to /h/personal/... and
                    // silently reverted the switch to personal.
                    const destination = user
                      ? buildMemoriesPath(hubRouteSlug(createdHub, user.id))
                      : "/memories";
                    router.push(destination);
                    setBarNotification({
                      type: "success",
                      message:
                        t.hubs.switchedTo?.replace("{name}", createdLabel) ??
                        `Switched to ${createdLabel}`,
                      autoDismissMs: 2000,
                      onDismiss: () => setBarNotification(null),
                    });
                  }}
                  onCancel={() => setShowCreate(false)}
                />
              </div>
            </div>
          </>,
          document.body,
        )}
    </>
  );
}
