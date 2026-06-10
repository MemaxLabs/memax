"use client";

import { use, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, AlertCircle } from "lucide-react";
import { Badge, Skeleton } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import {
  useAdminCampaign,
  useCancelAdminCampaign,
  useDeleteAdminCampaign,
  useScheduleAdminCampaign,
  useSendAdminCampaign,
  useTestSendAdminCampaign,
  useUpdateAdminCampaignDraft,
} from "@/hooks/use-admin-campaigns";
import type {
  AdminCampaign,
  AdminCampaignUpdateParams,
} from "@/lib/admin-client";
import { CampaignStatusBadge } from "@/components/features/admin/communications/campaign-status-badge";
import { CampaignAuditTimeline } from "@/components/features/admin/communications/campaign-audit-timeline";
import { CampaignLifecycleActions } from "@/components/features/admin/communications/campaign-lifecycle-actions";
import { CampaignForm } from "@/components/features/admin/communications/campaign-form";
import { CampaignDeliveryStatsPanel } from "@/components/features/admin/communications/campaign-delivery-stats";

export default function CampaignDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns;
  const router = useRouter();

  const { data, isLoading, error } = useAdminCampaign(id);
  const campaign = data?.campaign;

  const update = useUpdateAdminCampaignDraft(id);
  const send = useSendAdminCampaign();
  const schedule = useScheduleAdminCampaign(id);
  const cancel = useCancelAdminCampaign();
  const del = useDeleteAdminCampaign();
  const testSend = useTestSendAdminCampaign(id);

  const [editing, setEditing] = useState(false);
  const [mutationError, setMutationError] = useState<string | null>(null);

  const pending =
    update.isPending ||
    send.isPending ||
    schedule.isPending ||
    cancel.isPending ||
    del.isPending;

  if (error) {
    return (
      <div className="space-y-4">
        <BackLink />
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive">
          <AlertCircle className="h-3.5 w-3.5" />
          {copy.detail.loadError}
        </div>
      </div>
    );
  }

  if (isLoading || !campaign) {
    return (
      <div className="space-y-4">
        <BackLink />
        <Skeleton className="h-7 w-64" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  if (editing && campaign.status === "draft") {
    return (
      <div className="space-y-6">
        <BackLink />
        <h2 className="text-[18px] font-semibold tracking-tight text-fg-1">
          {copy.form.editTitle}
        </h2>
        <CampaignForm
          mode="edit"
          campaign={campaign}
          initial={{
            kind: campaign.kind,
            name: campaign.name,
            audienceID: campaign.audience_id,
            audienceSnapshot: campaign.audience_snapshot,
            content: (campaign.content ?? {}) as Record<string, unknown>,
          }}
          pending={update.isPending}
          submitError={mutationError}
          onCancel={() => setEditing(false)}
          onSubmit={async (params) => {
            setMutationError(null);
            try {
              await update.mutateAsync(params as AdminCampaignUpdateParams);
              setEditing(false);
            } catch (err) {
              setMutationError(
                err instanceof Error ? err.message : String(err),
              );
            }
          }}
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <BackLink />

      <header className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-[20px] font-semibold tracking-tight text-fg-1">
              {campaign.name}
            </h2>
            <CampaignStatusBadge status={campaign.status} />
          </div>
          <p className="mt-1 text-[13px] text-fg-2">
            {campaignKindLabel(campaign.kind, copy.kind)}
            {" · "}
            {copy.detail.updatedAt.replace(
              "{time}",
              new Date(campaign.updated_at).toLocaleString(),
            )}
          </p>
        </div>
      </header>

      {mutationError ? (
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive">
          <AlertCircle className="h-3.5 w-3.5" />
          {mutationError}
        </div>
      ) : null}

      <section className="space-y-4 rounded-surface border border-border/50 bg-card p-5">
        <h3 className="text-[14px] font-semibold text-fg-1">
          {copy.detail.summaryTitle}
        </h3>
        <SummaryFields campaign={campaign} />
      </section>

      <CampaignDeliveryStatsPanel
        campaignId={campaign.id}
        channel={campaign.channel}
        status={campaign.status}
      />

      <section className="space-y-3 rounded-surface border border-border/50 bg-card p-5">
        <h3 className="text-[14px] font-semibold text-fg-1">
          {copy.detail.actionsTitle}
        </h3>
        <CampaignLifecycleActions
          campaign={campaign}
          onEdit={() => setEditing(true)}
          pending={pending}
          onTestSend={
            campaign.channel === "email"
              ? async (to) => {
                  await testSend.mutateAsync({ to });
                }
              : undefined
          }
          onSendNow={async () => {
            setMutationError(null);
            try {
              await send.mutateAsync(campaign.id);
            } catch (err) {
              setMutationError(
                err instanceof Error ? err.message : String(err),
              );
            }
          }}
          onSchedule={async (scheduledAt) => {
            setMutationError(null);
            try {
              await schedule.mutateAsync({ scheduled_at: scheduledAt });
            } catch (err) {
              setMutationError(
                err instanceof Error ? err.message : String(err),
              );
            }
          }}
          onCancel={async () => {
            setMutationError(null);
            try {
              await cancel.mutateAsync(campaign.id);
            } catch (err) {
              setMutationError(
                err instanceof Error ? err.message : String(err),
              );
            }
          }}
          onDelete={async () => {
            setMutationError(null);
            try {
              await del.mutateAsync(campaign.id);
              router.push("/admin/communications/campaigns");
            } catch (err) {
              setMutationError(
                err instanceof Error ? err.message : String(err),
              );
            }
          }}
        />
      </section>

      <section className="space-y-3 rounded-surface border border-border/50 bg-card p-5">
        <h3 className="text-[14px] font-semibold text-fg-1">
          {copy.audit.title}
        </h3>
        <CampaignAuditTimeline entries={data?.audit ?? []} />
      </section>
    </div>
  );
}

function BackLink() {
  const { t } = useLocale();
  return (
    <Link
      href="/admin/communications/campaigns"
      className="inline-flex items-center gap-1 text-[12.5px] text-fg-2 hover:text-fg-1"
    >
      <ArrowLeft className="h-3 w-3" />
      {t.admin.communications.campaigns.form.backToList}
    </Link>
  );
}

function SummaryFields({ campaign }: { campaign: AdminCampaign }) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.detail;

  const rows: { label: string; value: React.ReactNode }[] = [];
  if (campaign.audience_snapshot) {
    const s = campaign.audience_snapshot;
    rows.push({
      label: copy.audience,
      value: (
        <span className="inline-flex items-center gap-2">
          <Badge variant="secondary" className="text-[11px]">
            {s.rule_type}
          </Badge>
          {s.audience_id ? (
            <span className="text-fg-2">
              {copy.fromSaved.replace("{id}", s.audience_id.slice(0, 8))}
            </span>
          ) : null}
        </span>
      ),
    });
  }
  if (campaign.scheduled_at) {
    rows.push({
      label: copy.scheduledAt,
      value: new Date(campaign.scheduled_at).toLocaleString(),
    });
  }
  if (campaign.send_started_at) {
    rows.push({
      label: copy.sendStartedAt,
      value: new Date(campaign.send_started_at).toLocaleString(),
    });
  }
  if (campaign.send_finished_at) {
    rows.push({
      label: copy.sendFinishedAt,
      value: new Date(campaign.send_finished_at).toLocaleString(),
    });
  }
  rows.push({
    label: copy.counts,
    value: copy.countsFormat
      .replace("{sent}", String(campaign.sent_count))
      .replace("{failed}", String(campaign.failed_count)),
  });
  if (campaign.error_message) {
    rows.push({
      label: copy.errorLabel,
      value: <span className="text-destructive">{campaign.error_message}</span>,
    });
  }

  return (
    <dl className="grid gap-y-2 text-[13px] sm:grid-cols-[160px_1fr]">
      {rows.map((row) => (
        <div key={row.label} className="contents">
          <dt className="text-fg-2">{row.label}</dt>
          <dd className="text-fg-1">{row.value}</dd>
        </div>
      ))}
      <div className="contents">
        <dt className="text-fg-2">{copy.contentLabel}</dt>
        <dd>
          <pre className="overflow-x-auto whitespace-pre-wrap rounded-md bg-surface-1 px-2 py-1.5 text-[11.5px] leading-relaxed text-fg-2">
            {JSON.stringify(campaign.content, null, 2)}
          </pre>
        </dd>
      </div>
    </dl>
  );
}

function campaignKindLabel(
  kind: string,
  labels: { announcement: string; gift: string; email: string },
): string {
  switch (kind) {
    case "system_notice":
      return labels.announcement;
    case "gift_invite_link":
      return labels.gift;
    case "email_announcement":
      return labels.email;
    default:
      return kind;
  }
}
