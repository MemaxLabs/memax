"use client";

import { useAuth } from "@/lib/auth";
import { useLocale } from "@/i18n";
import {
  isImpersonating,
  stopImpersonating,
  getOriginalUserName,
} from "@/lib/impersonation";

/**
 * Persistent top banner shown when the dev is impersonating another user.
 * Renders a fixed bar at the top of the viewport with the impersonated user's
 * identity and a "Stop" button that restores the original session.
 */
export function ImpersonationBar() {
  const { user } = useAuth();
  const { t } = useLocale();

  if (!isImpersonating()) return null;

  const targetName = user?.name ?? user?.email ?? t.dev.impersonateUnknown;
  const callerName = getOriginalUserName() ?? "";
  const bannerText = t.dev.impersonateBanner
    .replace("{target}", targetName)
    .replace("{caller}", callerName);

  return (
    <div className="fixed inset-x-0 top-0 z-toast flex items-center justify-center gap-3 bg-amber-500/90 backdrop-blur-sm px-4 py-1.5 text-sm text-black font-medium">
      <span>{bannerText}</span>
      <button
        onClick={() => stopImpersonating()}
        className="rounded-md bg-black/20 px-2.5 py-0.5 text-xs font-semibold hover:bg-black/30 transition-colors cursor-pointer"
      >
        {t.dev.impersonateStop}
      </button>
    </div>
  );
}
