"use client";

import { Suspense, useEffect, useMemo } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  useAdminEmailTemplate,
  useAdminEmailTemplates,
} from "@/hooks/use-admin-email";
import { useLocale } from "@/i18n";
import { Skeleton } from "@memaxlabs/ui";
import { EmailTemplateList } from "@/components/features/admin/email/email-template-list";
import { EmailTemplateEditor } from "@/components/features/admin/email/email-template-editor";
import { CommunicationsSectionHeader } from "@/components/features/admin/communications/communications-section-header";

function TransactionalContent() {
  const { t } = useLocale();
  const router = useRouter();
  const searchParams = useSearchParams();
  const selectedTemplate = searchParams.get("template") ?? undefined;

  const { data: templatesData, isLoading: listLoading } =
    useAdminEmailTemplates();
  const templates = useMemo(
    () => templatesData?.templates ?? [],
    [templatesData?.templates],
  );

  useEffect(() => {
    if (!selectedTemplate && templates.length > 0) {
      const params = new URLSearchParams(searchParams.toString());
      params.set("template", templates[0].name);
      router.replace(
        `/admin/communications/transactional?${params.toString()}`,
      );
    }
  }, [router, searchParams, selectedTemplate, templates]);

  const { data: detail, isLoading: detailLoading } =
    useAdminEmailTemplate(selectedTemplate);

  return (
    <div className="space-y-6">
      <CommunicationsSectionHeader
        eyebrow={t.admin.communications.transactional.eyebrow}
        title={t.admin.communications.transactional.title}
        description={t.admin.communications.transactional.description}
      />

      <div className="grid gap-6 xl:grid-cols-[320px_minmax(0,1fr)]">
        <EmailTemplateList
          templates={templates}
          isLoading={listLoading}
          selectedName={selectedTemplate}
        />

        {detailLoading || !detail ? (
          <div className="rounded-surface border border-border/50 bg-card p-6">
            <Skeleton className="mb-4 h-6 w-48" />
            <Skeleton className="mb-3 h-10 w-full" />
            <Skeleton className="mb-3 h-40 w-full" />
            <Skeleton className="h-64 w-full" />
          </div>
        ) : (
          <EmailTemplateEditor detail={detail} />
        )}
      </div>
    </div>
  );
}

export default function TransactionalPage() {
  return (
    <Suspense
      fallback={
        <div className="space-y-6">
          <div>
            <Skeleton className="mb-2 h-3 w-24" />
            <Skeleton className="mb-2 h-7 w-56" />
            <Skeleton className="h-5 w-[32rem] max-w-full" />
          </div>
          <div className="grid gap-6 xl:grid-cols-[320px_minmax(0,1fr)]">
            <EmailTemplateList
              templates={[]}
              isLoading
              selectedName={undefined}
            />
            <div className="rounded-surface border border-border/50 bg-card p-6">
              <Skeleton className="mb-4 h-6 w-48" />
              <Skeleton className="mb-3 h-10 w-full" />
              <Skeleton className="mb-3 h-40 w-full" />
              <Skeleton className="h-64 w-full" />
            </div>
          </div>
        </div>
      }
    >
      <TransactionalContent />
    </Suspense>
  );
}
