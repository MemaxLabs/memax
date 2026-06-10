"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  Hub,
  HubDetailResult,
  HubSettings,
  HubSettingsInput,
  HubWithRole,
} from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { useLocale } from "@/i18n";
import { hubDetailQueryKey, hubSummaryQueryKey } from "./use-hub-management";
import { hubListQueryKey } from "./use-hubs";

/**
 * applyHubSettingsPatch mirrors the server's partial-merge semantics:
 * null deletes the override, a boolean sets it, and missing keys
 * preserve existing values. Exported for unit tests; the mutation
 * below uses it to compute optimistic cache state.
 */
export function applyHubSettingsPatch(
  existing: HubSettings | undefined,
  patch: HubSettingsInput,
): HubSettings {
  const next: HubSettings = { ...(existing ?? {}) };
  for (const key of Object.keys(patch) as (keyof HubSettings)[]) {
    const value = patch[key];
    if (value === null) {
      delete next[key];
    } else if (value !== undefined) {
      next[key] = value;
    }
  }
  return next;
}

/**
 * countHubOverrides reports how many phase keys this hub has
 * explicitly overridden. Drives the summary line on the disclosure
 * card ("2 overrides" vs "Following account defaults").
 */
export function countHubOverrides(hub: Pick<Hub, "settings">): number {
  return Object.keys(hub.settings ?? {}).length;
}

/**
 * useUpdateHubSettings sends a partial-merge patch through the
 * SDK's hubs.update. Null values clear an override; boolean
 * values set it. Optimistically updates both the hubs list (used
 * by the per-hub overrides panel) and the hub-detail caches so
 * the UI reflects the change before the server round-trips.
 *
 * Reusing existing query keys (hubListQueryKey from use-hubs.ts,
 * hubDetailQueryKey from use-hub-management.ts) keeps every hub
 * consumer in sync — no extra cache to reason about.
 */
export function useUpdateHubSettings() {
  const qc = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    meta: {
      errorMessage: t.hubOverrides.saveFailed,
      errorAction: t.errors.action.updateHubSettings,
    },
    mutationFn: ({
      hubId,
      settings,
    }: {
      hubId: string;
      settings: HubSettingsInput;
    }) => getMemaxClient().hubs.update(hubId, { settings }),
    onMutate: async ({ hubId, settings }) => {
      await qc.cancelQueries({ queryKey: hubListQueryKey });
      await qc.cancelQueries({ queryKey: hubDetailQueryKey(hubId) });
      const prevList = qc.getQueryData<HubWithRole[]>(hubListQueryKey);
      const prevDetail = qc.getQueryData<HubDetailResult>(
        hubDetailQueryKey(hubId),
      );

      if (prevList) {
        qc.setQueryData<HubWithRole[]>(
          hubListQueryKey,
          prevList.map((item) =>
            item.hub.id === hubId
              ? {
                  ...item,
                  hub: {
                    ...item.hub,
                    settings: applyHubSettingsPatch(
                      item.hub.settings,
                      settings,
                    ),
                  },
                }
              : item,
          ),
        );
      }
      if (prevDetail) {
        qc.setQueryData<HubDetailResult>(hubDetailQueryKey(hubId), {
          ...prevDetail,
          hub: {
            ...prevDetail.hub,
            settings: applyHubSettingsPatch(prevDetail.hub.settings, settings),
          },
        });
      }
      return { prevList, prevDetail };
    },
    onError: (_err, { hubId }, context) => {
      if (context?.prevList) {
        qc.setQueryData(hubListQueryKey, context.prevList);
      }
      if (context?.prevDetail) {
        qc.setQueryData(hubDetailQueryKey(hubId), context.prevDetail);
      }
    },
    onSettled: (_data, _err, { hubId }) => {
      qc.invalidateQueries({ queryKey: hubListQueryKey });
      qc.invalidateQueries({ queryKey: hubDetailQueryKey(hubId) });
      qc.invalidateQueries({ queryKey: hubSummaryQueryKey(hubId) });
    },
  });
}
