"use client";

import { useRef, useState } from "react";
import { Button } from "@memaxlabs/ui";
import { AlertCircle } from "lucide-react";
import { useLocale } from "@/i18n";
import type {
  AdminAudienceSnapshot,
  AdminCampaign,
  AdminCampaignCreateParams,
  AdminCampaignUpdateParams,
} from "@/lib/admin-client";
import {
  CampaignScenarioPicker,
  type CampaignKind,
} from "./campaign-scenario-picker";
import { CampaignContentFields } from "./campaign-content-fields";
import {
  AudiencePicker,
  type AudiencePickerHandle,
  type AudiencePickerValue,
} from "./audience-picker";
import { CampaignTemplateScaffoldPicker } from "./campaign-template-scaffold-picker";
import { SaveAsCustomTemplateButton } from "./save-as-custom-template-button";

const inputClass =
  "w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10";

export interface CampaignFormInitial {
  kind?: CampaignKind;
  name?: string;
  audienceID?: string;
  audienceSnapshot?: AdminAudienceSnapshot;
  content?: Record<string, unknown>;
}

export function CampaignForm({
  initial,
  campaign,
  mode,
  onSubmit,
  onCancel,
  pending,
  submitError,
  secondaryAction,
}: {
  initial?: CampaignFormInitial;
  campaign?: AdminCampaign;
  mode: "create" | "edit";
  onSubmit: (
    params: AdminCampaignCreateParams | AdminCampaignUpdateParams,
  ) => Promise<void>;
  onCancel?: () => void;
  pending: boolean;
  submitError?: string | null;
  secondaryAction?: { label: string; onClick: () => void; disabled?: boolean };
}) {
  const { t } = useLocale();
  const copy = t.admin.communications.campaigns;

  const [kind, setKind] = useState<CampaignKind>(
    initial?.kind ?? "system_notice",
  );
  const [name, setName] = useState(initial?.name ?? "");

  const initialPicker: AudiencePickerValue = initial?.audienceID
    ? { audienceID: initial.audienceID }
    : initial?.audienceSnapshot
      ? { snapshot: initial.audienceSnapshot }
      : { snapshot: { rule_type: "users", rule: {} } };
  const [audience, setAudience] = useState<AudiencePickerValue>(initialPicker);
  // Flushed at the top of handleSubmit so mobile taps (where `blur` may
  // not fire before the form submit handler runs) don't lose the last
  // typed recipient. The picker parses synchronously and returns the
  // post-flush value — we use that directly rather than waiting for the
  // setAudience round-trip.
  const audienceRef = useRef<AudiencePickerHandle>(null);

  const c0 = (initial?.content ?? {}) as Record<string, string>;
  const [title, setTitle] = useState(c0.title ?? "");
  const [body, setBody] = useState(c0.body ?? "");
  const [link, setLink] = useState(c0.link ?? "");
  const [linkText, setLinkText] = useState(c0.link_text ?? "");

  // gift fields
  const initialGift =
    (c0 as unknown as {
      sender?: { display?: string };
      token?: string;
      url?: string;
      expires_at?: string;
    }) ?? {};
  const [senderDisplay, setSenderDisplay] = useState(
    initialGift.sender?.display ?? "memax team",
  );
  const [giftToken, setGiftToken] = useState(initialGift.token ?? "");
  const [giftUrl, setGiftUrl] = useState(initialGift.url ?? "");
  const [giftExpiresAt, setGiftExpiresAt] = useState(
    initialGift.expires_at ?? "",
  );

  // email_announcement content fields
  const initialEmail =
    (c0 as unknown as {
      subject?: string;
      body_html?: string;
      body_text?: string;
    }) ?? {};
  const [emailSubject, setEmailSubject] = useState(initialEmail.subject ?? "");
  const [emailBodyHtml, setEmailBodyHtml] = useState(
    initialEmail.body_html ?? "",
  );
  const [emailBodyText, setEmailBodyText] = useState(
    initialEmail.body_text ?? "",
  );

  const [formError, setFormError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);

    // Flush any in-progress recipient textarea BEFORE reading audience
    // state. On mobile, tapping a button inside the same form doesn't
    // always fire textarea.blur first, so without this the last typed
    // recipient can be silently dropped. flush() parses synchronously
    // and returns the resolved value we use for submission.
    const flushedAudience = audienceRef.current?.flush() ?? audience;

    if (!name.trim()) {
      setFormError(copy.form.errors.missingName);
      return;
    }

    let content: Record<string, unknown>;
    if (kind === "system_notice") {
      if (!title && !body) {
        setFormError(copy.errors.missingTitleOrBody);
        return;
      }
      content = { title, body };
      if (link) content.link = link;
      if (linkText) content.link_text = linkText;
    } else if (kind === "email_announcement") {
      if (
        !emailSubject.trim() ||
        !emailBodyHtml.trim() ||
        !emailBodyText.trim()
      ) {
        setFormError(copy.errors.missingEmailFields);
        return;
      }
      content = {
        subject: emailSubject,
        body_html: emailBodyHtml,
        body_text: emailBodyText,
      };
    } else {
      if (!giftToken) {
        setFormError(copy.errors.missingToken);
        return;
      }
      content = {
        sender: { id: "", display: senderDisplay },
        token: giftToken,
        expires_at:
          giftExpiresAt || new Date(Date.now() + 30 * 86400000).toISOString(),
      };
      if (giftUrl) content.url = giftUrl;
    }

    if (!flushedAudience.audienceID && !flushedAudience.snapshot) {
      setFormError(copy.form.errors.missingAudience);
      return;
    }

    const base = {
      name: name.trim(),
      audience_id: flushedAudience.audienceID,
      audience_snapshot: flushedAudience.snapshot,
      content,
    };
    if (mode === "create") {
      await onSubmit({
        ...base,
        kind,
        channel: kind === "email_announcement" ? "email" : "inapp",
      } satisfies AdminCampaignCreateParams);
    } else {
      await onSubmit(base satisfies AdminCampaignUpdateParams);
    }
  }

  const lockedKind = mode === "edit";
  const status = campaign?.status;
  const disableFields = pending || (mode === "edit" && status !== "draft");

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      {!lockedKind ? (
        <CampaignScenarioPicker
          value={kind}
          onChange={setKind}
          labels={copy.scenarios}
        />
      ) : null}

      <div className="space-y-5 rounded-surface border border-border/50 bg-card p-6">
        <label className="block space-y-1.5">
          <span className="block text-[12px] font-medium text-fg-2">
            {copy.form.nameLabel}
          </span>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={copy.form.namePlaceholder}
            className={inputClass}
            disabled={disableFields}
          />
        </label>

        <div>
          <h3 className="text-[13px] font-semibold text-fg-1">
            {copy.audience.sectionTitle}
          </h3>
          <p className="mb-3 mt-0.5 text-[12.5px] text-fg-2">
            {copy.audience.sectionDescription}
          </p>
          <AudiencePicker
            ref={audienceRef}
            value={audience}
            onChange={setAudience}
          />
        </div>

        <div className="h-px bg-border/40" />

        {/*
          Scaffold picker: only shown for email_announcement campaigns.
          Picks a transactional template (hub_invite, etc.) and copies
          its effective body into the campaign's inline content. After
          copy, the fields are independent — editing a template later
          doesn't mutate this campaign. Disabled on edit-mode locks and
          when pending.
        */}
        {kind === "email_announcement" && !disableFields ? (
          <CampaignTemplateScaffoldPicker
            currentContent={{
              subject: emailSubject,
              bodyHtml: emailBodyHtml,
              bodyText: emailBodyText,
            }}
            onScaffold={(next) => {
              setEmailSubject(next.subject);
              setEmailBodyHtml(next.bodyHtml);
              setEmailBodyText(next.bodyText);
            }}
          />
        ) : null}

        {kind === "email_announcement" && !disableFields ? (
          <SaveAsCustomTemplateButton
            disabled={false}
            currentContent={{
              subject: emailSubject,
              bodyHtml: emailBodyHtml,
              bodyText: emailBodyText,
            }}
          />
        ) : null}

        <CampaignContentFields
          kind={kind}
          announcement={{
            title,
            body,
            link,
            linkText,
            onTitleChange: setTitle,
            onBodyChange: setBody,
            onLinkChange: setLink,
            onLinkTextChange: setLinkText,
          }}
          gift={{
            senderDisplay,
            token: giftToken,
            url: giftUrl,
            expiresAt: giftExpiresAt,
            onSenderDisplayChange: setSenderDisplay,
            onTokenChange: setGiftToken,
            onUrlChange: setGiftUrl,
            onExpiresAtChange: setGiftExpiresAt,
          }}
          email={{
            subject: emailSubject,
            bodyHtml: emailBodyHtml,
            bodyText: emailBodyText,
            onSubjectChange: setEmailSubject,
            onBodyHtmlChange: setEmailBodyHtml,
            onBodyTextChange: setEmailBodyText,
          }}
          labels={copy.content}
        />
      </div>

      {formError || submitError ? (
        <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive">
          <AlertCircle className="h-3.5 w-3.5 shrink-0" />
          <span>{formError ?? submitError}</span>
        </div>
      ) : null}

      <div className="flex flex-wrap items-center justify-end gap-2">
        {onCancel ? (
          <Button
            type="button"
            variant="outline"
            onClick={onCancel}
            disabled={pending}
            className="cursor-pointer"
          >
            {copy.form.cancel}
          </Button>
        ) : null}
        {secondaryAction ? (
          <Button
            type="button"
            variant="outline"
            onClick={secondaryAction.onClick}
            disabled={secondaryAction.disabled || pending}
            className="cursor-pointer"
          >
            {secondaryAction.label}
          </Button>
        ) : null}
        <Button type="submit" disabled={pending} className="cursor-pointer">
          {pending
            ? copy.form.saving
            : mode === "create"
              ? copy.form.saveDraft
              : copy.form.saveEdit}
        </Button>
      </div>
    </form>
  );
}
