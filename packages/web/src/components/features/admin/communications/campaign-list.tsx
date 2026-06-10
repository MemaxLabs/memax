"use client";

import Link from "next/link";
import { Badge, Button, Skeleton } from "@memaxlabs/ui";
import { Plus, AlertCircle, Layers } from "lucide-react";
import { useLocale } from "@/i18n";
import { useAdminCampaigns } from "@/hooks/use-admin-campaigns";
import type { AdminCampaign, AdminCampaignStatus } from "@/lib/admin-client";
import { useState } from "react";
import { CampaignStatusBadge } from "./campaign-status-badge";

const FILTERS: readonly ("all" | AdminCampaignStatus)[] = [
  "all",
  "draft",
  "scheduled",
  "sending",
  "sent",
  "cancelled",
  "failed",
] as const;

export function CampaignList() {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.list;
  const [filter, setFilter] = useState<"all" | AdminCampaignStatus>("all");

  const { data, isLoading, error } = useAdminCampaigns(
    filter === "all" ? undefined : { status: filter },
  );
  const campaigns = data?.campaigns ?? [];

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-[18px] font-semibold tracking-tight text-fg-1">
            {copy.title}
          </h2>
          <p className="mt-1 max-w-2xl text-[13.5px] text-fg-2">
            {copy.description}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Link href="/admin/communications/campaigns/templates">
            <Button variant="outline" className="cursor-pointer">
              <Layers className="mr-1 h-3.5 w-3.5" />
              {copy.manageTemplates}
            </Button>
          </Link>
          <Link href="/admin/communications/campaigns/new">
            <Button className="cursor-pointer">
              <Plus className="mr-1 h-3.5 w-3.5" />
              {copy.newButton}
            </Button>
          </Link>
        </div>
      </div>

      <div className="flex flex-wrap gap-1">
        {FILTERS.map((f) => {
          const active = filter === f;
          const label =
            f === "all"
              ? copy.filters.all
              : t.admin.communications.campaigns.status[f];
          return (
            <button
              key={f}
              type="button"
              onClick={() => setFilter(f)}
              className={`rounded-full px-3 py-1 text-[12.5px] transition-colors cursor-pointer ${
                active
                  ? "bg-foreground text-background font-medium"
                  : "bg-surface-1 text-fg-2 hover:bg-surface-2 hover:text-fg-1"
              }`}
            >
              {label}
            </button>
          );
        })}
      </div>

      {error ? (
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive">
          <AlertCircle className="h-3.5 w-3.5" />
          {copy.loadError}
        </div>
      ) : null}

      <div className="rounded-surface border border-border/50 bg-card">
        {isLoading ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        ) : campaigns.length === 0 ? (
          <p className="py-10 text-center text-[13px] text-fg-2">
            {copy.empty}
          </p>
        ) : (
          <ul className="divide-y divide-border/40">
            {campaigns.map((c) => (
              <CampaignListRow key={c.id} campaign={c} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function CampaignListRow({ campaign }: { campaign: AdminCampaign }) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns;
  const kindLabel =
    campaign.kind === "system_notice" ? copy.kind.announcement : copy.kind.gift;
  const audienceLabel = audienceSummary(campaign, t);

  return (
    <li>
      <Link
        href={`/admin/communications/campaigns/${campaign.id}`}
        className="flex items-center gap-4 px-5 py-4 transition-colors hover:bg-surface-1"
      >
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <p className="truncate text-[14px] font-semibold text-fg-1">
              {campaign.name}
            </p>
            <CampaignStatusBadge status={campaign.status} />
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[12px] text-fg-2">
            <Badge variant="secondary" className="text-[11px]">
              {kindLabel}
            </Badge>
            <span>{audienceLabel}</span>
            <span>
              {copy.list.sentLabel.replace(
                "{count}",
                String(campaign.sent_count),
              )}
            </span>
            <span className="text-fg-3">
              {new Date(campaign.updated_at).toLocaleString()}
            </span>
          </div>
        </div>
      </Link>
    </li>
  );
}

function audienceSummary(
  campaign: AdminCampaign,
  t: ReturnType<typeof useLocale>["t"],
): string {
  const copy = t.admin.communications.campaigns.audienceSummary;
  const snapshot = campaign.audience_snapshot;
  if (!snapshot) return copy.none;
  if (snapshot.rule_type === "all") return copy.all;
  if (snapshot.rule_type === "hub") {
    return copy.hub
      .replace("{id}", (snapshot.rule.hub_id ?? "").slice(0, 8))
      .replace("{role}", snapshot.rule.hub_member_role || "—");
  }
  const ids = snapshot.rule.user_ids ?? [];
  const emails = snapshot.rule.emails ?? [];
  return copy.users.replace("{count}", String(ids.length + emails.length));
}
