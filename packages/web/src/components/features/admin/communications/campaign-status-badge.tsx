"use client";

import { useLocale } from "@/i18n";
import type { AdminCampaignStatus } from "@/lib/admin-client";

const styles: Record<AdminCampaignStatus, string> = {
  draft: "border-border/60 bg-surface-1 text-fg-2",
  scheduled: "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300",
  sending:
    "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  sent: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  cancelled: "border-border/60 bg-surface-1 text-fg-3",
  failed: "border-destructive/30 bg-destructive/8 text-destructive",
};

export function CampaignStatusBadge({
  status,
}: {
  status: AdminCampaignStatus;
}) {
  const { t } = useLocale();
  const label = t.admin.communications.campaigns.status[status];
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] font-medium uppercase tracking-wide ${styles[status]}`}
    >
      {label}
    </span>
  );
}
