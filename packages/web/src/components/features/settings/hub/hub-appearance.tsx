"use client";

import { useAuth } from "@/lib/auth";
import { useLocale, useInterpolate } from "@/i18n";
import { useSettings, useUpdateSettings } from "@/hooks/use-settings";
import { useUpdateHub } from "@/hooks/use-hub-management";
import { HubHeaderBanner, type HubHeaderAuroraMode } from "@memaxlabs/ui";
import { getHubDisplayInitial } from "@/lib/hub-display";
import { Section, SelectionOption } from "../shared/section";
import type { HubHeaderAuroraMode as SDKHubHeaderAuroraMode } from "memax-sdk";

/**
 * Hub > Appearance — aurora mode for the hub header banner.
 *
 * Personal hubs read from user settings (1 user ↔ 1 personal hub, so the
 * user-level key maps cleanly to the hub). Team hubs store their own
 * per-hub value on the hub row; only owners/admins can change it, and the
 * server rejects writes from other roles.
 */
export function HubAppearance({ hubId }: { hubId: string }) {
  const { hubs, user } = useAuth();
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { data: settings } = useSettings();
  const updateSettings = useUpdateSettings();
  const updateHub = useUpdateHub();

  const hubWithRole = hubs.find((h) => h.hub.id === hubId);
  if (!hubWithRole) return null;

  const { hub, role } = hubWithRole;
  const isPersonal = hub.hub_type === "personal";
  const canEdit = isPersonal || role === "owner" || role === "admin";

  const personalMode =
    (settings?.hub_header_aurora_mode as HubHeaderAuroraMode | undefined) ??
    "signature";
  const teamMode = (hub.header_aurora_mode ??
    "signature") as HubHeaderAuroraMode;
  const hubHeaderMode: HubHeaderAuroraMode = isPersonal
    ? personalMode
    : teamMode;

  const handleSelect = (value: HubHeaderAuroraMode) => {
    if (!canEdit) return;
    if (isPersonal) {
      updateSettings.mutate({ hub_header_aurora_mode: value });
    } else {
      updateHub.mutate({
        hubId: hub.id,
        header_aurora_mode: value as SDKHubHeaderAuroraMode,
      });
    }
  };

  return (
    <Section title={t.userSettings.hubHeaderMode}>
      <div className="space-y-1">
        <div className="px-1 pb-1">
          <p className="mt-0.5 text-[13px] text-fg-3">
            {isPersonal
              ? t.userSettings.hubHeaderModeDesc
              : canEdit
                ? (t.userSettings.hubHeaderModeTeamDesc ??
                  t.userSettings.hubHeaderModeDesc)
                : (t.userSettings.hubHeaderModeTeamViewerDesc ??
                  t.userSettings.hubHeaderModeDesc)}
          </p>
        </div>
        {(
          [
            {
              value: "none",
              label: t.userSettings.hubHeaderModeNone,
              desc: t.userSettings.hubHeaderModeNoneDesc,
            },
            {
              value: "signature",
              label: t.userSettings.hubHeaderModeSignature,
              desc: t.userSettings.hubHeaderModeSignatureDesc,
            },
            {
              value: "time",
              label: t.userSettings.hubHeaderModeTime,
              desc: t.userSettings.hubHeaderModeTimeDesc,
            },
          ] as const
        ).map((option) => (
          <SelectionOption
            key={option.value}
            label={option.label}
            sublabel={option.desc}
            checked={hubHeaderMode === option.value}
            disabled={!canEdit}
            onSelect={() => handleSelect(option.value)}
          />
        ))}
      </div>
      <div className="mt-4 overflow-hidden rounded-surface border border-border/40">
        <HubHeaderBanner
          hubName={hub.name}
          hubType={hub.hub_type as "personal" | "team"}
          hubAccent={hub.accent}
          avatarLabel={getHubDisplayInitial(hub, t)}
          greeting={interpolate(t.hubHeader.greeting.morningDreamA, {
            name: user?.display_name || user?.name || "",
            n: "4",
          })}
          timeBucket="morning"
          statsLine={
            <div className="flex flex-wrap items-center gap-x-2 text-[12px] text-fg-3">
              <span className="tabular-nums">
                {interpolate(t.topics.memories, { n: "247" })}
              </span>
              <span className="text-fg-4">·</span>
              <span className="tabular-nums">
                {interpolate(t.topics.topics, { n: "12" })}
              </span>
            </div>
          }
          aurora={hubHeaderMode}
          compact
        />
      </div>
    </Section>
  );
}
