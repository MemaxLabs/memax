"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowLeft, ArchiveRestore, Archive, Trash2 } from "lucide-react";
import { Badge, Button, Skeleton } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import {
  useAdminCampaignTemplates,
  useDeleteAdminCampaignTemplate,
  useSetAdminCampaignTemplateArchived,
} from "@/hooks/use-admin-campaign-templates";
import type { AdminCampaignTemplate } from "@/lib/admin-client";

/**
 * Campaign templates management page. Archive/unarchive/delete is the
 * full admin surface — creation happens via "Save as template" from
 * the campaign form, which is where admins naturally have the content
 * to save. A dedicated Create button here would duplicate the surface
 * without adding founder value.
 *
 * Toggle at the top reveals archived templates so admins can
 * unarchive; delete is gated behind a two-click confirm-button pattern
 * (matches the scaffold picker's overwrite confirm).
 */
export default function CampaignTemplatesManagementPage() {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns.campaignTemplates;

  const [includeArchived, setIncludeArchived] = useState(false);
  const listQ = useAdminCampaignTemplates(includeArchived);
  const archiveMut = useSetAdminCampaignTemplateArchived();
  const deleteMut = useDeleteAdminCampaignTemplate();

  const [pendingDelete, setPendingDelete] = useState<string | null>(null);

  const templates = listQ.data?.templates ?? [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-2">
        <Link
          href="/admin/communications/campaigns"
          className="inline-flex items-center gap-1 text-[12.5px] text-fg-2 hover:text-fg-1"
        >
          <ArrowLeft className="h-3 w-3" />
          {copy.backToCampaigns}
        </Link>
        <label className="inline-flex items-center gap-2 text-[12.5px] text-fg-2">
          <input
            type="checkbox"
            checked={includeArchived}
            onChange={(e) => setIncludeArchived(e.target.checked)}
            className="h-3.5 w-3.5 cursor-pointer rounded border-border/60 accent-fg-1"
          />
          {copy.showArchived}
        </label>
      </div>

      <header>
        <h2 className="text-[20px] font-semibold tracking-tight text-fg-1">
          {copy.title}
        </h2>
        <p className="mt-1 max-w-2xl text-[13.5px] leading-relaxed text-fg-2">
          {copy.description}
        </p>
      </header>

      {listQ.isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      ) : listQ.isError ? (
        <div className="rounded-surface border border-destructive/30 bg-destructive/8 p-4 text-[13px] text-destructive">
          {copy.loadError}
        </div>
      ) : templates.length === 0 ? (
        <div className="rounded-surface border border-border/50 bg-card p-6 text-[13px] text-fg-2">
          {includeArchived ? copy.emptyAll : copy.emptyActive}
        </div>
      ) : (
        <ul className="space-y-3">
          {templates.map((tmpl) => (
            <TemplateRow
              key={tmpl.id}
              tmpl={tmpl}
              onToggleArchive={() =>
                archiveMut.mutate({
                  id: tmpl.id,
                  archived: !tmpl.is_archived,
                })
              }
              onDelete={() => {
                if (pendingDelete === tmpl.id) {
                  deleteMut.mutate(tmpl.id);
                  setPendingDelete(null);
                } else {
                  setPendingDelete(tmpl.id);
                }
              }}
              onCancelDelete={() => setPendingDelete(null)}
              // Delete is only offered for archived rows — hard-delete
              // on an active row is a recoveryless footgun. Admin must
              // archive first (one click), then delete.
              deleteAvailable={tmpl.is_archived}
              busy={archiveMut.isPending || deleteMut.isPending}
              confirmingDelete={pendingDelete === tmpl.id}
              labels={{
                archived: copy.archivedBadge,
                archive: copy.archive,
                unarchive: copy.unarchive,
                delete: copy.delete,
                confirmDelete: copy.confirmDelete,
                cancel: copy.cancel,
                slugPrefix: copy.slugPrefix,
                noSubject: copy.noSubject,
                updatedAt: copy.updatedAt,
              }}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function TemplateRow({
  tmpl,
  onToggleArchive,
  onDelete,
  onCancelDelete,
  deleteAvailable,
  busy,
  confirmingDelete,
  labels,
}: {
  tmpl: AdminCampaignTemplate;
  onToggleArchive: () => void;
  onDelete: () => void;
  onCancelDelete: () => void;
  deleteAvailable: boolean;
  busy: boolean;
  confirmingDelete: boolean;
  labels: {
    archived: string;
    archive: string;
    unarchive: string;
    delete: string;
    confirmDelete: string;
    cancel: string;
    slugPrefix: string;
    noSubject: string;
    updatedAt: string;
  };
}) {
  return (
    <li className="rounded-surface border border-border/50 bg-card p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-[14px] font-semibold text-fg-1">{tmpl.name}</h3>
            {tmpl.is_archived ? (
              <Badge
                variant="outline"
                className="border-amber-500/40 bg-amber-500/10 text-[11px] text-amber-700 dark:text-amber-300"
              >
                {labels.archived}
              </Badge>
            ) : null}
          </div>
          <p className="mt-0.5 font-mono text-[11.5px] text-fg-3">
            {labels.slugPrefix}
            {tmpl.slug}
          </p>
          {tmpl.description ? (
            <p className="mt-1.5 text-[12.5px] text-fg-2">{tmpl.description}</p>
          ) : null}
          <p className="mt-1.5 text-[12px] text-fg-3">
            {tmpl.subject || labels.noSubject}
          </p>
          <p className="mt-1 text-[11.5px] text-fg-4">
            {labels.updatedAt.replace(
              "{time}",
              new Date(tmpl.updated_at).toLocaleString(),
            )}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={onToggleArchive}
            disabled={busy}
            className="cursor-pointer"
          >
            {tmpl.is_archived ? (
              <>
                <ArchiveRestore className="mr-1 h-3.5 w-3.5" />
                {labels.unarchive}
              </>
            ) : (
              <>
                <Archive className="mr-1 h-3.5 w-3.5" />
                {labels.archive}
              </>
            )}
          </Button>
          {deleteAvailable ? (
            confirmingDelete ? (
              <>
                <Button
                  type="button"
                  onClick={onDelete}
                  disabled={busy}
                  className="cursor-pointer"
                >
                  <Trash2 className="mr-1 h-3.5 w-3.5" />
                  {labels.confirmDelete}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  onClick={onCancelDelete}
                  disabled={busy}
                  className="cursor-pointer"
                >
                  {labels.cancel}
                </Button>
              </>
            ) : (
              <Button
                type="button"
                variant="outline"
                onClick={onDelete}
                disabled={busy}
                className="cursor-pointer"
              >
                <Trash2 className="mr-1 h-3.5 w-3.5" />
                {labels.delete}
              </Button>
            )
          ) : null}
        </div>
      </div>
    </li>
  );
}
