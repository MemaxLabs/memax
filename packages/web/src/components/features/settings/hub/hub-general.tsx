"use client";

import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { HubIdentityEditor } from "@/components/features/hub/hub-identity-editor";
import { Section } from "../shared/section";

export function HubGeneral({ hubId }: { hubId: string }) {
  const { hubs, user, refreshProfile } = useAuth();
  const { t } = useLocale();

  const hubWithRole = hubs.find((h) => h.hub.id === hubId);
  if (!hubWithRole) return null;

  const { hub, role } = hubWithRole;
  const isPersonal = hub.hub_type === "personal";
  const canEdit = role === "owner" || role === "admin";

  return (
    <Section>
      <HubIdentityEditor
        hub={hub}
        canEdit={canEdit}
        viewerName={isPersonal ? user?.name : undefined}
        viewerDisplayName={isPersonal ? user?.display_name : undefined}
        badge={
          <span className="shrink-0 rounded-full bg-surface-2 px-2 py-0.5 text-[12px] text-fg-3">
            {isPersonal
              ? (t.userSettings?.personalHub ?? "Personal hub")
              : (t.hubs?.team ?? "Team hub")}
          </span>
        }
        subtitle={
          isPersonal
            ? (t.userSettings?.personalHubDesc ?? undefined)
            : undefined
        }
        onUpdated={refreshProfile}
      />
    </Section>
  );
}
