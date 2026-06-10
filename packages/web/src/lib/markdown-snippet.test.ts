/**
 * Regression tests for @memaxlabs/ui's MarkdownSnippet.
 *
 * This primitive owns two jobs:
 *   1. Full inline-markdown rendering for DETAIL views (bold / italic / code
 *      come through as <strong> / <em> / <code>).
 *   2. Flat rendering for LIST PREVIEWS — strips all inline emphasis so the
 *      row has a single scan anchor (the title). See kitchen section 4
 *      "Summary rendering — flat in rows, full in detail" for the rule.
 *
 * These tests lock in both paths so future refactors can't silently flip a
 * list preview back to multi-anchor. If a test here fails, read the kitchen
 * card BEFORE loosening the assertion — the card is the SoT.
 *
 * Authored in .ts (no JSX) to avoid web's tsconfig jsx:"preserve" setting,
 * which causes vitest to emit raw JSX that breaks at runtime. React.createElement
 * is the portable equivalent and works across any transformer.
 */
import { createElement, type ReactElement } from "react";
import { describe, it, expect } from "vitest";
import { renderToString } from "react-dom/server";
import { MarkdownSnippet } from "@memaxlabs/ui";

interface SnippetOpts {
  flat?: boolean;
  maxLen?: number;
  highlight?: string;
}

function render(text: string, opts: SnippetOpts = {}): string {
  return renderToString(
    createElement(MarkdownSnippet, { text, ...opts }) as ReactElement,
  );
}

describe("MarkdownSnippet — flat mode (list previews)", () => {
  it("strips **bold** markers", () => {
    const html = render("Hello **world**", { flat: true });
    expect(html).not.toContain("<strong");
    expect(html).toContain("Hello world");
  });

  it("strips *italic* markers", () => {
    const html = render("Hello *world*", { flat: true });
    expect(html).not.toContain("<em");
    expect(html).toContain("Hello world");
  });

  it("strips `code` markers", () => {
    const html = render("Run `pnpm build`", { flat: true });
    expect(html).not.toContain("<code");
    expect(html).toContain("Run pnpm build");
  });

  it("strips the canonical AI distillation label pattern", () => {
    // This is the exact shape of summary the distillation prompt emits today
    // and the reason flat mode exists. If this ever fails, the preview is
    // regressing into multi-anchor territory — revisit kitchen section 4
    // before relaxing the assertion.
    const html = render(
      "**Inbox positioning decided:** desktop places entry in top-right chrome",
      { flat: true },
    );
    expect(html).not.toContain("<strong");
    expect(html).toContain(
      "Inbox positioning decided: desktop places entry in top-right chrome",
    );
  });

  it("strips mixed emphasis in one string", () => {
    const html = render("**bold** and *italic* and `code`", { flat: true });
    expect(html).not.toContain("<strong");
    expect(html).not.toContain("<em");
    expect(html).not.toContain("<code");
    expect(html).toContain("bold and italic and code");
  });

  it("preserves plain text unchanged", () => {
    const html = render("Just plain text with no emphasis", { flat: true });
    expect(html).toContain("Just plain text with no emphasis");
  });

  it("handles empty input", () => {
    expect(() => render("", { flat: true })).not.toThrow();
  });

  it("flat mode still highlights search matches", () => {
    const html = render("**Project memax** ships tomorrow", {
      flat: true,
      highlight: "memax",
    });
    expect(html).not.toContain("<strong");
    // highlight renders as <mark>
    expect(html).toContain("<mark");
    expect(html).toContain("memax");
  });
});

describe("MarkdownSnippet — default mode (detail views)", () => {
  it("renders <strong> for **bold**", () => {
    const html = render("Hello **world**");
    expect(html).toContain("<strong");
    expect(html).toContain("world");
  });

  it("renders <em> for *italic*", () => {
    const html = render("Hello *world*");
    expect(html).toContain("<em");
  });

  it("renders <code> for `code`", () => {
    const html = render("Run `pnpm build`");
    expect(html).toContain("<code");
  });

  it("renders the canonical distillation pattern as bold lead-in", () => {
    // Detail view is where **Label:** is allowed to feel like a lead-in.
    // If this fails, default mode has been accidentally flattened.
    const html = render("**Inbox positioning decided:** desktop places entry");
    expect(html).toContain("<strong");
    expect(html).toContain("Inbox positioning decided:");
  });
});

describe("MarkdownSnippet — truncation", () => {
  it("truncates plain text around maxLen", () => {
    const long = "a".repeat(200);
    const html = render(long, { maxLen: 50 });
    const aCount = (html.match(/a/g) || []).length;
    expect(aCount).toBeLessThanOrEqual(60);
  });

  it("shows ellipsis when text exceeds maxLen", () => {
    const html = render("a".repeat(200), { maxLen: 50 });
    expect(html).toContain("…");
  });

  it("does not show ellipsis when text fits", () => {
    const html = render("short text", { maxLen: 120 });
    expect(html).not.toContain("…");
  });
});

describe("MarkdownSnippet — block markdown stripping (always on)", () => {
  it("strips H1..H6 headings", () => {
    const html = render("# Big heading\nbody line", { flat: true });
    expect(html).toContain("Big heading");
    expect(html).toContain("body line");
    expect(html).not.toContain("#");
  });

  it("strips code fences entirely (content is dropped, not inlined)", () => {
    const html = render("before ```\ncode block\n``` after", { flat: true });
    expect(html).toContain("before");
    expect(html).toContain("after");
    expect(html).not.toContain("```");
  });

  it("strips unordered list markers", () => {
    const html = render("- item one\n- item two", { flat: true });
    expect(html).toContain("item one");
    expect(html).toContain("item two");
  });

  it("strips ordered list numbers", () => {
    const html = render("1. first\n2. second", { flat: true });
    expect(html).toContain("first");
    expect(html).toContain("second");
  });

  it("collapses newlines to spaces", () => {
    const html = render("line one\nline two", { flat: true });
    expect(html).toContain("line one line two");
  });

  it("normalizes __bold__ to render as strong in default mode", () => {
    const html = render("__emphasized__");
    expect(html).toContain("<strong");
  });
});

describe("MarkdownSnippet — broken / truncated input does not throw", () => {
  it("unclosed **bold", () => {
    expect(() => render("**unclosed bold text")).not.toThrow();
  });

  it("stray single *", () => {
    expect(() => render("hello * world")).not.toThrow();
  });

  it("mismatched `code", () => {
    expect(() => render("hello `code")).not.toThrow();
  });

  it("input cut mid-token at boundary", () => {
    // The snippet's boundary-token fixup should salvage this.
    expect(() =>
      render("a **bold phrase that gets", { maxLen: 20 }),
    ).not.toThrow();
  });
});
