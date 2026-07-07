"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useSettingsDialog } from "@/contexts/settings-dialog-context";

/**
 * `/settings` deep link — opens the settings dialog over the app.
 *
 * The dialog state lives in SettingsDialogProvider, mounted in
 * AppShell ABOVE the route tree, so it survives the redirect. Open it
 * synchronously in the effect, THEN navigate. The previous version
 * did the reverse with a 100ms setTimeout — router.replace unmounted
 * this page and the effect cleanup cleared the timer before it fired,
 * so the deep link landed on the overview with no dialog at all
 * (audit D5).
 */
export default function SettingsPage() {
  const router = useRouter();
  const { open } = useSettingsDialog();

  useEffect(() => {
    open({ kind: "you", section: "profile" });
    router.replace("/home");
  }, [router, open]);

  return null;
}
