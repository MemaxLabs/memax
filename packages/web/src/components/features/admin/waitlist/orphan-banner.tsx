"use client";

import { useMemo } from "react";
import Link from "next/link";
import { AlertTriangle, ArrowRight } from "lucide-react";
import { useLocale, useInterpolate } from "@/i18n";
import { useAdminOrphans } from "@/hooks/use-admin-orphans";

/**
 * OrphanBanner — surface the orphan-cohort count on pages where
 * operators do waitlist work. Renders nothing when the cohort is
 * empty (banner should never be noise).
 *
 * Scan window defaults to 60 days; the regression ran 2026-04-14
 * → 2026-04-20, so 60 days covers it with headroom for stragglers
 * reported out-of-band. Operators who want to scan further back
 * open the /admin/waitlist/orphans page and change the since
 * field there.
 *
 * Loading / error states stay silent. The main /admin/waitlist
 * surface is fully functional without the banner — a failed
 * orphan fetch shouldn't block normal waitlist work.
 */
export function OrphanBanner() {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  // `since` rounds to UTC midnight of "today minus 60 days" so the
  // React Query key is stable across renders within the same day
  // (a full ISO timestamp would drift every render, causing a
  // refetch loop and a cohort count that never settles).
  const since = useMemo(() => {
    const d = new Date(Date.now() - 60 * 24 * 60 * 60 * 1000);
    d.setUTCHours(0, 0, 0, 0);
    return d.toISOString();
  }, []);
  const { data, isLoading } = useAdminOrphans({ since, limit: 500 });

  if (isLoading || !data || data.candidates.length === 0) {
    return null;
  }

  const count = data.candidates.length;
  const template =
    count === 1
      ? t.admin.waitlist.orphans.bannerTitle
      : t.admin.waitlist.orphans.bannerTitlePlural;

  return (
    <Link
      href="/admin/waitlist/orphans"
      className="mb-6 flex items-center gap-3 rounded-surface border border-amber-500/30 bg-amber-500/[0.06] px-4 py-3 transition-colors duration-[var(--dur-fast)] ease-[var(--ease-spring)] hover:bg-amber-500/[0.10]"
    >
      <AlertTriangle
        className="h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400"
        aria-hidden
      />
      <span className="flex-1 text-[13px] text-fg-1">
        {interpolate(template, { count: String(count) })}
      </span>
      <span className="flex items-center gap-1 text-[13px] font-medium text-amber-700 dark:text-amber-300">
        {t.admin.waitlist.orphans.bannerCta}
        <ArrowRight className="h-3.5 w-3.5" aria-hidden />
      </span>
    </Link>
  );
}
