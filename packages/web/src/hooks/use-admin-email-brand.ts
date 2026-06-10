"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocale } from "@/i18n";
import * as adminClient from "@/lib/admin-client";
import type {
  AdminEmailBrandSettings,
  AdminEmailBrandUpdateParams,
} from "@/lib/admin-client";

const adminEmailBrandKey = ["admin", "email", "brand"] as const;

export function useAdminEmailBrand() {
  return useQuery<AdminEmailBrandSettings>({
    queryKey: adminEmailBrandKey,
    queryFn: () => adminClient.getEmailBrand(),
    staleTime: 30_000,
  });
}

export function useUpdateAdminEmailBrand() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    AdminEmailBrandSettings,
    Error,
    AdminEmailBrandUpdateParams
  >({
    meta: {
      errorMessage: t.toast.updateFailed,
      errorAction: t.errors.action.adminSaveEmailTemplate,
    },
    mutationFn: (params) => adminClient.updateEmailBrand(params),
    onSuccess: (settings) => {
      qc.setQueryData(adminEmailBrandKey, settings);
      // Invalidate template queries — their previews embed the brand layout.
      qc.invalidateQueries({ queryKey: ["admin", "email", "templates"] });
    },
  });
}
