"use client";

import "@react-email/editor/themes/default.css";

import { EmailEditor, type EmailEditorRef } from "@react-email/editor";

const VISUAL_DOCUMENT_KEY = "document";

export function ReactEmailVisualEditor({
  editorRef,
  editorState,
  htmlFallback,
  placeholder,
  editorKey,
  onSnapshotChange,
}: {
  editorRef: React.RefObject<EmailEditorRef | null>;
  editorState?: Record<string, unknown>;
  htmlFallback: string;
  placeholder: string;
  editorKey: string;
  onSnapshotChange: (snapshot: string) => void;
}) {
  const content = resolveEditorContent(editorState, htmlFallback);

  return (
    <div className="overflow-hidden rounded-surface border border-border/50 bg-white shadow-sm">
      <EmailEditor
        key={editorKey}
        ref={editorRef}
        content={content}
        theme="minimal"
        placeholder={placeholder}
        onReady={(editor) =>
          onSnapshotChange(
            JSON.stringify({
              [VISUAL_DOCUMENT_KEY]: editor.getJSON(),
            }),
          )
        }
        onChange={(editor) =>
          onSnapshotChange(
            JSON.stringify({
              [VISUAL_DOCUMENT_KEY]: editor.getJSON(),
            }),
          )
        }
        className="min-h-[560px]"
      />
    </div>
  );
}

function resolveEditorContent(
  editorState: Record<string, unknown> | undefined,
  htmlFallback: string,
) {
  if (
    editorState &&
    typeof editorState[VISUAL_DOCUMENT_KEY] === "object" &&
    editorState[VISUAL_DOCUMENT_KEY] !== null
  ) {
    return editorState[VISUAL_DOCUMENT_KEY];
  }
  return htmlFallback;
}
