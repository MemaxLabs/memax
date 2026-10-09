"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { useLocale } from "@/i18n";
import { useCreateAdminCampaign } from "@/hooks/use-admin-campaigns";
import { CampaignForm } from "@/components/features/admin/communications/campaign-form";
import type { AdminCampaignCreateParams } from "@/lib/admin-client";
import { useState } from "react";

export default function NewCampaignPage() {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns;
  const router = useRouter();
  const create = useCreateAdminCampaign();
  const [error, setError] = useState<string | null>(null);

  return (
    <div className="space-y-6">
      <div>
        <Link
          href="/admin/communications/campaigns"
          className="inline-flex items-center gap-1 text-[12.5px] text-fg-2 hover:text-fg-1"
        >
          <ArrowLeft className="h-3 w-3" />
          {copy.form.backToList}
        </Link>
        <h2 className="mt-2 text-[18px] font-semibold tracking-tight text-fg-1">
          {copy.form.newTitle}
        </h2>
        <p className="mt-1 max-w-2xl text-[13.5px] text-fg-2">
          {copy.form.newDescription}
        </p>
      </div>

      <CampaignForm
        mode="create"
        pending={create.isPending}
        submitError={error}
        onCancel={() => router.push("/admin/communications/campaigns")}
        onSubmit={async (params) => {
          setError(null);
          try {
            const created = await create.mutateAsync(
              params as AdminCampaignCreateParams,
            );
            router.push(`/admin/communications/campaigns/${created.id}`);
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />
    </div>
  );
}
