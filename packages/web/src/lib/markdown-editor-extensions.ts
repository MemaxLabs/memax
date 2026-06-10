"use client";

/**
 * Shared Tiptap extension config for the memax markdown editor surface.
 *
 * Single source of truth consumed by:
 *   - `<ComposeModal>` body editor (plan 20)
 *   - `<MemoryDetailEditor>` inline body editor (plan 21 phase 3)
 *
 * Extracted so the two surfaces stay byte-identical in capability:
 *   - Same markdown shortcuts (`# `, `- `, `**`, `>`, `` ` ``,
 *     `[text](url)`, `![alt](url)`)
 *   - Same Link semantics (`openOnClick: false` — Notion convention,
 *     editor surface doesn't navigate on left-click; `Cmd+click` still
 *     follows)
 *   - Same image / typography / serializer behavior
 *   - Same StarterKit `link: false` opt-out (the bundled Link
 *     conflicts with our explicit Link config)
 *
 * The compose markdown round-trip golden tests at
 * `compose-markdown-roundtrip.dom.test.tsx` exercise this exact
 * config — both surfaces inherit that coverage.
 *
 * Future surfaces (e.g. inline reply, chat composition) consume this
 * helper rather than re-listing extensions; that prevents drift like
 * "compose got Image but reply didn't" and keeps the round-trip
 * contract one shape.
 *
 * The factory takes a `placeholder` because that's the only piece
 * each call site customizes; everything else is identical.
 */

import StarterKit from "@tiptap/starter-kit";
import Placeholder from "@tiptap/extension-placeholder";
import Link from "@tiptap/extension-link";
import Image from "@tiptap/extension-image";
import Typography from "@tiptap/extension-typography";
import { Markdown } from "tiptap-markdown";
import type { AnyExtension, Editor } from "@tiptap/react";

/**
 * Build the markdown-editor extension list. Call site supplies
 * `placeholder` text; everything else is shared.
 */
export function markdownEditorExtensions(placeholder: string): AnyExtension[] {
  return [
    // StarterKit ships its own Link in Tiptap v3; disable so our
    // explicit Link config wins (without this, runtime emits
    // "Duplicate extension names found: ['link']").
    StarterKit.configure({ link: false }),
    Placeholder.configure({ placeholder }),
    // openOnClick:false — editor surface doesn't navigate on
    // left-click. Cmd+click still works because that path goes
    // through the browser, not Tiptap's internal handler. This
    // matches Notion/Linear/Bear and avoids the "user accidentally
    // navigated away from their unsaved work" footgun.
    Link.configure({
      openOnClick: false,
      autolink: true,
      linkOnPaste: true,
      HTMLAttributes: {
        rel: "noopener noreferrer",
        target: "_blank",
      },
    }),
    Image,
    Typography,
    Markdown.configure({ html: false }),
  ];
}

/**
 * Editor-level keydown handler that consumes browser-history chord
 * keys (`Cmd+ArrowLeft` / `Cmd+ArrowRight`) so the user doesn't
 * accidentally navigate away while editing. macOS browsers default
 * those chords to history back/forward — fine when not in a text
 * field, but inside an editor users expect cursor motion (or at
 * worst, a no-op) and definitely not a route change. Without this
 * hook, hitting `⌘←` while typing in a seed memory pulled the user
 * back to the previously visited admin route.
 *
 * Pass into useEditor via `editorProps: { handleKeyDown: ... }`.
 * Returning `true` tells ProseMirror "I handled this" and prevents
 * default; the browser's history shortcut never fires.
 */
export function consumeBrowserHistoryChords(
  _view: unknown,
  event: KeyboardEvent,
): boolean {
  if (!event.metaKey && !event.ctrlKey) return false;
  if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return false;
  event.preventDefault();
  return true;
}

interface MarkdownStorage {
  getMarkdown: () => string;
}

/**
 * Read serialized markdown out of a Tiptap editor instance configured
 * with the shared `markdownEditorExtensions`. Centralizes the cast
 * required for the `tiptap-markdown` storage augmentation, which
 * doesn't ship a TypeScript module declaration we can pick up.
 */
export function getEditorMarkdown(editor: Editor): string {
  return (
    (editor.storage as unknown as Record<string, unknown>)[
      "markdown"
    ] as MarkdownStorage
  ).getMarkdown();
}
