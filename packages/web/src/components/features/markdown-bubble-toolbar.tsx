"use client";

/**
 * MarkdownBubbleToolbar — selection-anchored floating toolbar for the
 * markdown editor surfaces (compose modal + memory-detail body editor).
 *
 * Plan 26 phase 5: until now the editor exposed no quick formatting
 * affordance — users had to remember markdown shortcuts (`**bold**`,
 * `*italic*`, `` `code` ``, `[label](url)`) or know the keyboard
 * commands. The bubble toolbar appears when text is selected and
 * exposes the four most-used inline marks plus a heading toggle so
 * the editor reads as a real WYSIWYG capture surface.
 *
 * Why a single shared component:
 *   - Both editor surfaces use the same `markdownEditorExtensions()`
 *     factory, so they share the same mark schema (StarterKit's bold,
 *     italic, code + Link). The toolbar is a thin React shell over
 *     `editor.chain().focus()` calls — it's surface-agnostic by
 *     construction.
 *   - One source for the visual treatment means the two surfaces
 *     never drift.
 *
 * Material choice — why NOT `.glass-pill`:
 *   The kitchen's small-interactive glass recipe (.glass-pill, 52%
 *   --background + 12px blur) only renders correctly when the element
 *   has NO transformed ancestor. Tiptap's `<BubbleMenu>` wraps its
 *   children in a Floating-UI / @tiptap/react/menus positioner that
 *   uses `transform: translate3d(...)` to track the selection. That
 *   transform creates a compositing layer, and `backdrop-filter`
 *   inside it stops sampling the page below — the blur reads as
 *   nearly flat. Instead this toolbar uses a near-opaque card
 *   material (`bg-card/95`) plus a soft drop shadow that visually
 *   reads as glass without depending on backdrop-filter. Same logic
 *   memory-detail's notification-card had to apply (glass-panel-
 *   opaque) for transformed surfaces.
 *
 * BubbleMenu primitive comes from `@tiptap/react/menus` (subpath
 * import). Tiptap v3 ships a separate package per menu type so apps
 * that don't use bubble menus don't pay for the extension code.
 *
 * Active-state visuals: each button's `data-active` attribute drives
 * the bg-surface-2 highlight. Tiptap exposes `editor.isActive('mark')`
 * synchronously; reading it during render is cheap and reactive
 * because BubbleMenu re-renders on selection changes.
 */

import { BubbleMenu } from "@tiptap/react/menus";
import type { Editor } from "@tiptap/react";
import {
  Bold,
  Code,
  Italic,
  Link as LinkIcon,
  Strikethrough,
} from "lucide-react";
import { useLocale } from "@/i18n";

interface MarkdownBubbleToolbarProps {
  editor: Editor | null;
}

interface ToolbarButtonProps {
  onClick: () => void;
  active: boolean;
  ariaLabel: string;
  children: React.ReactNode;
}

function ToolbarButton({
  onClick,
  active,
  ariaLabel,
  children,
}: ToolbarButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={ariaLabel}
      aria-pressed={active}
      data-active={active ? "" : undefined}
      // mousedown preventDefault keeps the editor selection alive when
      // clicking a toolbar button — without this, clicking would blur
      // the editor (collapsing the selection) before the chain runs,
      // and the format would apply to nothing.
      onMouseDown={(e) => e.preventDefault()}
      className="flex h-7 w-7 items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-1 cursor-pointer data-[active]:bg-surface-2 data-[active]:text-foreground"
    >
      {children}
    </button>
  );
}

export function MarkdownBubbleToolbar({ editor }: MarkdownBubbleToolbarProps) {
  const { t } = useLocale();
  if (!editor) return null;

  const promptForLink = () => {
    if (typeof window === "undefined") return;
    const previous = editor.getAttributes("link").href as string | undefined;
    const url = window.prompt(t.composeModal.linkPromptLabel, previous ?? "");
    if (url === null) return; // user cancelled
    if (url === "") {
      editor.chain().focus().extendMarkRange("link").unsetLink().run();
      return;
    }
    editor.chain().focus().extendMarkRange("link").setLink({ href: url }).run();
  };

  return (
    <BubbleMenu
      editor={editor}
      // Render only when the user has a non-empty selection (default
      // tiptap behavior) — prevents the toolbar from following every
      // caret movement during typing.
      options={{ placement: "top" }}
    >
      <div className="flex items-center gap-0.5 rounded-full border border-border/40 bg-card/95 p-1 shadow-lg">
        <ToolbarButton
          onClick={() => editor.chain().focus().toggleBold().run()}
          active={editor.isActive("bold")}
          ariaLabel={t.composeModal.toolbarBoldAria}
        >
          <Bold className="h-3.5 w-3.5" strokeWidth={2.4} />
        </ToolbarButton>
        <ToolbarButton
          onClick={() => editor.chain().focus().toggleItalic().run()}
          active={editor.isActive("italic")}
          ariaLabel={t.composeModal.toolbarItalicAria}
        >
          <Italic className="h-3.5 w-3.5" strokeWidth={2.4} />
        </ToolbarButton>
        <ToolbarButton
          onClick={() => editor.chain().focus().toggleStrike().run()}
          active={editor.isActive("strike")}
          ariaLabel={t.composeModal.toolbarStrikeAria}
        >
          <Strikethrough className="h-3.5 w-3.5" strokeWidth={2.4} />
        </ToolbarButton>
        <ToolbarButton
          onClick={() => editor.chain().focus().toggleCode().run()}
          active={editor.isActive("code")}
          ariaLabel={t.composeModal.toolbarCodeAria}
        >
          <Code className="h-3.5 w-3.5" strokeWidth={2.4} />
        </ToolbarButton>
        <span aria-hidden className="mx-0.5 h-4 w-px bg-border/60" />
        <ToolbarButton
          onClick={promptForLink}
          active={editor.isActive("link")}
          ariaLabel={t.composeModal.toolbarLinkAria}
        >
          <LinkIcon className="h-3.5 w-3.5" strokeWidth={2.4} />
        </ToolbarButton>
      </div>
    </BubbleMenu>
  );
}
