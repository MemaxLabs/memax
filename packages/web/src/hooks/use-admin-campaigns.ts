"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as adminClient from "@/lib/admin-client";
import { useLocale } from "@/i18n";
import type {
  AdminCampaign,
  AdminCampaignCreateParams,
  AdminCampaignDeliveryStats,
  AdminCampaignDetail,
  AdminCampaignListParams,
  AdminCampaignListResponse,
  AdminCampaignScheduleParams,
  AdminCampaignUpdateParams,
} from "@/lib/admin-client";

const campaignsKey = ["admin", "campaigns"] as const;

export function useAdminCampaigns(params?: AdminCampaignListParams) {
  return useQuery<AdminCampaignListResponse>({
    queryKey: [...campaignsKey, "list", params?.status ?? "all"],
    queryFn: () => adminClient.listCampaigns(params ?? {}),
    staleTime: 15_000,
  });
}

export function useAdminCampaign(id?: string) {
  return useQuery<AdminCampaignDetail>({
    queryKey: [...campaignsKey, "detail", id],
    queryFn: () => adminClient.getCampaign(id as string),
    enabled: Boolean(id),
    staleTime: 10_000,
  });
}

// useAdminCampaignDeliveryStats polls campaign delivery stats. Pass the
// campaign's current `status` so polling only fires while a send is
// actively in-flight; terminal statuses (sent/failed/cancelled) fetch
// once and stop. `enabled=false` disables entirely (in-app campaigns).
export function useAdminCampaignDeliveryStats(
  id?: string,
  enabled = true,
  status?: string,
) {
  const inflight = status === "sending" || status === "scheduled";
  return useQuery<AdminCampaignDeliveryStats>({
    queryKey: [...campaignsKey, "deliveries", "stats", id],
    queryFn: () => adminClient.getCampaignDeliveryStats(id as string),
    enabled: Boolean(id) && enabled,
    staleTime: inflight ? 0 : 5_000,
    // Actively refetch while a send is running so operators see counts
    // climb live. Disabled on terminal states to avoid idle polling.
    refetchInterval: inflight ? 5_000 : false,
    refetchIntervalInBackground: false,
  });
}

export function useCreateAdminCampaign() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<AdminCampaign, Error, AdminCampaignCreateParams>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminCreateCampaign,
    },
    mutationFn: (params) => adminClient.createCampaign(params),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignsKey });
    },
  });
}

export function useUpdateAdminCampaignDraft(id?: string) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<AdminCampaign, Error, AdminCampaignUpdateParams>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminUpdateCampaign,
    },
    mutationFn: (params) =>
      adminClient.updateCampaignDraft(id as string, params),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignsKey });
    },
  });
}

export function useDeleteAdminCampaign() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<{ status: string; id: string }, Error, string>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminDeleteCampaign,
    },
    mutationFn: (id) => adminClient.deleteCampaign(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignsKey });
    },
  });
}

export function useSendAdminCampaign() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<AdminCampaign, Error, string>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminSendCampaign,
    },
    mutationFn: (id) => adminClient.sendCampaign(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignsKey });
    },
  });
}

export function useScheduleAdminCampaign(id?: string) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<AdminCampaign, Error, AdminCampaignScheduleParams>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminScheduleCampaign,
    },
    mutationFn: (params) => adminClient.scheduleCampaign(id as string, params),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignsKey });
    },
  });
}

export function useCancelAdminCampaign() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<AdminCampaign, Error, string>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminCancelCampaign,
    },
    mutationFn: (id) => adminClient.cancelCampaign(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignsKey });
    },
  });
}

export function useTestSendAdminCampaign(id?: string) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    adminClient.AdminCampaignTestSendResult,
    Error,
    { to: string }
  >({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminTestSendCampaign,
    },
    mutationFn: ({ to }) => adminClient.testSendCampaign(id as string, to),
    onSuccess: () => {
      // Audit timeline gains a test_send entry — refresh detail view.
      qc.invalidateQueries({ queryKey: [...campaignsKey, "detail", id] });
    },
  });
}
