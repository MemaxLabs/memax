import { describe, expect, it } from "vitest";
import {
  hasMorePreviewLines,
  previewLines,
  renderMarkdownFragment,
} from "./recall.js";

describe("previewLines", () => {
  it("drops blank lines and preserves only the requested number of lines", () => {
    expect(previewLines("one\n\n two \n\nthree\nfour", 2)).toEqual([
      "one",
      " two",
    ]);
  });
});

describe("hasMorePreviewLines", () => {
  it("is true when there are more non-empty lines than the limit", () => {
    expect(hasMorePreviewLines("one\n\ntwo\nthree", 2)).toBe(true);
  });

  it("is false when the visible lines fit within the limit", () => {
    expect(hasMorePreviewLines("one\n\ntwo", 2)).toBe(false);
  });
});

describe("renderMarkdownFragment", () => {
  it("renders lightweight markdown fragments without requiring full document structure", () => {
    expect(
      renderMarkdownFragment("## Heading\n- item one\nRegular `code` text", {
        indent: "    ",
      }),
    ).toEqual(["    Heading", "    • item one", "    Regular code text"]);
  });

  it("ignores fenced code markers and renders the code lines", () => {
    expect(
      renderMarkdownFragment("```md\nconst x = 1;\n```", {
        indent: "    ",
      }),
    ).toEqual(["    const x = 1;"]);
  });
});
