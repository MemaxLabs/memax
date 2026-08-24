"use client";

/**
 * LocaleServerSync — reconciles the UI locale with the server-persisted
 * `settings.locale` (工单 7: agentic output follows the reader's
 * language, which requires the server to know it).
 *
 * Direction of truth:
 *   - Server value set ("en"/"zh") → it wins. Devices converge on the
 *     account preference instead of each keeping its own localStorage.
 *   - Server value unset ("") + this device has an EXPLICIT localStorage
 *     choice → migrate that choice up once. Pre-existing users who
 *     picked a language before the server field existed keep it without
 *     re-choosing, and the server learns it.
 *   - Neither set → browser detection stands, nothing is written. A
 *     detected default is not a preference; persisting it would freeze
 *     a guess.
 *
 * User changes flow through the settings panel, which PATCHes and sets
 * locally — this component never writes in response to those (the
 * server cache updates optimistically, so server === local and both
 * effects below no-op).
 *
 * Mount: inside AuthProvider, gated on an authenticated user — the
 * settings query 401s otherwise.
 */

import { useEffect, useRef } from "react";
import { useAuth } from "@/lib/auth";
import { useLocale, getStoredLocale, type Locale } from "@/i18n";
import { useSettings, useUpdateSettings } from "@/hooks/use-settings";

export function LocaleServerSync() {
  const { user } = useAuth();
  return user ? <LocaleServerSyncInner /> : null;
}

function LocaleServerSyncInner() {
  const { locale, applyServerLocale } = useLocale();
  const { data: settings } = useSettings();
  const updateSettings = useUpdateSettings();
  // One-shot migration guard: without it, a slow PATCH (server value
  // still "" on refetch) would re-fire the migration.
  const migratedRef = useRef(false);

  const serverLocale = settings?.locale;
  const settingsLoaded = settings !== undefined;

  // Server → local.
  useEffect(() => {
    if (serverLocale === "en" || serverLocale === "zh") {
      if (serverLocale !== locale) applyServerLocale(serverLocale);
    }
  }, [serverLocale, locale, applyServerLocale]);

  // One-time migration: explicit local choice, server unset.
  const mutateRef = useRef(updateSettings.mutate);
  mutateRef.current = updateSettings.mutate;
  useEffect(() => {
    if (!settingsLoaded || migratedRef.current) return;
    if (serverLocale === "en" || serverLocale === "zh") return;
    const stored: Locale | null = getStoredLocale();
    if (stored) {
      migratedRef.current = true;
      mutateRef.current({ locale: stored });
    }
  }, [settingsLoaded, serverLocale]);

  return null;
}
