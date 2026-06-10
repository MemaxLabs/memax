"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocale } from "@/i18n";
import * as adminClient from "@/lib/admin-client";
import type {
  AdminCampaignTemplate,
  AdminCampaignTemplateCreateParams,
  AdminCampaignTemplateListResponse,
  AdminCampaignTemplateUpdateParams,
} from "@/lib/admin-client";

// Namespaced query key. We include includeArchived in the key so the
// management view and the picker can coexist without flushing each
// other's cache: archived-visible and archived-hidden are distinct
// server queries with different shapes in practice.
const campaignTemplatesKey = ["admin", "campaign-templates"] as const;

export function useAdminCampaignTemplates(includeArchived = false) {
  return useQuery<AdminCampaignTemplateListResponse>({
    queryKey: [...campaignTemplatesKey, "list", includeArchived],
    queryFn: () => adminClient.listCampaignTemplates(includeArchived),
    staleTime: 15_000,
  });
}

export function useAdminCampaignTemplate(id?: string) {
  return useQuery<AdminCampaignTemplate>({
    queryKey: [...campaignTemplatesKey, "detail", id],
    queryFn: () => adminClient.getCampaignTemplate(id as string),
    enabled: Boolean(id),
    staleTime: 15_000,
  });
}

export function useCreateAdminCampaignTemplate() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    AdminCampaignTemplate,
    Error,
    AdminCampaignTemplateCreateParams
  >({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminSaveEmailTemplate,
    },
    mutationFn: (params) => adminClient.createCampaignTemplate(params),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignTemplatesKey });
    },
  });
}

export function useUpdateAdminCampaignTemplate(id?: string) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    AdminCampaignTemplate,
    Error,
    AdminCampaignTemplateUpdateParams
  >({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminSaveEmailTemplate,
    },
    mutationFn: (params) =>
      adminClient.updateCampaignTemplate(id as string, params),
    onSuccess: (tmpl) => {
      qc.setQueryData([...campaignTemplatesKey, "detail", id], tmpl);
      qc.invalidateQueries({ queryKey: campaignTemplatesKey });
    },
  });
}

export function useSetAdminCampaignTemplateArchived() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    AdminCampaignTemplate,
    Error,
    { id: string; archived: boolean }
  >({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminSaveEmailTemplate,
    },
    mutationFn: ({ id, archived }) =>
      adminClient.setCampaignTemplateArchived(id, archived),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignTemplatesKey });
    },
  });
}

export function useDeleteAdminCampaignTemplate() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<{ status: string; id: string }, Error, string>({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminSaveEmailTemplate,
    },
    mutationFn: (id) => adminClient.deleteCampaignTemplate(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: campaignTemplatesKey });
    },
  });
}
