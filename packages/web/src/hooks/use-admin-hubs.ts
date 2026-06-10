"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import * as adminClient from "@/lib/admin-client";
import { useLocale } from "@/i18n";
import type {
  AdminHubDetail,
  AdminHubListParams,
  AdminHubListResponse,
  AdminHubMemberListParams,
  AdminHubMemberListResponse,
  HubSubscription,
  PlanDefinition,
} from "@/lib/admin-client";

const adminHubsKey = ["admin", "hubs"] as const;

export function useAdminHubs(params: AdminHubListParams = {}) {
  return useQuery<AdminHubListResponse>({
    queryKey: [...adminHubsKey, params],
    queryFn: () => adminClient.listTeamHubs(params),
    staleTime: 30 * 1000,
  });
}

export function useAdminHubMembers(
  hubId: string,
  params: AdminHubMemberListParams = {},
) {
  return useQuery<AdminHubMemberListResponse>({
    queryKey: [...adminHubsKey, hubId, "members", params],
    queryFn: () => adminClient.listHubMembers(hubId, params),
    staleTime: 30 * 1000,
    enabled: !!hubId,
  });
}

export function useAdminHubDetail(hubId: string) {
  return useQuery<AdminHubDetail>({
    queryKey: [...adminHubsKey, hubId],
    queryFn: () => adminClient.getHub(hubId),
    staleTime: 30 * 1000,
    enabled: !!hubId,
  });
}

export function useAdminHubSubscription(hubId: string) {
  return useQuery<HubSubscription | null>({
    queryKey: [...adminHubsKey, hubId, "subscription"],
    queryFn: () => adminClient.getHubSubscription(hubId),
    staleTime: 30 * 1000,
    enabled: !!hubId,
  });
}

export function useSetHubPlan() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminSetHubPlan,
    },
    mutationFn: ({ hubId, planId }: { hubId: string; planId: string }) =>
      adminClient.setHubPlan(hubId, planId),
    onSuccess: (_data, { hubId }) => {
      qc.invalidateQueries({ queryKey: adminHubsKey });
      qc.invalidateQueries({
        queryKey: [...adminHubsKey, hubId, "subscription"],
      });
    },
  });
}
