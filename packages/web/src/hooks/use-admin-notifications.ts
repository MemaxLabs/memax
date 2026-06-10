"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import * as adminClient from "@/lib/admin-client";
import { useLocale } from "@/i18n";
import type {
  AdminSendNotificationParams,
  AdminSendNotificationResult,
  AdminSentNotificationsResponse,
} from "@/lib/admin-client";

const sentNotificationsKey = ["admin", "notifications", "sent"] as const;

export function useAdminSendNotification() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation<
    AdminSendNotificationResult,
    Error,
    AdminSendNotificationParams
  >({
    meta: {
      errorMessage: t.states.error.unexpected,
      errorAction: t.errors.action.adminSendNotification,
    },
    mutationFn: (params) => adminClient.sendNotification(params),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: sentNotificationsKey });
    },
  });
}

export function useAdminSentNotifications(params?: {
  kind?: string;
  limit?: number;
}) {
  return useQuery<AdminSentNotificationsResponse>({
    queryKey: [...sentNotificationsKey, params?.kind ?? "all"],
    queryFn: () =>
      adminClient.listSentNotifications({
        kind: params?.kind,
        limit: params?.limit,
      }),
    staleTime: 30_000,
  });
}
