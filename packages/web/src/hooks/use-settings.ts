"use client";

import { useQuery, useMutation } from "@tanstack/react-query";
import { getMemaxClient } from "@/lib/memax-client";
import { queryClient } from "@/lib/query-client";
import { useLocale } from "@/i18n";
import {
  applyStoredHubHeaderMode,
  setStoredHubHeaderMode,
} from "@/lib/hub-header-mode";
import type {
  DevFlagsSettings,
  MemoryKind,
  Settings as SDKSettings,
  SettingsUpdateInput,
} from "memax-sdk";

const DEFAULT_DEV_FLAGS: DevFlagsSettings = {
  mockDreams: false,
  mockDreaming: false,
  mockEmptyInbox: false,
  mockProUser: false,
  debuggerEnabled: false,
  skipRerank: false,
};

export interface Settings extends SDKSettings {
  dreams_organize_enabled: boolean;
  // dreams_restructure_enabled: controls phase 5 (topic tree
  // restructure). Engine reads this in resolveHubContext; UI added
  // the toggle in commit 3.4.
  dreams_restructure_enabled: boolean;
  dreams_excluded_kinds: MemoryKind[];
  dev_flags?: DevFlagsSettings;
}

export function useSettings(opts?: { enabled?: boolean }) {
  return useQuery<Settings>({
    queryKey: ["settings"],
    queryFn: async () =>
      applyStoredHubHeaderMode(
        (await getMemaxClient().settings.get()) as Settings,
      ),
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
    enabled: opts?.enabled ?? true,
  });
}

function mergeDevFlags(
  current: DevFlagsSettings | undefined,
  patch: Partial<DevFlagsSettings>,
): DevFlagsSettings {
  return {
    ...DEFAULT_DEV_FLAGS,
    ...(current ?? {}),
    ...patch,
  };
}

export function useUpdateSettings() {
  const { t } = useLocale();
  return useMutation<
    Settings,
    Error,
    SettingsUpdateInput,
    { previous?: Settings }
  >({
    meta: {
      errorMessage: t.toast.settingsFailed,
      errorAction: t.errors.action.updateSettings,
    },
    mutationFn: (patch) =>
      getMemaxClient().settings.update(patch) as Promise<Settings>,
    onMutate: async (patch) => {
      await queryClient.cancelQueries({ queryKey: ["settings"] });
      const previous = queryClient.getQueryData<Settings>(["settings"]);
      if (patch.hub_header_aurora_mode !== undefined) {
        setStoredHubHeaderMode(patch.hub_header_aurora_mode);
      }

      if (previous) {
        const { dev_flags, ...topLevelPatch } = patch;
        const nextSettings: Settings = {
          ...applyStoredHubHeaderMode(previous),
          ...topLevelPatch,
        };
        if (dev_flags) {
          nextSettings.dev_flags = mergeDevFlags(previous.dev_flags, dev_flags);
        }
        queryClient.setQueryData<Settings>(["settings"], {
          ...nextSettings,
        });
      }

      return { previous };
    },
    onError: (_error, _patch, context) => {
      if (context?.previous) {
        queryClient.setQueryData(
          ["settings"],
          applyStoredHubHeaderMode(context.previous),
        );
      }
    },
    onSuccess: (settings) => {
      queryClient.setQueryData(
        ["settings"],
        applyStoredHubHeaderMode(settings),
      );
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["settings"] });
    },
  });
}
