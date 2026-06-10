"use client";

import { Button } from "@memaxlabs/ui";
import type { AdminEmailTemplateDetail } from "@/lib/admin-client";
import { useLocale } from "@/i18n";

const VISUAL_EDITOR_KIND = "react_email_editor";

export function EmailTemplateSidebar({
  detail,
  editorKind,
  sampleData,
  onSampleDataChange,
  recipientEmail,
  onRecipientEmailChange,
  onSend,
  sendDisabled,
}: {
  detail: AdminEmailTemplateDetail;
  editorKind: string;
  sampleData: Record<string, string>;
  onSampleDataChange: (
    updater: (current: Record<string, string>) => Record<string, string>,
  ) => void;
  recipientEmail: string;
  onRecipientEmailChange: (next: string) => void;
  onSend: () => void;
  sendDisabled: boolean;
}) {
  const { t, locale } = useLocale();
  const revisions = detail.revisions ?? [];

  return (
    <div className="space-y-4">
      <div className="rounded-surface border border-border/50 bg-surface-1 p-4">
        <p className="text-[13px] font-medium text-fg-2">
          {t.admin.email.variablesTitle}
        </p>
        <div className="mt-3 space-y-3">
          {detail.variables.map((variable) => (
            <div
              key={variable.key}
              className="rounded-surface border border-border/40 bg-card p-3"
            >
              <div className="flex items-center justify-between gap-2">
                <code className="text-[12px] text-fg-2">{`{{.${variable.key}}}`}</code>
              </div>
              <p className="mt-1 text-[12px] text-fg-3">
                {variable.description}
              </p>
              <input
                value={sampleData[variable.key] ?? ""}
                onChange={(event) =>
                  onSampleDataChange((current) => ({
                    ...current,
                    [variable.key]: event.target.value,
                  }))
                }
                className="mt-2 w-full rounded-lg border border-border/50 bg-background px-3 py-2 text-[12px] text-fg-2 outline-none focus:border-fg-3"
              />
            </div>
          ))}
        </div>
      </div>

      <div className="rounded-surface border border-border/50 bg-surface-1 p-4">
        <p className="text-[13px] font-medium text-fg-2">
          {t.admin.email.defaultsTitle}
        </p>
        <p className="mt-1 text-[12px] text-fg-3">
          {t.admin.email.defaultsDescription}
        </p>
        <div className="mt-3 grid gap-2 text-[12px] text-fg-3">
          <div className="rounded-lg border border-border/40 bg-card px-3 py-2">
            {t.admin.email.defaultSubjectLabel}: {detail.default_subject}
          </div>
          <div className="rounded-lg border border-border/40 bg-card px-3 py-2">
            {t.admin.email.editorKindLabel}:{" "}
            {editorKind === VISUAL_EDITOR_KIND
              ? t.admin.email.mode.visual
              : t.admin.email.mode.source}
          </div>
        </div>
      </div>

      <div className="rounded-surface border border-border/50 bg-surface-1 p-4">
        <p className="text-[13px] font-medium text-fg-2">
          {t.admin.email.revisionsTitle}
        </p>
        <div className="mt-3 space-y-2">
          {revisions.length === 0 ? (
            <p className="text-[12px] text-fg-3">
              {t.admin.email.revisionsEmpty}
            </p>
          ) : (
            revisions.map((revision) => (
              <div
                key={revision.id}
                className="rounded-lg border border-border/40 bg-card px-3 py-2"
              >
                <div className="flex items-center justify-between gap-2 text-[12px]">
                  <span className="font-medium text-fg-2">
                    {revision.action === "reset"
                      ? t.admin.email.revisionActions.reset
                      : t.admin.email.revisionActions.saved}
                  </span>
                  <span className="text-fg-3">
                    {new Intl.DateTimeFormat(locale, {
                      dateStyle: "medium",
                      timeStyle: "short",
                    }).format(new Date(revision.created_at))}
                  </span>
                </div>
                <p className="mt-1 line-clamp-2 text-[12px] text-fg-3">
                  {revision.subject}
                </p>
              </div>
            ))
          )}
        </div>
      </div>

      <div className="rounded-surface border border-border/50 bg-surface-1 p-4">
        <p className="text-[13px] font-medium text-fg-2">
          {t.admin.email.sendTitle}
        </p>
        <p className="mt-1 text-[12px] text-fg-3">
          {t.admin.email.sendDescription}
        </p>
        <label className="mt-3 block">
          <span className="mb-2 block text-[12px] font-medium text-fg-3">
            {t.admin.email.recipientLabel}
          </span>
          <input
            value={recipientEmail}
            onChange={(event) => onRecipientEmailChange(event.target.value)}
            placeholder={t.admin.email.recipientPlaceholder}
            className="w-full rounded-lg border border-border/50 bg-card px-3 py-2 text-[12px] text-fg-2 outline-none focus:border-fg-3"
          />
        </label>
        <Button
          className="mt-3 w-full"
          variant="outline"
          onClick={onSend}
          disabled={sendDisabled}
        >
          {t.admin.email.actions.send}
        </Button>
      </div>
    </div>
  );
}
