"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ChevronDown } from "lucide-react";
import { useRouter, usePathname } from "next/navigation";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { buildMemoriesPath, getHubSlugForPath } from "@/lib/route-helpers";
import {
  pillClass,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@memaxlabs/ui";
import { HubSwitcherMenu } from "@/components/features/hub/hub-switcher-menu";
import { HubBadge } from "@/components/features/hub/hub-badge";
import { HubCreateDialog } from "@/components/features/settings/hub-create-dialog";
import { useAuth } from "@/lib/auth";
import { useBar } from "@/contexts/bar-context";
import { useLocale } from "@/i18n";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";
import { startLiveSurfaceTransition } from "@/lib/recent-navigation";
import { warmHubLandingRoute } from "@/lib/hub-route-readiness";

interface HubIdentityChipProps {
  kind: "personal" | "team";
  name: string;
  icon?: string;
  accent?: string;
  role?: string;
  variant?: "standalone" | "embedded";
  viewerName?: string;
  viewerDisplayName?: string;
}

function HubRoleTag({ role }: { role: string }) {
  const { t } = useLocale();
  const normalized = role.toLowerCase();
  const label =
    normalized === "owner"
      ? t.hubs.owner
      : normalized === "admin"
        ? t.hubs.admin
        : role;

  return (
    <span className="rounded-chrome bg-surface-2 px-1.5 py-0.5 text-[10px] font-medium text-fg-3">
      {label}
    </span>
  );
}

export function HubIdentityChip({
  kind,
  name,
  icon,
  accent,
  role,
  variant = "standalone",
  viewerName,
  viewerDisplayName,
}: HubIdentityChipProps) {
  const { t } = useLocale();
  const router = useRouter();
  const pathname = usePathname();
  const { user, switchHub } = useAuth();
  const { setBarNotification } = useBar();
  const [open, setOpen] = useState(false);
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

  const label = getHubDisplayName(
    { name, icon, hub_type: kind },
    t,
    kind === "personal"
      ? { name: viewerName, displayName: viewerDisplayName }
      : undefined,
  );
  const badgeLabel = getHubDisplayInitial(
    { name, icon, hub_type: kind },
    t,
    kind === "personal"
      ? { name: viewerName, displayName: viewerDisplayName }
      : undefined,
  );
  const isEmbedded = variant === "embedded";
  // Standalone trigger uses .glass-pill — the canonical glass material for
  // small interactive surfaces (toggle buttons, capsules, floating controls;
  // see globals.css:674). Matches the same glass family as the bar and the
  // topic explorer. The HubBadge inside keeps its accent color so the hub
  // identity still reads as a distinct affordance; chrome stays neutral
  // (Linear/Notion pattern: brand colour lives on the inner affordance,
  // never on the surface).
  const triggerClass = isEmbedded
    ? "inline-flex items-center gap-1.5 rounded-full px-1 py-0.5 text-[12px] text-fg-2 transition-colors hover:text-fg-1 cursor-pointer"
    : pillClass({
        variant: "select",
        size: "lg",
        // `rounded-full` capsule for the hub dropdown trigger (more
        // curved than the previous rounded-card 20px per user's
        // "more curved" feedback). The chip is short + wide enough
        // that fully-rounded reads as a clean pill rather than an
        // overshoot. Other 20px-curve surfaces (memory cards, hub
        // dropdown popover, logo hover affordance) keep rounded-card
        // because they're tall enough that a full-round would warp.
        className: "max-w-full glass-pill rounded-full",
      });

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger className={triggerClass}>
        <HubBadge
          kind={kind}
          label={badgeLabel}
          accent={accent}
          size={isEmbedded ? "md" : "lg"}
        />
        <span className="truncate">{label}</span>
        {role && <HubRoleTag role={role} />}
        <ChevronDown
          className={`shrink-0 text-fg-4 ${isEmbedded ? "h-3 w-3" : "h-4 w-4"}`}
        />
      </PopoverTrigger>
      <PopoverContent side="bottom" align="start" sideOffset={6}>
        <div className="w-60 p-1">
          <HubSwitcherMenu
            onSelect={() => setOpen(false)}
            onCreateHub={() => setShowCreate(true)}
          />
        </div>
      </PopoverContent>

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
                    setShowCreate(false);
                    // Use the SDK response directly rather than looking
                    // it up in `hubs` — that closure is stale until the
                    // next render after refreshProfile, so a lookup
                    // here would fall back to the previous hub's name
                    // and icon and flash during the transition.
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
                    // Match the new hub's URL when on a v2 hub-scoped
                    // route. Falls through to the v1 overview otherwise.
                    const onV2Route = getHubSlugForPath(pathname) !== null;
                    const destination =
                      onV2Route && user
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
    </Popover>
  );
}
