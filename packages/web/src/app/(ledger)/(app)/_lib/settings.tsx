"use client";

import { useCallback, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { isRetryable, toFailure } from "@/lib/v2/data/command-error";
import {
  applyChange,
  type NotificationChange,
  type NotificationSettingsView,
} from "@/lib/v2/data/settings";
import { IntentKeys } from "@/lib/v2/intent-keys";
import { useSource } from "./data";
import { dreamKeys } from "./dream";

/**
 * TanStack Query over the source's settings (lib/v2/data/settings.ts):
 * the person's notification settings, edited from the version last read
 * (If-Match), and what Settings › Security reads from the server. The
 * demo's data comes from `peek*`, so the first render (and every
 * screenshot) has it.
 */

export const settingsKeys = {
  notifications: (kind: string) =>
    ["v2", kind, "settings", "notifications"] as const,
  security: (kind: string) => ["v2", kind, "settings", "security"] as const,
};

export function useNotificationSettings() {
  const source = useSource();
  return useQuery({
    queryKey: settingsKeys.notifications(source.kind),
    queryFn: ({ signal }) => source.settings.notifications({ signal }),
    initialData: source.settings.peekNotifications?.(),
    staleTime: 30_000,
  });
}

export function useSecurity() {
  const source = useSource();
  return useQuery({
    queryKey: settingsKeys.security(source.kind),
    queryFn: ({ signal }) => source.settings.security({ signal }),
    initialData: source.settings.peekSecurity?.(),
    staleTime: 5 * 60_000,
  });
}

/** What an edit came to. */
export type SettingsOutcome =
  | { ok: true; value: NotificationSettingsView }
  | { ok: false; failure: ReturnType<typeof toFailure> };

/**
 * Change the notification settings from the version on hand. The change
 * shows at once; a refusal puts it back. Changes made in quick succession
 * go one after another, each from the version the last one wrote, so a
 * second click never clashes with the first. A clash (the unsubscribe
 * link, another tab) reloads the settings, so the person sees what's
 * there before they change it again. One idempotency key per change,
 * reused while a retry is the same request.
 */
export function useUpdateNotificationSettings() {
  const source = useSource();
  const queryClient = useQueryClient();
  const keys = useRef(new IntentKeys()).current;
  // The server's latest answer while changes are in flight, the changes
  // not answered yet, and the queue they wait in.
  const server = useRef<NotificationSettingsView | null>(null);
  const pending = useRef<NotificationChange[]>([]);
  const queue = useRef<Promise<unknown>>(Promise.resolve());
  return useCallback(
    (change: NotificationChange): Promise<SettingsOutcome> => {
      const key = settingsKeys.notifications(source.kind);
      if (pending.current.length === 0) {
        server.current =
          queryClient.getQueryData<NotificationSettingsView>(key) ?? null;
      }
      if (!server.current) {
        return Promise.resolve({
          ok: false,
          failure: { kind: "unknown", message: null },
        });
      }
      const show = () => {
        if (!server.current) return;
        queryClient.setQueryData(
          key,
          pending.current.reduce(applyChange, server.current),
        );
      };
      pending.current.push(change);
      show();
      const settle = () => {
        pending.current = pending.current.filter((c) => c !== change);
        show();
      };
      const run = async (): Promise<SettingsOutcome> => {
        const from = server.current!;
        const intent = `notifications:${from.version}:${JSON.stringify(change)}`;
        try {
          const value = await source.settings.updateNotifications({
            change,
            version: from.version,
            idempotencyKey: keys.keyFor(intent),
          });
          keys.settle(intent);
          server.current = value;
          settle();
          if (change.email?.morning_edition !== undefined) {
            // The morning email is Dream's setting too (Settings › Account).
            await queryClient.invalidateQueries({
              queryKey: dreamKeys.settings(source.kind),
            });
          }
          return { ok: true, value };
        } catch (err) {
          const failure = toFailure(err);
          if (!isRetryable(failure)) keys.settle(intent);
          settle();
          if (failure.kind === "clash" && pending.current.length === 0) {
            await queryClient.invalidateQueries({ queryKey: key });
          }
          return { ok: false, failure };
        }
      };
      const next = queue.current.then(run, run);
      queue.current = next;
      return next;
    },
    [keys, queryClient, source],
  );
}
