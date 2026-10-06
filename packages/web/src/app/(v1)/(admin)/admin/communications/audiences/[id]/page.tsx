"use client";

import { use, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { AlertCircle, ArrowLeft, Trash2 } from "lucide-react";
import { Button, Skeleton } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import {
  useAdminAudience,
  useDeleteAdminAudience,
  useUpdateAdminAudience,
} from "@/hooks/use-admin-audiences";
import { AudienceForm } from "@/components/features/admin/communications/audience-form";

export default function AudienceDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { t } = useLocale();
  const copy = t.admin.communications.audiences;
  const router = useRouter();

  const { data, isLoading, error } = useAdminAudience(id);
  const update = useUpdateAdminAudience(id);
  const del = useDeleteAdminAudience();

  const [editing, setEditing] = useState(false);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  if (isLoading) {
    return (
      <div className="space-y-4">
        <BackLink />
        <Skeleton className="h-7 w-64" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }
  if (error || !data) {
    return (
      <div className="space-y-4">
        <BackLink />
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive">
          <AlertCircle className="h-3.5 w-3.5" />
          {copy.loadError}
        </div>
      </div>
    );
  }

  if (editing) {
    return (
      <div className="space-y-5">
        <BackLink />
        <h2 className="text-[18px] font-semibold tracking-tight text-fg-1">
          {copy.form.editTitle}
        </h2>
        <AudienceForm
          mode="edit"
          initial={{
            name: data.name,
            description: data.description,
            ruleType: data.rule_type,
            rule: data.rule,
          }}
          pending={update.isPending}
          submitError={mutationError}
          onCancel={() => setEditing(false)}
          onSubmit={async (params) => {
            setMutationError(null);
            try {
              await update.mutateAsync(params);
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
    <div className="space-y-5">
      <BackLink />
      <header className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-[20px] font-semibold tracking-tight text-fg-1">
            {data.name}
          </h2>
          {data.description ? (
            <p className="mt-1 max-w-2xl text-[13.5px] text-fg-2">
              {data.description}
            </p>
          ) : null}
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() => setEditing(true)}
            className="cursor-pointer"
          >
            {copy.detail.edit}
          </Button>
          <Button
            variant="outline"
            onClick={() => setConfirmingDelete(true)}
            disabled={del.isPending}
            className="cursor-pointer"
          >
            <Trash2 className="mr-1 h-3.5 w-3.5" />
            {copy.detail.delete}
          </Button>
        </div>
      </header>

      {mutationError ? (
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive">
          <AlertCircle className="h-3.5 w-3.5" />
          {mutationError}
        </div>
      ) : null}

      <section className="rounded-surface border border-border/50 bg-card p-5">
        <h3 className="text-[14px] font-semibold text-fg-1">
          {copy.detail.ruleTitle}
        </h3>
        <p className="mt-0.5 text-[12.5px] text-fg-2">
          {copy.ruleTypeLabel[data.rule_type]}
        </p>
        <pre className="mt-3 overflow-x-auto whitespace-pre-wrap rounded-md bg-surface-1 px-3 py-2 text-[12px] leading-relaxed text-fg-2">
          {JSON.stringify(data.rule, null, 2)}
        </pre>
      </section>

      {confirmingDelete ? (
        <div className="rounded-surface border border-amber-500/30 bg-amber-500/8 p-4">
          <p className="text-[13px] text-amber-700 dark:text-amber-300">
            {copy.detail.confirmDelete}
          </p>
          <div className="mt-3 flex gap-2">
            <Button
              onClick={async () => {
                setMutationError(null);
                try {
                  await del.mutateAsync(id);
                  router.push("/admin/communications/audiences");
                } catch (err) {
                  setMutationError(
                    err instanceof Error ? err.message : String(err),
                  );
                  setConfirmingDelete(false);
                }
              }}
              disabled={del.isPending}
              className="cursor-pointer"
            >
              {copy.detail.delete}
            </Button>
            <Button
              variant="outline"
              onClick={() => setConfirmingDelete(false)}
              disabled={del.isPending}
              className="cursor-pointer"
            >
              {copy.form.cancel}
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function BackLink() {
  const { t } = useLocale();
  return (
    <Link
      href="/admin/communications/audiences"
      className="inline-flex items-center gap-1 text-[12.5px] text-fg-2 hover:text-fg-1"
    >
      <ArrowLeft className="h-3 w-3" />
      {t.admin.communications.audiences.form.backToList}
    </Link>
  );
}
