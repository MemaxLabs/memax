"use client";

import { Skeleton } from "@memaxlabs/ui";
import {
  CheckCircle2,
  Clock,
  Mail,
  MailX,
  Send,
  UserMinus,
} from "lucide-react";
import { useLocale } from "@/i18n";
import { useAdminCampaignDeliveryStats } from "@/hooks/use-admin-campaigns";

// Delivered/opened/bounced/complained are populated by the Resend
// webhook handler. Tiles for those stay at 0 until webhook events land
// — the panel description tells operators why so "0 delivered" doesn't
// look broken.

export function CampaignDeliveryStatsPanel({
  campaignId,
  channel,
  status,
}: {
  campaignId: string;
  channel: string;
  status: string;
}) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.deliveryStats;
  const isEmail = channel === "email";
  const { data, isLoading, error } = useAdminCampaignDeliveryStats(
    campaignId,
    isEmail,
    status,
  );

  if (!isEmail) return null;

  if (isLoading) {
    return (
      <div className="rounded-surface border border-border/50 bg-card p-5">
        <Skeleton className="mb-3 h-4 w-32" />
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="rounded-surface border border-border/50 bg-card p-5">
        <p className="text-[13px] text-fg-2">{copy.loadError}</p>
      </div>
    );
  }

  const tiles = [
    { key: "total", icon: Mail, label: copy.total, value: data.total },
    { key: "queued", icon: Clock, label: copy.queued, value: data.queued },
    { key: "sent", icon: Send, label: copy.sent, value: data.sent },
    {
      key: "delivered",
      icon: CheckCircle2,
      label: copy.delivered,
      value: data.delivered,
    },
    {
      key: "suppressed",
      icon: UserMinus,
      label: copy.suppressed,
      value: data.suppressed,
    },
    { key: "bounced", icon: MailX, label: copy.bounced, value: data.bounced },
    { key: "failed", icon: MailX, label: copy.failed, value: data.failed },
  ];

  return (
    <section className="rounded-surface border border-border/50 bg-card p-5">
      <div className="mb-4">
        <h3 className="text-[14px] font-semibold tracking-tight text-fg-1">
          {copy.title}
        </h3>
        <p className="mt-0.5 text-[12.5px] text-fg-3">{copy.description}</p>
      </div>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4">
        {tiles.map((tile) => {
          const Icon = tile.icon;
          return (
            <div
              key={tile.key}
              className="rounded-lg border border-border/40 bg-surface-1 p-3"
            >
              <div className="flex items-center gap-1.5 text-[11.5px] font-medium text-fg-3">
                <Icon className="h-3 w-3 shrink-0" />
                <span>{tile.label}</span>
              </div>
              <p className="mt-1 text-[22px] font-semibold tabular-nums text-fg-1">
                {tile.value.toLocaleString()}
              </p>
            </div>
          );
        })}
      </div>
    </section>
  );
}
