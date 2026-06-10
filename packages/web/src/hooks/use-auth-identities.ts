"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { AuthIdentity, AuthProviderName } from "memax-sdk";
import { getAccessToken } from "@/lib/auth";
import { getMemaxClient } from "@/lib/memax-client";
import { useLocale } from "@/i18n";

export const authIdentitiesQueryKey = ["auth-identities"] as const;

interface LinkStartResponse {
  data?: { url?: string };
  error?: { code?: string; message?: string };
}

export function useAuthIdentities() {
  return useQuery<AuthIdentity[]>({
    queryKey: authIdentitiesQueryKey,
    queryFn: () => getMemaxClient().auth.listIdentities(),
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}

export function useStartProviderLink() {
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.toast.linkProviderFailed,
      errorAction: t.errors.action.linkProvider,
    },
    mutationFn: async (provider: AuthProviderName) => {
      const token = getAccessToken();
      if (!token) {
        throw new Error("Not authenticated.");
      }

      const redirectURI = `${window.location.origin}/settings?account_linked=${provider}`;
      const response = await fetch(
        `/api/auth/link/${provider}?redirect_uri=${encodeURIComponent(
          redirectURI,
        )}`,
        {
          headers: { Authorization: `Bearer ${token}` },
          cache: "no-store",
        },
      );
      const payload = (await response.json()) as LinkStartResponse;
      if (!response.ok || payload.error || !payload.data?.url) {
        throw new Error(payload.error?.message ?? "Could not start sign-in.");
      }
      return payload.data.url;
    },
  });
}

export function useUnlinkProvider() {
  const queryClient = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      successMessage: t.toast.providerDisconnected,
      errorMessage: t.toast.unlinkProviderFailed,
      errorAction: t.errors.action.unlinkProvider,
    },
    mutationFn: (provider: AuthProviderName) =>
      getMemaxClient().auth.unlinkProvider(provider),
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: authIdentitiesQueryKey }),
  });
}
