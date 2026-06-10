"use client";

import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import {
  useHubDetail,
  useUpdateHub,
  useCreateOwnershipTransfer,
  useAcceptOwnershipTransfer,
  useCancelOwnershipTransfer,
} from "@/hooks/use-hub-management";
import { HubPermissionsSection } from "@/components/features/teams/hub-permissions-section";
import { HubOwnershipSection } from "@/components/features/teams/hub-ownership-section";
import { ROLE_ORDER, type HubRole } from "@/components/features/teams/shared";
import type { Hub } from "memax-sdk";

export function HubPermissions({ hubId }: { hubId: string }) {
  const { user, hubs } = useAuth();
  const { t } = useLocale();

  const hubWithRole = hubs.find((h) => h.hub.id === hubId);
  const myRole = (hubWithRole?.role ?? "viewer") as HubRole;
  const isOwner = myRole === "owner";
  const canManage = isOwner || myRole === "admin";

  const { data: hubDetail } = useHubDetail(hubId);
  const updateHub = useUpdateHub();
  const createTransfer = useCreateOwnershipTransfer();
  const acceptTransfer = useAcceptOwnershipTransfer();
  const cancelTransfer = useCancelOwnershipTransfer();

  const hub = (hubDetail?.hub ?? hubWithRole?.hub) as Hub | undefined;
  const members = hubDetail?.members ?? [];
  const pendingTransfer = hubDetail?.pending_transfer ?? null;
  const isTransferTarget = pendingTransfer?.target_user_id === user?.id;

  if (!hub || !canManage) return null;

  const sortedMembers = [...members].sort(
    (a, b) => ROLE_ORDER.indexOf(a.role) - ROLE_ORDER.indexOf(b.role),
  );
  const transferTargets = sortedMembers.filter(
    (member) => member.user_id !== user?.id,
  );

  return (
    <div>
      <HubPermissionsSection
        hubId={hubId}
        hub={hub}
        updateHub={updateHub}
        t={t}
      />

      {(isOwner || isTransferTarget) && (
        <HubOwnershipSection
          t={t}
          pendingTransfer={pendingTransfer}
          isOwner={isOwner}
          isTransferTarget={isTransferTarget}
          members={transferTargets}
          onCreate={(targetUserId) =>
            createTransfer.mutate({ hubId, targetUserId })
          }
          onAccept={() =>
            pendingTransfer &&
            acceptTransfer.mutate({ hubId, transferId: pendingTransfer.id })
          }
          onCancel={() =>
            pendingTransfer &&
            cancelTransfer.mutate({ hubId, transferId: pendingTransfer.id })
          }
        />
      )}
    </div>
  );
}
