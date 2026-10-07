"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { useLocale } from "@/i18n";
import { useCreateAdminAudience } from "@/hooks/use-admin-audiences";
import { AudienceForm } from "@/components/features/admin/communications/audience-form";
import { useState } from "react";

export default function NewAudiencePage() {
  const { t } = useLocale();
  const copy = t.admin.communications.audiences;
  const router = useRouter();
  const create = useCreateAdminAudience();
  const [error, setError] = useState<string | null>(null);

  return (
    <div className="space-y-5">
      <div>
        <Link
          href="/admin/communications/audiences"
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

      <AudienceForm
        mode="create"
        pending={create.isPending}
        submitError={error}
        onCancel={() => router.push("/admin/communications/audiences")}
        onSubmit={async (params) => {
          setError(null);
          try {
            const created = await create.mutateAsync(params);
            router.push(`/admin/communications/audiences/${created.id}`);
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />
    </div>
  );
}
