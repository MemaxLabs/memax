"use client";

import { useState } from "react";
import { Check, Plus } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { MenuItem } from "@memaxlabs/ui";
import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { useHubs } from "@/hooks/use-hubs";
import { HubBadge } from "@/components/features/hub/hub-badge";
import { startLiveSurfaceTransition } from "@/lib/recent-navigation";
import { warmHubLandingRoute } from "@/lib/hub-route-readiness";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { buildHubSwitchPath, isPulseRoute } from "@/lib/route-helpers";

interface HubSwitcherMenuProps {
  onSelect?: () => void;
  onCreateHub?: () => void;
  className?: string;
}

function PersonalTag() {
  const { t } = useLocale();
  return (
    <span className="shrink-0 rounded-md bg-surface-2 px-1.5 py-0.5 text-[10px] font-medium text-fg-3">
      {t.hubs.personal}
    </span>
  );
}

export function HubSwitcherMenu({
  onSelect,
  onCreateHub,
  className = "",
}: HubSwitcherMenuProps) {
  const { user, hubs, activeHubId, switchHub } = useAuth();
  const router = useRouter();
  const pathname = usePathname();
  const { t } = useLocale();
  const { data: liveHubs } = useHubs();
  const [pendingHubId, setPendingHubId] = useState<string | null>(null);

  const hubList = liveHubs ?? hubs;

  return (
    <div className={className}>
      {hubList.map(({ hub, memory_count }) => {
        const isActive = hub.id === activeHubId;
        const label = getHubDisplayName(
          hub,
          t,
          user
            ? { name: user.name, displayName: user.display_name }
            : undefined,
        );
        const badgeLabel = getHubDisplayInitial(
          hub,
          t,
          user
            ? { name: user.name, displayName: user.display_name }
            : undefined,
        );
        const kind = hub.hub_type === "team" ? "team" : "personal";
        const showPersonalTag = kind === "personal";
        return (
          <button
            key={hub.id}
            disabled={pendingHubId !== null}
            onClick={async () => {
              if (pendingHubId) return;
              if (isActive) {
                onSelect?.();
                return;
              }
              setPendingHubId(hub.id);
              // Surface-preserving switch: on pulse, warm + land on
              // the TARGET hub's pulse; everywhere else the target
              // hub's memories overview (topic/memory detail ids
              // belong to the origin hub and don't exist over there).
              const warmPromise = warmHubLandingRoute(
                isPulseRoute(pathname) ? "/pulse" : "/memories",
                hub.id,
              );
              startLiveSurfaceTransition({
                kind: "hub-switch",
                hubName: label,
                hubBadgeLabel: badgeLabel,
                hubAccent: hub.accent,
                hubKind: kind,
                minDurationMs: 480,
                maxDurationMs: 2500,
                waitFor: warmPromise,
              });
              switchHub(hub.id);
              // Always navigate to the TARGET hub's v2 slug. The old
              // "/memories" fallback for non-v2 routes bounced through
              // the middleware rewrite to /h/personal/..., which made
              // useV2RouteHubSync silently revert a team-hub selection
              // back to personal.
              const destination = user
                ? buildHubSwitchPath(pathname, hubRouteSlug(hub, user.id))
                : "/memories";
              router.push(destination);
              // No success toast: the transition overlay already names
              // the destination hub, and the hub header IS the
              // confirmation. Firing a "Switched" toast here raced the
              // "Switching…" overlay — both were visible at once.
              setPendingHubId(null);
              onSelect?.();
            }}
            className={`flex w-full gap-3 rounded-lg px-3 py-3 text-[15px] transition-colors cursor-pointer ${
              isActive
                ? "bg-surface-2 font-medium text-foreground"
                : "text-fg-2 hover:bg-surface-1 hover:text-fg-2"
            } ${pendingHubId ? "pointer-events-none opacity-60" : ""}`}
          >
            <div className="mt-0.5 shrink-0">
              <HubBadge kind={kind} label={badgeLabel} accent={hub.accent} />
            </div>
            <div className="min-w-0 flex-1 text-left">
              {/* Every row shows its memory count — same metadata for
                  active and inactive hubs (the old "Show X's recent"
                  hint line read as mystery-meat and hid the count). */}
              <div className="flex items-center gap-2">
                <span className="flex-1 truncate">{label}</span>
                {showPersonalTag && <PersonalTag />}
                <span className="shrink-0 text-[12px] tabular-nums text-fg-4">
                  {memory_count ?? 0}
                </span>
                {isActive && <Check className="h-3 w-3 text-fg-3" />}
              </div>
            </div>
          </button>
        );
      })}
      <MenuItem
        onClick={() => {
          onSelect?.();
          onCreateHub?.();
        }}
        icon={<Plus className="h-3 w-3" />}
      >
        {t.hubs.createTeam}
      </MenuItem>
    </div>
  );
}
