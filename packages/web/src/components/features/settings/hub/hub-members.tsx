"use client";

import { ArrowRight, Bot, Users } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { useLocale, useInterpolate } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import {
  useHubDetail,
  useHubInvites,
  useUpdateMemberRole,
  useRemoveMember,
  useCreateInvite,
  useRevokeInvite,
  useRegenerateInvite,
} from "@/hooks/use-hub-management";
import { useApiKeys } from "@/hooks/use-api-keys";
import { HubMembersSection } from "@/components/features/teams/hub-members-section";
import { HubInvitesSection } from "@/components/features/teams/hub-invites-section";
import {
  ROLE_ORDER,
  Section,
  isExpired,
  type HubRole,
} from "@/components/features/teams/shared";
import { DataCardSkeleton, DataSectionCard, Surface } from "@memaxlabs/ui";

export function HubMembers({
  hubId,
  onNavigateToKeys,
}: {
  hubId: string;
  onNavigateToKeys: () => void;
}) {
  const { user, hubs } = useAuth();
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const hubWithRole = hubs.find((h) => h.hub.id === hubId);
  const myRole = (hubWithRole?.role ?? "viewer") as HubRole;
  const canManage = myRole === "owner" || myRole === "admin";

  const { data: hubDetail, isLoading, isError, refetch } = useHubDetail(hubId);
  const { data: invites } = useHubInvites(hubId, canManage);
  const { data: apiKeys } = useApiKeys();

  const updateMemberRole = useUpdateMemberRole();
  const removeMember = useRemoveMember();
  const createInvite = useCreateInvite();
  const revokeInvite = useRevokeInvite();
  const regenerateInvite = useRegenerateInvite();

  const members = hubDetail?.members ?? [];
  const sortedMembers = [...members].sort(
    (a, b) => ROLE_ORDER.indexOf(a.role) - ROLE_ORDER.indexOf(b.role),
  );
  const activeInvites = (invites ?? []).filter(
    (invite) => !invite.accepted_by && !isExpired(invite.expires_at),
  );

  // Keys scoped to this hub
  const hubScopedKeys = (apiKeys ?? []).filter(
    (k) => k.hub_id === hubId || k.hub_ids?.includes(hubId),
  );

  if (!hubDetail && isLoading) {
    return (
      <DataSectionCard
        label={t.hubs?.membersTab ?? "Members"}
        phase="loading"
        skeleton={<DataCardSkeleton rows={3} />}
      />
    );
  }

  if (!hubDetail && isError) {
    return (
      <DataSectionCard
        label={t.hubs?.membersTab ?? "Members"}
        phase="error"
        errorCopy={{
          title: t.states?.error?.network ?? "Failed to load",
          retryLabel: t.states?.error?.retry ?? "Retry",
          onRetry: () => void refetch(),
        }}
      />
    );
  }

  return (
    <>
      {members.length === 0 && canManage ? (
        <Section>
          <div className="rounded-surface border border-border/60 px-6 py-6 flex flex-col items-center gap-2 text-center">
            <Users className="h-5 w-5 text-fg-4" />
            <p className="text-[14px] font-medium text-fg-2">
              {t.hubs?.inviteFirst ?? "Invite your first member"}
            </p>
            <p className="text-[13px] text-fg-3">
              {t.hubs?.inviteFirstDesc ?? ""}
            </p>
          </div>
        </Section>
      ) : null}

      {members.length > 0 && (
        <HubMembersSection
          members={sortedMembers}
          currentUserId={user?.id}
          canManage={canManage}
          t={t}
          interpolate={interpolate}
          onUpdateRole={(userId, role) =>
            updateMemberRole.mutate({ hubId, userId, role })
          }
          onRemoveMember={(userId) =>
            removeMember.mutateAsync({ hubId, userId })
          }
        />
      )}

      {canManage && (
        <HubInvitesSection
          hubId={hubId}
          invites={activeInvites}
          t={t}
          interpolate={interpolate}
          onCreateInvite={createInvite}
          onRevokeInvite={({ hubId, inviteId }) =>
            revokeInvite.mutateAsync({ hubId, inviteId })
          }
          onRegenerateInvite={regenerateInvite}
        />
      )}

      {/* Agents with access — read-only view of hub-scoped keys */}
      {hubScopedKeys.length > 0 && (
        <Section title={t.hubMembers?.agentsWithAccess ?? "Agents with access"}>
          <div className="space-y-1">
            {hubScopedKeys.map((key) => (
              <div
                key={key.id}
                className="flex items-center gap-2.5 px-1 py-2 text-[13px]"
              >
                <Bot className="h-3.5 w-3.5 text-fg-3 shrink-0" />
                <code className="text-fg-2 font-mono">{key.prefix}...</code>
                {key.agent_name && (
                  <span className="text-fg-3">{key.agent_name}</span>
                )}
                {key.last_used && (
                  <span className="text-fg-4 ml-auto">
                    {formatAge(key.last_used, t, interpolate)}
                  </span>
                )}
              </div>
            ))}
          </div>
          <button
            onClick={onNavigateToKeys}
            className="flex items-center gap-1.5 mt-2 text-[13px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
          >
            {t.hubMembers?.manageKeys ?? "Manage keys"}
            <ArrowRight className="h-3 w-3" />
          </button>
        </Section>
      )}
    </>
  );
}
