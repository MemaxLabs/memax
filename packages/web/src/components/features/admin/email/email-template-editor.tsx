"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { EmailEditorRef } from "@react-email/editor";
import type { AdminEmailTemplateDetail } from "@/lib/admin-client";
import {
  usePreviewAdminEmailTemplate,
  usePublishAdminEmailTemplate,
  useResetAdminEmailTemplate,
  useSendAdminEmailTemplate,
  useUpdateAdminEmailTemplate,
} from "@/hooks/use-admin-email";
import { useLocale } from "@/i18n";
import { EmailTemplateAuthoringSurface } from "./email-template-authoring-surface";
import { EmailTemplateHeader } from "./email-template-header";
import { EmailTemplatePreview } from "./email-template-preview";
import { EmailTemplateSidebar } from "./email-template-sidebar";

const VISUAL_EDITOR_KIND = "react_email_editor";
const SOURCE_EDITOR_KIND = "source";

export function EmailTemplateEditor({
  detail,
}: {
  detail: AdminEmailTemplateDetail;
}) {
  const { t } = useLocale();
  const visualEditorRef = useRef<EmailEditorRef>(null);

  const [subject, setSubject] = useState(detail.effective_subject);
  const [html, setHtml] = useState(detail.effective_html);
  const [text, setText] = useState(detail.effective_text);
  const [notes, setNotes] = useState(detail.override?.notes ?? "");
  const [sampleData, setSampleData] = useState<Record<string, string>>(
    detail.preview.data,
  );
  const [recipientEmail, setRecipientEmail] = useState("");
  const [editorKind, setEditorKind] = useState(
    normalizeEditorKind(detail.effective_editor_kind),
  );
  const [visualSeedState, setVisualSeedState] = useState<
    Record<string, unknown> | undefined
  >(detail.override?.editor_state ?? undefined);
  const [visualSnapshot, setVisualSnapshot] = useState("");
  const [initialVisualSnapshot, setInitialVisualSnapshot] = useState("");
  const [visualPreviewText, setVisualPreviewText] = useState(
    detail.effective_text,
  );
  const [visualVersion, setVisualVersion] = useState(0);
  const [isSyncingVisual, setIsSyncingVisual] = useState(false);

  useEffect(() => {
    setSubject(detail.effective_subject);
    setHtml(detail.effective_html);
    setText(detail.effective_text);
    setNotes(detail.override?.notes ?? "");
    setSampleData(detail.preview.data);
    setRecipientEmail("");
    setEditorKind(normalizeEditorKind(detail.effective_editor_kind));
    setVisualSeedState(detail.override?.editor_state ?? undefined);
    setVisualSnapshot("");
    setInitialVisualSnapshot("");
    setVisualPreviewText(detail.effective_text);
    setVisualVersion(0);
  }, [detail]);

  const updateMutation = useUpdateAdminEmailTemplate(detail.name);
  const previewMutation = usePreviewAdminEmailTemplate(detail.name);
  const resetMutation = useResetAdminEmailTemplate(detail.name);
  const sendMutation = useSendAdminEmailTemplate(detail.name);
  const publishMutation = usePublishAdminEmailTemplate(detail.name);

  const preview = previewMutation.data ?? detail.preview;
  const isVisualMode = editorKind === VISUAL_EDITOR_KIND;
  const isBusy =
    isSyncingVisual ||
    updateMutation.isPending ||
    previewMutation.isPending ||
    resetMutation.isPending ||
    sendMutation.isPending ||
    publishMutation.isPending;

  // A published row exists when the server populated `published_at` and
  // the draft hasn't diverged from the live content. We drive the
  // "draft — not published" badge from the server's flag on the
  // override, or from local `isDirty` if the admin is mid-edit.
  const serverDraftPending = Boolean(
    detail.override &&
    (detail.override.draft_subject !== detail.override.subject ||
      detail.override.draft_html !== detail.override.html ||
      detail.override.draft_text !== detail.override.text),
  );

  const isDirty = useMemo(() => {
    if (subject !== detail.effective_subject) return true;
    if (notes !== (detail.override?.notes ?? "")) return true;
    if (editorKind !== normalizeEditorKind(detail.effective_editor_kind)) {
      return true;
    }
    if (!isVisualMode) {
      return html !== detail.effective_html || text !== detail.effective_text;
    }
    return (
      visualSnapshot !== "" &&
      initialVisualSnapshot !== "" &&
      visualSnapshot !== initialVisualSnapshot
    );
  }, [
    detail,
    editorKind,
    html,
    initialVisualSnapshot,
    isVisualMode,
    notes,
    subject,
    text,
    visualSnapshot,
  ]);

  async function resolveDraft() {
    if (!isVisualMode || !visualEditorRef.current) {
      return {
        subject,
        html,
        text,
        editor_kind: SOURCE_EDITOR_KIND,
        editor_state: undefined,
      };
    }

    setIsSyncingVisual(true);
    try {
      const exported = await visualEditorRef.current.export();
      const editorState = {
        document: visualEditorRef.current.getJSON() as Record<string, unknown>,
      };
      const snapshot = JSON.stringify(editorState);
      setHtml(exported.html);
      setText(exported.text);
      setVisualPreviewText(exported.text);
      setVisualSnapshot(snapshot);
      if (initialVisualSnapshot === "") {
        setInitialVisualSnapshot(snapshot);
      }
      return {
        subject,
        html: exported.html,
        text: exported.text,
        editor_kind: VISUAL_EDITOR_KIND,
        editor_state: editorState,
      };
    } finally {
      setIsSyncingVisual(false);
    }
  }

  async function handlePreview() {
    const draft = await resolveDraft();
    await previewMutation
      .mutateAsync({
        subject: draft.subject,
        html: draft.html,
        text: draft.text,
        sample_data: sampleData,
      })
      .catch(() => undefined);
  }

  async function handleSave() {
    const draft = await resolveDraft();
    await updateMutation
      .mutateAsync({
        subject: draft.subject,
        html: draft.html,
        text: draft.text,
        notes,
        editor_kind: draft.editor_kind,
        editor_state: draft.editor_state,
      })
      .catch(() => undefined);
  }

  async function handleModeChange(nextKind: string) {
    if (nextKind === editorKind || isBusy) {
      return;
    }
    if (nextKind === SOURCE_EDITOR_KIND && isVisualMode) {
      const draft = await resolveDraft();
      setHtml(draft.html);
      setText(draft.text);
    }
    if (nextKind === VISUAL_EDITOR_KIND) {
      setVisualSeedState(undefined);
      setVisualVersion((current) => current + 1);
    }
    setEditorKind(nextKind);
  }

  async function handleSend() {
    const draft = await resolveDraft();
    await sendMutation
      .mutateAsync({
        to: recipientEmail,
        subject: draft.subject,
        html: draft.html,
        text: draft.text,
        sample_data: sampleData,
      })
      .catch(() => undefined);
  }

  return (
    <section className="space-y-6">
      {sendMutation.data ? (
        <div className="rounded-surface border border-emerald-500/30 bg-emerald-500/8 px-4 py-3 text-[13px] text-fg-2">
          {t.admin.email.sendQueued
            .replace("{email}", sendMutation.data.to)
            .replace("{subject}", sendMutation.data.subject)}
        </div>
      ) : null}

      <div className="rounded-surface border border-border/50 bg-card p-6">
        <EmailTemplateHeader
          name={detail.name}
          description={detail.description}
          hasOverride={Boolean(detail.override)}
          hasUnpublishedDraft={serverDraftPending || isDirty}
          customLabel={t.admin.email.status.custom}
          defaultLabel={t.admin.email.status.default}
          draftBadgeLabel={t.admin.email.status.draftNotPublished}
          previewLabel={t.admin.email.actions.preview}
          resetLabel={t.admin.email.actions.reset}
          saveLabel={t.admin.email.actions.save}
          publishLabel={t.admin.email.actions.publish}
          onPreview={() => void handlePreview()}
          onReset={() => resetMutation.mutate()}
          onSave={() => void handleSave()}
          onPublish={() => publishMutation.mutate()}
          previewDisabled={isBusy}
          resetDisabled={resetMutation.isPending || !detail.override}
          saveDisabled={isBusy || !isDirty}
          // Publish is only meaningful when there's a saved draft that
          // differs from published. No draft → nothing to promote.
          publishDisabled={isBusy || !serverDraftPending || isDirty}
        />

        <div className="mt-6 grid gap-6 2xl:grid-cols-[minmax(0,1fr)_300px]">
          <EmailTemplateAuthoringSurface
            subject={subject}
            onSubjectChange={setSubject}
            notes={notes}
            onNotesChange={setNotes}
            html={html}
            onHTMLChange={setHtml}
            text={text}
            onTextChange={setText}
            editorKind={editorKind}
            onModeChange={(nextKind) => void handleModeChange(nextKind)}
            visualEditorRef={visualEditorRef}
            visualSeedState={visualSeedState}
            visualVersion={visualVersion}
            visualPreviewText={visualPreviewText}
            visualPlaceholder={t.admin.email.visualPlaceholder}
            visualSnapshot={visualSnapshot}
            initialVisualSnapshot={initialVisualSnapshot}
            onVisualSnapshotChange={(snapshot, isInitial) => {
              setVisualSnapshot(snapshot);
              if (isInitial) {
                setInitialVisualSnapshot(snapshot);
              }
            }}
            detailName={detail.name}
            labels={{
              modeTitle: t.admin.email.modeTitle,
              modeDescription: t.admin.email.modeDescription,
              visualMode: t.admin.email.mode.visual,
              sourceMode: t.admin.email.mode.source,
              subject: t.admin.email.fields.subject,
              html: t.admin.email.fields.html,
              text: t.admin.email.fields.text,
              notes: t.admin.email.fields.notes,
              visualTitle: t.admin.email.visualTitle,
              visualDescription: t.admin.email.visualDescription,
              generatedTextTitle: t.admin.email.generatedTextTitle,
              generatedTextDescription: t.admin.email.generatedTextDescription,
            }}
          />

          <EmailTemplateSidebar
            detail={detail}
            editorKind={editorKind}
            sampleData={sampleData}
            onSampleDataChange={(updater) => setSampleData(updater)}
            recipientEmail={recipientEmail}
            onRecipientEmailChange={setRecipientEmail}
            onSend={() => void handleSend()}
            sendDisabled={isBusy || recipientEmail.trim() === ""}
          />
        </div>
      </div>

      <EmailTemplatePreview preview={preview} />
    </section>
  );
}

function normalizeEditorKind(kind: string | undefined) {
  return kind === VISUAL_EDITOR_KIND ? VISUAL_EDITOR_KIND : SOURCE_EDITOR_KIND;
}
