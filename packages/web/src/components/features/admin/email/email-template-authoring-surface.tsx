"use client";

import type { EmailEditorRef } from "@react-email/editor";
import { EditorField, ModeButton } from "./email-template-form-primitives";
import { ReactEmailVisualEditor } from "./react-email-visual-editor";

const VISUAL_EDITOR_KIND = "react_email_editor";
const SOURCE_EDITOR_KIND = "source";

export function EmailTemplateAuthoringSurface({
  subject,
  onSubjectChange,
  notes,
  onNotesChange,
  html,
  onHTMLChange,
  text,
  onTextChange,
  editorKind,
  onModeChange,
  visualEditorRef,
  visualSeedState,
  visualVersion,
  visualPreviewText,
  visualPlaceholder,
  visualSnapshot,
  initialVisualSnapshot,
  onVisualSnapshotChange,
  detailName,
  labels,
}: {
  subject: string;
  onSubjectChange: (next: string) => void;
  notes: string;
  onNotesChange: (next: string) => void;
  html: string;
  onHTMLChange: (next: string) => void;
  text: string;
  onTextChange: (next: string) => void;
  editorKind: string;
  onModeChange: (nextKind: string) => void;
  visualEditorRef: React.RefObject<EmailEditorRef | null>;
  visualSeedState?: Record<string, unknown>;
  visualVersion: number;
  visualPreviewText: string;
  visualPlaceholder: string;
  visualSnapshot: string;
  initialVisualSnapshot: string;
  onVisualSnapshotChange: (snapshot: string, isInitial: boolean) => void;
  detailName: string;
  labels: {
    modeTitle: string;
    modeDescription: string;
    visualMode: string;
    sourceMode: string;
    subject: string;
    html: string;
    text: string;
    notes: string;
    visualTitle: string;
    visualDescription: string;
    generatedTextTitle: string;
    generatedTextDescription: string;
  };
}) {
  const isVisualMode = editorKind === VISUAL_EDITOR_KIND;

  return (
    <div className="space-y-4">
      <div className="rounded-surface border border-border/50 bg-surface-1 p-4">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <p className="text-[13px] font-medium text-fg-2">
              {labels.modeTitle}
            </p>
            <p className="mt-1 text-[12px] text-fg-4">
              {labels.modeDescription}
            </p>
          </div>

          <div className="flex gap-2 rounded-surface border border-border/50 bg-card p-1">
            <ModeButton
              active={editorKind === VISUAL_EDITOR_KIND}
              label={labels.visualMode}
              onClick={() => onModeChange(VISUAL_EDITOR_KIND)}
            />
            <ModeButton
              active={editorKind === SOURCE_EDITOR_KIND}
              label={labels.sourceMode}
              onClick={() => onModeChange(SOURCE_EDITOR_KIND)}
            />
          </div>
        </div>
      </div>

      <EditorField
        label={labels.subject}
        value={subject}
        onChange={onSubjectChange}
        rows={3}
      />

      {isVisualMode ? (
        <div className="space-y-4">
          <div className="rounded-surface border border-border/50 bg-surface-1 p-4">
            <p className="text-[13px] font-medium text-fg-2">
              {labels.visualTitle}
            </p>
            <p className="mt-1 text-[12px] text-fg-4">
              {labels.visualDescription}
            </p>
          </div>

          <ReactEmailVisualEditor
            editorRef={visualEditorRef}
            editorState={visualSeedState}
            htmlFallback={html}
            placeholder={visualPlaceholder}
            editorKey={`${detailName}:${visualVersion}`}
            onSnapshotChange={(snapshot) =>
              onVisualSnapshotChange(
                snapshot,
                initialVisualSnapshot === "" && visualSnapshot === "",
              )
            }
          />

          <div className="rounded-surface border border-border/50 bg-surface-1 p-4">
            <p className="text-[13px] font-medium text-fg-2">
              {labels.generatedTextTitle}
            </p>
            <p className="mt-1 text-[12px] text-fg-4">
              {labels.generatedTextDescription}
            </p>
            <pre className="mt-3 max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-surface border border-border/40 bg-card px-3 py-3 text-[12px] leading-5 text-fg-3">
              {visualPreviewText}
            </pre>
          </div>
        </div>
      ) : (
        <div className="space-y-4">
          <EditorField
            label={labels.html}
            value={html}
            onChange={onHTMLChange}
            rows={18}
          />
          <EditorField
            label={labels.text}
            value={text}
            onChange={onTextChange}
            rows={12}
          />
        </div>
      )}

      <EditorField
        label={labels.notes}
        value={notes}
        onChange={onNotesChange}
        rows={4}
      />
    </div>
  );
}
