"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as adminClient from "@/lib/admin-client";
import { useLocale } from "@/i18n";
import type {
  AdminAudience,
  AdminAudienceCreateParams,
  AdminAudienceEstimate,
  AdminAudienceEstimateParams,
  AdminAudienceUpdateParams,
} from "@/lib/admin-client";

const audiencesKey = ["admin", "audiences"] as const;

export function useAdminAudiences() {
  return useQuery<{ audiences: AdminAudience[] }>({
    queryKey: [...audiencesKey, "list"],
    queryFn: () => adminClient.listAudiences(),
    staleTime: 30_000,
  });
}

export function useAdminAudience(id?: string) {
  return useQuery<AdminAudience>({
    queryKey: [...audiencesKey, "detail", id],
    queryFn: () => adminClient.getAudience(id as string),
    enabled: Boolean(id),
    staleTime: 30_000,
  });
}

export function useCreateAdminAudience() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<AdminAudience, Error, AdminAudienceCreateParams>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminCreateAudience,
    },
    mutationFn: (params) => adminClient.createAudience(params),
    onSuccess: () => qc.invalidateQueries({ queryKey: audiencesKey }),
  });
}

export function useUpdateAdminAudience(id?: string) {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<AdminAudience, Error, AdminAudienceUpdateParams>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminUpdateAudience,
    },
    mutationFn: (params) => adminClient.updateAudience(id as string, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: audiencesKey }),
  });
}

export function useDeleteAdminAudience() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<{ status: string; id: string }, Error, string>({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminDeleteAudience,
    },
    mutationFn: (id) => adminClient.deleteAudience(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: audiencesKey }),
  });
}

export function useEstimateAdminAudience() {
  const { t } = useLocale();
  return useMutation<AdminAudienceEstimate, Error, AdminAudienceEstimateParams>(
    {
      meta: {
        errorMessage: t.states.error.unexpected,
        errorAction: t.errors.action.adminEstimateAudience,
      },
      mutationFn: (params) => adminClient.estimateAudience(params),
    },
  );
}
