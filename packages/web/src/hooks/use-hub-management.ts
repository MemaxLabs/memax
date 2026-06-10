"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  MemaxError,
  type Hub,
  type HubDetailResult,
  type HubHeaderAuroraMode,
  type HubInvite,
  type HubInviteeInput,
  type HubMember,
  type HubOwnershipTransfer,
  type HubSummary,
  type ContributorDeletePolicy,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useLocale } from "@/i18n";

type HubRole = HubMember["role"];

export const hubDetailQueryKey = (hubId: string) =>
  ["hub-detail", hubId] as const;
export const hubSummaryQueryKey = (hubId: string) =>
  ["hub-summary", hubId] as const;
export const hubInvitesQueryKey = (hubId: string) =>
  ["hub-invites", hubId] as const;

export function getHubSummaryQueryOptions(hubId: string) {
  return {
    queryKey: hubSummaryQueryKey(hubId),
    queryFn: () => getMemaxClient().hubs.summary(hubId),
    staleTime: 60 * 1000,
  } as const;
}

export function useHubDetail(hubId: string | null) {
  return useQuery<HubDetailResult>({
    queryKey: hubDetailQueryKey(hubId!),
    queryFn: () => getMemaxClient().hubs.get(hubId!),
    enabled: !!hubId,
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}

export function useHubSummary(hubId: string | null) {
  return useQuery<HubSummary>({
    ...getHubSummaryQueryOptions(hubId!),
    enabled: !!hubId,
  });
}

export function useMarkHubVisit() {
  return useMutation({
    mutationFn: (hubId: string) => getMemaxClient().hubs.markVisit(hubId),
  });
}

export function useUpdateHub() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.updateHub,
    },
    mutationFn: ({
      hubId,
      ...params
    }: {
      hubId: string;
      name?: string;
      icon?: string;
      accent?: Hub["accent"];
      slug?: string;
      allow_contributor_topics?: boolean;
      allow_contributor_dreams?: boolean;
      contributor_delete_policy?: ContributorDeletePolicy;
      /** Empty string clears the per-hub override and restores default. */
      header_aurora_mode?: HubHeaderAuroraMode | "";
    }) => getMemaxClient().hubs.update(hubId, params),
    onMutate: async ({ hubId, ...params }) => {
      await qc.cancelQueries({ queryKey: hubDetailQueryKey(hubId) });
      const prev = qc.getQueryData<HubDetailResult>(hubDetailQueryKey(hubId));
      if (prev) {
        const updates: Partial<Hub> = {};
        for (const [k, v] of Object.entries(params)) {
          if (v !== undefined) (updates as Record<string, unknown>)[k] = v;
        }
        qc.setQueryData(hubDetailQueryKey(hubId), {
          ...prev,
          hub: { ...prev.hub, ...updates },
        });
      }
      return { prev };
    },
    onError: (_err, { hubId }, context) => {
      if (context?.prev)
        qc.setQueryData(hubDetailQueryKey(hubId), context.prev);
    },
    onSettled: (_data, _err, { hubId }) => {
      qc.invalidateQueries({ queryKey: hubDetailQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: hubSummaryQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: ["hubs"] });
    },
  });
}

export function useUpdateMemberRole() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.updateMember,
    },
    mutationFn: ({
      hubId,
      userId,
      role,
    }: {
      hubId: string;
      userId: string;
      role: Exclude<HubRole, "owner">;
    }) => getMemaxClient().hubs.updateMemberRole(hubId, userId, role),
    onMutate: async ({ hubId, userId, role }) => {
      await qc.cancelQueries({ queryKey: hubDetailQueryKey(hubId) });
      const prev = qc.getQueryData<HubDetailResult>(hubDetailQueryKey(hubId));
      if (prev) {
        qc.setQueryData(hubDetailQueryKey(hubId), {
          ...prev,
          members: prev.members.map((member: HubMember) =>
            member.user_id === userId ? { ...member, role } : member,
          ),
        });
      }
      return { prev };
    },
    onError: (_err, { hubId }, context) => {
      if (context?.prev)
        qc.setQueryData(hubDetailQueryKey(hubId), context.prev);
    },
    onSettled: (_data, _err, { hubId }) => {
      qc.invalidateQueries({ queryKey: hubDetailQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: hubSummaryQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: ["hubs"] });
    },
  });
}

export function useRemoveMember() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.removeMember,
    },
    mutationFn: ({ hubId, userId }: { hubId: string; userId: string }) =>
      getMemaxClient().hubs.removeMember(hubId, userId),
    onMutate: async ({ hubId, userId }) => {
      await qc.cancelQueries({ queryKey: hubDetailQueryKey(hubId) });
      const prev = qc.getQueryData<HubDetailResult>(hubDetailQueryKey(hubId));
      if (prev) {
        qc.setQueryData(hubDetailQueryKey(hubId), {
          ...prev,
          members: prev.members.filter((m: HubMember) => m.user_id !== userId),
          pending_transfer:
            prev.pending_transfer?.target_user_id === userId
              ? null
              : (prev.pending_transfer ?? null),
        });
      }
      return { prev };
    },
    onError: (_err, { hubId }, context) => {
      if (context?.prev)
        qc.setQueryData(hubDetailQueryKey(hubId), context.prev);
    },
    onSettled: (_data, _err, { hubId }) => {
      qc.invalidateQueries({ queryKey: hubDetailQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: hubSummaryQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: ["hubs"] });
    },
  });
}

export function useLeaveHub() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.leaveHub,
    },
    mutationFn: (hubId: string) => getMemaxClient().hubs.leave(hubId),
    onSettled: (_data, _err, hubId) => {
      qc.invalidateQueries({ queryKey: hubDetailQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: hubSummaryQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: ["hubs"] });
    },
  });
}

export function useDeleteHub() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.deleteHub,
    },
    mutationFn: async (hubId: string) => {
      try {
        return await getMemaxClient().hubs.delete(hubId);
      } catch (err) {
        if (
          err instanceof MemaxError &&
          (err.status === 404 || err.status === 410)
        ) {
          return;
        }
        throw err;
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["hub-summary"] });
      qc.invalidateQueries({ queryKey: ["hubs"] });
    },
  });
}

export function useCreateOwnershipTransfer() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.transferOwnership,
    },
    mutationFn: ({
      hubId,
      targetUserId,
    }: {
      hubId: string;
      targetUserId: string;
    }) => getMemaxClient().hubs.createOwnershipTransfer(hubId, targetUserId),
    onSuccess: (transfer, { hubId }) => {
      qc.setQueryData<HubDetailResult | undefined>(
        hubDetailQueryKey(hubId),
        (prev) => (prev ? { ...prev, pending_transfer: transfer } : prev),
      );
      qc.invalidateQueries({ queryKey: hubDetailQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: hubSummaryQueryKey(hubId) });
    },
  });
}

export function useAcceptOwnershipTransfer() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.acceptTransfer,
    },
    mutationFn: ({
      hubId,
      transferId,
    }: {
      hubId: string;
      transferId: string;
    }) => getMemaxClient().hubs.acceptOwnershipTransfer(hubId, transferId),
    onSettled: (_data, _err, { hubId }) => {
      qc.invalidateQueries({ queryKey: hubDetailQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: hubSummaryQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: ["hubs"] });
    },
  });
}

export function useCancelOwnershipTransfer() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.cancelTransfer,
    },
    mutationFn: ({
      hubId,
      transferId,
    }: {
      hubId: string;
      transferId: string;
    }) => getMemaxClient().hubs.cancelOwnershipTransfer(hubId, transferId),
    onSuccess: (_data, { hubId }) => {
      qc.setQueryData<HubDetailResult | undefined>(
        hubDetailQueryKey(hubId),
        (prev) => (prev ? { ...prev, pending_transfer: null } : prev),
      );
      qc.invalidateQueries({ queryKey: hubDetailQueryKey(hubId) });
    },
  });
}

export function useHubInvites(hubId: string | null, canManage: boolean) {
  return useQuery<HubInvite[]>({
    queryKey: hubInvitesQueryKey(hubId!),
    queryFn: () => getMemaxClient().hubs.listInvites(hubId!),
    enabled: !!hubId && canManage,
    staleTime: 30 * 1000,
    gcTime: 10 * 60 * 1000,
  });
}

export function useCreateInvite() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.hubs.inviteError,
      errorAction: t.errors.action.createInvite,
    },
    mutationFn: ({
      hubId,
      role,
      invitee,
    }: {
      hubId: string;
      role?: HubRole;
      invitee?: HubInviteeInput;
    }) =>
      getMemaxClient().hubs.createInvite(hubId, {
        role,
        invitee,
      }),
    onSettled: (_data, _err, { hubId }) => {
      qc.invalidateQueries({ queryKey: hubInvitesQueryKey(hubId) });
      // An addressed invite also fans out a hub_invite notification
      // to the invitee's inbox. The invitee sees it via SSE; the
      // inviter's local notifications cache doesn't need to change.
    },
  });
}

export function useRevokeInvite() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.revokeInvite,
    },
    mutationFn: ({ hubId, inviteId }: { hubId: string; inviteId: string }) =>
      getMemaxClient().hubs.revokeInvite(hubId, inviteId),
    onMutate: async ({ hubId, inviteId }) => {
      await qc.cancelQueries({ queryKey: hubInvitesQueryKey(hubId) });
      const prev = qc.getQueryData<HubInvite[]>(hubInvitesQueryKey(hubId));
      if (prev) {
        qc.setQueryData(
          hubInvitesQueryKey(hubId),
          prev.filter((inv) => inv.id !== inviteId),
        );
      }
      return { prev };
    },
    onError: (_err, { hubId }, context) => {
      if (context?.prev)
        qc.setQueryData(hubInvitesQueryKey(hubId), context.prev);
    },
    onSettled: (_data, _err, { hubId }) => {
      qc.invalidateQueries({ queryKey: hubInvitesQueryKey(hubId) });
    },
  });
}

export function useRegenerateInvite() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.regenerateInvite,
    },
    mutationFn: ({ hubId, inviteId }: { hubId: string; inviteId: string }) =>
      getMemaxClient().hubs.regenerateInvite(hubId, inviteId),
    onMutate: async ({ hubId, inviteId }) => {
      await qc.cancelQueries({ queryKey: hubInvitesQueryKey(hubId) });
      const prev = qc.getQueryData<HubInvite[]>(hubInvitesQueryKey(hubId));
      return { prev, inviteId };
    },
    onSuccess: (invite, { hubId, inviteId }) => {
      qc.setQueryData<HubInvite[] | undefined>(
        hubInvitesQueryKey(hubId),
        (prev) => {
          if (!prev) return [invite];
          return prev.map((item) => (item.id === inviteId ? invite : item));
        },
      );
    },
    onError: (_err, { hubId }, context) => {
      if (context?.prev)
        qc.setQueryData(hubInvitesQueryKey(hubId), context.prev);
    },
  });
}

export function useResendInvite() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.resendInvite,
    },
    mutationFn: ({ hubId, inviteId }: { hubId: string; inviteId: string }) =>
      getMemaxClient().hubs.resendInvite(hubId, inviteId),
    onSettled: (_data, _err, { hubId }) => {
      qc.invalidateQueries({ queryKey: hubInvitesQueryKey(hubId) });
    },
  });
}
