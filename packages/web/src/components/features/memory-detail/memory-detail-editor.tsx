"use client";

/**
 * `<MemoryDetailEditor>` — Tiptap-based body editor used inside
 * `<MemoryDetailView>` when the user enters edit mode (plan 21 phase 3).
 *
 * Uses the SAME extension config as compose modal via the shared
 * `markdownEditorExtensions()` factory + `getEditorMarkdown()`
 * helper, so the two surfaces stay byte-identical in capability +
 * serialization. The compose markdown round-trip golden tests at
 * `compose-markdown-roundtrip.dom.test.tsx` exercise that exact
 * config — both surfaces inherit the contract.
 *
 * Save flow:
 *   - Caller passes `initialContent` (the memory's current content).
 *   - On save, the editor calls `onSave(markdown)` with the
 *     serialized markdown. The caller drives the mutation (so this
 *     component stays pure-presentational and easy to test).
 *   - On cancel, the caller calls `onCancel()` — the editor doesn't
 *     persist anything.
 *
 * Race-correctness while saving (codex 5.5 H-finding):
 *   - `saving={true}` from the caller (driven by mutation isPending)
 *     causes the editor to enter read-only mode via
 *     `editor.setEditable(false)` and BOTH `handleSave` AND the
 *     `⌘↵` hotkey early-return. Without these, a second save fires
 *     during the in-flight mutation (duplicate PATCH) AND any post-
 *     submit typing gets dropped when `onSuccess` exits edit mode.
 *
 * Hotkey: `⌘↵` saves while focused. `Esc` cancels. Both also surface
 * via the floating action bar buttons.
 */

import { useEffect } from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import { Loader2, X, Check } from "lucide-react";
import {
  markdownEditorExtensions,
  getEditorMarkdown,
} from "@/lib/markdown-editor-extensions";
import { MarkdownBubbleToolbar } from "@/components/features/markdown-bubble-toolbar";
import { useLocale } from "@/i18n";

interface MemoryDetailEditorProps {
  initialContent: string;
  onSave: (markdown: string) => void;
  onCancel: () => void;
  /**
   * When `true`, the editor enters read-only mode AND `handleSave`/
   * `⌘↵` early-return so a second submit can't fire during the
   * caller's in-flight mutation. Caller should pass
   * `mutation.isPending`.
   */
  saving?: boolean;
}

export function MemoryDetailEditor({
  initialContent,
  onSave,
  onCancel,
  saving = false,
}: MemoryDetailEditorProps) {
  const { t } = useLocale();

  const editor = useEditor({
    extensions: markdownEditorExtensions(t.composeModal.bodyPlaceholder),
    content: initialContent,
    immediatelyRender: false,
  });

  // Auto-focus on mount so the user can start typing immediately
  // after entering edit mode.
  useEffect(() => {
    if (!editor) return;
    const handle = window.setTimeout(() => editor.commands.focus("end"), 0);
    return () => window.clearTimeout(handle);
  }, [editor]);

  // Lock the editor while a save is in flight. Read-only mode blocks
  // typing AND blocks the editor's own keymap firing — the inline
  // `handleKeyDown` below adds a defense-in-depth saving guard so
  // the hotkey path can't fire either. Codex 5.5 H-finding.
  useEffect(() => {
    if (!editor) return;
    editor.setEditable(!saving);
  }, [editor, saving]);

  const handleSave = () => {
    if (!editor) return;
    if (saving) return;
    onSave(getEditorMarkdown(editor));
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.shiftKey) {
      e.preventDefault();
      // Same `saving` guard as `handleSave` — keyboard path doesn't
      // route through the disabled button so we re-check here.
      if (!saving) handleSave();
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
    }
  };

  return (
    <div onKeyDown={handleKeyDown}>
      <div className="memax-prose text-fg-1 text-[16px]">
        <EditorContent editor={editor} />
      </div>
      <MarkdownBubbleToolbar editor={editor} />
      {/* Floating action bar — sits at the bottom-right of the body
          while editing. Same dark fill as compose's Save button so
          the affordance reads as "primary commit" across both
          surfaces. Cancel is secondary (ghost). */}
      <div className="mt-4 flex items-center justify-end gap-2 text-[12px] text-fg-3">
        <span aria-hidden className="mr-auto">
          {t.composeModal.hotkeyHint}
        </span>
        <button
          type="button"
          onClick={onCancel}
          aria-label={t.note.actions.cancelEdit}
          className="inline-flex items-center gap-1.5 rounded-chrome px-3 py-1.5 text-[13px] font-medium text-fg-2 hover:bg-foreground/5 transition-colors cursor-pointer"
        >
          <X className="h-3.5 w-3.5" />
          {t.note.actions.cancelEdit}
        </button>
        <button
          type="button"
          onClick={handleSave}
          disabled={saving}
          className="inline-flex items-center gap-1.5 rounded-chrome bg-foreground px-3 py-1.5 text-[13px] font-medium text-background transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40 cursor-pointer"
        >
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Check className="h-3.5 w-3.5" />
          )}
          {t.note.actions.saveEdit}
        </button>
      </div>
    </div>
  );
}
