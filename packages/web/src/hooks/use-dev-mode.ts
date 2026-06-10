"use client";

import { useCallback } from "react";
import { useAuth } from "@/lib/auth";
import { useSettings, useUpdateSettings } from "@/hooks/use-settings";
import type { DevFlagsSettings } from "memax-sdk";

/**
 * Dev mode flag system — gated by GitHub org membership.
 *
 * Backend checks if user belongs to DEV_GITHUB_ORG during OAuth
 * and sets dev_access=true in user preferences. This hook reads that
 * gate and stores the dev-tool toggles in persisted user settings.
 */

// Local extension of the SDK's DevFlagsSettings. The SDK type is
// closed; new flags added here flow through the server as part of
// the dev_flags JSONB blob without needing an SDK release. Bump
// the SDK to match when the flag stabilises and other consumers
// need to read it.
export interface DevFlags extends DevFlagsSettings {
  // Show the per-tool capabilities chips row above the chat
  // composer. Off by default — even for dev users — because the
  // catalog is a debug surface, not a feature the operator picks
  // intentionally. Toggle in Settings → Dev to surface it when
  // exercising a new tool.
  showChatCapabilities?: boolean;
}

const DEFAULT_FLAGS: DevFlags = {
  mockDreams: false,
  mockDreaming: false,
  mockEmptyInbox: false,
  mockProUser: false,
  debuggerEnabled: false,
  skipRerank: false,
  showChatCapabilities: false,
};

export function useDevMode() {
  const { user } = useAuth();
  const { data: settings } = useSettings();
  const updateSettings = useUpdateSettings();
  const isDevUser = Boolean(user?.dev_access);
  const flags: DevFlags = isDevUser
    ? { ...DEFAULT_FLAGS, ...(settings?.dev_flags ?? {}) }
    : DEFAULT_FLAGS;

  const updateFlags = useCallback(
    (patch: Partial<DevFlags>) => {
      if (!isDevUser) return;
      updateSettings.mutate({
        dev_flags: patch,
      });
    },
    [isDevUser, updateSettings],
  );

  const setFlag = useCallback(
    <K extends keyof DevFlags>(key: K, value: DevFlags[K]) => {
      updateFlags({ [key]: value } as Pick<DevFlags, K>);
    },
    [updateFlags],
  );

  return { isDevUser, flags, setFlag, updateFlags };
}
