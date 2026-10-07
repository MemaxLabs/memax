"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocale } from "@/i18n";
import {
  adminV2UIClient,
  type AdminV2UI,
  type AdminV2UISetting,
} from "@/lib/admin-client";

/** The per-user V2 UI flag's cache key, under the admin user's own. */
export const adminV2UIKey = (userId: string) =>
  ["admin", "users", userId, "v2-ui"] as const;

/** A person's V2 UI flag and why (the admin user page's V2 UI card). */
export function useAdminV2UI(userId: string) {
  return useQuery<AdminV2UI>({
    queryKey: adminV2UIKey(userId),
    queryFn: () => adminV2UIClient.get(userId),
    staleTime: 30 * 1000,
    enabled: !!userId,
  });
}

/** Turns the V2 UI on or off for a person, or back to the rules. */
export function useSetAdminV2UI() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminSetV2Ui,
    },
    mutationFn: ({
      userId,
      setting,
    }: {
      userId: string;
      setting: AdminV2UISetting;
    }) => adminV2UIClient.set(userId, setting),
    onSuccess: (data, { userId }) => {
      qc.setQueryData(adminV2UIKey(userId), data);
    },
  });
}
