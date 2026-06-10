// @vitest-environment jsdom

/**
 * Markdown round-trip golden tests for the compose modal's editor.
 *
 * What these tests protect (plan 20 §5.3, P1-11):
 *
 *   The compose modal saves memories as `content_type: "markdown"`.
 *   The user types markdown shortcuts (`# `, `- `, `**`, `>`, `` ` ``,
 *   `[text](url)`, `![alt](url)`) which the editor renders inline as
 *   the user goes. On save we serialize the editor state back to
 *   markdown via `tiptap-markdown`'s `getMarkdown()`. If the
 *   serializer drops or transforms a syntax in a way the user didn't
 *   expect, their memory's stored content drifts away from what they
 *   wrote — a bug only catchable by golden round-trip tests.
 *
 * We instantiate a real Editor with the EXACT extension config
 * compose-modal uses (StarterKit + Link + Image + Typography +
 * Markdown), feed it a markdown input, then read getMarkdown() back.
 * The asserted output is "byte-equal-or-canonicalized" — Tiptap's
 * markdown serializer normalizes whitespace and emphasis style, so
 * we accept canonical equivalents (e.g., `*foo*` → `*foo*`,
 * `_foo_` → `*foo*`) but flag any LOSS or ADDITION of meaning.
 */

import { describe, it, expect, afterEach } from "vitest";
import { Editor } from "@tiptap/react";
import {
  markdownEditorExtensions,
  getEditorMarkdown,
} from "@/lib/markdown-editor-extensions";

let activeEditor: Editor | null = null;

function makeEditor(content: string): Editor {
  // Use the SAME shared factory production consumes (compose modal +
  // memory detail editor). Any drift between this test's editor and
  // the production editors would mean the round-trip contract isn't
  // actually being validated for the surfaces that ship.
  const editor = new Editor({
    extensions: markdownEditorExtensions("placeholder"),
    content,
  });
  activeEditor = editor;
  return editor;
}

function roundtrip(input: string): string {
  return getEditorMarkdown(makeEditor(input));
}

afterEach(() => {
  activeEditor?.destroy();
  activeEditor = null;
});

describe("compose markdown round-trip", () => {
  it("preserves an h1 heading", () => {
    expect(roundtrip("# Hello world").trim()).toBe("# Hello world");
  });

  it("preserves an h2 heading", () => {
    expect(roundtrip("## Sub heading").trim()).toBe("## Sub heading");
  });

  it("preserves a bullet list", () => {
    const out = roundtrip("- one\n- two\n- three").trim();
    expect(out).toContain("- one");
    expect(out).toContain("- two");
    expect(out).toContain("- three");
  });

  it("preserves an ordered list", () => {
    const out = roundtrip("1. first\n2. second\n3. third").trim();
    expect(out).toContain("1. first");
    expect(out).toContain("2. second");
    expect(out).toContain("3. third");
  });

  it("preserves bold text", () => {
    expect(roundtrip("This is **bold** word.").trim()).toBe(
      "This is **bold** word.",
    );
  });

  it("preserves italic text", () => {
    // tiptap-markdown emits `*` for italic emphasis; both `*foo*` and
    // `_foo_` are valid markdown but the serializer canonicalizes to
    // `*` — assert the canonical form.
    expect(roundtrip("This is *italic* word.").trim()).toBe(
      "This is *italic* word.",
    );
  });

  it("preserves combined bold+italic", () => {
    // Markdown rules: `***foo***` is bold+italic. Serializer may emit
    // `***foo***` or the equivalent `**_foo_**` form. Assert that the
    // word survives with both marks via DOM probe (parsed bold AND
    // italic), not the literal punctuation pattern.
    const editor = makeEditor("This has ***both*** marks.");
    const html = editor.getHTML();
    expect(html).toMatch(/<strong[^>]*>.*<em[^>]*>both<\/em>.*<\/strong>/);
  });

  it("preserves an inline link", () => {
    const out = roundtrip("Visit [memax](https://memax.app) today.").trim();
    expect(out).toContain("[memax](https://memax.app)");
  });

  it("preserves a fenced code block", () => {
    const out = roundtrip("```ts\nconst x = 1;\n```").trim();
    expect(out).toContain("```");
    expect(out).toContain("const x = 1;");
  });

  it("preserves inline code", () => {
    expect(roundtrip("Run `pnpm test` here.").trim()).toBe(
      "Run `pnpm test` here.",
    );
  });

  it("preserves a blockquote", () => {
    const out = roundtrip("> Quoted line").trim();
    expect(out).toContain("> Quoted line");
  });

  it("preserves an image", () => {
    const out = roundtrip("![alt text](https://example.com/img.png)").trim();
    expect(out).toContain("![alt text](https://example.com/img.png)");
  });

  it("Typography input rules don't corrupt straight quotes from markdown source", () => {
    // Typography swaps quote chars at INPUT time (typing `"` produces
    // `"`). When we hydrate from markdown, the source already has the
    // plain ASCII quote, so it should pass through untouched. Without
    // this guard, a stored memory's content could mutate on every
    // edit. tiptap-markdown serializes back to plain ASCII quotes.
    expect(roundtrip('She said "hello".').trim()).toBe('She said "hello".');
  });

  it("preserves multi-paragraph content", () => {
    const input = "First para.\n\nSecond para.\n\nThird para.";
    const out = roundtrip(input).trim();
    expect(out).toContain("First para.");
    expect(out).toContain("Second para.");
    expect(out).toContain("Third para.");
  });

  it("preserves nested list (one level)", () => {
    const input = "- parent\n  - child\n- sibling";
    const out = roundtrip(input).trim();
    expect(out).toContain("parent");
    expect(out).toContain("child");
    expect(out).toContain("sibling");
  });

  it("preserves a thematic break", () => {
    // `---` between paragraphs is a horizontal rule. StarterKit's
    // HorizontalRule extension handles input rules + serialization.
    // tiptap-markdown emits `---` (or any of the canonical thematic-
    // break variants); assert that one appears as its own line
    // somewhere in the output. Codex 5.5 caught the prior regex's
    // false-positive (the `^|\n` start-of-string alternative
    // matched even when no break was present).
    const input = "Above break.\n\n---\n\nBelow break.";
    const out = roundtrip(input).trim();
    expect(out).toContain("Above break.");
    expect(out).toContain("Below break.");
    const lines = out.split("\n").map((line) => line.trim());
    const hasBreak = lines.some(
      (line) => line === "---" || line === "***" || line === "___",
    );
    expect(hasBreak).toBe(true);
  });

  it("preserves hard breaks within a paragraph", () => {
    // Two trailing spaces + newline = hard break. tiptap-markdown may
    // serialize as backslash-newline (`\\\n`) or two-space-newline
    // depending on canonicalization. Assert both lines survive in
    // order on the same logical paragraph.
    const editor = makeEditor("Line one  \nLine two");
    const html = editor.getHTML();
    expect(html).toMatch(/Line one[\s\S]*<br[\s\S]*>[\s\S]*Line two/);
  });
});
