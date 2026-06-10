"use client";

import type { CampaignKind } from "./campaign-scenario-picker";
import { AIAssistButton } from "./ai-assist-button";

const inputClass =
  "w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10";

function LabeledField({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block space-y-1.5">
      <span className="block text-[12px] font-medium text-fg-2">{label}</span>
      {children}
      {hint ? (
        <span className="block text-[11px] text-fg-3">{hint}</span>
      ) : null}
    </label>
  );
}

export function CampaignContentFields({
  kind,
  announcement,
  gift,
  email,
  labels,
}: {
  kind: CampaignKind;
  announcement: {
    title: string;
    body: string;
    link: string;
    linkText: string;
    onTitleChange: (v: string) => void;
    onBodyChange: (v: string) => void;
    onLinkChange: (v: string) => void;
    onLinkTextChange: (v: string) => void;
  };
  gift: {
    senderDisplay: string;
    token: string;
    url: string;
    expiresAt: string;
    onSenderDisplayChange: (v: string) => void;
    onTokenChange: (v: string) => void;
    onUrlChange: (v: string) => void;
    onExpiresAtChange: (v: string) => void;
  };
  email: {
    subject: string;
    bodyHtml: string;
    bodyText: string;
    onSubjectChange: (v: string) => void;
    onBodyHtmlChange: (v: string) => void;
    onBodyTextChange: (v: string) => void;
  };
  labels: {
    sectionTitle: string;
    sectionDescription: string;
    announcement: {
      titleLabel: string;
      titlePlaceholder: string;
      bodyLabel: string;
      bodyPlaceholder: string;
      linkLabel: string;
      linkPlaceholder: string;
      linkTextLabel: string;
      linkTextPlaceholder: string;
    };
    gift: {
      senderLabel: string;
      senderPlaceholder: string;
      tokenLabel: string;
      tokenPlaceholder: string;
      urlLabel: string;
      urlPlaceholder: string;
      expiresLabel: string;
      expiresPlaceholder: string;
      expiresHint: string;
    };
    email: {
      subjectLabel: string;
      subjectPlaceholder: string;
      bodyHtmlLabel: string;
      bodyHtmlPlaceholder: string;
      bodyHtmlHint: string;
      bodyTextLabel: string;
      bodyTextPlaceholder: string;
    };
  };
}) {
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-[13px] font-semibold text-fg-1">
          {labels.sectionTitle}
        </h3>
        <p className="mt-0.5 text-[12.5px] text-fg-2">
          {labels.sectionDescription}
        </p>
      </div>

      {kind === "email_announcement" ? (
        <div className="space-y-3">
          <LabeledField label={labels.email.subjectLabel}>
            <input
              value={email.subject}
              onChange={(e) => email.onSubjectChange(e.target.value)}
              placeholder={labels.email.subjectPlaceholder}
              className={inputClass}
            />
            <div className="mt-2">
              <AIAssistButton
                fieldKind="subject"
                current={email.subject}
                onApply={email.onSubjectChange}
              />
            </div>
          </LabeledField>
          <LabeledField
            label={labels.email.bodyHtmlLabel}
            hint={labels.email.bodyHtmlHint}
          >
            <textarea
              value={email.bodyHtml}
              onChange={(e) => email.onBodyHtmlChange(e.target.value)}
              placeholder={labels.email.bodyHtmlPlaceholder}
              rows={8}
              className={`${inputClass} font-mono text-[12.5px]`}
            />
            <div className="mt-2">
              <AIAssistButton
                fieldKind="body_html"
                current={email.bodyHtml}
                onApply={email.onBodyHtmlChange}
              />
            </div>
          </LabeledField>
          <LabeledField label={labels.email.bodyTextLabel}>
            <textarea
              value={email.bodyText}
              onChange={(e) => email.onBodyTextChange(e.target.value)}
              placeholder={labels.email.bodyTextPlaceholder}
              rows={5}
              className={inputClass}
            />
            <div className="mt-2">
              <AIAssistButton
                fieldKind="body_text"
                current={email.bodyText}
                onApply={email.onBodyTextChange}
              />
            </div>
          </LabeledField>
        </div>
      ) : kind === "system_notice" ? (
        <div className="space-y-3">
          <LabeledField label={labels.announcement.titleLabel}>
            <input
              value={announcement.title}
              onChange={(e) => announcement.onTitleChange(e.target.value)}
              placeholder={labels.announcement.titlePlaceholder}
              className={inputClass}
            />
          </LabeledField>
          <LabeledField label={labels.announcement.bodyLabel}>
            <textarea
              value={announcement.body}
              onChange={(e) => announcement.onBodyChange(e.target.value)}
              placeholder={labels.announcement.bodyPlaceholder}
              rows={4}
              className={inputClass}
            />
          </LabeledField>
          <div className="grid gap-2 sm:grid-cols-[1fr_14rem]">
            <LabeledField label={labels.announcement.linkLabel}>
              <input
                value={announcement.link}
                onChange={(e) => announcement.onLinkChange(e.target.value)}
                placeholder={labels.announcement.linkPlaceholder}
                className={inputClass}
              />
            </LabeledField>
            <LabeledField label={labels.announcement.linkTextLabel}>
              <input
                value={announcement.linkText}
                onChange={(e) => announcement.onLinkTextChange(e.target.value)}
                placeholder={labels.announcement.linkTextPlaceholder}
                className={inputClass}
              />
            </LabeledField>
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          <LabeledField label={labels.gift.senderLabel}>
            <input
              value={gift.senderDisplay}
              onChange={(e) => gift.onSenderDisplayChange(e.target.value)}
              placeholder={labels.gift.senderPlaceholder}
              className={inputClass}
            />
          </LabeledField>
          <LabeledField label={labels.gift.tokenLabel}>
            <input
              value={gift.token}
              onChange={(e) => gift.onTokenChange(e.target.value)}
              placeholder={labels.gift.tokenPlaceholder}
              className={inputClass}
            />
          </LabeledField>
          <div className="grid gap-2 sm:grid-cols-[1fr_14rem]">
            <LabeledField label={labels.gift.urlLabel}>
              <input
                value={gift.url}
                onChange={(e) => gift.onUrlChange(e.target.value)}
                placeholder={labels.gift.urlPlaceholder}
                className={inputClass}
              />
            </LabeledField>
            <LabeledField
              label={labels.gift.expiresLabel}
              hint={labels.gift.expiresHint}
            >
              <input
                value={gift.expiresAt}
                onChange={(e) => gift.onExpiresAtChange(e.target.value)}
                placeholder={labels.gift.expiresPlaceholder}
                className={inputClass}
              />
            </LabeledField>
          </div>
        </div>
      )}
    </section>
  );
}
