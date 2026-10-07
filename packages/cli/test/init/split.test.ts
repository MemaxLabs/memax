// Splitting agent files into statements: deterministic, with fixtures for
// every shape agent files take.
import { describe, expect, it } from "vitest";
import {
  splitMarkdown,
  sectionFor,
  MAX_STATEMENTS_PER_FILE,
} from "../../src/lib/init/split.js";
import {
  countHidden,
  describeHidden,
  totalHidden,
} from "../../src/lib/init/hidden.js";

const texts = (md: string) =>
  splitMarkdown(md, "repository").statements.map((s) => s.text);

describe("splitMarkdown", () => {
  it("makes list items and paragraphs statements, with their headings and lines", () => {
    const md = [
      "# Acme",
      "",
      "This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.",
      "",
      "## Testing",
      "",
      "- Run tests with `pnpm test`.",
      "* Use `pnpm vitest run` for one file,",
      "  and `--watch` while you work.",
      "1. Never commit `.only`.",
      "",
      "## Decisions",
      "",
      "- Background jobs use River, not Temporal.",
      "",
      "Deploys go through Fly.io.",
      "Previews too.",
    ].join("\n");
    const out = splitMarkdown(md, "repository");
    expect(out.statements.map((s) => [s.line, s.text])).toEqual([
      [7, "Run tests with `pnpm test`."],
      [8, "Use `pnpm vitest run` for one file, and `--watch` while you work."],
      [10, "Never commit `.only`."],
      [14, "Background jobs use River, not Temporal."],
      [16, "Deploys go through Fly.io. Previews too."],
    ]);
    expect(out.statements[0].heading).toEqual(["Acme", "Testing"]);
    expect(out.statements[1].endLine).toBe(9);
    expect(out.statements[0]).toMatchObject({
      section: "conventions",
      kind: "fact",
    });
    expect(out.statements[3]).toMatchObject({
      section: "decisions",
      kind: "decision",
    });
    // Claude Code's /init line about the file itself is not a statement.
    expect(out.skipped.boilerplate).toBe(1);
  });

  it("carries a lead-in into the items under it", () => {
    const md = [
      "## Commands",
      "- Testing:",
      "  - Run `pnpm test`",
      "  - Run `pnpm e2e` before release",
      "- Lint with `pnpm lint`",
      "",
      "Read these first:",
      "",
      "- docs/plans/02-system-architecture.md",
      "",
      "Notes:",
      "Nothing here yet.",
    ].join("\n");
    expect(texts(md)).toEqual([
      "Testing: Run `pnpm test`",
      "Testing: Run `pnpm e2e` before release",
      "Lint with `pnpm lint`",
      "Read these first: docs/plans/02-system-architecture.md",
      "Notes: Nothing here yet.",
    ]);
  });

  it("skips code, comments, imports, frontmatter and Memax's block", () => {
    const md = [
      "---",
      "description: Testing rules",
      "globs: **/*.test.ts,**/*.test.tsx",
      "alwaysApply: false",
      "---",
      "# Testing",
      "```bash",
      "pnpm test # not a statement",
      "```",
      "~~~",
      "- not an item",
      "~~~",
      "<!-- a note to self -->",
      "<!--",
      "- hidden in a comment",
      "-->",
      "@AGENTS.md",
      "<!-- memax:start -->",
      "- Memax wrote this line",
      "<!-- memax:end -->",
      "- Mock nothing but the network.",
      "> A quoted tip:",
      "> ```",
      "> rm -rf node_modules",
      "> ```",
    ].join("\n");
    const out = splitMarkdown(md, "repository");
    expect(out.statements.map((s) => s.text)).toEqual([
      "Mock nothing but the network.",
      "A quoted tip:",
    ]);
    expect(out.paths).toEqual(["**/*.test.ts", "**/*.test.tsx"]);
    expect(out.skipped).toMatchObject({ code: 3, comments: 2, imports: 1 });
    expect(out.skipped.managed).toBeGreaterThan(0);
  });

  it("reads applyTo and paths: lists as where a file applies", () => {
    expect(
      splitMarkdown(
        "---\napplyTo: 'packages/web/**'\n---\n- Use the ledger tokens.",
        "repository",
      ).paths,
    ).toEqual(["packages/web/**"]);
    expect(
      splitMarkdown(
        "---\npaths:\n  - src/**/*.go\n  - cmd/**\n---\n- Wrap errors.",
        "repository",
      ).paths,
    ).toEqual(["cmd/**", "src/**/*.go"]);
  });

  it("leaves a file Memax compiled alone, but reads a person's file with a Memax block", () => {
    const compiled =
      "<!-- Compiled by Memax from memax-v2 at C-0881 · edit it at https://memax.app/memax-v2/brief -->\n# Brief\n- Use pnpm. [M-0071]\n";
    expect(splitMarkdown(compiled, "repository")).toMatchObject({
      compiled: true,
      statements: [],
    });
    const owned =
      "# Mine\n- I keep this line.\n<!-- memax:start -->\n<!-- Compiled by Memax from memax-v2 at C-0881 · x -->\n@AGENTS.md\n<!-- memax:end -->\n";
    const out = splitMarkdown(owned, "repository");
    expect(out.compiled).toBe(false);
    expect(out.statements.map((s) => s.text)).toEqual(["I keep this line."]);
  });

  it("makes table rows statements and skips the header", () => {
    const md = [
      "| Command | What |",
      "| --- | --- |",
      "| `pnpm test` | unit tests |",
      "| `pnpm e2e` | browser tests |",
    ].join("\n");
    expect(texts(md)).toEqual([
      "`pnpm test` — unit tests",
      "`pnpm e2e` — browser tests",
    ]);
  });

  it("reads setext headings, task lists and long paragraphs", () => {
    const long = Array.from(
      { length: 8 },
      (_, i) =>
        `Sentence number ${i + 1} explains one more part of the deploy, in some detail.`,
    ).join(" ");
    const md = [
      "Release",
      "=======",
      "",
      "- [x] Tag the release.",
      "- [ ] Announce it.",
      "",
      long,
    ].join("\n");
    const out = splitMarkdown(md, "repository");
    expect(out.statements[0]).toMatchObject({
      text: "Tag the release.",
      heading: ["Release"],
    });
    expect(out.statements[1].text).toBe("Announce it.");
    expect(out.statements.length).toBe(2 + 8);
  });

  it("strips hidden characters and counts them by kind", () => {
    const md = "- Run\u{202E} tests\u{200B} with pnpm\u{E0041}\u{E0042}.\n";
    const out = splitMarkdown(md, "repository");
    expect(out.statements[0].text).toBe("Run tests with pnpm.");
    expect(out.statements[0].hidden).toMatchObject({
      bidi: 1,
      zero_width: 1,
      tag: 2,
    });
    expect(totalHidden(out.hidden)).toBe(4);
    expect(describeHidden(out.hidden)).toBe(
      "1 bidi control, 1 zero-width character, 2 tag characters",
    );
  });

  it("stops at the per-file limit and reports what is too long", () => {
    const md =
      Array.from(
        { length: MAX_STATEMENTS_PER_FILE + 5 },
        (_, i) => `- Fact number ${i}.`,
      ).join("\n") + `\n\n- ${"x".repeat(2001)}`;
    const out = splitMarkdown(md, "repository");
    expect(out.statements.length).toBe(MAX_STATEMENTS_PER_FILE);
    expect(out.overLimit.length).toBe(5);
    expect(out.tooLong.length).toBe(1);
  });

  it("is deterministic", () => {
    const md = "# A\n- one\n- two:\n  - three\n";
    expect(splitMarkdown(md, "home")).toEqual(splitMarkdown(md, "home"));
  });
});

describe("sectionFor", () => {
  it("maps headings onto the Brief's sections", () => {
    expect(
      sectionFor(["Acme", "Architecture decisions"], "repository"),
    ).toEqual({ section: "decisions", kind: "decision" });
    expect(sectionFor(["Open questions"], "repository").section).toBe(
      "open_question",
    );
    expect(sectionFor(["My preferences"], "home").section).toBe("preferences");
    expect(sectionFor(["Style"], "repository").section).toBe("conventions");
    expect(sectionFor([], "home").section).toBe("preferences");
  });
});

describe("countHidden", () => {
  it("ignores line breaks and tabs and names every kind", () => {
    expect(totalHidden(countHidden("a\tb\nc\r"))).toBe(0);
    expect(countHidden("\u{2066}x\u{2069}\u{FE0F}\u{0007}")).toEqual({
      bidi: 2,
      zero_width: 0,
      tag: 0,
      variation: 1,
      other: 1,
    });
  });
});
